//! The notification queue and notification center (Arc2 W3 / M27).
//!
//! Split out of driving_award.zig (#3754) — shared registry state lives in the
//! module root and is reached through `da`; this file owns the seam's own
//! private state.

const da = @import("../driving_award.zig");
const mark_dirty = da.mark_dirty;
const notify_dismiss_ticks = da.notify_dismiss_ticks;
const notify_max = da.notify_max;
const notify_text_max = da.notify_text_max;
const virtio_snd = da.virtio_snd;

/// Push a notification into the bounded FIFO (drop-oldest on overflow).
/// Called from the syscall handler (kernel context).
pub fn notify_push(text: []const u8, level: u8) void {
    const slot = (da.notify_head + da.notify_count) % notify_max;
    const copy_len = @min(text.len, notify_text_max);
    @memcpy(da.notify_texts[slot][0..copy_len], text[0..copy_len]);
    da.notify_lens[slot] = copy_len;
    da.notify_levels[slot] = level;
    da.notify_ticks[slot] = 0;
    if (da.notify_count < notify_max) {
        da.notify_count += 1;
    } else {
        // Drop-oldest: advance head.
        da.notify_head = (da.notify_head + 1) % notify_max;
    }
    // M27 G5: sound feedback for notifications
    if (virtio_snd.snd_status() == 0x0f) {
        if (level == 2) {
            _ = virtio_snd.snd_beep(880, 100);
        } else {
            _ = virtio_snd.snd_beep(440, 50);
        }
    }
}

/// Drain (dismiss) a notification by index within the visible set.
/// Returns true if dismissed.
pub fn notify_dismiss(index: usize) bool {
    if (index >= da.notify_count) return false;
    // Compact: shift remaining entries forward.
    var i: usize = index;
    while (i + 1 < da.notify_count) : (i += 1) {
        const src = (da.notify_head + i + 1) % notify_max;
        const dst = (da.notify_head + i) % notify_max;
        const n = da.notify_lens[src];
        @memcpy(da.notify_texts[dst][0..n], da.notify_texts[src][0..n]);
        da.notify_lens[dst] = n;
        da.notify_levels[dst] = da.notify_levels[src];
        da.notify_ticks[dst] = da.notify_ticks[src];
    }
    da.notify_count -= 1;
    return true;
}

/// Advance notification ticks (called once per composite). Auto-dismiss
/// entries that have exceeded notify_dismiss_ticks.
pub fn notify_advance_ticks() void {
    if (da.notify_count == 0) return;
    var i: usize = da.notify_count;
    while (i > 0) {
        i -= 1;
        const slot = (da.notify_head + i) % notify_max;
        da.notify_ticks[slot] +|= 1;
        if (da.notify_ticks[slot] >= notify_dismiss_ticks) {
            _ = notify_dismiss(i);
        }
    }
}

/// The current notification count (for compositor rendering).
pub fn notify_count_visible() usize {
    return da.notify_count;
}

/// Read notification data by visible index (0 = newest).
pub fn notify_entry(index: usize) ?struct { text: []const u8, level: u8 } {
    if (index >= da.notify_count) return null;
    const slot = (da.notify_head + index) % notify_max;
    return .{ .text = da.notify_texts[slot][0..da.notify_lens[slot]], .level = da.notify_levels[slot] };
}

/// M21 W5: toggle the notification center panel. Called on tray clock click.
pub fn notif_center_toggle() void {
    da.notif_center_open = !da.notif_center_open;
    _ = mark_dirty(0);
}

/// M32 WMS6 Gate B (issue #626): open/close the panel explicitly — the
/// decision channel for the registered WM (NOTIF_CENTER cmd 6). The kernel
/// clamps + blits; the WM says open or closed.
pub fn notif_center_set_open(open: bool) void {
    da.notif_center_open = open;
    _ = mark_dirty(0);
}

/// M21 W5: dismiss a notification by index in the center panel.
pub fn notif_center_dismiss(index: usize) bool {
    const result = notify_dismiss(index);
    if (result) _ = mark_dirty(0);
    return result;
}

/// M21 W5: clear all notifications from the center panel.
pub fn notif_center_clear_all() void {
    da.notify_count = 0;
    da.notify_head = 0;
    _ = mark_dirty(0);
}

// #3759 (fourth item): the kernel's `notif_center_hit_test` is DELETED —
// hit-testing a shown panel is the seat's decision, and the kernel's own
// click path was already drained by WMS8 (panel clicks fan out as kind 19;
// the seat hit-tests through the shared wnd_core.notif_center_hit_at rule,
// the same numbers draw_chrome paints).
