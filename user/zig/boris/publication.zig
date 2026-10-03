//! Serial, single-entry publication over B5 pins. Not a site transaction.
const std = @import("std");
const fs = @import("sdk").fs;
const policy = @import("policy.zig");

pub const State = struct {
    stage_name: [45]u8 = undefined,
    staged: bool = false,
    submitted: bool = false,
    published: usize = 0,
};
const OwnedDirectory = struct { path: []const u8, identity: fs.Identity };

/// No normalization: aliases, traversal and overlapping source/output roots
/// refuse before entropy or mutation. Both roots must already exist.
pub fn validateRoots(content: []const u8, output: []const u8) !void {
    try fs.rootedPath(content, "");
    try fs.rootedPath(output, "");
    if (overlaps(content, output) or overlaps(output, content))
        return error.TargetOutputCollision;
}
fn overlaps(prefix: []const u8, child: []const u8) bool {
    return std.mem.eql(u8, prefix, child) or
        (std.mem.startsWith(u8, child, prefix) and child.len > prefix.len and child[prefix.len] == '/');
}

fn ownedIdentity(owned: []const OwnedDirectory, path: []const u8) ?fs.Identity {
    for (owned) |dir| if (std.mem.eql(u8, dir.path, path)) return dir.identity;
    return null;
}

/// Keep only the supplied root and the current child, never one pin per depth.
/// When walking our stage, every child must still be the directory we created.
fn parent(backend: anytype, root: fs.Directory, path: []const u8, create: bool, owned: ?*std.ArrayList(OwnedDirectory), allocator: std.mem.Allocator) !fs.Directory {
    if (path.len == 0) return root;
    var current = root;
    errdefer if (current.token != root.token) backend.closeDirectory(current) catch {};
    var parts = std.mem.splitScalar(u8, path, '/');
    var end: usize = 0;
    while (parts.next()) |name| {
        end += name.len;
        const prefix = path[0..end];
        const next = backend.openChildDirectory(current, name) catch |err| blk: {
            if (err != error.FileNotFound or !create) return err;
            // Allocate tracking BEFORE creating an owned directory.
            if (owned) |dirs| {
                try dirs.ensureUnusedCapacity(allocator, 1);
                const retained = try allocator.dupe(u8, prefix);
                errdefer allocator.free(retained);
                const child = try backend.makeDirectory(current, name);
                errdefer backend.closeDirectory(child) catch {};
                const metadata = try backend.directoryMetadata(child);
                dirs.appendAssumeCapacity(.{ .path = retained, .identity = metadata.identity });
                break :blk child;
            }
            break :blk try backend.makeDirectory(current, name);
        };
        errdefer backend.closeDirectory(next) catch {};
        if (owned) |dirs| {
            const expected = ownedIdentity(dirs.items, prefix) orelse return error.StageChanged;
            if (!(try backend.directoryMetadata(next)).identity.eql(expected))
                return error.StageChanged;
        }
        if (current.token != root.token) try backend.closeDirectory(current);
        current = next;
        end += 1;
    }
    return current;
}
fn closeParent(backend: anytype, root: fs.Directory, dir: fs.Directory) !void {
    if (dir.token != root.token) try backend.closeDirectory(dir);
}
fn dirname(path: []const u8) []const u8 {
    return if (std.mem.lastIndexOfScalar(u8, path, '/')) |slash| path[0..slash] else "";
}
fn basename(path: []const u8) []const u8 {
    return if (std.mem.lastIndexOfScalar(u8, path, '/')) |slash| path[slash + 1 ..] else path;
}

/// First compile/validate ALL records, then entropy, exclusive stage, complete
/// writes and file sync, then one contained rename per artifact. Earlier
/// replacements survive later failures. A submitted failure can be unknown.
/// Never delete/copy a destination or pretend to roll back. Failed stages are
/// retained by name for inspection, not recursively removed through old paths.
pub fn publish(backend: anytype, allocator: std.mem.Allocator, content: []const u8, output: []const u8, records: anytype, state: *State) !void {
    try validateRoots(content, output);
    try policy.outputs(records);
    // Stage prefix adds a component and 46 bytes to each native full path.
    for (records) |record| {
        if (output.len + 1 + state.stage_name.len + 1 + record.path.len > fs.path_max)
            return error.NameTooLong;
        var depth: usize = 1;
        for (record.path) |byte| if (byte == '/') {
            depth += 1;
        };
        var root_depth: usize = 0;
        for (output[5..]) |byte| if (byte == '/') {
            root_depth += 1;
        };
        if (root_depth + 1 + depth > fs.pinned_depth_max) return error.ResourceLimit;
        // File/directory collisions must refuse before any output mutation.
        for (records) |other| {
            if (record.path.ptr == other.path.ptr) continue;
            if (overlaps(record.path, other.path)) return error.InvalidArtifactTree;
        }
    }
    const out = try backend.openDirectory(output, "");
    defer backend.closeDirectory(out) catch {};
    const source = try backend.metadata(content, "");
    if (source.identity.eql((try backend.directoryMetadata(out)).identity))
        return error.TargetOutputCollision;
    var owned: std.ArrayList(OwnedDirectory) = .empty;
    defer {
        for (owned.items) |dir| allocator.free(dir.path);
        owned.deinit(allocator);
    }
    try owned.ensureUnusedCapacity(allocator, 1);
    const stage_path = try allocator.alloc(u8, state.stage_name.len);
    errdefer if (!state.staged) allocator.free(stage_path);
    var entropy: [16]u8 = undefined;
    // Real SDK randomSecure uses slot 72, loops short fills, fails on zero/error.
    try backend.io().randomSecure(&entropy);
    @memcpy(state.stage_name[0..13], ".boris-stage-");
    const hex = "0123456789abcdef";
    for (entropy, 0..) |byte, i| {
        state.stage_name[13 + i * 2] = hex[byte >> 4];
        state.stage_name[14 + i * 2] = hex[byte & 15];
    }
    @memcpy(stage_path, &state.stage_name);
    const stage = try backend.makeDirectory(out, &state.stage_name);
    state.staged = true;
    const stage_metadata = backend.directoryMetadata(stage) catch |err| {
        allocator.free(stage_path);
        backend.closeDirectory(stage) catch {};
        return err;
    };
    owned.appendAssumeCapacity(.{ .path = stage_path, .identity = stage_metadata.identity });
    try backend.closeDirectory(stage);

    for (records) |record| {
        const relative = try std.fmt.allocPrint(allocator, "{s}/{s}", .{ state.stage_name, dirname(record.path) });
        defer allocator.free(relative);
        const path = std.mem.trimEnd(u8, relative, "/");
        const dir = try parent(backend, out, path, true, &owned, allocator);
        const file = backend.createExclusive(dir, basename(record.path)) catch |err| {
            try closeParent(backend, out, dir);
            return err;
        };
        const written: ?anyerror = blk: {
            file.writeStreamingAll(backend.io(), record.bytes) catch |err| break :blk err;
            file.sync(backend.io()) catch |err| break :blk err;
            const metadata = backend.fileMetadata(file) catch |err| break :blk err;
            if (metadata.size != record.bytes.len) break :blk error.WriteFailed;
            break :blk null;
        };
        try backend.closeChecked(file);
        try closeParent(backend, out, dir);
        if (written) |err| return err;
    }
    for (records) |record| {
        const relative = try std.fmt.allocPrint(allocator, "{s}/{s}", .{ state.stage_name, dirname(record.path) });
        defer allocator.free(relative);
        const from = try parent(backend, out, std.mem.trimEnd(u8, relative, "/"), false, &owned, allocator);
        const to = parent(backend, out, dirname(record.path), true, null, allocator) catch |err| {
            try closeParent(backend, out, from);
            return err;
        };
        state.submitted = true;
        const renamed = backend.rename(from, basename(record.path), to, basename(record.path), .replace);
        try closeParent(backend, out, to);
        try closeParent(backend, out, from);
        try renamed;
        state.published += 1;
    }
    // All files moved. Remove ONLY empty directories we created, deepest first.
    var index = owned.items.len;
    while (index > 0) {
        index -= 1;
        const dir = owned.items[index];
        const child = try parent(backend, out, dir.path, false, &owned, allocator);
        try closeParent(backend, out, child);
        const containing = try parent(backend, out, dirname(dir.path), false, &owned, allocator);
        const removed = backend.removeDirectory(containing, basename(dir.path));
        try closeParent(backend, out, containing);
        try removed;
    }
    state.staged = false;
}
