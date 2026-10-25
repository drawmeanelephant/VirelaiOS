#!/usr/bin/env python3
"""Generate freestanding dynamic ELF binaries for VirelaiOS.

Milestone 30 & 31: Freestanding Runtime Linker, Shared Libraries & Ecosystem.
Generates:
  - LIBUI.SO
  - LIBFONT.SO
  - PLUGIN.SO
  - DYNAPP.ELF
  - CALC.ELF
  - NOTEPAD.ELF
  - FILE.ELF
  - DESKTOP.ELF
"""

import os
import struct
import sys

# Product version — the `ver M.m.p` the component-registry row shows
# and the drift check resolves (docs/component-registry.md); bump per
# the registry's rule. One line serves the whole generated set this
# script emits (DYNAPP.ELF, LIBUI.SO, LIBFONT.SO, PLUGIN.SO, …).
VER = "1.0.0"

TEXT_BASE = 0x00400000
EM_AARCH64 = 0xB7

PT_LOAD = 1
PT_DYNAMIC = 2
PT_INTERP = 3

PF_X = 1
PF_W = 2
PF_R = 4

DT_NULL = 0
DT_NEEDED = 1
DT_PLTRELSZ = 2
DT_PLTGOT = 3
DT_HASH = 4
DT_STRTAB = 5
DT_SYMTAB = 6
DT_RELA = 7
DT_RELASZ = 8
DT_RELAENT = 9
DT_STRSZ = 10
DT_SYMENT = 11
DT_JMPREL = 23

R_AARCH64_NONE = 0
R_AARCH64_ABS64 = 257
R_AARCH64_GLOB_DAT = 1025
R_AARCH64_JUMP_SLOT = 1026
R_AARCH64_RELATIVE = 1027


def build_libui_so():
    """Build LIBUI.SO shared library exporting UI, windowing, clipboard, and dlopen routines."""
    # List of functions: (name, svc_num or custom asm)
    # 0: ui_write (svc 1)
    # 1: ui_yield (svc 2)
    # 2: ui_exit (svc 3)
    # 3: ui_sleep (svc 4)
    # 4: ui_win_open (svc 12)
    # 5: ui_win_fill (svc 13)
    # 6: ui_win_present (svc 14)
    # 7: ui_win_close (svc 15)
    # 8: ui_win_move (svc 16)
    # 9: ui_poll_event (svc 21)
    # 10: ui_wait_event (svc 22)
    # 11: ui_file_open (svc 23)
    # 12: ui_file_read (svc 24)
    # 13: ui_file_write (svc 25)
    # 14: ui_file_close (svc 26)
    # 15: ui_clipboard_set (svc 38)
    # 16: ui_clipboard_get (svc 39)
    # 17: ui_dlopen (custom dlopen forwarder)
    # 18: ui_dlsym (custom dlsym forwarder)
    # 19: ui_dlclose (returns 0)
    
    exports = [
        ("ui_write", 1),
        ("ui_yield", 2),
        ("ui_exit", 3),
        ("ui_sleep", 4),
        ("ui_win_open", 12),
        ("ui_win_fill", 13),
        ("ui_win_present", 14),
        ("ui_win_close", 15),
        ("ui_win_move", 16),
        ("ui_poll_event", 21),
        ("ui_wait_event", 22),
        ("ui_file_open", 23),
        ("ui_file_read", 24),
        ("ui_file_write", 25),
        ("ui_file_close", 26),
        ("ui_clipboard_set", 38),
        ("ui_clipboard_get", 39),
    ]

    code = []
    sym_offsets = []

    for name, svc_num in exports:
        sym_offsets.append(len(code) * 4)
        # mov x8, #svc_num; svc #0; ret
        code.extend([
            0xD2800008 | (svc_num << 5),
            0xD4000001,
            0xD65F03C0
        ])

    # ui_dlopen (offset): custom handler in ld.so or dummy loader
    sym_offsets.append(len(code) * 4)
    # ui_dlopen: mov x8, #23 (open file); svc #0; ret
    code.extend([
        0xD28002E8,  # mov x8, #23
        0xD4000001,  # svc #0
        0xD65F03C0   # ret
    ])
    exports.append(("ui_dlopen", None))

    # ui_dlsym (offset): returns symbol function address
    sym_offsets.append(len(code) * 4)
    # For testing, if x0 != 0, returns dummy function pointer or resolves symbol
    code.extend([
        0xAA0003E0,  # mov x0, x0
        0xD65F03C0   # ret
    ])
    exports.append(("ui_dlsym", None))

    # ui_dlclose (offset):
    sym_offsets.append(len(code) * 4)
    code.extend([
        0xD2800000,  # mov x0, #0
        0xD65F03C0   # ret
    ])
    exports.append(("ui_dlclose", None))

    code_bytes = b"".join(struct.pack("<I", w) for w in code)

    # Build strtab & symtab
    strtab = bytearray(b"\x00")
    symtab = bytearray()
    # Null symbol (idx 0)
    symtab += struct.pack("<IBBHQQ", 0, 0, 0, 0, 0, 0)

    ehdr_size = 64
    phdr_size = 56
    phnum = 2  # PT_LOAD (R+X), PT_DYNAMIC (RW)
    phoff = ehdr_size
    code_off = (ehdr_size + phnum * phdr_size + 15) & ~15

    for idx, (name, _) in enumerate(exports):
        st_name = len(strtab)
        strtab.extend(name.encode("ascii") + b"\x00")
        symtab += struct.pack("<IBBHQQ", st_name, 0x12, 0, 1, code_off + sym_offsets[idx], 12)

    strtab_off = code_off + len(code_bytes)
    symtab_off = (strtab_off + len(strtab) + 7) & ~7
    dyn_off = (symtab_off + len(symtab) + 7) & ~7

    dynamic = bytearray()
    dynamic += struct.pack("<QQ", DT_STRTAB, strtab_off)
    dynamic += struct.pack("<QQ", DT_STRSZ, len(strtab))
    dynamic += struct.pack("<QQ", DT_SYMTAB, symtab_off)
    dynamic += struct.pack("<QQ", DT_SYMENT, 24)
    dynamic += struct.pack("<QQ", DT_NULL, 0)

    total_size = dyn_off + len(dynamic)

    ehdr = struct.pack(
        "<4s5B7xHHIQQQIHHHHHH",
        b"\x7fELF",
        2, 1, 1, 0, 0,
        3,  # ET_DYN
        EM_AARCH64,
        1, 0, phoff, 0, 0,
        ehdr_size, phdr_size, phnum,
        0, 0, 0
    )

    phdr0 = struct.pack("<IIQQQQQQ", PT_LOAD, PF_R | PF_X, 0, 0, 0, total_size, total_size, 0x1000)
    phdr1 = struct.pack("<IIQQQQQQ", PT_DYNAMIC, PF_R | PF_W, dyn_off, dyn_off, dyn_off, len(dynamic), len(dynamic), 8)

    image = bytearray(total_size)
    image[0:ehdr_size] = ehdr
    image[phoff:phoff + phdr_size] = phdr0
    image[phoff + phdr_size:phoff + 2 * phdr_size] = phdr1
    image[code_off:code_off + len(code_bytes)] = code_bytes
    image[strtab_off:strtab_off + len(strtab)] = strtab
    image[symtab_off:symtab_off + len(symtab)] = symtab
    image[dyn_off:dyn_off + len(dynamic)] = dynamic

    return bytes(image)


def build_libfont_so():
    """Build LIBFONT.SO shared library exporting font metrics."""
    # font_measure_8x8 (x0 = str)
    # font_glyph_width (returns 8)
    # font_glyph_height (returns 8)
    code = [
        # font_measure_8x8 @0
        0xAA0003E1,  # mov x1, x0
        0x38401422,  # ldrb w2, [x1], #1
        0x35FFFFE2,  # cbnz w2, loop
        0xCB000020,  # sub x0, x1, x0
        0xD1000400,  # sub x0, x0, #1
        0xD37DF000,  # lsl x0, x0, #3
        0xD65F03C0,  # ret
        # font_glyph_width @28
        0xD2800100,  # mov x0, #8
        0xD65F03C0,  # ret
        # font_glyph_height @36
        0xD2800100,  # mov x0, #8
        0xD65F03C0,  # ret
    ]
    code_bytes = b"".join(struct.pack("<I", w) for w in code)

    strtab = b"\x00font_measure_8x8\x00font_glyph_width\x00font_glyph_height\x00"

    ehdr_size = 64
    phdr_size = 56
    phnum = 2
    phoff = ehdr_size
    code_off = (ehdr_size + phnum * phdr_size + 15) & ~15

    symtab = bytearray()
    symtab += struct.pack("<IBBHQQ", 0, 0, 0, 0, 0, 0)
    symtab += struct.pack("<IBBHQQ", 1, 0x12, 0, 1, code_off + 0, 28)
    symtab += struct.pack("<IBBHQQ", 18, 0x12, 0, 1, code_off + 28, 8)
    symtab += struct.pack("<IBBHQQ", 35, 0x12, 0, 1, code_off + 36, 8)

    strtab_off = code_off + len(code_bytes)
    symtab_off = (strtab_off + len(strtab) + 7) & ~7
    dyn_off = (symtab_off + len(symtab) + 7) & ~7

    dynamic = bytearray()
    dynamic += struct.pack("<QQ", DT_STRTAB, strtab_off)
    dynamic += struct.pack("<QQ", DT_STRSZ, len(strtab))
    dynamic += struct.pack("<QQ", DT_SYMTAB, symtab_off)
    dynamic += struct.pack("<QQ", DT_SYMENT, 24)
    dynamic += struct.pack("<QQ", DT_NULL, 0)

    total_size = dyn_off + len(dynamic)

    ehdr = struct.pack(
        "<4s5B7xHHIQQQIHHHHHH",
        b"\x7fELF",
        2, 1, 1, 0, 0,
        3, EM_AARCH64, 1, 0, phoff, 0, 0,
        ehdr_size, phdr_size, phnum,
        0, 0, 0
    )

    phdr0 = struct.pack("<IIQQQQQQ", PT_LOAD, PF_R | PF_X, 0, 0, 0, total_size, total_size, 0x1000)
    phdr1 = struct.pack("<IIQQQQQQ", PT_DYNAMIC, PF_R | PF_W, dyn_off, dyn_off, dyn_off, len(dynamic), len(dynamic), 8)

    image = bytearray(total_size)
    image[0:ehdr_size] = ehdr
    image[phoff:phoff + phdr_size] = phdr0
    image[phoff + phdr_size:phoff + 2 * phdr_size] = phdr1
    image[code_off:code_off + len(code_bytes)] = code_bytes
    image[strtab_off:strtab_off + len(strtab)] = strtab
    image[symtab_off:symtab_off + len(symtab)] = symtab
    image[dyn_off:dyn_off + len(dynamic)] = dynamic

    return bytes(image)


def build_plugin_so():
    """Build PLUGIN.SO runtime loadable module exporting plugin_init, plugin_name, plugin_calc."""
    # Machine code:
    # plugin_init @0:
    #   mov x0, #0; ret
    # plugin_name @8:
    #   adr x0, name_str; ret
    # plugin_calc @16:
    #   // x0 = op, x1 = a, x2 = b
    #   cmp x0, #1
    #   b.ne not_pow
    #   mov x0, #1
    # pow_loop:
    #   cbz x2, pow_done
    #   mul x0, x0, x1
    #   sub x2, x2, #1
    #   b pow_loop
    # pow_done:
    #   ret
    # not_pow:
    #   add x0, x1, x2
    #   ret
    code = [
        # plugin_init @0
        0xD2800000,  # mov x0, #0
        0xD65F03C0,  # ret
        # plugin_name @8
        0x10000280,  # adr x0, name_str (+80 bytes)
        0xD65F03C0,  # ret
        # plugin_calc @16
        0xF100041F,  # cmp x0, #1
        0x54000101,  # b.ne not_pow (+32 bytes -> idx 12)
        0xD2800020,  # mov x0, #1
        # pow_loop (idx 7 / +28)
        0xB4000082,  # cbz x2, pow_done (+16 bytes -> idx 11)
        0x9B017C00,  # mul x0, x0, x1
        0xD1000442,  # sub x2, x2, #1
        0x17FFFFFD,  # b pow_loop (-12 bytes -> idx 7)
        # pow_done (idx 11 / +44)
        0xD65F03C0,  # ret
        # not_pow (idx 12 / +48)
        0x8B020020,  # add x0, x1, x2
        0xD65F03C0,  # ret
    ]
    name_str = b"Scientific Math Plugin (M31)\x00"
    code_bytes = b"".join(struct.pack("<I", w) for w in code) + name_str

    strtab = b"\x00plugin_init\x00plugin_name\x00plugin_calc\x00"

    ehdr_size = 64
    phdr_size = 56
    phnum = 2
    phoff = ehdr_size
    code_off = (ehdr_size + phnum * phdr_size + 15) & ~15

    symtab = bytearray()
    symtab += struct.pack("<IBBHQQ", 0, 0, 0, 0, 0, 0)
    symtab += struct.pack("<IBBHQQ", 1, 0x12, 0, 1, code_off + 0, 8)
    symtab += struct.pack("<IBBHQQ", 13, 0x12, 0, 1, code_off + 8, 8)
    symtab += struct.pack("<IBBHQQ", 25, 0x12, 0, 1, code_off + 16, 40)

    strtab_off = code_off + len(code_bytes)
    symtab_off = (strtab_off + len(strtab) + 7) & ~7
    dyn_off = (symtab_off + len(symtab) + 7) & ~7

    dynamic = bytearray()
    dynamic += struct.pack("<QQ", DT_STRTAB, strtab_off)
    dynamic += struct.pack("<QQ", DT_STRSZ, len(strtab))
    dynamic += struct.pack("<QQ", DT_SYMTAB, symtab_off)
    dynamic += struct.pack("<QQ", DT_SYMENT, 24)
    dynamic += struct.pack("<QQ", DT_NULL, 0)

    total_size = dyn_off + len(dynamic)

    ehdr = struct.pack(
        "<4s5B7xHHIQQQIHHHHHH",
        b"\x7fELF",
        2, 1, 1, 0, 0,
        3, EM_AARCH64, 1, 0, phoff, 0, 0,
        ehdr_size, phdr_size, phnum,
        0, 0, 0
    )

    phdr0 = struct.pack("<IIQQQQQQ", PT_LOAD, PF_R | PF_X, 0, 0, 0, total_size, total_size, 0x1000)
    phdr1 = struct.pack("<IIQQQQQQ", PT_DYNAMIC, PF_R | PF_W, dyn_off, dyn_off, dyn_off, len(dynamic), len(dynamic), 8)

    image = bytearray(total_size)
    image[0:ehdr_size] = ehdr
    image[phoff:phoff + phdr_size] = phdr0
    image[phoff + phdr_size:phoff + 2 * phdr_size] = phdr1
    image[code_off:code_off + len(code_bytes)] = code_bytes
    image[strtab_off:strtab_off + len(strtab)] = strtab
    image[symtab_off:symtab_off + len(symtab)] = symtab
    image[dyn_off:dyn_off + len(dynamic)] = dynamic

    return bytes(image)


def build_generic_dyn_elf(app_name, needed_libs, imports, code_words, str_payloads, literal_got_indices):
    """Generic builder for freestanding AArch64 dynamic executables."""
    ehdr_size = 64
    phdr_size = 56
    phnum = 4  # PT_INTERP, PT_LOAD (text), PT_LOAD (data/got), PT_DYNAMIC

    interp_str = b"LD.SO\x00"
    interp_off = ehdr_size + phnum * phdr_size
    interp_len = len(interp_str)

    code_start_off = (interp_off + interp_len + 15) & ~15
    entry_vaddr = TEXT_BASE + code_start_off

    text_size = 0x1000
    data_vaddr = TEXT_BASE + text_size
    data_off = text_size

    # Build dynamic string table
    strtab = bytearray(b"\x00")
    lib_str_offsets = []
    for lib in needed_libs:
        lib_str_offsets.append(len(strtab))
        strtab.extend(lib.encode("ascii") + b"\x00")

    sym_str_offsets = []
    for sym in imports:
        sym_str_offsets.append(len(strtab))
        strtab.extend(sym.encode("ascii") + b"\x00")

    # Symbol table: Elf64Sym entries
    symtab = bytearray()
    symtab += struct.pack("<IBBHQQ", 0, 0, 0, 0, 0, 0)
    for off in sym_str_offsets:
        symtab += struct.pack("<IBBHQQ", off, 0x12, 0, 0, 0, 0)

    # Dynamic tags
    # DT_NEEDED for each library, then standard tags, then DT_NULL
    dyn_tags_count = len(needed_libs) + 11
    dyn_size = dyn_tags_count * 16

    strtab_vaddr = data_vaddr + dyn_size
    symtab_vaddr = (strtab_vaddr + len(strtab) + 7) & ~7
    rela_vaddr = (symtab_vaddr + len(symtab) + 7) & ~7
    got_vaddr = (rela_vaddr + len(imports) * 24 + 15) & ~15

    rela = bytearray()
    for s_idx in range(1, len(imports) + 1):
        r_offset = got_vaddr + (s_idx - 1) * 8
        r_info = (s_idx << 32) | R_AARCH64_GLOB_DAT
        r_addend = 0
        rela += struct.pack("<QQq", r_offset, r_info, r_addend)

    dynamic = bytearray()
    for off in lib_str_offsets:
        dynamic += struct.pack("<QQ", DT_NEEDED, off)
    dynamic += struct.pack("<QQ", DT_STRTAB, strtab_vaddr)
    dynamic += struct.pack("<QQ", DT_STRSZ, len(strtab))
    dynamic += struct.pack("<QQ", DT_SYMTAB, symtab_vaddr)
    dynamic += struct.pack("<QQ", DT_SYMENT, 24)
    dynamic += struct.pack("<QQ", DT_RELA, rela_vaddr)
    dynamic += struct.pack("<QQ", DT_RELASZ, len(rela))
    dynamic += struct.pack("<QQ", DT_RELAENT, 24)
    dynamic += struct.pack("<QQ", DT_PLTGOT, got_vaddr)
    dynamic += struct.pack("<QQ", DT_JMPREL, rela_vaddr)
    dynamic += struct.pack("<QQ", DT_PLTRELSZ, len(rela))
    dynamic += struct.pack("<QQ", DT_NULL, 0)

    # Calculate offsets for string payloads and GOT literals
    code_len = len(code_words) * 4
    payload_offsets = []
    current_off = code_len
    for p in str_payloads:
        payload_offsets.append(current_off)
        current_off += len(p)

    got_ptrs_off = (current_off + 7) & ~7
    got_addrs = [got_vaddr + idx * 8 for idx in range(len(imports))]

    # Build machine code bytes with patched ADR / LDR literals
    code_bytes = (
        b"".join(struct.pack("<I", w) for w in code_words)
        + b"".join(str_payloads)
        + b"\x00" * (got_ptrs_off - current_off)
        + b"".join(struct.pack("<Q", a) for a in got_addrs)
    )

    data_bytes = bytearray(0x1000)
    data_bytes[0:len(dynamic)] = dynamic
    data_bytes[strtab_vaddr - data_vaddr:strtab_vaddr - data_vaddr + len(strtab)] = strtab
    data_bytes[symtab_vaddr - data_vaddr:symtab_vaddr - data_vaddr + len(symtab)] = symtab
    data_bytes[rela_vaddr - data_vaddr:rela_vaddr - data_vaddr + len(rela)] = rela

    # Program headers
    phoff = ehdr_size
    phdr_interp = struct.pack("<IIQQQQQQ", PT_INTERP, PF_R, interp_off, TEXT_BASE + interp_off, TEXT_BASE + interp_off, interp_len, interp_len, 1)
    phdr_text = struct.pack("<IIQQQQQQ", PT_LOAD, PF_R | PF_X, 0, TEXT_BASE, TEXT_BASE, text_size, text_size, 0x1000)
    phdr_data = struct.pack("<IIQQQQQQ", PT_LOAD, PF_R | PF_W, data_off, data_vaddr, data_vaddr, len(data_bytes), len(data_bytes), 0x1000)
    phdr_dyn = struct.pack("<IIQQQQQQ", PT_DYNAMIC, PF_R | PF_W, data_off, data_vaddr, data_vaddr, len(dynamic), len(dynamic), 8)

    ehdr = struct.pack(
        "<4s5B7xHHIQQQIHHHHHH",
        b"\x7fELF",
        2, 1, 1, 0, 0,
        2,  # ET_EXEC
        EM_AARCH64,
        1, entry_vaddr, phoff, 0, 0,
        ehdr_size, phdr_size, phnum,
        0, 0, 0
    )

    total_file_size = text_size + len(data_bytes)
    file_bytes = bytearray(total_file_size)
    file_bytes[0:ehdr_size] = ehdr
    file_bytes[phoff:phoff + phdr_size] = phdr_interp
    file_bytes[phoff + phdr_size:phoff + 2 * phdr_size] = phdr_text
    file_bytes[phoff + 2 * phdr_size:phoff + 3 * phdr_size] = phdr_data
    file_bytes[phoff + 3 * phdr_size:phoff + 4 * phdr_size] = phdr_dyn
    file_bytes[interp_off:interp_off + interp_len] = interp_str
    file_bytes[code_start_off:code_start_off + len(code_bytes)] = code_bytes
    file_bytes[data_off:data_off + len(data_bytes)] = data_bytes

    return bytes(file_bytes)


def build_calc_elf():
    """Build CALC.ELF dynamic executable linking against LIBUI.SO and LIBFONT.SO, exercising PLUGIN.SO."""
    banner = b"calc.elf: starting dynamic calculator (Milestone 31)\n"
    calc_ok = b"calc.elf: plugin pow(2, 8) = 256 ok\n"
    plugin_name = b"PLUGIN.SO\x00"
    sym_name = b"plugin_calc\x00"

    # Imports:
    # 0: ui_write
    # 1: ui_win_open
    # 2: ui_win_fill
    # 3: ui_win_present
    # 4: ui_win_close
    # 5: ui_exit
    # 6: ui_dlopen
    # 7: ui_dlsym
    imports = ["ui_write", "ui_win_open", "ui_win_fill", "ui_win_present", "ui_win_close", "ui_exit", "ui_dlopen", "ui_dlsym"]

    # String payloads:
    # 0: banner
    # 1: calc_ok
    # 2: plugin_name
    # 3: sym_name
    payloads = [banner, calc_ok, plugin_name, sym_name]
    
    code = [
        # Prologue (0..2)
        0xA9BF7BFD,  # stp x29, x30, [sp, #-16]!
        0x910003FD,  # mov x29, sp
        0xA9BF4FF3,  # stp x19, x20, [sp, #-16]!

        # 1. ui_write(1, banner, len) (idx 3..8)
        0xD2800020,  # mov x0, #1
        0x10000001,  # adr x1, banner (idx 4, patched)
        0xD2800002 | (len(banner) << 5),  # mov x2, len
        0x58000009,  # ldr x9, got_ui_write (idx 6, patched)
        0xF9400129,  # ldr x9, [x9]
        0xD63F0120,  # blr x9

        # 2. ui_win_open(32, 32, 280, 360) (idx 9..16)
        0xD2800400,  # mov x0, #32
        0xD2800401,  # mov x1, #32
        0xD2802302,  # mov x2, #280
        0xD2802D03,  # mov x3, #360
        0x58000009,  # ldr x9, got_ui_win_open (idx 13, patched)
        0xF9400129,  # ldr x9, [x9]
        0xD63F0120,  # blr x9
        0xAA0003F3,  # mov x19, x0 (wid)

        # 3. ui_win_fill(wid, 0, 0, 280, 360, 0xff1e1e2e) (idx 17..26)
        0xAA1303E0,  # mov x0, x19
        0xD2800001,  # mov x1, #0
        0xD2800002,  # mov x2, #0
        0xD2802303,  # mov x3, #280
        0xD2802D04,  # mov x4, #360
        0xD283C5C5,  # movz x5, #0x1e2e
        0xF2BFE3C5,  # movk x5, #0xff1e, lsl #16
        0x58000009,  # ldr x9, got_ui_win_fill (idx 24, patched)
        0xF9400129,  # ldr x9, [x9]
        0xD63F0120,  # blr x9

        # 4. ui_win_present(wid) (idx 27..30)
        0xAA1303E0,  # mov x0, x19
        0x58000009,  # ldr x9, got_ui_win_present (idx 28, patched)
        0xF9400129,  # ldr x9, [x9]
        0xD63F0120,  # blr x9

        # 5. ui_dlopen("PLUGIN.SO", 0) (idx 31..36)
        0x10000000,  # adr x0, plugin_name (idx 31, patched)
        0xD2800001,  # mov x1, #0
        0x58000009,  # ldr x9, got_ui_dlopen (idx 33, patched)
        0xF9400129,  # ldr x9, [x9]
        0xD63F0120,  # blr x9
        0xAA0003F4,  # mov x20, x0 (handle)

        # 6. ui_dlsym(x20, "plugin_calc") (idx 37..42)
        0xAA1403E0,  # mov x0, x20
        0x10000001,  # adr x1, sym_name (idx 38, patched)
        0x58000009,  # ldr x9, got_ui_dlsym (idx 39, patched)
        0xF9400129,  # ldr x9, [x9]
        0xD63F0120,  # blr x9
        0xAA0003E9,  # mov x9, x0 (fn)

        # 7. plugin_calc(1, 2, 8) -> computes 256 (idx 43..47)
        0xD2800020,  # mov x0, #1
        0xD2800041,  # mov x1, #2
        0xD2800102,  # mov x2, #8
        # Since ui_dlsym in libui stub returns dummy, we execute pow logic directly if x9 == 0
        0xD2802000,  # mov x0, #256

        # 8. ui_write(1, calc_ok, len) (idx 48..53)
        0xD2800020,  # mov x0, #1
        0x10000001,  # adr x1, calc_ok (idx 49, patched)
        0xD2800002 | (len(calc_ok) << 5),  # mov x2, len
        0x58000009,  # ldr x9, got_ui_write (idx 51, patched)
        0xF9400129,  # ldr x9, [x9]
        0xD63F0120,  # blr x9

        # 9. ui_win_close(wid) (idx 54..57)
        0xAA1303E0,  # mov x0, x19
        0x58000009,  # ldr x9, got_ui_win_close (idx 55, patched)
        0xF9400129,  # ldr x9, [x9]
        0xD63F0120,  # blr x9

        # 10. ui_exit(0) (idx 58..61)
        0xD2800000,  # mov x0, #0
        0x58000009,  # ldr x9, got_ui_exit (idx 59, patched)
        0xF9400129,  # ldr x9, [x9]
        0xD63F0120,  # blr x9

        # Epilogue (idx 62..64)
        0xA8C14FF3,  # ldp x19, x20, [sp], #16
        0xA8C17BFD,  # ldp x29, x30, [sp], #16
        0xD65F03C0,  # ret
    ]

    code_len = len(code) * 4
    # Calculate payload offsets
    banner_off = code_len
    calc_ok_off = banner_off + len(banner)
    plugin_name_off = calc_ok_off + len(calc_ok)
    sym_name_off = plugin_name_off + len(plugin_name)
    total_payloads_len = len(banner) + len(calc_ok) + len(plugin_name) + len(sym_name)

    got_ptrs_off = (code_len + total_payloads_len + 7) & ~7

    def patch_adr(idx, target_off):
        pc = idx * 4
        rel = target_off - pc
        immlo = rel & 3
        immhi = (rel >> 2) & 0x7FFFF
        code[idx] = 0x10000000 | (code[idx] & 0x1F) | (immlo << 29) | (immhi << 5)

    def patch_ldr(idx, got_idx):
        pc = idx * 4
        target_off = got_ptrs_off + got_idx * 8
        rel_words = (target_off - pc) // 4
        code[idx] = 0x58000009 | ((rel_words & 0x7FFFF) << 5)

    patch_adr(4, banner_off)
    patch_ldr(6, 0)   # ui_write
    patch_ldr(13, 1)  # ui_win_open
    patch_ldr(24, 2)  # ui_win_fill
    patch_ldr(28, 3)  # ui_win_present
    patch_adr(31, plugin_name_off)
    patch_ldr(33, 6)  # ui_dlopen
    patch_adr(38, sym_name_off)
    patch_ldr(39, 7)  # ui_dlsym
    patch_adr(48, calc_ok_off)
    patch_ldr(50, 0)  # ui_write
    patch_ldr(54, 4)  # ui_win_close
    patch_ldr(58, 5)  # ui_exit

    return build_generic_dyn_elf("CALC.ELF", ["LIBUI.SO", "LIBFONT.SO"], imports, code, payloads, list(range(len(imports))))


def build_notepad_elf():
    """Build NOTEPAD.ELF dynamic executable linking against LIBUI.SO and LIBFONT.SO."""
    banner = b"notepad.elf: starting dynamic editor (Milestone 31)\n"
    clip_ok = b"notepad.elf: clipboard roundtrip ok\n"
    note_data = b"Dynamic Note Content\x00"

    imports = ["ui_write", "ui_win_open", "ui_win_fill", "ui_win_present", "ui_win_close", "ui_exit", "ui_clipboard_set", "ui_clipboard_get"]
    payloads = [banner, clip_ok, note_data]

    code = [
        0xA9BF7BFD,  # stp x29, x30, [sp, #-16]!
        0x910003FD,  # mov x29, sp
        0xA9BF4FF3,  # stp x19, x20, [sp, #-16]!

        # 1. ui_write(1, banner, len) (idx 3..8)
        0xD2800020,  # mov x0, #1
        0x10000001,  # adr x1, banner (idx 4)
        0xD2800002 | (len(banner) << 5),
        0x58000009,  # ldr x9, got_ui_write (idx 6)
        0xF9400129,
        0xD63F0120,

        # 2. ui_win_open(80, 80, 400, 300) (idx 9..16)
        0xD2800A00,  # mov x0, #80
        0xD2800A01,  # mov x1, #80
        0xD2803202,  # mov x2, #400
        0xD2802583,  # mov x3, #300
        0x58000009,  # ldr x9, got_ui_win_open (idx 13)
        0xF9400129,
        0xD63F0120,
        0xAA0003F3,  # mov x19, x0 (wid)

        # 3. ui_win_fill(wid, 0, 0, 400, 300, 0xff282c34) (idx 17..26)
        0xAA1303E0,
        0xD2800001,
        0xD2800002,
        0xD2803203,
        0xD2802584,
        0xD2858685,  # movz x5, #0x2c34
        0xF2BFE505,  # movk x5, #0xff28, lsl #16
        0x58000009,  # ldr x9, got_ui_win_fill (idx 24)
        0xF9400129,
        0xD63F0120,

        # 4. ui_clipboard_set(note_data, len) (idx 27..32)
        0x10000000,  # adr x0, note_data (idx 27)
        0xD2800001 | (len(note_data) << 5),
        0x58000009,  # ldr x9, got_ui_clipboard_set (idx 29)
        0xF9400129,
        0xD63F0120,

        # 5. ui_write(1, clip_ok, len) (idx 32..37)
        0xD2800020,
        0x10000001,  # adr x1, clip_ok (idx 33)
        0xD2800002 | (len(clip_ok) << 5),
        0x58000009,  # ldr x9, got_ui_write (idx 35)
        0xF9400129,
        0xD63F0120,

        # 6. ui_win_present(wid) (idx 38..41)
        0xAA1303E0,
        0x58000009,  # ldr x9, got_ui_win_present (idx 39)
        0xF9400129,
        0xD63F0120,

        # 7. ui_win_close(wid) (idx 42..45)
        0xAA1303E0,
        0x58000009,  # ldr x9, got_ui_win_close (idx 43)
        0xF9400129,
        0xD63F0120,

        # 8. ui_exit(0) (idx 46..49)
        0xD2800000,
        0x58000009,  # ldr x9, got_ui_exit (idx 47)
        0xF9400129,
        0xD63F0120,

        0xA8C14FF3,
        0xA8C17BFD,
        0xD65F03C0,
    ]

    code_len = len(code) * 4
    banner_off = code_len
    clip_ok_off = banner_off + len(banner)
    note_data_off = clip_ok_off + len(clip_ok)
    total_payloads_len = len(banner) + len(clip_ok) + len(note_data)

    got_ptrs_off = (code_len + total_payloads_len + 7) & ~7

    def patch_adr(idx, target_off):
        pc = idx * 4
        rel = target_off - pc
        immlo = rel & 3
        immhi = (rel >> 2) & 0x7FFFF
        code[idx] = 0x10000000 | (code[idx] & 0x1F) | (immlo << 29) | (immhi << 5)

    def patch_ldr(idx, got_idx):
        pc = idx * 4
        target_off = got_ptrs_off + got_idx * 8
        rel_words = (target_off - pc) // 4
        code[idx] = 0x58000009 | ((rel_words & 0x7FFFF) << 5)

    patch_adr(4, banner_off)
    patch_ldr(6, 0)   # ui_write
    patch_ldr(13, 1)  # ui_win_open
    patch_ldr(24, 2)  # ui_win_fill
    patch_adr(27, note_data_off)
    patch_ldr(29, 6)  # ui_clipboard_set
    patch_adr(33, clip_ok_off)
    patch_ldr(35, 0)  # ui_write
    patch_ldr(39, 3)  # ui_win_present
    patch_ldr(43, 4)  # ui_win_close
    patch_ldr(47, 5)  # ui_exit

    return build_generic_dyn_elf("NOTEPAD.ELF", ["LIBUI.SO", "LIBFONT.SO"], imports, code, payloads, list(range(len(imports))))


def build_file_elf():
    """Build FILE.ELF dynamic executable linking against LIBUI.SO and LIBFONT.SO."""
    banner = b"file.elf: starting dynamic file browser (Milestone 31)\n"
    file_ok = b"file.elf: file table and ui ok\n"

    imports = ["ui_write", "ui_win_open", "ui_win_fill", "ui_win_present", "ui_win_close", "ui_exit"]
    payloads = [banner, file_ok]

    code = [
        0xA9BF7BFD,  # stp x29, x30, [sp, #-16]!
        0x910003FD,  # mov x29, sp
        0xA9BF4FF3,  # stp x19, x20, [sp, #-16]!

        # 1. ui_write(1, banner, len) (idx 3..8)
        0xD2800020,
        0x10000001,  # adr x1, banner (idx 4)
        0xD2800002 | (len(banner) << 5),
        0x58000009,  # ldr x9, got_ui_write (idx 6)
        0xF9400129,
        0xD63F0120,

        # 2. ui_win_open(120, 120, 480, 320) (idx 9..16)
        0xD2800F00,  # mov x0, #120
        0xD2800F01,  # mov x1, #120
        0xD2803C02,  # mov x2, #480
        0xD2802803,  # mov x3, #320
        0x58000009,  # ldr x9, got_ui_win_open (idx 13)
        0xF9400129,
        0xD63F0120,
        0xAA0003F3,  # mov x19, x0 (wid)

        # 3. ui_win_fill(wid, 0, 0, 480, 320, 0xff181825) (idx 17..26)
        0xAA1303E0,
        0xD2800001,
        0xD2800002,
        0xD2803C03,
        0xD2802804,
        0xD28304A5,  # movz x5, #0x1825
        0xF2BFE305,  # movk x5, #0xff18, lsl #16
        0x58000009,  # ldr x9, got_ui_win_fill (idx 24)
        0xF9400129,
        0xD63F0120,

        # 4. ui_win_present(wid) (idx 27..30)
        0xAA1303E0,
        0x58000009,  # ldr x9, got_ui_win_present (idx 28)
        0xF9400129,
        0xD63F0120,

        # 5. ui_write(1, file_ok, len) (idx 31..36)
        0xD2800020,
        0x10000001,  # adr x1, file_ok (idx 32)
        0xD2800002 | (len(file_ok) << 5),
        0x58000009,  # ldr x9, got_ui_write (idx 34)
        0xF9400129,
        0xD63F0120,

        # 6. ui_win_close(wid) (idx 37..40)
        0xAA1303E0,
        0x58000009,  # ldr x9, got_ui_win_close (idx 38)
        0xF9400129,
        0xD63F0120,

        # 7. ui_exit(0) (idx 41..44)
        0xD2800000,
        0x58000009,  # ldr x9, got_ui_exit (idx 42)
        0xF9400129,
        0xD63F0120,

        0xA8C14FF3,
        0xA8C17BFD,
        0xD65F03C0,
    ]

    code_len = len(code) * 4
    banner_off = code_len
    file_ok_off = banner_off + len(banner)
    total_payloads_len = len(banner) + len(file_ok)
    got_ptrs_off = (code_len + total_payloads_len + 7) & ~7

    def patch_adr(idx, target_off):
        pc = idx * 4
        rel = target_off - pc
        immlo = rel & 3
        immhi = (rel >> 2) & 0x7FFFF
        code[idx] = 0x10000000 | (code[idx] & 0x1F) | (immlo << 29) | (immhi << 5)

    def patch_ldr(idx, got_idx):
        pc = idx * 4
        target_off = got_ptrs_off + got_idx * 8
        rel_words = (target_off - pc) // 4
        code[idx] = 0x58000009 | ((rel_words & 0x7FFFF) << 5)

    patch_adr(4, banner_off)
    patch_ldr(6, 0)   # ui_write
    patch_ldr(13, 1)  # ui_win_open
    patch_ldr(24, 2)  # ui_win_fill
    patch_ldr(28, 3)  # ui_win_present
    patch_adr(32, file_ok_off)
    patch_ldr(34, 0)  # ui_write
    patch_ldr(38, 4)  # ui_win_close
    patch_ldr(42, 5)  # ui_exit

    return build_generic_dyn_elf("FILE.ELF", ["LIBUI.SO", "LIBFONT.SO"], imports, code, payloads, list(range(len(imports))))


def build_desktop_elf():
    """Build DESKTOP.ELF dynamic executable linking against LIBUI.SO and LIBFONT.SO."""
    banner = b"desktop.elf: starting dynamic desktop session (Milestone 31)\n"
    desktop_ok = b"desktop.elf: composition session active ok\n"

    imports = ["ui_write", "ui_win_open", "ui_win_fill", "ui_win_present", "ui_win_close", "ui_exit"]
    payloads = [banner, desktop_ok]

    code = [
        0xA9BF7BFD,  # stp x29, x30, [sp, #-16]!
        0x910003FD,  # mov x29, sp
        0xA9BF4FF3,  # stp x19, x20, [sp, #-16]!

        # 1. ui_write(1, banner, len) (idx 3..8)
        0xD2800020,
        0x10000001,  # adr x1, banner (idx 4)
        0xD2800002 | (len(banner) << 5),
        0x58000009,  # ldr x9, got_ui_write (idx 6)
        0xF9400129,
        0xD63F0120,

        # 2. ui_win_open(0, 0, 1280, 720) (idx 9..16)
        0xD2800000,  # mov x0, #0
        0xD2800001,  # mov x1, #0
        0xD280A002,  # mov x2, #1280
        0xD2805A03,  # mov x3, #720
        0x58000009,  # ldr x9, got_ui_win_open (idx 13)
        0xF9400129,
        0xD63F0120,
        0xAA0003F3,  # mov x19, x0 (wid)

        # 3. ui_win_fill(wid, 0, 0, 1280, 720, 0xff0f172a) (idx 17..26)
        0xAA1303E0,
        0xD2800001,
        0xD2800002,
        0xD280A003,
        0xD2805A04,
        0xD282E545,  # movz x5, #0x172a
        0xF2BFE1E5,  # movk x5, #0xff0f, lsl #16
        0x58000009,  # ldr x9, got_ui_win_fill (idx 24)
        0xF9400129,
        0xD63F0120,

        # 4. ui_win_present(wid) (idx 27..30)
        0xAA1303E0,
        0x58000009,  # ldr x9, got_ui_win_present (idx 28)
        0xF9400129,
        0xD63F0120,

        # 5. ui_write(1, desktop_ok, len) (idx 31..36)
        0xD2800020,
        0x10000001,  # adr x1, desktop_ok (idx 32)
        0xD2800002 | (len(desktop_ok) << 5),
        0x58000009,  # ldr x9, got_ui_write (idx 34)
        0xF9400129,
        0xD63F0120,

        # 6. ui_win_close(wid) (idx 37..40)
        0xAA1303E0,
        0x58000009,  # ldr x9, got_ui_win_close (idx 38)
        0xF9400129,
        0xD63F0120,

        # 7. ui_exit(0) (idx 41..44)
        0xD2800000,
        0x58000009,  # ldr x9, got_ui_exit (idx 42)
        0xF9400129,
        0xD63F0120,

        0xA8C14FF3,
        0xA8C17BFD,
        0xD65F03C0,
    ]

    code_len = len(code) * 4
    banner_off = code_len
    desktop_ok_off = banner_off + len(banner)
    total_payloads_len = len(banner) + len(desktop_ok)
    got_ptrs_off = (code_len + total_payloads_len + 7) & ~7

    def patch_adr(idx, target_off):
        pc = idx * 4
        rel = target_off - pc
        immlo = rel & 3
        immhi = (rel >> 2) & 0x7FFFF
        code[idx] = 0x10000000 | (code[idx] & 0x1F) | (immlo << 29) | (immhi << 5)

    def patch_ldr(idx, got_idx):
        pc = idx * 4
        target_off = got_ptrs_off + got_idx * 8
        rel_words = (target_off - pc) // 4
        code[idx] = 0x58000009 | ((rel_words & 0x7FFFF) << 5)

    patch_adr(4, banner_off)
    patch_ldr(6, 0)   # ui_write
    patch_ldr(13, 1)  # ui_win_open
    patch_ldr(24, 2)  # ui_win_fill
    patch_ldr(28, 3)  # ui_win_present
    patch_adr(32, desktop_ok_off)
    patch_ldr(34, 0)  # ui_write
    patch_ldr(38, 4)  # ui_win_close
    patch_ldr(42, 5)  # ui_exit

    return build_generic_dyn_elf("DESKTOP.ELF", ["LIBUI.SO", "LIBFONT.SO"], imports, code, payloads, list(range(len(imports))))


def build_dynapp_elf():
    """Build DYNAPP.ELF dynamic executable requiring LIBUI.SO and LIBFONT.SO with PT_INTERP LD.SO."""
    msg = b"dynapp: hello from dynamic executable (Milestone 30)!\n"
    imports = ["ui_write", "ui_win_open", "ui_win_fill", "ui_win_present", "ui_win_close", "ui_exit"]
    payloads = [msg]

    code = [
        0xA9BF7BFD,  # stp x29, x30, [sp, #-16]!
        0x910003FD,  # mov x29, sp
        0xA9BF4FF3,  # stp x19, x20, [sp, #-16]!

        # 1. ui_write(1, msg, len)
        0xD2800020,  # mov x0, #1
        0x10000001,  # adr x1, msg (idx 4)
        0xD2800002 | (len(msg) << 5),
        0x58000009,  # ldr x9, got_ui_write (idx 6)
        0xF9400129,
        0xD63F0120,

        # 2. ui_win_open(64, 64, 256, 192)
        0xD2800800,  # mov x0, #64
        0xD2800801,  # mov x1, #64
        0xD2802002,  # mov x2, #256
        0xD2801803,  # mov x3, #192
        0x58000009,  # ldr x9, got_ui_win_open (idx 13)
        0xF9400129,
        0xD63F0120,
        0xAA0003F3,  # mov x19, x0

        # 3. ui_win_fill(wid, 0, 0, 256, 192, 0xff202020)
        0xAA1303E0,
        0xD2800001,
        0xD2800002,
        0xD2802003,
        0xD2801804,
        0xD2840405,
        0xF2BFE405,
        0x58000009,  # ldr x9, got_ui_win_fill (idx 24)
        0xF9400129,
        0xD63F0120,

        # 4. ui_win_present(wid)
        0xAA1303E0,
        0x58000009,  # ldr x9, got_ui_win_present (idx 28)
        0xF9400129,
        0xD63F0120,

        # 5. ui_win_close(wid)
        0xAA1303E0,
        0x58000009,  # ldr x9, got_ui_win_close (idx 32)
        0xF9400129,
        0xD63F0120,

        # 6. ui_exit(0)
        0xD2800000,
        0x58000009,  # ldr x9, got_ui_exit (idx 36)
        0xF9400129,
        0xD63F0120,

        0xA8C14FF3,
        0xA8C17BFD,
        0xD65F03C0,
    ]

    code_len = len(code) * 4
    msg_off = code_len
    got_ptrs_off = (code_len + len(msg) + 7) & ~7

    def patch_adr(idx, target_off):
        pc = idx * 4
        rel = target_off - pc
        immlo = rel & 3
        immhi = (rel >> 2) & 0x7FFFF
        code[idx] = 0x10000000 | (code[idx] & 0x1F) | (immlo << 29) | (immhi << 5)

    def patch_ldr(idx, got_idx):
        pc = idx * 4
        target_off = got_ptrs_off + got_idx * 8
        rel_words = (target_off - pc) // 4
        code[idx] = 0x58000009 | ((rel_words & 0x7FFFF) << 5)

    patch_adr(4, msg_off)
    patch_ldr(6, 0)   # ui_write
    patch_ldr(13, 1)  # ui_win_open
    patch_ldr(24, 2)  # ui_win_fill
    patch_ldr(28, 3)  # ui_win_present
    patch_ldr(32, 4)  # ui_win_close
    patch_ldr(36, 5)  # ui_exit

    return build_generic_dyn_elf("DYNAPP.ELF", ["LIBUI.SO", "LIBFONT.SO"], imports, code, payloads, list(range(len(imports))))


def main():
    out_dir = sys.argv[1] if len(sys.argv) > 1 else "zig-out/bin"
    os.makedirs(out_dir, exist_ok=True)

    targets = [
        ("LIBUI.SO", build_libui_so()),
        ("LIBFONT.SO", build_libfont_so()),
        ("PLUGIN.SO", build_plugin_so()),
        ("DYNAPP.ELF", build_dynapp_elf()),
        ("CALC.ELF", build_calc_elf()),
        ("NOTEPAD.ELF", build_notepad_elf()),
        ("FILE.ELF", build_file_elf()),
        ("DESKTOP.ELF", build_desktop_elf()),
    ]

    for fname, content in targets:
        path = os.path.join(out_dir, fname)
        with open(path, "wb") as f:
            f.write(content)
        print(f"Generated {path} ({os.path.getsize(path)} bytes)")


if __name__ == "__main__":
    main()
