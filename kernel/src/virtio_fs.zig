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
const directory = @import("directory.zig");
const alloc = @import("alloc.zig");
pub const metadata = @import("fs_metadata.zig");

pub const st_ok: u8 = 0;
pub const st_not_found: u8 = 1;
pub const st_is_dir: u8 = 2;
pub const st_truncated: u8 = 3;
pub const st_host_error: u8 = 4;
pub const st_exists: u8 = 5;
pub const st_handle: u8 = 6;
pub const st_limit: u8 = 7;
pub const st_path_limit: u8 = 8;
pub const st_changed: u8 = 9;
pub const st_access: u8 = 10;
pub const st_unsupported: u8 = 11;

pub const RenameMode = enum { preserve_existing, replace };

pub const virtio_fs_did: u32 = 0x105a;
pub const virtio_fs_device_id: u32 = 26;
pub const tag = "virelaios";
pub const max_path: usize = directory.path_max;
pub const max_io: usize = 2048;

const descriptor_count: usize = 16;
const max_payload: usize = 40 + max_io;
const max_message: usize = std.mem.alignForward(usize, 40 + max_payload, 64);
const max_queue: u16 = @intCast(descriptor_count);
const root_nodeid: u64 = 1;
const fuse_in_header_len: usize = 40;
const fuse_out_header_len: usize = 16;
const poll_budget: usize = 16_000_000;
// Legacy caching is optional; one short-path entry plus root keeps the
// encoded kernel below the unchanged 16 MiB loader ceiling. Longer paths
// remain lossless, uncached. B3 never consumes this pathname cache.
const node_cache_count: usize = 2;
const cache_path_max: usize = 31;
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
    // Cache maintenance must not discard dirty CPU-owned queue state.
    desc: [descriptor_count]VirtqDesc align(64) = undefined,
    avail: VirtqAvail align(64) = undefined,
    used: VirtqUsed align(64) = undefined,
    last_used: u16 align(64) = 0,
    size: u16 = 0,
    notify_off: u16 = 0,
};

const Node = struct {
    path: [cache_path_max]u8 = [_]u8{0} ** cache_path_max,
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
    contained: bool = false,
    access: metadata.Access = .read,
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
// Mount lifetime discriminator, not a pathname hash or synthesized inode.
var mount_identity: u64 = 0;
pub const identity_pin_max: usize = 512;
const IdentityPin = extern struct { node: u64, pending: u64 };
var identity_nodes: ?*[identity_pin_max]IdentityPin = null;
pub var identity_pin_count: usize = 0;
var test_identity_nodes: [identity_pin_max]IdentityPin = undefined;
pub var test_exchange: ?*const fn (u32, u64, []const u8) ?[]const u8 = null;
var nodes: [node_cache_count]Node = [_]Node{.{}} ** node_cache_count;
var handles: [file_handle_count]FileHandle = [_]FileHandle{.{}} ** file_handle_count;
var fs_lock = spinlock.IrqSaveSpinlock{};
var request_buf: [max_message]u8 align(64) = undefined;
var reply_buf: [max_message]u8 align(64) = undefined;
var read_result_buf: [max_io]u8 align(16) = undefined;
var read_result_len: usize = 0;

const flag_next: u16 = 1;
const flag_write: u16 = 2;
const fuse_init: u32 = 26;
const fuse_lookup: u32 = 1;
const fuse_forget: u32 = 2;
const fuse_getattr: u32 = 3;
const fuse_setattr: u32 = 4;
const fuse_mknod: u32 = 8;
const fuse_mkdir: u32 = 9;
const fuse_unlink: u32 = 10;
const fuse_rmdir: u32 = 11;
const fuse_rename: u32 = 12;
const fuse_rename2: u32 = 45;
const rename_noreplace: u32 = 1;
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
    reset_identity_nodes(); // Device reset ends the previous pin lifetime.
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
    mount_identity += 1;
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
    if (builtin.is_test) {
        if (test_exchange) |exchange| {
            fs_error = 0;
            return exchange(opcode, nodeid, payload);
        }
    }
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
        if (used.id != 0 or used.len > reply_buf.len) return null;
        // FUSE_FORGET has no reply. The used descriptor still acknowledges
        // consumption, so the request storage can safely be reused.
        if (opcode == fuse_forget) return "";
        if (used.len < fuse_out_header_len) return null;
        mmu.invalidate_dcache_range(@intFromPtr(&reply_buf), used.len);
        const decoded = decode_fuse_reply(reply_buf[0..used.len], unique) orelse return null;
        fs_error = decoded.errno;
        if (fs_error != 0) return null;
        return decoded.body;
    }
    fs_ready = false; // Do not reuse buffers while a timed-out DMA is armed.
    return null;
}

pub const Root = struct { path: []const u8 };

pub fn contained_backend(root: *Root) metadata.Backend {
    // The relocatable kernel must materialize callbacks at its live base.
    // Individual volatile stores prevent a compiler-generated memcpy from
    // an absolute-address rodata table (see B3's loader finding).
    const ops: *volatile metadata.ContainedOps = &contained_ops;
    ops.root = pin_root;
    ops.lookup = pin_lookup;
    ops.release = unpin;
    ops.stat = pin_stat;
    ops.open = pin_open;
    ops.close = pin_close;
    ops.read = pin_read;
    ops.write = pin_write;
    return .{ .context = root, .root_path = root.path, .ops = if (available()) &contained_ops else null };
}

fn metadata_failure() metadata.Error {
    return if (fs_error == 0) error.Io else metadata.fuseError(fs_error);
}

fn attr_locked(node: u64) metadata.Error!metadata.Metadata {
    const input = [_]u8{0} ** 16;
    const reply = transact(fuse_getattr, node, &input) orelse return metadata_failure();
    return metadata.decodeFuseGetattr(mount_identity, reply);
}

fn lookup_locked(parent: u64, name: []const u8) metadata.Error!metadata.Object {
    if (!directory.valid_name(name)) return error.InvalidPath;
    var input: [directory.name_max + 1]u8 = .{0} ** (directory.name_max + 1);
    @memcpy(input[0..name.len], name);
    const reply = transact(fuse_lookup, parent, input[0 .. name.len + 1]) orelse return metadata_failure();
    // Even malformed attributes can carry a valid lookup reference.
    const decoded = metadata.decodeFuseLookup(mount_identity, reply) catch |err| {
        if (reply.len >= 8 and read64(reply, 0) != 0) forget_locked(read64(reply, 0));
        return err;
    };
    // VZ may recycle attr.ino after the last LOOKUP reference is dropped.
    // Keep one actual reference per object for the bounded mount lifetime,
    // never a pathname hash or invented inode. Extra lookups still release.
    if (decoded.metadata.kind != .symlink) {
        retain_identity_locked(decoded.node) catch |err| {
            forget_locked(decoded.node);
            return err;
        };
    }
    return .{ .token = decoded.node, .metadata = decoded.metadata };
}

fn retain_identity_locked(node: u64) metadata.Error!void {
    if (node == root_nodeid) return;
    if (identity_nodes) |entries| {
        for (entries[0..identity_pin_count]) |pinned| if (pinned.node == node) return;
    }
    if (identity_pin_count == identity_pin_max) return error.TreeLimit;
    if (identity_nodes == null) {
        identity_nodes = if (builtin.is_test) &test_identity_nodes else @ptrFromInt(alloc.alloc_pages(2) orelse return error.HandleLimit);
    }
    // Retain the existing reference; unpin skips exactly its first release.
    identity_nodes.?[identity_pin_count] = .{ .node = node, .pending = 1 };
    identity_pin_count += 1;
}

fn release_lookup_locked(node: u64) void {
    if (identity_nodes) |entries| {
        for (entries[0..identity_pin_count]) |*pinned| {
            if (pinned.node == node and pinned.pending != 0) {
                pinned.pending = 0;
                return;
            }
        }
    }
    forget_locked(node);
}

fn reset_identity_nodes() void {
    if (!builtin.is_test) {
        if (identity_nodes) |entries| _ = alloc.free_pages(@intFromPtr(entries), 2);
    }
    identity_nodes = null;
    identity_pin_count = 0;
}

fn forget_locked(node: u64) void {
    if (node == root_nodeid) return;
    var input: [8]u8 = undefined;
    write64(&input, 0, 1);
    // A failed release poisons this mount instead of claiming recovery.
    if (transact(fuse_forget, node, &input) == null) fs_ready = false;
}

fn pin_root(context: *anyopaque) metadata.Error!metadata.Object {
    const root: *Root = @ptrCast(@alignCast(context));
    try metadata.validatePath(root.path);
    if (!available()) return error.ContainmentUnavailable;
    const saved = fs_lock.lock();
    defer fs_lock.unlock(saved);
    var current = metadata.Object{ .token = root_nodeid, .metadata = try attr_locked(root_nodeid) };
    errdefer release_lookup_locked(current.token);
    var parts = std.mem.splitScalar(u8, root.path, '/');
    while (parts.next()) |name| {
        if (current.metadata.kind == .symlink) return error.SymlinkRejected;
        if (current.metadata.kind != .directory) return error.NotDirectory;
        if (name.len == 0) break;
        const child = try lookup_locked(current.token, name);
        release_lookup_locked(current.token);
        current = child;
    }
    return current;
}

fn pin_lookup(_: *anyopaque, parent: metadata.Object, name: []const u8) metadata.Error!metadata.Object {
    if (!available()) return error.ContainmentUnavailable;
    const saved = fs_lock.lock();
    defer fs_lock.unlock(saved);
    return lookup_locked(parent.token, name);
}

fn unpin(_: *anyopaque, object: metadata.Object) void {
    const saved = fs_lock.lock();
    defer fs_lock.unlock(saved);
    release_lookup_locked(object.token);
}

fn pin_stat(_: *anyopaque, object: metadata.Object) metadata.Error!metadata.Metadata {
    if (!available()) return error.ContainmentUnavailable;
    const saved = fs_lock.lock();
    defer fs_lock.unlock(saved);
    return attr_locked(object.token);
}

fn pin_open(_: *anyopaque, object: metadata.Object, access: metadata.Access) metadata.Error!u64 {
    if (!available()) return error.ContainmentUnavailable;
    const saved = fs_lock.lock();
    defer fs_lock.unlock(saved);
    var slot: usize = 0;
    while (slot < handles.len and handles[slot].valid) : (slot += 1) {}
    if (slot == handles.len) return error.HandleLimit;
    var input: [8]u8 = .{0} ** 8;
    // FUSE opens this lookup-pinned inode, not its former name. Never
    // create/truncate, never O_RDWR for a read-only request.
    write32(&input, 0, if (access == .read) open_read_only else open_write_only);
    const reply = transact(fuse_open, object.token, &input) orelse return metadata_failure();
    if (reply.len < 16) return error.InvalidMetadata;
    handles[slot] = .{
        .nodeid = object.token,
        .fuse_handle = read64(reply, 0),
        .valid = true,
        .contained = true,
        .access = access,
    };
    return slot + 1;
}

pub fn contained_close(id: u16) metadata.Error!void {
    if (!available()) return error.ContainmentUnavailable;
    const saved = fs_lock.lock();
    defer fs_lock.unlock(saved);
    const handle = find_handle(id) orelse return error.StaleIdentity;
    var input: [24]u8 = .{0} ** 24;
    write64(&input, 0, handle.fuse_handle);
    _ = transact(fuse_release, handle.nodeid, &input) orelse return metadata_failure();
    handle.* = .{};
}

fn pin_close(_: *anyopaque, id: u64) metadata.Error!void {
    if (id > handles.len) return error.StaleIdentity;
    return contained_close(@intCast(id));
}

pub fn contained_read(id: u16, offset: u64, out: []u8) metadata.Error!usize {
    if (!available()) return error.ContainmentUnavailable;
    const saved = fs_lock.lock();
    defer fs_lock.unlock(saved);
    const handle = find_handle(id) orelse return error.StaleIdentity;
    if (!handle.contained or handle.access != .read) return error.AccessDenied;
    const take = @min(out.len, max_io);
    if (take == 0) return 0;
    var input: [40]u8 = .{0} ** 40;
    write64(&input, 0, handle.fuse_handle);
    write64(&input, 8, offset);
    write32(&input, 16, @intCast(take));
    const reply = transact(fuse_read, handle.nodeid, &input) orelse return metadata_failure();
    if (reply.len > take) return error.InvalidMetadata;
    @memcpy(out[0..reply.len], reply);
    return reply.len;
}

fn pin_read(_: *anyopaque, id: u64, out: []u8) metadata.Error!usize {
    if (id > handles.len) return error.StaleIdentity;
    const h = find_handle(@intCast(id)) orelse return error.StaleIdentity;
    const n = try contained_read(@intCast(id), h.cursor, out);
    h.cursor += n;
    return n;
}

fn pin_write(_: *anyopaque, id: u64, bytes: []const u8) metadata.Error!usize {
    if (id > handles.len) return error.StaleIdentity;
    var written: u64 = 0;
    const st = write(@intCast(id), bytes, &written);
    if (st != st_ok) return metadata_failure();
    if (written > bytes.len) return error.InvalidMetadata;
    return @intCast(written);
}

var contained_ops: metadata.ContainedOps = undefined;

/// Wire-level fixture for backend + EL0 host tests, never a live backend.
pub const TestMetadataServer = struct {
    pub var lookup_count: usize = 0;
    pub var pins: usize = 0;
    pub var opens: usize = 0;
    pub var swap_directory: bool = false;
    pub var swapped: bool = false;
    pub var swap_leaf: bool = false;
    var leaf_swapped: bool = false;
    pub var include_link: bool = false;
    pub var symlink_node: u64 = 6;
    pub var fail_opcode: u32 = 0;
    pub var fail_errno: i32 = -5;
    pub var short_attr: bool = false;
    pub var zero_inode: bool = false;
    pub var mtime: u64 = 1_790_897_123;
    pub var size: u64 = 4;
    var bytes: [4]u8 = undefined;
    var reply: [512]u8 = undefined;

    pub fn start() void {
        std.debug.assert(builtin.is_test);
        reset_identity_nodes();
        lookup_count = 0;
        pins = 0;
        opens = 0;
        swap_directory = false;
        swapped = false;
        swap_leaf = false;
        leaf_swapped = false;
        include_link = false;
        symlink_node = 6;
        fail_opcode = 0;
        short_attr = false;
        zero_inode = false;
        size = 4;
        mtime = 1_790_897_123;
        bytes = .{ 's', 'a', 'f', 'e' };
        handles = [_]FileHandle{.{}} ** file_handle_count;
        mount_identity = 7;
        fs_ready = true;
        fs_initialized = true;
        test_exchange = exchange;
    }

    pub fn stop() void {
        fail_opcode = 0;
        if (identity_nodes) |entries| {
            for (entries[0..identity_pin_count]) |pinned| forget_locked(pinned.node);
        }
        std.debug.assert(pins == 0);
        reset_identity_nodes();
        test_exchange = null;
        fs_ready = false;
        fs_initialized = false;
        handles = [_]FileHandle{.{}} ** file_handle_count;
    }

    pub fn transient_pins() usize {
        return pins - identity_pin_count;
    }

    fn attr(node: u64, at: usize) void {
        write64(&reply, at, if (zero_inode) 0 else 100 + node);
        write64(&reply, at + 8, if (node == 5) size else 4096);
        write64(&reply, at + 24, 1_790_897_100);
        write64(&reply, at + 32, mtime);
        write64(&reply, at + 40, 1_790_897_124);
        write32(&reply, at + 52, 123);
        write32(&reply, at + 60, if (node == symlink_node) 0o120777 else if (node == 5) 0o100644 else 0o040755);
        write32(&reply, at + 68, 501);
        write32(&reply, at + 72, 20);
    }

    fn row(at: usize, cookie: u64, name: []const u8) usize {
        write64(&reply, at + 8, cookie);
        write32(&reply, at + 16, @intCast(name.len));
        @memcpy(reply[at + 24 ..][0..name.len], name);
        return (24 + name.len + 7) & ~@as(usize, 7);
    }

    fn exchange(opcode: u32, node: u64, input: []const u8) ?[]const u8 {
        @memset(&reply, 0);
        if (opcode == fail_opcode) {
            fs_error = fail_errno;
            return null;
        }
        switch (opcode) {
            fuse_lookup => {
                lookup_count += 1;
                const name = input[0 .. input.len - 1];
                const child: u64 = if (node == 1 and std.mem.eql(u8, name, "content")) 2 else if (node == 2 and std.mem.eql(u8, name, "a")) (if (swapped) 6 else 3) else if (node == 3 and std.mem.eql(u8, name, "b")) 4 else if (node == 4 and std.mem.eql(u8, name, "page.md")) (if (leaf_swapped) 6 else 5) else if (std.mem.eql(u8, name, "link")) 6 else {
                    fs_error = -2;
                    return null;
                };
                write64(&reply, 0, child);
                write64(&reply, 8, 1);
                attr(child, 40);
                pins += 1;
                if (swap_directory and child == 3) swapped = true;
                return reply[0..128];
            },
            fuse_getattr => {
                attr(node, 16);
                return reply[0..if (short_attr) @as(usize, 103) else 104];
            },
            fuse_forget => {
                std.debug.assert(pins > 0);
                pins -= 1;
                return "";
            },
            fuse_open => {
                opens += 1;
                if (swap_leaf) leaf_swapped = true;
                write64(&reply, 0, node + 1000);
                return reply[0..16];
            },
            fuse_read => {
                if (node != 5) return null;
                const offset = read64(input, 8);
                if (offset >= bytes.len) return "";
                const n: usize = @intCast(@min(read32(input, 16), bytes.len - offset));
                @memcpy(reply[0..n], bytes[@intCast(offset)..][0..n]);
                return reply[0..n];
            },
            fuse_write => {
                if (node != 5) return null;
                const n = @min(bytes.len, read32(input, 16));
                @memcpy(bytes[0..n], input[40..][0..n]);
                size = n;
                mtime += 1;
                write32(&reply, 0, @intCast(n));
                return reply[0..8];
            },
            fuse_opendir => {
                write64(&reply, 0, node + 1000);
                return reply[0..16];
            },
            fuse_readdir => {
                if (read64(input, 8) != 0) return "";
                const name: []const u8 = switch (node) {
                    1 => "content",
                    2 => "a",
                    3 => "b",
                    4 => "page.md",
                    else => return "",
                };
                var len = row(0, 10, name);
                if (include_link) len += row(len, 20, "link");
                return reply[0..len];
            },
            fuse_release, fuse_releasedir, fuse_fsync => return "",
            else => return null,
        }
    }
};

test "B3 backend: uncached pinned FUSE traversal survives directory replacement" {
    const S = TestMetadataServer;
    S.start();
    defer S.stop();
    const Allow = struct {
        fn check(_: *anyopaque, _: []const u8, _: metadata.Access) metadata.Error!void {}
    };
    var root = Root{ .path = "content" };
    const auth = metadata.Authorizer{ .context = &root, .check = Allow.check };
    S.swap_directory = true;
    var file = try metadata.open(contained_backend(&root), auth, "a/b/page.md", .read);
    var bytes: [4]u8 = undefined;
    try std.testing.expectEqual(@as(usize, 4), try file.read(&bytes));
    try std.testing.expectEqualStrings("safe", &bytes);
    try file.close();
    try std.testing.expectEqual(@as(usize, 0), S.transient_pins());
    try std.testing.expectError(error.SymlinkRejected, metadata.stat(contained_backend(&root), auth, "a/b/page.md"));
    try std.testing.expectEqual(@as(usize, 0), S.transient_pins());
}

test "B3 backend: root intermediate and leaf links refuse with balanced pins" {
    const S = TestMetadataServer;
    const Allow = struct {
        fn check(_: *anyopaque, _: []const u8, _: metadata.Access) metadata.Error!void {}
    };
    for ([_]u64{ 2, 3, 4, 5 }) |node| {
        S.start();
        defer S.stop();
        S.symlink_node = node;
        var root = Root{ .path = "content" };
        const auth = metadata.Authorizer{ .context = &root, .check = Allow.check };
        try std.testing.expectError(error.SymlinkRejected, metadata.stat(contained_backend(&root), auth, "a/b/page.md"));
        try std.testing.expectError(error.SymlinkRejected, metadata.open(contained_backend(&root), auth, "a/b/page.md", .read));
        try std.testing.expectEqual(@as(usize, 0), S.transient_pins());
        try std.testing.expectEqual(@as(usize, 0), S.opens);
    }
}

test "B3 backend: identity pins are bounded, reused and released at mount end" {
    const S = TestMetadataServer;
    S.start();
    defer S.stop();
    const Allow = struct {
        fn check(_: *anyopaque, _: []const u8, _: metadata.Access) metadata.Error!void {}
    };
    var root = Root{ .path = "content" };
    const auth = metadata.Authorizer{ .context = &root, .check = Allow.check };
    const first = try metadata.stat(contained_backend(&root), auth, "a/b/page.md");
    try std.testing.expectEqual(@as(usize, 4), identity_pin_count);
    const again = try metadata.stat(contained_backend(&root), auth, "a/b/page.md");
    try std.testing.expect(first.identity.eql(again.identity));
    try std.testing.expectEqual(@as(usize, 4), S.pins);
    try std.testing.expectEqual(@as(usize, 0), S.transient_pins());
    for (test_identity_nodes[identity_pin_count..], 0..) |*pin, i| {
        pin.* = .{ .node = 100 + i, .pending = 0 };
        S.pins += 1;
    }
    identity_pin_count = identity_pin_max;
    S.symlink_node = 0;
    try std.testing.expectError(error.TreeLimit, metadata.stat(contained_backend(&root), auth, "link"));
    try std.testing.expectEqual(identity_pin_max, S.pins);
}

test "B3 backend: DMA invalidation ranges are cache-line isolated" {
    try std.testing.expectEqual(@as(usize, 0), @offsetOf(Queue, "used") % 64);
    const used_end = @offsetOf(Queue, "used") + @sizeOf(VirtqUsed);
    const owned = @offsetOf(Queue, "last_used");
    try std.testing.expect(owned < @offsetOf(Queue, "used") or owned >= std.mem.alignForward(usize, used_end, 64));
    try std.testing.expectEqual(@as(usize, 0), max_message % 64);
    try std.testing.expectEqual(@as(usize, 8192), @sizeOf([identity_pin_max]IdentityPin));
}

/// B3 enumeration uses a pinned directory and fresh no-follow LOOKUPs.
/// B2's immutable name snapshot, by contrast, is not containment.
pub fn metadata_snapshot(backend: metadata.Backend, auth: metadata.Authorizer, path: []const u8, out: *metadata.Snapshot) metadata.Error!void {
    out.count = 0;
    const object = try metadata.resolve(backend, auth, path, .metadata);
    defer unpin(backend.context, object);
    const before = try metadata.checkedStat(backend, object);
    if (before.kind != .directory) return error.NotDirectory;
    const saved = fs_lock.lock();
    defer fs_lock.unlock(saved);
    var open_input: [8]u8 = .{0} ** 8;
    const opened = transact(fuse_opendir, object.token, &open_input) orelse return metadata_failure();
    if (opened.len < 16) return error.InvalidMetadata;
    var release: [24]u8 = .{0} ** 24;
    write64(&release, 0, read64(opened, 0));
    var released = false;
    defer if (!released) {
        if (transact(fuse_releasedir, object.token, &release) == null) fs_ready = false;
    };
    var offset: u64 = 0;
    var pages: usize = 0;
    while (true) {
        if (pages == directory.entry_max + 2) return error.TreeLimit;
        pages += 1;
        var input: [40]u8 = .{0} ** 40;
        write64(&input, 0, read64(&release, 0));
        write64(&input, 8, offset);
        write32(&input, 16, max_io);
        const reply = transact(fuse_readdir, object.token, &input) orelse return metadata_failure();
        if (reply.len == 0) break;
        if (reply.len > max_io) return error.InvalidMetadata;
        var page: [max_io]u8 = undefined;
        @memcpy(page[0..reply.len], reply);
        const len = reply.len;
        var at: usize = 0;
        while (at < len) {
            if (len - at < 24) return error.InvalidMetadata;
            const next = read64(&page, at + 8);
            const nlen = read32(&page, at + 16);
            if (nlen > directory.name_max) return error.PathLimit;
            const step = (@as(usize, 24) + nlen + 7) & ~@as(usize, 7);
            if (nlen == 0 or step > len - at or next == offset or next == 0) return error.InvalidMetadata;
            const name = page[at + 24 ..][0..nlen];
            if (!std.mem.eql(u8, name, ".") and !std.mem.eql(u8, name, "..")) {
                var relative: [metadata.max_relative_path]u8 = undefined;
                const prefix: usize = if (path.len == 0) 0 else path.len + 1;
                if (prefix + name.len > relative.len) return error.PathLimit;
                @memcpy(relative[0..path.len], path);
                if (prefix != 0) relative[path.len] = '/';
                @memcpy(relative[prefix..][0..name.len], name);
                const child_path = relative[0 .. prefix + name.len];
                try metadata.validatePath(child_path);
                if (backend.root_path.len + @intFromBool(backend.root_path.len != 0) + child_path.len > metadata.max_relative_path) return error.PathLimit;
                try auth.check(auth.context, child_path, .metadata);
                const child = try lookup_locked(object.token, name);
                defer release_lookup_locked(child.token);
                const value = try attr_locked(child.token);
                if (!value.identity.eql(child.metadata.identity)) return error.StaleIdentity;
                try out.append(name, value);
            }
            offset = next;
            at += step;
        }
    }
    const after = try attr_locked(object.token);
    if (!after.identity.eql(before.identity) or
        metadata.Metadata.watchChanged(before, after) or
        after.ctime.toNanoseconds() != before.ctime.toNanoseconds()) return error.StaleIdentity;
    _ = transact(fuse_releasedir, object.token, &release) orelse return metadata_failure();
    released = true;
    out.sort();
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
        1, 13 => st_access, // EPERM / EACCES (FUSE wire errno, not native errno)
        38, 95 => st_unsupported, // ENOSYS / EOPNOTSUPP
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
    if (path.len == 0 or path.len > cache_path_max) return;
    var slot: usize = 1;
    while (slot < nodes.len and nodes[slot].valid) : (slot += 1) {}
    if (slot == nodes.len) slot = 1 + (nodeid % (nodes.len - 1));
    nodes[slot] = .{
        .path = [_]u8{0} ** cache_path_max,
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
    if (!directory.valid_name(name)) return null;
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
        .path = [_]u8{0} ** cache_path_max,
        .path_len = 0, // Only cache_put materializes a bounded cache key.
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
    if (handle.contained and handle.access != .write) return st_access;
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

fn directory_stamp(nodeid: u64) ?[24]u8 {
    var input: [16]u8 = .{0} ** 16;
    const reply = transact(fuse_getattr, nodeid, &input) orelse return null;
    if (reply.len < 104) return null;
    var stamp: [24]u8 = undefined;
    // fuse_attr mtime/ctime seconds and their nanoseconds (not atime).
    @memcpy(stamp[0..16], reply[16 + 32 ..][0..16]);
    @memcpy(stamp[16..24], reply[16 + 52 ..][0..8]);
    return stamp;
}

/// Parse a complete FUSE page and preserve the backend's opaque cookie.
/// A malformed/non-progressing page is an error, never end-of-directory.
pub fn snapshot_page(page: []const u8, offset: *u64, parent: ?u64, out: *directory.Snapshot) u8 {
    var at: usize = 0;
    while (at < page.len) {
        if (page.len - at < 24) return st_host_error;
        const next = read64(page, at + 8);
        const nlen = read32(page, at + 16);
        if (nlen > directory.name_max) return st_path_limit;
        const len = (@as(usize, 24) + nlen + 7) & ~@as(usize, 7);
        if (nlen == 0 or len > page.len - at or next == offset.* or next == 0) return st_host_error;
        const name = page[at + 24 ..][0..nlen];
        if (!std.mem.eql(u8, name, ".") and !std.mem.eql(u8, name, "..")) {
            var size: u64 = 0;
            var is_dir = read32(page, at + 20) == dirent_type_dir;
            if (parent) |nodeid| {
                const child = lookup(nodeid, name, "") orelse return status_from_error();
                size = child.size;
                is_dir = (child.mode & file_type_mask) == file_type_dir;
                if (is_dir) size = 0;
            }
            out.append(name, size, is_dir) catch |err| return switch (err) {
                error.EntryLimit => st_limit,
                error.NameLimit => st_path_limit,
                else => st_changed,
            };
        }
        offset.* = next;
        at += len;
    }
    return st_ok;
}

pub fn snapshot(path: []const u8, out: *directory.Snapshot) u8 {
    out.count = 0;
    if (!directory.valid_path(path, directory.depth_max)) return st_path_limit;
    if (!available()) return st_host_error;
    const saved = fs_lock.lock();
    defer fs_lock.unlock(saved);
    // A snapshot opens the current path, not a stale cached directory.
    cache_clear();
    const node = resolve_path(path) orelse return status_from_error();
    if ((node.mode & file_type_mask) != file_type_dir) return st_is_dir;
    const before = directory_stamp(node.nodeid) orelse return st_host_error;
    var open_in: [8]u8 = .{0} ** 8;
    const opened = transact(fuse_opendir, node.nodeid, &open_in) orelse return status_from_error();
    if (opened.len < 8) return st_host_error;
    var release: [24]u8 = .{0} ** 24;
    write64(&release, 0, read64(opened, 0));
    var released = false;
    defer if (!released) {
        _ = transact(fuse_releasedir, node.nodeid, &release);
    };
    var offset: u64 = 0;
    // Even hostile streams containing only dot entries cannot loop forever.
    var pages: usize = 0;
    while (true) {
        if (pages == directory.entry_max + 2) return st_host_error;
        pages += 1;
        var input: [40]u8 = .{0} ** 40;
        write64(&input, 0, read64(&release, 0));
        write64(&input, 8, offset);
        write32(&input, 16, max_io);
        const reply = transact(fuse_readdir, node.nodeid, &input) orelse return status_from_error();
        if (reply.len == 0) break; // FUSE EOF, including after exactly 256 entries
        if (reply.len > max_io) return st_host_error;
        // LOOKUP uses the transport reply buffer too.
        var page: [max_io]u8 = undefined;
        @memcpy(page[0..reply.len], reply);
        const status = snapshot_page(page[0..reply.len], &offset, node.nodeid, out);
        if (status != st_ok) return status;
    }
    const after = directory_stamp(node.nodeid) orelse return st_host_error;
    if (!std.mem.eql(u8, &before, &after)) return st_changed;
    _ = transact(fuse_releasedir, node.nodeid, &release) orelse return status_from_error();
    released = true;
    out.sort();
    return st_ok;
}

test "B2: FUSE pages retain opaque continuation and names without clipping" {
    var snapshot_rows = directory.Snapshot{};
    var offset: u64 = 0;
    var page: [280]u8 = .{0} ** 280;
    write64(&page, 8, 0x1234);
    write32(&page, 16, 255);
    @memset(page[24..279], 'x');
    try std.testing.expectEqual(st_ok, snapshot_page(&page, &offset, null, &snapshot_rows));
    try std.testing.expectEqual(@as(u64, 0x1234), offset);
    try std.testing.expectEqual(@as(u16, 255), snapshot_rows.entries[0].name_len);
    try std.testing.expectEqualStrings(&([_]u8{'x'} ** 255), snapshot_rows.entries[0].name[0..255]);
    write64(&page, 8, 0x5678);
    write32(&page, 16, 4);
    @memcpy(page[24..28], "next");
    try std.testing.expectEqual(st_ok, snapshot_page(page[0..32], &offset, null, &snapshot_rows));
    try std.testing.expectEqual(@as(u64, 0x5678), offset);
    try std.testing.expectEqual(@as(usize, 2), snapshot_rows.count);
    try std.testing.expectEqual(st_ok, snapshot_page("", &offset, null, &snapshot_rows));
}

test "B2: malformed, duplicate, non-progressing and overlong FUSE rows refuse" {
    var snapshot_rows = directory.Snapshot{};
    var offset: u64 = 0;
    var page: [280]u8 = .{0} ** 280;
    write64(&page, 8, 1);
    write32(&page, 16, 4);
    @memcpy(page[24..28], "file");
    try std.testing.expectEqual(st_host_error, snapshot_page(page[0..27], &offset, null, &snapshot_rows));
    try std.testing.expectEqual(st_ok, snapshot_page(page[0..32], &offset, null, &snapshot_rows));
    try std.testing.expectEqual(st_host_error, snapshot_page(page[0..32], &offset, null, &snapshot_rows));
    write64(&page, 8, 2);
    try std.testing.expectEqual(st_changed, snapshot_page(page[0..32], &offset, null, &snapshot_rows));
    write32(&page, 16, 256);
    try std.testing.expectEqual(st_path_limit, snapshot_page(&page, &offset, null, &snapshot_rows));
    write32(&page, 16, 4);
    @memcpy(page[24..28], "a\x00bc");
    try std.testing.expectEqual(st_changed, snapshot_page(page[0..32], &offset, null, &snapshot_rows));
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

/// One server-side rename: replacement uses FUSE_RENAME; preserve-existing
/// requires FUSE_RENAME2/NOREPLACE. Never emulate it with a racy lookup followed
/// by FUSE_RENAME. An older/unsupported server refuses without a fallback.
pub fn rename(raw_from: []const u8, raw_to: []const u8, mode: RenameMode) u8 {
    if (!available()) return st_host_error;
    const saved = fs_lock.lock();
    defer fs_lock.unlock(saved);
    if (mode == .preserve_existing and fs_minor < 23) return st_unsupported;
    const from = resolve_parent(raw_from) orelse return status_from_error();
    const to = resolve_parent(raw_to) orelse return status_from_error();
    var input: [16 + max_path * 2 + 2]u8 = [_]u8{0} ** (16 + max_path * 2 + 2);
    const len = encode_rename(to.parent, from.name, to.name, mode, &input) orelse return st_host_error;
    const opcode = if (mode == .replace) fuse_rename else fuse_rename2;
    _ = transact(opcode, from.parent, input[0..len]) orelse return status_from_error();
    cache_clear();
    return st_ok;
}

fn encode_rename(parent: u64, from: []const u8, to: []const u8, mode: RenameMode, out: []u8) ?usize {
    const header_len: usize = if (mode == .replace) 8 else 16;
    const len = header_len + from.len + 1 + to.len + 1;
    if (from.len == 0 or to.len == 0 or from.len > max_path or to.len > max_path or len > out.len) return null;
    if (std.mem.indexOfScalar(u8, from, 0) != null or std.mem.indexOfScalar(u8, to, 0) != null) return null;
    @memset(out[0..len], 0);
    write64(out, 0, parent);
    if (mode == .preserve_existing) write32(out, 8, rename_noreplace);
    @memcpy(out[header_len..][0..from.len], from);
    @memcpy(out[header_len + from.len + 1 ..][0..to.len], to);
    return len;
}

test "virtio_fs: replacement and atomic no-replace have distinct FUSE requests" {
    var payload: [16 + max_path * 2 + 2]u8 = undefined;
    const replace_len = encode_rename(19, "stage", "output", .replace, &payload).?;
    try std.testing.expectEqual(@as(usize, 21), replace_len);
    try std.testing.expectEqual(@as(u64, 19), read64(&payload, 0));
    try std.testing.expectEqualSlices(u8, "stage\x00output\x00", payload[8..replace_len]);
    const preserve_len = encode_rename(19, "stage", "output", .preserve_existing, &payload).?;
    try std.testing.expectEqual(@as(usize, 29), preserve_len);
    try std.testing.expectEqual(rename_noreplace, read32(&payload, 8));
    try std.testing.expectEqual(@as(u32, 0), read32(&payload, 12));
    try std.testing.expectEqualSlices(u8, "stage\x00output\x00", payload[16..preserve_len]);
    try std.testing.expect(encode_rename(1, "stage", "output", .replace, payload[0..20]) == null);
    try std.testing.expect(encode_rename(1, "", "output", .replace, &payload) == null);
    try std.testing.expect(encode_rename(1, "stage\x00escape", "output", .replace, &payload) == null);
}

test "virtio_fs: rename failure statuses are explicit" {
    const before = fs_error;
    defer fs_error = before;
    for ([_]struct { errno: i32, status: u8 }{
        .{ .errno = -2, .status = st_not_found },
        .{ .errno = -17, .status = st_exists },
        .{ .errno = -13, .status = st_access },
        .{ .errno = -1, .status = st_access },
        .{ .errno = -38, .status = st_unsupported },
        .{ .errno = -95, .status = st_unsupported },
        .{ .errno = -5, .status = st_host_error },
    }) |case| {
        fs_error = case.errno;
        try std.testing.expectEqual(case.status, status_from_error());
    }
}

test "virtio_fs: an old server refuses no-replace before any request" {
    const ready = fs_ready;
    const initialized = fs_initialized;
    const minor = fs_minor;
    const unique = next_unique;
    defer {
        fs_ready = ready;
        fs_initialized = initialized;
        fs_minor = minor;
    }
    fs_ready = true;
    fs_initialized = true;
    fs_minor = 22;
    try std.testing.expectEqual(st_unsupported, rename("stage", "output", .preserve_existing));
    try std.testing.expectEqual(unique, next_unique);
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
        .path = [_]u8{0} ** cache_path_max,
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
