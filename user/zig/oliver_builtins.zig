//! C1's five compiler support symbols, included in the guarded compilation.
//! Scalar volatile memory loops cannot lower into recursive libc calls.
//! Integer division is a bounded 128-step operation, not a hosted runtime.
pub export fn memcpy(dest: [*]u8, src: [*]const u8, len: usize) callconv(.c) [*]u8 {
    @setRuntimeSafety(false);
    const d: [*]volatile u8 = dest;
    const s: [*]const volatile u8 = src;
    for (0..len) |i| d[i] = s[i];
    return dest;
}
pub export fn memset(dest: [*]u8, value: c_int, len: usize) callconv(.c) [*]u8 {
    @setRuntimeSafety(false);
    const d: [*]volatile u8 = dest;
    for (0..len) |i| d[i] = @truncate(@as(c_uint, @bitCast(value)));
    return dest;
}
pub export fn memmove(dest: [*]u8, src: [*]const u8, len: usize) callconv(.c) [*]u8 {
    @setRuntimeSafety(false);
    const d: [*]volatile u8 = dest;
    const s: [*]const volatile u8 = src;
    if (@intFromPtr(dest) <= @intFromPtr(src)) {
        for (0..len) |i| d[i] = s[i];
    } else {
        var i = len;
        while (i > 0) {
            i -= 1;
            d[i] = s[i];
        }
    }
    return dest;
}

const Division = struct { quotient: u128, remainder: u128 };
fn divide(n: u128, d: u128) Division {
    if (d == 0) @panic("DivisionByZero");
    var q: u128 = 0;
    var r: u128 = 0;
    var i: usize = 128;
    while (i > 0) {
        i -= 1;
        const shift: u7 = @intCast(i);
        const carry = r >> 127 != 0;
        r = (r << 1) | ((n >> shift) & 1);
        if (carry or r >= d) {
            r -%= d;
            q |= @as(u128, 1) << shift;
        }
    }
    return .{ .quotient = q, .remainder = r };
}
pub export fn __udivti3(n: u128, d: u128) callconv(.c) u128 {
    return divide(n, d).quotient;
}
pub export fn __umodti3(n: u128, d: u128) callconv(.c) u128 {
    return divide(n, d).remainder;
}

test "C1 compiler support: copy, fill, overlap and u128 carry" {
    const std = @import("std");
    var bytes: [9]u8 = undefined;
    _ = memset(&bytes, 0x141, bytes.len);
    try std.testing.expectEqualSlices(u8, "AAAAAAAAA", &bytes);
    _ = memcpy(&bytes, "123456789", bytes.len);
    _ = memmove(bytes[1..].ptr, &bytes, 8);
    try std.testing.expectEqualSlices(u8, "112345678", &bytes);
    _ = memmove(&bytes, bytes[1..].ptr, 8);
    try std.testing.expectEqualSlices(u8, "123456788", &bytes);
    const cases = [_]u128{ 0, 1, 17, @as(u128, 1) << 127, std.math.maxInt(u128) };
    for (cases) |n| for (cases[1..]) |d| {
        const result = divide(n, d);
        // Independent identity avoids calling the exported division helpers.
        try std.testing.expect(result.remainder < d);
        try std.testing.expectEqual(n, result.quotient * d + result.remainder);
    };
}
