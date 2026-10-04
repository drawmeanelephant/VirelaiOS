const sdk = @import("sdk").runtime;

fn diagnostic(bytes: []const u8) i64 {
    return sdk.native.call(1, .{ 2, @intFromPtr(bytes.ptr), bytes.len, 0, 0, 0 });
}

pub fn invariant() noreturn {
    sdk.console.writeAll(diagnostic, "EngineInvariant\n") catch {};
    sdk.native.exit(71);
}
