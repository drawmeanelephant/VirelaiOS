//! Synchronous native std.Io. No POSIX calls, implicit tty, or invented metadata.
const std = @import("std");
const Io = std.Io;
const File = Io.File;
const Dir = Io.Dir;
const platform = @import("platform.zig");
const refusals = @import("refusals.zig");

pub fn Backend(comptime Driver: type) type {
    return struct {
        const Self = @This();
        const Record = struct { token: i64, fd: u64, writable: bool };
        // Three reserved stream identities + the borrowed cwd + four files.
        records: [4]?Record = @splat(null),
        next_token: i64 = 1,
        peak_files: usize = 0,
        cancel_protection: Io.CancelProtection = .unblocked,
        canceled: bool = false,
        debug_locked: bool = false,
        debug_writer: File.Writer = undefined,

        pub const vtable: Io.VTable = table: {
            @setEvalBranchQuota(100000);
            const names = @import("operations.zig").names;
            const fields = @typeInfo(Io.VTable).@"struct".fields;
            if (fields.len != names.len) @compileError("UnreviewedIoVTable");
            var table: Io.VTable = undefined;
            for (fields, names) |field, name| {
                if (!std.mem.eql(u8, field.name, name)) @compileError("UnreviewedIoVTable:" ++ field.name);
                @field(table, name) = if (@hasDecl(Self, name))
                    @field(Self, name)
                else
                    refusals.function(Driver, name, field.type);
            }
            break :table table;
        };

        pub fn io(self: *Self) Io {
            return .{ .userdata = self, .vtable = &vtable };
        }
        fn state(ptr: ?*anyopaque) *Self {
            return @ptrCast(@alignCast(ptr orelse Driver.instance()));
        }
        fn reject(comptime name: []const u8, comptime R: type) R {
            return refusals.result(Driver, name, R);
        }
        fn fileRecord(self: *Self, file: File) ?*Record {
            for (&self.records) |*entry| {
                if (entry.*) |*record| {
                    if (record.token == file.handle) return record;
                }
            }
            return null;
        }
        pub fn liveFiles(self: *Self) usize {
            var count: usize = 0;
            for (self.records) |record| if (record != null) {
                count += 1;
            };
            return count;
        }
        /// Cwd is lexical SDK state, not proof of no-follow containment (B3).
        pub fn path(dir: Dir, name: []const u8, buffer: *[64]u8) File.OpenError![]const u8 {
            if (dir.handle != platform.cwd_token) return reject("DirectoryToken", File.OpenError![]const u8);
            if (name.len == 0 or std.mem.indexOfAny(u8, name, "\x00\\:") != null)
                return error.BadPathName;
            const absolute = name[0] == '/';
            if (absolute and !std.mem.startsWith(u8, name, "/host/")) return error.AccessDenied;
            const relative = if (absolute) name[6..] else name;
            var parts = std.mem.splitScalar(u8, relative, '/');
            while (parts.next()) |part| {
                if (part.len == 0 or std.mem.eql(u8, part, ".") or std.mem.eql(u8, part, ".."))
                    return error.BadPathName;
                if (part.len > 31) return error.NameTooLong;
            }
            if (relative.len > 58) return error.NameTooLong;
            @memcpy(buffer[0..6], "/host/");
            @memcpy(buffer[6..][0..relative.len], relative);
            return buffer[0 .. 6 + relative.len];
        }
        fn open(self: *Self, dir: Dir, name: []const u8, flags: u32) File.OpenError!File {
            var buffer: [64]u8 = undefined;
            const full = try path(dir, name, &buffer);
            var free: ?*?Record = null;
            for (&self.records) |*entry| {
                if (entry.* == null) {
                    free = entry;
                    break;
                }
            }
            const entry = free orelse return error.ProcessFdQuotaExceeded;
            if (self.next_token == std.math.maxInt(i64)) return error.SystemResources;
            const result = Driver.call(23, .{ @intFromPtr(full.ptr), full.len, flags, 0, 0, 0 });
            if (result < 0) return switch (result) {
                -5 => error.SystemResources,
                -6 => error.FileNotFound,
                -7 => error.AccessDenied,
                -8 => error.NameTooLong,
                -9 => error.PathAlreadyExists,
                else => error.Unexpected,
            };
            if (result >= 8) Driver.fatal("InvalidNativeHandle");
            const token = self.next_token;
            self.next_token += 1;
            entry.* = .{ .token = token, .fd = @intCast(result), .writable = flags & 2 != 0 };
            self.peak_files = @max(self.peak_files, self.liveFiles());
            return .{ .handle = @intCast(token), .flags = .{ .nonblocking = false } };
        }
        fn dirOpenFile(ptr: ?*anyopaque, dir: Dir, name: []const u8, options: Dir.OpenFileOptions) File.OpenError!File {
            if (options.mode != .read_only or options.path_only or options.lock != .none or
                options.lock_nonblocking or options.allow_ctty or !options.follow_symlinks or options.resolve_beneath)
                return reject("OpenOptions", File.OpenError!File);
            return state(ptr).open(dir, name, 1);
        }
        fn dirCreateFile(ptr: ?*anyopaque, dir: Dir, name: []const u8, options: Dir.CreateFileOptions) File.OpenError!File {
            if (options.read or !options.truncate or options.exclusive or options.lock != .none or
                options.lock_nonblocking or options.resolve_beneath or options.permissions != .default_file)
                return reject("CreateOptions", File.OpenError!File);
            return state(ptr).open(dir, name, 2 | 4);
        }
        fn dirCreateDir(ptr: ?*anyopaque, dir: Dir, name: []const u8, permissions: Dir.Permissions) Dir.CreateDirError!void {
            if (permissions != .default_dir) return reject("DirectoryPermissions", Dir.CreateDirError!void);
            const self = state(ptr);
            const file = self.open(dir, name, 2 | 4 | 16) catch |err| return switch (err) {
                error.AccessDenied => error.AccessDenied,
                error.NameTooLong => error.NameTooLong,
                error.BadPathName => error.BadPathName,
                error.FileNotFound => error.FileNotFound,
                error.PathAlreadyExists => error.PathAlreadyExists,
                else => error.Unexpected,
            };
            self.closeChecked(file) catch Driver.fatal("CloseFailed");
        }
        /// Fallible SDK close for callers that need to propagate failure.
        /// std's void close must terminate on failure, never imply success.
        pub fn closeChecked(self: *Self, file: File) error{CloseFailed}!void {
            for (&self.records) |*entry| {
                if (entry.*) |record| {
                    if (record.token != file.handle) continue;
                    if (Driver.call(26, .{ record.fd, 0, 0, 0, 0, 0 }) != 0) return error.CloseFailed;
                    entry.* = null;
                    return;
                }
            }
            return error.CloseFailed;
        }
        pub fn closeAll(self: *Self) error{CloseFailed}!void {
            for (self.records) |entry| if (entry) |record| {
                try self.closeChecked(.{ .handle = @intCast(record.token), .flags = .{ .nonblocking = false } });
            };
        }
        fn fileClose(ptr: ?*anyopaque, files: []const File) void {
            for (files) |file| state(ptr).closeChecked(file) catch Driver.fatal("CloseFailed");
        }
        fn fileSync(ptr: ?*anyopaque, file: File) File.SyncError!void {
            const record = state(ptr).fileRecord(file) orelse return reject("UnboundOrClosedFile", File.SyncError!void);
            const result = Driver.call(77, .{ record.fd, 0, 0, 0, 0, 0 });
            if (result != 0) return switch (result) {
                -7 => error.AccessDenied,
                -5 => error.NoSpaceLeft,
                else => error.InputOutput,
            };
        }
        fn fileSetLength(ptr: ?*anyopaque, file: File, length: u64) File.SetLengthError!void {
            if (length > std.math.maxInt(u32)) return error.FileTooBig;
            const record = state(ptr).fileRecord(file) orelse return reject("UnboundOrClosedFile", File.SetLengthError!void);
            if (!record.writable) return error.AccessDenied;
            if (Driver.call(36, .{ record.fd, length, 0, 0, 0, 0 }) != 0) return error.Unexpected;
        }
        fn read(self: *Self, op: Io.Operation.FileReadStreaming) Io.Operation.FileReadStreaming.Result {
            const record = self.fileRecord(op.file) orelse return reject("UnboundOrClosedInput", Io.Operation.FileReadStreaming.Result);
            if (record.writable) return error.NotOpenForReading;
            for (op.data) |buffer| {
                if (buffer.len == 0) continue;
                const chunk = buffer[0..@min(buffer.len, 2048)];
                const n = Driver.call(24, .{ record.fd, @intFromPtr(chunk.ptr), chunk.len, 0, 0, 0 });
                if (n < 0) return if (n == -7) error.AccessDenied else error.InputOutput;
                if (n > chunk.len) return error.InputOutput;
                if (n == 0) return error.EndOfStream;
                return @intCast(n);
            }
            return 0; // A supported, valid handle and an empty request.
        }
        fn writeChunk(self: *Self, file: File, bytes: []const u8) Io.Operation.FileWriteStreaming.Result {
            if (file.handle == platform.diagnostic_token) {
                Driver.diagnostic(bytes);
                return bytes.len;
            }
            const record = self.fileRecord(file) orelse return reject("UnboundOrClosedOutput", Io.Operation.FileWriteStreaming.Result);
            if (!record.writable) return error.NotOpenForWriting;
            if (bytes.len == 0) return 0;
            const chunk = bytes[0..@min(bytes.len, 2048)];
            const n = Driver.call(25, .{ record.fd, @intFromPtr(chunk.ptr), chunk.len, 0, 0, 0 });
            if (n <= 0 or n > chunk.len) return switch (n) {
                -5 => error.NoSpaceLeft,
                -7 => error.AccessDenied,
                else => error.InputOutput,
            };
            return @intCast(n);
        }
        fn write(self: *Self, op: Io.Operation.FileWriteStreaming) Io.Operation.FileWriteStreaming.Result {
            // Validate even an empty request; an unbound stream is not a sink.
            if (op.file.handle != platform.diagnostic_token) {
                const record = self.fileRecord(op.file) orelse
                    return reject("UnboundOrClosedOutput", Io.Operation.FileWriteStreaming.Result);
                if (!record.writable) return error.NotOpenForWriting;
            }
            if (op.header.len != 0) return self.writeChunk(op.file, op.header);
            for (op.data, 0..) |bytes, i| {
                if (i + 1 == op.data.len and op.splat == 0) break;
                if (bytes.len != 0) return self.writeChunk(op.file, bytes);
            }
            return 0;
        }
        fn operate(ptr: ?*anyopaque, op: Io.Operation) Io.Cancelable!Io.Operation.Result {
            try checkCancel(ptr);
            return switch (op) {
                .file_read_streaming => |request| .{ .file_read_streaming = state(ptr).read(request) },
                .file_write_streaming => |request| .{ .file_write_streaming = state(ptr).write(request) },
                .net_receive => refused: {
                    Driver.diagnostic("Unsupported:net_receive\n");
                    break :refused .{ .net_receive = .{ error.Unexpected, 0 } };
                },
                .device_io_control => Driver.fatal("Unsupported:device_io_control"),
            };
        }
        fn netSend(_: ?*anyopaque, _: Io.net.Socket.Handle, _: []Io.net.OutgoingMessage, _: Io.net.SendFlags) struct { ?Io.net.Socket.SendError, usize } {
            Driver.diagnostic("Unsupported:netSend\n");
            return .{ error.Unexpected, 0 };
        }
        fn fileReadPositional(_: ?*anyopaque, _: File, _: []const []u8, _: u64) File.ReadPositionalError!usize {
            return error.Unseekable;
        }
        fn fileWritePositional(_: ?*anyopaque, _: File, _: []const u8, _: []const []const u8, _: usize, _: u64) File.WritePositionalError!usize {
            return error.Unseekable;
        }
        fn fileSeekTo(_: ?*anyopaque, _: File, _: u64) File.SeekError!void {
            return error.Unseekable;
        }
        fn fileSeekBy(_: ?*anyopaque, _: File, _: i64) File.SeekError!void {
            return error.Unseekable;
        }
        fn processCurrentPath(_: ?*anyopaque, buffer: []u8) std.process.CurrentPathError!usize {
            if (buffer.len < 5) return error.NameTooLong;
            @memcpy(buffer[0..5], "/host");
            return 5;
        }
        fn randomSecure(_: ?*anyopaque, buffer: []u8) Io.RandomSecureError!void {
            var at: usize = 0;
            while (at < buffer.len) {
                const chunk = buffer[at..][0..@min(256, buffer.len - at)];
                const n = Driver.call(72, .{ @intFromPtr(chunk.ptr), chunk.len, 0, 0, 0, 0 });
                if (n <= 0 or n > chunk.len) return error.EntropyUnavailable;
                at += @intCast(n);
            }
        }
        fn random(ptr: ?*anyopaque, buffer: []u8) void {
            randomSecure(ptr, buffer) catch Driver.fatal("EntropyUnavailable");
        }
        fn async(_: ?*anyopaque, result: []u8, _: std.mem.Alignment, context: []const u8, _: std.mem.Alignment, start: *const fn (*const anyopaque, *anyopaque) void) ?*Io.AnyFuture {
            start(context.ptr, result.ptr);
            return null; // The result has actually been populated synchronously.
        }
        fn concurrent(_: ?*anyopaque, _: usize, _: std.mem.Alignment, _: []const u8, _: std.mem.Alignment, _: *const fn (*const anyopaque, *anyopaque) void) Io.ConcurrentError!*Io.AnyFuture {
            return error.ConcurrencyUnavailable;
        }
        fn groupAsync(_: ?*anyopaque, _: *Io.Group, context: []const u8, _: std.mem.Alignment, start: *const fn (*const anyopaque) void) void {
            start(context.ptr);
        }
        fn groupConcurrent(_: ?*anyopaque, _: *Io.Group, _: []const u8, _: std.mem.Alignment, _: *const fn (*const anyopaque) void) Io.ConcurrentError!void {
            return error.ConcurrencyUnavailable;
        }
        fn batchAwaitConcurrent(_: ?*anyopaque, _: *Io.Batch, _: Io.Timeout) Io.Batch.AwaitConcurrentError!void {
            return error.ConcurrencyUnavailable;
        }
        fn recancel(ptr: ?*anyopaque) void {
            state(ptr).canceled = true;
        }
        fn swapCancelProtection(ptr: ?*anyopaque, new: Io.CancelProtection) Io.CancelProtection {
            const self = state(ptr);
            const old = self.cancel_protection;
            self.cancel_protection = new;
            return old;
        }
        fn checkCancel(ptr: ?*anyopaque) Io.Cancelable!void {
            const self = state(ptr);
            if (self.cancel_protection == .unblocked and self.canceled) {
                self.canceled = false;
                return error.Canceled;
            }
        }
        fn lockStderr(ptr: ?*anyopaque, mode: ?Io.Terminal.Mode) Io.Cancelable!Io.LockedStderr {
            const self = state(ptr);
            if (self.debug_locked) Driver.fatal("Unsupported:ReentrantDebugLock");
            if (mode != null and mode.? != .no_color) Driver.fatal("Unsupported:DebugTerminalMode");
            self.debug_locked = true;
            self.debug_writer = .initStreaming(.{
                .handle = platform.diagnostic_token,
                .flags = .{ .nonblocking = false },
            }, self.io(), &.{});
            return .{ .file_writer = &self.debug_writer, .terminal_mode = .no_color };
        }
        fn tryLockStderr(ptr: ?*anyopaque, mode: ?Io.Terminal.Mode) Io.Cancelable!?Io.LockedStderr {
            if (state(ptr).debug_locked) return null;
            return try lockStderr(ptr, mode);
        }
        fn unlockStderr(ptr: ?*anyopaque) void {
            const self = state(ptr);
            if (!self.debug_locked) Driver.fatal("InvalidDebugUnlock");
            self.debug_writer.interface.flush() catch Driver.fatal("DebugFlushFailed");
            if (self.debug_writer.err != null) Driver.fatal("DebugWriteFailed");
            self.debug_writer.interface.buffer = &.{};
            self.debug_locked = false;
        }
    };
}

test "native Io: short writes, splats, empty header, sequential EOF, bounded read" {
    Mock.reset();
    var backend: TestBackend = .{};
    const io = backend.io();
    const file = try platform.cwd().createFile(io, "output", .{});
    Mock.maximum = 17;
    var buffer: [29]u8 = undefined;
    var writer = file.writer(io, &buffer); // default positional path falls back honestly
    try writer.interface.writeAll("x" ** 5000);
    try writer.interface.flush();
    try file.writeStreamingAll(io, "end");
    try std.testing.expectEqual(@as(usize, 5003), Mock.output_len);
    try std.testing.expectEqualStrings("x" ** 5000 ++ "end", Mock.output[0..Mock.output_len]);
    var vectors = [_][]const u8{ "", "ab", "z" };
    try writer.interface.writeSplatAll(&vectors, 3);
    try writer.interface.flush();
    try std.testing.expectEqualStrings("abzzz", Mock.output[5003..Mock.output_len]);
    var no_last = [_][]const u8{ "prefix", "absent" };
    try writer.interface.writeSplatAll(&no_last, 0);
    try writer.interface.flush();
    try std.testing.expectEqualStrings("prefix", Mock.output[5008..Mock.output_len]);
    try file.sync(io);
    file.close(io);
    const input = try platform.cwd().openFile(io, "input", .{});
    const empty_write = try io.operate(.{ .file_write_streaming = .{ .file = input, .data = &.{} } });
    try std.testing.expectError(error.NotOpenForWriting, empty_write.file_write_streaming);
    Mock.input = "a" ** 5000;
    const out = try std.testing.allocator.alloc(u8, 5000);
    defer std.testing.allocator.free(out);
    try std.testing.expectEqualStrings(Mock.input, try readBounded(input, io, out));
    input.close(io);
    const over = try platform.cwd().openFile(io, "input", .{});
    Mock.input = "a" ** 5001;
    Mock.input_at = 0;
    try std.testing.expectError(error.InputLimit, readBounded(over, io, out));
    over.close(io);
    try std.testing.expectEqual(@as(usize, 0), backend.liveFiles());
}

test "native Io: zero progress, overcount and native read write sync close failures" {
    Mock.reset();
    var backend: TestBackend = .{};
    const io = backend.io();
    const output = try platform.cwd().createFile(io, "output", .{});
    for ([_]i64{ 0, -1, 2049 }) |n| {
        Mock.forced = n;
        try std.testing.expectError(error.InputOutput, output.writeStreamingAll(io, "hello"));
    }
    Mock.forced = -7;
    try std.testing.expectError(error.AccessDenied, output.writeStreamingAll(io, "hello"));
    Mock.forced = -5;
    try std.testing.expectError(error.NoSpaceLeft, output.writeStreamingAll(io, "hello"));
    try std.testing.expectError(error.NoSpaceLeft, output.sync(io));
    try std.testing.expectError(error.CloseFailed, backend.closeChecked(output));
    try std.testing.expectEqual(@as(usize, 1), backend.liveFiles());
    Mock.forced = null;
    try backend.closeChecked(output);
    const input = try platform.cwd().openFile(io, "input", .{});
    var byte: [1]u8 = undefined;
    for ([_]i64{ -1, 2 }) |n| {
        Mock.forced = n;
        try std.testing.expectError(error.InputOutput, readBounded(input, io, &byte));
    }
    Mock.forced = null;
    try backend.closeChecked(input);
}

test "native Io: unbound streams and stale tokens never alias native fd zero" {
    Mock.reset();
    var backend: TestBackend = .{};
    const io = backend.io();
    const file = try platform.cwd().createFile(io, "output", .{});
    file.close(io);
    const next = try platform.cwd().createFile(io, "next", .{});
    try std.testing.expect(file.handle != next.handle);
    const before = Mock.calls;
    const standard = [_]File{
        .{ .handle = -1, .flags = .{ .nonblocking = false } },
        .{ .handle = -2, .flags = .{ .nonblocking = false } },
        .{ .handle = -3, .flags = .{ .nonblocking = false } },
        file,
    };
    for (standard) |stream| {
        try std.testing.expectError(error.Unexpected, stream.writeStreamingAll(io, "x"));
        var byte: [1]u8 = undefined;
        try std.testing.expectError(error.Unexpected, readBounded(stream, io, &byte));
        try std.testing.expectError(error.CloseFailed, backend.closeChecked(stream));
    }
    try std.testing.expectEqual(before, Mock.calls);
    next.close(io);
}

test "native Io: unsupported open options refuse before touching a path" {
    Mock.reset();
    var backend: TestBackend = .{};
    const io = backend.io();
    for ([_]Dir.OpenFileOptions{
        .{ .mode = .write_only },     .{ .mode = .read_write },      .{ .follow_symlinks = false },
        .{ .resolve_beneath = true }, .{ .path_only = true },        .{ .lock = .exclusive },
        .{ .allow_ctty = true },      .{ .lock_nonblocking = true },
    }) |options| try std.testing.expectError(error.Unexpected, platform.cwd().openFile(io, "unchanged", options));
    for ([_]Dir.CreateFileOptions{
        .{ .read = true },            .{ .truncate = false }, .{ .exclusive = true },
        .{ .resolve_beneath = true }, .{ .lock = .shared },   .{ .lock_nonblocking = true },
    }) |options| try std.testing.expectError(error.Unexpected, platform.cwd().createFile(io, "unchanged", options));
    try std.testing.expectEqual(@as(usize, 0), Mock.calls);
}

test "native Io: path component and full limits reject escapes without clipping" {
    var path_buffer: [64]u8 = undefined;
    const cwd = platform.cwd();
    try std.testing.expectEqualStrings("/host/a/b", try TestBackend.path(cwd, "a/b", &path_buffer));
    try std.testing.expectEqual(@as(usize, 64), (try TestBackend.path(cwd, "x" ** 31 ++ "/" ++ "y" ** 26, &path_buffer)).len);
    try std.testing.expectError(error.NameTooLong, TestBackend.path(cwd, "x" ** 31 ++ "/" ++ "y" ** 27, &path_buffer));
    try std.testing.expectError(error.NameTooLong, TestBackend.path(cwd, "x" ** 32, &path_buffer));
    for ([_][]const u8{ "", "a/../b", "./a", "a//b", "a/", "a\x00b", "host:x", "a\\b" }) |name|
        try std.testing.expectError(error.BadPathName, TestBackend.path(cwd, name, &path_buffer));
    for ([_][]const u8{ "/usb/x", "/dev/tty", "/hostile/x", "//host/x" }) |name|
        try std.testing.expectError(error.AccessDenied, TestBackend.path(cwd, name, &path_buffer));
}

test "native Io: resource ceiling, output preflight and native truncate width" {
    Mock.reset();
    var backend: TestBackend = .{};
    const io = backend.io();
    var files: [4]File = undefined;
    for (&files) |*file| file.* = try platform.cwd().createFile(io, "output", .{});
    const calls = Mock.calls;
    try std.testing.expectError(error.ProcessFdQuotaExceeded, platform.cwd().createFile(io, "overflow", .{}));
    try std.testing.expectError(error.OutputLimit, writeBounded(files[0], io, "12345", 4));
    try std.testing.expectError(error.FileTooBig, files[0].setLength(io, 0x1_0000_0000));
    try std.testing.expectEqual(calls, Mock.calls);
    try writeBounded(files[0], io, "1234", 4);
    try files[0].setLength(io, 0xffff_ffff);
    try std.testing.expectEqual(@as(u64, 0xffff_ffff), Mock.last_args[1]);
    try backend.closeAll();
    try std.testing.expectEqual(@as(usize, 4), backend.peak_files);
    try std.testing.expectEqual(@as(usize, 0), backend.liveFiles());
}

test "native Io: metadata, locks, traversal, rename, network and spawn refuse" {
    Mock.reset();
    var backend: TestBackend = .{};
    const io = backend.io();
    const file = try platform.cwd().openFile(io, "input", .{});
    try std.testing.expectError(error.Unexpected, file.stat(io));
    try std.testing.expectError(error.Unexpected, file.length(io));
    try std.testing.expectError(error.Unexpected, file.tryLock(io, .exclusive));
    try std.testing.expectError(error.Unseekable, io.vtable.fileSeekTo(io.userdata, file, 0));
    try std.testing.expectError(error.Unexpected, platform.cwd().openDir(io, "nested", .{}));
    try std.testing.expectError(error.Unexpected, io.vtable.dirRename(io.userdata, platform.cwd(), "a", platform.cwd(), "b"));
    try std.testing.expectError(error.OperationUnsupported, io.vtable.processSpawn(io.userdata, .{ .argv = &.{"child"} }));
    try std.testing.expectEqual(error.Unexpected, io.vtable.netSend(io.userdata, 0, &.{}, .{})[0].?);
    try std.testing.expect(Mock.diagnostics > 0);
    file.close(io);
}

test "native Io: entropy short fill and errors, never zero-filled fallback" {
    Mock.reset();
    var backend: TestBackend = .{};
    const io = backend.io();
    var bytes: [513]u8 = undefined;
    Mock.maximum = 17;
    try io.randomSecure(&bytes);
    try std.testing.expect(std.mem.allEqual(u8, &bytes, 0xa7));
    try std.testing.expectEqual(@as(usize, 31), Mock.calls);
    for ([_]i64{ 0, -1, 257 }) |n| {
        Mock.forced = n;
        @memset(&bytes, 0xcc);
        try std.testing.expectError(error.EntropyUnavailable, io.randomSecure(&bytes));
        try std.testing.expect(std.mem.allEqual(u8, &bytes, 0xcc));
    }
}

test "native Io: synchronous async actually runs; concurrent cannot pretend to run" {
    Mock.reset();
    var backend: TestBackend = .{};
    const io = backend.io();
    const Work = struct {
        fn run(n: u32) u32 {
            return n + 1;
        }
    };
    var task = io.async(Work.run, .{@as(u32, 41)});
    try std.testing.expectEqual(@as(u32, 42), task.await(io));
    try std.testing.expectError(error.ConcurrencyUnavailable, io.concurrent(Work.run, .{@as(u32, 41)}));
    const GroupWork = struct {
        fn run(value: *u32) void {
            value.* += 1;
        }
    };
    var group: Io.Group = .init;
    var completed: u32 = 0;
    group.async(io, GroupWork.run, .{&completed});
    try group.await(io);
    group.cancel(io);
    try std.testing.expectEqual(@as(u32, 1), completed);
    io.recancel();
    const old = io.swapCancelProtection(.blocked);
    try io.checkCancel();
    _ = io.swapCancelProtection(old);
    try std.testing.expectError(error.Canceled, io.checkCancel());
    try io.checkCancel();
}

const TestBackend = Backend(Mock);
const Mock = struct {
    var calls: usize = 0;
    var diagnostics: usize = 0;
    var maximum: usize = 2048;
    var forced: ?i64 = null;
    var input: []const u8 = "";
    var input_at: usize = 0;
    var output: [8192]u8 = undefined;
    var output_len: usize = 0;
    var last_args: [6]u64 = undefined;
    fn reset() void {
        calls = 0;
        diagnostics = 0;
        maximum = 2048;
        forced = null;
        input = "";
        input_at = 0;
        output_len = 0;
    }
    pub fn instance() *anyopaque {
        @panic("test must supply userdata");
    }
    pub fn diagnostic(_: []const u8) void {
        diagnostics += 1;
    }
    pub fn fatal(_: []const u8) noreturn {
        @panic("unexpected fatal refusal");
    }
    pub fn call(number: u64, args: [6]u64) i64 {
        calls += 1;
        last_args = args;
        if (forced) |n| return n;
        switch (number) {
            23 => return 0, // Deliberately recycle native fd zero.
            24 => {
                std.debug.assert(args[2] <= 2048);
                const n = @min(args[2], maximum, input.len - input_at);
                const out: [*]u8 = @ptrFromInt(args[1]);
                @memcpy(out[0..n], input[input_at..][0..n]);
                input_at += n;
                return @intCast(n);
            },
            25 => {
                std.debug.assert(args[2] <= 2048);
                const n = @min(args[2], maximum);
                const bytes: [*]const u8 = @ptrFromInt(args[1]);
                @memcpy(output[output_len..][0..n], bytes[0..n]);
                output_len += n;
                return @intCast(n);
            },
            26, 36, 77 => return 0,
            72 => {
                std.debug.assert(args[1] <= 256);
                const n = @min(args[1], maximum);
                const bytes: [*]u8 = @ptrFromInt(args[0]);
                @memset(bytes[0..n], 0xa7);
                return @intCast(n);
            },
            else => @panic("unexpected native operation"),
        }
    }
};

/// Metadata-free sequential reader. A full limit is probed by one extra byte.
pub fn readBounded(file: File, io: Io, output: []u8) (File.Reader.Error || error{InputLimit})![]u8 {
    var reader: File.Reader = .initStreaming(file, io, &.{});
    const n = reader.interface.readSliceShort(output) catch return reader.err.?;
    if (n == output.len) {
        var extra: [1]u8 = undefined;
        if ((reader.interface.readSliceShort(&extra) catch return reader.err.?) != 0)
            return error.InputLimit;
    }
    return output[0..n];
}

/// Check a complete rendered output before the first write. No seek/stat needed.
pub fn writeBounded(file: File, io: Io, bytes: []const u8, limit: usize) (File.Writer.Error || error{OutputLimit})!void {
    if (bytes.len > limit) return error.OutputLimit;
    try file.writeStreamingAll(io, bytes);
}
