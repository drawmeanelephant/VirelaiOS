"""Whole-image prospective-frame guards, including emitted compiler-rt.

At each function entry require SP - its entire frame >= entry SP - 120 KiB.
The check allocates no stack. Unlike a current-SP check, it also protects
large frames without a per-frame reserve. The 128 KiB ADR budget is unchanged.
"""
import re
from collections import deque

FLOOR_BYTES = 120 * 1024
STACK_BUDGET = 128 * 1024


def frames(text):
    """SDK frame accounting without discarding the evidence on budget failure."""
    result, name = {}, None
    aliases = dict(re.findall(r"(?m)^(\S+) = (\S+)$", text))
    for raw in text.splitlines():
        line = raw.strip()
        match = re.fullmatch(r"\.type\s+(.+),@function", line)
        if match:
            name = match[1] if match[1] not in aliases else None
            if name is not None:
                result[name] = 0
        if line.startswith(".size"):
            name = None
        if name is None:
            continue
        match = re.fullmatch(r"sub\s+sp, sp, #(\d+)(?:, lsl #(\d+))?", line)
        if match:
            result[name] += int(match[1]) << int(match[2] or 0)
        elif re.search(r"\[sp, #-(\d+)\]!", line):
            result[name] += int(re.search(r"\[sp, #-(\d+)\]!", line)[1])
        elif re.search(r"\[sp[^\]]*\]!", line) or re.search(r"\[sp\],", line):
            if not re.search(r"\[sp\], #\d+$", line):
                raise ValueError(f"UnreviewedStackAdjustment: {name}: {line}")
        elif re.search(r"\[sp, #-", line):
            raise ValueError(f"UnreviewedStackAdjustment: below-SP access: {name}: {line}")
        elif re.match(r"\w+\s+sp,", line) and not re.fullmatch(r"add\s+sp, sp, #\d+(?:, lsl #\d+)?", line):
            raise ValueError(f"UnreviewedStackAdjustment: {name}: {line}")
    if not result:
        raise ValueError("StackBudget: missing emitted function frames")
    return result


def proof(text, sizes):
    """Fail closed on repeated frame allocation or unguarded call targets.

    Frame sums overestimate shrink-wrapped paths. A backward CFG edge may
    not cross a decrement. Switch-table targets are included in that check.
    There are no dynamic SP writes (frames() rejects them), so the sum bounds
    each activation, not only its straight-line prologue.
    """
    aliases = dict(re.findall(r"(?m)^(\S+) = (\S+)$", text))
    exempt = {aliases.get("_start"), aliases.get("boris_stack_refused")}
    bodies = {}
    name = None
    for raw in text.splitlines():
        line = raw.strip()
        match = re.fullmatch(r"\.type\s+(.+),@function", line)
        if match:
            name = match[1] if match[1] not in aliases else None
            if name is not None:
                bodies[name] = []
        if line.startswith(".size"):
            name = None
        if name is not None:
            bodies[name].append(line)
    if set(bodies) != set(sizes) or None in exempt or not exempt <= set(bodies):
        raise ValueError("StackBudget: incomplete function inventory")
    # Global jump-table labels resolve only within their containing body.
    switch_targets = set(re.findall(r"(?m)^\s*\.word\s+(\.LBB\S+?)(?:-|\s|$)", text))
    direct, indirect, call_floor = {}, 0, FLOOR_BYTES
    for name, lines in bodies.items():
        if name in exempt:
            if sizes[name] or any(re.match(r"(bl|blr)\s", s) for s in lines):
                raise ValueError("StackBudget: nonterminal or allocating guard exemption")
            direct[name] = []
            continue
        labels = {line[:-1]: i for i, line in enumerate(lines) if line.endswith(":")}
        decrements = [i for i, line in enumerate(lines) if
                      re.match(r"sub\s+sp, sp,", line) or re.search(r"\[sp, #-\d+\]!", line)]
        edges = []
        calls = []
        successors = [[i + 1] if i + 1 < len(lines) else [] for i in range(len(lines))]
        call_sites = []
        for i, line in enumerate(lines):
            match = re.match(r"(bl|b|b\.\w+|cbnz|cbz|tbnz|tbz)\s+(.+)", line)
            if match:
                target = (match[2] if match[1] in ("bl", "b") else match[2].rsplit(",", 1)[-1]).strip()
                target = aliases.get(target, target)
                if target in sizes:
                    if match[1] in ("bl", "b"):
                        calls.append(target)
                    else:
                        raise ValueError("StackBudget: conditional inter-function branch")
                    if match[1] == "b":
                        successors[i] = []
                elif target in labels:
                    edges.append((i, labels[target]))
                    successors[i] = ([labels[target]] if match[1] == "b" else
                                     successors[i] + [labels[target]])
                else:
                    raise ValueError(f"StackBudget: unknown branch target: {name}: {line}")
            if re.match(r"blr\s", line):
                indirect += 1
                calls.append("<guarded-image-indirect>")
            if re.match(r"br\s", line):
                targets = [labels[target] for target in switch_targets if target in labels]
                edges.extend((i, target) for target in targets)
                successors[i] = targets
            if re.match(r"ret(?:\s|$)", line):
                successors[i] = []
            if re.match(r"(bl|blr)\s", line):
                call_sites.append(i)
            if re.match(r"(bl|blr)\s", line) and sizes[name] == 0:
                raise ValueError(f"StackBudget: zero-frame non-tail call: {name}")
        for source, target in edges:
            if target <= source and any(target <= i <= source for i in decrements):
                raise ValueError(f"StackBudget: repeated frame allocation: {name}")
        # Verify every reachable non-tail call actually holds a frame,
        # including shrink-wrapped branches, rather than assuming the maximum
        # frame is allocated on every path.
        usage = [None] * len(lines)
        usage[0] = 0
        queue = deque([0])
        while queue:
            i = queue.popleft()
            line = lines[i]
            delta = 0
            match = re.fullmatch(r"(sub|add)\s+sp, sp, #(\d+)(?:, lsl #(\d+))?", line)
            if match:
                delta = (int(match[2]) << int(match[3] or 0)) * (1 if match[1] == "sub" else -1)
            match = re.search(r"\[sp, #-(\d+)\]!", line)
            if match:
                delta = int(match[1])
            match = re.search(r"\[sp\], #(\d+)", line)
            if match:
                delta = -int(match[1])
            after = usage[i] + delta
            if not 0 <= after <= sizes[name]:
                raise ValueError(f"StackBudget: unbalanced frame control flow: {name}: {line}")
            for target in successors[i]:
                if usage[target] is None or after < usage[target]:
                    usage[target] = after
                    queue.append(target)
        for i in call_sites:
            if usage[i] is not None:
                if usage[i] < 16:
                    raise ValueError(f"StackBudget: non-tail call without a live frame: {name}")
                call_floor = min(call_floor, usage[i])
        direct[name] = calls
    minimum = min(min(n for n in sizes.values() if n), call_floor)
    # Every non-tail call saves LR in a >=minimum frame. Zero-frame entries
    # make no non-tail calls. Thus even recursion/indirect calls have a finite
    # active non-tail depth. Tail calls do not grow that depth.
    return {
        "guarded_stack_bytes": FLOOR_BYTES,
        "stack_budget_bytes": STACK_BUDGET,
        "minimum_nonzero_frame_bytes": minimum,
        "minimum_live_non_tail_call_bytes": call_floor,
        "maximum_frame_bytes": max(sizes.values()),
        "maximum_active_non_tail_calls": FLOOR_BYTES // minimum,
        "maximum_active_frames_including_leaf": FLOOR_BYTES // minimum + 1,
        "functions": len(sizes),
        "indirect_calls": indirect,
        "calls": direct,
        "recursive_calls": "prospective-frame check before every descent",
        "frame_control_flow": "no backward edge crosses a stack decrement",
    }


def guard(text, frames):
    if max(frames.values()) > FLOOR_BYTES:
        raise ValueError("StackBudget: a compiler frame exceeds the guarded stack")
    aliases = dict(re.findall(r"(?m)^(\S+) = (\S+)$", text))
    if "_start" not in aliases or "boris_stack_refused" not in aliases:
        raise ValueError("StackBudget: entry/refusal aliases missing")
    exempt = {aliases["_start"], aliases["boris_stack_refused"]}
    functions = [n for n in re.findall(r"(?m)^\s*\.type\s+(.+),@function$", text) if n not in aliases]
    if set(functions) != set(frames):
        raise ValueError("StackBudget: frame/body inventory mismatch")
    proof(text, frames)
    output, guarded = [], []
    pending = None
    for line in text.splitlines():
        match = re.fullmatch(r"\s*\.type\s+(.+),@function", line)
        if match:
            pending = match[1] if match[1] not in aliases else None
        output.append(line)
        if pending is not None and line.startswith(pending + ":"):
            if pending not in exempt:
                i = len(guarded)
                output += ["\tadrp x16, boris_stack_floor",
                           "\tldr x16, [x16, :lo12:boris_stack_floor]",
                           "\tmov x17, sp"]
                size = frames[pending]
                if size // 4096:
                    output.append(f"\tsub x17, x17, #{size // 4096}, lsl #12")
                if size % 4096:
                    output.append(f"\tsub x17, x17, #{size % 4096}")
                output += ["\tcmp x17, x16",
                           f"\tb.hs .Lc3_stack_ok_{i}", "\tb boris_stack_refused",
                           f".Lc3_stack_ok_{i}:"]
            guarded.append(pending)
            pending = None
    if set(guarded) != set(functions) or not exempt <= set(guarded):
        raise ValueError("StackBudget: unknown assembly label format")
    return "\n".join(output) + "\n"
