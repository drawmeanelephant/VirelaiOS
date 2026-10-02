//! Workload-local B1 calls, not a second std backend. No console fallback.
const std = @import("std");

pub fn Streams(comptime Driver: type) type {
    return struct {
        pub fn readBounded(fd: u64, bytes: []u8) ![]const u8 {
            var used: usize = 0;
            while (used < bytes.len) {
                const chunk = bytes[used..][0..@min(2048, bytes.len - used)];
                const n = Driver.call(24, .{ fd, @intFromPtr(chunk.ptr), chunk.len, 0, 0, 0 });
                if (n < 0 or n > chunk.len) return error.ReadFailed;
                if (n == 0) return bytes[0..used];
                used += @intCast(n);
            }
            var probe: [1]u8 = undefined;
            const n = Driver.call(24, .{ fd, @intFromPtr(&probe), 1, 0, 0, 0 });
            if (n < 0 or n > 1) return error.ReadFailed;
            if (n != 0) return error.InputLimit;
            return bytes;
        }
        pub fn writeAll(fd: u64, bytes: []const u8) !void {
            var used: usize = 0;
            while (used < bytes.len) {
                const chunk = bytes[used..][0..@min(2048, bytes.len - used)];
                const n = Driver.call(1, .{ fd, @intFromPtr(chunk.ptr), chunk.len, 0, 0, 0 });
                if (n <= 0 or n > chunk.len) return error.WriteFailed;
                used += @intCast(n);
            }
        }
        pub fn close(fd: u64) !void {
            if (Driver.call(26, .{ fd, 0, 0, 0, 0, 0 }) != 0) return error.CloseFailed;
        }
    };
}

test "C1 redirected EOF, exact/over input and confirmed writes" {
    const Mock = struct {
        var remaining: usize = 0;
        var written: usize = 0;
        var result: i64 = 17;
        pub fn call(number: u64, args: [6]u64) i64 {
            if (number == 24) {
                const n = @min(remaining, @as(usize, @intCast(args[2])));
                const ptr: [*]u8 = @ptrFromInt(args[1]);
                @memset(ptr[0..n], 'a');
                remaining -= n;
                return @intCast(n);
            }
            if (number == 26) return result;
            if (result <= 0 or result > 2048) return result;
            const n = @min(args[2], @as(u64, @intCast(result)));
            written += @intCast(n);
            return @intCast(n);
        }
    };
    const S = Streams(Mock);
    var buf: [4097]u8 = undefined;
    Mock.remaining = buf.len;
    try std.testing.expectEqual(buf.len, (try S.readBounded(0x100, &buf)).len);
    try std.testing.expectEqual(@as(usize, 0), (try S.readBounded(0x100, &buf)).len);
    Mock.remaining = buf.len + 1;
    try std.testing.expectError(error.InputLimit, S.readBounded(0x100, &buf));
    Mock.written = 0;
    Mock.result = 17;
    try S.writeAll(2, &buf);
    try std.testing.expectEqual(buf.len, Mock.written);
    for ([_]i64{ 0, -2, 2049 }) |result| {
        Mock.result = result;
        try std.testing.expectError(error.WriteFailed, S.writeAll(1, &buf));
    }
    Mock.result = 0;
    try S.close(0x100);
    Mock.result = -2;
    try std.testing.expectError(error.CloseFailed, S.close(0x100));
}
