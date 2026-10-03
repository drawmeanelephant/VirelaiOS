//! C3 serial-offline compiler and contained single-entry publisher.
const std = @import("std");
const sdk = @import("sdk");
const core = @import("boris/core.zig");
const fixture = @import("boris/fixture.zig");
const policy = @import("boris/policy.zig");
const discovery = @import("boris/discovery.zig");
const publication = @import("boris/publication.zig");
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
var publication_state: publication.State = .{};
export var boris_stack_floor: usize = 0;

const Driver = struct {
    pub fn call(number: u64, args: [6]u64) i64 {
        // Offline artifact: no B6 socket/DNS syscall can be emitted.
        if (number == 80) return -4;
        if (number == 72) {
            const mode = @import("boris_options").entropy_mode;
            if (mode == .unavailable) return -1;
            var limited = args;
            if (mode == .short) limited[1] = @min(args[1], 3);
            return sdk.native.call(number, limited);
        }
        return sdk.native.call(number, args);
    }
    pub fn networkNow() u64 {
        fatal("Unsupported:Network");
    }
    pub const filesystem_b2 = true;
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
    if (publication_state.staged) {
        var receipt: [192]u8 = undefined;
        emit(std.fmt.bufPrint(&receipt, "boris-publication: retained_stage={s} published={d} outcome={s}\n", .{
            publication_state.stage_name,                                               publication_state.published,
            if (publication_state.submitted) "partial_or_unknown" else "not_published",
        }) catch "boris-publication: retained stage\n");
    }
    if (backend) |state| state.closeAll() catch emit("boris-guest: CloseFailed\n");
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
    _ = @import("boris/memory.zig");
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
    if (command == .build)
        publication.validateRoots(args.args[2], args.args[3]) catch |err| fail(@errorName(err), 70);
    sdk.initialize(@import("boris_options").arena_bytes) catch |err| fail(@errorName(err), 70);
    backend = os.heap.page_allocator.create(Backend) catch fail("OutOfMemory", 70);
    backend.?.* = .{};
    run(command, args.args[1..args.argc]) catch |err| fail(@errorName(err), 70);
    backend.?.closeAll() catch fail("CloseFailed", 70);
    for (1..3) |i|
        if (sdk.native.call(26, .{ 0x100 + i, 0, 0, 0, 0, 0 }) != 0)
            fail("StreamCloseFailed", 70);
    sdk.native.exit(0);
}
fn run(command: policy.Command, args: []const []const u8) !void {
    switch (command) {
        .help => try sdk.print("boris-guest: build ROOT OUT | inspect ROOT | compile ROOT | probe | version | help\nbuild requires existing disjoint /host roots and quiescent inputs; serial offline HTML plus compiler evidence.\nPublication is per-entry, not a site transaction or power-loss durability. Failed stages are retained; earlier replacements are not rolled back.\nwatch/preview/online/auth/editor/capture/parallel are unsupported.\n"),
        .version => try sdk.print("boris-guest 08969742f85238443ce5cd1cd53ceab1b1f3f85a (serial offline, no libc)\n"),
        .inspect, .compile, .build => {
            if (@import("boris_options").exhaust_resources) {
                for (0..4) |_| _ = try backend.?.openSnapshot(args[1], "");
                emit("boris-test: resources_peak=8\n");
            }
            var inventory = try discovery.discover(backend.?, os.heap.page_allocator, args[1]);
            defer inventory.deinit();
            if (command == .inspect) {
                var receipt: [128]u8 = undefined;
                try sdk.print(try std.fmt.bufPrint(&receipt, "boris-inventory: visited={d} resources_peak={d}\n", .{
                    inventory.entries.items.len, 4 + backend.?.peak_files,
                }));
            } else {
                var captured = try discovery.capture(backend.?, os.heap.page_allocator, args[1], &inventory);
                defer captured.deinit();
                var result = try core.compile(backend.?.io(), os.heap.page_allocator, captured.files.items);
                defer result.deinit();
                if (command == .build) {
                    try publication.publish(backend.?, os.heap.page_allocator, args[1], args[2], result.artifacts.items(), &publication_state);
                    var published: [128]u8 = undefined;
                    try sdk.print(try std.fmt.bufPrint(&published, "boris-published: artifacts={d} jobs=1 offline=1\n", .{publication_state.published}));
                } else {
                    try bundle(&result);
                }
                var receipt: [192]u8 = undefined;
                emit(try std.fmt.bufPrint(&receipt, "boris-native: visited={d} input_bytes={d} arena_peak={d} stack_high_water={d} resources_peak={d}\n", .{
                    inventory.entries.items.len, captured.bytes, sdk.currentArena().peak(), sdk.stackHighWater(), 4 + backend.?.peak_files,
                }));
            }
        },
        .probe => {
            var result = try core.compile(backend.?.io(), os.heap.page_allocator, &fixture.files);
            defer result.deinit();
            try bundle(&result);
            var receipt: [128]u8 = undefined;
            emit(try std.fmt.bufPrint(&receipt, "boris-probe: arena_peak={d} stack_high_water={d} files_peak={d}\n", .{
                sdk.currentArena().peak(), sdk.stackHighWater(), backend.?.peak_files,
            }));
        },
    }
}
fn bundle(result: *const @import("boris").Compilation) !void {
    // Complete and validate compilation before writing any normal output.
    var buffer: [4096]u8 = undefined;
    var writer: std.Io.Writer = .{ .buffer = &buffer, .vtable = &.{ .drain = drain } };
    try core.write(&writer, result);
    try writer.flush();
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
