//! B5's allocation-free explicit-context subset, not std.Thread or Io.Threaded.
//! Caller owns disjoint stack/TLS storage until successful join. No detach,
//! cancellation, allocator use, compiler threadlocal, or hosted TLS loader.
//! Body offsets follow pinned AArch64 static local-exec (variant I).
const native = @import("native");
pub const tls = @import("thread_tls");
pub const Error = error{ Capacity, InvalidContext, InvalidJoin, UnsupportedPrecision, DeadlineOverflow, WaitUnavailable };
pub const Entry = *const fn (usize) u8;
const Header = extern struct { entry: usize, arg: usize };
comptime {
    if (@sizeOf(Header) != tls.prefix_bytes) @compileError("ThreadPrefixDrift");
}

pub const Thread = struct {
    token: u64,

    /// The stack is borrowed, at least 4 KiB and 16-byte aligned. The TLS
    /// buffer must meet the planner's alignment/capacity rules. The complete
    /// buffers must be mapped writable and disjoint, including the template.
    pub fn spawn(stack: []align(16) u8, template: []const u8, memory_size: usize, alignment: usize, buffer: []u8, entry: Entry, arg: usize) (Error || tls.Error)!Thread {
        if (stack.len < 4096 or stack.len % 16 != 0) return error.InvalidContext;
        const layout = try tls.plan(template.len, memory_size, alignment, buffer.len);
        if (overlap(stack, buffer[0..layout.size]) or overlap(stack, template)) return error.InvalidContext;
        _ = try tls.initialize(template, memory_size, alignment, buffer);
        const header: *Header = @ptrCast(@alignCast(buffer.ptr));
        header.* = .{ .entry = @intFromPtr(entry), .arg = arg };
        const result = native.call(73, .{ 0, @intFromPtr(&trampoline), @intFromPtr(stack.ptr) + stack.len, arg, @intFromPtr(buffer.ptr), 0 });
        if (result == -11) return error.Capacity;
        if (result < 0) return error.InvalidContext;
        return .{ .token = @intCast(result) };
    }

    /// Successful join consumes the handle and orders the child's writes.
    /// Kernel exception-stack pages are reclaimed by the idle reaper.
    pub fn join(self: *Thread) Error!u8 {
        if (self.token == 0) return error.InvalidJoin;
        const result = native.call(73, .{ 2, self.token, 0, 0, 0, 0 });
        if (result < 0 or result > 255) return error.InvalidJoin;
        self.token = 0;
        return @intCast(result);
    }
};

fn overlap(a: []const u8, b: []const u8) bool {
    return a.len != 0 and b.len != 0 and
        @intFromPtr(a.ptr) < @intFromPtr(b.ptr) + b.len and
        @intFromPtr(b.ptr) < @intFromPtr(a.ptr) + a.len;
}

pub fn context() [*]u8 {
    return @ptrFromInt(asm volatile ("mrs %[v], tpidr_el0"
        : [v] "=r" (-> usize),
        :
        : .{ .memory = true }));
}

/// Initialize/attach the primary task before any TP-relative access.
pub fn attach(template: []const u8, memory_size: usize, alignment: usize, buffer: []u8) (Error || tls.Error)!void {
    _ = try tls.initialize(template, memory_size, alignment, buffer);
    if (native.call(73, .{ 3, @intFromPtr(buffer.ptr), 0, 0, 0, 0 }) != 0) return error.InvalidContext;
}

fn trampoline(_: usize) callconv(.c) noreturn {
    const header: *const Header = @ptrCast(@alignCast(context()));
    const entry: Entry = @ptrFromInt(header.entry);
    const status = entry(header.arg);
    _ = native.call(73, .{ 1, status, 0, 0, 0, 0 });
    native.exit(70); // an exit refusal is fatal, not a returned child
}

pub const Wait = enum { woken, changed, timed_out };

/// Relative duration, whole seconds only, zero = indefinite. Native op 2
/// includes a phase-guard tick, never a fine-wait success or silent rounding.
/// Wake is advisory: always recheck the acquire-loaded condition in a loop.
pub fn wait(word: *u32, expected: u32, timeout_ns: u64) Error!Wait {
    _ = tls.coarse_deadline(0, timeout_ns) catch |err| return switch (err) {
        error.UnsupportedPrecision => error.UnsupportedPrecision,
        error.Overflow => error.DeadlineOverflow,
    };
    return switch (native.call(74, .{ 2, @intFromPtr(word), expected, timeout_ns, 0, 0 })) {
        0 => .woken,
        -11 => .changed,
        -12 => .timed_out,
        else => error.WaitUnavailable,
    };
}

pub fn wake(word: *u32, count: usize) Error!usize {
    const result = native.call(74, .{ 1, @intFromPtr(word), count, 0, 0, 0 });
    if (result < 0) return error.WaitUnavailable;
    return @intCast(result);
}

/// Optional caller-owned mutex; no allocator/global SDK synchronization.
pub const Mutex = struct {
    word: u32 = 0,

    pub fn lock(self: *Mutex) Error!void {
        while (@cmpxchgStrong(u32, &self.word, 0, 1, .acquire, .monotonic) != null) {
            _ = try wait(&self.word, 1, 0);
        }
    }

    pub fn unlock(self: *Mutex) Error!void {
        @atomicStore(u32, &self.word, 0, .release);
        _ = try wake(&self.word, 1);
    }
};
