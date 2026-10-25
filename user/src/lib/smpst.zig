//! VirelaiOS four-core stress-hammer syscall shim — SMPST (claim 907,
//! issue #858). A tiny freestanding EL0 helper the four domain hammers
//! (SMPFILE.BIN / SMPNET.BIN / SMPWIN.BIN / SMPEV.BIN) share: inline-asm
//! ADR 0007 syscall invocations + a line writer + the per-domain ops.
//! No libc, no POSIX; every helper host-gates the svc (a host unit test
//! that calls one would trap off-guest, so the gate functions also check
//! the freestanding tag and return 0/-1 — the host tests only pin the
//! marker constants, never run the payload).

/// Product version — the `ver M.m.p` the component-registry lib row
/// shows and the drift check resolves (docs/component-registry.md).
/// The registry's bump rule says when it moves.
pub const version = "1.0.0";

const builtin = @import("builtin");
const std = @import("std");

// ADR 0007 syscall slots (the frozen ABI — kernel/src/syscall.zig).
pub const sys_write_num: u64 = 1;
pub const sys_yield_num: u64 = 2;
pub const sys_exit_num: u64 = 3;
pub const sys_sleep_num: u64 = 4;
pub const sys_udp_listen_num: u64 = 9;
pub const sys_udp_send_num: u64 = 10;
pub const sys_udp_recv_num: u64 = 11;
pub const sys_win_open_num: u64 = 12;
pub const sys_win_fill_num: u64 = 13;
pub const sys_win_present_num: u64 = 14;
pub const sys_win_close_num: u64 = 15;
pub const sys_poll_event_num: u64 = 21;
pub const sys_file_open_num: u64 = 23;
pub const sys_file_read_num: u64 = 24;
pub const sys_file_write_num: u64 = 25;
pub const sys_file_close_num: u64 = 26;
pub const sys_timer_set_num: u64 = 40;
pub const sys_timer_cancel_num: u64 = 41;

pub const MODE_READ: u32 = 0x0001;
pub const MODE_WRITE: u32 = 0x0002;
pub const MODE_CREATE: u32 = 0x0004;

/// The ADR 0009 event shape (user/src/lib/ui.zig:148).
pub const Event = extern struct {
    kind: u16,
    flags: u16,
    seq: u32,
    arg0: u32,
    arg1: u32,
};
/// EVENT_TIMER kind (ui.zig:127).
pub const EVENT_TIMER: u16 = 9;

pub fn syscall0(num: u64) i64 {
    if (builtin.os.tag != .freestanding) return 0;
    var res: i64 = undefined;
    asm volatile ("svc #0"
        : [res] "={x0}" (res),
        : [num] "{x8}" (num),
        : .{ .memory = true });
    return res;
}

pub fn syscall1(num: u64, arg0: u64) i64 {
    if (builtin.os.tag != .freestanding) return 0;
    var res: i64 = undefined;
    asm volatile ("svc #0"
        : [res] "={x0}" (res),
        : [num] "{x8}" (num),
          [arg0] "{x0}" (arg0),
        : .{ .memory = true });
    return res;
}

pub fn syscall2(num: u64, arg0: u64, arg1: u64) i64 {
    if (builtin.os.tag != .freestanding) return 0;
    var res: i64 = undefined;
    asm volatile ("svc #0"
        : [res] "={x0}" (res),
        : [num] "{x8}" (num),
          [arg0] "{x0}" (arg0),
          [arg1] "{x1}" (arg1),
        : .{ .memory = true });
    return res;
}

pub fn syscall3(num: u64, arg0: u64, arg1: u64, arg2: u64) i64 {
    if (builtin.os.tag != .freestanding) return 0;
    var res: i64 = undefined;
    asm volatile ("svc #0"
        : [res] "={x0}" (res),
        : [num] "{x8}" (num),
          [arg0] "{x0}" (arg0),
          [arg1] "{x1}" (arg1),
          [arg2] "{x2}" (arg2),
        : .{ .memory = true });
    return res;
}

pub fn syscall4(num: u64, arg0: u64, arg1: u64, arg2: u64, arg3: u64) i64 {
    if (builtin.os.tag != .freestanding) return 0;
    var res: i64 = undefined;
    asm volatile ("svc #0"
        : [res] "={x0}" (res),
        : [num] "{x8}" (num),
          [arg0] "{x0}" (arg0),
          [arg1] "{x1}" (arg1),
          [arg2] "{x2}" (arg2),
          [arg3] "{x3}" (arg3),
        : .{ .memory = true });
    return res;
}

pub fn syscall6(num: u64, arg0: u64, arg1: u64, arg2: u64, arg3: u64, arg4: u64, arg5: u64) i64 {
    if (builtin.os.tag != .freestanding) return 0;
    var res: i64 = undefined;
    asm volatile ("svc #0"
        : [res] "={x0}" (res),
        : [num] "{x8}" (num),
          [arg0] "{x0}" (arg0),
          [arg1] "{x1}" (arg1),
          [arg2] "{x2}" (arg2),
          [arg3] "{x3}" (arg3),
          [arg4] "{x4}" (arg4),
          [arg5] "{x5}" (arg5),
        : .{ .memory = true });
    return res;
}

/// Write `msg` to the console (fd 1, slot 1) in ONE syscall — the kernel
/// holds the console TX lock for the whole line, so a multi-core burst
/// cannot byte-interleave it (the claim-881 slice-4 lesson).
pub fn write_line(msg: []const u8) void {
    if (msg.len == 0) return;
    _ = syscall3(sys_write_num, 1, @intFromPtr(msg.ptr), msg.len);
}

pub fn print(comptime tag: []const u8, comptime fmt: []const u8, args: anytype) void {
    var buf: [160]u8 = undefined;
    const line = std.fmt.bufPrint(&buf, tag ++ fmt, args) catch return;
    write_line(line);
}

pub fn yield_task() void {
    _ = syscall0(sys_yield_num);
}

pub fn sleep_ticks(ticks: u64) void {
    _ = syscall1(sys_sleep_num, ticks);
}

pub fn exit_process(status: u64) noreturn {
    _ = syscall1(sys_exit_num, status);
    while (true) yield_task();
}

// --- FILE domain (slots 23-26) ------------------------------------------

pub fn file_open(path: []const u8, flags: u32) i64 {
    return syscall3(sys_file_open_num, @intFromPtr(path.ptr), path.len, flags);
}

pub fn file_read(handle: u32, buf: []u8) i64 {
    return syscall3(sys_file_read_num, handle, @intFromPtr(buf.ptr), buf.len);
}

pub fn file_write(handle: u32, data: []const u8) i64 {
    return syscall3(sys_file_write_num, handle, @intFromPtr(data.ptr), data.len);
}

pub fn file_close(handle: u32) void {
    _ = syscall1(sys_file_close_num, handle);
}

// --- NET domain (slots 9-11) ---------------------------------------------

pub fn udp_listen(port: u16) i64 {
    return syscall1(sys_udp_listen_num, port);
}

pub fn udp_send(dst_ip: u32, dst_port: u16, payload: []const u8) i64 {
    return syscall4(sys_udp_send_num, dst_ip, dst_port, @intFromPtr(payload.ptr), payload.len);
}

pub fn udp_recv(port: u16, buf: []u8) i64 {
    return syscall3(sys_udp_recv_num, port, @intFromPtr(buf.ptr), buf.len);
}

// --- WIN domain (slots 12-15) --------------------------------------------

pub fn win_open(x: u32, y: u32, w: u32, h: u32) i64 {
    return syscall4(sys_win_open_num, x, y, w, h);
}

pub fn win_fill(id: u32, x: u32, y: u32, w: u32, h: u32, rgb: u32) i64 {
    return syscall6(sys_win_fill_num, id, x, y, w, h, rgb);
}

pub fn win_present(id: u32) i64 {
    return syscall1(sys_win_present_num, id);
}

pub fn win_close(id: u32) void {
    _ = syscall1(sys_win_close_num, id);
}

// --- EV domain (slots 21/40) ---------------------------------------------

pub fn poll_event(ev: *Event) i64 {
    return syscall1(sys_poll_event_num, @intFromPtr(ev));
}

pub fn timer_set(delay_ticks: u64) i64 {
    return syscall1(sys_timer_set_num, delay_ticks);
}

pub fn timer_cancel() i64 {
    return syscall0(sys_timer_cancel_num);
}
