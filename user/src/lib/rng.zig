//! M51 SSH-P1 (issue #1166, ADR 0025 D5): the EL0 entropy wrapper.
//!
//! Thin userland wrapper over ADR 0007 slot 72 `sys_getrandom(buf, len)`.
//! The kernel owns the CSPRNG (`kernel/src/csprng.zig`); this only READS it:
//! it never seeds, reseeds, or weakens the stream. The SSH client uses it for
//! the ephemeral X25519 secret, the KEXINIT cookie, and per-packet padding;
//! the GOOS=virelai Go runtime (#1163) consumes the same shared slot for its
//! hash seed. No capability is required — every principal may read entropy.

/// Product version — the `ver M.m.p` the component-registry lib row
/// shows and the drift check resolves (docs/component-registry.md).
/// The registry's bump rule says when it moves.
pub const version = "1.0.0";

const std = @import("std");
// Module-mapped dep (the `ssh/` pattern): the wrapper is its own module in
// the SSH.BIN graph, so `ui` arrives as a mapped module. Resolving
// `ui/abi.zig` by path here would load the ui tree into this module too and
// collide with the `ui` module that `userauth.zig` also imports.
const abi = @import("ui").abi;

/// ADR 0007 slot 72: `sys_getrandom(buf, len)`.
pub const sys_getrandom_num: u64 = 72;

/// The kernel's per-call fill cap (`syscall.getrandom_max`). A longer
/// request is clamped by the kernel, never refused; loop for more.
pub const max_len: usize = 256;

/// The bytes one call fills for a request of `len`: 0 for an empty request,
/// otherwise `min(len, max_len)`. Pure, so the clamp contract is host-tested
/// without a running kernel.
pub fn fillLen(len: usize) usize {
    return @min(len, max_len);
}

/// Fill `buf` from the kernel CSPRNG. Returns the bytes written
/// (`fillLen(buf.len)`), or a negative ADR 0007 error: `EFAULT` (-3) for a
/// bad buffer, `EINVAL` (-1) for a non-process caller. An empty buffer
/// returns 0 without issuing a call.
pub fn getrandom(buf: []u8) i64 {
    const take = fillLen(buf.len);
    if (take == 0) return 0;
    return abi.syscall2(sys_getrandom_num, @intFromPtr(buf.ptr), @intCast(take));
}

test "rng: fillLen clamps to the kernel cap and keeps the empty request at 0" {
    try std.testing.expectEqual(@as(usize, 0), fillLen(0));
    try std.testing.expectEqual(@as(usize, 1), fillLen(1));
    try std.testing.expectEqual(max_len, fillLen(max_len));
    try std.testing.expectEqual(max_len, fillLen(max_len + 1));
    try std.testing.expectEqual(max_len, fillLen(1 << 20));
}
