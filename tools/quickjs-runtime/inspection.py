"""Exact platform closure, native ELF budgets, and C/Zig frame receipts."""
import importlib.util
import json
from pathlib import Path
import re
import struct

ROOT = Path(__file__).resolve().parents[2]
INVENTORY = json.loads((ROOT / "user/zig/quickjs/inventory.json").read_text())
PRIVATE = {
    "qjs_native_assert", "qjs_native_charge_interrupt_site", "qjs_native_check_alloca",
    "qjs_native_checkpoint", "qjs_native_emit", "qjs_native_hosted_diagnostic",
    "qjs_native_out_of_memory", "qjs_native_stack_failure", "qjs_native_step",
    "qjs_native_unsupported_feature", "qjs_native_refuse_feature",
    "qjs_guard_save", "qjs_guard_restore", "qjs_guard_enter", "qjs_guard_leave",
    "qjs_guard_high_water",
    "qjs_native_checked_alloca_size",
}
COMPILER_RT = {"__udivti3"}


def symbols(data):
    if data[:7] != b"\x7fELF\x02\x01\x01":
        raise ValueError("UnexpectedObjectFormat")
    if struct.unpack_from("<HH", data, 16) != (1, 183):
        raise ValueError("UnexpectedObjectTarget")
    offset = struct.unpack_from("<Q", data, 40)[0]
    width, count = struct.unpack_from("<HH", data, 58)
    sections = [struct.unpack_from("<II4QII2Q", data, offset + i * width) for i in range(count)]
    defined, undefined = set(), set()
    for section in sections:
        if section[1] != 2:
            continue
        strings = sections[section[6]]
        table = data[strings[4]:strings[4] + strings[5]]
        for pos in range(section[4], section[4] + section[5], section[9]):
            name, info, _other, index, _value, _size = struct.unpack_from("<IBBHQQ", data, pos)
            if name and info >> 4 in (1, 2):
                name = table[name:table.index(b"\0", name)].decode()
                (defined if index else undefined).add(name)
    return defined, undefined


def imports(paths):
    defined, undefined = set(), set()
    for path in paths:
        local, external = symbols(path.read_bytes())
        defined |= local
        undefined |= external
    canonical = set(INVENTORY["provided"]) | set(INVENTORY["stubbed"])
    canonical.remove("assert")
    unexpected = undefined - defined - canonical - PRIVATE - COMPILER_RT
    refused = (defined | undefined) & set(INVENTORY["refused"])
    if unexpected or refused:
        raise ValueError("UnexpectedImport: " + ",".join(sorted(unexpected | refused)))
    return {
        "remaining_c_external_names": sorted(undefined - defined),
        "compiler_rt_external_names": sorted((undefined - defined) & COMPILER_RT),
        "provided_count": len(INVENTORY["provided"]),
        "stubbed_count": len(INVENTORY["stubbed"]),
        "refused_count": len(INVENTORY["refused"]),
        "refused_features_count": len(INVENTORY["refused_features"]),
    }


def c_frames(assembly):
    frames, dynamic = {}, set()
    function = None
    for line in assembly.splitlines():
        line = line.strip()
        if re.search(r"\b[wx]18\b", line.split("//")[0]) and not line.startswith("."):
            raise ValueError("ReservedX18")
        match = re.fullmatch(r"\.type\s+(.+),@function", line)
        if match:
            function = match[1]
            frames[function] = 0
        if line.startswith(".size"):
            function = None
        if function is None:
            continue
        code = line.split("//")[0].strip()
        match = re.fullmatch(r"sub\s+sp, sp, #(\d+)(?:, lsl #(\d+))?", code)
        if match:
            frames[function] += int(match[1]) << int(match[2] or 0)
        elif re.search(r"\[sp, #-(\d+)\]!", code):
            frames[function] += int(re.search(r"\[sp, #-(\d+)\]!", code)[1])
        elif re.match(r"\w+\s+sp,", code) and not re.fullmatch(r"add\s+sp, sp, #\d+(?:, lsl #\d+)?", code):
            dynamic.add(function)
    if not frames or max(frames.values()) > 8192:
        raise ValueError("CFrameBudget")
    if dynamic - {"JS_CallInternal", "js_call_c_function", "js_call_bound_function", "js_c_function_data_call"}:
        raise ValueError("UnreviewedDynamicCFrame: " + ",".join(sorted(dynamic)))
    return {"static_frames": frames, "maximum_static_frame_bytes": max(frames.values()),
            "dynamic_alloca_frames": sorted(dynamic),
            "dynamic_policy": "All four alloca frames use the prospectively checked private alloca macro; QuickJS's own guard also remains enabled."}


def elf(path):
    spec = importlib.util.spec_from_file_location("sdk_contract", ROOT / "tools/check-zc-host-contract.py")
    checker = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(checker)
    report = checker.check(path.read_bytes(), "sdk")
    if report["file_bytes"] > 4_194_304 or report["mapped_bytes"] > 4_194_304 or report["segments"][1]["memsz"] > 1_048_576:
        raise ValueError("ImageBudget")
    return report


def compiler_rt_object(path, assembly, c_paths):
    """Read AArch64 prologues directly, without an unpinned disassembler."""
    data = path.read_bytes()
    defined, undefined = symbols(data)
    if undefined - {"qjs_native_checkpoint"}:
        raise ValueError("CompilerRtUnexpectedImport: " + ",".join(sorted(undefined)))
    offset = struct.unpack_from("<Q", data, 40)[0]
    width, count = struct.unpack_from("<HH", data, 58)
    sections = [struct.unpack_from("<II4QII2Q", data, offset + i * width) for i in range(count)]
    frames = {}
    function_by_section = {}
    string_index = struct.unpack_from("<H", data, 62)[0]
    section_names = sections[string_index]
    section_strings = data[section_names[4]:section_names[4] + section_names[5]]
    for index, section in enumerate(sections):
        if section[1] == 1 and section[2] & 4 and section[5]:
            name = section_strings[section[0]:section_strings.index(b"\0", section[0])].decode()
            function_by_section[index] = name.removeprefix(".text.")
    for section in sections:
        if section[1] != 2:
            continue
        strings = sections[section[6]]
        table = data[strings[4]:strings[4] + strings[5]]
        for pos in range(section[4], section[4] + section[5], section[9]):
            name, info, _other, index, value, size = struct.unpack_from("<IBBHQQ", data, pos)
            if not name or info & 15 != 2 or not 0 < index < len(sections) or not size:
                continue
            target = sections[index]
            start = target[4] + value
            frame = 0
            for address in range(start, start + size, 4):
                word = struct.unpack_from("<I", data, address)[0]
                if word & 0xff8003ff == 0xd10003ff:  # SUB SP, SP, #imm
                    frame += ((word >> 10) & 0xfff) << (12 if word & (1 << 22) else 0)
                elif word & 0x3b800000 == 0x29800000 and (word >> 5) & 31 == 31:  # STP preindexed SP
                    immediate = (word >> 15) & 127
                    if immediate & 64:
                        immediate -= 128
                    opcode = word >> 30
                    scale = (4 << opcode) if word & (1 << 26) else (8 if opcode == 2 else 4)
                    frame += max(0, -immediate * scale)
                elif word & 0x3b200c00 == 0x38000c00 and (word >> 5) & 31 == 31:  # STR preindexed SP
                    immediate = (word >> 12) & 511
                    if immediate & 256:
                        immediate -= 512
                    frame += max(0, -immediate)
                elif word & 0xffe003ff == 0xcb2003ff:
                    raise ValueError("CompilerRtDynamicStack")
            identifier = table[name:table.index(b"\0", name)].decode()
            frames[identifier] = frame
            function_by_section[index] = identifier
    # The compiler's stripped archive omits local function symbols, but its
    # function sections retain names and exact byte ranges.
    for index, identifier in function_by_section.items():
        if identifier in frames:
            continue
        section = sections[index]
        frame = 0
        for address in range(section[4], section[4] + section[5], 4):
            word = struct.unpack_from("<I", data, address)[0]
            if word & 0xff8003ff == 0xd10003ff:
                frame += ((word >> 10) & 0xfff) << (12 if word & (1 << 22) else 0)
            elif word & 0x3b800000 == 0x29800000 and (word >> 5) & 31 == 31:
                immediate = (word >> 15) & 127
                if immediate & 64:
                    immediate -= 128
                opcode = word >> 30
                scale = (4 << opcode) if word & (1 << 26) else (8 if opcode == 2 else 4)
                frame += max(0, -immediate * scale)
            elif word & 0x3b200c00 == 0x38000c00 and (word >> 5) & 31 == 31:
                immediate = (word >> 12) & 511
                if immediate & 256:
                    immediate -= 512
                frame += max(0, -immediate)
        frames[identifier] = frame
    references = {}
    for section in sections:
        if section[1] != 4 or section[7] not in function_by_section:
            continue
        origin = function_by_section[section[7]]
        symtab = sections[section[6]]
        strings = sections[symtab[6]]
        names = data[strings[4]:strings[4] + strings[5]]
        for position in range(section[4], section[4] + section[5], section[9]):
            _offset, info, _addend = struct.unpack_from("<QQq", data, position)
            if info & 0xffffffff not in (282, 283):  # AArch64 JUMP26/CALL26
                continue
            symbol = symtab[4] + (info >> 32) * symtab[9]
            name, _info, _other, target, _value, _size = struct.unpack_from("<IBBHQQ", data, symbol)
            identifier = names[name:names.index(b"\0", name)].decode() if name else function_by_section.get(target)
            if identifier not in frames and target in function_by_section:
                identifier = function_by_section[target]
            if identifier:
                references.setdefault(origin, set()).add(identifier)
    strong = set(re.findall(r"(?m)^\s*\.globl\s+(\w+)", assembly))
    c_undefined = set()
    for path in c_paths:
        local, external = symbols(path.read_bytes())
        strong |= local
        c_undefined |= external
    roots = (set(re.findall(r"(?m)^\s*(?:bl|b)\s+([A-Za-z_]\w*)", assembly)) | c_undefined) & defined - strong
    pending, reachable = list(roots), set()
    while pending:
        function = pending.pop()
        if function in reachable or function in strong:
            continue
        reachable.add(function)
        pending.extend(references.get(function, set()) & set(frames))
    selected = {name: frames[name] for name in sorted(reachable) if name in frames}
    if not frames or (selected and max(selected.values()) > 8192):
        raise ValueError("CompilerRtFrameBudget: " + str(sorted(frames.items(), key=lambda item: item[1], reverse=True)[:5]))
    return {"archive_defined_names": sorted(defined), "undefined_names": sorted(undefined),
            "linked_roots": sorted(roots), "reachable_function_names": sorted(reachable),
            "static_frames": selected, "maximum_static_frame_bytes": max(selected.values(), default=0),
            "excluded_unreferenced_archive_maximum_frame": max(frames.values())}
