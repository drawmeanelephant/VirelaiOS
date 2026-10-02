//! Native C1 library proof and render/meta CLI; no hosted main or libc.
const std = @import("std");
const sdk = @import("sdk");
const core = @import("oliver_core.zig");
const oliver = @import("oliver");
const S = @import("oliver_streams.zig").Streams(sdk.native);
pub const virelai = sdk.platform;
pub const os = sdk.os;
pub const std_options = sdk.std_options;
pub const std_options_FilePermissions = sdk.std_options_FilePermissions;
pub const std_options_cwd = sdk.std_options_cwd;
pub const panic = std.debug.FullPanic(panicImpl);
var diagnostics: sdk.console.Diagnostics = .{};
const Files = sdk.io_helpers.Backend(struct {
    pub const call = sdk.native.call;
    pub fn instance() *anyopaque {
        return files orelse fail("IoNotInitialized", 70);
    }
    pub fn diagnostic(bytes: []const u8) void {
        diagnostics.emit(stderr, bytes) catch fail("DiagnosticLimit", 70);
    }
    pub fn fatal(name: []const u8) noreturn {
        fail(name, 70);
    }
});
var files: ?*Files = null;
fn fileIo() std.Io {
    return files.?.io();
}
export var oliver_startup_state: usize = 1;
// The builder guards EVERY emitted function entry against this floor.
// It leaves 8 KiB for a final frame; the builder rejects a larger frame.
export var oliver_stack_floor: usize = 0;

export fn oliver_stack_refused() callconv(.naked) noreturn {
    asm volatile (
        \\mov x8, #1
        \\mov x0, #2
        \\adr x1, 1f
        \\mov x2, #20
        \\svc #0
        \\mov x8, #3
        \\mov x0, #70
        \\svc #0
        \\b .
        \\1: .ascii "oliver: StackBudget\n"
    );
}
noinline fn stackProbe(depth: usize) void {
    var scratch: [4096]u8 = undefined;
    @memset(&scratch, @intCast(depth & 255));
    if (depth > 0) stackProbe(depth - 1);
    asm volatile (""
        :
        : [ptr] "r" (&scratch),
        : .{ .memory = true });
}

fn stderr(bytes: []const u8) i64 {
    return sdk.native.call(1, .{ 2, @intFromPtr(bytes.ptr), bytes.len, 0, 0, 0 });
}
fn fail(name: []const u8, code: u8) noreturn {
    diagnostics.emit(stderr, "oliver: ") catch {};
    diagnostics.emit(stderr, name) catch {};
    diagnostics.emit(stderr, "\n") catch {};
    sdk.native.exit(code);
}
fn panicImpl(_: []const u8, _: ?usize) noreturn {
    fail("Panic", 71);
}
fn closeStreams() !void {
    // stdin may be unbound in the explicit-file library proof. Successful
    // CLI mode closes it separately after observing genuine EOF.
    try S.close(0x101);
    try S.close(0x102);
}
fn metrics() !void {
    const high_water = sdk.stackHighWater();
    if (high_water > sdk.stack_budget) return error.StackBudget;
    // Gate instrumentation only, after all workload-owned files are closed.
    if (!@import("build_options").probe) return;
    var buf: [160]u8 = undefined;
    const file = try std.Io.Dir.cwd().createFile(fileIo(), "OLIVER.METRICS", .{});
    const text = try std.fmt.bufPrint(&buf, "arena_peak={d} stack_high_water={d} resources_peak={d} files_peak={d}\n", .{
        sdk.currentArena().peak(), high_water, 3 + files.?.peak_files, files.?.peak_files,
    });
    try sdk.io_helpers.writeBounded(file, fileIo(), text, buf.len);
    try file.sync(fileIo());
    try files.?.closeChecked(file);
}
fn library(args: []const []const u8, a: std.mem.Allocator) !void {
    if (args.len != 2) return error.Usage;
    const cwd: std.Io.Dir = .cwd();
    // Validate BOTH paths before opening either, never clip a long path.
    var path_buf: [64]u8 = undefined;
    _ = Files.path(cwd, args[0], &path_buf) catch return error.PathLimit;
    _ = Files.path(cwd, args[1], &path_buf) catch return error.PathLimit;
    const input = try cwd.openFile(fileIo(), args[0], .{});
    const buffer = try a.alloc(u8, core.input_limit);
    const bytes = try sdk.io_helpers.readBounded(input, fileIo(), buffer);
    try files.?.closeChecked(input);
    try core.preflight(bytes, try core.config(&.{ "render", "--from", "markdown" }));
    var result = try oliver.parse(a, bytes, .markdown, .{});
    defer result.deinit();
    try core.checkTree(a, result.document.root);
    const out = try a.alloc(u8, core.output_limit);
    var writer = std.Io.Writer.fixed(out);
    oliver.html.render(a, &writer, &result.document, .{}) catch |err|
        return if (err == error.WriteFailed) error.OutputLimit else err;
    const output = try cwd.createFile(fileIo(), args[1], .{});
    try sdk.io_helpers.writeBounded(output, fileIo(), writer.buffered(), core.output_limit);
    try output.sync(fileIo());
    try files.?.closeChecked(output);
}
fn run(args: *const sdk.startup.Startup) !void {
    try sdk.initialize(core.arena_limit);
    const a = sdk.os.heap.page_allocator;
    files = try a.create(Files);
    files.?.* = .{};
    if (@import("build_options").probe and args.argc == 2) {
        if (std.mem.eql(u8, args.args[1], "probe-panic")) @panic("C1 panic probe");
        if (std.mem.eql(u8, args.args[1], "probe-stack")) stackProbe(64);
    }
    if (args.argc > 1 and std.mem.eql(u8, args.args[1], "library")) {
        try library(args.args[2..args.argc], a);
    } else {
        const cfg = try core.config(args.args[1..args.argc]);
        const input = try a.alloc(u8, core.input_limit);
        const bytes = try S.readBounded(0x100, input);
        try S.close(0x100);
        const output = try a.alloc(u8, core.output_limit);
        const rendered = try core.run(a, cfg, bytes, output);
        // Confirm all diagnostic writes before publishing normal output.
        try diagnostics.emit(stderr, rendered.diagnostics);
        try S.writeAll(1, rendered.normal);
    }
    try metrics();
    try files.?.closeAll();
    try closeStreams();
}
comptime {
    _ = @import("oliver_builtins.zig");
    const Entry = struct {
        fn start() callconv(.naked) noreturn {
            asm volatile (
                \\mov x2, sp
                \\sub x3, x2, #30, lsl #12
                \\adrp x4, oliver_stack_floor
                \\str x3, [x4, :lo12:oliver_stack_floor]
                \\sub x3, x2, #33, lsl #12
                \\mov w4, #0xa5a5
                \\movk w4, #0xa5a5, lsl #16
                \\1:
                \\str w4, [x3], #4
                \\cmp x3, x2
                \\b.lo 1b
                \\b oliver_enter
            );
        }
        fn enter(argc: usize, argv: usize, sp: usize) callconv(.c) noreturn {
            oliver_startup_state +%= 1;
            const args = sdk.receive(argc, argv, sp) catch |err| fail(@errorName(err), 64);
            run(&args) catch |err| fail(@errorName(err), 70);
            sdk.native.exit(0);
        }
    };
    @export(&Entry.start, .{ .name = "_start" });
    @export(&Entry.enter, .{ .name = "oliver_enter" });
}
