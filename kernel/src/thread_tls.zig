//! Static AArch64 local-exec TLS preparation, not a loader or thread runtime.
//! The caller supplies a validated template and real accessible buffers.
//! No allocation, ELF acceptance, TP register writes, or production activation.
const std = @import("std");

pub const prefix_bytes: usize = 16;
pub const max_alignment: usize = 4096;
pub const Error = error{
    InvalidAlignment,
    InvalidSize,
    Overflow,
    InsufficientCapacity,
    Overlap,
};

pub const Layout = struct {
    alignment: usize,
    tls_offset: usize,
    memory_size: usize,
    /// Includes the zeroed prefix, alignment gaps and trailing stride padding.
    size: usize,
};

fn rounded(value: usize, alignment: usize) Error!usize {
    const padding = (0 -% value) & (alignment - 1);
    return std.math.add(usize, value, padding) catch error.Overflow;
}

/// Alignment is a nonzero power of two, at most one native page. Capacity is
/// caller policy, not a new kernel resource budget. Empty templates are valid.
pub fn plan(initialized_size: usize, memory_size: usize, alignment: usize, capacity: usize) Error!Layout {
    if (alignment == 0 or alignment > max_alignment or !std.math.isPowerOfTwo(alignment))
        return error.InvalidAlignment;
    if (initialized_size > memory_size) return error.InvalidSize;
    const tls_offset = try rounded(prefix_bytes, alignment);
    const used = std.math.add(usize, tls_offset, memory_size) catch return error.Overflow;
    const buffer_alignment = @max(prefix_bytes, alignment);
    const size = try rounded(used, buffer_alignment);
    if (size > capacity) return error.InsufficientCapacity;
    return .{ .alignment = buffer_alignment, .tls_offset = tls_offset, .memory_size = memory_size, .size = size };
}

/// Validates every extent before writing. Source must not overlap the output
/// span; adjacent storage and unused destination capacity are not modified.
/// Recompute the plan here rather than trust a caller-constructed Layout.
pub fn initialize(template: []const u8, memory_size: usize, alignment: usize, destination: []u8) Error!Layout {
    const layout = try plan(template.len, memory_size, alignment, destination.len);
    const target = @intFromPtr(destination.ptr);
    if (target & (layout.alignment - 1) != 0) return error.InvalidAlignment;
    _ = std.math.add(usize, target, destination.len) catch return error.Overflow;
    const output_end = std.math.add(usize, target, layout.size) catch return error.Overflow;
    const source = @intFromPtr(template.ptr);
    const source_end = std.math.add(usize, source, template.len) catch return error.Overflow;
    if (template.len != 0 and source < output_end and target < source_end) return error.Overlap;

    @memset(destination[0..layout.size], 0);
    @memcpy(destination[layout.tls_offset..][0..template.len], template);
    return layout;
}

test "thread TLS: variant-I prefix, alignment and stride" {
    for ([_]usize{ 1, 2, 4, 8, 16, 32, 64, 4096 }) |alignment| {
        const layout = try plan(1, 1, alignment, 8192);
        const offset = @max(prefix_bytes, alignment);
        try std.testing.expectEqual(offset, layout.tls_offset);
        try std.testing.expectEqual(offset, layout.alignment);
        try std.testing.expectEqual(2 * offset, layout.size);
        try std.testing.expectEqual(@as(usize, 1), layout.memory_size);
    }
    const mixed = try plan(8, 83, 64, 192);
    try std.testing.expectEqual(@as(usize, 64), mixed.tls_offset);
    try std.testing.expectEqual(@as(usize, 192), mixed.size);
    const empty = try plan(0, 0, 1, prefix_bytes);
    try std.testing.expectEqual(prefix_bytes, empty.size);
}

test "thread TLS: exact capacity, size and alignment refusals" {
    _ = try plan(8, 83, 64, 192);
    try std.testing.expectError(error.InsufficientCapacity, plan(8, 83, 64, 191));
    try std.testing.expectError(error.InsufficientCapacity, plan(0, 0, 1, 0));
    try std.testing.expectError(error.InvalidSize, plan(9, 8, 8, 4096));
    for ([_]usize{ 0, 3, 24, 4097, 8192, std.math.maxInt(usize) }) |alignment|
        try std.testing.expectError(error.InvalidAlignment, plan(0, 0, alignment, 4096));
}

test "thread TLS: checked extent and rounding overflow" {
    const maximum = std.math.maxInt(usize);
    try std.testing.expectError(error.Overflow, plan(0, maximum, 1, maximum));
    try std.testing.expectError(error.Overflow, plan(0, maximum - prefix_bytes, 1, maximum));
    const exact = try plan(0, maximum - 31, 1, maximum);
    try std.testing.expectEqual(maximum - 15, exact.size);
    try std.testing.expectError(error.Overflow, plan(0, maximum - max_alignment, max_alignment, maximum));
}

test "thread TLS: initialized bytes, zero fill and untouched spare capacity" {
    const template = [_]u8{ 0xf0, 0xde, 0xbc, 0x9a, 0x78, 0x56, 0x34, 0x12 };
    var buffer: [256]u8 align(64) = @splat(0xa5);
    const layout = try initialize(&template, 83, 64, &buffer);
    try std.testing.expectEqualSlices(u8, &template, buffer[64..72]);
    for (buffer[0..64]) |byte| try std.testing.expectEqual(@as(u8, 0), byte);
    for (buffer[72..192]) |byte| try std.testing.expectEqual(@as(u8, 0), byte);
    for (buffer[192..]) |byte| try std.testing.expectEqual(@as(u8, 0xa5), byte);
    try std.testing.expectEqual(@as(usize, 192), layout.size);
}

test "thread TLS: zero-only, initialized-only and independent storage" {
    var first: [64]u8 align(16) = @splat(0xa5);
    var second: [64]u8 align(16) = @splat(0xb6);
    const zero = try initialize(&.{}, 19, 8, &first);
    for (first[0..zero.size]) |byte| try std.testing.expectEqual(@as(u8, 0), byte);
    const template = [_]u8{ 1, 2, 3, 4 };
    const layout = try initialize(&template, template.len, 4, &first);
    _ = try initialize(&template, template.len, 4, &second);
    first[layout.tls_offset] = 9;
    try std.testing.expectEqual(@as(u8, 1), second[layout.tls_offset]);
    try std.testing.expectEqualSlices(u8, &template, second[16..20]);
    const empty = try initialize(&.{}, 0, 1, &first);
    for (first[0..empty.size]) |byte| try std.testing.expectEqual(@as(u8, 0), byte);
}

test "thread TLS: validation refusals leave the whole buffer unchanged" {
    var buffer: [256]u8 align(64) = @splat(0xa5);
    const before = buffer;
    const template = [_]u8{ 1, 2, 3, 4 };
    for ([_]usize{ 0, 3, 8192 }) |alignment| {
        try std.testing.expectError(error.InvalidAlignment, initialize(&template, 4, alignment, &buffer));
        try std.testing.expectEqualSlices(u8, &before, &buffer);
    }
    try std.testing.expectError(error.InvalidSize, initialize(&template, 3, 8, &buffer));
    try std.testing.expectEqualSlices(u8, &before, &buffer);
    try std.testing.expectError(error.InsufficientCapacity, initialize(&template, 83, 64, buffer[0..191]));
    try std.testing.expectEqualSlices(u8, &before, &buffer);
    try std.testing.expectError(error.InvalidAlignment, initialize(&template, 4, 16, buffer[1..]));
    try std.testing.expectEqualSlices(u8, &before, &buffer);
    try std.testing.expectError(error.Overflow, initialize(&template, std.math.maxInt(usize), 16, &buffer));
    try std.testing.expectEqualSlices(u8, &before, &buffer);
}

test "thread TLS: overlapping prefix, template, zero tail and crossing ranges refuse without writes" {
    var buffer: [256]u8 align(64) = @splat(0xa5);
    const before = buffer;
    for ([_][2]usize{ .{ 64, 68 }, .{ 80, 84 }, .{ 108, 112 }, .{ 60, 68 }, .{ 108, 116 } }) |range| {
        try std.testing.expectError(error.Overlap, initialize(buffer[range[0]..range[1]], 32, 16, buffer[64..128]));
        try std.testing.expectEqualSlices(u8, &before, &buffer);
    }
    // Identical spans refuse too, before zeroing the source.
    try std.testing.expectError(error.Overlap, initialize(buffer[64..128], 64, 16, buffer[64..144]));
    try std.testing.expectEqualSlices(u8, &before, &buffer);
}

test "thread TLS: adjacent source and unused destination capacity remain intact" {
    var buffer: [192]u8 align(64) = @splat(0xa5);
    _ = try initialize(buffer[60..64], 4, 16, buffer[64..128]);
    for (buffer[60..64]) |byte| try std.testing.expectEqual(@as(u8, 0xa5), byte);
    _ = try initialize(buffer[96..100], 4, 16, buffer[64..128]);
    try std.testing.expectEqualSlices(u8, &.{ 0xa5, 0xa5, 0xa5, 0xa5 }, buffer[80..84]);
    for (buffer[96..]) |byte| try std.testing.expectEqual(@as(u8, 0xa5), byte);
}

test "thread TLS: address extent overflow refuses before touching memory" {
    var buffer: [64]u8 align(16) = @splat(0xa5);
    const before = buffer;
    const source: [*]const u8 = @ptrFromInt(std.math.maxInt(usize) - 7);
    try std.testing.expectError(error.Overflow, initialize(source[0..16], 16, 16, &buffer));
    try std.testing.expectEqualSlices(u8, &before, &buffer);
    const destination: [*]u8 = @ptrFromInt(std.math.maxInt(usize) - 15);
    try std.testing.expectError(error.Overflow, initialize(&.{}, 0, 16, destination[0..32]));
}
