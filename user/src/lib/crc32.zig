//! VirelaiOS Freestanding CRC-32 (ISO 3309 / ITU-T V.42 / PNG).
//!
//! Milestone 23 / Issue #824 (IMG3).
//! Polynomial 0xEDB88320, comptime table-driven, zero heap allocation.

/// Product version — the `ver M.m.p` the component-registry lib row
/// shows and the drift check resolves (docs/component-registry.md).
/// The registry's bump rule says when it moves.
pub const version = "1.0.0";

const std = @import("std");

pub const table = blk: {
    @setEvalBranchQuota(10000);
    var t: [256]u32 = undefined;
    var i: u32 = 0;
    while (i < 256) : (i += 1) {
        var c: u32 = i;
        var j: u32 = 0;
        while (j < 8) : (j += 1) {
            if ((c & 1) != 0) {
                c = 0xEDB88320 ^ (c >> 1);
            } else {
                c >>= 1;
            }
        }
        t[i] = c;
    }
    break :blk t;
};

/// Compute CRC-32 over a byte slice.
pub fn crc32(data: []const u8) u32 {
    var c: u32 = 0xFFFFFFFF;
    for (data) |b| {
        c = table[(c ^ b) & 0xFF] ^ (c >> 8);
    }
    return c ^ 0xFFFFFFFF;
}

/// Compute running CRC-32 across multiple buffers (pre-conditioned with 0xFFFFFFFF).
pub fn update(initial: u32, data: []const u8) u32 {
    var c: u32 = initial;
    for (data) |b| {
        c = table[(c ^ b) & 0xFF] ^ (c >> 8);
    }
    return c;
}

// ---------------------------------------------------------------------------
// Host Unit Tests
// ---------------------------------------------------------------------------

test "crc32: standard vectors" {
    // Empty buffer
    try std.testing.expectEqual(@as(u32, 0x00000000), crc32(""));

    // Standard check vector: "123456789" -> 0xCBF43926
    try std.testing.expectEqual(@as(u32, 0xCBF43926), crc32("123456789"));

    // Chunked update matches single-pass
    const chunk1 = "12345";
    const chunk2 = "6789";
    var running: u32 = 0xFFFFFFFF;
    running = update(running, chunk1);
    running = update(running, chunk2);
    try std.testing.expectEqual(@as(u32, 0xCBF43926), running ^ 0xFFFFFFFF);
}
