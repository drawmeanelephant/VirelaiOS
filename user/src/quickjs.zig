//! census (M188e, #3190): QJS.BIN — gate-referenced: live-quickjs; built by tools/js-client/build.py, not build.zig.
//! ADR 0039's sole product adapter. JS owns no native handles.
const std = @import("std");
const sdk = @import("sdk").runtime;
const qjs = if (@import("build_options").engine) @import("qjs") else struct {};
const policy = @import("product_policy");
pub const os = sdk.os;
pub const std_options = sdk.std_options;
pub const panic = std.debug.FullPanic(sdk.panic);
pub const virelai = sdk.platform;
pub const std_options_debug_io = sdk.std_options_debug_io;
pub const std_options_FilePermissions = sdk.std_options_FilePermissions;
pub const std_options_cwd = sdk.std_options_cwd;

/// QJS.BIN's product version — the `ver M.m.p` the component-registry
/// row shows and the drift check resolves
/// (docs/component-registry.md). The registry's bump rule says when it
/// moves.
pub const version = "1.0.0";

comptime {
    @import("sdk").entry.exportEntry(dispatch);
}

fn diagnosticChunk(bytes: []const u8) i64 {
    return sdk.native.call(1, .{ 2, @intFromPtr(bytes.ptr), bytes.len, 0, 0, 0 });
}

fn fail(name: []const u8, status: u8) noreturn {
    sdk.console.writeAll(diagnosticChunk, name) catch {};
    sdk.console.writeAll(diagnosticChunk, "\n") catch {};
    sdk.native.exit(status);
}

fn dispatch(args: *const sdk.startup.Startup) !void {
    run(args) catch |err| fail(@errorName(err), 70);
}

const Session = struct {
    runtime: *qjs.Runtime,
    receipt: ?std.Io.File = null,
    tty: ?u64 = null,
    attached: bool = false,
    queue: []u8,
    queued: usize = 0,
    queue_read: usize = 0,
    queue_overflow: bool = false,
    drain_pending: bool = false,
    io_failure: ?anyerror = null,
    first_output: u64 = 0,
    cancel_counter: u64 = 0,
    peak_files: usize = 0,
    peak_resources: usize = 4,
    metadata: [768]u8 = undefined,
    // M211a (#3867): the --dom-rpc worker's per-eval reply capture — the
    // output a script's print()/completion emits, bounded and clipped.
    rpc_out: []u8 = &.{},
    rpc_out_len: usize = 0,
    rpc_clipped: bool = false,
    // M211b (#3868): the v2 DOM half of the wire. A script's DOM ask
    // becomes a request line on fd-1 (`rpc_seq` correlates the answer);
    // `rpc_ops` is the per-script op count the `script_budget_ops`
    // refusal names; `rpc_ans` is the rendezvous read scratch.
    rpc_seq: u64 = 0,
    rpc_ops: u32 = 0,
    rpc_ans: []u8 = &.{},
    dom_ready: bool = false,
    dom_failure: [64:0]u8 = undefined,

    fn resources(self: *Session) !void {
        const files = sdk.filesystem().liveFiles() + @intFromBool(self.tty != null);
        const records = sdk.filesystem().liveResources() + @intFromBool(self.tty != null);
        try self.runtime.checkResources(files, records);
        self.peak_files = @max(self.peak_files, files);
        self.peak_resources = @max(self.peak_resources, records);
    }

    fn mirror(self: *Session, kind: u8, bytes: []const u8) !void {
        if (self.receipt) |file| {
            var header: [40]u8 = undefined;
            const prefix = try std.fmt.bufPrint(&header, "{c} {d}\n", .{ kind, bytes.len });
            try self.runtime.chargeReceipt(prefix.len + bytes.len);
            try file.writeStreamingAll(sdk.io, prefix);
            try file.writeStreamingAll(sdk.io, bytes);
        }
    }

    fn write(context: *anyopaque, bytes: []const u8) !usize {
        const self: *Session = @ptrCast(@alignCast(context));
        if (self.io_failure != null) return error.WriteFailed;
        const count = sdk.native.consoleChunk(bytes);
        if (count <= 0 or count > bytes.len) return error.WriteFailed;
        const confirmed: usize = @intCast(count);
        if (self.first_output == 0)
            self.first_output = self.runtime.control.clock.counter(self.runtime.control.clock.context);
        self.mirror('O', bytes[0..confirmed]) catch |err| {
            self.io_failure = err;
            return err;
        };
        return confirmed;
    }

    fn diagnostic(context: *anyopaque, bytes: []const u8) !void {
        const self: *Session = @ptrCast(@alignCast(context));
        try sdk.console.writeAll(diagnosticChunk, bytes);
        self.mirror('D', bytes) catch |err| {
            self.io_failure = err;
            return err;
        };
    }

    fn notice(self: *Session, bytes: []const u8) !void {
        try self.runtime.control.chargeDiagnostic(bytes.len);
        try diagnostic(self, bytes);
    }

    fn terminal(self: *Session, bytes: []const u8) !void {
        if (bytes.len > qjs.limits.terminal_diagnostic or
            bytes.len > qjs.limits.diagnostics - self.runtime.control.diagnostic_bytes)
            return error.DiagnosticLimit;
        self.runtime.control.diagnostic_bytes += bytes.len;
        try diagnostic(self, bytes);
    }

    fn sink(self: *Session) qjs.Sink {
        return .{ .context = self, .write = write, .diagnostic = diagnostic, .poll = poll };
    }

    fn poll(context: *anyopaque) bool {
        const self: *Session = @ptrCast(@alignCast(context));
        const fd = self.tty orelse return false;
        var bytes: [128]u8 = undefined;
        const n = sdk.native.call(24, .{ fd, @intFromPtr(&bytes), bytes.len, 0, 0, 0 });
        if (n < 0 or n > bytes.len) {
            self.io_failure = error.ReadFailed;
            self.runtime.control.refuse(error.WriteFailed);
            return false;
        }
        var cancel = false;
        for (bytes[0..@intCast(n)]) |byte| {
            if (byte == 3 and self.runtime.control.running) {
                if (self.cancel_counter == 0)
                    self.cancel_counter = self.runtime.control.clock.counter(self.runtime.control.clock.context);
                cancel = true;
            } else if (self.drain_pending) {
                if (byte == '\n' or byte == '\r') self.drain_pending = false;
            } else if (self.queued < self.queue.len) {
                self.queue[self.queued] = byte;
                self.queued += 1;
            } else {
                self.queue_overflow = true;
                self.queued = 0;
                self.queue_read = 0;
                // Discard the queued prefix, never evaluate a clipped line.
                // If this byte terminates the oversized line, preserve later
                // lines in subsequent reads instead of dropping them forever.
                self.drain_pending = byte != '\n' and byte != '\r';
                self.runtime.control.refuse(error.LineLimit);
            }
        }
        return cancel;
    }

    fn evaluate(self: *Session, source: []const u8, filename: []const u8, interactive: bool) !bool {
        if (self.queue_read != 0) {
            std.mem.copyForwards(u8, self.queue[0 .. self.queued - self.queue_read], self.queue[self.queue_read..self.queued]);
            self.queued -= self.queue_read;
            self.queue_read = 0;
        }
        self.first_output = 0;
        self.cancel_counter = 0;
        var kept: usize = 0;
        for (self.queue[0..self.queued]) |byte| {
            if (byte == 3) {
                self.cancel_counter = self.runtime.control.clock.counter(self.runtime.control.clock.context);
                self.runtime.cancel();
            } else {
                self.queue[kept] = byte;
                kept += 1;
            }
        }
        self.queued = kept;
        const result = try self.runtime.eval(source, filename, self.sink(), interactive);
        // The poll discarded the oversized prefix and preserved only bytes
        // after its terminator. Refusal already reset the context.
        self.queue_overflow = false;
        const stack = sdk.stackHighWater();
        if (stack > qjs.limits.stack) return error.StackLimit;
        const row = try std.fmt.bufPrint(&self.metadata, "eval={d} failure={s} reset={d} output={d} arena_peak={d} stack={d} c_stack={d} sites={d} gap={d} frequency={d} start={d} end={d} first={d} files={d} resources={d}\n", .{ self.runtime.control.evals, if (result.failure) |err| @errorName(err) else "none", @intFromBool(result.context_reset), result.output_bytes, result.arena_peak_bytes, stack, result.c_stack_high_water, result.work_sites, result.maximum_unchecked_ticks, result.counter_frequency, result.start_counter, result.end_counter, self.first_output, self.peak_files, self.peak_resources });
        try self.mirror('R', row);
        if (result.failure) |failure| {
            var message: [96]u8 = undefined;
            try self.notice(try std.fmt.bufPrint(&message, "qjs: {s}\n", .{@errorName(failure)}));
            if (result.context_reset) try self.notice("qjs: ContextReset\n");
        }
        const completion = self.runtime.control.clock.counter(self.runtime.control.clock.context);
        var timing: [160]u8 = undefined;
        try self.mirror('T', try std.fmt.bufPrint(&timing, "eval={d} completion={d} cancel={d}\n", .{ self.runtime.control.evals, completion, self.cancel_counter }));
        if (self.io_failure) |err| return err;
        return result.failure == null;
    }

    fn openReceipt(self: *Session, name: []const u8) !void {
        try policy.path(name);
        const slash = std.mem.lastIndexOfScalar(u8, name, '/').?;
        const parent = if (slash == 5) "" else name[6..slash];
        const directory = try sdk.filesystem().openDirectory("/host", parent);
        var pinned = true;
        defer if (pinned) sdk.filesystem().closeDirectory(directory) catch {};
        try self.resources();
        self.receipt = sdk.filesystem().createExclusive(directory, name[slash + 1 ..]) catch |err|
            return if (err == error.PathAlreadyExists) error.ReceiptExists else err;
        try self.resources();
        try sdk.filesystem().closeDirectory(directory);
        pinned = false;
        try self.mirror('H', "QJS/1\n");
    }

    fn close(self: *Session) !void {
        var failure: ?anyerror = null;
        if (self.attached) {
            if (sdk.native.call(67, .{ 0, 0, 0, 0, 0, 0 }) != 0) failure = error.DetachFailed;
            self.attached = false;
        }
        if (self.tty) |fd| {
            if (sdk.native.call(26, .{ fd, 0, 0, 0, 0, 0 }) != 0) failure = error.CloseFailed;
            self.tty = null;
        }
        if (self.receipt) |file| {
            file.sync(sdk.io) catch {
                failure = error.SyncFailed;
            };
            sdk.filesystem().closeChecked(file) catch {
                failure = error.CloseFailed;
            };
            self.receipt = null;
        }
        if (failure) |err| return err;
    }

    /// ADR 0039 amendment (M200c): the JS `virelai.readFile`/`writeFile`
    /// seams. The runtime bounds path and payload; policy.path and the
    /// contained I/O below name every refusal that reaches JS verbatim.
    fn hostReadFile(context: *anyopaque, name: []const u8, out: []u8) anyerror!usize {
        const self: *Session = @ptrCast(@alignCast(context));
        try policy.path(name);
        const input = try sdk.filesystem().openContained("/host", name[6..], .read);
        var opened = true;
        defer if (opened) sdk.filesystem().closeChecked(input) catch {};
        try self.resources();
        const data = try sdk.io_helpers.readBounded(input, sdk.io, out);
        try sdk.filesystem().closeChecked(input);
        opened = false;
        try self.resources();
        return data.len;
    }

    /// writeFile creates exclusively — an existing name is PathAlreadyExists,
    /// never a silent truncate (the receipt rule applied to JS).
    fn hostWriteFile(context: *anyopaque, name: []const u8, data: []const u8) anyerror!void {
        const self: *Session = @ptrCast(@alignCast(context));
        try policy.path(name);
        const slash = std.mem.lastIndexOfScalar(u8, name, '/').?;
        const parent = if (slash == 5) "" else name[6..slash];
        const directory = try sdk.filesystem().openDirectory("/host", parent);
        var pinned = true;
        defer if (pinned) sdk.filesystem().closeDirectory(directory) catch {};
        const output = try sdk.filesystem().createExclusive(directory, name[slash + 1 ..]);
        var opened = true;
        defer if (opened) sdk.filesystem().closeChecked(output) catch {};
        try sdk.filesystem().closeDirectory(directory);
        pinned = false;
        try self.resources();
        try output.writeStreamingAll(sdk.io, data);
        try output.sync(sdk.io);
        try sdk.filesystem().closeChecked(output);
        opened = false;
        try self.resources();
    }

    fn evalFile(self: *Session, name: []const u8) !void {
        const input = try sdk.filesystem().openContained("/host", name[6..], .read);
        var opened = true;
        defer if (opened) sdk.filesystem().closeChecked(input) catch {};
        try self.resources();
        const buffer = try self.runtime.allocate(qjs.limits.source);
        defer self.runtime.release(buffer);
        const source = try sdk.io_helpers.readBounded(input, sdk.io, buffer);
        try sdk.filesystem().closeChecked(input);
        opened = false;
        try self.notice("qjs: file-ready\n");
        if (!try self.evaluate(source, name, false)) return error.EvaluationFailed;
    }

    fn repl(self: *Session) !void {
        const name = "/dev/tty";
        const fd = sdk.native.call(23, .{ @intFromPtr(name.ptr), name.len, 3, 0, 0, 0 });
        if (fd < 0 or fd >= 8) return error.TerminalUnavailable;
        self.tty = @intCast(fd);
        try self.resources();
        if (sdk.native.call(67, .{ 1, 0, 0, 0, 0, 0 }) != 0) return error.TerminalUnavailable;
        self.attached = true;
        const line = try self.runtime.allocate(qjs.limits.line);
        defer self.runtime.release(line);
        var used: usize = 0;
        var draining = false;
        var idle = self.runtime.control.clock.counter(self.runtime.control.clock.context);
        try self.ready();
        while (true) {
            if (self.queue_read == self.queued) {
                self.queue_read = 0;
                self.queued = 0;
                _ = poll(self);
                if (self.io_failure) |err| return err;
                if (self.queued == 0) {
                    try self.runtime.control.checkIdle(idle);
                    _ = sdk.native.call(2, .{ 0, 0, 0, 0, 0, 0 });
                    continue;
                }
            }
            // Poll removes Ctrl-C during active eval. At line input it is
            // read explicitly below so a partial line is discarded.
            const byte = self.queue[self.queue_read];
            self.queue_read += 1;
            idle = self.runtime.control.clock.counter(self.runtime.control.clock.context);
            if (byte == 3) {
                used = 0;
                draining = false;
                try self.notice("qjs: LineCancelled\n");
                try self.ready();
            } else if (byte == 4 and used == 0 and !draining) {
                break;
            } else if (byte == '\n' or byte == '\r') {
                if (draining or self.queue_overflow) {
                    self.queue_overflow = false;
                    self.runtime.control.recovered();
                    try self.notice("qjs: LineLimit\n");
                } else if (std.mem.eql(u8, line[0..used], ":quit")) {
                    break;
                } else if (std.mem.eql(u8, line[0..used], ":reset")) {
                    try self.runtime.reset();
                    try self.notice("qjs: ContextReset\n");
                } else if (used != 0) {
                    _ = try self.evaluate(line[0..used], "repl", true);
                }
                used = 0;
                draining = false;
                try self.ready();
            } else if (byte == 8 or byte == 127) {
                if (!draining and used != 0) used -= 1;
            } else if (!draining) {
                if (used == line.len) {
                    draining = true;
                } else {
                    line[used] = byte;
                    used += 1;
                }
            }
        }
        try self.notice("qjs: quit\n");
    }

    fn ready(self: *Session) !void {
        var message: [64]u8 = undefined;
        try self.notice(try std.fmt.bufPrint(&message, "qjs: ready eval={d}\n", .{self.runtime.control.evals}));
    }

    /// M211a (#3867): the `QJS.BIN --dom-rpc` worker session — reads JSON-lines
    /// ops, writes exactly one reply line per op on fd-1. With an argv ops
    /// path the ops come from the share (the direct-drive form a monitor
    /// exec or gate leg uses); without it they arrive on the inherited fd-0
    /// endpoint the stream-bound parent staged. fd-2 stays console, so the
    /// reply stream carries JSON only and `qjs:` diagnostics never pollute it.
    fn domRpc(self: *Session, ops_path: ?[]const u8) !void {
        const ops = try self.runtime.allocate(qjs.domrpc.ops_max);
        defer self.runtime.release(ops);
        const out = try self.runtime.allocate(qjs.domrpc.out_max);
        defer self.runtime.release(out);
        const ans = try self.runtime.allocate(qjs.domrpc.reply_max);
        defer self.runtime.release(ans);
        self.rpc_out = out;
        self.rpc_ans = ans;
        var len: usize = 0;
        if (ops_path) |name| {
            len = try rpcReadOpsFile(name, ops);
        } else {
            len = try rpcReadStdin(ops);
        }
        var greeted = false;
        var rest = ops[0..len];
        while (rest.len > 0) {
            const nl = @min(std.mem.indexOfScalar(u8, rest, '\n') orelse rest.len, rest.len);
            // Trim in place — the mutable slice is what parseOp decodes into.
            var line = rest[0..nl];
            while (line.len > 0 and (line[0] == ' ' or line[0] == '\t' or line[0] == '\r')) line = line[1..];
            while (line.len > 0 and (line[line.len - 1] == ' ' or line[line.len - 1] == '\t' or line[line.len - 1] == '\r')) line = line[0 .. line.len - 1];
            rest = rest[@min(nl + 1, rest.len)..];
            if (line.len == 0) continue;
            try self.rpcOp(line, &greeted);
        }
    }

    fn rpcWrite(self: *Session, bytes: []const u8) !void {
        try sdk.console.writeAll(sdk.native.consoleChunk, bytes);
        try self.mirror('O', bytes);
    }

    fn rpcError(self: *Session, name: ?[]const u8, word: []const u8) !void {
        var buf: [384]u8 = undefined;
        const line = if (name) |n|
            try std.fmt.bufPrint(&buf, "{{\"op\":\"{s}\",\"ok\":false,\"error\":\"{s}\"}}\n", .{ n, word })
        else
            try std.fmt.bufPrint(&buf, "{{\"op\":null,\"ok\":false,\"error\":\"{s}\"}}\n", .{word});
        try self.rpcWrite(line);
    }

    fn rpcOp(self: *Session, line: []u8, greeted: *bool) !void {
        const op = qjs.domrpc.parseOp(line) catch
            return self.rpcError(null, "dom_rpc_parse");
        if (std.mem.eql(u8, op.name, "hello")) {
            var buf: [256]u8 = undefined;
            if (op.version != null and op.version.? == qjs.domrpc.version) {
                greeted.* = true;
                return self.rpcWrite(try std.fmt.bufPrint(&buf, "{{\"op\":\"hello\",\"ok\":true,\"version\":{d}}}\n", .{qjs.domrpc.version}));
            }
            return self.rpcWrite(try std.fmt.bufPrint(&buf, "{{\"op\":\"hello\",\"ok\":false,\"error\":\"dom_rpc_version\",\"version\":{d},\"got\":{d}}}\n", .{ qjs.domrpc.version, op.version orelse 0 }));
        }
        if (!greeted.*)
            return self.rpcError(op.name, "dom_rpc_hello");
        if (!std.mem.eql(u8, op.name, "eval"))
            return self.rpcError(op.name, "dom_rpc_op");
        const src = op.src orelse return self.rpcError("eval", "dom_rpc_parse");
        return self.rpcEval(src);
    }

    /// M211b (#3868): the DOM surface is adapter-owned source, not C
    /// bindings — `virelai.dom` is the only native it rides. `document`
    /// covers the v1 op set (`doc.title`, `node.text`, `node.attr`) and
    /// `console.log` becomes a wire op, so a script's log is a serial
    /// marker the parent prints, not buffered `out`. The refused stub
    /// names are the known-but-not-admitted op vocabulary; each still
    /// crosses the wire so the parent's `dom_op_refused` answers it.
    const dom_prologue =
        \\var __domops = virelai.dom;
        \\var document = {};
        \\document.getElementById = function(id) {
        \\  var node = {};
        \\  var sid = String(id);
        \\  Object.defineProperty(node, "id", { value: sid });
        \\  Object.defineProperty(node, "textContent", {
        \\    get: function() { return __domops("node.text", sid); }
        \\  });
        \\  node.getAttribute = function(name) {
        \\    return __domops("node.attr", sid, String(name));
        \\  };
        \\  return node;
        \\};
        \\Object.defineProperty(document, "title", {
        \\  get: function() { var t = __domops("doc.title"); return t === null ? "" : t; }
        \\});
        \\document.getElementsByTagName = function() { return __domops("nodes.byTag", "x"); };
        \\document.getElementsByClassName = function() { return __domops("nodes.byClass", "x"); };
        \\document.querySelector = function() { return __domops("node.query", "x"); };
        \\document.querySelectorAll = function() { return __domops("node.query", "x"); };
        \\document.createElement = function() { return __domops("doc.write", "x"); };
        \\document.write = function() { return __domops("doc.write", "x"); };
        \\console.log = function() {
        \\  var parts = [];
        \\  for (var i = 0; i < arguments.length; i++) parts.push(String(arguments[i]));
        \\  __domops("console.log", parts.join(" "));
        \\};
        \\var setTimeout = function() { return __domops("timer.set", "x"); };
        \\var setInterval = setTimeout;
        \\var fetch = function() { return __domops("net.fetch", "x"); };
        \\var XMLHttpRequest = fetch;
    ;

    /// Installs the DOM surface once per context — a context reset (a
    /// budget or refusal that poisoned the graph) drops `dom_ready` so
    /// the next eval re-installs into the fresh context. A failed
    /// prologue is a named diagnostic, never a silent half-surface.
    fn rpcPrologue(self: *Session) void {
        self.dom_ready = true;
        const result = self.runtime.eval(dom_prologue, "dom-prologue", self.rpcSink(), false) catch {
            self.notice("qjs: DomPrologue\n") catch {};
            return;
        };
        if (result.context_reset) self.dom_ready = false;
        if (result.failure) |failure| {
            var message: [96]u8 = undefined;
            self.notice(std.fmt.bufPrint(&message, "qjs: DomPrologue {s}\n", .{@errorName(failure)}) catch "qjs: DomPrologue\n") catch {};
        }
    }

    /// One bounded DOM op: name + two args become a request line on fd-1,
    /// and the poll reads the rendezvous until the matching `seq` answer
    /// or a named bound refuses. Waiting on the parent is the parent's
    /// clock, not the script's — each slept span shifts the eval's
    /// deadline origin forward, so only JS-side execution counts against
    /// the four-second script budget. A parent that never answers meets
    /// the wire's own bound, `dom_op_wire`, not the eval's deadline.
    const dom_wait_s: u64 = 10;

    fn hostDom(context: *anyopaque, op: []const u8, a: []const u8, b: []const u8, out: []u8) anyerror!usize {
        const self: *Session = @ptrCast(@alignCast(context));
        if (!qjs.domrpc.nameOk(op)) return self.domFail("dom_op_unknown");
        const clock = self.runtime.control.clock;
        if (clock.counter(clock.context) -% self.runtime.control.start >= clock.frequency * 4)
            return self.domFail("script_budget_time");
        if (self.rpc_ops >= qjs.domrpc.script_ops_max) return self.domFail("script_budget_ops");
        self.rpc_ops += 1;
        self.rpc_seq += 1;
        var line: [qjs.domrpc.reply_max]u8 = undefined;
        const used = qjs.domrpc.request(&line, op, self.rpc_seq, a, b);
        self.rpcWrite(line[0..used]) catch return self.domFail("dom_op_wire");
        const wait_start = clock.counter(clock.context);
        while (true) {
            // Everything in a poll iteration — the rendezvous read, the
            // parse, the nap — is the parent's clock: shift the deadline
            // origin by the iteration's whole span so only the request's
            // emit and the script's own work count against the budget.
            const iter = clock.counter(clock.context);
            if (iter -% wait_start >= clock.frequency * dom_wait_s)
                return self.domFail("dom_op_wire");
            if (!self.runtime.control.poll())
                return self.domFail(@errorName(self.runtime.control.failure orelse error.Cancelled));
            const n = rpcReadAnswer(self.rpc_ans) catch 0;
            if (n > 0) {
                if (qjs.domrpc.parseAnswer(self.rpc_ans[0..n])) |answer| {
                    if (answer.seq == self.rpc_seq) {
                        if (!answer.ok) return self.domFail(answer.err orelse "dom_op_refused");
                        const value = answer.value orelse return qjs.domrpc.dom_null;
                        if (value.len > out.len) return self.domFail("dom_op_bounds");
                        @memcpy(out[0..value.len], value);
                        return value.len;
                    }
                } else |_| {}
            }
            _ = sdk.native.call(94, .{ 1_000_000, 0, 0, 0, 0, 0 });
            self.runtime.control.start +%= clock.counter(clock.context) -% iter;
        }
    }

    /// Stores a refusal name verbatim — the wire's word or the adapter's
    /// own — so `qjs_native_host_failure` hands it to JS untouched.
    fn domFail(self: *Session, word: []const u8) anyerror {
        const take = @min(word.len, self.dom_failure.len - 1);
        @memcpy(self.dom_failure[0..take], word[0..take]);
        self.dom_failure[take] = 0;
        self.runtime.host_failure = self.dom_failure[0..take :0];
        return error.DomOpRefused;
    }

    fn rpcEval(self: *Session, src: []const u8) !void {
        if (!self.dom_ready) self.rpcPrologue();
        self.rpc_out_len = 0;
        self.rpc_clipped = false;
        self.rpc_ops = 0;
        const result = try self.runtime.eval(src, "dom-rpc", self.rpcSink(), true);
        if (result.context_reset) self.dom_ready = false;
        if (sdk.stackHighWater() > qjs.limits.stack) return error.StackLimit;
        if (result.failure) |failure| {
            // M211b: in the worker the engine's execution bounds — the
            // four-second wall deadline and the work-site budget — ARE
            // the per-script time budget; the reply names one refusal.
            const word: []const u8 = if (failure == error.EvaluationDeadline or failure == error.WorkLimit) "script_budget_time" else @errorName(failure);
            var buf: [384]u8 = undefined;
            const line = try std.fmt.bufPrint(&buf, "{{\"op\":\"eval\",\"ok\":false,\"error\":\"{s}\",\"ops\":{d}{s}}}\n", .{ word, self.rpc_ops, if (result.context_reset) ",\"reset\":true" else "" });
            return self.rpcWrite(line);
        }
        var buf: [qjs.domrpc.reply_max]u8 = undefined;
        var used = (try std.fmt.bufPrint(&buf, "{{\"op\":\"eval\",\"ok\":true,\"ops\":{d},\"out\":\"", .{self.rpc_ops})).len;
        used += qjs.domrpc.escape(buf[used..], self.rpc_out[0..self.rpc_out_len]);
        const tail: []const u8 = if (self.rpc_clipped) "\",\"clipped\":true}\n" else "\"}\n";
        @memcpy(buf[used..][0..tail.len], tail);
        used += tail.len;
        try self.rpcWrite(buf[0..used]);
    }

    fn rpcCapture(context: *anyopaque, bytes: []const u8) anyerror!usize {
        const self: *Session = @ptrCast(@alignCast(context));
        const take = @min(self.rpc_out.len - self.rpc_out_len, bytes.len);
        @memcpy(self.rpc_out[self.rpc_out_len..][0..take], bytes[0..take]);
        self.rpc_out_len += take;
        if (take < bytes.len) self.rpc_clipped = true;
        return bytes.len;
    }

    fn rpcSink(self: *Session) qjs.Sink {
        return .{ .context = self, .write = rpcCapture, .diagnostic = diagnostic };
    }
};

/// The kernel stream endpoint index fd-0 binds (file_table.zig stream_base).
const dom_rpc_stdin: u64 = 0x100;

/// M211b (#3868): the parent→worker half of a DOM request — WEB publishes
/// each answer atomically here (WriteFileSafe's delete-then-rename), so an
/// absent or half-published read is simply "not yet", never torn input.
/// The path is a protocol constant, not argv, so it needs no policy pass.
const dom_ans_path = "/host/WEBRPC.ANS";

/// Reads the whole rendezvous file through the same generic file ABI the
/// argv ops path uses (slots 23/24/26) — a fresh open per poll is what
/// lets the worker see a file the parent REPLACES: the share clamps reads
/// to the open-time size, so only a new open observes the new answer.
/// Absent file, torn read or a stale `seq` all return to the poll loop.
fn rpcReadAnswer(out: []u8) !usize {
    const fd = sdk.native.call(23, .{ @intFromPtr(dom_ans_path.ptr), dom_ans_path.len, 1, 0, 0, 0 });
    if (fd < 0) return error.DomRpcAns;
    defer _ = sdk.native.call(26, .{ @as(u64, @intCast(fd)), 0, 0, 0, 0, 0 });
    var used: usize = 0;
    while (used < out.len) {
        const n = sdk.native.call(24, .{ @as(u64, @intCast(fd)), @intFromPtr(out.ptr + used), out.len - used, 0, 0, 0 });
        if (n < 0) return error.DomRpcAns;
        if (n == 0) return used;
        used += @intCast(n);
    }
    return out.len;
}

/// Reads the argv-named ops spool through the generic file ABI (slots
/// 23/24/26), not the contained API — the contained pin only answers on the
/// custom-virtio channel, and the direct-drive form must work on whatever
/// share the caller armed. The path was already policy-checked at argv.
fn rpcReadOpsFile(name: []const u8, ops: []u8) !usize {
    const fd = sdk.native.call(23, .{ @intFromPtr(name.ptr), name.len, 1, 0, 0, 0 });
    if (fd < 0) return error.DomRpcOps;
    defer _ = sdk.native.call(26, .{ @as(u64, @intCast(fd)), 0, 0, 0, 0, 0 });
    var used: usize = 0;
    while (used < ops.len) {
        const n = sdk.native.call(24, .{ @as(u64, @intCast(fd)), @intFromPtr(ops.ptr + used), ops.len - used, 0, 0, 0 });
        if (n < 0) return error.DomRpcOps;
        if (n == 0) return used;
        used += @intCast(n);
    }
    var probe: [1]u8 = undefined;
    const n = sdk.native.call(24, .{ @as(u64, @intCast(fd)), @intFromPtr(&probe), 1, 0, 0, 0 });
    if (n < 0) return error.DomRpcOps;
    if (n > 0) return error.DomRpcOpsLimit;
    return used;
}

/// Drains the inherited fd-0 endpoint — the ops spool a stream-bound parent
/// staged — to EOF. Filling the wire bound without EOF is a named refusal,
/// not a line a parser never sees.
fn rpcReadStdin(ops: []u8) !usize {
    var used: usize = 0;
    while (used < ops.len) {
        const n = sdk.native.call(24, .{ dom_rpc_stdin, @intFromPtr(ops.ptr + used), ops.len - used, 0, 0, 0 });
        if (n < 0) return error.DomRpcRead;
        if (n == 0) return used;
        used += @intCast(n);
    }
    var probe: [1]u8 = undefined;
    const n = sdk.native.call(24, .{ dom_rpc_stdin, @intFromPtr(&probe), 1, 0, 0, 0 });
    if (n < 0) return error.DomRpcRead;
    if (n > 0) return error.DomRpcOpsLimit;
    return used;
}

fn run(args: *const sdk.startup.Startup) !void {
    const options = policy.parse(args.args[0..args.argc], args.env[0..args.envc]) catch |err|
        fail(@errorName(err), 64);
    if (!@import("build_options").engine) return;
    try sdk.initialize(qjs.limits.arena);
    const arena = sdk.currentArena();
    const base = arena.used();
    const runtime = try qjs.Runtime.create(arena, try qjs.nativeClock());
    var alive = true;
    defer if (alive) runtime.destroy() catch {};
    const queue = try runtime.allocate(qjs.limits.line + 1);
    var session: Session = .{ .runtime = runtime, .queue = queue };
    runtime.host = .{
        .context = &session,
        .readFile = Session.hostReadFile,
        .writeFile = Session.hostWriteFile,
        .args = args.args[0..args.argc],
        // M211b: the DOM dispatch exists only on the dom-rpc wire —
        // every other session's virelai.dom refuses dom_op_refused.
        .dom = if (options.domrpc) Session.hostDom else null,
    };
    var status: u8 = 0;
    if (options.receipt) |name| session.openReceipt(name) catch |err| {
        session.close() catch {};
        runtime.release(queue);
        try runtime.destroy();
        alive = false;
        fail(@errorName(err), 70);
    };
    const operation = if (options.domrpc)
        session.domRpc(options.ops)
    else if (options.repl)
        session.repl()
    else
        session.evalFile(options.script.?);
    operation catch |err| {
        status = 70;
        var message: [96]u8 = undefined;
        session.terminal(std.fmt.bufPrint(&message, "qjs: {s}\n", .{@errorName(err)}) catch unreachable) catch {};
    };
    var summary: [192]u8 = undefined;
    const row = try std.fmt.bufPrint(&summary, "status={d} evals={d} source={d} output={d} diagnostics={d} receipt={d}\n", .{ status, runtime.control.evals, runtime.control.session_source, runtime.control.session_output, runtime.control.diagnostic_bytes, runtime.control.receipt_bytes });
    session.mirror('Z', row) catch {
        status = 70;
    };
    session.close() catch |err| {
        try sdk.console.writeAll(diagnosticChunk, @errorName(err));
        try sdk.console.writeAll(diagnosticChunk, "\n");
        status = 70;
    };
    const diagnostic_left = qjs.limits.diagnostics - runtime.control.diagnostic_bytes;
    runtime.release(queue);
    runtime.destroy() catch |err| {
        alive = false;
        fail(@errorName(err), 70);
    };
    alive = false;
    if (arena.used() != base or sdk.filesystem().liveFiles() != 0) return error.ResourceLeak;
    const final_stack = sdk.stackHighWater();
    if (final_stack > qjs.limits.stack) return error.StackLimit;
    var final_buffer: [160]u8 = undefined;
    const final = try std.fmt.bufPrint(&final_buffer, "qjs: final stack={d} arena_peak={d} files=0\nqjs: clean\n", .{ final_stack, arena.peak() });
    if (final.len > diagnostic_left) return error.DiagnosticLimit;
    try sdk.console.writeAll(diagnosticChunk, final);
    sdk.finish(status);
}
