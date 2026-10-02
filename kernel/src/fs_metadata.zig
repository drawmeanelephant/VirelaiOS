//! B3 (#1870): honest metadata and a bounded, no-follow traversal core.
//!
//! The live adapter is virtio_fs.zig; file_table.zig authorizes every use,
//! and syscall slot 79 exposes the bounded metadata/contained operations.
//! An adapter may opt into `ContainedOps` only when it pins each lookup
//! result, never follows links, and operates on those pins rather than
//! re-opening a checked pathname. A stat-then-path-open adapter is unsafe.
//! Backends without that primitive explicitly return ContainmentUnavailable.
//!
//! FUSE attributes are decoded without path hashes, synthetic directory
//! inodes, clamped sizes, default timestamps, or host-to-guest ownership
//! translation. `filesystem` identifies the adapter's filesystem lifetime;
//! it must be nonzero, stable for that lifetime, and change on remount.

const std = @import("std");
const directory = @import("directory.zig");

pub const Error = error{
    InvalidPath,
    PathLimit,
    TreeLimit,
    InvalidMetadata,
    MetadataUnavailable,
    ContainmentUnavailable,
    FileNotFound,
    AccessDenied,
    NotDirectory,
    NotFile,
    SymlinkRejected,
    StaleIdentity,
    HandleLimit,
    Io,
};

pub const Kind = enum(u32) {
    file,
    directory,
    symlink,
    character_device,
    block_device,
    fifo,
    socket,
};

pub const Identity = extern struct {
    filesystem: u64,
    inode: u64,

    pub fn eql(a: Identity, b: Identity) bool {
        return a.filesystem == b.filesystem and a.inode == b.inode;
    }
};

pub const Timestamp = struct {
    seconds: i64,
    nanoseconds: u32,

    pub fn toNanoseconds(self: Timestamp) i128 {
        return @as(i128, self.seconds) * 1_000_000_000 + self.nanoseconds;
    }
};

/// All fields are required. Host mode/uid/gid are facts from FUSE, NOT the
/// Virelai principal/mode policy; the caller must still authorize each use.
pub const Metadata = struct {
    identity: Identity,
    kind: Kind,
    size: u64,
    atime: Timestamp,
    mtime: Timestamp,
    ctime: Timestamp,
    host_mode: u32,
    host_uid: u32,
    host_gid: u32,

    pub fn validate(self: Metadata) Error!void {
        if (self.identity.filesystem == 0 or self.identity.inode == 0)
            return error.InvalidMetadata;
        for ([_]Timestamp{ self.atime, self.mtime, self.ctime }) |t| {
            if (t.nanoseconds >= 1_000_000_000) return error.InvalidMetadata;
        }
        if (try kindFromMode(self.host_mode) != self.kind) return error.InvalidMetadata;
    }

    /// Boris's watch prerequisite, not a watcher or a filesystem change feed.
    pub fn watchChanged(old: Metadata, new: Metadata) bool {
        return old.size != new.size or old.mtime.toNanoseconds() != new.mtime.toNanoseconds();
    }
};

fn read32(bytes: []const u8, off: usize) u32 {
    return std.mem.readInt(u32, bytes[off..][0..4], .little);
}

fn read64(bytes: []const u8, off: usize) u64 {
    return std.mem.readInt(u64, bytes[off..][0..8], .little);
}

fn kindFromMode(mode: u32) Error!Kind {
    // Avoid LLVM's pointer table of constant error-union results. Such
    // absolute rodata pointers are not relocated by the current encoder.
    var kind: Kind = undefined;
    const out: *volatile Kind = &kind;
    switch (mode & 0o170000) {
        0o100000 => out.* = .file,
        0o040000 => out.* = .directory,
        0o120000 => out.* = .symlink,
        0o020000 => out.* = .character_device,
        0o060000 => out.* = .block_device,
        0o010000 => out.* = .fifo,
        0o140000 => out.* = .socket,
        else => return error.InvalidMetadata,
    }
    return kind;
}

pub const fuse_attr_bytes: usize = 88;
pub const fuse_entry_bytes: usize = 128;
pub const fuse_getattr_bytes: usize = 104;

/// FUSE 7.31 `fuse_attr`, used by both LOOKUP and GETATTR. Timestamp zero
/// is accepted only when actually present in the reply, never as a fallback.
pub fn decodeFuseAttr(filesystem: u64, bytes: []const u8) Error!Metadata {
    if (bytes.len < fuse_attr_bytes) return error.InvalidMetadata;
    const mode = read32(bytes, 60);
    const result = Metadata{
        .identity = .{ .filesystem = filesystem, .inode = read64(bytes, 0) },
        .kind = try kindFromMode(mode),
        .size = read64(bytes, 8),
        .atime = .{ .seconds = @bitCast(read64(bytes, 24)), .nanoseconds = read32(bytes, 48) },
        .mtime = .{ .seconds = @bitCast(read64(bytes, 32)), .nanoseconds = read32(bytes, 52) },
        .ctime = .{ .seconds = @bitCast(read64(bytes, 40)), .nanoseconds = read32(bytes, 56) },
        .host_mode = mode,
        .host_uid = read32(bytes, 68),
        .host_gid = read32(bytes, 72),
    };
    try result.validate();
    return result;
}

pub const Lookup = struct {
    node: u64,
    generation: u64,
    metadata: Metadata,
};

pub fn decodeFuseLookup(filesystem: u64, bytes: []const u8) Error!Lookup {
    if (bytes.len < fuse_entry_bytes or read64(bytes, 0) == 0)
        return error.InvalidMetadata;
    return .{
        .node = read64(bytes, 0),
        .generation = read64(bytes, 8),
        .metadata = try decodeFuseAttr(filesystem, bytes[40..]),
    };
}

pub fn decodeFuseGetattr(filesystem: u64, bytes: []const u8) Error!Metadata {
    if (bytes.len < fuse_getattr_bytes) return error.InvalidMetadata;
    return decodeFuseAttr(filesystem, bytes[16..]);
}

/// These are transport-side FUSE error numbers, never native syscall errno.
/// An unrecognized or malformed response cannot turn into not-found or EOF.
pub fn fuseError(errno: i32) Error {
    return switch (errno) {
        -1, -13 => error.AccessDenied,
        -2 => error.FileNotFound,
        -20 => error.NotDirectory,
        -40 => error.SymlinkRejected,
        -38, -95 => error.MetadataUnavailable,
        -116 => error.StaleIdentity,
        0...std.math.maxInt(i32) => error.InvalidMetadata,
        else => error.Io,
    };
}

// B2's approved full-native-path/name ceilings, still bounded below a root.
pub const guest_prefix = "/host/";
pub const max_guest_path: usize = directory.path_max;
pub const max_relative_path: usize = max_guest_path - guest_prefix.len;
pub const max_component: usize = directory.name_max;
pub const max_depth: usize = directory.depth_max;

pub const stat_op: u64 = 0;
pub const identity_op: u64 = 1;
pub const read_open_op: u64 = 2;
pub const write_open_op: u64 = 3;
pub const dir_open_op: u64 = 4;
pub const dir_page_op: u64 = 5;
pub const dir_close_op: u64 = 6;

pub const WireTime = extern struct {
    seconds: i64,
    nanoseconds: u32,
    reserved: u32 = 0,
};
/// Host attributes are facts, never guest ownership/capabilities.
pub const Wire = extern struct {
    version: u32 = 1,
    kind: Kind,
    identity: Identity,
    size: u64,
    atime: WireTime,
    mtime: WireTime,
    ctime: WireTime,
    host_mode: u32,
    host_uid: u32,
    host_gid: u32,
    reserved: u32 = 0,

    pub fn from(value: Metadata) Wire {
        return .{
            .kind = value.kind,
            .identity = value.identity,
            .size = value.size,
            .atime = .{ .seconds = value.atime.seconds, .nanoseconds = value.atime.nanoseconds },
            .mtime = .{ .seconds = value.mtime.seconds, .nanoseconds = value.mtime.nanoseconds },
            .ctime = .{ .seconds = value.ctime.seconds, .nanoseconds = value.ctime.nanoseconds },
            .host_mode = value.host_mode,
            .host_uid = value.host_uid,
            .host_gid = value.host_gid,
        };
    }
};

pub const Entry = extern struct {
    name: [256]u8 = .{0} ** 256,
    name_len: u16,
    reserved: [6]u8 = .{0} ** 6,
    metadata: Wire,
};
pub const Page = extern struct {
    header: directory.Header = .{},
    entries: [directory.page_max]Entry = undefined,
};
pub const Snapshot = struct {
    count: usize = 0,
    entries: [directory.entry_max]Entry = undefined,

    pub fn append(self: *Snapshot, name: []const u8, value: Metadata) Error!void {
        if (!directory.valid_name(name)) return error.InvalidMetadata;
        try value.validate();
        if (value.kind == .symlink) return error.SymlinkRejected;
        if (self.count == directory.entry_max) return error.TreeLimit;
        for (self.entries[0..self.count]) |entry| {
            if (std.mem.eql(u8, entry.name[0..entry.name_len], name)) return error.StaleIdentity;
        }
        var entry = Entry{ .name_len = @intCast(name.len), .metadata = Wire.from(value) };
        @memcpy(entry.name[0..name.len], name);
        self.entries[self.count] = entry;
        self.count += 1;
    }

    fn less(_: void, a: Entry, b: Entry) bool {
        return std.mem.order(u8, a.name[0..a.name_len], b.name[0..b.name_len]) == .lt;
    }

    pub fn sort(self: *Snapshot) void {
        // Block sort's 512-entry cache consumes 184,320 stack bytes here.
        // Names are unique, so stable ordering is unnecessary.
        std.sort.heap(Entry, self.entries[0..self.count], {}, less);
    }
};
comptime {
    if (@sizeOf(Wire) != 96 or @sizeOf(Entry) != 360)
        @compileError("B3 metadata wire layout changed");
}

pub fn validatePath(path: []const u8) Error!void {
    if (path.len > max_relative_path) return error.PathLimit;
    if (std.mem.indexOfScalar(u8, path, 0) != null) return error.InvalidPath;
    if (path.len == 0) return; // the supplied content root itself
    var parts = std.mem.splitScalar(u8, path, '/');
    var depth: usize = 0;
    while (parts.next()) |part| {
        if (part.len == 0 or std.mem.eql(u8, part, ".") or std.mem.eql(u8, part, ".."))
            return error.InvalidPath;
        if (part.len > max_component) return error.PathLimit;
        depth += 1;
        if (depth > max_depth) return error.TreeLimit;
    }
}

fn validateRootedPath(root: []const u8, path: []const u8) Error!void {
    try validatePath(root);
    try validatePath(path);
    const separator: usize = if (root.len != 0 and path.len != 0) 1 else 0;
    if (root.len + separator + path.len > max_relative_path) return error.PathLimit;
}

/// A backend-owned pin, not an unchecked pathname, cached stat, or EL0 fd.
pub const Object = struct {
    token: u64,
    metadata: Metadata,
};

pub const Access = enum { metadata, read, write };
pub const Authorizer = struct {
    context: *anyopaque,
    /// Paths are relative to Backend.root_path. The adapter must join that
    /// root before applying the real Virelai principal policy. Empty path
    /// means root. Called before lookup/open and at every use.
    check: *const fn (*anyopaque, []const u8, Access) Error!void,
};

pub const ContainedOps = struct {
    /// Pins the actual supplied root without following a root symlink.
    root: *const fn (*anyopaque) Error!Object,
    /// Looks up one component relative to the pinned directory, no-follow.
    /// The returned object stays pinned until release, even if renamed.
    lookup: *const fn (*anyopaque, Object, []const u8) Error!Object,
    release: *const fn (*anyopaque, Object) void,
    /// Reads attributes from the pin, not its former pathname.
    stat: *const fn (*anyopaque, Object) Error!Metadata,
    /// Opens that exact pinned regular file, no-follow, without truncating.
    /// Inability to preserve identity/containment is an explicit refusal.
    open: *const fn (*anyopaque, Object, Access) Error!u64,
    close: *const fn (*anyopaque, u64) Error!void,
    read: *const fn (*anyopaque, u64, []u8) Error!usize,
    write: *const fn (*anyopaque, u64, []const u8) Error!usize,
};

pub const Backend = struct {
    context: *anyopaque,
    /// Canonical, share-relative spelling of the root pinned by `ops.root`.
    /// Accounted in the full /host/ path budget, not an unchecked host path.
    root_path: []const u8,
    ops: ?*const ContainedOps,

    fn contained(self: Backend) Error!*const ContainedOps {
        return self.ops orelse error.ContainmentUnavailable;
    }
};

fn noSymlink(metadata: Metadata) Error!void {
    try metadata.validate();
    if (metadata.kind == .symlink) return error.SymlinkRejected;
}

pub fn checkedStat(backend: Backend, object: Object) Error!Metadata {
    const result = try (try backend.contained()).stat(backend.context, object);
    try noSymlink(result);
    if (!result.identity.eql(object.metadata.identity) or result.kind != object.metadata.kind)
        return error.StaleIdentity;
    return result;
}

fn authorizePath(auth: Authorizer, path: []const u8, access: Access) Error!void {
    try auth.check(auth.context, "", .metadata);
    var at: usize = 0;
    while (at < path.len) {
        const end = std.mem.indexOfScalarPos(u8, path, at, '/') orelse path.len;
        try auth.check(auth.context, path[0..end], if (end == path.len) access else .metadata);
        at = end + 1;
    }
}

/// At most two simultaneous pins. No per-depth handles, heap, or recursion.
/// The caller owns the returned pin and must release it.
pub fn resolve(backend: Backend, auth: Authorizer, path: []const u8, access: Access) Error!Object {
    try validateRootedPath(backend.root_path, path);
    const ops = try backend.contained();
    try auth.check(auth.context, "", .metadata);
    var current = try ops.root(backend.context);
    errdefer ops.release(backend.context, current);
    try noSymlink(current.metadata);
    if (current.metadata.kind != .directory) return error.NotDirectory;
    const filesystem = current.metadata.identity.filesystem;
    if (path.len == 0) return current;

    var at: usize = 0;
    while (at < path.len) {
        const end = std.mem.indexOfScalarPos(u8, path, at, '/') orelse path.len;
        const want: Access = if (end == path.len) access else .metadata;
        try auth.check(auth.context, path[0..end], want);
        const child = try ops.lookup(backend.context, current, path[at..end]);
        ops.release(backend.context, current);
        current = child;
        try noSymlink(current.metadata);
        if (current.metadata.identity.filesystem != filesystem)
            return error.ContainmentUnavailable;
        if (end < path.len and current.metadata.kind != .directory)
            return error.NotDirectory;
        at = end + 1;
    }
    return current;
}

pub fn stat(backend: Backend, auth: Authorizer, path: []const u8) Error!Metadata {
    const ops = try backend.contained();
    const object = try resolve(backend, auth, path, .metadata);
    defer ops.release(backend.context, object);
    try authorizePath(auth, path, .metadata);
    return checkedStat(backend, object);
}

/// Retains the pathname solely for reauthorization. Reads/writes use the
/// backend handle, so swapping the former path for a symlink cannot redirect
/// an already-open stream. No successful EOF is returned for backend errors.
pub const File = struct {
    backend: Backend,
    auth: Authorizer,
    token: u64,
    access: Access,
    path: [max_relative_path]u8,
    path_len: usize,
    closed: bool = false,

    pub fn read(self: *File, out: []u8) Error!usize {
        if (self.closed) return error.StaleIdentity;
        if (self.access != .read) return error.AccessDenied;
        try authorizePath(self.auth, self.path[0..self.path_len], .read);
        const n = try (try self.backend.contained()).read(self.backend.context, self.token, out);
        if (n > out.len) return error.InvalidMetadata;
        return n;
    }

    pub fn write(self: *File, bytes: []const u8) Error!usize {
        if (self.closed) return error.StaleIdentity;
        if (self.access != .write) return error.AccessDenied;
        try authorizePath(self.auth, self.path[0..self.path_len], .write);
        const n = try (try self.backend.contained()).write(self.backend.context, self.token, bytes);
        if (n > bytes.len) return error.InvalidMetadata;
        if (bytes.len != 0 and n == 0) return error.Io;
        return n;
    }

    pub fn close(self: *File) Error!void {
        if (self.closed) return error.StaleIdentity;
        const ops = self.backend.ops orelse unreachable;
        try ops.close(self.backend.context, self.token);
        self.closed = true;
    }
};

pub fn openToken(backend: Backend, auth: Authorizer, path: []const u8, access: Access) Error!u64 {
    if (access == .metadata) return error.NotFile;
    const ops = try backend.contained();
    const object = try resolve(backend, auth, path, access);
    defer ops.release(backend.context, object);
    const metadata = try checkedStat(backend, object);
    if (metadata.kind != .file) return error.NotFile;
    try authorizePath(auth, path, access);
    return ops.open(backend.context, object, access);
}

pub fn open(backend: Backend, auth: Authorizer, path: []const u8, access: Access) Error!File {
    const token = try openToken(backend, auth, path, access);
    var result = File{
        .backend = backend,
        .auth = auth,
        .token = token,
        .access = access,
        .path = undefined,
        .path_len = path.len,
    };
    @memcpy(result.path[0..path.len], path);
    return result;
}

// Unit fixtures are an explicit in-memory backend. Passing these tests does
// not opt VirtioFS or the legacy host channel into the containment contract.
fn testAttr(inode: u64, mode: u32) [fuse_attr_bytes]u8 {
    var bytes = [_]u8{0} ** fuse_attr_bytes;
    std.mem.writeInt(u64, bytes[0..8], inode, .little);
    std.mem.writeInt(u64, bytes[8..16], 0x1_0000_0017, .little);
    std.mem.writeInt(i64, bytes[24..32], -1, .little);
    std.mem.writeInt(u64, bytes[32..40], 1_790_897_123, .little);
    std.mem.writeInt(u64, bytes[40..48], 1_790_897_124, .little);
    std.mem.writeInt(u32, bytes[48..52], 123, .little);
    std.mem.writeInt(u32, bytes[52..56], 456, .little);
    std.mem.writeInt(u32, bytes[56..60], 789, .little);
    std.mem.writeInt(u32, bytes[60..64], mode, .little);
    std.mem.writeInt(u32, bytes[68..72], 501, .little);
    std.mem.writeInt(u32, bytes[72..76], 20, .little);
    return bytes;
}

const Fixture = struct {
    const Edge = struct { parent: u64, name: []const u8, child: u64 };
    objects: [5]Metadata,
    edges: [4]Edge = .{
        .{ .parent = 0, .name = "a", .child = 1 },
        .{ .parent = 1, .name = "b", .child = 2 },
        .{ .parent = 2, .name = "page.md", .child = 3 },
        .{ .parent = 0, .name = "link", .child = 4 },
    },
    root_token: u64 = 0,
    pins: usize = 0,
    peak_pins: usize = 0,
    handles: usize = 0,
    lookups: usize = 0,
    opens: usize = 0,
    reads: usize = 0,
    writes: usize = 0,
    denies: ?[]const u8 = null,
    deny_access: ?Access = null,
    lookup_failure: ?Error = null,
    stat_failure: ?Error = null,
    open_failure: ?Error = null,
    read_failure: ?Error = null,
    write_failure: ?Error = null,
    close_failure: ?Error = null,
    invalid_count: bool = false,
    zero_write: bool = false,
    replace_after_lookup: ?usize = null,
    replace_before_open: bool = false,
    bytes: [4]u8 = .{ 's', 'a', 'f', 'e' },

    fn init() !Fixture {
        var result: Fixture = .{ .objects = undefined };
        for (&result.objects, 0..) |*metadata, i| {
            const mode: u32 = if (i < 3) 0o040755 else if (i == 3) 0o100644 else 0o120777;
            const bytes = testAttr(100 + i, mode);
            metadata.* = try decodeFuseAttr(7, &bytes);
        }
        return result;
    }

    fn self(context: *anyopaque) *Fixture {
        return @ptrCast(@alignCast(context));
    }

    fn backend(f: *Fixture) Backend {
        return .{ .context = f, .root_path = "", .ops = &ops };
    }

    fn auth(f: *Fixture) Authorizer {
        return .{ .context = f, .check = check };
    }

    fn check(context: *anyopaque, path: []const u8, access: Access) Error!void {
        const f = self(context);
        if (f.denies) |denied| {
            if (std.mem.eql(u8, path, denied) and
                (f.deny_access == null or f.deny_access.? == access))
                return error.AccessDenied;
        }
    }

    fn pin(f: *Fixture, token: u64) Object {
        f.pins += 1;
        f.peak_pins = @max(f.peak_pins, f.pins);
        return .{ .token = token, .metadata = f.objects[token] };
    }

    fn root(context: *anyopaque) Error!Object {
        const f = self(context);
        return f.pin(f.root_token);
    }

    fn lookup(context: *anyopaque, parent: Object, name: []const u8) Error!Object {
        const f = self(context);
        f.lookups += 1;
        if (f.lookup_failure) |err| return err;
        for (&f.edges, 0..) |*edge, i| {
            if (edge.parent == parent.token and std.mem.eql(u8, edge.name, name)) {
                const child = f.pin(edge.child);
                // Replace the name AFTER pinning the original object.
                if (f.replace_after_lookup == i) edge.child = 4;
                return child;
            }
        }
        return error.FileNotFound;
    }

    fn release(context: *anyopaque, _: Object) void {
        self(context).pins -= 1;
    }

    fn attr(context: *anyopaque, object: Object) Error!Metadata {
        const f = self(context);
        if (f.stat_failure) |err| return err;
        return f.objects[object.token];
    }

    fn openFile(context: *anyopaque, object: Object, _: Access) Error!u64 {
        const f = self(context);
        f.opens += 1;
        if (f.open_failure) |err| return err;
        if (f.replace_before_open) f.edges[2].child = 4;
        if (object.token != 3) return error.NotFile;
        f.handles += 1;
        return object.token;
    }

    fn closeFile(context: *anyopaque, _: u64) Error!void {
        const f = self(context);
        if (f.close_failure) |err| return err;
        f.handles -= 1;
    }

    fn readFile(context: *anyopaque, token: u64, out: []u8) Error!usize {
        const f = self(context);
        f.reads += 1;
        if (f.read_failure) |err| return err;
        if (token != 3) return error.StaleIdentity;
        if (f.invalid_count) return out.len + 1;
        const n = @min(out.len, f.bytes.len);
        @memcpy(out[0..n], f.bytes[0..n]);
        return n;
    }

    fn writeFile(context: *anyopaque, token: u64, bytes: []const u8) Error!usize {
        const f = self(context);
        f.writes += 1;
        if (f.write_failure) |err| return err;
        if (token != 3) return error.StaleIdentity;
        if (f.invalid_count) return bytes.len + 1;
        if (f.zero_write) return 0;
        const n = @min(bytes.len, f.bytes.len);
        @memcpy(f.bytes[0..n], bytes[0..n]);
        return n;
    }

    const ops = ContainedOps{
        .root = root,
        .lookup = lookup,
        .release = release,
        .stat = attr,
        .open = openFile,
        .close = closeFile,
        .read = readFile,
        .write = writeFile,
    };
};

test "metadata: FUSE facts preserve 64-bit sizes and all timestamps" {
    const attr = testAttr(0xabcdef0123456789, 0o100640);
    const value = try decodeFuseAttr(7, &attr);
    try std.testing.expectEqual(@as(u64, 0xabcdef0123456789), value.identity.inode);
    try std.testing.expectEqual(@as(u64, 7), value.identity.filesystem);
    try std.testing.expectEqual(@as(u64, 0x1_0000_0017), value.size);
    try std.testing.expectEqual(@as(i128, -999_999_877), value.atime.toNanoseconds());
    try std.testing.expectEqual(@as(i128, 1_790_897_123_000_000_456), value.mtime.toNanoseconds());
    try std.testing.expectEqual(@as(i128, 1_790_897_124_000_000_789), value.ctime.toNanoseconds());
    try std.testing.expectEqual(@as(u32, 0o100640), value.host_mode);
    try std.testing.expectEqual(@as(u32, 501), value.host_uid);
    try std.testing.expectEqual(@as(u32, 20), value.host_gid);
}

test "metadata: all seven FUSE kinds are distinguished" {
    const modes = [_]u32{ 0o100000, 0o040000, 0o120000, 0o020000, 0o060000, 0o010000, 0o140000 };
    const kinds = [_]Kind{ .file, .directory, .symlink, .character_device, .block_device, .fifo, .socket };
    for (modes, kinds) |mode, kind| {
        const attr = testAttr(1, mode | 0o644);
        try std.testing.expectEqual(kind, (try decodeFuseAttr(7, &attr)).kind);
    }
}

test "metadata: every truncated FUSE attribute refuses" {
    const attr = testAttr(1, 0o040755);
    for (0..fuse_attr_bytes) |cut|
        try std.testing.expectError(error.InvalidMetadata, decodeFuseAttr(7, attr[0..cut]));
}

test "metadata: zero identities invalid kinds and nanoseconds refuse" {
    var attr = testAttr(0, 0o040755);
    try std.testing.expectError(error.InvalidMetadata, decodeFuseAttr(7, &attr));
    attr = testAttr(1, 0o040755);
    try std.testing.expectError(error.InvalidMetadata, decodeFuseAttr(0, &attr));
    var inconsistent = try decodeFuseAttr(7, &attr);
    inconsistent.kind = .file;
    try std.testing.expectError(error.InvalidMetadata, inconsistent.validate());
    attr = testAttr(1, 0o755);
    try std.testing.expectError(error.InvalidMetadata, decodeFuseAttr(7, &attr));
    for ([_]usize{ 48, 52, 56 }) |off| {
        attr = testAttr(1, 0o040755);
        std.mem.writeInt(u32, attr[off..][0..4], 1_000_000_000, .little);
        try std.testing.expectError(error.InvalidMetadata, decodeFuseAttr(7, &attr));
    }
}

test "metadata: valid epoch zero comes from actual reply bytes" {
    var attr = [_]u8{0} ** fuse_attr_bytes;
    std.mem.writeInt(u64, attr[0..8], 1, .little);
    std.mem.writeInt(u32, attr[60..64], 0o040755, .little);
    const value = try decodeFuseAttr(7, &attr);
    try std.testing.expectEqual(@as(i128, 0), value.mtime.toNanoseconds());
    try std.testing.expectEqual(@as(u64, 0), value.size);
}

test "metadata: LOOKUP rejects every cut and a zero node id" {
    var entry = [_]u8{0} ** fuse_entry_bytes;
    std.mem.writeInt(u64, entry[0..8], 99, .little);
    std.mem.writeInt(u64, entry[8..16], 17, .little);
    @memcpy(entry[40..], &testAttr(123, 0o040755));
    for (0..fuse_entry_bytes) |cut|
        try std.testing.expectError(error.InvalidMetadata, decodeFuseLookup(7, entry[0..cut]));
    const result = try decodeFuseLookup(7, &entry);
    try std.testing.expectEqual(@as(u64, 99), result.node);
    try std.testing.expectEqual(@as(u64, 17), result.generation);
    try std.testing.expectEqual(@as(u64, 123), result.metadata.identity.inode);
    std.mem.writeInt(u64, entry[0..8], 0, .little);
    try std.testing.expectError(error.InvalidMetadata, decodeFuseLookup(7, &entry));
}

test "metadata: GETATTR rejects every cut and decodes root attributes" {
    var reply = [_]u8{0} ** fuse_getattr_bytes;
    @memcpy(reply[16..], &testAttr(77, 0o040700));
    for (0..fuse_getattr_bytes) |cut|
        try std.testing.expectError(error.InvalidMetadata, decodeFuseGetattr(7, reply[0..cut]));
    const result = try decodeFuseGetattr(7, &reply);
    try std.testing.expectEqual(@as(u64, 77), result.identity.inode);
    try std.testing.expectEqual(Kind.directory, result.kind);
    try std.testing.expectEqual(@as(u64, 0x1_0000_0017), result.size);
}

test "metadata: FUSE failures have explicit distinct meanings" {
    try std.testing.expectEqual(error.FileNotFound, fuseError(-2));
    try std.testing.expectEqual(error.AccessDenied, fuseError(-13));
    try std.testing.expectEqual(error.AccessDenied, fuseError(-1));
    try std.testing.expectEqual(error.NotDirectory, fuseError(-20));
    try std.testing.expectEqual(error.SymlinkRejected, fuseError(-40));
    try std.testing.expectEqual(error.MetadataUnavailable, fuseError(-38));
    try std.testing.expectEqual(error.MetadataUnavailable, fuseError(-95));
    try std.testing.expectEqual(error.StaleIdentity, fuseError(-116));
    try std.testing.expectEqual(error.Io, fuseError(-5));
    try std.testing.expectEqual(error.Io, fuseError(std.math.minInt(i32)));
    try std.testing.expectEqual(error.InvalidMetadata, fuseError(0));
    try std.testing.expectEqual(error.InvalidMetadata, fuseError(2));
}

test "metadata: watch stamp detects independent mtime and size changes" {
    var f = try Fixture.init();
    const before = f.objects[3];
    try std.testing.expect(!Metadata.watchChanged(before, before));
    f.objects[3].mtime.nanoseconds += 1;
    try std.testing.expect(Metadata.watchChanged(before, f.objects[3]));
    f.objects[3] = before;
    f.objects[3].size += 1;
    try std.testing.expect(Metadata.watchChanged(before, f.objects[3]));
    f.objects[3] = before;
    f.objects[3].atime.seconds += 1;
    f.objects[3].ctime.seconds += 1;
    try std.testing.expect(!Metadata.watchChanged(before, f.objects[3]));
}

test "metadata: approved B2 full-path and component budgets are exact" {
    try validatePath("x" ** 255);
    try std.testing.expectError(error.PathLimit, validatePath("x" ** 256));
    const exact = "x" ** 255 ++ "/" ++ "y" ** 250;
    try std.testing.expectEqual(@as(usize, max_guest_path), exact.len + guest_prefix.len);
    try validatePath(exact);
    try std.testing.expectError(error.PathLimit, validatePath(exact ++ "y"));
    const at_content_root = "x" ** 255 ++ "/" ++ "y" ** 242;
    try std.testing.expectEqual(@as(usize, max_guest_path), guest_prefix.len + "content/".len + at_content_root.len);
    try validateRootedPath("content", at_content_root);
    try std.testing.expectError(error.PathLimit, validateRootedPath("content", at_content_root ++ "y"));
    var f = try Fixture.init();
    var backend = f.backend();
    backend.root_path = "content";
    try std.testing.expectError(error.PathLimit, stat(backend, f.auth(), at_content_root ++ "y"));
    try std.testing.expectEqual(@as(usize, 0), f.pins);
}

test "metadata: depth and unnormalized paths refuse before lookup" {
    try validatePath("");
    try validatePath("a/b/c/d/e/f/g/h");
    try std.testing.expectError(error.TreeLimit, validatePath("a/b/c/d/e/f/g/h/i"));
    for ([_][]const u8{ "/a", "a/", "a//b", ".", "..", "a/../b", "a/./b", "a\x00b" }) |path|
        try std.testing.expectError(error.InvalidPath, validatePath(path));
    var f = try Fixture.init();
    try std.testing.expectError(error.InvalidPath, stat(f.backend(), f.auth(), "a/../b"));
    try std.testing.expectEqual(@as(usize, 0), f.lookups);
    try std.testing.expectEqual(@as(usize, 0), f.pins);
}

test "metadata: nested scanner identities are distinct stable and filesystem scoped" {
    var f = try Fixture.init();
    var seen = std.AutoHashMap(Identity, void).init(std.testing.allocator);
    defer seen.deinit();
    for ([_][]const u8{ "", "a", "a/b" }) |path| {
        const first = try stat(f.backend(), f.auth(), path);
        const again = try stat(f.backend(), f.auth(), path);
        try std.testing.expect(first.identity.eql(again.identity));
        try std.testing.expect(!seen.contains(first.identity));
        try seen.put(first.identity, {});
    }
    try std.testing.expectEqual(@as(usize, 3), seen.count());
    const same_inode_other_fs = Identity{ .filesystem = 8, .inode = 100 };
    try std.testing.expect(!seen.contains(same_inode_other_fs));
    try std.testing.expectEqual(@as(usize, 0), f.pins);
    try std.testing.expectEqual(@as(usize, 2), f.peak_pins);
}

test "metadata: root intermediate leaf and dangling symlinks are rejected" {
    for (0..4) |position| {
        var f = try Fixture.init();
        if (position == 0) {
            f.root_token = 4;
        } else {
            f.edges[position - 1].child = 4;
        }
        try std.testing.expectError(error.SymlinkRejected, stat(f.backend(), f.auth(), "a/b/page.md"));
        try std.testing.expectError(error.SymlinkRejected, open(f.backend(), f.auth(), "a/b/page.md", .read));
        try std.testing.expectEqual(@as(usize, 0), f.opens);
        try std.testing.expectEqual(@as(usize, 0), f.pins);
    }
    var f = try Fixture.init();
    try std.testing.expectError(error.SymlinkRejected, stat(f.backend(), f.auth(), "link"));
}

test "metadata: directory replacement after lookup cannot redirect traversal" {
    var f = try Fixture.init();
    f.replace_after_lookup = 0;
    const found = try stat(f.backend(), f.auth(), "a/b/page.md");
    try std.testing.expectEqual(@as(u64, 103), found.identity.inode);
    try std.testing.expectEqual(@as(usize, 0), f.pins);
    try std.testing.expectError(error.SymlinkRejected, stat(f.backend(), f.auth(), "a/b/page.md"));
}

test "metadata: leaf replacement between check and open never opens the replacement" {
    var f = try Fixture.init();
    f.replace_before_open = true;
    var file = try open(f.backend(), f.auth(), "a/b/page.md", .read);
    var bytes: [4]u8 = undefined;
    try std.testing.expectEqual(@as(usize, 4), try file.read(&bytes));
    try std.testing.expectEqualStrings("safe", &bytes);
    try file.close();
    try std.testing.expectError(error.SymlinkRejected, open(f.backend(), f.auth(), "a/b/page.md", .read));
    try std.testing.expectEqual(@as(usize, 0), f.handles);
    try std.testing.expectEqual(@as(usize, 0), f.pins);
}

test "metadata: mutation after leaf replacement writes only the pinned file" {
    var f = try Fixture.init();
    var file = try open(f.backend(), f.auth(), "a/b/page.md", .write);
    f.edges[2].child = 4;
    try std.testing.expectEqual(@as(usize, 4), try file.write("edit"));
    try std.testing.expectEqualStrings("edit", &f.bytes);
    try file.close();
    try std.testing.expectError(error.SymlinkRejected, open(f.backend(), f.auth(), "a/b/page.md", .write));
    try std.testing.expectEqual(@as(usize, 0), f.pins);
}

test "metadata: changed identity or kind at stat refuses instead of trusting old metadata" {
    var f = try Fixture.init();
    const object = try resolve(f.backend(), f.auth(), "a/b/page.md", .metadata);
    defer Fixture.release(&f, object);
    f.objects[3].identity.inode += 1;
    try std.testing.expectError(error.StaleIdentity, checkedStat(f.backend(), object));
    f.objects[3] = f.objects[4];
    try std.testing.expectError(error.SymlinkRejected, checkedStat(f.backend(), object));
}

test "metadata: cross-filesystem lookup is an explicit containment refusal" {
    var f = try Fixture.init();
    f.objects[1].identity.filesystem = 8;
    try std.testing.expectError(error.ContainmentUnavailable, stat(f.backend(), f.auth(), "a/b/page.md"));
    try std.testing.expectEqual(@as(usize, 0), f.pins);
}

test "metadata: missing metadata or containment is never a successful empty stat" {
    var f = try Fixture.init();
    const unsupported = Backend{ .context = &f, .root_path = "", .ops = null };
    try std.testing.expectError(error.ContainmentUnavailable, stat(unsupported, f.auth(), ""));
    try std.testing.expectError(error.ContainmentUnavailable, open(unsupported, f.auth(), "a", .read));
    f.stat_failure = error.MetadataUnavailable;
    try std.testing.expectError(error.MetadataUnavailable, stat(f.backend(), f.auth(), ""));
    try std.testing.expectEqual(@as(usize, 0), f.pins);
}

test "metadata: root intermediate and leaf authorization precedes backend use" {
    for ([_][]const u8{ "", "a", "a/b", "a/b/page.md" }, 0..) |path, allowed_lookups| {
        var f = try Fixture.init();
        f.denies = path;
        try std.testing.expectError(error.AccessDenied, stat(f.backend(), f.auth(), "a/b/page.md"));
        try std.testing.expectEqual(allowed_lookups -| 1, f.lookups);
        try std.testing.expectEqual(@as(usize, 0), f.pins);
    }
}

test "metadata: permission revocation is enforced on every read and write" {
    for ([_]Access{ .read, .write }) |access| {
        for ([_][]const u8{ "", "a", "a/b", "a/b/page.md" }) |denied| {
            var f = try Fixture.init();
            var file = try open(f.backend(), f.auth(), "a/b/page.md", access);
            f.denies = denied;
            var bytes: [4]u8 = undefined;
            if (access == .read) {
                try std.testing.expectError(error.AccessDenied, file.read(&bytes));
            } else {
                try std.testing.expectError(error.AccessDenied, file.write("evil"));
                try std.testing.expectEqualStrings("safe", &f.bytes);
            }
            try std.testing.expectEqual(@as(usize, 0), f.reads + f.writes);
            try file.close();
        }
    }
}

test "metadata: wrong-direction access and closed handles explicitly refuse" {
    var f = try Fixture.init();
    var file = try open(f.backend(), f.auth(), "a/b/page.md", .read);
    try std.testing.expectError(error.AccessDenied, file.write("evil"));
    try file.close();
    var bytes: [4]u8 = undefined;
    try std.testing.expectError(error.StaleIdentity, file.read(&bytes));
    try std.testing.expectError(error.StaleIdentity, file.close());
    try std.testing.expectEqual(@as(usize, 0), f.handles);
}

test "metadata: lookup stat and open failures release all traversal pins" {
    for ([_]Error{ error.FileNotFound, error.AccessDenied, error.Io, error.StaleIdentity, error.HandleLimit }) |err| {
        var f = try Fixture.init();
        f.lookup_failure = err;
        try std.testing.expectError(err, stat(f.backend(), f.auth(), "a/b/page.md"));
        try std.testing.expectEqual(@as(usize, 0), f.pins);
        f.lookup_failure = null;
        f.stat_failure = err;
        try std.testing.expectError(err, open(f.backend(), f.auth(), "a/b/page.md", .read));
        try std.testing.expectEqual(@as(usize, 0), f.pins);
        f.stat_failure = null;
        f.open_failure = err;
        try std.testing.expectError(err, open(f.backend(), f.auth(), "a/b/page.md", .write));
        try std.testing.expectEqual(@as(usize, 0), f.pins);
        try std.testing.expectEqual(@as(usize, 0), f.handles);
    }
}

test "metadata: directory and special-file opens refuse without touching data" {
    var f = try Fixture.init();
    try std.testing.expectError(error.NotFile, open(f.backend(), f.auth(), "a/b", .read));
    try std.testing.expectError(error.NotFile, open(f.backend(), f.auth(), "a/b/page.md", .metadata));
    f.objects[3] = try decodeFuseAttr(7, &testAttr(103, 0o010600));
    try std.testing.expectError(error.NotFile, open(f.backend(), f.auth(), "a/b/page.md", .write));
    try std.testing.expectEqual(@as(usize, 0), f.opens);
    try std.testing.expectEqual(@as(usize, 0), f.pins);
}

test "metadata: failed I/O cannot become EOF or confirmed progress" {
    var f = try Fixture.init();
    var reader = try open(f.backend(), f.auth(), "a/b/page.md", .read);
    var writer = try open(f.backend(), f.auth(), "a/b/page.md", .write);
    f.read_failure = error.Io;
    f.write_failure = error.Io;
    var bytes: [4]u8 = undefined;
    try std.testing.expectError(error.Io, reader.read(&bytes));
    try std.testing.expectError(error.Io, writer.write("evil"));
    try std.testing.expectEqualStrings("safe", &f.bytes);
    f.read_failure = null;
    f.write_failure = null;
    f.invalid_count = true;
    try std.testing.expectError(error.InvalidMetadata, reader.read(&bytes));
    try std.testing.expectError(error.InvalidMetadata, writer.write("evil"));
    f.invalid_count = false;
    f.zero_write = true;
    try std.testing.expectError(error.Io, writer.write("evil"));
    try std.testing.expectEqual(@as(usize, 0), try writer.write(""));
    try reader.close();
    try writer.close();
}

test "metadata: close failure is explicit and does not pretend resource recovery" {
    var f = try Fixture.init();
    var file = try open(f.backend(), f.auth(), "a/b/page.md", .read);
    f.close_failure = error.Io;
    try std.testing.expectError(error.Io, file.close());
    try std.testing.expect(!file.closed);
    try std.testing.expectEqual(@as(usize, 1), f.handles);
    f.close_failure = null;
    try file.close();
    try std.testing.expect(file.closed);
    try std.testing.expectEqual(@as(usize, 0), f.handles);
}

test "B3: rich snapshot wire preserves facts padding and exact entry ceiling" {
    var snapshot = Snapshot{};
    const attr = testAttr(123, 0o040755);
    const value = try decodeFuseAttr(7, &attr);
    try snapshot.append("directory", value);
    const row = snapshot.entries[0];
    try std.testing.expectEqual(@as(u64, 0x1_0000_0017), row.metadata.size);
    try std.testing.expectEqual(value.identity, row.metadata.identity);
    try std.testing.expectEqual(@as(u32, 456), row.metadata.mtime.nanoseconds);
    try std.testing.expectEqualSlices(u8, &([_]u8{0} ** 6), &row.reserved);
    try std.testing.expectEqualSlices(u8, &([_]u8{0} ** 247), row.name[9..]);
    try std.testing.expectEqual(@as(u32, 0), row.metadata.mtime.reserved);
    try std.testing.expectError(error.StaleIdentity, snapshot.append("directory", value));
    const link = try decodeFuseAttr(7, &testAttr(124, 0o120777));
    try std.testing.expectError(error.SymlinkRejected, snapshot.append("link", link));
    var name: [16]u8 = undefined;
    while (snapshot.count < directory.entry_max) {
        try snapshot.append(try std.fmt.bufPrint(&name, "row-{d}", .{snapshot.count}), value);
    }
    try std.testing.expectError(error.TreeLimit, snapshot.append("overflow", value));
    try std.testing.expectEqual(directory.entry_max, snapshot.count);
}

test "B3: supplied root and relative depth budgets remain independent" {
    try validateRootedPath("content/one/two/three", "a/b/c/d/e/f/g/h");
    try std.testing.expectError(error.TreeLimit, validateRootedPath("content", "a/b/c/d/e/f/g/h/i"));
    try std.testing.expectError(error.PathLimit, validateRootedPath("root", "a" ** 255 ++ "/" ++ "b" ** 250));
}

test "B3: in-place snapshot sort preserves every full-capacity row" {
    var snapshot = Snapshot{};
    var name: [16]u8 = undefined;
    for (0..directory.entry_max) |i| {
        const index = directory.entry_max - i - 1;
        var value = try decodeFuseAttr(7, &testAttr(index + 1, 0o100640));
        value.size = index;
        try snapshot.append(try std.fmt.bufPrint(&name, "row-{d:0>3}", .{index}), value);
    }
    for (0..2) |_| {
        snapshot.sort();
        try std.testing.expectEqual(directory.entry_max, snapshot.count);
        for (snapshot.entries[0..snapshot.count], 0..) |entry, i| {
            try std.testing.expectEqualStrings(
                try std.fmt.bufPrint(&name, "row-{d:0>3}", .{i}),
                entry.name[0..entry.name_len],
            );
            try std.testing.expectEqual(@as(u64, i + 1), entry.metadata.identity.inode);
            try std.testing.expectEqual(@as(u64, i), entry.metadata.size);
            try std.testing.expectEqual(@as(u32, 0o100640), entry.metadata.host_mode);
            try std.testing.expect(std.mem.allEqual(u8, entry.name[entry.name_len..], 0));
            try std.testing.expect(std.mem.allEqual(u8, &entry.reserved, 0));
        }
    }
}
