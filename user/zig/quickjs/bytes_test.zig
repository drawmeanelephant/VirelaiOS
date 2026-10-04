const std = @import("std");

var polls: usize = 0;
var latches: usize = 0;
extern fn qjs_bytes_contract() callconv(.c) c_int;
extern fn qjs_stub_contract(c_int) callconv(.c) c_int;
extern fn qjs_format_contract() callconv(.c) c_int;
extern fn qjs_format_unsupported() callconv(.c) c_int;

export fn qjs_native_checkpoint() c_int {
    polls += 1;
    return 0;
}
export fn qjs_native_hosted_diagnostic() void {
    latches += 1;
}

test "all nine byte/string entries and abs match independent C vectors" {
    polls = 0;
    try std.testing.expectEqual(@as(c_int, 0), qjs_bytes_contract());
    try std.testing.expect(polls >= 30);
}

test "each diagnostic stub fails without reading arguments and latches once" {
    for (0..5) |which| {
        latches = 0;
        try std.testing.expectEqual(@as(c_int, 0), qjs_stub_contract(@intCast(which)));
        try std.testing.expectEqual(@as(usize, 1), latches);
    }
}

test "snprintf and vsnprintf counts, truncation, width, precision and integer lengths" {
    try std.testing.expectEqual(@as(c_int, 0), qjs_format_contract());
}

test "unsupported formats return failure, clear the destination and latch" {
    latches = 0;
    try std.testing.expectEqual(@as(c_int, 0), qjs_format_unsupported());
    try std.testing.expectEqual(@as(usize, 1), latches);
}
