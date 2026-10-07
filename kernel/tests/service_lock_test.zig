//! Lock-lifetime probes through the production dispatchers, with only the
//! registered handlers replaced. Keep one kernel import graph so observations
//! read the very same svclock globals the dispatchers acquire.

const std = @import("std");
const monitor = @import("monitor");
const syscall = monitor.syscall;
const svclock = monitor.svclock;

var observed: u5 = 0;
var calls: usize = 0;

fn held_mask() u5 {
    var bits: u5 = 0;
    if (svclock.file.held()) bits |= svclock.dom_bit(.file);
    if (svclock.net.held()) bits |= svclock.dom_bit(.net);
    if (svclock.win.held()) bits |= svclock.dom_bit(.win);
    if (svclock.ev.held()) bits |= svclock.dom_bit(.ev);
    if (svclock.kernel.held()) bits |= svclock.dom_bit(.kernel);
    return bits;
}

fn expect_unlocked() !void {
    try std.testing.expectEqual(@as(u5, 0), held_mask());
    inline for (.{ &svclock.file, &svclock.net, &svclock.win, &svclock.ev, &svclock.kernel }) |lock| {
        // Check the spinlock as well as its holder record.
        try std.testing.expect(lock.try_acquire());
        lock.release();
    }
}

fn observe_syscall(args: syscall.Args, _: *syscall.exceptions.VectorFrame) u64 {
    observed = held_mask();
    calls += 1;
    return args[0];
}

fn discard(_: []const u8) void {}

test "service locks: real syscall dispatch holds each domain through success and refusal" {
    syscall.init(discard);
    try expect_unlocked();
    defer svclock.release_set(held_mask());
    const cases = .{
        .{ syscall.sys_file_open, svclock.dom_bit(.file) },
        .{ syscall.sys_win_open, svclock.dom_bit(.win) },
        .{ syscall.sys_poll_event, svclock.dom_bit(.ev) },
        .{ syscall.sys_tcp_connect, svclock.dom_bit(.net) },
        .{ syscall.sys_procs, svclock.dom_bit(.kernel) },
        .{ syscall.sys_exec, svclock.dom_bit(.file) | svclock.dom_bit(.kernel) },
        .{ syscall.sys_exit, svclock.all_bits },
        .{ syscall.sys_ping, @as(u5, 0) },
    };
    inline for (cases) |case| {
        const slot = &syscall.table_storage[case[0]];
        const saved = slot.*;
        defer slot.* = saved;
        slot.handler = observe_syscall;
        var frame = std.mem.zeroes(syscall.exceptions.VectorFrame);
        for ([_]u64{ 123, syscall.error_result(.einval), syscall.error_result(.enosys) }) |result| {
            observed = 0;
            calls = 0;
            const count = syscall.call_count(case[0]);
            try std.testing.expectEqual(result, syscall.dispatch(case[0], .{ result, 0, 0, 0, 0, 0 }, &frame));
            try std.testing.expectEqual(@as(usize, 1), calls);
            try std.testing.expectEqual(case[1], observed);
            try std.testing.expectEqual(count + 1, syscall.call_count(case[0]));
            try expect_unlocked();
        }
    }
}

test "service locks: real syscall dispatch releases on a reserved slot and out-of-range ENOSYS" {
    syscall.init(discard);
    try expect_unlocked();
    defer svclock.release_set(held_mask());
    // Exercise the null-handler early return with a slot that takes FILE and
    // KERNEL. Unassigned slots otherwise have no domains to leak.
    const slot = &syscall.table_storage[syscall.sys_exec];
    const saved = slot.*;
    defer slot.* = saved;
    slot.handler = null;
    var frame = std.mem.zeroes(syscall.exceptions.VectorFrame);
    try std.testing.expectEqual(syscall.error_result(.enosys), syscall.dispatch(syscall.sys_exec, .{ 0, 0, 0, 0, 0, 0 }, &frame));
    try std.testing.expectEqual(@as(u64, 1), syscall.call_count(syscall.sys_exec));
    try expect_unlocked();
    try std.testing.expectEqual(syscall.error_result(.enosys), syscall.dispatch(syscall.slot_count, .{ 0, 0, 0, 0, 0, 0 }, &frame));
    try expect_unlocked();
}

fn make_monitor(mock: anytype) monitor.Monitor {
    return monitor.Monitor.init(mock.console(), .{
        .handoff = std.mem.zeroes(monitor.handoff.HandoffV2),
        .map = monitor.memmap.MapView.init(&.{}, @sizeOf(monitor.memmap.MemoryDescriptor), 0),
        .console_name = "lock-probe",
    }, monitor.MachineControl.disabled());
}

fn observe_command(_: *monitor.Monitor, args: []const []const u8) monitor.ExecError {
    observed = held_mask();
    calls += 1;
    return if (args.len == 0) .none else .invalid_argument;
}

test "service locks: real monitor exec holds each domain through success and refusal" {
    try expect_unlocked();
    defer svclock.release_set(held_mask());
    var mock = monitor.console.MockConsole(512){};
    var mon = make_monitor(&mock);
    const cases = .{
        .{ "ls", svclock.dom_bit(.file) },
        .{ "dui", svclock.dom_bit(.win) },
        .{ "mbox", svclock.dom_bit(.ev) },
        .{ "net", svclock.dom_bit(.net) },
        .{ "version", @as(u5, 0) },
        .{ "settings", svclock.dom_bit(.file) | svclock.dom_bit(.win) },
    };
    inline for (cases) |case| {
        const cmd = @constCast(monitor.lookup(case[0]).?);
        const saved = cmd.*;
        defer cmd.* = saved;
        try std.testing.expectEqual(case[1], cmd.dom);
        cmd.handler = observe_command;
        for ([_][]const []const u8{ &.{case[0]}, &.{ case[0], "refuse" } }) |argv| {
            observed = 0;
            calls = 0;
            const result: monitor.ExecError = if (argv.len == 1) .none else .invalid_argument;
            try std.testing.expectEqual(result, monitor.exec(&mon, argv));
            try std.testing.expectEqual(@as(usize, 1), calls);
            try std.testing.expectEqual(case[1] | svclock.dom_bit(.kernel), observed);
            try expect_unlocked();
        }
    }
}

test "service locks: monitor dispatch refusals do not invoke a handler or leak locks" {
    try expect_unlocked();
    defer svclock.release_set(held_mask());
    var mock = monitor.console.MockConsole(512){};
    var mon = make_monitor(&mock);
    const cmd = @constCast(monitor.lookup("cat").?);
    const saved = cmd.*;
    defer cmd.* = saved;
    cmd.handler = observe_command;
    calls = 0;
    try std.testing.expectEqual(monitor.ExecError.usage, monitor.exec(&mon, &.{"cat"}));
    try std.testing.expectEqual(@as(usize, 0), calls);
    try expect_unlocked();
    try std.testing.expectEqual(monitor.ExecError.unknown_command, monitor.exec(&mon, &.{"no-such-lock-probe"}));
    try expect_unlocked();
    try std.testing.expectEqual(monitor.ExecError.usage, monitor.exec(&mon, &.{}));
    try expect_unlocked();
}

var nested_result: monitor.ExecError = .none;
var nested_observed: u5 = 0;

fn nested_command(mon: *monitor.Monitor, _: []const []const u8) monitor.ExecError {
    observed = held_mask();
    calls += 1;
    const result = monitor.exec(mon, &.{"settings"});
    // The inner dispatcher must not release any of the outer handler's locks.
    nested_observed = held_mask();
    return result;
}

fn nested_leaf(_: *monitor.Monitor, _: []const []const u8) monitor.ExecError {
    calls += 1;
    if (held_mask() != (svclock.dom_bit(.file) | svclock.dom_bit(.win) | svclock.dom_bit(.kernel))) return .usage;
    return nested_result;
}

test "service locks: nested monitor exec preserves a complete outer hold" {
    try expect_unlocked();
    defer svclock.release_set(held_mask());
    var mock = monitor.console.MockConsole(512){};
    var mon = make_monitor(&mock);
    const outer = @constCast(monitor.lookup("settings").?);
    const inner = @constCast(monitor.lookup("time").?);
    const saved_outer = outer.*;
    const saved_inner = inner.*;
    defer outer.* = saved_outer;
    defer inner.* = saved_inner;
    // Use distinct registered names but the same domain set for the nested
    // entry: acquire_missing must take nothing and release nothing.
    outer.handler = nested_leaf;
    inner.dom = outer.dom;
    inner.min_args = 0;
    inner.handler = nested_command;
    for ([_]monitor.ExecError{ .none, .invalid_argument }) |result| {
        calls = 0;
        observed = 0;
        nested_observed = 0;
        nested_result = result;
        try std.testing.expectEqual(result, monitor.exec(&mon, &.{"time"}));
        try std.testing.expectEqual(@as(usize, 2), calls);
        const expected = svclock.dom_bit(.file) | svclock.dom_bit(.win) | svclock.dom_bit(.kernel);
        try std.testing.expectEqual(expected, observed);
        try std.testing.expectEqual(expected, nested_observed);
        try expect_unlocked();
    }
}

test "service locks: monitor exec releases only newly taken domains under a partial outer hold" {
    try expect_unlocked();
    defer svclock.release_set(held_mask());
    var mock = monitor.console.MockConsole(512){};
    var mon = make_monitor(&mock);
    const cmd = @constCast(monitor.lookup("settings").?);
    const saved = cmd.*;
    defer cmd.* = saved;
    cmd.handler = observe_command;
    svclock.file.acquire();
    const expected = svclock.dom_bit(.file) | svclock.dom_bit(.win) | svclock.dom_bit(.kernel);
    for ([_][]const []const u8{ &.{"settings"}, &.{ "settings", "refuse" } }) |argv| {
        calls = 0;
        observed = 0;
        const result: monitor.ExecError = if (argv.len == 1) .none else .invalid_argument;
        try std.testing.expectEqual(result, monitor.exec(&mon, argv));
        try std.testing.expectEqual(expected, observed);
        try std.testing.expectEqual(@as(u5, svclock.dom_bit(.file)), held_mask());
    }
    svclock.file.release();
    try expect_unlocked();
}
