//! The drag-and-drop payload record (M52 card 3) — set by the seat, consumed by the drop target.
//!
//! Split out of driving_award.zig (#3754) — shared registry state lives in the
//! module root and is reached through `da`; this file owns the seam's own
//! private state.

const da = @import("../driving_award.zig");
const drag_payload_max = da.drag_payload_max;

/// Arc4 #237: store a drag payload from the source app (slot 48).
/// Called from the syscall handler.
pub fn drag_start(payload: []const u8, source_pid: usize) void {
    const copy_len = @min(payload.len, drag_payload_max);
    @memcpy(da.drag_payload[0..copy_len], payload[0..copy_len]);
    da.drag_payload_len = copy_len;
    da.drag_active = true;
    da.drag_source_pid = source_pid;
    da.drag_over_id = null;
}

/// Arc4 #237: check if a drag is currently active.
pub fn drag_is_active() bool {
    return da.drag_active;
}

/// Arc4 #237: get the drag payload for copy to target.
pub fn drag_get_payload() []const u8 {
    return da.drag_payload[0..da.drag_payload_len];
}

/// Arc4 #237: cancel an active drag (e.g. source window closed).
/// M52 card 3 (#1240): the source pid clears with the payload — a cancelled
/// drag must not leave state naming a process that may already be gone (the
/// DRAG_ENTER/LEAVE/DROP fans are gated on `drag_active`, so a stale pid was
/// inert, but "inert stale capture state" is exactly what this card bans).
pub fn drag_cancel() void {
    da.drag_active = false;
    da.drag_payload_len = 0;
    da.drag_source_pid = 0;
    da.drag_over_id = null;
}
