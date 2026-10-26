//! Window persistence (M21 W11): WINDOWS.SAV serialize/restore across sessions.
//!
//! Split out of driving_award.zig (#3754) — shared registry state lives in the
//! module root and is reached through `da`; this file owns the seam's own
//! private state.

const da = @import("../driving_award.zig");
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
