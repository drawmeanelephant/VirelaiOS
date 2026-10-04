//! C math bridge. Transcendental compiler intrinsics use pinned compiler-rt,
//! never system libm. Do not define sin/exp wrappers that call themselves.
const std = @import("std");
const m = std.math;

pub const Unary = *const fn (f64) callconv(.c) f64;
pub const Binary = *const fn (f64, f64) callconv(.c) f64;
pub const names = [_][]const u8{
    "acos",  "acosh", "asin",  "asinh", "atan",  "atan2", "atanh", "cbrt",
    "ceil",  "cos",   "cosh",  "exp",   "expm1", "fabs",  "floor", "fmax",
    "fmin",  "fmod",  "hypot", "log",   "log10", "log1p", "log2",  "lrint",
    "modf",  "pow",   "round", "sin",   "sinh",  "sqrt",  "tan",   "tanh",
    "trunc",
};

// These seven definitions belong to the checked Zig compiler-rt closure.
pub extern fn sin(f64) callconv(.c) f64;
pub extern fn cos(f64) callconv(.c) f64;
pub extern fn tan(f64) callconv(.c) f64;
pub extern fn exp(f64) callconv(.c) f64;
pub extern fn log(f64) callconv(.c) f64;
pub extern fn log2(f64) callconv(.c) f64;
pub extern fn log10(f64) callconv(.c) f64;
extern fn qjs_native_checkpoint() callconv(.c) c_int;

/// IEEE binary64 remainder by shift/subtract. The finite exponent difference
/// is at most 2097; poll each step instead of leaving compiler-rt's long
/// remainder loop unchecked. No division intrinsic can recurse into fmod.
pub export fn fmod(x: f64, y: f64) f64 {
    if (qjs_native_checkpoint() != 0) return m.nan(f64);
    if (y == 0 or m.isNan(x) or m.isNan(y) or m.isInf(x)) return m.nan(f64);
    var ux: u64 = @bitCast(x);
    var uy: u64 = @bitCast(y);
    const sign = ux & (1 << 63);
    ux &= ~(@as(u64, 1) << 63);
    uy &= ~(@as(u64, 1) << 63);
    if (ux <= uy) return if (ux == uy) m.copysign(@as(f64, 0), x) else x;
    var ex: i32 = @intCast((ux >> 52) & 0x7ff);
    var ey: i32 = @intCast((uy >> 52) & 0x7ff);
    if (ex == 0) {
        while (ux < 1 << 52) : (ex -= 1) {
            if (qjs_native_checkpoint() != 0) return m.nan(f64);
            ux <<= 1;
        }
        ex += 1;
    } else {
        ux = (ux & ((1 << 52) - 1)) | (1 << 52);
    }
    if (ey == 0) {
        while (uy < 1 << 52) : (ey -= 1) {
            if (qjs_native_checkpoint() != 0) return m.nan(f64);
            uy <<= 1;
        }
        ey += 1;
    } else {
        uy = (uy & ((1 << 52) - 1)) | (1 << 52);
    }
    while (ex > ey) : (ex -= 1) {
        if (qjs_native_checkpoint() != 0) return m.nan(f64);
        if (ux >= uy) ux -= uy;
        if (ux == 0) return @bitCast(sign);
        ux <<= 1;
    }
    if (ux >= uy) ux -= uy;
    if (ux == 0) return @bitCast(sign);
    while (ux < 1 << 52) : (ex -= 1) {
        if (qjs_native_checkpoint() != 0) return m.nan(f64);
        ux <<= 1;
    }
    if (ex > 0) ux = (ux - (1 << 52)) | (@as(u64, @intCast(ex)) << 52) else ux >>= @intCast(1 - ex);
    return @bitCast(ux | sign);
}

pub export fn acos(x: f64) f64 {
    return m.acos(x);
}
pub export fn acosh(x: f64) f64 {
    return m.acosh(x);
}
pub export fn asin(x: f64) f64 {
    return m.asin(x);
}
pub export fn asinh(x: f64) f64 {
    return m.asinh(x);
}
pub export fn atan(x: f64) f64 {
    return m.atan(x);
}
pub export fn atan2(y: f64, x: f64) f64 {
    return m.atan2(y, x);
}
pub export fn atanh(x: f64) f64 {
    return m.atanh(x);
}
pub export fn cbrt(x: f64) f64 {
    return m.cbrt(x);
}
pub export fn ceil(x: f64) f64 {
    return @ceil(x);
}
pub export fn cosh(x: f64) f64 {
    return m.cosh(x);
}
pub export fn expm1(x: f64) f64 {
    return m.expm1(x);
}
pub export fn fabs(x: f64) f64 {
    return @abs(x);
}
pub export fn floor(x: f64) f64 {
    return @floor(x);
}
pub export fn fmax(x: f64, y: f64) f64 {
    if (m.isNan(x)) return y;
    if (m.isNan(y)) return x;
    if (x == 0 and y == 0) return if (m.signbit(x) and m.signbit(y)) -0.0 else 0.0;
    return if (x > y) x else y;
}
pub export fn fmin(x: f64, y: f64) f64 {
    if (m.isNan(x)) return y;
    if (m.isNan(y)) return x;
    if (x == 0 and y == 0) return if (m.signbit(x) or m.signbit(y)) -0.0 else 0.0;
    return if (x < y) x else y;
}
pub export fn hypot(x: f64, y: f64) f64 {
    return m.hypot(x, y);
}
pub export fn log1p(x: f64) f64 {
    return m.log1p(x);
}
pub export fn lrint(x: f64) c_long {
    // nearest/ties-even, independent of an unprovided fenv API.
    if (!m.isFinite(x) or x >= 0x1p63 or x < -0x1p63) return m.minInt(c_long);
    if (@abs(x) >= 0x1p52) return @intFromFloat(x);
    const base = @floor(x);
    const fractional = x - base;
    const integer: c_long = @intFromFloat(base);
    return integer + @as(c_long, if (fractional > 0.5 or (fractional == 0.5 and integer & 1 != 0)) 1 else 0);
}
pub export fn modf(x: f64, integer: *f64) f64 {
    if (m.isNan(x)) {
        integer.* = x;
        return x;
    }
    integer.* = @trunc(x);
    if (m.isInf(x)) return m.copysign(@as(f64, 0), x);
    const fractional = x - integer.*;
    return if (fractional == 0) m.copysign(@as(f64, 0), x) else fractional;
}
pub export fn pow(x: f64, y: f64) f64 {
    return m.pow(f64, x, y);
}
pub export fn round(x: f64) f64 {
    return @round(x);
}
pub export fn sinh(x: f64) f64 {
    return m.sinh(x);
}
pub export fn sqrt(x: f64) f64 {
    return @sqrt(x);
}
pub export fn tanh(x: f64) f64 {
    return m.tanh(x);
}
pub export fn trunc(x: f64) f64 {
    return @trunc(x);
}

test "C bridge rounding distinguishes nearest-even from ties-away" {
    for ([_]f64{ 0.5, 1.5, 2.5, -0.5, -1.5, -2.5 }, [_]c_long{ 0, 2, 2, 0, -2, -2 }) |x, expected|
        try std.testing.expectEqual(expected, lrint(x));
    for ([_]f64{ 0.5, 1.5, 2.5, -0.5, -1.5, -2.5 }, [_]f64{ 1, 2, 3, -1, -2, -3 }) |x, expected|
        try std.testing.expectEqual(expected, round(x));
    try std.testing.expect(m.signbit(round(-0.0)));
    try std.testing.expect(m.signbit(trunc(-0.5)));
    try std.testing.expectEqual(m.minInt(c_long), lrint(m.nan(f64)));
    try std.testing.expectEqual(m.minInt(c_long), lrint(m.inf(f64)));
}

test "C bridge min/max, modf and remainder preserve C signed-zero rules" {
    const nan = m.nan(f64);
    try std.testing.expectEqual(@as(f64, 3), fmax(nan, 3));
    try std.testing.expectEqual(@as(f64, 3), fmin(3, nan));
    try std.testing.expect(m.isNan(fmax(nan, nan)));
    try std.testing.expect(!m.signbit(fmax(-0.0, 0.0)));
    try std.testing.expect(m.signbit(fmin(0.0, -0.0)));
    try std.testing.expect(m.signbit(fmod(-4, 2)));
    try std.testing.expectEqual(@as(f64, -1), fmod(-5, 2));
    try std.testing.expect(m.isNan(fmod(1, 0)));
    try std.testing.expect(m.isNan(fmod(m.inf(f64), 1)));
    var integer: f64 = undefined;
    try std.testing.expectEqual(@as(f64, -0.5), modf(-3.5, &integer));
    try std.testing.expectEqual(@as(f64, -3), integer);
    try std.testing.expect(m.signbit(modf(-m.inf(f64), &integer)));
    try std.testing.expectEqual(-m.inf(f64), integer);
    try std.testing.expect(m.signbit(modf(-2, &integer)));
    try std.testing.expect(m.isNan(modf(nan, &integer)));
}

test "native math bridge baseline values and exceptional values" {
    const Case = struct { operation: Unary, input: f64, expected: f64 };
    for ([_]Case{
        .{ .operation = acos, .input = 1, .expected = 0 },
        .{ .operation = acosh, .input = 1, .expected = 0 },
        .{ .operation = asin, .input = 0, .expected = 0 },
        .{ .operation = asinh, .input = 0, .expected = 0 },
        .{ .operation = atan, .input = 0, .expected = 0 },
        .{ .operation = atanh, .input = 0, .expected = 0 },
        .{ .operation = cbrt, .input = -8, .expected = -2 },
        .{ .operation = ceil, .input = 1.2, .expected = 2 },
        .{ .operation = cos, .input = 0, .expected = 1 },
        .{ .operation = cosh, .input = 0, .expected = 1 },
        .{ .operation = exp, .input = 0, .expected = 1 },
        .{ .operation = expm1, .input = 0, .expected = 0 },
        .{ .operation = fabs, .input = -2, .expected = 2 },
        .{ .operation = floor, .input = -1.2, .expected = -2 },
        .{ .operation = log, .input = 1, .expected = 0 },
        .{ .operation = log10, .input = 100, .expected = 2 },
        .{ .operation = log1p, .input = 0, .expected = 0 },
        .{ .operation = log2, .input = 8, .expected = 3 },
        .{ .operation = round, .input = 1.5, .expected = 2 },
        .{ .operation = sin, .input = 0, .expected = 0 },
        .{ .operation = sinh, .input = 0, .expected = 0 },
        .{ .operation = sqrt, .input = 4, .expected = 2 },
        .{ .operation = tan, .input = 0, .expected = 0 },
        .{ .operation = tanh, .input = 0, .expected = 0 },
        .{ .operation = trunc, .input = -1.2, .expected = -1 },
    }) |case| {
        try std.testing.expectApproxEqAbs(case.expected, case.operation(case.input), 1e-14);
        try std.testing.expect(m.isNan(case.operation(m.nan(f64))));
    }
    try std.testing.expectApproxEqAbs(@as(f64, m.pi) / 4.0, atan2(1, 1), 1e-14);
    try std.testing.expectEqual(@as(f64, 5), hypot(3, 4));
    try std.testing.expectEqual(@as(f64, 8), pow(2, 3));
    try std.testing.expectEqual(@as(f64, 1), pow(m.nan(f64), 0));
    try std.testing.expect(m.signbit(pow(-0.0, 3)));
    try std.testing.expect(m.isNan(sqrt(-1)));
    try std.testing.expect(m.isNan(acos(2)));
    try std.testing.expectEqual(m.inf(f64), acosh(m.inf(f64)));
    try std.testing.expectEqual(m.inf(f64), hypot(m.inf(f64), m.nan(f64)));
    const subnormal: f64 = @bitCast(@as(u64, 1));
    try std.testing.expectEqual(subnormal, fabs(-subnormal));
    try std.testing.expect(m.signbit(sqrt(-0.0)));
    try std.testing.expectEqual(m.inf(f64), exp(1000));
    try std.testing.expectEqual(@as(f64, 0), exp(-1000));
    try std.testing.expectEqual(-m.inf(f64), log(0));
    try std.testing.expect(m.signbit(sin(-0.0)));
    try std.testing.expect(m.signbit(tan(-0.0)));
    try std.testing.expectEqual(subnormal, fmod(subnormal, 2 * subnormal));
    try std.testing.expectEqual(subnormal, fmod(3 * subnormal, 2 * subnormal));
    try std.testing.expectEqual(@as(f64, 0), fmod(m.floatMax(f64), subnormal));
    try std.testing.expectEqual(@as(f64, 3), fmod(3, m.inf(f64)));
    try std.testing.expectApproxEqAbs(@as(f64, -0.8178819121159085), sin(1e300), 1e-14);
    try std.testing.expectApproxEqAbs(@as(f64, -0.5753861119575491), cos(1e300), 1e-14);
}
