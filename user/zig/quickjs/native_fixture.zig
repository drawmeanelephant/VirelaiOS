const std = @import("std");
const sdk = @import("sdk").runtime;
pub const os = sdk.os;
pub const std_options = sdk.std_options;
pub const panic = std.debug.FullPanic(sdk.panic);
pub const virelai = sdk.platform;
pub const std_options_debug_io = sdk.std_options_debug_io;
pub const std_options_FilePermissions = sdk.std_options_FilePermissions;
pub const std_options_cwd = sdk.std_options_cwd;
const qjs = if (@import("build_options").engine) @import("qjs") else struct {};

comptime {
    sdkEntry();
}
fn sdkEntry() void {
    @import("sdk").entry.exportEntry(run);
}
fn run(args: *const sdk.startup.Startup) !void {
    try sdk.initialize(4_194_304);
    if (@import("build_options").engine) {
        _ = qjs;
        if (args.argc > 1 and std.mem.eql(u8, args.args[1], "clock")) {
            const Sink = struct {
                fn write(_: *anyopaque, bytes: []const u8) !usize {
                    const count = sdk.native.consoleChunk(bytes);
                    if (count < 0) return error.WriteFailed;
                    return @intCast(count);
                }
                fn diagnostic(_: *anyopaque, bytes: []const u8) !void {
                    try sdk.print(bytes);
                }
            };
            var token: u8 = 0;
            const runtime = try qjs.Runtime.create(sdk.currentArena(), try qjs.nativeClock());
            defer runtime.destroy() catch |err| sdk.fail(@errorName(err), 70);
            const result = try runtime.eval("1+2*3", "contract.js", .{ .context = &token, .write = Sink.write, .diagnostic = Sink.diagnostic }, true);
            if (result.failure) |failure| return failure;
        } else try @import("contract.zig").run(sdk.currentArena());
    }
    if (sdk.stackHighWater() > sdk.stack_budget) return error.StackLimit;
    try sdk.print("quickjs-runtime: native linked contract complete\n");
}
