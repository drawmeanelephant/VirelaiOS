//! Window persistence (M21 W11): WINDOWS.SAV serialize/restore across sessions.
//!
//! Split out of driving_award.zig (#3754) — shared registry state lives in the
//! module root and is reached through `da`; this file owns the seam's own
//! private state.

const da = @import("../driving_award.zig");
const svclock = @import("../svclock.zig");
const trust = @import("../trust.zig");
const virtio_file = @import("../virtio_file.zig");
const UserOpenResult = da.UserOpenResult;
const arm = da.arm;
const find_user_window = da.find_user_window;
const set_window_title = da.set_window_title;
const std = da.std;
const user_open = da.user_open;
const user_windows_max = da.user_windows_max;
const virtio_gpu = da.virtio_gpu;

// ---------------------------------------------------------------------------
// M21 W11 — Window persistence across sessions
// ---------------------------------------------------------------------------

pub const persist_record_bytes: usize = 32;
pub const persist_max_records: usize = 16;
pub const persist_max_bytes: usize = persist_record_bytes * persist_max_records;

pub fn serialize_state(buf: []u8) usize {
    if (buf.len < persist_max_bytes) return 0;
    var off: usize = 0;
    var wi: usize = 0;
    while (wi < da.win_count and off + persist_record_bytes <= buf.len) : (wi += 1) {
        const w = &da.windows[wi];
        // #2865: a phantom is a replay of a record this file already
        // carried — writing it again would grow the file every boot (the
        // app's fresh windows record themselves) until the eight slots
        // were all ghosts and sys_win_open refused ENOSPC. The file
        // describes LIVE windows only.
        if (w.kind != .user or w.phantom) continue;
        var flags: u8 = 0;
        if (w.minimized) flags |= 0x01;
        if (w.maximized) flags |= 0x02;
        if (w.always_on_top) flags |= 0x04;
        buf[off] = w.id;
        buf[off + 1] = flags;
        buf[off + 2] = w.workspace;
        buf[off + 3] = 0;
        buf[off + 4] = @truncate(w.x);
        buf[off + 5] = @truncate(w.x >> 8);
        buf[off + 6] = @truncate(w.x >> 16);
        buf[off + 7] = @truncate(w.x >> 24);
        buf[off + 8] = @truncate(w.y);
        buf[off + 9] = @truncate(w.y >> 8);
        buf[off + 10] = @truncate(w.y >> 16);
        buf[off + 11] = @truncate(w.y >> 24);
        buf[off + 12] = @truncate(w.w);
        buf[off + 13] = @truncate(w.w >> 8);
        buf[off + 14] = @truncate(w.w >> 16);
        buf[off + 15] = @truncate(w.w >> 24);
        buf[off + 16] = @truncate(w.h);
        buf[off + 17] = @truncate(w.h >> 8);
        buf[off + 18] = @truncate(w.h >> 16);
        buf[off + 19] = @truncate(w.h >> 24);
        @memset(buf[off + 20 .. off + 32], 0);
        const title = if (w.title_len > 0) w.title_buf[0..w.title_len] else w.title;
        const tlen = @min(title.len, 12);
        @memcpy(buf[off + 20 .. off + 20 + tlen], title[0..tlen]);
        off += persist_record_bytes;
    }
    return off;
}

pub fn restore_state(buf: []const u8, owner: usize) usize {
    var restored: usize = 0;
    var off: usize = 0;
    while (off + persist_record_bytes <= buf.len) : (off += persist_record_bytes) {
        const id: u8 = buf[off];
        if (id == 0) continue;
        const flags: u8 = buf[off + 1];
        const ws: u8 = buf[off + 2];
        const x = read_u32_le(buf[off + 4 .. off + 8]);
        const y = read_u32_le(buf[off + 8 .. off + 12]);
        const ww = read_u32_le(buf[off + 12 .. off + 16]);
        const wh = read_u32_le(buf[off + 16 .. off + 20]);
        if (ww == 0 or wh == 0) continue;
        if (x >= virtio_gpu.fb_width or y >= virtio_gpu.fb_height) continue;
        const result = user_open(x, y, ww, wh, owner);
        if (result != .opened) continue;
        const new_id = result.opened;
        const w = find_user_window(new_id) orelse continue;
        w.minimized = (flags & 0x01) != 0;
        w.maximized = (flags & 0x02) != 0;
        w.always_on_top = (flags & 0x04) != 0;
        w.workspace = ws;
        w.owner = owner;
        // #2865: mark the replay so serialize_state never re-persists it
        // and a slot-hungry live open can evict it.
        w.phantom = true;
        var tlen: usize = 0;
        while (tlen < 12 and buf[off + 20 + tlen] != 0) : (tlen += 1) {}
        if (tlen > 0) {
            _ = set_window_title(new_id, buf[off + 20 .. off + 20 + tlen]);
        }
        restored += 1;
    }
    return restored;
}

pub fn read_u32_le(b: []const u8) u32 {
    return @as(u32, b[0]) | (@as(u32, b[1]) << 8) | (@as(u32, b[2]) << 16) | (@as(u32, b[3]) << 24);
}

// ---------------------------------------------------------------------------
// WINDOWS.SAV file channel — moved out of shell.zig for #3759's fifth item.
// The serialize/replay above are the record primitives; the functions below
// are the APPLIED operation every decision channel lands on: the shim's boot
// restore + idle tick and the seat's slot-65 PERSIST verb run the same code
// (parity by construction). Who calls them and when is the policy the drain
// moved to userland; what they do to the registry and the share is the
// kernel fact.
// ---------------------------------------------------------------------------

/// Bracket one persist operation in the domains it touches: serialize reads
/// the window registry (WIN), the WINDOWS.SAV I/O rides the host file
/// channel (FILE), and a replay's `user_open` can fan kind-20 mirrors into
/// the seat's queue (EV — the hook fires only while one is registered).
/// A caller that already holds a lock ranking ABOVE file or win cannot take
/// the lower pair under its hold — sys_wmctl's dispatch domain is EV — so
/// the held set is dropped, the union re-taken in canonical order, and the
/// caller's own bits stay held at exit (the exit-thread precedent).
const PersistLock = struct {
    missing: u5,
};

fn persist_lock() PersistLock {
    const need = svclock.dom_bit(.file) | svclock.dom_bit(.win) | svclock.dom_bit(.ev);
    const held = svclock.held_bits();
    const missing = need & ~held;
    if (missing == 0) return .{ .missing = 0 };
    svclock.release_set(held);
    svclock.acquire_set(held | missing);
    return .{ .missing = missing };
}

fn persist_unlock(guard: PersistLock) void {
    svclock.release_set(guard.missing);
}

/// #563: skip the write when the serialized state is byte-identical to the
/// last one actually written. The persist tick fires every 300 idle cycles
/// — at the real (~250 Hz) idle-loop rate that is roughly once a second —
/// and each write's cluster-allocation scan reads ~170 FAT sectors (the
/// ESP's file region spans ~21.8k clusters). A stable desktop was rewriting
/// identical WINDOWS.SAV content every tick, keeping the virtio-blk
/// transport continuously busy and starving concurrent reads (the
/// verify-live-desktop CALC exec hit a transport timeout and surfaced as
/// ENOENT). Unchanged state is not persisted: no disk traffic, same
/// restore semantics.
var last_saved_windows: [persist_max_bytes]u8 = undefined;
var last_saved_windows_len: usize = 0;
var have_last_saved_windows: bool = false;

/// Serialize the LIVE window set and persist it as WINDOWS.SAV on the host
/// share. M34 HF6 (issue #740): the share is the only store — with no file
/// channel the write refuses honestly and the dedup stays unarmed, so the
/// next save retries the same content instead of skipping it.
pub fn save_now() void {
    const guard = persist_lock();
    defer persist_unlock(guard);
    var buf: [persist_max_bytes]u8 = undefined;
    const n = serialize_state(&buf);
    if (have_last_saved_windows and last_saved_windows_len == n and std.mem.eql(u8, last_saved_windows[0..n], buf[0..n])) return;
    // M50 TS2 (ADR 0024 D4): the kernel-actor gate (secret paths deny).
    // #2865: an empty live set is persisted TOO — restored phantoms are
    // never re-serialized, so a stale file would otherwise replay the
    // same dead rows every boot forever (a fixed-point leak, not
    // accumulation but the same ghost). Delete it; absent is already the
    // goal state.
    if (n == 0) {
        if (trust.check(trust.kernel_actor(), .host, "WINDOWS.SAV", .delete) != .allow) return;
        const dst = virtio_file.delete("WINDOWS.SAV");
        if (dst == virtio_file.st_ok or dst == virtio_file.st_not_found) {
            last_saved_windows_len = 0;
            have_last_saved_windows = true;
        }
        return;
    }
    if (trust.check(trust.kernel_actor(), .host, "WINDOWS.SAV", .write) != .allow) return;
    if (virtio_file.write_whole("WINDOWS.SAV", buf[0..n]) == virtio_file.st_ok) {
        @memcpy(last_saved_windows[0..n], buf[0..n]);
        last_saved_windows_len = n;
        have_last_saved_windows = true;
    }
}

/// #3759 (fifth item): a replay is consumed once per boot — whichever
/// decision channel lands first (the shim's wm=none boot restore, the
/// seat_impossible fallback, or the seat's PERSIST verb) owns it. A second
/// call reports 0 rather than stacking a duplicate phantom set on the same
/// records: `restore_state` opens NEW windows each pass, so an unguarded
/// re-replay fills the slots with copies. Reset by `arm()` — the per-boot
/// arm in production, the per-test boot simulation in host tests.
pub var replay_done: bool = false;

/// Replay WINDOWS.SAV: read the share file and re-open each record as a
/// phantom adopted by `owner` — the CALLER's adoption decision (the shim
/// passes the sentinel 99; a seat names whichever pid it wants over
/// slot-65). Returns the restored count; 0 when the boot's replay is
/// already consumed, the file is absent/empty, or the trust/channel path
/// yields nothing.
pub fn restore_now(owner: usize) usize {
    if (replay_done) return 0;
    const guard = persist_lock();
    defer persist_unlock(guard);
    if (replay_done) return 0; // re-check under the lock
    var content: [persist_max_bytes]u8 = undefined;
    const n = blk: {
        if (trust.check(trust.kernel_actor(), .host, "WINDOWS.SAV", .read) != .allow) break :blk null;
        break :blk virtio_file.read_whole("WINDOWS.SAV", &content);
    } orelse return 0;
    if (n == 0) return 0;
    replay_done = true;
    return restore_state(content[0..n], owner);
}

test "persist: a WINDOWS.SAV replay is never re-serialized (#2865)" {
    // Boot 1: two live windows serialize to two records.
    arm();
    _ = switch (user_open(32, 32, 320, 240, 7)) {
        .opened => |i| i,
        else => return error.TestUnexpectedResult,
    };
    _ = switch (user_open(96, 96, 320, 240, 8)) {
        .opened => |i| i,
        else => return error.TestUnexpectedResult,
    };
    var buf: [persist_max_bytes]u8 = undefined;
    const saved = serialize_state(&buf);
    try std.testing.expectEqual(2 * persist_record_bytes, saved);

    // Boot 2: the replay lands as phantoms — live windows in every
    // respect except persistence. The app's fresh windows open
    // alongside, and the serialized set is the LIVE windows only: the
    // file cannot grow across boots (the pre-fix 4 -> 8 -> ENOSPC leak).
    arm();
    try std.testing.expectEqual(2, restore_state(buf[0..saved], 99));
    var wi: usize = 0;
    while (wi < da.win_count) : (wi += 1) {
        if (da.windows[wi].kind == .user) try std.testing.expect(da.windows[wi].phantom);
    }
    try std.testing.expectEqual(0, serialize_state(&buf));
    _ = switch (user_open(32, 32, 320, 240, 7)) {
        .opened => |i| i,
        else => return error.TestUnexpectedResult,
    };
    _ = switch (user_open(96, 96, 320, 240, 8)) {
        .opened => |i| i,
        else => return error.TestUnexpectedResult,
    };
    try std.testing.expectEqual(saved, serialize_state(&buf));
}

test "persist: a live open evicts a phantom before reporting .full (#2865)" {
    // Boot 1: eight live windows serialize to a full record set.
    arm();
    var i: usize = 0;
    while (i < user_windows_max) : (i += 1) {
        _ = switch (user_open(64, 48, 320, 240, 7)) {
            .opened => |id| id,
            else => return error.TestUnexpectedResult,
        };
    }
    var buf: [persist_max_bytes]u8 = undefined;
    const saved = serialize_state(&buf);
    try std.testing.expectEqual(user_windows_max * persist_record_bytes, saved);

    // Boot 2: eight phantoms hold every slot — the pre-fix boot died
    // here (sys_win_open -> ENOSPC). Each live open must evict a ghost.
    arm();
    try std.testing.expectEqual(user_windows_max, restore_state(buf[0..saved], 99));
    i = 0;
    while (i < user_windows_max) : (i += 1) {
        _ = switch (user_open(64, 48, 320, 240, 7)) {
            .opened => |id| id,
            else => return error.TestUnexpectedResult,
        };
    }
    // Every phantom was evicted — all eight slots are live again, and
    // the serialized set is exactly those eight live windows.
    var phantoms: usize = 0;
    var wi: usize = 0;
    while (wi < da.win_count) : (wi += 1) {
        if (da.windows[wi].kind == .user and da.windows[wi].phantom) phantoms += 1;
    }
    try std.testing.expectEqual(0, phantoms);
    try std.testing.expectEqual(user_windows_max * persist_record_bytes, serialize_state(&buf));
    // Eight LIVE windows is still honestly .full.
    try std.testing.expectEqual(UserOpenResult.full, user_open(64, 48, 320, 240, 7));
}
