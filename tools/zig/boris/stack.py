"""C1's whole-image entry-guard strategy, with C3-owned symbols.

Every compiler-emitted body is checked before it can allocate a frame.
Entry SP - 120 KiB is the floor, and every frame must be <= 8 KiB.
Thus input recursion refuses BEFORE exceeding ADR 0038's 128 KiB limit.
No uninstrumented compiler-rt is permitted in the final object-only link.
"""
import re


def frames(text):
    """SDK frame accounting without discarding the evidence on budget failure."""
    result, name = {}, None
    for raw in text.splitlines():
        line = raw.strip()
        match = re.fullmatch(r"\.type\s+(.+),@function", line)
        if match:
            name = match[1]
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
        elif re.match(r"\w+\s+sp,", line) and not re.fullmatch(r"add\s+sp, sp, #\d+(?:, lsl #\d+)?", line):
            raise ValueError(f"UnreviewedStackAdjustment: {name}: {line}")
    if not result:
        raise ValueError("StackBudget: missing emitted function frames")
    return result


def guard(text, frames):
    if max(frames.values()) > 8192:
        raise ValueError("StackBudget: a compiler frame exceeds the reserved 8 KiB")
    aliases = dict(re.findall(r"(?m)^(\S+) = (\S+)$", text))
    if "_start" not in aliases or "boris_stack_refused" not in aliases:
        raise ValueError("StackBudget: entry/refusal aliases missing")
    exempt = {aliases["_start"], aliases["boris_stack_refused"]}
    functions = [n for n in re.findall(r"(?m)^\s*\.type\s+(.+),@function$", text) if n not in aliases]
    if set(functions) != set(frames):
        raise ValueError("StackBudget: frame/body inventory mismatch")
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
                           "\tmov x17, sp", "\tcmp x17, x16",
                           f"\tb.hs .Lc3_stack_ok_{i}", "\tb boris_stack_refused",
                           f".Lc3_stack_ok_{i}:"]
            guarded.append(pending)
            pending = None
    if set(guarded) != set(functions) or not exempt <= set(guarded):
        raise ValueError("StackBudget: unknown assembly label format")
    return "\n".join(output) + "\n"
