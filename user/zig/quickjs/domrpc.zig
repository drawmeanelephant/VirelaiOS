//! M211a (#3867): the DOM-RPC wire — the one channel ADR 0039's amended
//! window-surface fence admits between its one bounded consumer (WEB) and a
//! `QJS.BIN --dom-rpc` worker. One JSON object per line in each direction:
//! ops parent→worker, exactly one reply line per op worker→parent. This is
//! the protocol half only — the exec/stream mechanics and the eval dispatch
//! stay adapter-side (user/src/quickjs.zig). Parsing and reply formatting
//! are pure and bounded, so a refusal is a named reply line, never a crash.

const std = @import("std");

/// The protocol version `hello` negotiates. An op carrying a newer or older
/// version is refused `dom_rpc_version`; bumping this is an ADR question.
/// v2 (M211b, #3868) adds the worker→parent DOM request lines and their
/// answers on the WEBRPC.ANS rendezvous.
pub const version: u64 = 2;

/// One op line's bound: the 1 MiB per-eval source ceiling plus a fixed
/// envelope for the JSON framing it may carry. The staged ops stream (every
/// line of one worker session) shares the bound — the producer clips its
/// op list to fit rather than split an op across lines.
pub const op_max: usize = 1_114_112;
pub const ops_max: usize = op_max;

/// Raw eval output (print calls plus the completion value) carried in one
/// reply before `clipped` is set. The bound is sized so the escaped form
/// always fits `reply_max`.
pub const out_max: usize = 2_048;

/// One reply line's bound: 2,048 output bytes escape to at most 12,288
/// plus the envelope. DOM request lines and answer lines share the bound —
/// they ride the same JSON-lines envelope the replies do.
pub const reply_max: usize = 16_384;

/// M211b (#3868): the per-script DOM budgets. `script_ops_max` counts every
/// DOM op one eval issues — reads and `console.log` alike — and the wall
/// budget is the engine's own four-second eval deadline, so a script that
/// spins in JS or waits on unanswered ops dies at the same named bound.
/// `arg_max` clips one request argument (id, attribute name, log text);
/// `value_max` clips one answered value.
pub const script_ops_max: u32 = 64;
pub const arg_max: usize = 4_096;
pub const value_max: usize = 2_048;

/// The host-dom callback's null sentinel: an answered op whose value is
/// JSON `null` (an absent node or attribute), distinct from a refusal.
pub const dom_null: usize = std.math.maxInt(usize);

/// `name` reports whether `op` is a wire-legal DOM op name — the same
/// charset the ops parser enforces, so a name that could never be quoted
/// back is refused `dom_op_unknown` without a round trip.
pub fn nameOk(op: []const u8) bool {
    if (op.len == 0 or op.len > 32) return false;
    for (op) |c| {
        switch (c) {
            'a'...'z', 'A'...'Z', '0'...'9', '_', '.', '-' => continue,
            else => return false,
        }
    }
    return true;
}

pub const Op = struct {
    /// The decoded op name — present on every successful parse, restricted
    /// to `[A-Za-z0-9_.-]` so it can be quoted back in a reply verbatim.
    name: []const u8,
    /// `hello`'s negotiated version, when the member exists.
    version: ?u64 = null,
    /// `eval`'s decoded source, when the member exists.
    src: ?[]const u8 = null,
};

pub const Error = error{DomRpcParse};

/// `line` is decoded IN PLACE: a JSON string's decoded form is never longer
/// than its spelling, so the write cursor always trails the read cursor
/// inside the caller's own buffer and no second allocation is needed.
pub fn parseOp(line: []u8) Error!Op {
    var p: Parser = .{ .s = line };
    p.ws();
    try p.expect('{');
    var op: Op = .{ .name = &.{} };
    p.ws();
    if (!p.eat('}')) {
        while (true) {
            p.ws();
            const key = try p.string();
            p.ws();
            try p.expect(':');
            p.ws();
            if (std.mem.eql(u8, key, "op")) {
                op.name = try p.string();
            } else if (std.mem.eql(u8, key, "version")) {
                op.version = try p.integer();
            } else if (std.mem.eql(u8, key, "src")) {
                if (p.peek() != '"') return error.DomRpcParse;
                op.src = try p.string();
            } else {
                try p.value(8);
            }
            p.ws();
            if (p.eat(',')) continue;
            try p.expect('}');
            break;
        }
    }
    p.ws();
    if (p.i != p.s.len or op.name.len == 0 or op.name.len > 32)
        return error.DomRpcParse;
    for (op.name) |c| {
        switch (c) {
            'a'...'z', 'A'...'Z', '0'...'9', '_', '.', '-' => continue,
            else => return error.DomRpcParse,
        }
    }
    return op;
}

/// One decoded answer line from the WEBRPC.ANS rendezvous — the parent's
/// half of a DOM request. `seq` correlates the answer to the request the
/// worker last wrote; anything else is ignored as stale.
pub const Answer = struct {
    seq: u64,
    ok: bool,
    /// `value` carries the answered bytes; `has_value` distinguishes a
    /// present member from an absent one, and `value == null` (JSON null)
    /// means an absent node/attribute — JS sees null either way.
    value: ?[]const u8 = null,
    has_value: bool = false,
    /// The named refusal when `ok` is false — verbatim, the worker quotes
    /// it back to JS as the exception name.
    err: ?[]const u8 = null,
};

/// `line` is decoded IN PLACE like parseOp. An answer missing `seq` or
/// `ok`, or a trailing byte, is malformed — the caller keeps polling
/// rather than mistake a half-published line for a refusal.
pub fn parseAnswer(line: []u8) Error!Answer {
    var p: Parser = .{ .s = line };
    p.ws();
    try p.expect('{');
    var a: Answer = .{ .seq = 0, .ok = false };
    var has_seq = false;
    var has_ok = false;
    p.ws();
    if (!p.eat('}')) {
        while (true) {
            p.ws();
            const key = try p.string();
            p.ws();
            try p.expect(':');
            p.ws();
            if (std.mem.eql(u8, key, "seq")) {
                a.seq = try p.integer();
                has_seq = true;
            } else if (std.mem.eql(u8, key, "ok")) {
                a.ok = try p.boolean();
                has_ok = true;
            } else if (std.mem.eql(u8, key, "value")) {
                a.value = try p.nullString();
                a.has_value = true;
            } else if (std.mem.eql(u8, key, "error")) {
                a.err = try p.string();
            } else {
                try p.value(8);
            }
            p.ws();
            if (p.eat(',')) continue;
            try p.expect('}');
            break;
        }
    }
    p.ws();
    if (p.i != p.s.len or !has_seq or !has_ok) return error.DomRpcParse;
    if (a.err) |word| {
        if (!nameOk(word)) return error.DomRpcParse;
    }
    return a;
}

/// Writes one DOM request line into `dst` —
/// `{"op":"<op>","seq":N,"a":"..","b":".."}` — returning the bytes used
/// including the trailing newline. `op` must already pass nameOk; `a`/`b`
/// are clipped to `arg_max` so a request can never outgrow `reply_max`.
/// The caller sizes `dst` from `reply_max`; the same trap rule as `escape`
/// applies.
pub fn request(dst: []u8, op: []const u8, seq: u64, a: []const u8, b: []const u8) usize {
    var w = (std.fmt.bufPrint(dst, "{{\"op\":\"{s}\",\"seq\":{d}", .{ op, seq }) catch unreachable).len;
    const args = [_]struct { k: []const u8, v: []const u8 }{
        .{ .k = "a", .v = a[0..@min(a.len, arg_max)] },
        .{ .k = "b", .v = b[0..@min(b.len, arg_max)] },
    };
    for (args) |arg| {
        if (arg.v.len == 0) continue;
        w += (std.fmt.bufPrint(dst[w..], ",\"{s}\":\"", .{arg.k}) catch unreachable).len;
        w += escape(dst[w..], arg.v);
        dst[w] = '"';
        w += 1;
    }
    dst[w] = '}';
    dst[w + 1] = '\n';
    return w + 2;
}

/// Writes `bytes` JSON-string-escaped into `dst`, returning the bytes used.
/// `"`, `\` and control bytes below 0x20 escape; every other byte —
/// including UTF-8 continuation bytes — passes through, since JS output is
/// already UTF-8. The caller sizes `dst` from `reply_max`/`out_max`, so a
/// short buffer is a caller bug and traps rather than emitting a
/// truncated escape mid-reply.
pub fn escape(dst: []u8, bytes: []const u8) usize {
    const hex = "0123456789abcdef";
    var w: usize = 0;
    const put = struct {
        fn run(d: []u8, at: *usize, b: u8) void {
            d[at.*] = b;
            at.* += 1;
        }
    }.run;
    for (bytes) |c| {
        switch (c) {
            '"', '\\' => {
                put(dst, &w, '\\');
                put(dst, &w, c);
            },
            '\n' => {
                put(dst, &w, '\\');
                put(dst, &w, 'n');
            },
            '\r' => {
                put(dst, &w, '\\');
                put(dst, &w, 'r');
            },
            '\t' => {
                put(dst, &w, '\\');
                put(dst, &w, 't');
            },
            8 => {
                put(dst, &w, '\\');
                put(dst, &w, 'b');
            },
            12 => {
                put(dst, &w, '\\');
                put(dst, &w, 'f');
            },
            0x00...0x07, 0x0b, 0x0e...0x1f => {
                put(dst, &w, '\\');
                put(dst, &w, 'u');
                put(dst, &w, '0');
                put(dst, &w, '0');
                put(dst, &w, hex[c >> 4]);
                put(dst, &w, hex[c & 15]);
            },
            else => put(dst, &w, c),
        }
    }
    return w;
}

const Parser = struct {
    s: []u8,
    i: usize = 0,

    fn ws(self: *Parser) void {
        while (self.i < self.s.len) : (self.i += 1) {
            switch (self.s[self.i]) {
                ' ', '\t', '\r', '\n' => continue,
                else => return,
            }
        }
    }

    fn peek(self: *Parser) ?u8 {
        if (self.i >= self.s.len) return null;
        return self.s[self.i];
    }

    fn eat(self: *Parser, c: u8) bool {
        if (self.peek() != c) return false;
        self.i += 1;
        return true;
    }

    fn expect(self: *Parser, c: u8) Error!void {
        if (!self.eat(c)) return error.DomRpcParse;
    }

    /// Requires a string member: a key or a field the protocol reads must
    /// never silently accept a different JSON kind.
    fn string(self: *Parser) Error![]const u8 {
        if (!self.eat('"')) return error.DomRpcParse;
        const start = self.i;
        var w = start;
        while (true) {
            const c = self.peek() orelse return error.DomRpcParse;
            self.i += 1;
            switch (c) {
                '"' => return self.s[start..w],
                '\\' => {
                    const e = self.peek() orelse return error.DomRpcParse;
                    self.i += 1;
                    switch (e) {
                        '"', '\\', '/' => {
                            self.s[w] = e;
                            w += 1;
                        },
                        'b' => {
                            self.s[w] = 8;
                            w += 1;
                        },
                        'f' => {
                            self.s[w] = 12;
                            w += 1;
                        },
                        'n' => {
                            self.s[w] = '\n';
                            w += 1;
                        },
                        'r' => {
                            self.s[w] = '\r';
                            w += 1;
                        },
                        't' => {
                            self.s[w] = '\t';
                            w += 1;
                        },
                        'u' => {
                            const cp = try self.unicodeEscape();
                            w += std.unicode.utf8Encode(cp, self.s[w..][0..4]) catch
                                return error.DomRpcParse;
                        },
                        else => return error.DomRpcParse,
                    }
                },
                else => {
                    if (c < 0x20) return error.DomRpcParse;
                    self.s[w] = c;
                    w += 1;
                },
            }
        }
    }

    fn hex4(self: *Parser) Error!u21 {
        var v: u21 = 0;
        for (0..4) |_| {
            const c = self.peek() orelse return error.DomRpcParse;
            self.i += 1;
            const d: u21 = switch (c) {
                '0'...'9' => c - '0',
                'a'...'f' => c - 'a' + 10,
                'A'...'F' => c - 'A' + 10,
                else => return error.DomRpcParse,
            };
            v = v * 16 + d;
        }
        return v;
    }

    fn unicodeEscape(self: *Parser) Error!u21 {
        const hi = try self.hex4();
        if (hi >= 0xD800 and hi <= 0xDBFF) {
            if (self.peek() != '\\') return error.DomRpcParse;
            self.i += 1;
            if (self.peek() != 'u') return error.DomRpcParse;
            self.i += 1;
            const lo = try self.hex4();
            if (lo < 0xDC00 or lo > 0xDFFF) return error.DomRpcParse;
            return 0x10000 + ((hi - 0xD800) << 10) + (lo - 0xDC00);
        }
        if (hi >= 0xDC00 and hi <= 0xDFFF) return error.DomRpcParse;
        return hi;
    }

    /// An unsigned integer member — a float, negative or absent digit is
    /// malformed input for a version field, not a negotiation.
    fn integer(self: *Parser) Error!u64 {
        var v: u64 = 0;
        var digits = false;
        while (self.peek()) |c| {
            if (c < '0' or c > '9') break;
            digits = true;
            v = std.math.mul(u64, v, 10) catch return error.DomRpcParse;
            v = std.math.add(u64, v, c - '0') catch return error.DomRpcParse;
            self.i += 1;
        }
        if (!digits) return error.DomRpcParse;
        return v;
    }

    /// A JSON `true`/`false` member — an answer's `ok` must never silently
    /// accept another kind.
    fn boolean(self: *Parser) Error!bool {
        if (self.peek() == 't') {
            try self.literal("true");
            return true;
        }
        try self.literal("false");
        return false;
    }

    /// A member that is either a JSON string or literal `null` — an
    /// answer's `value` carries both.
    fn nullString(self: *Parser) Error!?[]const u8 {
        if (self.peek() == 'n') {
            try self.literal("null");
            return null;
        }
        return try self.string();
    }

    /// Validates one JSON number's grammar without keeping it (members the
    /// protocol does not read are parsed and discarded, not refused).
    fn number(self: *Parser) Error!void {
        _ = self.eat('-');
        var digits = false;
        while (self.peek()) |c| {
            if (c < '0' or c > '9') break;
            digits = true;
            self.i += 1;
        }
        if (!digits) return error.DomRpcParse;
        if (self.eat('.')) {
            var frac = false;
            while (self.peek()) |c| {
                if (c < '0' or c > '9') break;
                frac = true;
                self.i += 1;
            }
            if (!frac) return error.DomRpcParse;
        }
        if (self.eat('e') or self.eat('E')) {
            _ = self.eat('+') or self.eat('-');
            var exp = false;
            while (self.peek()) |c| {
                if (c < '0' or c > '9') break;
                exp = true;
                self.i += 1;
            }
            if (!exp) return error.DomRpcParse;
        }
    }

    fn literal(self: *Parser, text: []const u8) Error!void {
        if (self.s.len - self.i < text.len or
            !std.mem.eql(u8, self.s[self.i..][0..text.len], text))
            return error.DomRpcParse;
        self.i += text.len;
    }

    /// Parses and discards any JSON value — unknown members carry whatever
    /// well-formed payload the producer sent; `depth` bounds nesting.
    fn value(self: *Parser, depth: u8) Error!void {
        if (depth == 0) return error.DomRpcParse;
        const c = self.peek() orelse return error.DomRpcParse;
        switch (c) {
            '"' => _ = try self.string(),
            '{' => {
                self.i += 1;
                self.ws();
                if (self.eat('}')) return;
                while (true) {
                    self.ws();
                    _ = try self.string();
                    self.ws();
                    try self.expect(':');
                    self.ws();
                    try self.value(depth - 1);
                    self.ws();
                    if (self.eat(',')) continue;
                    try self.expect('}');
                    return;
                }
            },
            '[' => {
                self.i += 1;
                self.ws();
                if (self.eat(']')) return;
                while (true) {
                    self.ws();
                    try self.value(depth - 1);
                    self.ws();
                    if (self.eat(',')) continue;
                    try self.expect(']');
                    return;
                }
            },
            't' => try self.literal("true"),
            'f' => try self.literal("false"),
            'n' => try self.literal("null"),
            else => try self.number(),
        }
    }
};

test "hello and eval ops parse, unknown members are skipped" {
    var hello: [64]u8 = undefined;
    const hello_text = "{\"op\":\"hello\",\"version\":1,\"x\":[1,{}]}";
    @memcpy(hello[0..hello_text.len], hello_text);
    const h = try parseOp(hello[0..hello_text.len]);
    try std.testing.expectEqualStrings("hello", h.name);
    try std.testing.expectEqual(@as(?u64, 1), h.version);

    var line: [80]u8 = undefined;
    const text = "{\"op\":\"eval\",\"src\":\"var a=1+2;\\nprint(\\\"x\\\")\"}";
    @memcpy(line[0..text.len], text);
    const e = try parseOp(line[0..text.len]);
    try std.testing.expectEqualStrings("eval", e.name);
    try std.testing.expectEqualStrings("var a=1+2;\nprint(\"x\")", e.src.?);
}

test "malformed ops refuse by name, never crash" {
    const cases = [_][]const u8{
        "",                                "not json",         "{}",           "{\"op\":1}",       "{\"op\":\"eval\",\"src\":[1]}",
        "{\"op\":\"hi\",\"version\":1.5}", "{\"op\":\"hi\",}", "{\"op\":\"hi", "{\"op\":\"a b\"}", "{\"op\":\"eval\",\"src\":\"\\uD800\"}",
        "{\"op\":\"x\",\"extra\":{{{",
    };
    for (cases) |c| {
        var buf: [128]u8 = undefined;
        @memcpy(buf[0..c.len], c);
        try std.testing.expectError(error.DomRpcParse, parseOp(buf[0..c.len]));
    }
}

test "escaped output appends bounded JSON" {
    var storage: [64]u8 = undefined;
    const used = escape(&storage, "a\"b\\c\nd\x01");
    try std.testing.expectEqualStrings("a\\\"b\\\\c\\nd\\u0001", storage[0..used]);
}

test "dom requests emit op, seq and bounded args" {
    var buf: [reply_max]u8 = undefined;
    const used = request(&buf, "node.attr", 7, "lead", "data-note");
    try std.testing.expectEqualStrings("{\"op\":\"node.attr\",\"seq\":7,\"a\":\"lead\",\"b\":\"data-note\"}\n", buf[0..used]);
    const bare = request(&buf, "doc.title", 2, "", "");
    try std.testing.expectEqualStrings("{\"op\":\"doc.title\",\"seq\":2}\n", buf[0..bare]);
    // args clip at arg_max and escape like any JSON string
    var big: [arg_max + 8]u8 = @splat('x');
    big[arg_max] = '"';
    const clipped = request(&buf, "console.log", 1, &big, "");
    try std.testing.expect(clipped < reply_max);
    const head = "{\"op\":\"console.log\",\"seq\":1,\"a\":\"";
    try std.testing.expectEqualStrings(head ++ @as([arg_max]u8, @splat('x')) ++ "\"}\n", buf[0..clipped]);
}

test "answers parse value, null and named refusals" {
    var line: [128]u8 = undefined;
    const ok_text = "{\"op\":\"node.text\",\"seq\":7,\"ok\":true,\"value\":\"alpha\"}";
    @memcpy(line[0..ok_text.len], ok_text);
    const ok = try parseAnswer(line[0..ok_text.len]);
    try std.testing.expect(ok.ok and ok.seq == 7 and ok.has_value);
    try std.testing.expectEqualStrings("alpha", ok.value.?);

    var nbuf: [96]u8 = undefined;
    const null_text = "{\"op\":\"node.attr\",\"seq\":8,\"ok\":true,\"value\":null}";
    @memcpy(nbuf[0..null_text.len], null_text);
    const n = try parseAnswer(nbuf[0..null_text.len]);
    try std.testing.expect(n.ok and n.has_value and n.value == null);

    var ebuf: [96]u8 = undefined;
    const err_text = "{\"op\":\"node.setText\",\"seq\":9,\"ok\":false,\"error\":\"dom_op_refused\"}";
    @memcpy(ebuf[0..err_text.len], err_text);
    const e = try parseAnswer(ebuf[0..err_text.len]);
    try std.testing.expect(!e.ok and e.seq == 9);
    try std.testing.expectEqualStrings("dom_op_refused", e.err.?);
}

test "malformed or incomplete answers refuse, never crash" {
    const cases = [_][]const u8{
        "",                                              "not json",             "{}",                                  "{\"seq\":1}",                                "{\"ok\":true}",
        "{\"seq\":\"1\",\"ok\":true}",                   "{\"seq\":1,\"ok\":1}", "{\"seq\":1,\"ok\":true,\"value\":4}", "{\"seq\":1,\"ok\":false,\"error\":\"a b\"}", "{\"seq\":1,\"ok\":true} trailing",
        "{\"seq\":1,\"ok\":true,\"value\":\"\\uD800\"}",
    };
    for (cases) |c| {
        var buf: [128]u8 = undefined;
        @memcpy(buf[0..c.len], c);
        try std.testing.expectError(error.DomRpcParse, parseAnswer(buf[0..c.len]));
    }
}

test "dom op names follow the wire charset" {
    try std.testing.expect(nameOk("doc.title"));
    try std.testing.expect(nameOk("node.attr"));
    try std.testing.expect(nameOk("console.log"));
    try std.testing.expect(!nameOk(""));
    try std.testing.expect(!nameOk("a b"));
    try std.testing.expect(!nameOk("a\"b"));
    try std.testing.expect(!nameOk(&@as([33]u8, @splat('x'))));
}
