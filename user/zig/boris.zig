//! C3 closure probe. Native site build fails before any filesystem mutation.
const std = @import("std");
const sdk = @import("runtime.zig");
const core = @import("boris/core.zig");
const fixture = @import("boris/fixture.zig");
const policy = @import("boris/policy.zig");
pub const virelai = sdk.platform;
pub const os = sdk.os;
pub const std_options = sdk.std_options;
pub const std_options_FilePermissions = sdk.std_options_FilePermissions;
pub const std_options_cwd = sdk.std_options_cwd;
pub const std_options_debug_io = std.Io{ .userdata = null, .vtable = &Backend.vtable };
pub const panic = std.debug.FullPanic(panicImpl);
const Backend = sdk.io_helpers.Backend(Driver);
var backend: ?*Backend = null;
var diagnostics: sdk.console.Diagnostics = .{};
export var boris_stack_floor: usize = 0;

const Driver = struct {
    pub const call = sdk.native.call;
    pub fn instance() *anyopaque {
        return backend orelse fail("IoNotInitialized", 70);
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
    emit("boris-guest: ");
    emit(name);
    emit("\n");
    sdk.native.exit(status);
}
fn panicImpl(message: []const u8, _: ?usize) noreturn {
    emit("boris-guest: panic: ");
    emit(message);
    emit("\n");
    sdk.native.exit(71);
}
comptime {
    @export(&start, .{ .name = "_start" });
    @export(&enter, .{ .name = "boris_guest_start" });
    @export(&stackRefused, .{ .name = "boris_stack_refused" });
}
fn start() callconv(.naked) noreturn {
    asm volatile (
        \\mov x2, sp
        \\sub x3, x2, #30, lsl #12
        \\adrp x4, boris_stack_floor
        \\str x3, [x4, :lo12:boris_stack_floor]
        \\sub x3, x2, #33, lsl #12
        \\mov w4, #0xa5a5
        \\movk w4, #0xa5a5, lsl #16
        \\1:
        \\str w4, [x3], #4
        \\cmp x3, x2
        \\b.lo 1b
        \\b boris_guest_start
    );
}
/// Terminal, allocation-free refusal used by the whole-image entry guards.
fn stackRefused() callconv(.naked) noreturn {
    asm volatile (
        \\mov x0, #2
        \\adr x1, 1f
        \\mov x2, #25
        \\mov x8, #1
        \\svc #0
        \\mov x0, #70
        \\mov x8, #3
        \\svc #0
        \\b .
        \\1: .ascii "boris-guest: StackBudget\n"
    );
}
fn enter(argc: usize, argv: usize, sp: usize) callconv(.c) noreturn {
    const args = sdk.receive(argc, argv, sp) catch |err| fail(@errorName(err), 64);
    const command = policy.parse(args.args[1..args.argc]) catch |err| fail(@errorName(err), 64);
    if (command == .build) fail("FilesystemIdentityUnavailable:B3NotIntegrated", 70);
    sdk.initialize(policy.arena_bytes) catch |err| fail(@errorName(err), 70);
    backend = os.heap.page_allocator.create(Backend) catch fail("OutOfMemory", 70);
    backend.?.* = .{};
    run(command) catch |err| fail(@errorName(err), 70);
    backend.?.closeAll() catch fail("CloseFailed", 70);
    for (1..3) |i|
        if (sdk.native.call(26, .{ 0x100 + i, 0, 0, 0, 0, 0 }) != 0)
            fail("StreamCloseFailed", 70);
    sdk.native.exit(0);
}
fn run(command: policy.Command) !void {
    switch (command) {
        .help => try sdk.print("boris-guest: compiler closure probe only; probe | version | help\nbuild refuses until B3 native identity/containment integration exists.\nwatch/preview/online/auth/editor/capture/parallel are unsupported.\n"),
        .version => try sdk.print("boris-guest 08969742f85238443ce5cd1cd53ceab1b1f3f85a (closure probe, not native publication)\n"),
        .build => unreachable,
        .probe => {
            var result = try core.compile(backend.?.io(), os.heap.page_allocator, &fixture.files);
            defer result.deinit();
            // Complete and validate compilation before writing any normal output.
            var buffer: [4096]u8 = undefined;
            var writer: std.Io.Writer = .{
                .buffer = &buffer,
                .vtable = &.{ .drain = drain },
            };
            try core.write(&writer, &result);
            try writer.flush();
            var receipt: [128]u8 = undefined;
            emit(try std.fmt.bufPrint(&receipt, "boris-probe: arena_peak={d} stack_high_water={d} files_peak={d}\n", .{
                sdk.currentArena().peak(), sdk.stackHighWater(), backend.?.peak_files,
            }));
        },
    }
}
fn drain(writer: *std.Io.Writer, data: []const []const u8, splat: usize) std.Io.Writer.Error!usize {
    sdk.print(writer.buffered()) catch return error.WriteFailed;
    writer.end = 0;
    var n: usize = 0;
    for (data, 0..) |bytes, i| {
        const repeats = if (i + 1 == data.len) splat else 1;
        for (0..repeats) |_| {
            sdk.print(bytes) catch return error.WriteFailed;
            n += bytes.len;
        }
    }
    return n;
}
