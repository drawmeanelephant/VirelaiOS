//! ADR 0007 calls, not libc/POSIX. No host success-shaped syscall substitutes.
const builtin = @import("builtin");

pub fn call(number: u64, args: [6]u64) i64 {
    if (builtin.os.tag != .freestanding or builtin.cpu.arch != .aarch64)
        @compileError("VirelaiNativeTargetRequired");
    return asm volatile ("svc #0"
        : [result] "={x0}" (-> i64),
        : [number] "{x8}" (number),
          [a0] "{x0}" (args[0]),
          [a1] "{x1}" (args[1]),
          [a2] "{x2}" (args[2]),
          [a3] "{x3}" (args[3]),
          [a4] "{x4}" (args[4]),
          [a5] "{x5}" (args[5]),
        : .{ .memory = true });
}

pub fn map(bytes: usize) ?[]align(4096) u8 {
    const result = call(63, .{ 0, bytes, 3, 0x8022, 0, 0 });
    if (result <= 0) return null;
    const ptr: [*]align(4096) u8 = @ptrFromInt(@as(usize, @intCast(result)));
    return ptr[0..bytes];
}

pub fn consoleChunk(bytes: []const u8) i64 {
    return call(1, .{ 1, @intFromPtr(bytes.ptr), bytes.len, 0, 0, 0 });
}

pub fn exit(status: u8) noreturn {
    _ = call(3, .{ status, 0, 0, 0, 0, 0 });
    while (true) asm volatile ("wfe");
}
