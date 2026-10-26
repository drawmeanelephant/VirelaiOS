//! Pointer input plumbing: absolute-pointer decode, the cursor blit surface, terminal-selection hit-testing, and the registered-WM fan-out.
//!
//! Split out of driving_award.zig (#3754) — shared registry state lives in the
//! module root and is reached through `da`; this file owns the seam's own
//! private state.

const da = @import("../driving_award.zig");
const CursorRect = da.CursorRect;
const arm = da.arm;
const clamp_resize_h = da.clamp_resize_h;
const clamp_resize_w = da.clamp_resize_w;
const cursor_h = da.cursor_h;
const cursor_hotspot = da.cursor_hotspot;
const cursor_mask = da.cursor_mask;
const cursor_outline_rgb = da.cursor_outline_rgb;
const cursor_rgb = da.cursor_rgb;
const cursor_w = da.cursor_w;
const drain = da.drain;
const events = da.events;
const find_user_window = da.find_user_window;
const focus = da.focus;
const focus_at = da.focus_at;
const font_metrics = da.font_metrics;
const hit_test = da.hit_test;
const input = da.input;
const is_resize_hit = da.is_resize_hit;
const klog = da.klog;
const mark_dirty = da.mark_dirty;
const modal_active = da.modal_active;
const put_px = da.put_px;
const raise = da.raise;
const std = da.std;
const terminal = da.terminal;
const tooltip_clear = da.tooltip_clear;
const topmost_modal_id = da.topmost_modal_id;
const user_close = da.user_close;
const user_open = da.user_open;
const user_resize = da.user_resize;
const user_set_visible = da.user_set_visible;
const user_title_h = da.user_title_h;
const virtio_gpu = da.virtio_gpu;

/// The last pointer state already fanned to the WM, in the hook's pixel
/// space. Shared by BOTH fan entry points — the per-report decode fan
/// (main.zig's cv_pointer_dispatch) and pointer_tick's state-sampling fan
/// — so a report one delivered is never repeated by the other.
pub var wm_fan_valid: bool = false;
var wm_fan_x: u32 = 0;
var wm_fan_y: u32 = 0;
var wm_fan_buttons: u8 = 0;

/// Fan ONE absolute-pointer state to the registered WM's kind-19 stream,
/// deduped against the last fanned state (mapped-pixel space — the hook's
/// coordinate contract — so identical reports and same-pixel logicals both
/// collapse, exactly like the tick sampler's moved/btn_changed check).
///
/// Two callers, one record: cv_pointer_dispatch calls this PER DECODED
/// REPORT — the used-ring pump can complete several reports in one idle
/// pass, and the tick's state sampling collapsed them to the last write
/// (observed on go-dogfood boot 08: a press batched with its first
/// held-move presented the MOVE's coordinates as the down edge — the app
/// saw a press off its list rows, the drag never armed, and the arm
/// markers never printed) — while pointer_tick calls it with the sampled
/// state, covering the USB path's one-report-per-pass decode and any
/// caller that bypassed the per-report fan. The keyboard seam has always
/// fanned per report (decode_keyboard_report -> wm_key_hook); this makes
/// the pointer seam symmetric.
pub fn wm_fan_pointer(x: u16, y: u16, buttons: u8) void {
    if (!da.wm_owns_input) return;
    const nx = map_pointer_axis(x, virtio_gpu.fb_width);
    const ny = map_pointer_axis(y, virtio_gpu.fb_height);
    if (wm_fan_valid and nx == wm_fan_x and ny == wm_fan_y and
        buttons == wm_fan_buttons) return;
    wm_fan_valid = true;
    wm_fan_x = nx;
    wm_fan_y = ny;
    wm_fan_buttons = buttons;
    // #2896: stamp the modifier byte HERE — at the sample's own decode —
    // not where the seat later consumes it. The seat's queue trails a
    // concurrent chord burst, so a live kb_mods read at forward time can
    // disagree with what was held when this sample was captured.
    if (da.wm_pointer_hook) |hook| hook(nx, ny, buttons, input.report().kb_mods);
}
var cursor_saved: [cursor_w * cursor_h]u32 = undefined;
pub var cursor_drawn: bool = false;
var cursor_drawn_x: u32 = 0;
var cursor_drawn_y: u32 = 0;
var cursor_drawn_resize: bool = false;
/// M49 SD5 (#1132): a terminal selection drag is in progress (pointer held
/// inside a terminal window's client area).
var term_sel_dragging: bool = false;

/// #2869: under `?1000` alone the press reported to the app but the mode
/// never subscribed to the drag — the press cell is latched here and the
/// first held-motion sample promotes it to a real selection, so a plain
/// click stays purely the app's while a drag selects.
var term_sel_pending: bool = false;
var term_sel_pending_hit: TermHit = .{ .win_id = 0, .line = 0, .col = 0, .row = 0 };

/// M49 SD5 (#1132): the terminal window under the pointer and the grid cell
/// (absolute line + column) it maps to, or null. Searches topmost first.
/// M73l: pub — the geometry gate calls it directly.
pub const TermHit = struct { win_id: u8, line: usize, col: usize, row: usize };

pub fn terminalHitAt(px: u32, py: u32) ?TermHit {
    var wi: usize = da.win_count;
    while (wi > 0) {
        wi -= 1;
        const w = &da.windows[wi];
        if (w.kind != .user or !w.visible) continue;
        const top_y = w.y + @as(u32, @intCast(user_title_h));
        if (px < w.x or px >= w.x + w.w) continue;
        if (py < top_y or py >= w.y + w.h) continue;
        const scr = terminal.screenForWindow(w.id) orelse continue;
        const rows_visible: usize = if (w.h > user_title_h) ((w.h - user_title_h) / font_metrics.cell_h) else 1;
        // M73k (#1637): `lineCount` is grid+history as one space — the
        // mirror of the painter's `lineCount - view - rows` walk, so a
        // click lands on the same row it paints (selection lives in
        // unified indices too).
        const total = scr.lineCount();
        const first: usize = if (total > scr.view + rows_visible) total - scr.view - rows_visible else 0;
        const row: usize = (py - top_y) / font_metrics.cell_h;
        const col: usize = (px - w.x) / font_metrics.cell_w;
        return .{ .win_id = w.id, .line = first + row, .col = col, .row = row };
    }
    return null;
}

/// M73i (#1635): does mouse tracking own this pointer position? Plain
/// (no Shift held) AND over a bound window with `?1000/1002/1003` on.
/// Every decision — report vs selection — asks this at ITS OWN coordinates:
/// the press latch and the cursor can disagree for one tick, and a per-tick
/// "did I just report?" flag cannot arbitrate between them. Shift — or
/// modes off — always leaves the pointer local (selection stays exact).
/// #2869: this answers the REPORT half only. The selection half asks the
/// armed SET: `?1002`/`?1003` own the whole gesture; `?1000` alone leaves
/// the drag to selection (content_pointer_pass).
/// #2896: `mods` is the modifier byte of the SAMPLE BEING JUDGED, not a
/// live read — the raw path passes report().kb_mods (synchronous, so live
/// IS capture), the seat-forwarded path passes the snapshot stamped on the
/// kind-19 event at fan time (drain-time reads misattributed a concurrent
/// chord's Shift to an older press — go-dogfood boot 08's stolen drag).
fn mouseOwnsAt(px: u32, py: u32, mods: u8) bool {
    if ((mods & 0x22) != 0) return false; // L/R shift held
    const mh = terminalHitAt(px, py) orelse return false;
    const s = terminal.screenForWindow(mh.win_id) orelse return false;
    return s.mouse_1000 or s.mouse_1002 or s.mouse_1003;
}

/// Map raw pointer buttons bitmask to ADR 0009 button flags.
pub fn mouse_buttons_to_flags(buttons: u8) u16 {
    var flags: u16 = 0;
    if ((buttons & 0x01) != 0) flags |= events.BTN_LEFT;
    if ((buttons & 0x02) != 0) flags |= events.BTN_RIGHT;
    if ((buttons & 0x04) != 0) flags |= events.BTN_MIDDLE;
    return flags;
}

// #1688 (issue #1688): the CONTENT half of one pointer sample — mouse-
// tracking report vs local text selection — with explicit coordinates and
// explicit edge flags. Called twice in the system: from pointer_tick with
// the raw virtio stream's edges (shim path — the pre-#1688 structure,
// WMS5's no-compositing end-state untouched), and from wm_content_pointer
// below with the registered seat's forwarded sample (slot-65 cmd 15).
// mouseOwnsAt arbitrates at the sample's own coordinates exactly as the
// local path always has; the selection guards lost only their redundant
// `!mouseOwnsAt` conjunct, which the else makes implicit.
// #2869: ownership is per mode-set, not per mode-presence. `?1002`/`?1003`
// subscribed to the drag itself, so they own the whole gesture end to end
// (Shift still overrides inside mouseOwnsAt). `?1000` alone subscribed to
// press/release/wheel edges only — those still report to the app — but
// `.drag`/`.motion` report NOTHING under it (the terminal.mouseReports
// truth table), so a plain drag was never the app's event and belongs to
// local selection: the press latches a pending cell and the first
// held-motion sample promotes it, keeping a click purely the app's.
fn content_pointer_pass(
    nx: u32,
    ny: u32,
    moved: bool,
    cur_left: bool,
    cur_right: bool,
    left_pressed: bool,
    right_pressed: bool,
    left_released: bool,
    right_released: bool,
    press_handled: bool,
    mods: u8,
) void {
    const owns = mouseOwnsAt(nx, ny, mods);
    var motion_owned = false;
    if (owns) {
        if (terminalHitAt(nx, ny)) |mh| {
            if (terminal.screenForWindow(mh.win_id)) |mscr| {
                motion_owned = mscr.mouse_1002 or mscr.mouse_1003;
                if (terminal.windowTerminal(mh.win_id)) |mtt| {
                    var kind: ?terminal.MouseKind = null;
                    var btn: u8 = 0;
                    if (left_pressed) {
                        kind = .press;
                        btn = 0;
                    } else if (right_pressed) {
                        kind = .press;
                        btn = 2;
                    } else if (left_released) {
                        kind = .release;
                        btn = 0;
                    } else if (right_released) {
                        kind = .release;
                        btn = 2;
                    } else if (moved and (cur_left or cur_right)) {
                        kind = .drag;
                        btn = if (cur_left) 0 else 2;
                    } else if (moved) {
                        kind = .motion;
                        btn = 0;
                    }
                    if (kind) |k| {
                        if (terminal.mouseReports(
                            mscr.mouse_1000,
                            mscr.mouse_1002,
                            mscr.mouse_1003,
                            k,
                        )) {
                            var enc: [24]u8 = undefined;
                            const n = terminal.encodeMouse(
                                mscr.mouse_1006,
                                k,
                                btn,
                                @intCast(mh.col + 1),
                                @intCast(mh.row + 1),
                                @intCast(mscr.cols),
                                &enc,
                            );
                            if (n > 0) {
                                if (k == .motion or k == .drag) {
                                    _ = mtt.stageMouseMotion(enc[0..n]);
                                } else {
                                    _ = mtt.pushMouse(enc[0..n]);
                                }
                            }
                        }
                    }
                }
            }
        }
    } else {
        if (left_pressed and !press_handled) {
            if (terminalHitAt(nx, ny)) |hit| {
                if (terminal.screenForWindow(hit.win_id)) |scr| {
                    scr.beginSelection(hit.line, hit.col);
                    term_sel_dragging = true;
                    klog.line("dui: term sel begin\n");
                }
            }
        } else if (cur_left and term_sel_dragging) {
            if (terminalHitAt(nx, ny)) |hit| {
                if (terminal.screenForWindow(hit.win_id)) |scr| {
                    scr.extendSelection(hit.line, hit.col);
                }
            }
        }
    }
    if (owns and !motion_owned) {
        // `?1000` alone: edges reported above; selection defers past the
        // click. Promotion runs before the latch so the press sample's own
        // `moved` cannot promote it — only a later held move does.
        if (term_sel_pending and cur_left and moved) {
            if (terminal.screenForWindow(term_sel_pending_hit.win_id)) |scr| {
                scr.beginSelection(term_sel_pending_hit.line, term_sel_pending_hit.col);
                term_sel_dragging = true;
                klog.line("dui: term sel begin\n");
            }
            term_sel_pending = false;
        }
        if (left_pressed and !press_handled) {
            term_sel_pending = false;
            if (terminalHitAt(nx, ny)) |hit| {
                term_sel_pending = true;
                term_sel_pending_hit = hit;
            }
        }
        if (cur_left and term_sel_dragging) {
            if (terminalHitAt(nx, ny)) |hit| {
                if (terminal.screenForWindow(hit.win_id)) |scr| {
                    scr.extendSelection(hit.line, hit.col);
                }
            }
        }
    }
    if (left_released) {
        term_sel_pending = false;
        if (term_sel_dragging) {
            // Land the final endpoint on the release position, then close
            // the selection (the gate's copy chord waits for this line).
            // Runs under ownership too: a selection that latched keeps its
            // own release, wherever it lands.
            if (terminalHitAt(nx, ny)) |hit| {
                if (terminal.screenForWindow(hit.win_id)) |scr| {
                    scr.extendSelection(hit.line, hit.col);
                }
            }
            term_sel_dragging = false;
            klog.line("dui: term sel end\n");
        }
    }
}

// #1688: the seat-serialized forward stream's own sample history. Press/
// release edges and "moved" derive here exactly as prev_ptr_buttons and
// the stream deltas do for the raw path in pointer_tick, so the content
// pass cannot tell the two apart.
var wm_fwd_prev_buttons: u8 = 0;
var wm_fwd_prev_x: u32 = 0;
var wm_fwd_prev_y: u32 = 0;

// #2878: the window-delivery half of one pointer sample — MOUSE_MOVE /
// MOUSE_DOWN / MOUSE_UP / MOUSE_RIGHT_DOWN / MOUSE_RIGHT_UP to the hit
// user window's owner, in window-local coords. One body for both input
// environments: pointer_tick calls it on the raw stream (shim — the #9228
// delivery, unchanged), and wm_content_pointer calls it on the seat's
// forwarded stream — under wm_owns_input pointer_tick returns before its
// local delivery, so this is the only pointer input a seat-hosted app
// ever sees (previously nothing was pushed there at all: keys+lifecycle
// only). `flags` rides move/down samples; `up_flags` carries the released
// button's own bit — on the release sample the live button state is
// already 0, so flags-from-buttons alone reported flags=0 on every
// MOUSE_UP and no app could identify the released button (#2878 Gap 1:
// gosol's BtnLeft-gated up()/drop was unreachable).
fn deliver_window_pointer(
    nx: u32,
    ny: u32,
    moved: bool,
    left_pressed: bool,
    left_released: bool,
    right_pressed: bool,
    right_released: bool,
    flags: u16,
    up_flags: u16,
) void {
    const hit_id = hit_test(nx, ny) orelse return;
    const w = find_user_window(hit_id) orelse return;
    const owner_pid = w.owner orelse return;
    const local_x: u32 = if (nx >= w.x) @min(nx - w.x, w.w - 1) else 0;
    const local_y: u32 = if (ny >= w.y) @min(ny - w.y, w.h - 1) else 0;
    if (moved) {
        events.push(owner_pid, .{
            .kind = events.MOUSE_MOVE,
            .flags = flags,
            .seq = 0,
            .arg0 = local_x,
            .arg1 = local_y,
        });
    }
    if (left_pressed) {
        events.push(owner_pid, .{
            .kind = events.MOUSE_DOWN,
            .flags = flags,
            .seq = 0,
            .arg0 = local_x,
            .arg1 = local_y,
        });
    }
    if (left_released) {
        events.push(owner_pid, .{
            .kind = events.MOUSE_UP,
            .flags = up_flags,
            .seq = 0,
            .arg0 = local_x,
            .arg1 = local_y,
        });
    }
    if (right_pressed) {
        events.push(owner_pid, .{
            .kind = events.MOUSE_RIGHT_DOWN,
            .flags = flags,
            .seq = 0,
            .arg0 = local_x,
            .arg1 = local_y,
        });
    }
    if (right_released) {
        events.push(owner_pid, .{
            .kind = events.MOUSE_RIGHT_UP,
            .flags = up_flags,
            .seq = 0,
            .arg0 = local_x,
            .arg1 = local_y,
        });
    }
}

/// #1688 (slot-65 cmd 15, wm_server.wmctl_content_ptr): the registered
/// seat forwarded one pointer sample for the kernel's LOCAL content path —
/// terminal text selection and mouse-tracking reports. Chrome the seat
/// consumed (start surface, rail, launcher) is never forwarded, so this
/// entry only ever sees content coordinates. Dormant with no seat: kernel
/// terminal selection stays exactly where WMS5 left it.
/// #2878 (Gap 2): the forwarded sample now ALSO delivers window pointer
/// events to the hit user window — under the seat this is a hosted app's
/// whole pointer input.
/// #2896: `mods` is the sample's own modifier snapshot — stamped on the
/// kind-19 event at the kernel's fan (decode) time and echoed back by the
/// seat, so the report-vs-selection judgment and the delivered flags both
/// reflect what was held AT CAPTURE, never the drain-time kb_mods a
/// concurrent chord burst may have since changed.
pub fn wm_content_pointer(x: u32, y: u32, buttons: u8, mods: u8) void {
    if (!da.wm_owns_input) return;
    const prev_buttons = wm_fwd_prev_buttons;
    const cur_left = (buttons & 0x01) != 0;
    const cur_right = (buttons & 0x02) != 0;
    const prev_left = (prev_buttons & 0x01) != 0;
    const prev_right = (prev_buttons & 0x02) != 0;
    const moved = (x != wm_fwd_prev_x or y != wm_fwd_prev_y);
    wm_fwd_prev_buttons = buttons;
    wm_fwd_prev_x = x;
    wm_fwd_prev_y = y;
    content_pointer_pass(
        x,
        y,
        moved,
        cur_left,
        cur_right,
        (!prev_left and cur_left),
        (!prev_right and cur_right),
        (prev_left and !cur_left),
        (prev_right and !cur_right),
        false, // the seat consumed chrome; no local press latch applies
        mods,
    );
    const flags = mouse_buttons_to_flags(buttons) |
        input.hid_modifiers_to_flags(mods);
    deliver_window_pointer(
        x,
        y,
        moved,
        (!prev_left and cur_left),
        (prev_left and !cur_left),
        (!prev_right and cur_right),
        (prev_right and !cur_right),
        flags,
        flags | mouse_buttons_to_flags(prev_buttons & ~buttons),
    );
}

/// Card U4 (claim 4993): the pointer tick — consume the pointer state from
/// the input path (motion + click edges). Returns the newly focused window
/// id when a CLICK landed on a window (D4: click = focus + raise), null
/// otherwise. Called from the shell idle loop; a no-op when the input path
/// is unarmed (the default VM).
/// Card E3 (claim 9228): converts absolute pointer motion and button clicks
/// within a user window to window-local coordinates and queues MOUSE_DOWN,
/// MOUSE_UP, and MOUSE_MOVE events to the hit user window's owning process.
pub fn pointer_tick(st: input.PointerState, click: ?input.Click) ?u8 {
    if (!da.armed_global) return null;
    var focused_changed: ?u8 = null;
    if (st.valid) {
        const nx = map_pointer_axis(st.x, virtio_gpu.fb_width);
        const ny = map_pointer_axis(st.y, virtio_gpu.fb_height);
        const moved = (!da.cursor_shown or nx != da.cursor_x or ny != da.cursor_y);
        if (moved) {
            if (da.cursor_shown and da.cursor_old_damage == null)
                da.cursor_old_damage = cursor_rect(da.cursor_x, da.cursor_y, virtio_gpu.fb_width, virtio_gpu.fb_height);
            da.cursor_x = nx;
            da.cursor_y = ny;
            da.cursor_shown = true;
            da.cursor_new_damage = cursor_rect(nx, ny, virtio_gpu.fb_width, virtio_gpu.fb_height);
            // M27 G6: clear tooltip on mouse move. M32 WMS6 Gate D (issue
            // #626): while a WM owns input the tooltip policy lives in the WM
            // — it receives the move (kind 19) and decides when to hide
            // (TOOLTIP cmd 8), so the kernel's blanket move-clear must NOT
            // fight its decisions (it gates behind !wm_owns_input like the
            // other shim decisions).
            if (!da.wm_owns_input) tooltip_clear();
            _ = mark_dirty(0); // the cursor moves over a full repaint
        }

        // M32 WMS5 (issue #625): the input-seam handover — while a WM is
        // registered it owns pointer GEOMETRY. The kernel keeps tracking the
        // cursor (a blit surface) but fans the raw stream out and consumes
        // nothing: no drag, no resize, no snap, no focus-at, no minimize/
        // close buttons. The WM hit-tests and issues SET_WINDOW rects; the
        // kernel clamps + blits those. Zero regression: shim mode (the
        // default) runs the full geometry block below byte-identically.
        if (da.wm_owns_input) {
            // Deliver the sample when the pointer state CHANGED — motion OR
            // a button edge (a release with no motion must still be seen).
            // wm_fan_pointer owns the dedup (shared with the per-report
            // decode fan): the tick is now only the residual fan for
            // reports that never saw decode (the USB path still samples
            // here; a cv report decode already fanned is a no-op repeat).
            wm_fan_pointer(st.x, st.y, st.buttons);
            da.prev_ptr_buttons = st.buttons;
            return null; // no kernel geometry decision while the WM owns input
        }

        const kb_flags = input.hid_modifiers_to_flags(input.report().kb_mods);
        const btn_flags = mouse_buttons_to_flags(st.buttons);
        const all_flags = btn_flags | kb_flags;

        const left_bit: u8 = 0x01;
        const right_bit: u8 = 0x02;
        const prev_left = (da.prev_ptr_buttons & left_bit) != 0;
        const prev_right = (da.prev_ptr_buttons & right_bit) != 0;
        const cur_left = (st.buttons & left_bit) != 0;
        const cur_right = (st.buttons & right_bit) != 0;
        const left_pressed = (!prev_left and cur_left) or (click != null);
        const left_released = (prev_left and !cur_left);
        const right_pressed = (!prev_right and cur_right);
        const right_released = (prev_right and !cur_right);

        // M49 SD5 (#1132): pointer text selection in a terminal-bound
        // window's client area. Selection is model-only here (terminal.zig);
        // Ctrl+Shift+C copies it (input.zig). The click below still focuses
        // the window. The press latch survives a whole injected drag being
        // drained in one pass (last-write-wins state would lose the down).
        // M73i (#1635): mouse tracking owns a plain pointer over a bound
        // window — the app gets the report, selection stays local, and
        // Shift (or modes off) hands everything back to the local path.
        // #1688: the report-or-selection decision itself lives in
        // content_pointer_pass below — one body, called from here with the
        // raw stream's edges and from wm_content_pointer with the registered
        // seat's forwarded sample — so the two paths cannot diverge.
        var press_handled = false;
        if (input.take_press()) |p| {
            const px = map_pointer_axis(p.x, virtio_gpu.fb_width);
            const py = map_pointer_axis(p.y, virtio_gpu.fb_height);
            if (mouseOwnsAt(px, py, input.report().kb_mods)) {
                // The latched press belongs to the app: consume it, suppress
                // the stale-cursor begin below, let the report path deliver.
                // #2869: `?1000` alone also latches the pending cell — the
                // press reported, but a following held move promotes it to
                // a selection (a click never does).
                press_handled = true;
                term_sel_pending = false;
                if (terminalHitAt(px, py)) |hit| {
                    if (terminal.screenForWindow(hit.win_id)) |scr| {
                        if (!scr.mouse_1002 and !scr.mouse_1003) {
                            term_sel_pending = true;
                            term_sel_pending_hit = hit;
                        }
                    }
                }
            } else if (terminalHitAt(px, py)) |hit| {
                if (terminal.screenForWindow(hit.win_id)) |scr| {
                    scr.beginSelection(hit.line, hit.col);
                    press_handled = true;
                    if (cur_left) {
                        // A held drag: keep extending on later samples.
                        term_sel_dragging = true;
                        if (terminalHitAt(da.cursor_x, da.cursor_y)) |end| {
                            if (end.win_id == hit.win_id) scr.extendSelection(end.line, end.col);
                        }
                        klog.line("dui: term sel begin\n");
                    } else {
                        // The whole injected drag drained in one pass: the
                        // current sample IS the end.
                        if (terminalHitAt(da.cursor_x, da.cursor_y)) |end| {
                            if (end.win_id == hit.win_id) scr.extendSelection(end.line, end.col);
                        }
                        klog.line("dui: term sel end\n");
                    }
                }
            }
        }
        content_pointer_pass(
            da.cursor_x,
            da.cursor_y,
            moved,
            cur_left,
            cur_right,
            left_pressed,
            right_pressed,
            left_released,
            right_released,
            press_handled,
            input.report().kb_mods,
        );

        // Step 5/6/7: drag + close + minimize handling on MOUSE_DOWN (left only).
        // M15 C4: dock handling must precede user windows — dock is at 0,0,24,700.
        if (left_pressed) {
            var handled_btn = false;
            // Arc4 #242: the unsaved-dialog click intercept is DELETED (M32
            // WMS8 Gate 4, issue #628) — the WM owns the unsaved-dialog
            // decision via slot-65 DIALOG (cmd 11); with a WM registered
            // pointer_tick returns early above, and without one the dialog
            // can never open (the close dirty-check is deleted too).
            // M21 W15: modal windows block input to windows below.
            // Clicks outside the modal are ignored (except to dismiss).
            if (!handled_btn and modal_active()) {
                if (topmost_modal_id()) |modal_id| {
                    if (find_user_window(modal_id)) |mw| {
                        if (da.cursor_x >= mw.x and da.cursor_x < mw.x + mw.w and
                            da.cursor_y >= mw.y and da.cursor_y < mw.y + mw.h)
                        {
                            // Click inside modal — process normally below.
                        } else {
                            // Click outside modal — dismiss transient popups.
                            if (mw.transient) {
                                _ = user_close(modal_id);
                                handled_btn = true;
                            } else {
                                // Non-transient modal: ignore click.
                                handled_btn = true;
                            }
                        }
                    }
                }
            }
            // M32 WMS8 Gate 7 (issue #628): the kernel's dock-click, tray-click,
            // and notification-panel click DECISION blocks are deleted — WMS6
            // Gates B and D proved the WM owns those decisions (kind-19 ->
            // NOTIF_CENTER / NOTIF_DISMISS / DOCK) with parity gates green, so
            // the !wm_owns_input-gated shim copies were dormant whenever a WM is
            // registered. The applied primitives stay (slot-65 cmds 6/7/9 + the
            // dui monitor commands drive them; the panel/blit rendering is
            // unchanged). Shim end-state (intended): with no WM registered,
            // dock/tray/panel clicks do nothing — the issue's "no compositing
            // policy" end-state.
            // M27 G2: the about-dialog close-button hit-test is DELETED (M32
            // WMS8 Gate 3, issue #628) — the WM owns the about dialog via
            // slot-65 DIALOG (cmd 11); with a WM registered pointer_tick returns
            // early above, and without one about_dialog_open can never be set,
            // so the click path was provably dead.
            if (!handled_btn) {
                // Check close/minimize buttons on user windows first.
                var wi: usize = da.win_count;
                while (wi > 0) {
                    wi -= 1;
                    const w = &da.windows[wi];
                    if (w.kind != .user or !w.visible) continue;
                    // Close button: top-right corner (x + w.w - 14, y + 4, 8x8).
                    if (da.cursor_x >= w.x + w.w - 16 and da.cursor_x < w.x + w.w - 4 and
                        da.cursor_y >= w.y and da.cursor_y < w.y + user_title_h)
                    {
                        // Arc4 #242 / M32 WMS8 Gate 4 (issue #628): the dirty-
                        // check is DELETED — the WM owns the unsaved-dialog
                        // decision (cmd-11 DIALOG actions 3-6); in shim mode
                        // closing a dirty window closes it immediately (the
                        // issue's "no compositing policy" end-state).
                        _ = user_close(w.id);
                        handled_btn = true;
                        break;
                    }
                    // Minimize button: left of close (x + w.w - 26, y + 4, 8x8).
                    if (da.cursor_x >= w.x + w.w - 28 and da.cursor_x < w.x + w.w - 16 and
                        da.cursor_y >= w.y and da.cursor_y < w.y + user_title_h)
                    {
                        _ = user_set_visible(w.id, false);
                        handled_btn = true;
                        break;
                    }
                    // Arc2 W1: resize handle — 6×6 bottom-right corner.
                    if (is_resize_hit(w.*, da.cursor_x, da.cursor_y)) {
                        da.resize_id = w.id;
                        da.resize_start_x = da.cursor_x;
                        da.resize_start_y = da.cursor_y;
                        da.resize_origin_w = w.w;
                        da.resize_origin_h = w.h;
                        _ = focus(w.id);
                        _ = raise(w.id);
                        _ = mark_dirty(0);
                        focused_changed = w.id;
                        handled_btn = true;
                        break;
                    }
                    // M32 WMS8 Gate 6 (issue #628): the title-bar DRAG-initiation
                    // block is DELETED — WMS5 proved the WM owns title-bar drag
                    // (kind 19 -> SET_WINDOW -> user_move). With a WM registered
                    // pointer_tick returns early, so this was dormant. A title-bar
                    // click below simply falls through to focus-at (focusing the
                    // window) — no drag is started by the kernel shim.
                }
            }
            // Fall through to normal focus-at if no button/title bar was hit.
            if (!handled_btn) {
                if (focus_at(da.cursor_x, da.cursor_y)) {
                    const id = da.focused_id;
                    _ = raise(id);
                    _ = mark_dirty(0);
                    focused_changed = id;
                }
            }
        }
        // Arc2 W2: right-click — focus the hit window (no drag/resize/close).
        if (right_pressed) {
            if (focus_at(da.cursor_x, da.cursor_y)) {
                const id = da.focused_id;
                _ = raise(id);
                _ = mark_dirty(0);
                focused_changed = id;
            }
        }

        // Arc2 W1: resize drag — live clamp + chrome repaint + WIN_RESIZE.
        if (da.resize_id) |rid| {
            if (moved and cur_left) {
                const dx = @as(i32, @intCast(da.cursor_x)) - @as(i32, @intCast(da.resize_start_x));
                const dy = @as(i32, @intCast(da.cursor_y)) - @as(i32, @intCast(da.resize_start_y));
                const req_w = @as(i32, @intCast(da.resize_origin_w)) + dx;
                const req_h = @as(i32, @intCast(da.resize_origin_h)) + dy;
                const win = find_user_window(rid) orelse null;
                if (win) |w| {
                    const new_w = clamp_resize_w(req_w, w.x);
                    const new_h = clamp_resize_h(req_h, w.y);
                    if (new_w != w.w or new_h != w.h) {
                        _ = user_resize(rid, new_w, new_h);
                    }
                }
            }
            if (left_released) {
                da.resize_id = null;
            }
        } else if (moved and !cur_left and !cur_right and da.focus_follows_mouse) {
            // M27 G13: Focus-follows-mouse
            if (focus_at(da.cursor_x, da.cursor_y)) {
                const id = da.focused_id;
                _ = raise(id);
                _ = mark_dirty(0);
                focused_changed = id;
            }
        }

        // Arc4 #237: drag-and-drop — detect window crossing during active drag.
        if (da.drag_active and moved) {
            const new_hit = hit_test(da.cursor_x, da.cursor_y);
            const new_id: ?u8 = if (new_hit) |hid| blk: {
                if (find_user_window(hid)) |w| {
                    if (w.owner) |_| break :blk hid;
                }
                break :blk null;
            } else null;
            if (new_id != da.drag_over_id) {
                // DRAG_LEAVE to old window.
                if (da.drag_over_id) |old_id| {
                    if (find_user_window(old_id)) |ow| {
                        if (ow.owner) |old_owner| {
                            events.push(old_owner, .{
                                .kind = events.DRAG_LEAVE,
                                .flags = 0,
                                .seq = 0,
                                .arg0 = 0,
                                .arg1 = @intCast(da.drag_source_pid),
                            });
                        }
                    }
                }
                // DRAG_ENTER to new window.
                if (new_id) |nid| {
                    if (find_user_window(nid)) |nw| {
                        if (nw.owner) |new_owner| {
                            events.push(new_owner, .{
                                .kind = events.DRAG_ENTER,
                                .flags = 0,
                                .seq = 0,
                                .arg0 = @intCast(da.drag_payload_len),
                                .arg1 = @intCast(da.drag_source_pid),
                            });
                        }
                    }
                }
                da.drag_over_id = new_id;
            }
        }
        // Arc4 #237: DROP on left release during active drag.
        // Two-step: DROP event signals payload available (arg0=size),
        // then the target calls sys_drag_read to copy the payload.
        if (da.drag_active and left_released) {
            if (hit_test(da.cursor_x, da.cursor_y)) |drop_id| {
                if (find_user_window(drop_id)) |dw| {
                    if (dw.owner) |drop_owner| {
                        events.push(drop_owner, .{
                            .kind = events.DROP,
                            .flags = 0,
                            .seq = 0,
                            .arg0 = @intCast(da.drag_payload_len),
                            .arg1 = @intCast(da.drag_source_pid),
                        });
                    }
                }
            }
            // Don't clear drag_active yet — sys_drag_read needs the payload.
            // Clear after the target reads or on a new sys_drag_start.
        }

        // Deliver pointer events to hit user window. #2878 (Gap 1): the up
        // events also carry the released button's own bit — on the release
        // sample st.buttons is already 0, so all_flags alone made every
        // MOUSE_UP flags=0 (gosol's BtnLeft-gated up()/drop was dead code).
        const up_flags = all_flags |
            mouse_buttons_to_flags(da.prev_ptr_buttons & ~st.buttons);
        deliver_window_pointer(
            da.cursor_x,
            da.cursor_y,
            moved,
            left_pressed,
            left_released,
            right_pressed,
            right_released,
            all_flags,
            up_flags,
        );

        da.prev_ptr_buttons = st.buttons;
    }
    return focused_changed;
}

/// HID absolute pointers report 0..32767 logical; map onto the framebuffer
/// axis. Pure — host-testable.
pub fn map_pointer_axis(v: u16, span: u32) u32 {
    const scaled = (@as(u32, v) * span + 16384) / 32768;
    return if (scaled >= span) span - 1 else scaled;
}

/// Card U4: the cursor's current framebuffer cell (for reports + tests).
pub fn cursor_pos() ?struct { x: u32, y: u32 } {
    if (!da.cursor_shown) return null;
    return .{ .x = da.cursor_x, .y = da.cursor_y };
}

pub fn cursor_rect(x: u32, y: u32, width: u32, height: u32) ?CursorRect {
    if (width == 0 or height == 0) return null;
    const left: i64 = @as(i64, x) - cursor_hotspot.x;
    const top: i64 = @as(i64, y) - cursor_hotspot.y;
    const x0 = @max(left, 0);
    const y0 = @max(top, 0);
    const x1 = @min(left + cursor_w, width);
    const y1 = @min(top + cursor_h, height);
    if (x1 <= x0 or y1 <= y0) return null;
    return .{ .x = @intCast(x0), .y = @intCast(y0), .w = @intCast(x1 - x0), .h = @intCast(y1 - y0) };
}

pub fn cursor_pixel(x: usize, y: usize, resizing: bool) ?u32 {
    if (y >= cursor_mask.len or x >= cursor_w) return null;
    return switch (cursor_mask[y][x]) {
        '#' => cursor_outline_rgb,
        'o' => if (resizing) 0xffaa00 else cursor_rgb,
        else => null,
    };
}

/// Save only opaque mask pixels. A seat frame gets the cursor LAST, and its
/// shared compose target is restored immediately after transfer/flush so the
/// seat never reads the cursor into its own chrome/back buffers.
pub fn cursor_draw() void {
    if (!da.cursor_shown) return;
    cursor_restore();
    const fb = &virtio_gpu.gpu_fb;
    for (0..cursor_h) |my| {
        for (0..cursor_w) |mx| {
            const rgb = cursor_pixel(mx, my, da.resize_id != null) orelse continue;
            const x = @as(i64, da.cursor_x) - cursor_hotspot.x + @as(i64, @intCast(mx));
            const y = @as(i64, da.cursor_y) - cursor_hotspot.y + @as(i64, @intCast(my));
            if (x < 0 or y < 0 or x >= virtio_gpu.fb_width or y >= virtio_gpu.fb_height) continue;
            const at: usize = @intCast((y * virtio_gpu.fb_width + x) * 4);
            cursor_saved[my * cursor_w + mx] = std.mem.readInt(u32, fb[at..][0..4], .little);
            put_px(fb, virtio_gpu.fb_width * 4, @intCast(x), @intCast(y), rgb);
        }
    }
    cursor_drawn_x = da.cursor_x;
    cursor_drawn_y = da.cursor_y;
    cursor_drawn_resize = da.resize_id != null;
    cursor_drawn = true;
}

pub fn cursor_restore() void {
    if (!cursor_drawn) return;
    const fb = &virtio_gpu.gpu_fb;
    for (0..cursor_h) |my| {
        for (0..cursor_w) |mx| {
            const rgb = cursor_pixel(mx, my, cursor_drawn_resize) orelse continue;
            const x = @as(i64, cursor_drawn_x) - cursor_hotspot.x + @as(i64, @intCast(mx));
            const y = @as(i64, cursor_drawn_y) - cursor_hotspot.y + @as(i64, @intCast(my));
            if (x < 0 or y < 0 or x >= virtio_gpu.fb_width or y >= virtio_gpu.fb_height) continue;
            const at: usize = @intCast((y * virtio_gpu.fb_width + x) * 4);
            // Do not restore stale underlay over a newer seat/application store.
            if (std.mem.readInt(u32, fb[at..][0..4], .little) == (rgb | 0xff000000))
                std.mem.writeInt(u32, fb[at..][0..4], cursor_saved[my * cursor_w + mx], .little);
        }
    }
    cursor_drawn = false;
}

pub fn cursor_damage_clear() void {
    da.cursor_old_damage = null;
    da.cursor_new_damage = null;
}

// #1688: line-hook capture for the forward's marker pins — the splash
// capture pattern, but APPENDING: the begin and end lines must coexist in
// one buffer to be pinned together.
var wm_fwd_test_log: [512]u8 = undefined;
var wm_fwd_test_log_len: usize = 0;

fn wm_fwd_test_capture(bytes: []const u8) void {
    const n = @min(bytes.len, wm_fwd_test_log.len - wm_fwd_test_log_len);
    @memcpy(wm_fwd_test_log[wm_fwd_test_log_len..][0..n], bytes[0..n]);
    wm_fwd_test_log_len += n;
}

test "wm content forward: a seat-forwarded drag runs local selection (#1688)" {
    arm();
    da.wm_owns_input = true;
    defer {
        da.wm_owns_input = false;
        wm_fwd_prev_buttons = 0;
        wm_fwd_prev_x = 0;
        wm_fwd_prev_y = 0;
        term_sel_dragging = false;
    }
    wm_fwd_test_log_len = 0;
    klog.line_hook = wm_fwd_test_capture;
    defer klog.line_hook = null;
    const id = switch (user_open(64, 48, 640, 400, 9)) {
        .opened => |i| i,
        else => return error.TestUnexpectedResult,
    };
    defer _ = user_close(id);
    const h = terminal.create(null).?;
    defer terminal.release(h);
    defer terminal.detachWindow(id);
    try std.testing.expect(terminal.get(h).?.attachWindow(id));
    // Press at (68,70): inside the client area (top_y = 48+16 = 64) —
    // row 0, col 0. The seat-derived press edge must latch a drag...
    wm_content_pointer(68, 70, 0x01, 0);
    try std.testing.expect(term_sel_dragging);
    // ...a held sample at (700,140) — row 4, col 79 — extends...
    wm_content_pointer(700, 140, 0x01, 0);
    // ...and the release edge closes it, end marker printed.
    wm_content_pointer(700, 140, 0x00, 0);
    try std.testing.expect(!term_sel_dragging);
    const cap = wm_fwd_test_log[0..wm_fwd_test_log_len];
    try std.testing.expect(std.mem.indexOf(u8, cap, "dui: term sel begin") != null);
    try std.testing.expect(std.mem.indexOf(u8, cap, "dui: term sel end") != null);
}

test "wm content forward: dormant with no seat; chrome never begins selection (#1688)" {
    arm();
    wm_fwd_test_log_len = 0;
    klog.line_hook = wm_fwd_test_capture;
    defer klog.line_hook = null;
    const id = switch (user_open(64, 48, 640, 400, 9)) {
        .opened => |i| i,
        else => return error.TestUnexpectedResult,
    };
    defer _ = user_close(id);
    const h = terminal.create(null).?;
    defer terminal.release(h);
    defer terminal.detachWindow(id);
    try std.testing.expect(terminal.get(h).?.attachWindow(id));
    // (a) no seat: the identical drag is fully dormant — WMS5's state.
    da.wm_owns_input = false;
    wm_content_pointer(68, 70, 0x01, 0);
    wm_content_pointer(700, 140, 0x01, 0);
    wm_content_pointer(700, 140, 0x00, 0);
    try std.testing.expect(!term_sel_dragging);
    // (b) seat on: chrome coordinates are never content — the title band
    // (terminalHitAt skips below top_y) and points outside the window.
    da.wm_owns_input = true;
    defer {
        da.wm_owns_input = false;
        wm_fwd_prev_buttons = 0;
        wm_fwd_prev_x = 0;
        wm_fwd_prev_y = 0;
        term_sel_dragging = false;
    }
    wm_content_pointer(100, 50, 0x01, 0); // title band (48..63)
    wm_content_pointer(100, 50, 0x00, 0);
    wm_content_pointer(10, 10, 0x01, 0); // outside the window (dock corner)
    wm_content_pointer(10, 10, 0x00, 0);
    try std.testing.expect(!term_sel_dragging);
    const cap = wm_fwd_test_log[0..wm_fwd_test_log_len];
    try std.testing.expect(std.mem.indexOf(u8, cap, "dui: term sel") == null);
}

test "wm content forward: ?1000 reports the click, a drag still selects (#2869)" {
    arm();
    // Keyboard modifiers and the forwarded-sample history are module
    // globals — a predecessor test's leftovers (e.g. a still-held Shift)
    // release mouse ownership to the local path before the test starts.
    input.decode_keyboard_report(&.{ 0, 0, 0, 0, 0, 0, 0, 0 });
    wm_fwd_prev_buttons = 0;
    wm_fwd_prev_x = 0;
    wm_fwd_prev_y = 0;
    term_sel_dragging = false;
    term_sel_pending = false;
    da.wm_owns_input = true;
    defer {
        da.wm_owns_input = false;
        wm_fwd_prev_buttons = 0;
        wm_fwd_prev_x = 0;
        wm_fwd_prev_y = 0;
        term_sel_dragging = false;
        term_sel_pending = false;
    }
    wm_fwd_test_log_len = 0;
    klog.line_hook = wm_fwd_test_capture;
    defer klog.line_hook = null;
    const id = switch (user_open(64, 48, 640, 400, 9)) {
        .opened => |i| i,
        else => return error.TestUnexpectedResult,
    };
    defer _ = user_close(id);
    const h = terminal.create(null).?;
    defer terminal.release(h);
    defer terminal.detachWindow(id);
    try std.testing.expect(terminal.get(h).?.attachWindow(id));
    const scr = terminal.screenForWindow(id).?;
    scr.mouse_1000 = true;
    scr.mouse_1006 = true;

    // A click at (68,70) — cell (0,0) -> SGR x=1 y=1 — reports both edges
    // and stays purely the app's: no selection, no pending left behind.
    wm_content_pointer(68, 70, 0x01, 0);
    wm_content_pointer(68, 70, 0x00, 0);
    try std.testing.expect(!term_sel_dragging);
    try std.testing.expect(!term_sel_pending);
    var buf: [128]u8 = undefined;
    const click_n = terminal.windowTerminal(id).?.readInput(&buf);
    try std.testing.expect(std.mem.indexOf(u8, buf[0..click_n], "\x1b[<0;1;1M") != null);
    try std.testing.expect(std.mem.indexOf(u8, buf[0..click_n], "\x1b[<32;1;1m") != null);
    try std.testing.expect(std.mem.indexOf(u8, wm_fwd_test_log[0..wm_fwd_test_log_len], "dui: term sel") == null);

    // A drag: press (same cell), held move to (700,140), release. The
    // press reported, the pending cell promoted on the move, the release
    // still reported — and the selection ran begin/extend/end.
    wm_content_pointer(68, 70, 0x01, 0);
    wm_content_pointer(700, 140, 0x01, 0);
    try std.testing.expect(term_sel_dragging);
    wm_content_pointer(700, 140, 0x00, 0);
    try std.testing.expect(!term_sel_dragging);
    const drag_n = terminal.windowTerminal(id).?.readInput(&buf);
    try std.testing.expect(std.mem.indexOf(u8, buf[0..drag_n], "\x1b[<0;1;1M") != null);
    try std.testing.expect(std.mem.indexOf(u8, buf[0..drag_n], "\x1b[<32;80;5m") != null);
    const cap = wm_fwd_test_log[0..wm_fwd_test_log_len];
    try std.testing.expect(std.mem.indexOf(u8, cap, "dui: term sel begin") != null);
    try std.testing.expect(std.mem.indexOf(u8, cap, "dui: term sel end") != null);
}

test "wm content forward: ?1002 keeps the whole gesture (#2869)" {
    arm();
    input.decode_keyboard_report(&.{ 0, 0, 0, 0, 0, 0, 0, 0 });
    wm_fwd_prev_buttons = 0;
    wm_fwd_prev_x = 0;
    wm_fwd_prev_y = 0;
    term_sel_dragging = false;
    term_sel_pending = false;
    da.wm_owns_input = true;
    defer {
        da.wm_owns_input = false;
        wm_fwd_prev_buttons = 0;
        wm_fwd_prev_x = 0;
        wm_fwd_prev_y = 0;
        term_sel_dragging = false;
        term_sel_pending = false;
    }
    wm_fwd_test_log_len = 0;
    klog.line_hook = wm_fwd_test_capture;
    defer klog.line_hook = null;
    const id = switch (user_open(64, 48, 640, 400, 9)) {
        .opened => |i| i,
        else => return error.TestUnexpectedResult,
    };
    defer _ = user_close(id);
    const h = terminal.create(null).?;
    defer terminal.release(h);
    defer terminal.detachWindow(id);
    try std.testing.expect(terminal.get(h).?.attachWindow(id));
    const scr = terminal.screenForWindow(id).?;
    scr.mouse_1002 = true;
    scr.mouse_1006 = true;

    // Button-motion is subscribed: the identical drag reports press, drag
    // (b=32 at the cursor cell) and release, and selection never engages.
    wm_content_pointer(68, 70, 0x01, 0);
    wm_content_pointer(700, 140, 0x01, 0);
    wm_content_pointer(700, 140, 0x00, 0);
    try std.testing.expect(!term_sel_dragging);
    try std.testing.expect(!term_sel_pending);
    var buf: [256]u8 = undefined;
    const n = terminal.windowTerminal(id).?.readInput(&buf);
    try std.testing.expect(std.mem.indexOf(u8, buf[0..n], "\x1b[<0;1;1M") != null);
    try std.testing.expect(std.mem.indexOf(u8, buf[0..n], "\x1b[<32;80;5M") != null);
    try std.testing.expect(std.mem.indexOf(u8, buf[0..n], "\x1b[<32;80;5m") != null);
    const cap = wm_fwd_test_log[0..wm_fwd_test_log_len];
    try std.testing.expect(std.mem.indexOf(u8, cap, "dui: term sel") == null);
}

test "wm content forward: the press is judged on the sample's own modifiers, not drain-time kb_mods (#2896)" {
    arm();
    input.decode_keyboard_report(&.{ 0, 0, 0, 0, 0, 0, 0, 0 });
    wm_fwd_prev_buttons = 0;
    wm_fwd_prev_x = 0;
    wm_fwd_prev_y = 0;
    term_sel_dragging = false;
    term_sel_pending = false;
    da.wm_owns_input = true;
    defer {
        da.wm_owns_input = false;
        wm_fwd_prev_buttons = 0;
        wm_fwd_prev_x = 0;
        wm_fwd_prev_y = 0;
        term_sel_dragging = false;
        term_sel_pending = false;
        input.decode_keyboard_report(&.{ 0, 0, 0, 0, 0, 0, 0, 0 });
    }
    wm_fwd_test_log_len = 0;
    klog.line_hook = wm_fwd_test_capture;
    defer klog.line_hook = null;
    const id = switch (user_open(64, 48, 640, 400, 9)) {
        .opened => |i| i,
        else => return error.TestUnexpectedResult,
    };
    defer _ = user_close(id);
    const h = terminal.create(null).?;
    defer terminal.release(h);
    defer terminal.detachWindow(id);
    try std.testing.expect(terminal.get(h).?.attachWindow(id));
    const scr = terminal.screenForWindow(id).?;
    scr.mouse_1002 = true;
    scr.mouse_1006 = true;

    // The defect's exact shape: the press was captured unmodified, but the
    // seat's queue trailed a concurrent chord burst, so at seat-DRAIN time
    // kb_mods holds Shift. Judged on the sample's own snapshot (mods=0)
    // the press still reports to the app; the live read it replaced stole
    // the gesture into `dui: term sel` and the ?1002 drag never armed.
    input.decode_keyboard_report(&.{ 0x02, 0, 0, 0, 0, 0, 0, 0 }); // LShift held NOW
    wm_content_pointer(68, 70, 0x01, 0x00); // captured before the chord
    try std.testing.expect(!term_sel_dragging);
    try std.testing.expect(!term_sel_pending);
    var buf: [128]u8 = undefined;
    const press_n = terminal.windowTerminal(id).?.readInput(&buf);
    try std.testing.expect(std.mem.indexOf(u8, buf[0..press_n], "\x1b[<0;1;1M") != null);
    try std.testing.expect(std.mem.indexOf(u8, wm_fwd_test_log[0..wm_fwd_test_log_len], "dui: term sel") == null);
    wm_content_pointer(68, 70, 0x00, 0x00);
    input.decode_keyboard_report(&.{ 0, 0, 0, 0, 0, 0, 0, 0 }); // chord over

    // The contract's other half (#2869): a press captured WITH Shift still
    // takes the escape even though Shift is physically up by drain — the
    // snapshot overrides the subscription the same way the raw path's live
    // read does.
    wm_content_pointer(68, 70, 0x01, 0x02);
    try std.testing.expect(term_sel_dragging);
    wm_content_pointer(700, 140, 0x01, 0x02);
    wm_content_pointer(700, 140, 0x00, 0x02);
    try std.testing.expect(!term_sel_dragging);
    const cap = wm_fwd_test_log[0..wm_fwd_test_log_len];
    try std.testing.expect(std.mem.indexOf(u8, cap, "dui: term sel begin") != null);
    try std.testing.expect(std.mem.indexOf(u8, cap, "dui: term sel end") != null);
}

test "wm content forward: delivered window flags carry the sample's snapshot too (#2896)" {
    arm();
    input.decode_keyboard_report(&.{ 0, 0, 0, 0, 0, 0, 0, 0 });
    wm_fwd_prev_buttons = 0;
    wm_fwd_prev_x = 0;
    wm_fwd_prev_y = 0;
    da.wm_owns_input = true;
    defer {
        da.wm_owns_input = false;
        wm_fwd_prev_buttons = 0;
        wm_fwd_prev_x = 0;
        wm_fwd_prev_y = 0;
    }
    const id = switch (user_open(64, 48, 640, 400, 9)) {
        .opened => |i| i,
        else => return error.TestUnexpectedResult,
    };
    defer _ = user_close(id);
    while (events.pop(9) != null) {}

    // Live Shift is NOT held; the forwarded snapshot says it was at
    // capture — the MOUSE_DOWN/MOUSE_UP the hit app receives must carry
    // the same modifiers the judgment used, not a drain-time read.
    wm_content_pointer(100, 100, 0x01, 0x02);
    wm_content_pointer(100, 100, 0x00, 0x02);
    var saw_shift_down = false;
    while (events.pop(9)) |ev| {
        if (ev.kind == events.MOUSE_DOWN) {
            saw_shift_down = ev.flags & events.MOD_SHIFT != 0;
        }
    }
    try std.testing.expect(saw_shift_down);
}

test "pointer_tick: MOUSE_UP carries the released button's flag bit (#2878)" {
    arm();
    const id = switch (user_open(64, 48, 640, 400, 9)) {
        .opened => |i| i,
        else => return error.TestUnexpectedResult,
    };
    defer _ = user_close(id);
    while (events.pop(9) != null) {}
    // Logical coords (0..32767) for scanout (384,248) — inside the window.
    _ = pointer_tick(.{ .x = 9830, .y = 11286, .buttons = 0x01, .valid = true }, null);
    _ = pointer_tick(.{ .x = 9830, .y = 11286, .buttons = 0x00, .valid = true }, null);
    var saw_down = false;
    var saw_up = false;
    while (events.pop(9)) |ev| {
        switch (ev.kind) {
            events.MOUSE_DOWN => {
                saw_down = true;
                try std.testing.expect(ev.flags & events.BTN_LEFT != 0);
            },
            // The release sample's live buttons are already 0; the up
            // event still names the button that came up — the gosol
            // up()/drop gate reads exactly this bit.
            events.MOUSE_UP => {
                saw_up = true;
                try std.testing.expect(ev.flags & events.BTN_LEFT != 0);
            },
            else => {},
        }
    }
    try std.testing.expect(saw_down);
    try std.testing.expect(saw_up);
    // Same contract on the right button.
    _ = pointer_tick(.{ .x = 9830, .y = 11286, .buttons = 0x02, .valid = true }, null);
    _ = pointer_tick(.{ .x = 9830, .y = 11286, .buttons = 0x00, .valid = true }, null);
    var saw_rup = false;
    while (events.pop(9)) |ev| {
        if (ev.kind == events.MOUSE_RIGHT_UP) {
            saw_rup = true;
            try std.testing.expect(ev.flags & events.BTN_RIGHT != 0);
        }
    }
    try std.testing.expect(saw_rup);
}

test "wm content forward: the seat delivers pointer events to the hit window (#2878)" {
    arm();
    da.wm_owns_input = true;
    defer {
        da.wm_owns_input = false;
        wm_fwd_prev_buttons = 0;
        wm_fwd_prev_x = 0;
        wm_fwd_prev_y = 0;
    }
    const id = switch (user_open(64, 48, 640, 400, 9)) {
        .opened => |i| i,
        else => return error.TestUnexpectedResult,
    };
    defer _ = user_close(id);
    while (events.pop(9) != null) {}

    // A seat-forwarded press+move+release inside the window's content:
    // the hosted app's whole pointer input — before #2878 nothing was
    // pushed at all (keys+lifecycle only).
    wm_content_pointer(100, 100, 0x01, 0);
    wm_content_pointer(120, 120, 0x01, 0);
    wm_content_pointer(120, 120, 0x00, 0);

    var kinds: [8]u16 = @splat(0);
    var flags: [8]u16 = @splat(0);
    var args: [8]u64 = @splat(0);
    var n: usize = 0;
    while (events.pop(9)) |ev| {
        if (n < kinds.len) {
            kinds[n] = ev.kind;
            flags[n] = ev.flags;
            args[n] = ev.arg0 | (@as(u64, ev.arg1) << 32);
        }
        n += 1;
    }
    // press sample also reports the move into place: MOVE, DOWN, MOVE, UP.
    try std.testing.expectEqual(@as(usize, 4), n);
    try std.testing.expectEqual(events.MOUSE_MOVE, kinds[0]);
    try std.testing.expectEqual(events.MOUSE_DOWN, kinds[1]);
    try std.testing.expectEqual(events.MOUSE_MOVE, kinds[2]);
    try std.testing.expectEqual(events.MOUSE_UP, kinds[3]);
    try std.testing.expect(flags[1] & events.BTN_LEFT != 0);
    try std.testing.expect(flags[3] & events.BTN_LEFT != 0);
    // Window-local coords: press (100,100) - origin (64,48) = (36,52).
    try std.testing.expectEqual(@as(u64, 36 | (52 << 32)), args[1]);

    // The same stream stays dormant with no seat registered.
    da.wm_owns_input = false;
    wm_content_pointer(100, 100, 0x01, 0);
    wm_content_pointer(100, 100, 0x00, 0);
    try std.testing.expect(events.pop(9) == null);
}
