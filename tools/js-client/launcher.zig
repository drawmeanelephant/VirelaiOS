//! Five fresh slot-28 launches. Timing begins before the accepted syscall.
const std = @import("std");
const sdk = @import("sdk").runtime;
pub const os = sdk.os;
pub const std_options = sdk.std_options;
pub const panic = std.debug.FullPanic(sdk.panic);
pub const virelai = sdk.platform;
pub const std_options_debug_io = sdk.std_options_debug_io;
pub const std_options_FilePermissions = sdk.std_options_FilePermissions;
pub const std_options_cwd = sdk.std_options_cwd;
comptime {
    @import("sdk").entry.exportEntry(run);
}

fn run(_: *const sdk.startup.Startup) !void {
    try sdk.initialize(1024 * 1024);
    const frequency = asm volatile ("mrs %[v], cntfrq_el0"
        : [v] "=r" (-> u64),
    );
    if (frequency == 0) return error.ClockUnavailable;
    const path = "QJS.BIN";
    for (1..6) |index| {
        var receipt: [64]u8 = undefined;
        const argument = try std.fmt.bufPrint(&receipt, "--receipt=/host/COLD{d}", .{index});
        var block: [sdk.startup.block_bytes]u8 = undefined;
        try sdk.startup.pack(&.{ path, "/host/COLD.js", argument }, &.{}, &block);
        const start = asm volatile ("mrs %[v], cntvct_el0"
            : [v] "=r" (-> u64),
        );
        const child = sdk.native.call(28, .{ @intFromPtr(path.ptr), path.len, @intFromPtr(&block) + sdk.startup.arg_bytes, 2, 0, 0 });
        if (child < 0) return error.LaunchRefused;
        if (sdk.native.call(8, .{ @intCast(child), 0, 0, 0, 0, 0 }) != 0) return error.ChildFailed;
        var row: [160]u8 = undefined;
        try sdk.print(try std.fmt.bufPrint(&row, "qjs-cold: sample={d} child={d} frequency={d} launch={d}\n", .{ index, child, frequency, start }));
    }
    try sdk.print("qjs-cold: done\n");
}
