const std = @import("std");
pub const startup = @import("startup.zig");
pub const memory = @import("arena.zig");
pub const native = @import("native.zig");
pub const console = @import("console.zig");

var arena: ?memory.Arena = null;
var attempted = false;
var diagnostics: console.Diagnostics = .{};
var initial_sp: usize = 0;
pub const stack_budget = 128 * 1024;
const stack_probe_bytes = stack_budget + 4096;

pub const std_options: std.Options = .{ .page_size_min = 4096, .page_size_max = 4096 };
pub const os = struct {
    pub const heap = struct {
        pub const page_allocator: std.mem.Allocator = .{ .ptr = &attempted, .vtable = &.{
            .alloc = alloc,
            .free = free,
            .resize = std.mem.Allocator.noResize,
            .remap = std.mem.Allocator.noRemap,
        } };
        fn alloc(_: *anyopaque, len: usize, alignment: std.mem.Alignment, ra: usize) ?[*]u8 {
            const current = arena orelse return null;
            return current.allocator().rawAlloc(len, alignment, ra);
        }
        fn free(_: *anyopaque, bytes: []u8, alignment: std.mem.Alignment, ra: usize) void {
            const current = arena orelse unreachable;
            current.allocator().rawFree(bytes, alignment, ra);
        }
    };
};

pub fn initialize(bytes: usize) error{ OutOfMemory, AlreadyInitialized }!void {
    if (attempted) return error.AlreadyInitialized;
    attempted = true;
    arena = try memory.Arena.reserve(bytes, native.map);
}

pub fn currentArena() memory.Arena {
    return arena.?;
}

pub fn receive(argc: usize, ptr: usize, sp: usize) startup.Error!startup.Startup {
    initial_sp = sp;
    if (ptr < 4096 or ptr > 0x10000000 - startup.block_bytes)
        return error.InvalidStartupBlock;
    const block: [*]const u8 = @ptrFromInt(ptr);
    return startup.Startup.parse(argc, block[0..startup.block_bytes]);
}

pub fn print(bytes: []const u8) console.Error!void {
    try console.writeAll(native.consoleChunk, bytes);
}

pub fn fail(name: []const u8, status: u8) noreturn {
    diagnostics.emit(native.consoleChunk, name) catch {};
    diagnostics.emit(native.consoleChunk, "\n") catch {};
    native.exit(if (status == 0) 1 else status);
}

pub fn panic(message: []const u8, _: ?usize) noreturn {
    diagnostics.emit(native.consoleChunk, "zig-guest: panic: ") catch {};
    diagnostics.emit(native.consoleChunk, message) catch {};
    diagnostics.emit(native.consoleChunk, "\n") catch {};
    native.exit(71);
}

/// The entry trampoline fills the budget plus a guard page BEFORE using stack.
/// A measured fixture is not a worst-case bound for a later workload's recursion.
pub noinline fn stackHighWater() usize {
    const bottom = initial_sp - stack_probe_bytes;
    const bytes: [*]const volatile u8 = @ptrFromInt(bottom);
    var untouched: usize = 0;
    while (untouched < stack_probe_bytes and bytes[untouched] == 0xa5) : (untouched += 1) {}
    return stack_probe_bytes - untouched;
}

pub fn finish(status: u8) noreturn {
    // A2 owns no buffered streams or handles. Every console write completes
    // synchronously and propagates failure. A3/B1 must add real flush/close.
    native.exit(status);
}

test {
    _ = startup;
    _ = memory;
    _ = console;
}
