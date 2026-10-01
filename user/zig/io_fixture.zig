//! A3 native proof, not an allowlist addition.
const std = @import("std");
const sdk = @import("runtime.zig");
pub fn run(args: *const sdk.startup.Startup) !void {
    var storage = try sdk.InitState.init(args);
    defer storage.deinit();
    const init = storage.get(args.argc, args.envc);
    var iterator = init.minimal.args.iterate();
    if (!std.mem.eql(u8, iterator.next().?, args.args[0])) return error.Args;
    if (init.preopens.get("stdout") != null) return error.InventedPreopen;
    if (init.preopens.get("/host") == null) return error.MissingCwd;
    var env_map = try init.minimal.environ.createMap(init.gpa);
    defer env_map.deinit();
    if (env_map.count() != init.environ_map.count()) return error.Environment;
    std.debug.print("zig-io: debug {d}\n", .{@as(u32, 1867)});
    const cwd: std.Io.Dir = .cwd();
    const input = try cwd.openFile(init.io, "ZIO.INPUT", .{});
    defer input.close(init.io);
    const bytes = try init.gpa.alloc(u8, 5000);
    defer init.gpa.free(bytes);
    const read = try sdk.io_helpers.readBounded(input, init.io, bytes);
    if (read.len != bytes.len) return error.ShortInput;
    for (read, 0..) |byte, i| if (byte != i % 251) {
        return error.WrongInput;
    };
    const output = try cwd.createFile(init.io, "ZIO.OUTPUT", .{});
    defer output.close(init.io);
    var buffer: [97]u8 = undefined;
    var writer = output.writerStreaming(init.io, &buffer);
    try writer.interface.writeAll(read);
    try writer.interface.flush();
    try output.sync(init.io);
    try cwd.createDir(init.io, "ZIO.DIR", .default_dir);
    const truncated = try cwd.createFile(init.io, "ZIO.DIR/TRUNCATED", .{});
    try sdk.io_helpers.writeBounded(truncated, init.io, "truncate-me", 11);
    try truncated.setLength(init.io, 3);
    try truncated.sync(init.io);
    truncated.close(init.io);
    if (input.stat(init.io)) |_| return error.InventedStat else |err| {
        if (err != error.Unexpected) return err;
    }
    if (std.Io.File.stdout().writeStreamingAll(init.io, "must not reach console")) |_| {
        return error.InventedStdout;
    } else |err| if (err != error.Unexpected) return err;
    var entropy: [513]u8 = undefined;
    try init.io.randomSecure(&entropy);
    // Not a cryptographic proof; verifies this path isn't the zero fallback.
    if (std.mem.allEqual(u8, &entropy, 0)) return error.ZeroEntropy;
    const high_water = sdk.stackHighWater();
    if (high_water > sdk.stack_budget) return error.StackBudget;
    std.debug.print("zig-io: file=5000 arena_peak={d} stack_high_water={d}\n", .{
        sdk.currentArena().peak(), high_water,
    });
    try sdk.print("zig-io: done\n");
}
