const std = @import("std");
pub const startup = @import("startup.zig");
pub const memory = @import("arena.zig");
pub const native = @import("native.zig");
pub const console = @import("console.zig");
pub const platform = @import("platform.zig");
pub const io_helpers = @import("io.zig");
const Backend = io_helpers.Backend(struct {
    pub const call = native.call;
    pub fn instance() *anyopaque {
        return io_state orelse fail("IoNotInitialized", 70);
    }
    pub fn diagnostic(bytes: []const u8) void {
        diagnostics.emit(native.consoleChunk, bytes) catch native.exit(70);
    }
    pub fn fatal(name: []const u8) noreturn {
        fail(name, 70);
    }
});
var io_state: ?*Backend = null;
pub const io: std.Io = .{ .userdata = null, .vtable = &Backend.vtable };
pub const std_options_debug_io = io;
pub const std_options_FilePermissions = platform.Permissions;
pub const std_options_cwd = platform.cwd;

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
    io_state = try arena.?.allocator().create(Backend);
    io_state.?.* = .{};
}

/// Caller keeps this object alive and stationary until deinit. All dynamically
/// allocated std state uses the same bounded arena, including the env map.
pub const InitState = struct {
    permanent: std.heap.ArenaAllocator,
    environ_map: std.process.Environ.Map,
    argv: [startup.arg_count][*:0]const u8 = undefined,
    envp: [startup.env_count:null]?[*:0]const u8 = @splat(null),

    pub fn init(args: *const startup.Startup) !InitState {
        const a = os.heap.page_allocator;
        var result: InitState = .{ .permanent = .init(a), .environ_map = .init(a) };
        errdefer result.environ_map.deinit();
        for (args.args[0..args.argc], 0..) |arg, i| result.argv[i] = @ptrCast(arg.ptr);
        for (args.env[0..args.envc], 0..) |entry, i| {
            result.envp[i] = @ptrCast(entry.ptr);
            const equal = std.mem.indexOfScalar(u8, entry, '=').?;
            try result.environ_map.put(entry[0..equal], entry[equal + 1 ..]);
        }
        return result;
    }
    pub fn get(self: *InitState, argc: usize, envc: usize) std.process.Init {
        return .{
            .minimal = .{
                .args = .{ .vector = self.argv[0..argc] },
                .environ = .{ .block = .{ .slice = self.envp[0..envc :null] } },
            },
            .arena = &self.permanent,
            .gpa = os.heap.page_allocator,
            .io = io,
            .environ_map = &self.environ_map,
            .preopens = .empty, // The overlay declares only the borrowed /host token.
        };
    }
    pub fn deinit(self: *InitState) void {
        self.environ_map.deinit();
        self.permanent.deinit();
    }
};

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
    // Application-owned writers must be flushed explicitly before finish.
    // The diagnostic hook flushes on unlock; no hidden standard-stream buffer.
    if (io_state) |backend| {
        if (backend.debug_locked) backend.io().unlockStderr();
        backend.closeAll() catch fail("CloseFailed", 70);
    }
    native.exit(status);
}

test {
    _ = startup;
    _ = memory;
    _ = console;
    _ = io_helpers;
}
