//! C2 native CLI. Engine and format implementations remain pinned upstream.
const std = @import("std");
const sdk = @import("runtime.zig");
const cli = @import("k4o/cli.zig");
const kt = @import("k4o");
const build_options = @import("build_options");
pub const virelai = sdk.platform;
pub const os = sdk.os;
pub const std_options = sdk.std_options;
pub const std_options_FilePermissions = sdk.std_options_FilePermissions;
pub const std_options_cwd = sdk.std_options_cwd;
pub const std_options_debug_io = std.Io{ .userdata = null, .vtable = &Files.vtable };
pub const panic = std.debug.FullPanic(panicImpl);
const Files = sdk.io_helpers.Backend(Driver);
var files: ?*Files = null;
var diagnostics: sdk.console.Diagnostics = .{};

const Driver = struct {
    pub const call = sdk.native.call;
    pub fn instance() *anyopaque {
        return files orelse fail("IoNotInitialized", 70);
    }
    pub fn diagnostic(bytes: []const u8) void {
        emit(bytes);
    }
    pub fn fatal(name: []const u8) noreturn {
        fail(name, 70);
    }
};

fn stderr(bytes: []const u8) i64 {
    return sdk.native.call(1, .{ 2, @intFromPtr(bytes.ptr), bytes.len, 0, 0, 0 });
}
fn emit(bytes: []const u8) void {
    diagnostics.emit(stderr, bytes) catch sdk.native.exit(70);
}
fn fail(name: []const u8, status: u8) noreturn {
    emit("k4o: ");
    emit(name);
    emit("\n");
    stats();
    if (files) |backend| backend.closeAll() catch {};
    sdk.native.exit(status);
}
fn panicImpl(message: []const u8, _: ?usize) noreturn {
    // No formatting or cleanup callbacks in a panic, including instrumented
    // builds. Native process teardown owns file/arena recovery.
    emit("k4o: panic: ");
    emit(message);
    emit("\n");
    sdk.native.exit(71);
}

comptime {
    @export(&start, .{ .name = "_start" });
    @export(&enter, .{ .name = "k4o_guest_start" });
}
fn start() callconv(.naked) noreturn {
    asm volatile (
        \\mov x2, sp
        \\sub x3, x2, #33, lsl #12
        \\mov w4, #0xa5a5
        \\movk w4, #0xa5a5, lsl #16
        \\1:
        \\str w4, [x3], #4
        \\cmp x3, x2
        \\b.lo 1b
        \\b k4o_guest_start
    );
}
fn enter(argc: usize, argv: usize, sp: usize) callconv(.c) noreturn {
    const args = sdk.receive(argc, argv, sp) catch |err| fail(@errorName(err), 64);
    run(&args) catch |err| fail(@errorName(err), 70);
    files.?.closeAll() catch fail("CloseFailed", 70);
    // stdin is unused and may be explicitly unbound by the launcher.
    // Only the two output bindings used by this CLI are closed here.
    for (1..3) |i|
        if (sdk.native.call(26, .{ 0x100 + i, 0, 0, 0, 0, 0 }) != 0)
            fail("StreamCloseFailed", 70);
    sdk.native.exit(0);
}

fn stdout(bytes: []const u8) !void {
    try sdk.console.writeAll(sdk.native.consoleChunk, bytes);
}
fn read(alloc: std.mem.Allocator, path: []const u8) ![]const u8 {
    const backend = files.?;
    const io = backend.io();
    const file = sdk.platform.cwd().openFile(io, path, .{}) catch |err| return switch (err) {
        error.NameTooLong, error.BadPathName => error.PathLimit,
        else => err,
    };
    // Fallible close on both success and failure; no void success fabrication.
    const buffer = alloc.alloc(u8, cli.input_limit) catch |err| {
        try backend.closeChecked(file);
        return err;
    };
    const result = sdk.io_helpers.readBounded(file, io, buffer) catch |err| {
        try backend.closeChecked(file);
        return err;
    };
    try backend.closeChecked(file);
    return result;
}

fn run(args: *const sdk.startup.Startup) !void {
    const command = cli.parse(args.args[1..args.argc]) catch |err| fail(@errorName(err), 64);
    try sdk.initialize(cli.arena_bytes);
    files = try os.heap.page_allocator.create(Files);
    files.?.* = .{};
    var temporary = std.heap.ArenaAllocator.init(os.heap.page_allocator);
    defer temporary.deinit();
    const alloc = temporary.allocator();
    switch (command) {
        .help => try stdout(cli.help),
        .version => try stdout("k4o " ++ build_options.version ++ "\n"),
        .render => |options| {
            const template = try read(alloc, options.template);
            const json = if (options.data) |path| try read(alloc, path) else null;
            var diagnostic: kt.Diagnostic = .{};
            const output = cli.render(alloc, template, json, options, &diagnostic) catch |err| {
                if (diagnostic.message.len > 0) {
                    emit("k4o: ");
                    emit(diagnostic.message);
                    emit("\n");
                }
                return err;
            };
            // Reject stack excess before any normal output. Static call-depth
            // checks are the release proof, this is independent live evidence.
            if (sdk.stackHighWater() > sdk.stack_budget) return error.StackBudget;
            stats();
            try stdout(output);
        },
    }
}

fn stats() void {
    if (!build_options.instrument or files == null) return;
    var buffer: [160]u8 = undefined;
    const message = std.fmt.bufPrint(&buffer, "k4o-budget: arena_peak={d} stack_high_water={d} files_peak={d}\n", .{
        sdk.currentArena().peak(), sdk.stackHighWater(), files.?.peak_files,
    }) catch return;
    emit(message);
}
