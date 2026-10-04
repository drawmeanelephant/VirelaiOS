//! Adapter-free, one-context native runtime. The adapter owns files and tty.
const std = @import("std");
const memory = @import("arena");
const allocation = @import("allocator.zig");
pub const policy = @import("control.zig");
pub const Error = policy.Error;
pub const Clock = policy.Clock;
pub const nativeClock = @import("clock.zig").native;
pub const limits = policy.limits;
pub const math = @import("math.zig");

const Engine = opaque {};
extern fn qjs_engine_create() callconv(.c) ?*Engine;
extern fn qjs_engine_eval(*Engine, [*:0]const u8, usize, [*:0]const u8, c_int) callconv(.c) c_int;
extern fn qjs_engine_destroy(*Engine) callconv(.c) c_int;
extern fn qjs_guard_high_water() callconv(.c) usize;

pub const Sink = struct {
    context: *anyopaque,
    write: *const fn (*anyopaque, []const u8) anyerror!usize,
    diagnostic: *const fn (*anyopaque, []const u8) anyerror!void,
    /// Runs at native polls. May cancel through Runtime.cancel but cannot
    /// allocate outside Runtime, re-enter eval, or perform blocking I/O.
    poll: ?*const fn (*anyopaque) bool = null,
};

pub const Result = struct {
    failure: ?Error = null,
    context_reset: bool = false,
    output_bytes: usize = 0,
    arena_peak_bytes: usize = 0,
    work_sites: u64 = 0,
    maximum_unchecked_ticks: u64 = 0,
    c_stack_high_water: usize = 0,
    counter_frequency: u64 = 0,
    start_counter: u64 = 0,
    end_counter: u64 = 0,
};

var current: ?*Runtime = null;

pub const Runtime = struct {
    arena: memory.Arena,
    bridge: allocation.Bridge,
    control: policy.Control,
    engine: ?*Engine = null,
    sink: ?Sink = null,
    in_boundary: bool = false,
    in_poll: bool = false,
    c_failed: bool = false,
    baseline_used: usize,

    pub fn create(arena: memory.Arena, clock: Clock) Error!*Runtime {
        if (current != null) return error.ContextLimit;
        if (arena.state.capacity != limits.arena) return error.OutOfMemory;
        const control = try policy.Control.init(clock);
        const baseline = arena.used();
        const self = arena.allocator().create(Runtime) catch return error.OutOfMemory;
        self.* = .{
            .arena = arena,
            .bridge = undefined,
            .control = control,
            .baseline_used = baseline,
        };
        self.bridge = allocation.Bridge.init(arena, .{ .context = self, .check = allocatorPoll });
        current = self;
        errdefer {
            self.control.cleaning = true;
            self.bridge.releaseAll();
            current = null;
            arena.allocator().destroy(self);
        }
        self.in_boundary = true;
        self.engine = qjs_engine_create();
        self.in_boundary = false;
        if (self.engine == null) return self.control.failure orelse error.OutOfMemory;
        return self;
    }

    fn allocatorPoll(context: *anyopaque) bool {
        const self: *Runtime = @ptrCast(@alignCast(context));
        return self.checkpoint();
    }

    fn checkpoint(self: *Runtime) bool {
        if (self.control.running and !self.control.cleaning and !self.in_poll) {
            if (self.sink) |sink| if (sink.poll) |poll| {
                self.in_poll = true;
                defer self.in_poll = false;
                if (poll(sink.context)) self.control.cancel();
            };
        }
        return self.control.poll();
    }

    /// Adapter buffers are charged to this same arena and survive automatic
    /// context reset. Release them before destroy. No raw SDK allocation may
    /// change while the runtime lives.
    pub fn allocate(self: *Runtime, bytes: usize) Error![]u8 {
        if (current != self) return error.ContextLimit;
        const before = self.bridge.engine_allocations;
        self.bridge.engine_allocations = false;
        defer self.bridge.engine_allocations = before;
        const ptr = self.bridge.allocate(bytes) orelse {
            self.control.refuse(error.OutOfMemory);
            return self.control.failure orelse error.OutOfMemory;
        };
        return ptr[0..bytes];
    }

    pub fn release(self: *Runtime, bytes: []u8) void {
        self.bridge.release(bytes.ptr);
    }

    pub fn cancel(self: *Runtime) void {
        self.control.cancel();
    }

    fn validateSource(self: *Runtime, source: []const u8) Error!void {
        var index: usize = 0;
        while (index < source.len) {
            if (index % 1024 < 4 and !self.checkpoint())
                return self.control.failure.?;
            const length = std.unicode.utf8ByteSequenceLength(source[index]) catch return error.InvalidUtf8;
            if (length > source.len - index) return error.InvalidUtf8;
            _ = std.unicode.utf8Decode(source[index .. index + length]) catch return error.InvalidUtf8;
            index += length;
        }
    }

    pub fn eval(self: *Runtime, source: []const u8, filename: []const u8, sink: Sink, emit_result: bool) Error!Result {
        if (current != self or self.engine == null or self.in_boundary) return error.ContextLimit;
        if (filename.len == 0 or filename.len > limits.filename or std.mem.indexOfScalar(u8, filename, 0) != null)
            return error.InvalidFilename;
        if (self.control.failure != null) {
            self.control.output = 0;
            self.control.work_sites = 0;
            self.control.maximum_unchecked_ticks = 0;
            return self.refusalResult();
        }
        try self.control.begin(source.len);
        self.sink = sink;
        defer self.sink = null;
        defer self.control.finish();
        self.validateSource(source) catch |err| {
            self.control.refuse(err);
            return self.refusalResult();
        };
        const text = self.allocate(source.len + 1) catch return self.refusalResult();
        const name = self.allocate(filename.len + 1) catch {
            self.release(text);
            return self.refusalResult();
        };
        var offset: usize = 0;
        while (offset < source.len) {
            if (!self.checkpoint()) break;
            const end = @min(source.len, offset + 1024);
            @memcpy(text[offset..end], source[offset..end]);
            offset = end;
        }
        text[source.len] = 0;
        @memcpy(name[0..filename.len], filename);
        name[filename.len] = 0;
        var code: c_int = -2;
        if (self.control.failure == null) {
            self.in_boundary = true;
            code = qjs_engine_eval(self.engine.?, @ptrCast(text.ptr), source.len, @ptrCast(name.ptr), @intFromBool(emit_result));
            self.in_boundary = false;
        }
        self.release(name);
        self.release(text);
        if (self.control.failure != null or code == -2) return self.refusalResult();
        return .{
            .failure = if (code == -1) error.JSException else null,
            .output_bytes = self.control.output,
            .arena_peak_bytes = self.arena.peak(),
            .work_sites = self.control.work_sites,
            .maximum_unchecked_ticks = self.control.maximum_unchecked_ticks,
            .c_stack_high_water = qjs_guard_high_water(),
            .counter_frequency = self.control.clock.frequency,
            .start_counter = self.control.start,
            .end_counter = self.control.clock.counter(self.control.clock.context),
        };
    }

    fn refusalResult(self: *Runtime) Error!Result {
        const failure = self.control.failure orelse error.OutOfMemory;
        var result: Result = .{
            .failure = failure,
            .context_reset = true,
            .output_bytes = self.control.output,
            .arena_peak_bytes = self.arena.peak(),
            .work_sites = self.control.work_sites,
            .maximum_unchecked_ticks = self.control.maximum_unchecked_ticks,
            .c_stack_high_water = qjs_guard_high_water(),
            .counter_frequency = self.control.clock.frequency,
            .start_counter = self.control.start,
        };
        try self.reset();
        result.maximum_unchecked_ticks = self.control.maximum_unchecked_ticks;
        result.c_stack_high_water = qjs_guard_high_water();
        result.end_counter = self.control.clock.counter(self.control.clock.context);
        return result;
    }

    /// On a nonlocal C refusal the graph is poisoned. Do not run its finalizers
    /// or chase partially updated links. All C storage is arena-owned and its
    /// independent allocation registry is the complete reclamation root.
    pub fn reset(self: *Runtime) Error!void {
        if (current != self or self.in_boundary) return error.ContextLimit;
        self.control.cleaning = true;
        self.engine = null;
        self.bridge.releaseEngine();
        self.bridge.oom = false;
        self.control.recovered();
        self.in_boundary = true;
        self.engine = qjs_engine_create();
        self.in_boundary = false;
        if (self.engine == null) {
            self.control.cleaning = true;
            self.bridge.releaseEngine();
            return self.control.failure orelse error.OutOfMemory;
        }
    }

    pub fn destroy(self: *Runtime) Error!void {
        std.debug.assert(current == self and !self.in_boundary);
        self.control.cleaning = true;
        // Clean contexts prove ordinary QuickJS teardown. Poisoned contexts
        // are only reclaimed through the separate registry.
        var failed = false;
        if (self.engine) |engine| {
            self.in_boundary = true;
            failed = qjs_engine_destroy(engine) != 0;
            self.in_boundary = false;
        }
        self.bridge.releaseAll();
        const arena = self.arena;
        const baseline = self.baseline_used;
        const failure = self.control.failure;
        current = null;
        arena.allocator().destroy(self);
        std.debug.assert(arena.used() == baseline);
        if (failed) return failure orelse error.StackLimit;
    }

    pub fn checkResources(_: *Runtime, file_count: usize, record_count: usize) Error!void {
        if (file_count > limits.files or record_count > limits.resources) return error.HandleLimit;
    }

    pub fn chargeReceipt(self: *Runtime, bytes: usize) Error!void {
        self.control.chargeReceipt(bytes) catch |err| {
            self.control.refuse(err);
            return err;
        };
    }
};

fn malloc(bytes: usize) callconv(.c) ?*anyopaque {
    const self = current orelse return null;
    self.bridge.engine_allocations = true;
    defer self.bridge.engine_allocations = false;
    const ptr = self.bridge.allocate(bytes) orelse {
        self.control.refuse(error.OutOfMemory);
        return null;
    };
    return ptr;
}
fn free(ptr: ?*anyopaque) callconv(.c) void {
    const self = current orelse {
        std.debug.assert(ptr == null);
        return;
    };
    self.bridge.release(if (ptr) |p| @ptrCast(p) else null);
}
fn realloc(ptr: ?*anyopaque, bytes: usize) callconv(.c) ?*anyopaque {
    const self = current orelse return null;
    self.bridge.engine_allocations = true;
    defer self.bridge.engine_allocations = false;
    const result = self.bridge.resize(if (ptr) |p| @ptrCast(p) else null, bytes);
    if (result == null and bytes != 0) self.control.refuse(error.OutOfMemory);
    return result;
}
fn malloc_usable_size(ptr: ?*const anyopaque) callconv(.c) usize {
    const self = current orelse return 0;
    return self.bridge.usable(if (ptr) |p| @ptrCast(p) else null);
}
export fn qjs_native_checkpoint() c_int {
    const self = current orelse return 0;
    return @intFromBool(!self.checkpoint());
}
export fn qjs_native_charge_interrupt_site() c_int {
    const self = current orelse return 0;
    return @intFromBool(!self.control.site());
}
export fn qjs_native_hosted_diagnostic() void {
    if (current) |self| self.control.refuse(error.UnsupportedHostedDiagnostic);
}
export fn qjs_native_unsupported_feature() void {
    if (current) |self| self.control.refuse(error.UnsupportedFeature);
}
export fn qjs_native_stack_failure() void {
    if (current) |self| self.control.refuse(error.StackLimit);
}
export fn qjs_native_out_of_memory() void {
    if (current) |self| self.control.refuse(error.OutOfMemory);
}
export fn qjs_native_emit(ptr: [*]const u8, bytes: usize, diagnostic: c_int) c_int {
    const self = current orelse return -1;
    const sink = self.sink orelse return -1;
    if (diagnostic != 0) {
        self.control.chargeDiagnostic(bytes) catch |err| {
            self.control.refuse(err);
            return -1;
        };
        sink.diagnostic(sink.context, ptr[0..bytes]) catch {
            self.control.refuse(error.WriteFailed);
            return -1;
        };
    } else {
        if (!self.control.chargeOutput(bytes)) return -1;
        var offset: usize = 0;
        while (offset < bytes) {
            if (!self.checkpoint()) return -1;
            const chunk = ptr[offset..@min(bytes, offset + 256)];
            const count = sink.write(sink.context, chunk) catch {
                self.control.refuse(error.WriteFailed);
                return -1;
            };
            if (count == 0 or count > chunk.len) {
                self.control.refuse(error.WriteFailed);
                return -1;
            }
            offset += count;
        }
    }
    return @intFromBool(!self.checkpoint());
}
export fn abort() noreturn {
    @import("hooks").invariant();
}
export fn qjs_native_assert() noreturn {
    @import("hooks").invariant();
}

comptime {
    _ = math;
    const prefix = if (@import("builtin").os.tag == .freestanding) "" else "qjs_host_";
    @export(&malloc, .{ .name = prefix ++ "malloc" });
    @export(&free, .{ .name = prefix ++ "free" });
    @export(&realloc, .{ .name = prefix ++ "realloc" });
    @export(&malloc_usable_size, .{ .name = prefix ++ "malloc_usable_size" });
}
