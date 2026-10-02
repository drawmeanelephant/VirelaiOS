const std = @import("std");
const fs = @import("fs.zig");
const platform = @import("platform.zig");
const Backend = @import("io.zig").Backend(Mock);
const expect = std.testing.expect;
const equal = std.testing.expectEqual;
const expectError = std.testing.expectError;

fn facts(inode: u64) fs.Metadata {
    return .{
        .version = 1,
        .kind = .file,
        .identity = .{ .filesystem = 7, .inode = inode },
        .size = 0x1_0000_0001,
        .atime = .{ .seconds = -1, .nanoseconds = 1 },
        .mtime = .{ .seconds = 123, .nanoseconds = 456 },
        .ctime = .{ .seconds = 789, .nanoseconds = 999_999_999 },
        .host_mode = 0o100640,
        .host_uid = 501,
        .host_gid = 20,
        .reserved = 0,
    };
}
const Mock = struct {
    pub const filesystem_b2 = true;
    var calls: usize = 0;
    var last: [6]u64 = undefined;
    var number: u64 = 0;
    var forced: ?i64 = null;
    var value: fs.Metadata = undefined;
    var page: fs.Page = undefined;
    var from: [512]u8 = undefined;
    var to: [512]u8 = undefined;
    var from_len: usize = 0;
    var to_len: usize = 0;
    var diagnostics: usize = 0;

    fn reset() void {
        calls = 0;
        diagnostics = 0;
        forced = null;
        value = facts(42);
        page = std.mem.zeroes(fs.Page);
        page.header = .{ .count = 2, .next = 2, .end = 0 };
        const names = [_][]const u8{ "p" ** 31 ++ "-one", "p" ** 31 ++ "-two" };
        for (names, 0..) |name, i| {
            page.entries[i].name_len = @intCast(name.len);
            @memcpy(page.entries[i].name[0..name.len], name);
            page.entries[i].metadata = facts(i + 1);
        }
    }
    pub fn instance() *anyopaque {
        @panic("userdata required");
    }
    pub fn fatal(_: []const u8) noreturn {
        @panic("unexpected fatal");
    }
    pub fn diagnostic(_: []const u8) void {
        diagnostics += 1;
    }
    pub fn call(n: u64, args: [6]u64) i64 {
        calls += 1;
        number = n;
        last = args;
        if (forced) |rc| return rc;
        switch (n) {
            23 => return 0,
            26, 36, 77 => return 0,
            24 => {
                const out: [*]u8 = @ptrFromInt(args[1]);
                @memcpy(out[0..3], "old");
                return 3;
            },
            25 => return @intCast(args[2]),
            35 => {
                from_len = @intCast(args[1] & ~(@as(u64, 1) << 63));
                to_len = @intCast(args[3]);
                const a: [*]const u8 = @ptrFromInt(args[0]);
                const b: [*]const u8 = @ptrFromInt(args[2]);
                @memcpy(from[0..from_len], a[0..from_len]);
                @memcpy(to[0..to_len], b[0..to_len]);
                return 0;
            },
            79 => switch (args[0]) {
                0 => {
                    const out: *fs.Metadata = @ptrFromInt(args[5]);
                    out.* = value;
                    return 0;
                },
                1 => {
                    const out: *fs.Identity = @ptrFromInt(args[5]);
                    out.* = value.identity;
                    return 0;
                },
                2, 3 => return 0,
                4 => return 123,
                5 => {
                    const out: *fs.Page = @ptrFromInt(args[4]);
                    out.* = page;
                    return page.header.count;
                },
                6 => return 0,
                else => @panic("unknown metadata operation"),
            },
            else => @panic("unexpected syscall"),
        }
    }
};

test "SDK fs: wire facts are lossless, fresh and filesystem-scoped" {
    Mock.reset();
    var backend: Backend = .{};
    const first = try backend.metadata("/host/content", "a");
    try equal(@as(u64, 0x1_0000_0001), first.size);
    try equal(@as(i128, -999_999_999), first.atime.toNanoseconds());
    try equal(@as(u32, 0o100640), first.host_mode);
    try equal(@as(u32, 501), first.host_uid);
    try expect(first.identity.eql(try backend.identity("/host/content", "a")));
    Mock.value.mtime.nanoseconds += 1;
    const fresh = try backend.metadata("/host/content", "a");
    try expect(first.mtime.nanoseconds != fresh.mtime.nanoseconds);
    Mock.value.identity.filesystem += 1;
    try expect(!first.identity.eql(try backend.identity("/host/content", "a")));
    for (0..7) |case| {
        Mock.value = facts(42);
        switch (case) {
            0 => Mock.value.version = 2,
            1 => Mock.value.identity.inode = 0,
            2 => Mock.value.identity.filesystem = 0,
            3 => Mock.value.ctime.nanoseconds = 1_000_000_000,
            4 => Mock.value.mtime.reserved = 1,
            5 => Mock.value.kind = @enumFromInt(99),
            6 => Mock.value.host_mode = 0,
            else => unreachable,
        }
        try expectError(error.ProtocolViolation, backend.metadata("/host", "a"));
    }
}

test "SDK fs: native errors stay distinct, never fabricate metadata or EOF" {
    Mock.reset();
    var backend: Backend = .{};
    const errors = [_]fs.Error{
        error.InvalidArgument,   error.InvalidHandle, error.BadAddress,        error.Unsupported,
        error.ResourceLimit,     error.FileNotFound,  error.AccessDenied,      error.NameTooLong,
        error.PathAlreadyExists, error.OutOfMemory,   error.ProtocolViolation,
    };
    for (errors, 1..) |err, n| {
        Mock.forced = -@as(i64, @intCast(n));
        try expectError(err, backend.metadata("/host", "a"));
        try expectError(err, backend.identity("/host", "a"));
        try expectError(err, backend.openContained("/host", "a", .read));
        try expectError(err, backend.openSnapshot("/host", ""));
        try expectError(err, backend.publish("a", "b", .replace));
        try equal(@as(usize, 4), backend.liveResources());
    }
}

test "SDK fs: B2 path/name bounds, B3 relative-root depth and opaque names" {
    Mock.reset();
    var backend: Backend = .{};
    const wide = try platform.cwd().openFile(backend.io(), "x" ** 255 ++ "/" ++ "y" ** 250, .{});
    try equal(@as(u64, 512), Mock.last[1]);
    wide.close(backend.io());
    const calls = Mock.calls;
    try expectError(error.NameTooLong, platform.cwd().openFile(backend.io(), "x" ** 256, .{}));
    try equal(calls, Mock.calls);
    var legacy_buffer: [64]u8 = undefined;
    const legacy_limit = "x" ** 31 ++ "/" ++ "y" ** 26;
    try equal(@as(usize, 64), (try Backend.path(platform.cwd(), legacy_limit, &legacy_buffer)).len);
    @memset(&legacy_buffer, 0xcc);
    try expectError(error.NameTooLong, Backend.path(platform.cwd(), legacy_limit ++ "z", &legacy_buffer));
    try expectError(error.NameTooLong, Backend.path(platform.cwd(), "x" ** 32, &legacy_buffer));
    try expect(std.mem.allEqual(u8, &legacy_buffer, 0xcc));
    var native_buffer: [512]u8 = undefined;
    try equal(@as(usize, 38), (try fs.fullPath("x" ** 32, &native_buffer)).len);
    try fs.rootedPath("/host", "x" ** 255 ++ "/" ++ "y" ** 250);
    try expectError(error.NameTooLong, fs.rootedPath("/host", "x" ** 255 ++ "/" ++ "y" ** 251));
    try expectError(error.NameTooLong, fs.rootedPath("/host", "x" ** 256));
    try fs.rootedPath("/host/a/b/c/d/e/f/g/h", "a/b/c/d/e/f/g/h");
    try expectError(error.ResourceLimit, fs.rootedPath("/host", "a/b/c/d/e/f/g/h/i"));
    try expectError(error.ResourceLimit, fs.rootedPath("/host/a/b/c/d/e/f/g/h/i", ""));
    try fs.rootedPath("/host", "a\\b:c\xff");
    for ([_][]const u8{ "/a", "a/", ".", "..", "a/../b", "a//b", "a\x00b" }) |name|
        try expectError(error.InvalidArgument, fs.rootedPath("/host", name));
    try expectError(error.AccessDenied, fs.rootedPath("/usb", "a"));
    try expectError(error.AccessDenied, fs.rootedPath("/hostile", "a"));
}

test "SDK fs: contained open is one native open, no preflight stat or old-path fd stat" {
    Mock.reset();
    var backend: Backend = .{};
    const file = try backend.openContained("/host/root", "file", .write);
    try equal(@as(usize, 1), Mock.calls);
    try equal(@as(u64, 79), Mock.number);
    try equal(@as(u64, 3), Mock.last[0]);
    try equal(@as(u64, 0), Mock.last[5]);
    const before = Mock.calls;
    try expectError(error.Unsupported, backend.fileMetadata(file));
    try expectError(error.Unexpected, file.stat(backend.io()));
    try expectError(error.Unexpected, file.length(backend.io()));
    try equal(before, Mock.calls);
    try file.writeStreamingAll(backend.io(), "new");
    try equal(@as(u64, 25), Mock.number);
    try file.setLength(backend.io(), 3);
    try file.sync(backend.io());
    try backend.closeChecked(file);
    const next = try backend.openContained("/host/root", "file", .read);
    try expect(next.handle != file.handle);
    try expectError(error.InvalidHandle, backend.fileMetadata(file));
    const io = backend.io();
    const cwd = platform.cwd();
    const guarded = try cwd.openFile(io, "file", .{ .follow_symlinks = false, .resolve_beneath = true });
    try equal(@as(u64, 2), Mock.last[0]);
    guarded.close(io);
    next.close(io);
}

test "SDK fs: rich pages keep names facts explicit EOF and retry state" {
    Mock.reset();
    var backend: Backend = .{};
    const cursor = try backend.openSnapshot("/host", "");
    var page: fs.Page = undefined;
    try backend.snapshotPage(cursor, 0, 7, &page);
    try equal(@as(usize, 2), page.rows().len);
    try std.testing.expectEqualStrings("p" ** 31 ++ "-one", page.rows()[0].nameBytes());
    try std.testing.expectEqualStrings("p" ** 31 ++ "-two", page.rows()[1].nameBytes());
    try equal(@as(u64, 0x1_0000_0001), page.rows()[1].metadata.size);
    const saved = page;
    Mock.forced = -3;
    try expectError(error.BadAddress, backend.snapshotPage(cursor, 0, 7, &page));
    try std.testing.expectEqualDeep(saved, page);
    Mock.forced = null;
    for (0..9) |case| {
        Mock.page = saved;
        switch (case) {
            0 => Mock.page.header.next = 99,
            1 => Mock.page.header.end = 2,
            2 => Mock.page.entries[0].name_len = 256,
            3 => Mock.page.entries[0].reserved[0] = 1,
            4 => Mock.page.entries[0].name[255] = 1,
            5 => Mock.page.entries[0].metadata.identity.inode = 0,
            6 => Mock.page.entries[1] = Mock.page.entries[0],
            7 => Mock.page.entries[0].name[1] = '/',
            8 => Mock.page.header.count = 0,
            else => unreachable,
        }
        try expectError(error.ProtocolViolation, backend.snapshotPage(cursor, 0, 7, &page));
        try std.testing.expectEqualDeep(saved, page);
    }
    Mock.page = saved;
    Mock.page.header = .{ .count = 0, .next = 2, .end = 1 };
    try backend.snapshotPage(cursor, 2, 7, &page);
    try expect(page.done() and page.rows().len == 0);
    const calls = Mock.calls;
    try expectError(error.InvalidArgument, backend.snapshotPage(cursor, 0, 17, &page));
    try equal(calls, Mock.calls);
    try backend.closeSnapshot(cursor);
    try expectError(error.InvalidHandle, backend.snapshotPage(cursor, 0, 7, &page));
}

test "SDK fs: files and snapshots share eight records, failed closes retain resources" {
    Mock.reset();
    var backend: Backend = .{};
    const io = backend.io();
    const a = try platform.cwd().createFile(io, "a", .{});
    const b = try backend.openContained("/host", "b", .read);
    const c = try backend.openSnapshot("/host", "c");
    const d = try backend.openSnapshot("/host", "d");
    try equal(@as(usize, 8), backend.liveResources());
    const calls = Mock.calls;
    try expectError(error.ResourceLimit, backend.openSnapshot("/host", "e"));
    try expectError(error.ResourceLimit, backend.openContained("/host", "e", .read));
    try expectError(error.ProcessFdQuotaExceeded, platform.cwd().createFile(io, "e", .{}));
    try expectError(error.InvalidHandle, backend.closeSnapshot(.{ .token = a.handle }));
    try expectError(error.CloseFailed, backend.closeChecked(.{ .handle = @intCast(c.token), .flags = .{ .nonblocking = false } }));
    try equal(calls, Mock.calls);
    Mock.forced = -7;
    try expectError(error.AccessDenied, backend.closeSnapshot(c));
    try equal(@as(usize, 8), backend.liveResources());
    Mock.forced = null;
    try backend.closeSnapshot(c);
    const reused = try backend.openSnapshot("/host", "e");
    try expect(reused.token != c.token);
    try expectError(error.InvalidHandle, backend.closeSnapshot(c));
    a.close(io);
    b.close(io);
    try backend.closeSnapshot(d);
    try backend.closeAll();
    try equal(@as(usize, 4), backend.liveResources());
    backend.next_token = std.math.maxInt(i64);
    try expectError(error.ResourceLimit, backend.openSnapshot("/host", ""));
}

test "SDK fs: publication is one B4 rename, correct bits and no fallback" {
    Mock.reset();
    var backend: Backend = .{};
    const cwd = platform.cwd();
    const io = backend.io();
    try cwd.rename("stage", cwd, "out", io);
    try equal(@as(usize, 1), Mock.calls);
    try equal(@as(u64, 35), Mock.number);
    try expect(Mock.last[1] & (@as(u64, 1) << 63) != 0);
    try std.testing.expectEqualStrings("/host/stage", Mock.from[0..Mock.from_len]);
    try std.testing.expectEqualStrings("/host/out", Mock.to[0..Mock.to_len]);
    try cwd.renamePreserve("stage", cwd, "out", io);
    try expect(Mock.last[1] & (@as(u64, 1) << 63) == 0);
    Mock.forced = -9;
    try expectError(error.PathAlreadyExists, cwd.renamePreserve("stage", cwd, "out", io));
    Mock.forced = -4;
    try expectError(error.OperationUnsupported, cwd.renamePreserve("stage", cwd, "out", io));
    try expectError(error.Unexpected, cwd.rename("stage", cwd, "out", io));
    try equal(@as(usize, 5), Mock.calls);
    try expectError(error.AccessDenied, backend.publish("stage", "/usb/out", .replace));
    try equal(@as(usize, 5), Mock.calls);
    try expectError(error.Unexpected, cwd.createFile(io, "stage", .{ .exclusive = true }));
    try expectError(error.Unexpected, cwd.createFile(io, "stage", .{ .resolve_beneath = true }));
    try equal(@as(usize, 5), Mock.calls);
}

test "SDK fs: std maps native I/O failure without inventing a path or fd-quota cause" {
    Mock.reset();
    var backend: Backend = .{};
    Mock.forced = -1; // Also the native transport error, not necessarily bad syntax.
    try expectError(error.Unexpected, platform.cwd().openFile(backend.io(), "valid", .{}));
    try expectError(error.Unexpected, platform.cwd().openFile(backend.io(), "valid", .{ .follow_symlinks = false }));
    Mock.forced = -5; // Native pool, depth, identity or global resource exhaustion.
    try expectError(error.SystemResources, platform.cwd().openFile(backend.io(), "valid", .{}));
    const before = Mock.calls;
    try expectError(error.BadPathName, platform.cwd().openFile(backend.io(), "a/../b", .{}));
    try equal(before, Mock.calls);
}
