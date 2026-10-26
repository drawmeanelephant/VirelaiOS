//! The compositor: paint_scene/composite/present plumbing, the presentation trace, drain(), and the M76a splash-to-seat handoff hold.
//!
//! Split out of driving_award.zig (#3754) — shared registry state lives in the
//! module root and is reached through `da`; this file owns the seam's own
//! private state.

const da = @import("../driving_award.zig");
const arm = da.arm;
const armed = da.armed;
const builtin = da.builtin;
const clipboard = da.clipboard;
const clock_accent_rgb = da.clock_accent_rgb;
const clock_fg_rgb = da.clock_fg_rgb;
const cursor_damage_clear = da.cursor_damage_clear;
const cursor_draw = da.cursor_draw;
const cursor_restore = da.cursor_restore;
const draw_chrome = da.draw_chrome;
const draw_glyph = da.draw_glyph;
const draw_string = da.draw_string;
const fade_half_frames = da.fade_half_frames;
const fill_rect = da.fill_rect;
const font = da.font;
const klog = da.klog;
const mark_dirty = da.mark_dirty;
const notify_advance_ticks = da.notify_advance_ticks;
const paint = da.paint;
const repaint_start = da.repaint_start;
const std = da.std;
const transient_advance_tick = da.transient_advance_tick;
const user_close = da.user_close;
const user_open = da.user_open;
const user_present = da.user_present;
const user_surface = da.user_surface;
const virtio_gpu = da.virtio_gpu;
const wallpaper_bot = da.wallpaper_bot;
const wallpaper_top = da.wallpaper_top;
const workspace_visible = da.workspace_visible;
const render_frontend_to_surface = @import("user.zig").render_frontend_to_surface;

// ---------------------------------------------------------------------------
// The compositor
// ---------------------------------------------------------------------------

/// Composite: repaint every visible window from the lowest dirty one UP (so
/// the clock is always re-blitted over a freshly repainted terminal), then
/// push one transfer + flush. Returns the flush result; `.ok` with no work
/// when the scene is clean (dirty-rect: unchanged windows are never
/// repainted).
/// M33 SB5 (claim 7397): the kernel's compositor LAYER — paint chrome +
/// unmigrated windows into the scanout framebuffer WITHOUT a transfer/flush.
/// While a WM is registered, wm_server.on_tick calls this BEFORE pushing the
/// COMPOSITE_TICK, so the kernel layer is UNDER the WM's compose-N stores
/// (correct z-order at flush time). Surface-backed (migrated) user windows
/// are skipped when `wm_owns_user_layer` — the WM owns those pixels. Pure
/// BSS writes: only gpu_transfer's `dc ivac` cache-clean is EL0-illegal, so
/// this half is host-test safe (the established `!builtin.is_test` gate stays
/// on the flush half).
pub fn paint_scene() virtio_gpu.CmdResult {
    if (!da.armed_global) return .not_ready;
    cursor_restore();
    // Arc2 W3: tray clock ticks on composite() without timer — detect theme/
    // clipboard changes that happened since last composite and mark taskbar
    // dirty so the tray repaints even without a tick. Tick-driven dirty is
    // handled in drain(); this covers settings/clipboard writes that occur
    // between ticks.
    {
        const cur_theme = da.theme_id;
        const cur_clip = clipboard.current_len();
        var need = false;
        if (cur_theme != da.tray_last_theme) {
            da.tray_last_theme = cur_theme;
            need = true;
        }
        if (cur_clip != da.tray_last_clip_len) {
            da.tray_last_clip_len = cur_clip;
            need = true;
        }
        if (need) _ = mark_dirty(255);
    }
    // M33 SB5: whether this scene painted anything (drives composite()'s
    // decision to flush — a clean scene is not flushed, pre-SB5 behavior).
    da.scene_dirty = false;
    const start = repaint_start() orelse return .ok;
    da.scene_dirty = true;
    // M71c (#1562): while a registered seat owns the scanout, the kernel
    // composites NONE of its own fixed desktop layer — the full-screen
    // Road Pops terminal window, the wallpaper gradient, the taskbar and
    // the dock. The seat writes its own desktop + chrome straight into the
    // same framebuffer (wm_server.scanout_bind maps gpu_fb_phys), so a
    // kernel blit here would double-draw over it. The tee calls this on
    // every dirty console batch (the monitor's `tasks worker advances=`
    // line keeps it permanently dirty), which is why the kernel terminal
    // used to land ON TOP of the seat: measured 2026-09-21 (#1562), the
    // captured frame held the kernel's console-green text and none of the
    // seat's pixels. In WM mode this layer is painted at COMPOSITE_TICK;
    // only the seat's REQUEST_PRESENT publishes the completed frame.
    const seat_owns_layer = da.wm_owns_user_layer;
    // Step 9: render the wallpaper gradient BEFORE windows so it is the background.
    if (start <= 1 and !seat_owns_layer) {
        // The wallpaper is at index 1 (after tray migration); if it or anything below is dirty, render gradient.
        const fb: [*]u8 = @ptrCast(&virtio_gpu.gpu_fb);
        const stride = virtio_gpu.fb_width * 4;
        var y: u32 = 0;
        while (y < virtio_gpu.fb_height) : (y += 1) {
            const t = y * 256 / virtio_gpu.fb_height;
            const r = ((wallpaper_top() >> 16) & 0xff) * (256 - t) / 256 + ((wallpaper_bot() >> 16) & 0xff) * t / 256;
            const g = ((wallpaper_top() >> 8) & 0xff) * (256 - t) / 256 + ((wallpaper_bot() >> 8) & 0xff) * t / 256;
            const b = (wallpaper_top() & 0xff) * (256 - t) / 256 + (wallpaper_bot() & 0xff) * t / 256;
            const row_color: u32 = (@as(u32, @intCast(r)) << 16) | (@as(u32, @intCast(g)) << 8) | @as(u32, @intCast(b));
            fill_rect(fb, stride, 0, y, virtio_gpu.fb_width, 1, row_color);
        }
    }
    var i = start;
    while (i < da.win_count) : (i += 1) {
        const w = &da.windows[i];
        if (!w.visible) continue;
        // Arc4 #241: skip user windows not in the current workspace.
        if (!workspace_visible(w)) {
            w.dirty = false;
            continue;
        }
        // Wallpaper is rendered via the gradient path above.
        if (w.kind == .wallpaper) {
            w.dirty = false;
            continue;
        }
        // M71c (#1562): the kernel's own fixed desktop layer (its full-screen
        // terminal window, wallpaper, taskbar, dock) is the seat's job while
        // a registered seat owns the scanout — skip the blit, consume the
        // damage. User windows are NOT skipped here: the migrated ones are
        // already gated by `wm_owns_user_layer` above, and an unmigrated
        // kernel-rendered user window (a hosted tty band) still paints.
        if (seat_owns_layer and (w.kind == .terminal or w.kind == .taskbar or w.kind == .dock)) {
            w.dirty = false;
            w.damaged = false;
            continue;
        }
        // M33 SB5 (claim 7397): while the registered WM owns the user layer
        // (scanout bound), surface-backed (migrated) user windows are the
        // WM's compositing job — their bytes are already in the scanout via
        // the WM's compose-N stores (written after this tick's chrome paint,
        // before the final present). Skip the kernel blit entirely; the
        // damage is consumed (the WM composites from the same tick mask).
        if (w.kind == .user and da.wm_owns_user_layer and user_surface(w.id) != null) {
            // M104f: only the SCANOUT BLIT is the seat's job — a window with
            // a kernel-rendered front-end (attached tty) still needs its grid
            // painted into the surface pages, or the seat's compose-N reads
            // zeros. Apps that paint their own surface have no terminal
            // screen and take the pure skip.
            render_frontend_to_surface(w);
            w.dirty = false;
            w.damaged = false;
            // M33 SB6 (claim 6864): the seam-B composite-cost saving — the
            // kernel did NOT blit this migrated window (the WM's compose-N
            // stores are the pixels).
            da.migrated_skips +%= 1;
            continue;
        }
        if (w.kind == .user) {
            // M33 SB6 (claim 6864): the pre-seam-B composite cost — a user
            // window the kernel actually blitted this scene.
            da.user_blits +%= 1;
        }
        paint(w);
        // M33 SB4 (claim 2382): record the rect paint() actually repainted so
        // the gate can observe it after the drain (unless whole-window, in
        // which case damaged was false and we leave the prior last_*).
        if (w.damaged) {
            w.last_dx = w.dx;
            w.last_dy = w.dy;
            w.last_dw = w.dw;
            w.last_dh = w.dh;
        }
        w.dirty = false;
        w.damaged = false; // the tracked damage rect was consumed by paint
    }
    // Arc4 #239: advance fade-in ticks after painting. Each composite
    // frame increments the tick; after 2 × fade_half_frames the fade
    // completes and the window renders at full opacity.
    {
        var fi: usize = 0;
        while (fi < da.win_count) : (fi += 1) {
            const fw = &da.windows[fi];
            if (fw.fade_phase == 1) {
                fw.fade_tick +|= 1;
                if (fw.fade_tick >= fade_half_frames * 2) {
                    fw.fade_phase = 0; // fade complete
                    fw.fade_tick = 0;
                }
                fw.dirty = true; // repaint at new alpha
            }
        }
    }
    // Arc4 #240: advance notification dismiss ticks once per composite.
    notify_advance_ticks();
    // M21 W16: advance transient window timeouts.
    _ = transient_advance_tick();
    draw_chrome();
    if (!da.wm_owns_user_layer) cursor_draw();
    return .ok;
}

/// #3731: whether one composite() call would still change the scanout — a
/// visible window carries unconsumed damage and nothing suppresses the
/// paint (splash hold, seat ownership). The kind-4 snapshot seam holds
/// its stream while this is true so it never ships a pre-composite frame.
pub fn composite_pending() bool {
    if (da.splash_hold or da.wm_owns_user_layer) return false;
    return repaint_start() != null;
}
var trace_seq: u32 = 0;
var trace_busy: bool = false;
const trace_file = @import("../virtio_file.zig");

pub fn presentation_trace_arm() void {
    if (builtin.is_test) return;
    da.presentation_trace = false;
    var consent: [4]u8 = undefined;
    const n = trace_file.read_whole("M91-PRESENTATION.TRACE", &consent) orelse return;
    da.presentation_trace = std.mem.eql(u8, consent[0..n], "v1\n");
    if (da.presentation_trace) klog.line("m91: trace armed guest-only\n");
}

fn trace_write(handle: u16, bytes: []const u8) bool {
    var off: usize = 0;
    while (off < bytes.len) {
        var written: u64 = 0;
        const n = @min(bytes.len - off, trace_file.write_chunk_limit());
        if (trace_file.write(handle, bytes[off..][0..n], &written) != trace_file.st_ok or
            written == 0 or written > n) return false;
        off += @intCast(written);
    }
    return true;
}

/// Lossless runs of (u32 count, u32 BGRX), prefixed with M91F/width/height/seq.
/// Each file is the framebuffer at a transfer boundary, not a delayed sample
/// requested by the host. Capture errors are reported, never treated as frames.
pub fn presentation_begin(owner: []const u8) u32 {
    if (!da.presentation_trace or builtin.is_test) return 0;
    if (trace_busy) {
        klog.line("m91: trace ERROR reentrant\n");
        return 0;
    }
    trace_busy = true;
    defer trace_busy = false;
    trace_seq +%= 1;
    var name_buf: [48]u8 = undefined;
    const path = std.fmt.bufPrint(&name_buf, "M91-FRAME-{d}.rle", .{trace_seq}) catch unreachable;
    var handle: u16 = 0;
    var captured = trace_file.open(path, trace_file.open_flag_create, &handle) == trace_file.st_ok;
    if (captured) {
        defer _ = trace_file.close(handle);
        captured = trace_file.truncate(handle, 0) == trace_file.st_ok;
        const buf = &trace_file.chunk_staging;
        @memcpy(buf[0..4], "M91F");
        std.mem.writeInt(u32, buf[4..8], virtio_gpu.fb_width, .little);
        std.mem.writeInt(u32, buf[8..12], virtio_gpu.fb_height, .little);
        std.mem.writeInt(u32, buf[12..16], trace_seq, .little);
        var used: usize = 16;
        var at: usize = 0;
        const pixels = virtio_gpu.fb_size / 4;
        while (captured and at < pixels) {
            const color = std.mem.readInt(u32, virtio_gpu.gpu_fb[at * 4 ..][0..4], .little);
            var end = at + 1;
            while (end < pixels and std.mem.readInt(u32, virtio_gpu.gpu_fb[end * 4 ..][0..4], .little) == color) : (end += 1) {}
            std.mem.writeInt(u32, buf[used..][0..4], @intCast(end - at), .little);
            std.mem.writeInt(u32, buf[used + 4 ..][0..4], color, .little);
            used += 8;
            if (used + 8 > buf.len) {
                captured = trace_write(handle, buf[0..used]);
                used = 0;
            }
            at = end;
        }
        if (captured) captured = trace_write(handle, buf[0..used]);
    }
    var line_buf: [192]u8 = undefined;
    const t = @import("../timer.zig");
    const ns: u64 = if (t.freq == 0) 0 else @intCast(@as(u128, t.cntpct()) * 1_000_000_000 / t.freq);
    const line = std.fmt.bufPrint(&line_buf, "m91: frame={d} owner={s} seat={d} phase=transfer captured={d} ns={d} cursor={d},{d},{d}\n", .{
        trace_seq,   owner,       @intFromBool(da.wm_owns_user_layer), @intFromBool(captured), ns,
        da.cursor_x, da.cursor_y, @intFromBool(da.cursor_shown),
    }) catch unreachable;
    klog.line(line);
    return trace_seq;
}

pub fn presentation_end(seq: u32, result: virtio_gpu.CmdResult) void {
    if (seq == 0) return;
    var buf: [96]u8 = undefined;
    klog.line(std.fmt.bufPrint(&buf, "m91: frame={d} phase=complete result={s}\n", .{ seq, @tagName(result) }) catch unreachable);
}
pub fn present_completed(owner: []const u8) virtio_gpu.CmdResult {
    const seq = presentation_begin(owner);
    const transfer = if (builtin.is_test)
        (if (da.transfer_hook) |h| h() else virtio_gpu.CmdResult.ok)
    else
        virtio_gpu.gpu_transfer();
    const result = if (transfer != .ok)
        transfer
    else if (builtin.is_test)
        (if (da.flush_hook) |h| h() else virtio_gpu.CmdResult.ok)
    else
        virtio_gpu.gpu_flush();
    presentation_end(seq, result);
    return result;
}

/// The shim composite: paint the scene, then transfer+flush the scanout
/// (the G1 seam). When the scene is clean, nothing is flushed (pre-SB5
/// behavior). The transfer/flush half runs `dc ivac` cache-maintenance asm
/// — EL0-illegal in host test binaries — so the flush half keeps the
/// established `!builtin.is_test` gating at ITS call sites (the WM path
/// flushes via wm_server.request_present, which already gates).
pub fn composite() virtio_gpu.CmdResult {
    // M76a (#1674): while the splash hold stands, refuse the kernel
    // present — no paint, no transfer, no flush, so the splash frame
    // stays on the scanout while boot continues underneath. A seat that
    // binds the scanout paints through its OWN present path (never
    // suppressed); the first kernel present after the bind releases the
    // hold. Once bound, only the seat may publish a completed frame.
    if (da.splash_hold) {
        if (!da.wm_owns_user_layer) return .ok;
        da.splash_hold = false;
    }
    // A console batch or pointer move must not repaint/flush between the
    // kernel's tick-side layer and the seat's chrome stores. Otherwise the
    // intermediate kernel frame flashes over the desktop.
    if (da.wm_owns_user_layer) return .ok;
    if (paint_scene() != .ok) return .not_ready;
    if (!da.scene_dirty) return .ok; // clean scene: no flush
    if (!virtio_gpu.gpu_ready) return .not_ready;
    const result = present_completed("shim");
    if (result == .ok) {
        da.presents += 1;
        cursor_damage_clear();
    }
    return result;
}
/// The timer.ticks value of the first evaluation — the bound's anchor.
var splash_anchor_tick: ?u64 = null;
/// The bound: 1 Hz timer ticks after the anchor before the hold expires
/// honestly. Stated and gated in tools/gate/specs/live-roadpops.spec.
pub const splash_bound_ticks: u64 = 3;

/// The deadline pump, called from drain(ticks) every idle iteration.
/// First evaluation anchors the bound; the expiry prints ONE line.
fn splash_eval(ticks: u64) void {
    if (!da.splash_hold) return;
    if (da.wm_owns_user_layer) {
        da.splash_hold = false; // the seat bound the scanout: silent handoff
        return;
    }
    const anchor = splash_anchor_tick orelse {
        splash_anchor_tick = ticks;
        return;
    };
    if (ticks < anchor + splash_bound_ticks) return;
    da.splash_hold = false;
    var buf: [96]u8 = undefined;
    const line = std.fmt.bufPrint(
        &buf,
        "splash: seat present not observed within {d}s, continuing with Road Pops\n",
        .{splash_bound_ticks},
    ) catch return;
    klog.line(line);
}

// Host tests pin the handoff state machine; the pixel half (the splash
// actually holding the scanout) is gated live by live-roadpops (seatless
// bound) and go-dogfood (happy-path handoff).
var splash_test_log: [192]u8 = undefined;
var splash_test_log_len: usize = 0;
fn splash_test_capture(bytes: []const u8) void {
    splash_test_log_len = @min(bytes.len, splash_test_log.len);
    @memcpy(splash_test_log[0..splash_test_log_len], bytes[0..splash_test_log_len]);
}

test "splash hold: bound expires with one honest line" {
    da.splash_hold = false;
    splash_anchor_tick = null;
    splash_test_log_len = 0;
    // Headless (gpu never armed): render_splash must NOT engage the hold.
    render_splash(1);
    try std.testing.expect(!da.splash_hold);
    klog.line_hook = splash_test_capture;
    defer klog.line_hook = null;
    // Engage, anchor on the first evaluation, hold through bound-1...
    da.splash_hold = true;
    splash_anchor_tick = null;
    splash_eval(10);
    try std.testing.expect(da.splash_hold);
    try std.testing.expect(splash_anchor_tick == 10);
    splash_eval(10 + splash_bound_ticks - 1);
    try std.testing.expect(da.splash_hold);
    // ...and expire exactly at anchor + bound, with the line.
    splash_eval(10 + splash_bound_ticks);
    try std.testing.expect(!da.splash_hold);
    try std.testing.expect(splash_test_log_len > 0);
    try std.testing.expect(std.mem.indexOf(
        u8,
        splash_test_log[0..splash_test_log_len],
        "seat present not observed within 3s",
    ) != null);
}

test "splash hold: a seat that binds releases silently" {
    da.splash_hold = true;
    splash_anchor_tick = null;
    splash_test_log_len = 0;
    klog.line_hook = splash_test_capture;
    defer klog.line_hook = null;
    da.wm_owns_user_layer = true;
    defer da.wm_owns_user_layer = false;
    splash_eval(50);
    try std.testing.expect(!da.splash_hold);
    // The happy path never prints: the seat's own markers carry the chain.
    try std.testing.expect(splash_test_log_len == 0);
}

test "splash hold: a seatless user present releases silently" {
    arm();
    da.splash_hold = true;
    splash_anchor_tick = null;
    splash_test_log_len = 0;
    da.seat_impossible = true;
    defer {
        da.splash_hold = false;
        da.seat_impossible = false;
        splash_anchor_tick = null;
    }
    klog.line_hook = splash_test_capture;
    defer klog.line_hook = null;
    const id = switch (user_open(64, 48, 640, 400, 9)) {
        .opened => |i| i,
        else => return error.TestUnexpectedResult,
    };
    defer _ = user_close(id);
    // #3731: the hold survives a mere open — the first PRESENT is the
    // successor content the shim-only boot was waiting on.
    try std.testing.expect(da.splash_hold);
    // user_open emits its per-open attribution marker (issue #990) — the
    // silence contract covers the hold's own release, so the measurement
    // starts here with the hook still armed.
    splash_test_log_len = 0;
    try std.testing.expect(user_present(id));
    try std.testing.expect(!da.splash_hold);
    // Silent like the seat bind: no expiry line — the app's own markers
    // carry the ordering.
    try std.testing.expect(splash_test_log_len == 0);
}

test "splash hold: a user present keeps holding while a seat may bind" {
    arm();
    da.splash_hold = true;
    splash_anchor_tick = null;
    da.seat_impossible = false;
    defer {
        da.splash_hold = false;
        splash_anchor_tick = null;
    }
    const id = switch (user_open(64, 48, 640, 400, 9)) {
        .opened => |i| i,
        else => return error.TestUnexpectedResult,
    };
    defer _ = user_close(id);
    try std.testing.expect(user_present(id));
    try std.testing.expect(da.splash_hold);
}

test "splash hold: composite refuses kernel presents while held" {
    da.splash_hold = true;
    defer da.splash_hold = false;
    const before = da.presents;
    // Held: .ok WITHOUT painting or flushing — the splash frame survives
    // on the scanout (the gpu flush half never runs: the guard returns
    // before paint_scene, so this is host-test safe with no device).
    try std.testing.expect(composite() == .ok);
    try std.testing.expect(da.presents == before);
}

test "compositor: a bound WM keeps exclusive paint and present ownership" {
    const saved_armed = da.armed_global;
    const saved_owned = da.wm_owns_user_layer;
    const saved_hold = da.splash_hold;
    const saved_dirty = da.scene_dirty;
    defer {
        da.armed_global = saved_armed;
        da.wm_owns_user_layer = saved_owned;
        da.splash_hold = saved_hold;
        da.scene_dirty = saved_dirty;
    }
    da.armed_global = false; // unowned path is .not_ready, with no hardware work
    da.splash_hold = false;
    da.wm_owns_user_layer = true;
    da.scene_dirty = true;
    const before = da.presents;
    try std.testing.expectEqual(virtio_gpu.CmdResult.ok, composite());
    try std.testing.expect(da.scene_dirty); // do not consume the WM's pending paint
    try std.testing.expectEqual(before, da.presents);
    da.splash_hold = true;
    try std.testing.expectEqual(virtio_gpu.CmdResult.ok, composite());
    try std.testing.expect(!da.splash_hold); // the scanout bind still ends the hold
    da.wm_owns_user_layer = false;
    try std.testing.expectEqual(virtio_gpu.CmdResult.not_ready, composite());
}

/// Step 13 (Issue #213): boot splash screen. Renders once into the framebuffer
/// and pushes a transfer+flush. Shows the system name in 8×16 font, version,
/// and a cycling progress indicator. Returns after `max_ticks` or when a
/// keystroke arrives (whichever is first).
pub fn render_splash(max_ticks: u64) void {
    if (!da.armed_global or !virtio_gpu.gpu_ready) return;
    const fb: [*]u8 = @ptrCast(&virtio_gpu.gpu_fb);
    const stride = virtio_gpu.fb_width * 4;
    // Fill background.
    fill_rect(fb, stride, 0, 0, virtio_gpu.fb_width, virtio_gpu.fb_height, wallpaper_top());
    // Title: "VirelaiOS" centered, using 8×16 font (the draw_glyph path,
    // stretched to 2× height).
    const title = "VirelaiOS";
    const title_x: usize = (virtio_gpu.fb_width - title.len * 8) / 2;
    const title_y: usize = virtio_gpu.fb_height / 2 - 40;
    // Render each glyph at 2× height (simple stretch).
    for (title, 0..) |ch, i| {
        if (ch < 0x20 or ch > 0x7e) continue;
        const glyph = font.glyphs[ch - 0x20];
        var gy: usize = 0;
        while (gy < 8) : (gy += 1) {
            const bits = glyph[gy];
            var gx: usize = 0;
            while (gx < 8) : (gx += 1) {
                if (font.row_pixel(bits, gx)) {
                    // Draw at 2× size.
                    fill_rect(fb, stride, title_x + i * 8 + gx * 2, title_y + gy * 2, 2, 2, clock_accent_rgb);
                }
            }
        }
    }
    // Version line.
    draw_string(fb, stride, title_x + 16, title_y + 24, "v0.1", clock_fg_rgb);
    // Push the splash frame.
    _ = virtio_gpu.gpu_transfer();
    _ = virtio_gpu.gpu_flush();
    // Cycle progress indicator for max_ticks (or until GPU not ready).
    var tick: u64 = 0;
    while (tick < max_ticks) : (tick += 1) {
        // Simple busy-wait for ~1 second per tick (the timer is not yet
        // running at this point in boot).
        var wait: u64 = 0;
        while (wait < 10_000_000) : (wait += 1) {}
        // Draw cycling dots.
        const dots = [5]u8{ '.', 'o', 'O', 'o', '.' };
        const dot_idx = tick % 5;
        fill_rect(fb, stride, title_x + 24, title_y + 40, 40, 8, wallpaper_top());
        draw_glyph(fb, stride, title_x + 24 + @as(usize, @intCast(dot_idx)) * 8, title_y + 40, dots[dot_idx], clock_fg_rgb);
        _ = virtio_gpu.gpu_transfer();
        _ = virtio_gpu.gpu_flush();
    }
    // M76a (#1674): the splash frame is now the scanout's LAST flush —
    // engage the handoff hold. Reached only on the armed+gpu-ready path
    // (the early return above left every headless boot untouched), so a
    // real splash was pushed and the hold may stand on it.
    da.splash_hold = true;
    splash_anchor_tick = null;
}

/// Refresh the tray clock from the 1 Hz generic timer and composite any dirty
/// windows. The shell idle loop is the drain site (the card-3d pattern).
/// Clock ticks on composite() without timer — drain marks taskbar dirty when
/// the minute (or tick) advances; composite() also handles theme/clipboard.
pub fn drain(ticks: u64) virtio_gpu.CmdResult {
    if (!da.armed_global) return .not_ready;
    // M76a (#1674): the splash-hold deadline pump — every idle iteration
    // while the shim composites. Anchors the bound on the first call,
    // releases silently when the seat binds, expires honestly at the
    // bound. While the hold stands, the clock/tray churn is skipped too:
    // nothing may mark the scene dirty enough to matter — composite()
    // refuses it anyway, and the seat's own presents are never suppressed.
    splash_eval(ticks);
    if (da.splash_hold) return .not_ready;
    var need = false;
    if (!da.tray_has_tick or ticks != da.tray_tick) {
        da.tray_has_tick = true;
        da.tray_tick = ticks;
        need = true;
        // Keep deprecated clock vars in sync for any external read.
        da.clock_has_tick = true;
        da.clock_shown_tick = ticks;
    }
    if (da.tray_last_theme != da.theme_id) {
        da.tray_last_theme = da.theme_id;
        need = true;
    }
    const cur_clip = clipboard.current_len();
    if (cur_clip != da.tray_last_clip_len) {
        da.tray_last_clip_len = cur_clip;
        need = true;
    }
    if (need) _ = mark_dirty(255);
    return composite();
}
