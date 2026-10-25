//! VirelaiOS userland shell operators (M45 card SH4 — ADR 0021 D6, issue
//! #1080).
//!
//! The pure parsing behind `|`, `>`, `>>`, `<`, and glob expansion, ported
//! from `kernel/src/shell.zig` (M19 P1/P2/P6). Splitting and matching take
//! no syscalls and allocate nothing, so the operator semantics — quote and
//! escape awareness, right-to-left `>>` preference, fnmatch-style classes —
//! are fully host-testable. The execution glue lives in `user/src/sh.zig`.

/// Product version — the `ver M.m.p` the component-registry lib row
/// shows and the drift check resolves (docs/component-registry.md).
/// The registry's bump rule says when it moves.
pub const version = "1.0.0";

const std = @import("std");

fn isSpace(byte: u8) bool {
    return byte == ' ' or byte == '\t';
}

pub fn trimStart(s: []const u8) []const u8 {
    var i: usize = 0;
    while (i < s.len and isSpace(s[i])) i += 1;
    return s[i..];
}

pub fn trimEnd(s: []const u8) []const u8 {
    var end = s.len;
    while (end > 0 and isSpace(s[end - 1])) end -= 1;
    return s[0..end];
}

fn trim(s: []const u8) []const u8 {
    return trimStart(trimEnd(s));
}

// ---------------------------------------------------------------------------
// Pipe split: the FIRST `|` outside quotes. More than one is refused.
// ---------------------------------------------------------------------------

pub const PipeSplit = struct { left: []const u8, right: []const u8 };

pub const PipeSplitResult = union(enum) {
    none,
    split: PipeSplit,
    multiple,
};

pub fn pipeSplit(line: []const u8) PipeSplitResult {
    var in_quote = false;
    var in_single = false;
    var esc = false;
    var first: ?usize = null;
    var i: usize = 0;
    while (i < line.len) : (i += 1) {
        if (esc) {
            esc = false;
            continue;
        }
        if (line[i] == '\\') {
            esc = true;
            continue;
        }
        if (line[i] == '\'') {
            in_single = !in_single;
            continue;
        }
        if (line[i] == '"') in_quote = !in_quote;
        if (!in_single and !in_quote and line[i] == '|') {
            if (first == null) {
                first = i;
            } else {
                return .multiple;
            }
        }
    }
    const idx = first orelse return .none;
    return .{ .split = .{ .left = line[0..idx], .right = line[idx + 1 ..] } };
}

// ---------------------------------------------------------------------------
// Redirection split: `>>`, `>`, `<` outside quotes.
// ---------------------------------------------------------------------------

pub const RedirectOp = enum { stdout_overwrite, stdout_append, stdin_file };

pub const RedirectSplit = struct {
    left: []const u8,
    right: []const u8,
    op: RedirectOp,
};

/// Find the first redirect operator (preferring `>>` over `>`) outside
/// quoted regions. Returns the command half and the filename half, each
/// trimmed, or null when absent (or when a half is empty).
pub fn redirectSplit(line: []const u8) ?RedirectSplit {
    var in_quote = false;
    var in_single = false;
    var esc = false;
    var i: usize = 0;
    while (i < line.len) : (i += 1) {
        if (esc) {
            esc = false;
            continue;
        }
        if (line[i] == '\\') {
            esc = true;
            continue;
        }
        if (line[i] == '\'') {
            in_single = !in_single;
            continue;
        }
        if (line[i] == '"') {
            in_quote = !in_quote;
            continue;
        }
        if (in_single or in_quote) continue;
        if (line[i] == '>') {
            if (i + 1 < line.len and line[i + 1] == '>') {
                const left = trimEnd(line[0..i]);
                const right = trimStart(line[i + 2 ..]);
                if (left.len > 0 and right.len > 0) {
                    return .{ .left = left, .right = right, .op = .stdout_append };
                }
                continue;
            }
            const left = trimEnd(line[0..i]);
            const right = trimStart(line[i + 1 ..]);
            if (left.len > 0 and right.len > 0) {
                return .{ .left = left, .right = right, .op = .stdout_overwrite };
            }
            continue;
        }
        if (line[i] == '<') {
            const left = trimEnd(line[0..i]);
            const right = trimStart(line[i + 1 ..]);
            if (left.len > 0 and right.len > 0) {
                return .{ .left = left, .right = right, .op = .stdin_file };
            }
            continue;
        }
    }
    return null;
}

// ---------------------------------------------------------------------------
// Globbing: `*`, `?`, `[abc]`, `[a-z]` (fnmatch-style, iterative).
// ---------------------------------------------------------------------------

/// One `[...]` class against `c`. `pattern[p]` must be `'['`; on a match
/// returns the index just past `']'`, else null. A `']'` directly after `[`
/// is a member, not the closer; an unclosed class never matches.
fn globClass(pattern: []const u8, p: usize, c: u8) ?usize {
    var j = p + 1;
    var matched = false;
    var first = true;
    while (j < pattern.len and (pattern[j] != ']' or first)) {
        first = false;
        if (j + 2 < pattern.len and pattern[j + 1] == '-' and pattern[j + 2] != ']') {
            if (c >= pattern[j] and c <= pattern[j + 2]) matched = true;
            j += 3;
        } else {
            if (pattern[j] == c) matched = true;
            j += 1;
        }
    }
    if (j >= pattern.len or !matched) return null;
    return j + 1;
}

/// fnmatch-style matcher: `*` any run (greedy with explicit backtrack
/// points — iterative, no recursion, no allocation), `?` one byte,
/// `[...]` classes. Everything else is a literal byte.
pub fn globMatch(pattern: []const u8, name: []const u8) bool {
    var p: usize = 0;
    var n: usize = 0;
    var star_p: ?usize = null;
    var star_n: usize = 0;
    while (n < name.len) {
        if (p < pattern.len) {
            switch (pattern[p]) {
                '*' => {
                    star_p = p;
                    star_n = n;
                    p += 1;
                    continue;
                },
                '?' => {
                    p += 1;
                    n += 1;
                    continue;
                },
                '[' => {
                    if (globClass(pattern, p, name[n])) |np| {
                        p = np;
                        n += 1;
                        continue;
                    }
                },
                else => {
                    if (pattern[p] == name[n]) {
                        p += 1;
                        n += 1;
                        continue;
                    }
                },
            }
        }
        if (star_p) |sp| {
            p = sp + 1;
            star_n += 1;
            n = star_n;
        } else return false;
    }
    while (p < pattern.len and pattern[p] == '*') p += 1;
    return p == pattern.len;
}

/// True when `arg` contains an unescaped, unquoted glob metacharacter. The
/// shell tokenizer already flags quoted/escaped wildcards, so callers pass
/// the tokenizer's `arg_glob` bits; this is a convenience for tests.
pub fn hasWildcard(arg: []const u8) bool {
    for (arg) |c| {
        if (c == '*' or c == '?' or c == '[') return true;
    }
    return false;
}

// ---------------------------------------------------------------------------
// Tests (pure)
// ---------------------------------------------------------------------------

test "pipe: pipeSplit finds the first | outside quotes and refuses two" {
    const r1 = pipeSplit("echo hi | cat");
    try std.testing.expectEqualStrings("echo hi ", r1.split.left);
    try std.testing.expectEqualStrings(" cat", r1.split.right);
    try std.testing.expectEqual(PipeSplitResult.none, pipeSplit("echo a b"));
    try std.testing.expectEqual(PipeSplitResult.multiple, pipeSplit("a | b | c"));
    // Quoted and escaped pipes are literal.
    try std.testing.expectEqual(PipeSplitResult.none, pipeSplit("echo 'a|b'"));
    try std.testing.expectEqual(PipeSplitResult.none, pipeSplit("echo a\\|b"));
    try std.testing.expectEqual(PipeSplitResult.none, pipeSplit("echo \"a|b\""));
}

test "pipe: redirectSplit recognizes >, >>, < outside quotes" {
    const o = redirectSplit("echo hi > OUT.TXT").?;
    try std.testing.expectEqual(RedirectOp.stdout_overwrite, o.op);
    try std.testing.expectEqualStrings("echo hi", o.left);
    try std.testing.expectEqualStrings("OUT.TXT", o.right);

    const a = redirectSplit("echo hi >> OUT.TXT").?;
    try std.testing.expectEqual(RedirectOp.stdout_append, a.op);
    try std.testing.expectEqualStrings("OUT.TXT", a.right);

    const i = redirectSplit("cat < IN.TXT").?;
    try std.testing.expectEqual(RedirectOp.stdin_file, i.op);
    try std.testing.expectEqualStrings("cat", i.left);
    try std.testing.expectEqualStrings("IN.TXT", i.right);

    try std.testing.expect(redirectSplit("echo a b") == null);
    // Quoted operators are literal.
    try std.testing.expect(redirectSplit("echo 'a > b'") == null);
    try std.testing.expect(redirectSplit("echo a \\> b") == null);
    // A missing half is not a redirect.
    try std.testing.expect(redirectSplit("> OUT.TXT") == null);
    try std.testing.expect(redirectSplit("echo hi >") == null);
}

test "pipe: globMatch supports star, question and classes" {
    try std.testing.expect(globMatch("*.BIN", "STATUS43.BIN"));
    try std.testing.expect(globMatch("*.BIN", "PS.BIN"));
    try std.testing.expect(!globMatch("*.BIN", "PS.ELF"));
    try std.testing.expect(globMatch("P?.BIN", "PS.BIN"));
    try std.testing.expect(!globMatch("P?.BIN", "PSS.BIN"));
    try std.testing.expect(globMatch("[SP]T*", "STATUS43.BIN"));
    try std.testing.expect(globMatch("[A-Z]*.BIN", "PS.BIN"));
    try std.testing.expect(!globMatch("[a-z]*.BIN", "PS.BIN"));
    try std.testing.expect(globMatch("file[0-9].txt", "file7.txt"));
    try std.testing.expect(!globMatch("file[0-9].txt", "fileX.txt"));
    // `*` can match across dots and empty runs.
    try std.testing.expect(globMatch("*", "anything"));
    try std.testing.expect(globMatch("a*b*c", "aXbYc"));
    try std.testing.expect(globMatch("abc", "abc"));
    try std.testing.expect(!globMatch("abc", "abcd"));
    try std.testing.expect(!globMatch("abcd", "abc"));
}

test "pipe: hasWildcard flags the metacharacters" {
    try std.testing.expect(hasWildcard("*.BIN"));
    try std.testing.expect(hasWildcard("f?le"));
    try std.testing.expect(hasWildcard("[a-z]"));
    try std.testing.expect(!hasWildcard("plain.txt"));
}
