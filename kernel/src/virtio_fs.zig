//! Minimal virtio-fs/FUSE client for the host directory share.
//!
//! The VZ device is the standard virtio-fs device (VID 0x1af4, device ID
//! 26, DID 0x105a), not the save/restore-incompatible custom-virtio device.
//! PCI discovery runs before ExitBootServices; queue setup and FUSE I/O run
//! after the identity map is installed. The driver deliberately implements
//! only the bounded file operations the existing /host API uses.

const std = @import("std");
const builtin = @import("builtin");
const mmio = @import("mmio.zig");
const mmu = @import("mmu.zig");
const pci = @import("pci.zig");
const spinlock = @import("spinlock.zig");

pub const st_ok: u8 = 0;
pub const st_not_found: u8 = 1;
pub const st_is_dir: u8 = 2;
pub const st_truncated: u8 = 3;
pub const st_host_error: u8 = 4;
pub const st_exists: u8 = 5;
pub const st_handle: u8 = 6;

pub const virtio_fs_did: u32 = 0x105a;
pub const virtio_fs_device_id: u32 = 26;
pub const tag = "virelaios";
pub const max_path: usize = 255;
pub const max_io: usize = 2048;

const descriptor_count: usize = 16;
const max_payload: usize = 40 + max_io;
const max_message: usize = 40 + max_payload;
const max_queue: u16 = @intCast(descriptor_count);
const root_nodeid: u64 = 1;
const fuse_in_header_len: usize = 40;
const fuse_out_header_len: usize = 16;
const poll_budget: usize = 16_000_000;
const node_cache_count: usize = 64;
const file_handle_count: usize = 8;

const VirtqDesc = extern struct {
    addr: u64,
    len: u32,
    flags: u16,
    next: u16,
};

const VirtqAvail = extern struct {
    flags: u16,
    idx: u16,
    ring: [descriptor_count]u16,
    used_event: u16,
};

const VirtqUsedElem = extern struct {
    id: u32,
    len: u32,
};

const VirtqUsed = extern struct {
    flags: u16,
    idx: u16,
    ring: [descriptor_count]VirtqUsedElem,
    avail_event: u16,
};

const Queue = struct {
    desc: [descriptor_count]VirtqDesc align(16) = undefined,
    avail: VirtqAvail align(2) = undefined,
    used: VirtqUsed align(4) = undefined,
    last_used: u16 = 0,
    size: u16 = 0,
    notify_off: u16 = 0,
};

const Node = struct {
    path: [max_path]u8 = [_]u8{0} ** max_path,
    path_len: usize = 0,
    nodeid: u64 = 0,
    mode: u32 = 0,
    size: u64 = 0,
    valid: bool = false,
};

const FileHandle = struct {
    nodeid: u64 = 0,
    fuse_handle: u64 = 0,
    cursor: u64 = 0,
    valid: bool = false,
};

pub const DirectoryEntry = struct {
    name: [31]u8 = [_]u8{0} ** 31,
    name_len: usize = 0,
    type: u8 = 0,
    size: u64 = 0,
};

pub const StatResult = struct {
    size: u64 = 0,
    is_dir: bool = false,
};

pub const ReadChunkResult = struct {
    status: u8 = st_host_error,
    bytes: usize = 0,
};

pub var fs_dev: u32 = 32;
pub var fs_common: u64 = 0;
pub var fs_notify: u64 = 0;
pub var fs_device_cfg: u64 = 0;
pub var fs_bar: u64 = 0;
pub var fs_common_bar: u64 = 0;
pub var fs_notify_bar: u64 = 0;
pub var fs_device_bar: u64 = 0;
pub var fs_ready: bool = false;
pub var fs_initialized: bool = false;
pub var fs_init_stage: u8 = 0;
pub var fs_error: i32 = 0;
pub var fs_feature_lo: u32 = 0;
pub var fs_feature_hi: u32 = 0;
pub var fs_minor: u32 = 0;
pub var fs_max_write: u32 = @intCast(max_io);
pub var fs_request_queues: u32 = 0;
pub var fs_request_queue_size: u16 = 0;

var notify_multiplier: u32 = 0;
var request_queue = Queue{};
var hiprio_queue = Queue{};
var next_unique: u64 = 1;
var nodes: [node_cache_count]Node = [_]Node{.{}} ** node_cache_count;
var handles: [file_handle_count]FileHandle = [_]FileHandle{.{}} ** file_handle_count;
var fs_lock = spinlock.IrqSaveSpinlock{};
var request_buf: [max_message]u8 align(16) = undefined;
var reply_buf: [max_message]u8 align(16) = undefined;
var read_result_buf: [max_io]u8 align(16) = undefined;
var read_result_len: usize = 0;

const flag_next: u16 = 1;
const flag_write: u16 = 2;
const fuse_init: u32 = 26;
const fuse_lookup: u32 = 1;
const fuse_getattr: u32 = 3;
const fuse_setattr: u32 = 4;
const fuse_mknod: u32 = 8;
const fuse_mkdir: u32 = 9;
const fuse_unlink: u32 = 10;
const fuse_rmdir: u32 = 11;
const fuse_rename: u32 = 12;
const fuse_open: u32 = 14;
const fuse_read: u32 = 15;
const fuse_write: u32 = 16;
const fuse_release: u32 = 18;
const fuse_fsync: u32 = 20;
const fuse_opendir: u32 = 27;
const fuse_readdir: u32 = 28;
const fuse_releasedir: u32 = 29;

const file_type_mask: u32 = 0o170000;
const file_type_dir: u32 = 0o040000;
const file_type_regular: u32 = 0o100000;
const fattr_size: u32 = 1 << 3;
const fattr_fh: u32 = 1 << 6;
const open_read_only: u32 = 0;
const open_write_only: u32 = 1;
const open_read_write: u32 = 2;
const open_append: u32 = 0o2000;
const dirent_type_dir: u32 = 4;

fn read16(bytes: []const u8, off: usize) u16 {
    return @as(u16, bytes[off]) | (@as(u16, bytes[off + 1]) << 8);
}

fn read32(bytes: []const u8, off: usize) u32 {
    return @as(u32, bytes[off]) |
        (@as(u32, bytes[off + 1]) << 8) |
        (@as(u32, bytes[off + 2]) << 16) |
        (@as(u32, bytes[off + 3]) << 24);
}

fn read64(bytes: []const u8, off: usize) u64 {
    return @as(u64, read32(bytes, off)) | (@as(u64, read32(bytes, off + 4)) << 32);
}

fn write16(bytes: []u8, off: usize, value: u16) void {
    bytes[off] = @truncate(value);
    bytes[off + 1] = @truncate(value >> 8);
}

fn write32(bytes: []u8, off: usize, value: u32) void {
    bytes[off] = @truncate(value);
    bytes[off + 1] = @truncate(value >> 8);
    bytes[off + 2] = @truncate(value >> 16);
    bytes[off + 3] = @truncate(value >> 24);
}

fn write64(bytes: []u8, off: usize, value: u64) void {
    write32(bytes, off, @truncate(value));
    write32(bytes, off + 4, @truncate(value >> 32));
}

pub const FuseReply = struct {
    errno: i32,
    body: []const u8,
};

/// Encode one FUSE request into a bounded byte slice. This pure helper is
/// also the host-testable contract for the 40-byte FUSE input header.
pub fn encode_fuse_request(
    opcode: u32,
    unique: u64,
    nodeid: u64,
    payload: []const u8,
    out: []u8,
) ?usize {
    if (payload.len > max_payload or out.len < fuse_in_header_len + payload.len) return null;
    const len = fuse_in_header_len + payload.len;
    @memset(out[0..len], 0);
    write32(out, 0, @intCast(len));
    write32(out, 4, opcode);
    write64(out, 8, unique);
    write64(out, 16, nodeid);
    @memcpy(out[fuse_in_header_len..][0..payload.len], payload);
    return len;
}

/// Validate and decode the fixed FUSE output header. Error replies are
/// returned with their negative errno and empty body, not treated as
/// malformed transport frames.
pub fn decode_fuse_reply(bytes: []const u8, expected_unique: u64) ?FuseReply {
    if (bytes.len < fuse_out_header_len) return null;
    const len = read32(bytes, 0);
    if (len < fuse_out_header_len or len > bytes.len or read64(bytes, 8) != expected_unique) return null;
    const errno: i32 = @bitCast(read32(bytes, 4));
    return .{
        .errno = errno,
        .body = if (errno == 0) bytes[fuse_out_header_len..len] else bytes[0..0],
    };
}

fn resolve_device(dev: u32) bool {
    var bars: [6]u64 = .{0} ** 6;
    var bi: usize = 0;
    while (bi < bars.len) : (bi += 1) {
        const low = pci.pci_read32(pci.pci_ecam, 0, dev, 0, 0x10 + @as(u32, @intCast(bi)) * 4);
        if ((low & 1) != 0) continue;
        bars[bi] = low & ~@as(u32, 0xf);
        if (((low >> 1) & 3) == 2 and bi + 1 < bars.len) {
            const high = pci.pci_read32(pci.pci_ecam, 0, dev, 0, 0x10 + @as(u32, @intCast(bi + 1)) * 4);
            bars[bi] |= @as(u64, high) << 32;
            bi += 1;
        }
    }

    var common_bar: u32 = 0xff;
    var common_off: u32 = 0;
    var notify_bar: u32 = 0xff;
    var notify_off: u32 = 0;
    var device_bar: u32 = 0xff;
    var device_off: u32 = 0;
    var device_len: u32 = 0;
    var mult: u32 = 0;
    var cap = pci.pci_read32(pci.pci_ecam, 0, dev, 0, 0x34) & 0xff;
    var count: usize = 0;
    while (cap != 0 and cap < 0x100 and (cap & 3) == 0 and count < 16) : (count += 1) {
        const header = pci.pci_read32(pci.pci_ecam, 0, dev, 0, cap);
        const id = header & 0xff;
        const next = (header >> 8) & 0xff;
        if (id == 0x09) {
            const kind = (header >> 24) & 0xff;
            const bar = pci.pci_read32(pci.pci_ecam, 0, dev, 0, cap + 4) & 0xff;
            const offset = pci.pci_read32(pci.pci_ecam, 0, dev, 0, cap + 8);
            const length = pci.pci_read32(pci.pci_ecam, 0, dev, 0, cap + 12);
            switch (kind) {
                1 => {
                    common_bar = bar;
                    common_off = offset;
                },
                2 => {
                    notify_bar = bar;
                    notify_off = offset;
                    mult = pci.pci_read32(pci.pci_ecam, 0, dev, 0, cap + 16);
                },
                4 => {
                    device_bar = bar;
                    device_off = offset;
                    device_len = length;
                },
                else => {},
            }
        }
        cap = next;
    }
    if (common_bar >= bars.len or notify_bar >= bars.len or
        device_bar >= bars.len or device_len < 36) return false;
    fs_common = bars[common_bar] + common_off;
    fs_notify = bars[notify_bar] + notify_off;
    fs_device_cfg = bars[device_bar] + device_off;
    fs_bar = bars[common_bar];
    fs_common_bar = bars[common_bar];
    fs_notify_bar = bars[notify_bar];
    fs_device_bar = bars[device_bar];
    notify_multiplier = mult;
    return true;
}

/// PRE-EXIT PCI-only discovery. False means no standard VZ VirtioFS device.
pub fn probe() bool {
    fs_ready = false;
    fs_initialized = false;
    fs_dev = 32;
    if (pci.pci_ecam == 0) return false;
    var dev: u32 = 0;
    while (dev < 32) : (dev += 1) {
        const id = pci.pci_read32(pci.pci_ecam, 0, dev, 0, 0);
        if ((id & 0xffff) != 0x1af4 or id >> 16 != virtio_fs_did) continue;
        fs_dev = dev;
        if (!resolve_device(dev)) {
            fs_dev = 32;
            return false;
        }
        return true;
    }
    return false;
}

pub fn add_device_windows(out: []mmu.DeviceWindow) usize {
    var count: usize = 0;
    const bases = [_]u64{ fs_common_bar, fs_notify_bar, fs_device_bar };
    for (bases, 0..) |base, i| {
        if (base == 0) continue;
        var duplicate = false;
        for (bases[0..i]) |prior| {
            if (prior == base) duplicate = true;
        }
        if (duplicate or count >= out.len) continue;
        out[count] = .{ .base = base, .len = 0x10000 };
        count += 1;
    }
    return count;
}

fn common_read8(offset: u32) u8 {
    return mmio.mmio_read8(fs_common + offset);
}

fn common_write8(offset: u32, value: u8) void {
    mmio.mmio_write8(fs_common + offset, value);
}

fn common_read16(offset: u32) u16 {
    return mmio.mmio_read16(fs_common + offset);
}

fn common_write16(offset: u32, value: u16) void {
    mmio.mmio_write16(fs_common + offset, value);
}

fn common_read32(offset: u32) u32 {
    return mmio.mmio_read32(fs_common + offset);
}

fn common_write32(offset: u32, value: u32) void {
    mmio.mmio_write32(fs_common + offset, value);
}

fn highest_power_of_two_at_most(n: u16) u16 {
    var size: u16 = 1;
    while (size <= n / 2 and size < max_queue) size *= 2;
    return size;
}

fn init_ring(q: *Queue, queue_index: u16, wanted: u16) bool {
    common_write16(0x16, queue_index);
    const device_size = common_read16(0x18);
    if (device_size == 0) return false;
    const size = @min(highest_power_of_two_at_most(device_size), wanted);
    if (size == 0) return false;
    q.* = .{};
    q.size = size;
    for (&q.desc) |*d| d.* = .{ .addr = 0, .len = 0, .flags = 0, .next = 0 };
    q.avail = .{ .flags = 0, .idx = 0, .ring = [_]u16{0} ** descriptor_count, .used_event = 0 };
    q.used = .{ .flags = 0, .idx = 0, .ring = [_]VirtqUsedElem{.{ .id = 0, .len = 0 }} ** descriptor_count, .avail_event = 0 };
    mmu.clean_dcache_range(@intFromPtr(&q.used), @sizeOf(VirtqUsed));
    const desc = mmu.to_phys(@intFromPtr(&q.desc));
    const avail = mmu.to_phys(@intFromPtr(&q.avail));
    const used = mmu.to_phys(@intFromPtr(&q.used));
    mmio.mmio_write32(fs_common + 0x20, @truncate(desc));
    mmio.mmio_write32(fs_common + 0x24, @truncate(desc >> 32));
    mmio.mmio_write32(fs_common + 0x28, @truncate(avail));
    mmio.mmio_write32(fs_common + 0x2c, @truncate(avail >> 32));
    mmio.mmio_write32(fs_common + 0x30, @truncate(used));
    mmio.mmio_write32(fs_common + 0x34, @truncate(used >> 32));
    common_write16(0x18, size);
    common_write16(0x1c, 1);
    q.notify_off = common_read16(0x1e);
    return common_read16(0x1c) == 1;
}

/// POST-MMU queue setup and FUSE_INIT handshake. Returns false on an
/// unsupported tag, feature set, queue layout, or FUSE protocol version.
pub fn init() bool {
    fs_ready = false;
    fs_initialized = false;
    fs_init_stage = 1; // transport reset
    if (fs_dev >= 32 or fs_common == 0) return false;

    const status = common_read8(0x14);
    if (status != 0) common_write8(0x14, 0);
    var spins: usize = 0;
    while (common_read8(0x14) != 0) : (spins += 1) {
        if (spins >= 1_000_000) return false;
    }
    fs_init_stage = 2; // device request-queue count
    fs_request_queues = mmio.mmio_read32(fs_device_cfg + 36);
    if (fs_request_queues == 0) return false;
    common_write8(0x14, 1 | 2);

    fs_init_stage = 3; // feature negotiation
    common_write32(0x00, 0);
    fs_feature_lo = common_read32(0x04);
    common_write32(0x00, 1);
    fs_feature_hi = common_read32(0x04);
    if ((fs_feature_hi & 1) == 0) return false; // VIRTIO_F_VERSION_1
    common_write32(0x08, 0);
    common_write32(0x0c, 0);
    common_write32(0x08, 1);
    common_write32(0x0c, 1); // accept only VIRTIO_F_VERSION_1
    common_write8(0x14, 1 | 2 | 8);
    fs_init_stage = 4; // FEATURES_OK
    if ((common_read8(0x14) & 8) == 0) return false;

    fs_init_stage = 5; // high-priority queue
    if (!init_ring(&hiprio_queue, 0, 1)) return false;
    fs_init_stage = 6; // request queue
    if (!init_ring(&request_queue, 1, max_queue)) return false;
    common_write8(0x14, 1 | 2 | 8 | 4);
    fs_init_stage = 7; // DRIVER_OK
    if ((common_read8(0x14) & 4) == 0) return false;

    fs_init_stage = 8; // share tag
    var i: usize = 0;
    while (i < 36) : (i += 1) {
        const got = mmio.mmio_read8(fs_device_cfg + i);
        const expected: u8 = if (i < tag.len) tag[i] else 0;
        if (got != expected) return false;
    }
    fs_ready = true;

    fs_init_stage = 9; // FUSE_INIT request/reply transport
    var init_in: [16]u8 = [_]u8{0} ** 16;
    write32(&init_in, 0, 7);
    write32(&init_in, 4, 31);
    write32(&init_in, 8, @intCast(max_io));
    const reply = transact(fuse_init, 0, &init_in) orelse {
        fs_ready = false;
        return false;
    };
    fs_init_stage = 10; // FUSE_INIT reply version/shape
    if (reply.len < 24 or read32(reply, 0) != 7) {
        fs_ready = false;
        return false;
    }
    fs_minor = @min(read32(reply, 4), 31);
    fs_max_write = @min(read32(reply, 20), @as(u32, @intCast(max_io)));
    if (fs_max_write == 0) fs_max_write = @intCast(max_io);
    fs_request_queue_size = request_queue.size;
    fs_initialized = true;
    fs_init_stage = 11; // initialized
    nodes[0] = .{
        .path_len = 0,
        .nodeid = root_nodeid,
        .mode = file_type_dir | 0o755,
        .size = 0,
        .valid = true,
    };
    return true;
}

/// True after PCI discovery, both queues, and FUSE_INIT succeed.
pub fn available() bool {
    return fs_ready and fs_initialized;
}

/// Maximum data bytes accepted by one FUSE write request. FUSE negotiates
/// this at init; callers use it to keep their existing larger stream chunks
/// within the active transport's bound.
pub fn write_chunk_limit() usize {
    return @intCast(@min(fs_max_write, @as(u32, @intCast(max_io))));
}

/// The queue transport serializes all FUSE requests. It submits a
/// device-readable request and a device-writable reply buffer, then polls
/// the used ring; VZ's FUSE backend does not require guest IRQs.
fn transact(opcode: u32, nodeid: u64, payload: []const u8) ?[]const u8 {
    if (!fs_ready or payload.len > max_payload) return null;
    fs_error = 0;
    const unique = next_unique;
    const request_len = encode_fuse_request(opcode, unique, nodeid, payload, &request_buf) orelse return null;
    next_unique +%= 1;

    request_queue.desc[0] = .{
        .addr = mmu.to_phys(@intFromPtr(&request_buf)),
        .len = @intCast(request_len),
        .flags = flag_next,
        .next = 1,
    };
    request_queue.desc[1] = .{
        .addr = mmu.to_phys(@intFromPtr(&reply_buf)),
        .len = @intCast(reply_buf.len),
        .flags = flag_write,
        .next = 0,
    };
    request_queue.avail.ring[request_queue.avail.idx % request_queue.size] = 0;
    request_queue.avail.idx +%= 1;
    mmu.clean_dcache_range(@intFromPtr(&request_buf), request_len);
    mmu.clean_dcache_range(@intFromPtr(&reply_buf), reply_buf.len);
    mmu.clean_dcache_range(@intFromPtr(&request_queue.desc), @sizeOf([descriptor_count]VirtqDesc));
    mmu.clean_dcache_range(@intFromPtr(&request_queue.avail), @sizeOf(VirtqAvail));
    mmio.mmio_write16(
        fs_notify + @as(u64, request_queue.notify_off) * notify_multiplier,
        1,
    );

    var spins: usize = 0;
    while (spins < poll_budget) : (spins += 1) {
        mmu.invalidate_dcache_range(@intFromPtr(&request_queue.used), @sizeOf(VirtqUsed));
        if (request_queue.used.idx == request_queue.last_used) continue;
        const slot = request_queue.last_used % request_queue.size;
        const used = request_queue.used.ring[slot];
        request_queue.last_used +%= 1;
        if (used.id != 0 or used.len < fuse_out_header_len or used.len > reply_buf.len) return null;
        mmu.invalidate_dcache_range(@intFromPtr(&reply_buf), used.len);
        const decoded = decode_fuse_reply(reply_buf[0..used.len], unique) orelse return null;
        fs_error = decoded.errno;
        if (fs_error != 0) return null;
        return decoded.body;
    }
    return null;
}

fn status_from_error() u8 {
    const errno: u32 = if (fs_error < 0)
        @as(u32, @intCast(-(fs_error + 1))) + 1
    else
        @intCast(fs_error);
    return switch (errno) {
        2 => st_not_found, // ENOENT
        17 => st_exists, // EEXIST
        21 => st_is_dir, // EISDIR
        else => st_host_error,
    };
}

fn cache_find(path: []const u8) ?Node {
    for (nodes) |node| {
        if (node.valid and node.path_len == path.len and std.mem.eql(u8, node.path[0..node.path_len], path)) return node;
    }
    return null;
}

fn cache_put(path: []const u8, nodeid: u64, mode: u32, size: u64) void {
    if (path.len > max_path) return;
    var slot: usize = 1;
    while (slot < nodes.len and nodes[slot].valid) : (slot += 1) {}
    if (slot == nodes.len) slot = 1 + (nodeid % (nodes.len - 1));
    nodes[slot] = .{
        .path = [_]u8{0} ** max_path,
        .path_len = path.len,
        .nodeid = nodeid,
        .mode = mode,
        .size = size,
        .valid = true,
    };
    @memcpy(nodes[slot].path[0..path.len], path);
}

fn cache_clear() void {
    for (nodes[1..]) |*node| node.valid = false;
}

fn lookup(parent: u64, name: []const u8, full_path: []const u8) ?Node {
    if (name.len == 0 or name.len > max_path or std.mem.indexOfScalar(u8, name, 0) != null) return null;
    var payload: [max_path + 1]u8 = [_]u8{0} ** (max_path + 1);
    @memcpy(payload[0..name.len], name);
    const reply = transact(fuse_lookup, parent, payload[0 .. name.len + 1]) orelse return null;
    // fuse_entry_out: nodeid/generation/validity (40 B) + fuse_attr.
    if (reply.len < 128) return null;
    const nodeid = read64(reply, 0);
    const attr = 40;
    const size = read64(reply, attr + 8);
    const mode = read32(reply, attr + 60);
    const node = Node{
        .path = [_]u8{0} ** max_path,
        .path_len = full_path.len,
        .nodeid = nodeid,
        .mode = mode,
        .size = size,
        .valid = true,
    };
    cache_put(full_path, nodeid, mode, size);
    return node;
}

fn next_component(path: []const u8, start: usize) struct { end: usize, next: usize } {
    var end = start;
    while (end < path.len and path[end] != '/') : (end += 1) {}
    var next = end;
    while (next < path.len and path[next] == '/') : (next += 1) {}
    return .{ .end = end, .next = next };
}

fn resolve_path(raw_path: []const u8) ?Node {
    var path = raw_path;
    while (path.len > 0 and path[0] == '/') path = path[1..];
    if (path.len == 0) return nodes[0];
    if (path.len > max_path or std.mem.indexOfScalar(u8, path, 0) != null) return null;

    var full: [max_path]u8 = undefined;
    var full_len: usize = 0;
    var nodeid = root_nodeid;
    var at: usize = 0;
    while (at < path.len) {
        const component = next_component(path, at);
        const name = path[at..component.end];
        if (name.len == 0 or std.mem.eql(u8, name, ".") or std.mem.eql(u8, name, "..")) return null;
        if (full_len > 0) {
            full[full_len] = '/';
            full_len += 1;
        }
        if (full_len + name.len > full.len) return null;
        @memcpy(full[full_len..][0..name.len], name);
        full_len += name.len;
        const full_path = full[0..full_len];
        var node = cache_find(full_path) orelse lookup(nodeid, name, full_path) orelse return null;
        if (component.next < path.len and (node.mode & file_type_mask) != file_type_dir) return null;
        nodeid = node.nodeid;
        if (component.next == path.len) {
            if (node.mode == 0) {
                node = getattr(node.nodeid) orelse return null;
                cache_put(full_path, node.nodeid, node.mode, node.size);
            }
            return node;
        }
        at = component.next;
    }
    return null;
}

fn resolve_parent(raw_path: []const u8) ?struct { parent: u64, name: []const u8 } {
    fs_error = 0;
    var path = raw_path;
    while (path.len > 0 and path[0] == '/') path = path[1..];
    if (path.len == 0 or path.len > max_path or std.mem.indexOfScalar(u8, path, 0) != null) return null;
    var slash: ?usize = null;
    for (path, 0..) |b, i| if (b == '/') {
        slash = i;
    };
    if (slash) |i| {
        if (i == 0 or i + 1 == path.len) return null;
        const parent = resolve_path(path[0..i]) orelse return null;
        if ((parent.mode & file_type_mask) != file_type_dir) return null;
        const name = path[i + 1 ..];
        if (std.mem.indexOfScalar(u8, name, '/') != null or name.len == 0) return null;
        return .{ .parent = parent.nodeid, .name = name };
    }
    return .{ .parent = root_nodeid, .name = path };
}

fn getattr(nodeid: u64) ?Node {
    var input: [16]u8 = [_]u8{0} ** 16;
    const reply = transact(fuse_getattr, nodeid, &input) orelse return null;
    if (reply.len < 104) return null;
    const attr = 16;
    return .{
        .nodeid = nodeid,
        .size = read64(reply, attr + 8),
        .mode = read32(reply, attr + 60),
        .valid = true,
    };
}

fn node_stat(raw_path: []const u8) ?Node {
    const node = resolve_path(raw_path) orelse return null;
    return getattr(node.nodeid);
}

fn find_handle(id: u16) ?*FileHandle {
    if (id == 0 or id > handles.len) return null;
    const handle = &handles[id - 1];
    if (!handle.valid) return null;
    return handle;
}

fn ensure_created(path: []const u8) u8 {
    if (resolve_path(path) != null) return st_ok;
    const parent = resolve_parent(path) orelse return status_from_error();
    var input: [16 + max_path]u8 = [_]u8{0} ** (16 + max_path);
    write32(&input, 0, file_type_regular | 0o644);
    write32(&input, 4, 0);
    write32(&input, 8, 0);
    write32(&input, 12, 0);
    @memcpy(input[16..][0..parent.name.len], parent.name);
    input[16 + parent.name.len] = 0;
    _ = transact(fuse_mknod, parent.parent, input[0 .. 17 + parent.name.len]) orelse return status_from_error();
    cache_clear();
    return st_ok;
}

pub fn stat(raw_path: []const u8, out: *StatResult) u8 {
    out.* = .{};
    if (!available()) return st_host_error;
    const saved = fs_lock.lock();
    defer fs_lock.unlock(saved);
    const node = node_stat(raw_path) orelse return status_from_error();
    out.size = node.size;
    out.is_dir = (node.mode & file_type_mask) == file_type_dir;
    return st_ok;
}

pub fn open(raw_path: []const u8, flags: u8, out_handle: *u16) u8 {
    out_handle.* = 0;
    if (!available()) return st_host_error;
    const saved = fs_lock.lock();
    defer fs_lock.unlock(saved);

    if ((flags & 1) != 0) {
        const status = ensure_created(raw_path);
        if (status != st_ok) return status;
    }
    const node = resolve_path(raw_path) orelse return status_from_error();
    if ((node.mode & file_type_mask) == file_type_dir) return st_is_dir;
    var input: [8]u8 = [_]u8{0} ** 8;
    write32(&input, 0, open_read_write);
    if ((flags & 2) != 0) write32(&input, 0, open_read_write | open_append);
    const reply = transact(fuse_open, node.nodeid, &input) orelse return status_from_error();
    if (reply.len < 8) return st_host_error;
    for (&handles, 0..) |*handle, i| {
        if (handle.valid) continue;
        handle.* = .{
            .nodeid = node.nodeid,
            .fuse_handle = read64(reply, 0),
            .cursor = if ((flags & 2) != 0) node.size else 0,
            .valid = true,
        };
        out_handle.* = @intCast(i + 1);
        return st_ok;
    }
    return st_handle;
}

pub fn close(id: u16) u8 {
    if (!available()) return st_host_error;
    const saved = fs_lock.lock();
    defer fs_lock.unlock(saved);
    const handle = find_handle(id) orelse return st_handle;
    var input: [24]u8 = [_]u8{0} ** 24;
    write64(&input, 0, handle.fuse_handle);
    _ = transact(fuse_release, handle.nodeid, &input) orelse {
        handle.valid = false;
        return status_from_error();
    };
    handle.valid = false;
    return st_ok;
}

pub fn fsync(id: u16) u8 {
    if (!available()) return st_host_error;
    const saved = fs_lock.lock();
    defer fs_lock.unlock(saved);
    const handle = find_handle(id) orelse return st_handle;
    var input: [16]u8 = [_]u8{0} ** 16;
    write64(&input, 0, handle.fuse_handle);
    _ = transact(fuse_fsync, handle.nodeid, &input) orelse return status_from_error();
    return st_ok;
}

pub fn write(id: u16, data: []const u8, written: *u64) u8 {
    written.* = 0;
    if (!available() or data.len > fs_max_write or data.len > max_io) return st_host_error;
    const saved = fs_lock.lock();
    defer fs_lock.unlock(saved);
    const handle = find_handle(id) orelse return st_handle;
    var input: [40 + max_io]u8 = [_]u8{0} ** (40 + max_io);
    write64(&input, 0, handle.fuse_handle);
    write64(&input, 8, handle.cursor);
    write32(&input, 16, @intCast(data.len));
    @memcpy(input[40..][0..data.len], data);
    const reply = transact(fuse_write, handle.nodeid, input[0 .. 40 + data.len]) orelse return status_from_error();
    if (reply.len < 4) return st_host_error;
    written.* = read32(reply, 0);
    if (written.* > data.len) return st_host_error;
    handle.cursor += written.*;
    return st_ok;
}

pub fn truncate(id: u16, size: u64) u8 {
    if (!available()) return st_host_error;
    const saved = fs_lock.lock();
    defer fs_lock.unlock(saved);
    const handle = find_handle(id) orelse return st_handle;
    var input: [88]u8 = [_]u8{0} ** 88;
    write32(&input, 0, fattr_size | fattr_fh);
    write64(&input, 8, handle.fuse_handle);
    write64(&input, 16, size);
    _ = transact(fuse_setattr, handle.nodeid, &input) orelse return status_from_error();
    cache_clear();
    return st_ok;
}

pub fn read_chunk(raw_path: []const u8, offset: u64, out: []u8) ReadChunkResult {
    if (!available()) return .{ .status = st_host_error };
    if (out.len == 0) return .{ .status = st_ok, .bytes = 0 };
    const saved = fs_lock.lock();
    defer fs_lock.unlock(saved);
    const node = resolve_path(raw_path) orelse return .{ .status = status_from_error() };
    if ((node.mode & file_type_mask) == file_type_dir) return .{ .status = st_is_dir };
    var open_in: [8]u8 = [_]u8{0} ** 8;
    const opened = transact(fuse_open, node.nodeid, &open_in) orelse return .{ .status = status_from_error() };
    if (opened.len < 8) return .{ .status = st_host_error };
    const fh = read64(opened, 0);
    var input: [40]u8 = [_]u8{0} ** 40;
    write64(&input, 0, fh);
    write64(&input, 8, offset);
    write32(&input, 16, @intCast(@min(@min(out.len, fs_max_write), max_io)));
    const reply = transact(fuse_read, node.nodeid, &input) orelse {
        const status = status_from_error();
        var release: [24]u8 = [_]u8{0} ** 24;
        write64(&release, 0, fh);
        _ = transact(fuse_release, node.nodeid, &release);
        return .{ .status = status };
    };
    const take = @min(reply.len, out.len);
    @memcpy(out[0..take], reply[0..take]);
    var release: [24]u8 = [_]u8{0} ** 24;
    write64(&release, 0, fh);
    _ = transact(fuse_release, node.nodeid, &release);
    return .{ .status = st_ok, .bytes = take };
}

pub fn read(raw_path: []const u8, offset: u64) ReadChunkResult {
    read_result_len = 0;
    const take = @min(read_result_buf.len, fs_max_write);
    const result = read_chunk(raw_path, offset, read_result_buf[0..take]);
    read_result_len = result.bytes;
    return result;
}

pub fn read_result() []const u8 {
    return read_result_buf[0..read_result_len];
}

pub fn list(raw_path: []const u8, out: []DirectoryEntry, out_count: *usize) u8 {
    out_count.* = 0;
    if (!available()) return st_host_error;
    const saved = fs_lock.lock();
    defer fs_lock.unlock(saved);
    var path = raw_path;
    while (path.len > 0 and path[0] == '/') path = path[1..];
    const node = resolve_path(path) orelse return status_from_error();
    if ((node.mode & file_type_mask) != file_type_dir) return st_is_dir;

    var open_in: [8]u8 = [_]u8{0} ** 8;
    const opened = transact(fuse_opendir, node.nodeid, &open_in) orelse return status_from_error();
    if (opened.len < 8) return st_host_error;
    const fh = read64(opened, 0);
    var offset: u64 = 0;
    while (out_count.* < out.len) {
        var input: [40]u8 = [_]u8{0} ** 40;
        write64(&input, 0, fh);
        write64(&input, 8, offset);
        write32(&input, 16, @intCast(max_io));
        const reply = transact(fuse_readdir, node.nodeid, &input) orelse {
            const status = status_from_error();
            var release: [24]u8 = [_]u8{0} ** 24;
            write64(&release, 0, fh);
            _ = transact(fuse_releasedir, node.nodeid, &release);
            return status;
        };
        if (reply.len == 0) break;
        var page: [max_io]u8 = [_]u8{0} ** max_io;
        @memcpy(page[0..reply.len], reply);
        var at: usize = 0;
        var page_next: u64 = offset;
        while (at + 24 <= reply.len and out_count.* < out.len) {
            const next_offset = read64(&page, at + 8);
            const name_len = read32(&page, at + 16);
            const kind = read32(&page, at + 20);
            const entry_len = (@as(usize, 24) + name_len + 7) & ~@as(usize, 7);
            if (name_len == 0 or entry_len > reply.len - at or name_len > max_path) break;
            const name = page[at + 24 ..][0..name_len];
            if (!std.mem.eql(u8, name, ".") and !std.mem.eql(u8, name, "..")) {
                var full: [max_path]u8 = undefined;
                var full_len: usize = 0;
                if (path.len > 0) {
                    if (path.len + 1 + name.len > full.len) break;
                    @memcpy(full[0..path.len], path);
                    full[path.len] = '/';
                    full_len = path.len + 1;
                }
                if (full_len + name.len > full.len) break;
                @memcpy(full[full_len..][0..name.len], name);
                full_len += name.len;
                const child = lookup(node.nodeid, name, full[0..full_len]);
                var entry = DirectoryEntry{
                    .name_len = @min(name.len, 31),
                    .type = if (kind == dirent_type_dir) 1 else 0,
                };
                @memcpy(entry.name[0..entry.name_len], name[0..entry.name_len]);
                if (child) |child_node| {
                    entry.type = if ((child_node.mode & file_type_mask) == file_type_dir) 1 else 0;
                    entry.size = child_node.size;
                }
                out[out_count.*] = entry;
                out_count.* += 1;
            }
            page_next = next_offset;
            at += entry_len;
        }
        if (at == 0 or page_next == offset) break;
        offset = page_next;
    }
    var release: [24]u8 = [_]u8{0} ** 24;
    write64(&release, 0, fh);
    _ = transact(fuse_releasedir, node.nodeid, &release);
    return st_ok;
}

pub fn mkdir(raw_path: []const u8) u8 {
    if (!available()) return st_host_error;
    const saved = fs_lock.lock();
    defer fs_lock.unlock(saved);
    const parent = resolve_parent(raw_path) orelse return status_from_error();
    var input: [8 + max_path]u8 = [_]u8{0} ** (8 + max_path);
    write32(&input, 0, file_type_dir | 0o755);
    write32(&input, 4, 0);
    @memcpy(input[8..][0..parent.name.len], parent.name);
    input[8 + parent.name.len] = 0;
    _ = transact(fuse_mkdir, parent.parent, input[0 .. 9 + parent.name.len]) orelse return status_from_error();
    cache_clear();
    return st_ok;
}

pub fn delete(raw_path: []const u8) u8 {
    if (!available()) return st_host_error;
    const saved = fs_lock.lock();
    defer fs_lock.unlock(saved);
    const parent = resolve_parent(raw_path) orelse return status_from_error();
    const existing = resolve_path(raw_path) orelse return status_from_error();
    var name: [max_path + 1]u8 = [_]u8{0} ** (max_path + 1);
    @memcpy(name[0..parent.name.len], parent.name);
    const opcode = if ((existing.mode & file_type_mask) == file_type_dir) fuse_rmdir else fuse_unlink;
    _ = transact(opcode, parent.parent, name[0 .. parent.name.len + 1]) orelse return status_from_error();
    cache_clear();
    return st_ok;
}

pub fn rename(raw_from: []const u8, raw_to: []const u8) u8 {
    if (!available()) return st_host_error;
    const saved = fs_lock.lock();
    defer fs_lock.unlock(saved);
    const from = resolve_parent(raw_from) orelse return status_from_error();
    const to = resolve_parent(raw_to) orelse return status_from_error();
    var input: [16 + max_path * 2 + 2]u8 = [_]u8{0} ** (16 + max_path * 2 + 2);
    write64(&input, 0, to.parent);
    var len: usize = 8;
    @memcpy(input[len..][0..from.name.len], from.name);
    len += from.name.len;
    input[len] = 0;
    len += 1;
    @memcpy(input[len..][0..to.name.len], to.name);
    len += to.name.len;
    input[len] = 0;
    len += 1;
    _ = transact(fuse_rename, from.parent, input[0..len]) orelse return status_from_error();
    cache_clear();
    return st_ok;
}

test "virtio_fs: virtio-fs identity and bounded wire helpers" {
    try std.testing.expectEqual(@as(u32, 26), virtio_fs_device_id);
    try std.testing.expectEqual(@as(u32, 0x105a), virtio_fs_did);
    try std.testing.expectEqual(@as(u16, 8), highest_power_of_two_at_most(15));
    try std.testing.expectEqual(@as(u16, 16), highest_power_of_two_at_most(128));
    var bytes = [_]u8{0} ** 8;
    write64(&bytes, 0, 0x0123456789abcdef);
    try std.testing.expectEqual(@as(u64, 0x0123456789abcdef), read64(&bytes, 0));
}

test "virtio_fs: FUSE request and reply framing" {
    var request: [fuse_in_header_len + 4]u8 = undefined;
    const payload = [_]u8{ 0x41, 0x42, 0x43, 0 };
    const request_len = encode_fuse_request(fuse_lookup, 0x1122334455667788, 7, &payload, &request) orelse return error.EncodeFailed;
    try std.testing.expectEqual(@as(usize, 44), request_len);
    try std.testing.expectEqual(@as(u32, 44), read32(&request, 0));
    try std.testing.expectEqual(fuse_lookup, read32(&request, 4));
    try std.testing.expectEqual(@as(u64, 0x1122334455667788), read64(&request, 8));
    try std.testing.expectEqual(@as(u64, 7), read64(&request, 16));
    try std.testing.expectEqualSlices(u8, &payload, request[fuse_in_header_len..]);
    try std.testing.expect(encode_fuse_request(fuse_lookup, 1, 1, &payload, request[0..43]) == null);

    var reply: [fuse_out_header_len + 3]u8 = [_]u8{0} ** (fuse_out_header_len + 3);
    write32(&reply, 0, @intCast(reply.len));
    write64(&reply, 8, 0x1122334455667788);
    @memcpy(reply[fuse_out_header_len..], "ok\n");
    const decoded = decode_fuse_reply(&reply, 0x1122334455667788) orelse return error.DecodeFailed;
    try std.testing.expectEqual(@as(i32, 0), decoded.errno);
    try std.testing.expectEqualSlices(u8, "ok\n", decoded.body);
    try std.testing.expect(decode_fuse_reply(&reply, 9) == null);
    try std.testing.expect(decode_fuse_reply(reply[0..8], 1) == null);
    write32(&reply, 4, @bitCast(@as(i32, -2)));
    const failed = decode_fuse_reply(&reply, 0x1122334455667788) orelse return error.DecodeFailed;
    try std.testing.expectEqual(@as(i32, -2), failed.errno);
    try std.testing.expectEqual(@as(usize, 0), failed.body.len);
}

test "virtio_fs: paths refuse traversal and keep host-relative names" {
    fs_initialized = true;
    fs_ready = true;
    nodes[0] = .{ .path_len = 0, .nodeid = root_nodeid, .mode = file_type_dir, .valid = true };
    defer {
        fs_initialized = false;
        fs_ready = false;
        nodes = [_]Node{.{}} ** node_cache_count;
    }
    nodes[1] = .{
        .path = [_]u8{0} ** max_path,
        .path_len = "SELFTEST".len,
        .nodeid = 2,
        .mode = file_type_dir | 0o755,
        .valid = true,
    };
    @memcpy(nodes[1].path[0.."SELFTEST".len], "SELFTEST");
    try std.testing.expect(resolve_parent("/SETTINGS.TXT") != null);
    try std.testing.expect(resolve_parent("SELFTEST/LAYOUT.txt") != null);
    try std.testing.expect(resolve_parent("../outside") == null);
    try std.testing.expect(resolve_parent("SELFTEST/../outside") == null);
}

comptime {
    if (builtin.cpu.arch == .aarch64) {
        _ = @sizeOf(VirtqDesc);
    }
}
