//! Compile-only static TLS fixture. Never staged or run in a guest.
const options = @import("tls_options");

threadlocal var initialized: u64 = 0x1234_5678_9abc_def0;
threadlocal var zeroed: [19]u8 align(options.alignment) = @splat(0);
var keep_data: usize = 1;

export fn tls_address() *u64 {
    return &initialized;
}

export fn tls_zero_address() *[19]u8 {
    return &zeroed;
}

export fn tls_read() u64 {
    return initialized;
}

export fn tls_write(value: u64) void {
    initialized = value;
}

export fn _start() callconv(.c) noreturn {
    const sink: *volatile usize = &keep_data;
    sink.* = @intFromPtr(tls_address()) + @intFromPtr(tls_zero_address());
    while (true) asm volatile ("wfe");
}
