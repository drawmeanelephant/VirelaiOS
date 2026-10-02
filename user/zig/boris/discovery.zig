//! Bounded native input capture, not a coherent-tree or fd-stat substitute.
//! Each snapshot/open is contained by B3. Identities describe snapshot rows,
//! not an earlier/later open file. Full Boris build stays blocked on that gap.
const std = @import("std");
const sdk = @import("sdk");
pub const fs = sdk.fs;
const helpers = sdk.io_helpers;
const boris = @import("boris");
const policy = @import("policy.zig");

pub const Entry = struct { path: []const u8, metadata: fs.Metadata };
pub const Inventory = struct {
    allocator: std.mem.Allocator,
    entries: std.ArrayList(Entry) = .empty,
    root_identity: fs.Identity,

    pub fn deinit(self: *Inventory) void {
        for (self.entries.items) |entry| self.allocator.free(entry.path);
        self.entries.deinit(self.allocator);
    }
};

/// Materialize one directory at a time, including ignored trees/entries.
/// No cursor survives to the next directory, regardless of nesting depth.
pub fn discover(backend: anytype, allocator: std.mem.Allocator, root: []const u8) !Inventory {
    try fs.rootedPath(root, "");
    const root_meta = try backend.metadata(root, "");
    if (root_meta.kind != .directory) return error.ContentDirMissing;
    var result = Inventory{ .allocator = allocator, .root_identity = root_meta.identity };
    errdefer result.deinit();
    try directory(backend, &result, root, "");
    var index: usize = 0;
    while (index < result.entries.items.len) : (index += 1) {
        const entry = result.entries.items[index];
        if (entry.metadata.kind != .directory) continue;
        // This is a fresh PATH query, not fd metadata or pinned-root continuity.
        const current = try backend.metadata(root, entry.path);
        if (current.kind != .directory or !current.identity.eql(entry.metadata.identity))
            return error.TreeChanged;
        try directory(backend, &result, root, entry.path);
    }
    return result;
}

fn directory(backend: anytype, result: *Inventory, root: []const u8, relative: []const u8) !void {
    const cursor = try backend.openSnapshot(root, relative);
    const outcome = materialize(backend, result, root, relative, cursor);
    // Cleanup refusal is an error too; the tracked cursor remains available to
    // top-level closeAll/death cleanup. Never silently imply successful close.
    try backend.closeSnapshot(cursor);
    try outcome;
}

fn materialize(backend: anytype, result: *Inventory, root: []const u8, relative: []const u8, cursor: fs.Snapshot) !void {
    const page = try result.allocator.create(fs.Page);
    defer result.allocator.destroy(page);
    var offset: u64 = 0;
    while (true) {
        try backend.snapshotPage(cursor, offset, fs.page_max, page);
        for (page.rows()) |*row| {
            if (result.entries.items.len == policy.entry_limit) return error.TreeLimit;
            const path = if (relative.len == 0)
                try result.allocator.dupe(u8, row.nameBytes())
            else
                try std.fmt.allocPrint(result.allocator, "{s}/{s}", .{ relative, row.nameBytes() });
            errdefer result.allocator.free(path);
            try fs.rootedPath(root, path);
            try policy.path(path);
            if (row.metadata.kind == .directory) {
                if (row.metadata.identity.eql(result.root_identity)) return error.SymlinkCycle;
                for (result.entries.items) |prior| {
                    if (prior.metadata.kind == .directory and prior.metadata.identity.eql(row.metadata.identity))
                        return error.SymlinkCycle;
                }
            }
            try result.entries.append(result.allocator, .{ .path = path, .metadata = row.metadata });
        }
        offset = page.header.next;
        if (page.done()) break;
    }
}

pub const Capture = struct {
    allocator: std.mem.Allocator,
    files: std.ArrayList(boris.SourceFile) = .empty,
    bytes: usize = 0,

    pub fn deinit(self: *Capture) void {
        for (self.files.items) |file| {
            self.allocator.free(file.path);
            self.allocator.free(file.bytes);
        }
        self.files.deinit(self.allocator);
    }
};

/// Diagnostic compiler input: real contained existing-file opens and bounded
/// EOF reads. No stat-then-legacy-open, old-path fd-stat, or metadata-sized read.
/// NOT proof that the open identities equal the inventory identities.
pub fn capture(backend: anytype, allocator: std.mem.Allocator, root: []const u8, inventory: *const Inventory) !Capture {
    var result = Capture{ .allocator = allocator };
    errdefer result.deinit();
    const buffer = try allocator.alloc(u8, policy.file_limit);
    defer allocator.free(buffer);
    for (inventory.entries.items) |entry| {
        if (entry.metadata.kind != .file) continue;
        const file = try backend.openContained(root, entry.path, .read);
        const bytes = helpers.readBounded(file, backend.io(), buffer[0..@min(policy.file_limit, policy.input_limit - result.bytes)]) catch |err| {
            try backend.closeChecked(file);
            return err;
        };
        try backend.closeChecked(file);
        const path = try allocator.dupe(u8, entry.path);
        errdefer allocator.free(path);
        const retained = try allocator.dupe(u8, bytes);
        errdefer allocator.free(retained);
        try result.files.append(allocator, .{ .path = path, .bytes = retained });
        result.bytes += bytes.len;
    }
    return result;
}
