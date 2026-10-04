"""Pinned-preprocessor safety patches; every compiled C loop/entry is covered."""
import json
import re
import subprocess

TOKENS = re.compile(r'"(?:\\.|[^"\\])*"|\'(?:\\.|[^\'\\])*\'|[A-Za-z_]\w*|\d+(?:\.\d*)?|\S')


def instrument_text(text):
    tokens = list(TOKENS.finditer(text))
    values = [token.group() for token in tokens]
    pairs, stack = {}, []
    for i, value in enumerate(values):
        if value in ("(", "[", "{"):
            stack.append(i)
        elif value in (")", "]", "}"):
            if not stack or values[stack[-1]] != {")": "(", "]": "[", "}": "{"}[value]:
                raise ValueError("SafetyUnbalancedC")
            left = stack.pop()
            pairs[left] = i
    if stack:
        raise ValueError("SafetyUnbalancedC")
    inserts, entries, loops, terminal_whiles = {}, [], [], set()

    def insert(position, content):
        inserts.setdefault(position, []).append(content)

    def statement(i):
        value = values[i]
        if value == "{":
            return pairs[i] + 1
        if value in ("for", "while", "switch", "if"):
            if values[i + 1] != "(":
                raise ValueError("SafetyStatementCondition")
            end = statement(pairs[i + 1] + 1)
            if value == "if" and end < len(values) and values[end] == "else":
                return statement(end + 1)
            return end
        if value == "do":
            end = statement(i + 1)
            if values[end] != "while":
                raise ValueError("SafetyDoWhile")
            terminal_whiles.add(end)
            end = pairs[end + 1] + 1
            if values[end] != ";":
                raise ValueError("SafetyDoWhileEnd")
            return end + 1
        if i + 1 < len(values) and values[i + 1] == ":":
            return statement(i + 2)
        while i < len(values):
            if values[i] == ";":
                return i + 1
            i = pairs[i] + 1 if i in pairs else i + 1
        raise ValueError("SafetyUnterminatedStatement")

    i, declaration = 0, 0
    while i < len(values):
        if values[i] == "{":
            close = pairs[i]
            if i and values[i - 1] == ")" and "=" not in values[declaration:i] and "typedef" not in values[declaration:i]:
                candidates = []
                j = declaration
                while j < i:
                    if values[j] == "(":
                        if j and values[j - 1] not in ("__attribute__", "__declspec", "typeof", "__typeof__"):
                            candidates.append(values[j - 1])
                        j = pairs[j] + 1
                    else:
                        j += 1
                if not candidates:
                    raise ValueError("SafetyMissingFunctionName")
                name = candidates[-1]
                insert(tokens[i].end(), " qjs_native_step(); ")
                entries.append({"function": name, "offset": tokens[i].start()})
                for k in range(i + 1, close):
                    kind = values[k]
                    if kind not in ("for", "while", "do") or k in terminal_whiles:
                        continue
                    if kind == "do":
                        start = k + 1
                        statement(k)  # Records the non-loop trailing while.
                    else:
                        if values[k + 1] != "(":
                            raise ValueError("SafetyMissingLoopCondition")
                        start = pairs[k + 1] + 1
                    end = statement(start)
                    if values[start] == "{":
                        insert(tokens[start].end(), " qjs_native_step(); ")
                    else:
                        insert(tokens[start].start(), "{ qjs_native_step(); ")
                        insert(tokens[end - 1].end(), " }")
                    loops.append({"function": name, "kind": kind, "offset": tokens[start].start()})
            i = close + 1
            declaration = i
        else:
            if values[i] == ";":
                declaration = i + 1
            i += 1
    if not entries or not loops:
        raise ValueError("MissingSafetyCoverage")
    for offset in sorted(inserts, reverse=True):
        text = text[:offset] + "".join(inserts[offset]) + text[offset:]
    return text, {"entries": entries, "loops": loops}


def instrument(compiler, source, flags, environment, log):
    # Zig's embedded clang supports -E, but not AST-dump actions. Use its
    # pinned preprocessing, not an unpinned system clang, then parse the small
    # C statement grammar. Ordinary compilation independently validates the
    # transformed token stream. Receipts enumerate every entry and loop.
    result = subprocess.run([str(compiler / "zig"), "cc", *flags, "-E", "-P", str(source),
                             "-o", str(log.with_suffix(".preprocess-output"))],
                            text=True, capture_output=True, env=environment)
    log.write_text(result.stderr)
    if result.returncode:
        raise ValueError("SafetyPreprocessFailed: " + result.stderr)
    original = result.stdout or log.with_suffix(".preprocess-output").read_text()
    text, receipt = instrument_text(original)
    source.with_suffix(source.suffix + ".preprocessed").write_text(original)
    source.write_text("extern void qjs_native_step(void);\n" + text)
    receipt.update(unit=source.name, policy="Check every compiled function entry and every loop iteration; C-boundary refusal propagates without traversing poisoned engine state.")
    source.with_suffix(source.suffix + ".safety.json").write_text(json.dumps(receipt, sort_keys=True, indent=2) + "\n")
    return receipt
