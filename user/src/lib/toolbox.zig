//! VirelaiOS userland tool core (M49 SD3 — issue #1130, umbrella #1127).
//!
//! A bounded, freestanding, busybox-style multicall: ONE engine implements
//! `head`, `tail`, `wc`, `grep`, `sort`, `cut`, `test`, `[` and `printf`,
//! dispatched by name. It is pure logic over three seams so the whole
//! surface is host-testable:
//!
//!   * `Stream` — a byte source (stdin or an open host-share file);
//!   * `Writer` — the output sink (the shell's capture buffer or the raw
//!     console, depending on the caller);
//!   * `Host` — host-share file access (stat/open/read/close).
//!
//! Both the shell (`SH.BIN`/`TERM.BIN` link the engine as builtins so pipes
//! and redirection capture their output) and the standalone multicall app
//! (`TOOL.BIN`, first argument selects the tool) sit on this file. The WASM
//! `wc` capstone (`user/src/wasm-corpus/wc.wasm`) is deliberately NOT
//! reused: it is a WASM guest for `WASM.BIN`, not an in-process library,
//! and the engine is a few hundred bytes of the shell's existing BSS.
//!
//! Bounded by construction — no allocation anywhere:
//!   * lines are capped at `line_cap` (512) bytes; a longer line is read
//!     through but only its first 512 bytes are used (honest, documented);
//!   * `sort` buffers at most `sort_cap` (8192) bytes; input beyond that is
//!     consumed but not sorted (documented, never silent truncation of the
//!     output count);
//!   * `wc`/`grep`/`cut`/`head`/`tail` stream line by line.

/// Product version — the `ver M.m.p` the component-registry lib row
/// shows and the drift check resolves (docs/component-registry.md).
/// The registry's bump rule says when it moves.
pub const version = "1.0.0";

const std = @import("std");

/// The most bytes used from one line (longer lines are read through but
/// truncated to this bound).
pub const line_cap: usize = 512;
/// `sort`'s fixed line-buffer bound.
pub const sort_cap: usize = 8192;

// Large per-tool working buffers live in module BSS, not on the task stack:
// the EL0 stack is small and `tail`'s ring alone is tens of KiB. One tool
// runs at a time (foreground command execution), documented.
var tail_ring: [64][line_cap]u8 = undefined;
var tail_lens: [64]usize = undefined;
var sort_store: [sort_cap]u8 = undefined;
var sort_lens: [256]usize = undefined;
var sort_offsets: [256]usize = undefined;

/// The multicall tools.
pub const Tool = enum { head, tail, wc, grep, sort, cut, test_, bracket, printf };

/// Lookup a tool by its typed name.
pub fn lookup(name: []const u8) ?Tool {
    const table = .{
        .{ "head", Tool.head },     .{ "tail", Tool.tail },
        .{ "wc", Tool.wc },         .{ "grep", Tool.grep },
        .{ "sort", Tool.sort },     .{ "cut", Tool.cut },
        .{ "test", Tool.test_ },    .{ "[", Tool.bracket },
        .{ "printf", Tool.printf },
    };
    inline for (table) |row| {
        if (std.mem.eql(u8, name, row[0])) return row[1];
    }
    return null;
}

/// A byte source: `read` returns bytes copied, 0 on EOF. Never blocks.
pub const Stream = struct {
    ctx: ?*anyopaque = null,
    read_fn: *const fn (ctx: ?*anyopaque, buf: []u8) usize,

    pub fn read(self: Stream, buf: []u8) usize {
        return self.read_fn(self.ctx, buf);
    }
};

/// A byte sink.
pub const Writer = struct {
    ctx: ?*anyopaque = null,
    write_fn: *const fn (ctx: ?*anyopaque, bytes: []const u8) void,

    pub fn write(self: Writer, bytes: []const u8) void {
        if (bytes.len > 0) self.write_fn(self.ctx, bytes);
    }
};

/// Host-share file access. `stat` returns 1 = regular file, 2 = directory,
/// 0 = absent.
pub const Host = struct {
    ctx: ?*anyopaque = null,
    open_fn: *const fn (ctx: ?*anyopaque, name: []const u8) u64,
    read_fn: *const fn (ctx: ?*anyopaque, handle: u64, buf: []u8) usize,
    close_fn: *const fn (ctx: ?*anyopaque, handle: u64) void,
    stat_fn: *const fn (ctx: ?*anyopaque, name: []const u8) u8,

    pub fn open(self: Host, name: []const u8) u64 {
        return self.open_fn(self.ctx, name);
    }

    pub fn read(self: Host, handle: u64, buf: []u8) usize {
        return self.read_fn(self.ctx, handle, buf);
    }

    pub fn close(self: Host, handle: u64) void {
        self.close_fn(self.ctx, handle);
    }

    pub fn stat(self: Host, name: []const u8) u8 {
        return self.stat_fn(self.ctx, name);
    }
};

/// A line-at-a-time reader over a `Stream` with a fixed line buffer and a
/// bounded chunk window (a single read may deliver many lines). A line
/// longer than the buffer is consumed through its newline but reported at
/// the cap. `next` returns null at EOF; `last_newline` says whether the
/// line just returned was newline-terminated (for byte-accurate `wc`).
pub const LineReader = struct {
    stream: Stream,
    buf: [line_cap]u8 = undefined,
    len: usize = 0,
    chunk: [256]u8 = undefined,
    chunk_len: usize = 0,
    chunk_pos: usize = 0,
    eof: bool = false,
    /// True when at least one byte of the last line was dropped at the cap.
    truncated: bool = false,
    /// True when the last returned line ended in a newline.
    last_newline: bool = false,

    pub fn init(stream: Stream) LineReader {
        return .{ .stream = stream };
    }

    fn nextChunk(self: *LineReader) ?u8 {
        while (self.chunk_pos >= self.chunk_len) {
            if (self.eof) return null;
            const n = self.stream.read(&self.chunk);
            if (n == 0) {
                self.eof = true;
                return null;
            }
            self.chunk_len = n;
            self.chunk_pos = 0;
        }
        const b = self.chunk[self.chunk_pos];
        self.chunk_pos += 1;
        return b;
    }

    pub fn next(self: *LineReader) ?[]const u8 {
        self.len = 0;
        self.truncated = false;
        self.last_newline = false;
        var any = false;
        while (self.nextChunk()) |b| {
            any = true;
            if (b == '\n') {
                self.last_newline = true;
                var line = self.buf[0..self.len];
                if (line.len > 0 and line[line.len - 1] == '\r') line = line[0 .. line.len - 1];
                return line;
            }
            if (self.len < line_cap) {
                self.buf[self.len] = b;
                self.len += 1;
            } else {
                self.truncated = true;
            }
        }
        if (!any) return null;
        var line = self.buf[0..self.len];
        if (line.len > 0 and line[line.len - 1] == '\r') line = line[0 .. line.len - 1];
        return line;
    }
};

// ---------------------------------------------------------------------------
// Small formatting helpers (no std.fmt in the hot paths)
// ---------------------------------------------------------------------------

fn writeUint(w: Writer, v: u64) void {
    var buf: [20]u8 = undefined;
    var n: usize = 0;
    var x = v;
    if (x == 0) {
        w.write("0");
        return;
    }
    while (x > 0) {
        buf[n] = '0' + @as(u8, @intCast(x % 10));
        n += 1;
        x /= 10;
    }
    var out: [20]u8 = undefined;
    var i: usize = 0;
    while (i < n) : (i += 1) out[i] = buf[n - 1 - i];
    w.write(out[0..n]);
}

fn writePadded(w: Writer, v: u64, width: usize) void {
    var buf: [24]u8 = undefined;
    var n: usize = 0;
    var x = v;
    if (x == 0) {
        buf[n] = '0';
        n += 1;
    } else {
        while (x > 0) {
            buf[n] = '0' + @as(u8, @intCast(x % 10));
            n += 1;
            x /= 10;
        }
    }
    var pad: usize = 0;
    while (pad + n < width and pad < buf.len) : (pad += 1) w.write(" ");
    var out: [24]u8 = undefined;
    var i: usize = 0;
    while (i < n) : (i += 1) out[i] = buf[n - 1 - i];
    w.write(out[0..n]);
}

fn digits(v: u64) usize {
    var n: usize = 1;
    var x = v;
    while (x >= 10) : (x /= 10) n += 1;
    return n;
}

fn eqlIgnoreCase(a: []const u8, b: []const u8) bool {
    if (a.len != b.len) return false;
    for (a, b) |x, y| {
        if (std.ascii.toLower(x) != std.ascii.toLower(y)) return false;
    }
    return true;
}

fn parseUint(s: []const u8) ?u64 {
    if (s.len == 0) return null;
    var v: u64 = 0;
    for (s) |c| {
        if (c < '0' or c > '9') return null;
        v = v * 10 + (c - '0');
        if (v > 1_000_000_000) return null;
    }
    return v;
}

// ---------------------------------------------------------------------------
// Input iteration: stdin or named files, in order.
// ---------------------------------------------------------------------------

const Source = struct {
    stream: Stream,
    name: []const u8 = "-",
    close_handle: u64 = 0,
    host: ?Host = null,

    fn close(self: *Source) void {
        if (self.host) |h| {
            if (self.close_handle != 0) h.close(self.close_handle);
        }
    }
};

fn fileStream(host: Host, handle: u64) Stream {
    _ = host;
    return .{
        .ctx = @ptrFromInt(handle),
        .read_fn = struct {
            fn read(ctx: ?*anyopaque, buf: []u8) usize {
                // The Host is a static seam in practice (the shell's abi calls);
                // the handle travels in ctx and the host functions are passed
                // through the module-level test hook below.
                const h: u64 = @intFromPtr(ctx.?);
                return active_host.read(h, buf);
            }
        }.read,
    };
}

/// The host used by `fileStream`'s read trampoline. The engine has exactly
/// one host per invocation, so a module-local is honest here (documented);
/// callers that need another host must not nest invocations (the shell runs
/// one command at a time).
var active_host: Host = .{
    .open_fn = undefined,
    .read_fn = undefined,
    .close_fn = undefined,
    .stat_fn = undefined,
};

fn openSource(argv_file: []const u8, stdin: Stream, host: Host) ?Source {
    if (std.mem.eql(u8, argv_file, "-")) {
        return .{ .stream = stdin, .name = "-" };
    }
    const st = host.stat(argv_file);
    if (st != 1) return null;
    const h = host.open(argv_file);
    if (h == 0) return null;
    active_host = host;
    return .{
        .stream = fileStream(host, h),
        .name = argv_file,
        .close_handle = h,
        .host = host,
    };
}

// ---------------------------------------------------------------------------
// Tools
// ---------------------------------------------------------------------------

/// `head [-n N] [FILE...]`
fn runHead(argv: []const []const u8, stdin: Stream, host: Host, out: Writer) u8 {
    var count: u64 = 10;
    var first_file: usize = 1;
    if (argv.len > 2 and std.mem.eql(u8, argv[1], "-n")) {
        count = parseUint(argv[2]) orelse {
            out.write("head: invalid number of lines\n");
            return 1;
        };
        first_file = 3;
    } else if (argv.len > 1 and argv[1].len > 1 and argv[1][0] == '-') {
        count = parseUint(argv[1][1..]) orelse {
            out.write("head: invalid number of lines\n");
            return 1;
        };
        first_file = 2;
    }
    var status: u8 = 0;
    var files: usize = 0;
    var fi = first_file;
    while (fi <= argv.len) : (fi += 1) {
        if (fi == argv.len and files > 0) break;
        const name: []const u8 = if (fi < argv.len) argv[fi] else "-";
        var src = openSource(name, stdin, host) orelse {
            out.write("head: ");
            out.write(name);
            out.write(": no such file\n");
            status = 1;
            files += 1;
            continue;
        };
        defer src.close();
        files += 1;
        var reader = LineReader.init(src.stream);
        var emitted: u64 = 0;
        while (emitted < count) : (emitted += 1) {
            const line = reader.next() orelse break;
            out.write(line);
            out.write("\n");
        }
        if (fi == argv.len) break;
    }
    return status;
}

/// `tail [-n N] [FILE...]` — a bounded ring of the last N lines.
fn runTail(argv: []const []const u8, stdin: Stream, host: Host, out: Writer) u8 {
    var count: u64 = 10;
    var first_file: usize = 1;
    if (argv.len > 2 and std.mem.eql(u8, argv[1], "-n")) {
        count = parseUint(argv[2]) orelse {
            out.write("tail: invalid number of lines\n");
            return 1;
        };
        first_file = 3;
    } else if (argv.len > 1 and argv[1].len > 1 and argv[1][0] == '-') {
        count = parseUint(argv[1][1..]) orelse {
            out.write("tail: invalid number of lines\n");
            return 1;
        };
        first_file = 2;
    }
    if (count > 64) count = 64; // bounded ring (documented)
    var status: u8 = 0;
    var files: usize = 0;
    var fi = first_file;
    while (fi <= argv.len) : (fi += 1) {
        if (fi == argv.len and files > 0) break;
        const name: []const u8 = if (fi < argv.len) argv[fi] else "-";
        var src = openSource(name, stdin, host) orelse {
            out.write("tail: ");
            out.write(name);
            out.write(": no such file\n");
            status = 1;
            files += 1;
            continue;
        };
        defer src.close();
        files += 1;
        var total: usize = 0;
        var reader = LineReader.init(src.stream);
        while (reader.next()) |line| {
            const slot = total % @as(usize, @intCast(count));
            const n = @min(line.len, line_cap);
            @memcpy(tail_ring[slot][0..n], line[0..n]);
            tail_lens[slot] = n;
            total += 1;
        }
        const kept = @min(total, @as(usize, @intCast(count)));
        var i: usize = 0;
        while (i < kept) : (i += 1) {
            const idx = (total - kept + i) % @as(usize, @intCast(count));
            out.write(tail_ring[idx][0..tail_lens[idx]]);
            out.write("\n");
        }
        if (fi == argv.len) break;
    }
    return status;
}

const Counts = struct { lines: u64 = 0, words: u64 = 0, bytes: u64 = 0 };

fn countStream(stream: Stream) Counts {
    var c = Counts{};
    var reader = LineReader.init(stream);
    while (reader.next()) |line| {
        c.lines += 1;
        c.bytes += line.len;
        if (reader.last_newline) c.bytes += 1;
        var in_word = false;
        for (line) |b| {
            const ws = b == ' ' or b == '\t';
            if (!ws and !in_word) {
                c.words += 1;
                in_word = true;
            } else if (ws) {
                in_word = false;
            }
        }
    }
    return c;
}

/// `wc [-l] [-w] [-c] [FILE...]`
fn runWc(argv: []const []const u8, stdin: Stream, host: Host, out: Writer) u8 {
    var show_l = false;
    var show_w = false;
    var show_c = false;
    var first_file: usize = 1;
    while (first_file < argv.len and argv[first_file].len > 1 and argv[first_file][0] == '-') {
        for (argv[first_file][1..]) |flag| switch (flag) {
            'l' => show_l = true,
            'w' => show_w = true,
            'c' => show_c = true,
            else => {},
        };
        first_file += 1;
    }
    const all = !show_l and !show_w and !show_c;
    var total = Counts{};
    var files: usize = 0;
    var status: u8 = 0;

    var fi = first_file;
    while (fi <= argv.len) : (fi += 1) {
        if (fi == argv.len and files > 0) break;
        const name: []const u8 = if (fi < argv.len) argv[fi] else "-";
        var src = openSource(name, stdin, host) orelse {
            out.write("wc: ");
            out.write(name);
            out.write(": no such file\n");
            status = 1;
            files += 1;
            continue;
        };
        defer src.close();
        files += 1;
        const c = countStream(src.stream);
        total.lines += c.lines;
        total.words += c.words;
        total.bytes += c.bytes;
        if (all) {
            writeUint(out, c.lines);
            out.write(" ");
            writeUint(out, c.words);
            out.write(" ");
            writeUint(out, c.bytes);
        } else {
            var first = true;
            if (show_l) {
                writeUint(out, c.lines);
                first = false;
            }
            if (show_w) {
                if (!first) out.write(" ");
                writeUint(out, c.words);
                first = false;
            }
            if (show_c) {
                if (!first) out.write(" ");
                writeUint(out, c.bytes);
            }
        }
        if (name.len > 0 and !std.mem.eql(u8, name, "-")) {
            out.write(" ");
            out.write(name);
        }
        out.write("\n");
        if (fi == argv.len) break;
    }
    if (files > 1) {
        if (all) {
            writeUint(out, total.lines);
            out.write(" ");
            writeUint(out, total.words);
            out.write(" ");
            writeUint(out, total.bytes);
        } else {
            var first = true;
            if (show_l) {
                writeUint(out, total.lines);
                first = false;
            }
            if (show_w) {
                if (!first) out.write(" ");
                writeUint(out, total.words);
                first = false;
            }
            if (show_c) {
                if (!first) out.write(" ");
                writeUint(out, total.bytes);
            }
        }
        out.write(" total\n");
    }
    return status;
}

/// `grep [-i] [-v] [-c] [-n] PATTERN [FILE...]`
fn runGrep(argv: []const []const u8, stdin: Stream, host: Host, out: Writer) u8 {
    var ignore_case = false;
    var invert = false;
    var count_only = false;
    var show_num = false;
    var i: usize = 1;
    while (i < argv.len and argv[i].len > 1 and argv[i][0] == '-') : (i += 1) {
        for (argv[i][1..]) |flag| switch (flag) {
            'i' => ignore_case = true,
            'v' => invert = true,
            'c' => count_only = true,
            'n' => show_num = true,
            else => {
                out.write("grep: usage: grep [-i] [-v] [-c] [-n] PATTERN [FILE...]\n");
                return 2;
            },
        };
    }
    if (i >= argv.len) {
        out.write("grep: usage: grep [-i] [-v] [-c] [-n] PATTERN [FILE...]\n");
        return 2;
    }
    const pattern = argv[i];
    i += 1;
    const first_file = i;
    var files: usize = 0;
    var status: u8 = 1;
    const multi_hint = first_file + 1 < argv.len;
    while (i <= argv.len) : (i += 1) {
        if (i == argv.len and files > 0) break;
        const name: []const u8 = if (i < argv.len) argv[i] else "-";
        var src = openSource(name, stdin, host) orelse {
            out.write("grep: ");
            out.write(name);
            out.write(": no such file\n");
            status = 2;
            files += 1;
            continue;
        };
        defer src.close();
        files += 1;
        var matches: u64 = 0;
        var lineno: u64 = 0;
        var reader = LineReader.init(src.stream);
        while (reader.next()) |line| {
            lineno += 1;
            const hit = if (ignore_case) containsIgnoreCase(line, pattern) else std.mem.indexOf(u8, line, pattern) != null;
            const selected = hit != invert;
            if (!selected) continue;
            matches += 1;
            status = 0;
            if (count_only) continue;
            if (multi_hint or files > 1) {
                out.write(name);
                out.write(":");
            }
            if (show_num) {
                writeUint(out, lineno);
                out.write(":");
            }
            out.write(line);
            out.write("\n");
        }
        if (count_only) {
            if (multi_hint or files > 1) {
                out.write(name);
                out.write(":");
            }
            writeUint(out, matches);
            out.write("\n");
        }
        if (i == argv.len) break;
    }
    return status;
}

fn containsIgnoreCase(hay: []const u8, needle: []const u8) bool {
    if (needle.len == 0) return true;
    if (needle.len > hay.len) return false;
    var i: usize = 0;
    while (i + needle.len <= hay.len) : (i += 1) {
        if (eqlIgnoreCase(hay[i .. i + needle.len], needle)) return true;
    }
    return false;
}

fn lessNumeric(a: []const u8, b: []const u8) bool {
    return parseNumPrefix(a) < parseNumPrefix(b);
}

fn parseNumPrefix(s: []const u8) i64 {
    var i: usize = 0;
    while (i < s.len and (s[i] == ' ' or s[i] == '\t')) i += 1;
    var neg = false;
    if (i < s.len and (s[i] == '-' or s[i] == '+')) {
        neg = s[i] == '-';
        i += 1;
    }
    var v: i64 = 0;
    while (i < s.len and s[i] >= '0' and s[i] <= '9') : (i += 1) v = v * 10 + (s[i] - '0');
    return if (neg) -v else v;
}

/// `sort [-r] [-n] [-u] [FILE...]` — bounded buffer, line-based.
fn runSort(argv: []const []const u8, stdin: Stream, host: Host, out: Writer) u8 {
    var reverse = false;
    var numeric = false;
    var unique = false;
    var first_file: usize = 1;
    while (first_file < argv.len and argv[first_file].len > 1 and argv[first_file][0] == '-') : (first_file += 1) {
        for (argv[first_file][1..]) |flag| switch (flag) {
            'r' => reverse = true,
            'n' => numeric = true,
            'u' => unique = true,
            else => {},
        };
    }
    var count: usize = 0;
    var used: usize = 0;
    var status: u8 = 0;
    var files: usize = 0;
    var fi = first_file;
    while (fi <= argv.len) : (fi += 1) {
        if (fi == argv.len and files > 0) break;
        const name: []const u8 = if (fi < argv.len) argv[fi] else "-";
        var src = openSource(name, stdin, host) orelse {
            out.write("sort: ");
            out.write(name);
            out.write(": no such file\n");
            status = 1;
            files += 1;
            continue;
        };
        defer src.close();
        files += 1;
        var reader = LineReader.init(src.stream);
        while (reader.next()) |line| {
            if (count >= sort_lens.len or used + line.len > sort_store.len) break;
            sort_offsets[count] = used;
            sort_lens[count] = line.len;
            @memcpy(sort_store[used..][0..line.len], line);
            used += line.len;
            count += 1;
        }
        if (fi == argv.len) break;
    }
    // Insertion sort (bounded 256 lines; fine).
    var a: usize = 1;
    while (a < count) : (a += 1) {
        const key_off = sort_offsets[a];
        const key_len = sort_lens[a];
        var key_buf: [line_cap]u8 = undefined;
        @memcpy(key_buf[0..key_len], sort_store[key_off..][0..key_len]);
        var b: usize = a;
        while (b > 0) {
            const prev = sort_store[sort_offsets[b - 1]..][0..sort_lens[b - 1]];
            const before = if (numeric) lessNumeric(key_buf[0..key_len], prev) else std.mem.order(u8, key_buf[0..key_len], prev) == .lt;
            if (!before) break;
            sort_offsets[b] = sort_offsets[b - 1];
            sort_lens[b] = sort_lens[b - 1];
            b -= 1;
        }
        sort_offsets[b] = key_off;
        sort_lens[b] = key_len;
    }
    var last: ?[]const u8 = null;
    var i: usize = 0;
    while (i < count) : (i += 1) {
        const j = if (reverse) count - 1 - i else i;
        const line = sort_store[sort_offsets[j]..][0..sort_lens[j]];
        if (unique) {
            if (last) |prev| {
                if (std.mem.eql(u8, prev, line)) continue;
            }
            last = line;
        }
        out.write(line);
        out.write("\n");
    }
    return status;
}

/// `cut -d C -f LIST [-s] [FILE...]`
fn runCut(argv: []const []const u8, stdin: Stream, host: Host, out: Writer) u8 {
    var delim: u8 = '\t';
    var suppress = false;
    var list: []const u8 = "";
    var first_file: usize = 1;
    var i: usize = 1;
    while (i < argv.len and argv[i].len >= 2 and argv[i][0] == '-') : (i += 1) {
        switch (argv[i][1]) {
            'd' => {
                if (argv[i].len >= 3) {
                    delim = argv[i][2];
                } else if (i + 1 < argv.len) {
                    i += 1;
                    delim = argv[i][0];
                } else {
                    out.write("cut: usage: cut -d C -f LIST [FILE...]\n");
                    return 2;
                }
            },
            'f' => {
                if (argv[i].len >= 3) {
                    list = argv[i][2..];
                } else if (i + 1 < argv.len) {
                    i += 1;
                    list = argv[i];
                } else {
                    out.write("cut: usage: cut -d C -f LIST [FILE...]\n");
                    return 2;
                }
            },
            's' => suppress = true,
            else => {
                out.write("cut: usage: cut -d C -f LIST [FILE...]\n");
                return 2;
            },
        }
    }
    if (list.len == 0) {
        out.write("cut: usage: cut -d C -f LIST [FILE...]\n");
        return 2;
    }
    first_file = i;
    var status: u8 = 0;
    var files: usize = 0;
    var fi = first_file;
    while (fi <= argv.len) : (fi += 1) {
        if (fi == argv.len and files > 0) break;
        const name: []const u8 = if (fi < argv.len) argv[fi] else "-";
        var src = openSource(name, stdin, host) orelse {
            out.write("cut: ");
            out.write(name);
            out.write(": no such file\n");
            status = 1;
            files += 1;
            continue;
        };
        defer src.close();
        files += 1;
        var reader = LineReader.init(src.stream);
        while (reader.next()) |line| {
            if (std.mem.indexOfScalar(u8, line, delim) == null) {
                if (suppress) continue;
                out.write(line);
                out.write("\n");
                continue;
            }
            var field_no: usize = 0;
            var start: usize = 0;
            var first_out = true;
            var j: usize = 0;
            while (j <= line.len) : (j += 1) {
                const at_end = j == line.len;
                if (at_end or line[j] == delim) {
                    field_no += 1;
                    if (fieldSelected(list, field_no)) {
                        if (!first_out) out.write(&[_]u8{delim});
                        out.write(line[start..j]);
                        first_out = false;
                    }
                    start = j + 1;
                }
            }
            out.write("\n");
        }
        if (fi == argv.len) break;
    }
    return status;
}

fn fieldSelected(list: []const u8, n: usize) bool {
    var i: usize = 0;
    while (i < list.len) {
        var j = i;
        while (j < list.len and list[j] != ',') j += 1;
        const item = list[i..j];
        i = j + 1;
        if (item.len == 0) continue;
        if (std.mem.indexOfScalar(u8, item, '-')) |dash| {
            const lo_s = item[0..dash];
            const hi_s = item[dash + 1 ..];
            const lo: u64 = if (lo_s.len == 0) 1 else (parseUint(lo_s) orelse continue);
            const hi: u64 = if (hi_s.len == 0) 0xffff_ffff else (parseUint(hi_s) orelse continue);
            if (n >= lo and n <= hi) return true;
        } else {
            const v = parseUint(item) orelse continue;
            if (v == n) return true;
        }
    }
    return false;
}

/// `test EXPR...` / `[ EXPR... ]`
fn runTest(tool: Tool, argv: []const []const u8, host: Host, out: Writer) u8 {
    var args = argv[1..];
    if (tool == .bracket) {
        if (args.len == 0 or !std.mem.eql(u8, args[args.len - 1], "]")) {
            out.write("[: missing ']'\n");
            return 2;
        }
        args = args[0 .. args.len - 1];
    }
    if (args.len == 0) return 1;
    var negate = false;
    if (std.mem.eql(u8, args[0], "!")) {
        negate = true;
        args = args[1..];
        if (args.len == 0) return 1;
    }
    const result = testEval(args, host);
    if (result == null) {
        out.write("test: bad expression\n");
        return 2;
    }
    const truth = result.? != negate;
    return if (truth) 0 else 1;
}

fn testEval(args: []const []const u8, host: Host) ?bool {
    if (args.len == 1) return args[0].len > 0;
    if (args.len == 2) {
        const op = args[0];
        const val = args[1];
        if (std.mem.eql(u8, op, "-z")) return val.len == 0;
        if (std.mem.eql(u8, op, "-n")) return val.len > 0;
        if (std.mem.eql(u8, op, "-e")) return host.stat(val) != 0;
        if (std.mem.eql(u8, op, "-f")) return host.stat(val) == 1;
        if (std.mem.eql(u8, op, "-d")) return host.stat(val) == 2;
        return null;
    }
    if (args.len == 3) {
        const a = args[0];
        const op = args[1];
        const b = args[2];
        if (std.mem.eql(u8, op, "=") or std.mem.eql(u8, op, "==")) return std.mem.eql(u8, a, b);
        if (std.mem.eql(u8, op, "!=")) return !std.mem.eql(u8, a, b);
        const x = parseNumPrefix(a);
        const y = parseNumPrefix(b);
        if (std.mem.eql(u8, op, "-eq")) return x == y;
        if (std.mem.eql(u8, op, "-ne")) return x != y;
        if (std.mem.eql(u8, op, "-lt")) return x < y;
        if (std.mem.eql(u8, op, "-le")) return x <= y;
        if (std.mem.eql(u8, op, "-gt")) return x > y;
        if (std.mem.eql(u8, op, "-ge")) return x >= y;
        return null;
    }
    return null;
}

/// `printf FORMAT [ARG...]`
fn runPrintf(argv: []const []const u8, out: Writer) u8 {
    if (argv.len < 2) return 0;
    const fmt = argv[1];
    var argi: usize = 2;
    while (true) {
        const before = argi;
        var i: usize = 0;
        while (i < fmt.len) {
            if (fmt[i] != '%') {
                if (fmt[i] == '\\' and i + 1 < fmt.len) {
                    i += 1;
                    switch (fmt[i]) {
                        'n' => out.write("\n"),
                        't' => out.write("\t"),
                        'r' => out.write("\r"),
                        // `printf '\e[31m'` is the bounded way a
                        // window-tty program can emit an ANSI CSI sequence
                        // without a special syscall.
                        'e' => out.write("\x1b"),
                        // M73a-2 (#1631): `\xHH` emits one raw byte — the
                        // only ASCII-typable way to get UTF-8 rune bytes
                        // (box frames, accents, CJK) onto a bound tty,
                        // since the gate runner types keyboard ASCII.
                        'x' => {
                            const hi = if (i + 1 < fmt.len) hexNibble(fmt[i + 1]) else null;
                            const lo = if (i + 2 < fmt.len) hexNibble(fmt[i + 2]) else null;
                            if (hi != null and lo != null) {
                                out.write(&[_]u8{(hi.? << 4) | lo.?});
                                i += 2;
                            } else {
                                // Not a hex pair: emit `\x` literally.
                                out.write(&[_]u8{ '\\', 'x' });
                            }
                        },
                        '\\' => out.write("\\"),
                        '0' => out.write(&[_]u8{0}),
                        else => {
                            out.write(&[_]u8{'\\'});
                            out.write(fmt[i .. i + 1]);
                        },
                    }
                } else {
                    out.write(fmt[i .. i + 1]);
                }
                i += 1;
                continue;
            }
            if (i + 1 < fmt.len and fmt[i + 1] == '%') {
                out.write("%");
                i += 2;
                continue;
            }
            // A conversion: find its type byte (skip width/flag chars).
            var j = i + 1;
            while (j < fmt.len and (fmt[j] == '-' or fmt[j] == '+' or fmt[j] == ' ' or
                fmt[j] == '#' or fmt[j] == '0' or (fmt[j] >= '0' and fmt[j] <= '9'))) j += 1;
            if (j >= fmt.len) {
                out.write(fmt[i..]);
                break;
            }
            const conv = fmt[j];
            const arg = if (argi < argv.len) argv[argi] else "";
            switch (conv) {
                's' => {
                    out.write(arg);
                    argi += 1;
                },
                'd', 'i' => {
                    writeSigned(out, arg);
                    argi += 1;
                },
                'u' => {
                    writeUint(out, parseUint(arg) orelse 0);
                    argi += 1;
                },
                'x', 'X' => {
                    var buf: [16]u8 = undefined;
                    const v = parseUint(arg) orelse 0;
                    const s = std.fmt.bufPrint(&buf, "{x}", .{v}) catch "0";
                    for (s) |c| out.write(&[_]u8{if (conv == 'X') std.ascii.toUpper(c) else c});
                    argi += 1;
                },
                'c' => {
                    if (arg.len > 0) out.write(arg[0..1]);
                    argi += 1;
                },
                else => {
                    // Unknown conversion: print it literally.
                    out.write(fmt[i .. j + 1]);
                },
            }
            i = j + 1;
        }
        // POSIX printf repeats the format while arguments remain; a pass
        // that consumed nothing stops (otherwise the loop could not end).
        if (argi >= argv.len or argi == before) break;
    }
    return 0;
}

fn hexNibble(c: u8) ?u8 {
    return switch (c) {
        '0'...'9' => c - '0',
        'a'...'f' => c - 'a' + 10,
        'A'...'F' => c - 'A' + 10,
        else => null,
    };
}

fn writeSigned(out: Writer, s: []const u8) void {
    const v = parseNumPrefix(s);
    if (v < 0) out.write("-");
    const mag: u64 = if (v < 0) @intCast(-v) else @intCast(v);
    writeUint(out, mag);
}

// ---------------------------------------------------------------------------
// Dispatch
// ---------------------------------------------------------------------------

/// Run `t` with `argv` (argv[0] is the typed verb), reading stdin from
/// `stdin`, host-share files through `host`, and writing stdout to `out`.
/// Returns the process exit status.
pub fn run(t: Tool, argv: []const []const u8, stdin: Stream, host: Host, out: Writer) u8 {
    return switch (t) {
        .head => runHead(argv, stdin, host, out),
        .tail => runTail(argv, stdin, host, out),
        .wc => runWc(argv, stdin, host, out),
        .grep => runGrep(argv, stdin, host, out),
        .sort => runSort(argv, stdin, host, out),
        .cut => runCut(argv, stdin, host, out),
        .test_ => runTest(.test_, argv, host, out),
        .bracket => runTest(.bracket, argv, host, out),
        .printf => runPrintf(argv, out),
    };
}

// ---------------------------------------------------------------------------
// Host tests (memory files + capture output; no syscalls)
// ---------------------------------------------------------------------------

const Mem = struct {
    var data: []const u8 = "";
    var present: bool = false;
    var is_dir: bool = false;
    var pos: usize = 0;
    /// The fixture answers only for this name (so `-e NOPE` is honestly
    /// absent).
    var name: []const u8 = "FILE.TXT";

    fn open(ctx: ?*anyopaque, requested: []const u8) u64 {
        _ = ctx;
        if (!std.mem.eql(u8, requested, name)) return 0;
        pos = 0;
        return if (present) 1 else 0;
    }

    fn read(ctx: ?*anyopaque, handle: u64, buf: []u8) usize {
        _ = ctx;
        _ = handle;
        const n = @min(buf.len, data.len - pos);
        @memcpy(buf[0..n], data[pos..][0..n]);
        pos += n;
        return n;
    }

    fn close(ctx: ?*anyopaque, handle: u64) void {
        _ = ctx;
        _ = handle;
    }

    fn stat(ctx: ?*anyopaque, requested: []const u8) u8 {
        _ = ctx;
        if (!std.mem.eql(u8, requested, name) or !present) return 0;
        return if (is_dir) 2 else 1;
    }

    fn host() Host {
        return .{ .open_fn = open, .read_fn = read, .close_fn = close, .stat_fn = stat };
    }
};

fn sliceStream(bytes: []const u8) Stream {
    const S = struct {
        fn read(ctx: ?*anyopaque, buf: []u8) usize {
            _ = ctx;
            if (test_pos >= test_stdin.len) return 0;
            const n = @min(buf.len, test_stdin.len - test_pos);
            @memcpy(buf[0..n], test_stdin[test_pos..][0..n]);
            test_pos += n;
            return n;
        }
    };
    test_stdin = bytes;
    test_pos = 0;
    return .{ .ctx = null, .read_fn = S.read };
}

var test_stdin: []const u8 = "";
var test_pos: usize = 0;

const Capture = struct {
    buf: [16384]u8 = undefined,
    len: usize = 0,

    fn sink(ctx: ?*anyopaque, bytes: []const u8) void {
        const self: *Capture = @ptrCast(@alignCast(ctx.?));
        const n = @min(bytes.len, self.buf.len - self.len);
        @memcpy(self.buf[self.len..][0..n], bytes[0..n]);
        self.len += n;
    }

    fn writer(self: *Capture) Writer {
        return .{ .ctx = self, .write_fn = sink };
    }

    fn contents(self: *const Capture) []const u8 {
        return self.buf[0..self.len];
    }

    fn reset(self: *Capture) void {
        self.len = 0;
    }
};

fn stdinStream(text: []const u8) Stream {
    return sliceStream(text);
}

fn runText(t: Tool, argv: []const []const u8, stdin_text: []const u8, cap: *Capture) u8 {
    const st = stdinStream(stdin_text);
    return run(t, argv, st, Mem.host(), cap.writer());
}

test "toolbox: lookup finds the names and rejects others" {
    try std.testing.expectEqual(Tool.head, lookup("head").?);
    try std.testing.expectEqual(Tool.wc, lookup("wc").?);
    try std.testing.expectEqual(Tool.bracket, lookup("[").?);
    try std.testing.expectEqual(Tool.printf, lookup("printf").?);
    try std.testing.expect(lookup("bogus") == null);
}

test "toolbox: head streams the first N lines (default 10)" {
    var cap = Capture{};
    var many: [256]u8 = undefined;
    var n: usize = 0;
    var i: usize = 0;
    while (i < 12) : (i += 1) {
        const line = std.fmt.bufPrint(many[n..], "l{d}\n", .{i}) catch unreachable;
        n += line.len;
    }
    const argv = [_][]const u8{ "head", "-n", "3" };
    const st = runText(.head, &argv, many[0..n], &cap);
    try std.testing.expectEqual(@as(u8, 0), st);
    try std.testing.expectEqualStrings("l0\nl1\nl2\n", cap.contents());
    cap.reset();
    const argv2 = [_][]const u8{"head"};
    _ = runText(.head, &argv2, many[0..n], &cap);
    try std.testing.expect(std.mem.startsWith(u8, cap.contents(), "l0\nl1\n"));
    try std.testing.expect(std.mem.indexOf(u8, cap.contents(), "l10") == null);
}

test "toolbox: head reads a host-share file and reports a missing one" {
    var cap = Capture{};
    Mem.present = true;
    Mem.data = "alpha\nbeta\n";
    defer Mem.present = false;
    const argv = [_][]const u8{ "head", "-n", "1", "FILE.TXT" };
    const st = run(.head, &argv, stdinStream(""), Mem.host(), cap.writer());
    try std.testing.expectEqual(@as(u8, 0), st);
    try std.testing.expectEqualStrings("alpha\n", cap.contents());
    cap.reset();
    Mem.present = false;
    const argv2 = [_][]const u8{ "head", "NOPE.TXT" };
    const st2 = run(.head, &argv2, stdinStream(""), Mem.host(), cap.writer());
    try std.testing.expectEqual(@as(u8, 1), st2);
    try std.testing.expect(std.mem.indexOf(u8, cap.contents(), "no such file") != null);
}

test "toolbox: tail keeps the last N lines" {
    var cap = Capture{};
    const argv = [_][]const u8{ "tail", "-n", "2" };
    const st = runText(.tail, &argv, "a\nb\nc\nd\n", &cap);
    try std.testing.expectEqual(@as(u8, 0), st);
    try std.testing.expectEqualStrings("c\nd\n", cap.contents());
}

test "toolbox: wc counts lines words bytes and flags select columns" {
    var cap = Capture{};
    const argv = [_][]const u8{"wc"};
    const st = runText(.wc, &argv, "one two\nthree\n", &cap);
    try std.testing.expectEqual(@as(u8, 0), st);
    try std.testing.expectEqualStrings("2 3 14\n", cap.contents());
    cap.reset();
    const argv_l = [_][]const u8{ "wc", "-l" };
    _ = runText(.wc, &argv_l, "one two\nthree\n", &cap);
    try std.testing.expectEqualStrings("2\n", cap.contents());
    cap.reset();
    const argv_c = [_][]const u8{ "wc", "-c" };
    _ = runText(.wc, &argv_c, "one two\nthree\n", &cap);
    try std.testing.expectEqualStrings("14\n", cap.contents());
}

test "toolbox: grep selects, inverts, counts, numbers, and ignores case" {
    var cap = Capture{};
    const argv = [_][]const u8{ "grep", "b" };
    const st = runText(.grep, &argv, "alpha\nbeta\ngamma\n", &cap);
    try std.testing.expectEqual(@as(u8, 0), st);
    try std.testing.expectEqualStrings("beta\n", cap.contents());
    cap.reset();
    const argv_v = [_][]const u8{ "grep", "-v", "e" };
    _ = runText(.grep, &argv_v, "alpha\nbeta\ngamma\n", &cap);
    // Every line contains 'e' except alpha and gamma.
    try std.testing.expectEqualStrings("alpha\ngamma\n", cap.contents());
    cap.reset();
    const argv_ic = [_][]const u8{ "grep", "-ic", "ALPHA" };
    _ = runText(.grep, &argv_ic, "alpha\nALPHA\nbeta\n", &cap);
    try std.testing.expectEqualStrings("2\n", cap.contents());
    cap.reset();
    const argv_n = [_][]const u8{ "grep", "-n", "e" };
    _ = runText(.grep, &argv_n, "alpha\nbeta\ngamma\n", &cap);
    try std.testing.expectEqualStrings("2:beta\n", cap.contents());
}

test "toolbox: grep exits 1 when nothing matched and 2 on usage" {
    var cap = Capture{};
    const argv = [_][]const u8{ "grep", "zzz" };
    try std.testing.expectEqual(@as(u8, 1), runText(.grep, &argv, "alpha\n", &cap));
    cap.reset();
    const argv_bad = [_][]const u8{"grep"};
    try std.testing.expectEqual(@as(u8, 2), runText(.grep, &argv_bad, "", &cap));
}

test "toolbox: sort sorts, reverses, numeric-sorts and uniques" {
    var cap = Capture{};
    const argv = [_][]const u8{"sort"};
    _ = runText(.sort, &argv, "gamma\nalpha\nbeta\n", &cap);
    try std.testing.expectEqualStrings("alpha\nbeta\ngamma\n", cap.contents());
    cap.reset();
    const argv_r = [_][]const u8{ "sort", "-r" };
    _ = runText(.sort, &argv_r, "gamma\nalpha\nbeta\n", &cap);
    try std.testing.expectEqualStrings("gamma\nbeta\nalpha\n", cap.contents());
    cap.reset();
    const argv_n = [_][]const u8{ "sort", "-n" };
    _ = runText(.sort, &argv_n, "10\n2\n30\n", &cap);
    try std.testing.expectEqualStrings("2\n10\n30\n", cap.contents());
    cap.reset();
    const argv_u = [_][]const u8{ "sort", "-u" };
    _ = runText(.sort, &argv_u, "b\na\nb\na\n", &cap);
    try std.testing.expectEqualStrings("a\nb\n", cap.contents());
}

test "toolbox: cut selects fields and suppresses delimiterless lines" {
    var cap = Capture{};
    const argv = [_][]const u8{ "cut", "-d", ":", "-f", "1,3" };
    _ = runText(.cut, &argv, "a:b:c\nd:e:f\nplain\n", &cap);
    try std.testing.expectEqualStrings("a:c\nd:f\nplain\n", cap.contents());
    cap.reset();
    const argv_s = [_][]const u8{ "cut", "-d:", "-f2", "-s" };
    _ = runText(.cut, &argv_s, "a:b:c\nplain\n", &cap);
    try std.testing.expectEqualStrings("b\n", cap.contents());
    cap.reset();
    const argv_r = [_][]const u8{ "cut", "-d", ":", "-f", "2-3" };
    _ = runText(.cut, &argv_r, "a:b:c:d\n", &cap);
    try std.testing.expectEqualStrings("b:c\n", cap.contents());
}

test "toolbox: test evaluates the bounded expression forms" {
    const host = Mem.host();
    Mem.present = true;
    Mem.data = "";
    Mem.is_dir = false;
    defer Mem.present = false;
    const t = struct {
        fn check(args: []const []const u8, want: u8) !void {
            var cap = Capture{};
            const st = run(.test_, args, stdinStream(""), Mem.host(), cap.writer());
            try std.testing.expectEqual(want, st);
        }
    };
    try t.check(&.{ "test", "-n", "x" }, 0);
    try t.check(&.{ "test", "-z", "x" }, 1);
    try t.check(&.{ "test", "a", "=", "a" }, 0);
    try t.check(&.{ "test", "a", "!=", "b" }, 0);
    try t.check(&.{ "test", "2", "-lt", "10" }, 0);
    try t.check(&.{ "test", "10", "-le", "2" }, 1);
    try t.check(&.{ "test", "!", "-z", "" }, 1);
    try t.check(&.{ "test", "!", "-z", "x" }, 0);
    try t.check(&.{ "test", "-f", "FILE.TXT" }, 0);
    try t.check(&.{ "test", "-d", "FILE.TXT" }, 1);
    try t.check(&.{ "test", "-e", "NOPE" }, 1);
    _ = host;
}

test "toolbox: bracket requires the closing bracket" {
    var cap = Capture{};
    const ok = [_][]const u8{ "[", "a", "=", "a", "]" };
    try std.testing.expectEqual(@as(u8, 0), runText(.bracket, &ok, "", &cap));
    cap.reset();
    const bad = [_][]const u8{ "[", "a", "=", "a" };
    try std.testing.expectEqual(@as(u8, 2), runText(.bracket, &bad, "", &cap));
    try std.testing.expect(std.mem.indexOf(u8, cap.contents(), "missing") != null);
}

test "toolbox: printf substitutes, escapes, and repeats the format" {
    var cap = Capture{};
    const argv = [_][]const u8{ "printf", "%s=%d\\n", "a", "1" };
    const st = run(.printf, &argv, stdinStream(""), Mem.host(), cap.writer());
    try std.testing.expectEqual(@as(u8, 0), st);
    try std.testing.expectEqualStrings("a=1\n", cap.contents());
    cap.reset();
    // Remaining arguments repeat the format (POSIX).
    const argv2 = [_][]const u8{ "printf", "%s\\n", "a", "b", "c" };
    _ = run(.printf, &argv2, stdinStream(""), Mem.host(), cap.writer());
    try std.testing.expectEqualStrings("a\nb\nc\n", cap.contents());
    cap.reset();
    const argv3 = [_][]const u8{ "printf", "%x!", "255" };
    _ = run(.printf, &argv3, stdinStream(""), Mem.host(), cap.writer());
    try std.testing.expectEqualStrings("ff!", cap.contents());
    cap.reset();
    const argv4 = [_][]const u8{ "printf", "100%%\n" };
    _ = run(.printf, &argv4, stdinStream(""), Mem.host(), cap.writer());
    try std.testing.expectEqualStrings("100%\n", cap.contents());
    cap.reset();
    const argv5 = [_][]const u8{ "printf", "\\e[31mred\\e[0m" };
    _ = run(.printf, &argv5, stdinStream(""), Mem.host(), cap.writer());
    try std.testing.expectEqualStrings("\x1b[31mred\x1b[0m", cap.contents());
}

test "toolbox: printf \\xHH emits raw bytes (M73a-2 #1631)" {
    var cap = Capture{};
    // The typed line is keyboard ASCII only (the gate runner is a keyboard);
    // \xHH is how UTF-8 rune bytes reach the tty: ┌ = E2 94 8C, é = C3 A9.
    const argv = [_][]const u8{ "printf", "\\xE2\\x94\\x8C-\\xC3\\xA9\\n" };
    _ = run(.printf, &argv, stdinStream(""), Mem.host(), cap.writer());
    try std.testing.expectEqualStrings("\xe2\x94\x8c-\xc3\xa9\n", cap.contents());
    cap.reset();
    // A bad or truncated pair is literal, never a swallowed tail.
    const argv2 = [_][]const u8{ "printf", "a\\xZZb\\x4" };
    _ = run(.printf, &argv2, stdinStream(""), Mem.host(), cap.writer());
    try std.testing.expectEqualStrings("a\\xZZb\\x4", cap.contents());
}

test "toolbox: a line longer than the cap is consumed but truncated" {
    var cap = Capture{};
    var long: [700]u8 = undefined;
    @memset(&long, 'x');
    long[699] = '\n';
    const argv = [_][]const u8{ "wc", "-c" };
    _ = runText(.wc, &argv, &long, &cap);
    // The engine only reports the capped line: 512 data bytes plus the
    // newline it actually consumed (the dropped tail bytes are not counted).
    try std.testing.expectEqualStrings("513\n", cap.contents());
}
