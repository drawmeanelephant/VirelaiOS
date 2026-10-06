//! M94a: unchanged monitor tracer, plus ADR 0043 wire types and ENOSYS stub.
const abi = @import("syscall_abi.zig");
const exceptions = @import("exceptions.zig");
const process = @import("process.zig");
const scheduler = @import("scheduler.zig");

pub const slot = abi.number("sys_trace");
pub const implemented = false;
pub const version = 1;
pub const max_pids = 8;
pub const ring_records = 256;
pub const string_bytes = 128;
pub const max_read_records = 16;
pub const op_arm = 0;
pub const op_disarm = 1;
pub const op_filter = 2;
pub const op_read = 3;
pub const op_status = 4;
pub const op_arm_exec = 5;
pub const flag_redacted = 1;
pub const flag_no_return = 2;

pub const Config = extern struct {
    version: u32,
    pid_count: u32,
    pids: [max_pids]u64,
    slots: [2]u64,
};
pub const ExecRequest = extern struct {
    path_ptr: u64,
    path_len: u64,
    argv_ptr: u64,
    argc: u64,
};
pub const ReadHeader = extern struct {
    version: u32,
    record_bytes: u32,
    count: u32,
    reserved: u32,
    dropped: u64,
};
pub const Record = extern struct {
    version: u32,
    flags: u32,
    pid: u64,
    tid: u64,
    cntpct: u64,
    number: u64,
    args: [6]u64,
    result: i64,
    errno: u32,
    string_mask: u32,
    fault_mask: u32,
    truncated_mask: u32,
    string_lengths: [6]u16,
    reserved: u32,
    strings: [6][string_bytes]u8,
};

pub fn handle(_: [6]u64, _: *exceptions.VectorFrame) u64 {
    return @bitCast(@as(i64, -4));
}

const Writer = *const fn ([]const u8) void;
var write_fn: ?Writer = null;
// syscall.strace_pid stays the monitor's assignable compatibility cell.
// The implementation lives here; no monitor.zig change or second copy.
var strace_control: ?*?usize = null;

pub fn init(writer: Writer, control: *?usize) void {
    write_fn = writer;
    strace_control = control;
}

fn target_pid() ?usize {
    const control = strace_control orelse return null;
    return control.*;
}

// M94b may extend this per-call value without editing dispatch. Its before
// hook receives all six arguments and the original vector frame; after gets
// the same value even when a scheduling handler stages a different task.
pub const Call = struct {};

pub fn before(number: u64, args: [6]u64, _: *exceptions.VectorFrame) Call {
    // The legacy exit line precedes the handler, byte-for-byte.
    if (tracing_current() and !trace_excluded(number)) {
        if (number == abi.number("sys_exit")) {
            var buf: [96]u8 = undefined;
            var pos: usize = append_trace_str(buf[0..], "[strace ");
            pos += append_trace_dec(buf[pos..], @intCast(traced_pid_for_current()));
            pos += append_trace_str(buf[pos..], "] sys_exit(");
            pos += append_trace_hex(buf[pos..], args[0]);
            pos += append_trace_str(buf[pos..], ") = \xe2\x80\x94\n");
            if (write_fn) |wp| wp(buf[0..pos]);
        }
    }
    return .{};
}

pub fn after(_: Call, number: u64, args: [6]u64, result: u64) void {
    if (!trace_excluded(number)) maybe_trace(number, args, result);
}

fn trace_excluded(number: u64) bool {
    return switch (number) {
        70, 71 => true,
        else => false,
    };
}

fn tracing_current() bool {
    const target = target_pid() orelse return false;
    const pid = process.find_by_task(scheduler.current_id()) orelse return false;
    return pid == target;
}

fn traced_pid_for_current() usize {
    return process.find_by_task(scheduler.current_id()) orelse 0;
}

fn maybe_trace(number: u64, args: [6]u64, result: u64) void {
    const target = target_pid() orelse return;
    const w = write_fn orelse return;
    const pid = process.find_by_task(scheduler.current_id()) orelse return;
    if (pid != target) return;
    var buf: [112]u8 = undefined;
    var pos: usize = 0;
    pos += append_trace_str(buf[pos..], "[strace ");
    pos += append_trace_dec(buf[pos..], @intCast(pid));
    pos += append_trace_str(buf[pos..], "] ");
    pos += append_trace_str(buf[pos..], abi.lookup(number).?.name());
    pos += append_trace_str(buf[pos..], "(");
    pos += append_trace_hex(buf[pos..], args[0]);
    pos += append_trace_str(buf[pos..], ", ");
    pos += append_trace_hex(buf[pos..], args[1]);
    pos += append_trace_str(buf[pos..], ", ");
    pos += append_trace_hex(buf[pos..], args[2]);
    pos += append_trace_str(buf[pos..], ") = ");
    pos += append_trace_hex(buf[pos..], result);
    if (pos < buf.len) {
        buf[pos] = '\n';
        pos += 1;
    }
    w(buf[0..pos]);
}

fn append_trace_str(buf: []u8, s: []const u8) usize {
    const take = @min(s.len, buf.len);
    @memcpy(buf[0..take], s[0..take]);
    return take;
}

fn append_trace_dec(buf: []u8, v_in: u64) usize {
    var v = v_in;
    var tmp: [20]u8 = undefined;
    var n: usize = 0;
    if (v == 0) {
        tmp[0] = '0';
        n = 1;
    }
    while (v > 0) : (v /= 10) {
        tmp[n] = @intCast('0' + v % 10);
        n += 1;
    }
    var i: usize = 0;
    while (i < n) : (i += 1) buf[i] = tmp[n - 1 - i];
    return n;
}

fn append_trace_hex(buf: []u8, v: u64) usize {
    const digits = "0123456789abcdef";
    buf[0] = '0';
    buf[1] = 'x';
    if (v == 0) {
        buf[2] = '0';
        return 3;
    }
    var tmp: [16]u8 = undefined;
    var n: usize = 0;
    var vv = v;
    while (vv > 0) : (vv /= 16) {
        tmp[n] = digits[@intCast(vv % 16)];
        n += 1;
    }
    var i: usize = 0;
    while (i < n) : (i += 1) buf[2 + i] = tmp[n - 1 - i];
    return 2 + n;
}
