//! The drag-and-drop payload record (M52 card 3) — set by the seat, consumed by the drop target.
//!
//! Split out of driving_award.zig (#3754) — shared registry state lives in the
//! module root and is reached through `da`; this file owns the seam's own
//! private state.
//!
//! #3759 (final item): the record's LIFECYCLE moved to the seat. While a
//! seat owns input, `sys_drag_start` stages the source's payload into the
//! `drag_armed_*` record and the kind-22 WM_DRAG mirror tells the seat a
//! record waits — the seat TAKEs it (slot-65 DRAG cmd 27) and owns it end
//! to end: it drives hover/drop from its own kind-19/20 streams and calls
//! back for each delivery the kernel applies (`drag_enter`/`drag_leave`/
//! `drag_drop` — the owner-resolved event pushes + the consume-on-read
//! staging are the kept kernel primitives). Shim mode is unchanged: the
//! kernel's own hit-test fan drives the delivery record directly.

const da = @import("../driving_award.zig");
const drag_payload_max = da.drag_payload_max;
const events = da.events;
const find_user_window = da.find_user_window;

/// Arc4 #237: store a drag payload from the source app (slot 48).
/// Called from the syscall handler.
/// #3759 (final item): under a registered seat (`wm_owns_input`) the arm
/// stages into `drag_armed_*` — the delivery record stays dark until the
/// seat's own DROP decision lands it. With no seat the delivery record
/// takes the payload directly, byte-identical to before (the kernel's
/// hit-test fan in pointer.zig drives it — the shim fallback).
pub fn drag_start(payload: []const u8, source_pid: usize) void {
    const copy_len = @min(payload.len, drag_payload_max);
    if (da.wm_owns_input) {
        @memcpy(da.drag_armed_payload[0..copy_len], payload[0..copy_len]);
        da.drag_armed_len = copy_len;
        da.drag_armed_source_pid = source_pid;
        da.drag_armed = true;
        if (da.wm_drag_hook) |hook| hook(copy_len, source_pid);
        return;
    }
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

// ---------------------------------------------------------------------------
// #3759 (final item): the seat-facing half — the arm staging the seat TAKEs
// and the delivery primitives the kernel applies on the seat's calls.
// ---------------------------------------------------------------------------

/// A view on the staged arm record for the seat's TAKE — NOT a consume:
/// the copy-out happens in the syscall layer and a failed one must not
/// lose the record (the seat retries the take). `drag_armed_cancel`
/// clears it after a successful copy, on the seat's CANCEL, or when the
/// source dies before the take.
pub const ArmedRecord = struct {
    source_pid: usize,
    payload: []const u8,
};

/// The staged arm record, or null when nothing waits for the seat.
pub fn drag_armed_record() ?ArmedRecord {
    if (!da.drag_armed) return null;
    return .{
        .source_pid = da.drag_armed_source_pid,
        .payload = da.drag_armed_payload[0..da.drag_armed_len],
    };
}

/// Clear the arm staging — after a successful TAKE, on the seat's CANCEL
/// decision, or when the source process dies before the record was taken
/// (the same no-stale-capture rule as the delivery record's cancel).
pub fn drag_armed_cancel() void {
    da.drag_armed = false;
    da.drag_armed_len = 0;
    da.drag_armed_source_pid = 0;
}

/// The seat's hover-edge call (DRAG cmd 27 act 1): push DRAG_ENTER to
/// `id`'s owner — the same arg shape the shim's pointer fan emitted
/// (`arg0` = payload size, `arg1` = source pid). False for an unknown or
/// unowned window id (nothing deliverable sits there).
pub fn drag_enter(id: u8, payload_len: usize, source_pid: usize) bool {
    const win = find_user_window(id) orelse return false;
    const owner = win.owner orelse return false;
    events.push(owner, .{
        .kind = events.DRAG_ENTER,
        .flags = 0,
        .seq = 0,
        .arg0 = @intCast(payload_len),
        .arg1 = @intCast(source_pid),
    });
    return true;
}

/// The seat's hover-edge call (DRAG cmd 27 act 2): push DRAG_LEAVE to
/// `id`'s owner (`arg0` = 0, `arg1` = source pid — the shim's shape).
pub fn drag_leave(id: u8, source_pid: usize) bool {
    const win = find_user_window(id) orelse return false;
    const owner = win.owner orelse return false;
    events.push(owner, .{
        .kind = events.DRAG_LEAVE,
        .flags = 0,
        .seq = 0,
        .arg0 = 0,
        .arg1 = @intCast(source_pid),
    });
    return true;
}

/// The seat's DROP decision (DRAG cmd 27 act 3): stage the seat's payload
/// copy into the DELIVERY record (the one `sys_drag_read` consumes) and
/// push DROP to the target's owner (`arg0` = payload size, `arg1` =
/// source pid — the shim's shape). The staged bytes stay until the target
/// reads (consume-on-read), a new drop re-stages, or a cancel clears —
/// the same contract the shim's own fan produced.
pub fn drag_drop(id: u8, payload: []const u8, source_pid: usize) bool {
    const win = find_user_window(id) orelse return false;
    const owner = win.owner orelse return false;
    const copy_len = @min(payload.len, drag_payload_max);
    @memcpy(da.drag_payload[0..copy_len], payload[0..copy_len]);
    da.drag_payload_len = copy_len;
    da.drag_source_pid = source_pid;
    da.drag_over_id = null;
    da.drag_active = true;
    events.push(owner, .{
        .kind = events.DROP,
        .flags = 0,
        .seq = 0,
        .arg0 = @intCast(copy_len),
        .arg1 = @intCast(source_pid),
    });
    return true;
}
