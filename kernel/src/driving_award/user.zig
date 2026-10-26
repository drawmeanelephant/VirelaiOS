//! The draw/window syscall seam: user_open/present/bind/fill/move/close and the surface-binding plumbing.
//!
//! Split out of driving_award.zig (#3754) — shared registry state lives in the
//! module root and is reached through `da`; this file owns the seam's own
//! private state.

const da = @import("../driving_award.zig");
const Window = da.Window;
const alloc = da.alloc;
const builtin = da.builtin;
const drag_cancel = da.drag_cancel;
const events = da.events;
const fill_rect = da.fill_rect;
const focus = da.focus;
const geom = da.geom;
const gpu3d_broker = da.gpu3d_broker;
const gpu3d_compute = @import("../gpu3d_compute.zig");
const kbuf_bytes = da.kbuf_bytes;
const kbuf_pages_for = da.kbuf_pages_for;
const kbuf_ptr = da.kbuf_ptr;
const klog = da.klog;
const mark_damage = da.mark_damage;
const mark_dirty = da.mark_dirty;
const paint = da.paint;
const process = da.process;
const raise = da.raise;
const render_terminal_screen = da.render_terminal_screen;
const resize_hit_size = da.resize_hit_size;
const resize_min_h = da.resize_min_h;
const resize_min_w = da.resize_min_w;
const std = da.std;
const terminal = da.terminal;
const test_arena = da.test_arena;
const user_win_max_h = da.user_win_max_h;
const user_win_max_w = da.user_win_max_w;
const user_window_id_base = da.user_window_id_base;
const user_windows_max = da.user_windows_max;
const virtio_gpu = da.virtio_gpu;

// ---------------------------------------------------------------------------
// Card G6 (claim 0487): the draw/window syscall seam — user windows
// ---------------------------------------------------------------------------

pub const UserOpenResult = union(enum) {
    opened: u8,
    invalid: void,
    full: void,
    /// WM1: geometry fit but the pool had no contiguous run for w×h×4.
    nomem: void,
};

/// Find a user window by id (only `.user` kinds — the terminal/clock ids
/// are fixed and never match the user id base).
pub fn find_user_window(id: u8) ?*Window {
    const idx = find_user_window_index(id) orelse return null;
    return &da.windows[idx];
}

/// The registry index of a user window, or null.
pub fn find_user_window_index(id: u8) ?usize {
    var i: usize = 0;
    while (i < da.win_count) : (i += 1) {
        if (da.windows[i].id == id and da.windows[i].kind == .user) return i;
    }
    return null;
}

/// Open a kernel-owned user window at screen position (x, y) with a
/// pool-backed back-buffer exactly w×h (WM1: carved from the kernel page
/// pool here, freed at close/`close_owner`), OWNED by the process `owner`
/// (the syscall layer records the caller's pid here). The window is
/// appended at the top of the z-order and focused. Returns `.opened`
/// with the id (2..9), `.invalid` for geometry outside the scanout
/// bounds (or when the manager is unarmed — no gpu), `.full` when all
/// eight user slots are already open, and `.nomem` when the pool has no
/// contiguous run for the buffer. The window is OWNED by `owner` — it
/// auto-closes when that process exits (the scheduler's exit path calls
/// `close_owner`, which frees the pages — the allocator is a lock-free
/// bitmap, so the exception-context contract holds).
pub fn user_open(x: u32, y: u32, w: u32, h: u32, owner: usize) UserOpenResult {
    if (!da.armed_global) return .invalid;
    if (w == 0 or h == 0 or w > user_win_max_w or h > user_win_max_h) return .invalid;
    if (x >= virtio_gpu.fb_width or y >= virtio_gpu.fb_height) return .invalid;
    if (w > virtio_gpu.fb_width - x or h > virtio_gpu.fb_height - y) return .invalid;
    // #2865: a restored WINDOWS.SAV phantom holds a real slot under the
    // sentinel owner but is never re-persisted — a live open evicts the
    // bottom-most one and rescans once before the eight slots are
    // honestly full. `.full` now means eight LIVE user windows.
    var attempt: usize = 0;
    while (attempt < 2) : (attempt += 1) {
        var i: usize = 0;
        while (i < user_windows_max) : (i += 1) {
            const id = user_window_id_base + @as(u8, @intCast(i));
            var used = false;
            var j: usize = 0;
            while (j < da.win_count) : (j += 1) {
                if (da.windows[j].id == id) {
                    used = true;
                    break;
                }
            }
            if (used) continue;
            const nbytes = kbuf_bytes(w, h);
            const npages = kbuf_pages_for(nbytes);
            const pa = alloc.alloc_pages(npages) orelse return .nomem;
            // Host tests cannot dereference pool phys: the CPU-visible bytes
            // live in the test arena (bump allocator — freed implicitly by
            // the per-test reset in arm()). On device this branch is out.
            var test_ptr: ?[*]u8 = null;
            if (builtin.is_test) {
                const backing = test_arena.fba.allocator().alloc(u8, nbytes) catch {
                    _ = alloc.free_pages(pa, npages);
                    return .nomem;
                };
                test_ptr = backing.ptr;
            }
            // The buffer starts cleared (the old BSS semantic — a fresh window
            // reads back zeros). Host tests cannot dereference pool phys, so
            // the CPU-visible bytes live in the test allocation instead.
            if (builtin.is_test) {
                @memset(test_ptr.?[0..nbytes], 0);
            } else {
                @memset(@as([*]u8, @ptrFromInt(pa))[0..nbytes], 0);
            }
            da.windows[da.win_count] = .{
                .id = id,
                .title = "user",
                .x = x,
                .y = y,
                .w = w,
                .h = h,
                .kind = .user,
                .visible = true,
                .dirty = true,
                .owner = owner,
                .fade_phase = 1, // Arc4 #239: start fade-in
                .fade_tick = 0,
                .workspace = da.current_workspace, // Arc4 #241: assign to current workspace
                .kbuf_pa = pa,
                .kbuf_pages = npages,
                .kbuf_test = test_ptr,
            };
            da.win_count += 1;
            // Issue #990 (claim #997): the open-attribution marker — the
            // WM2 gate's overview counted a PHANTOM third window (n=3 vs the
            // pinned n=2) with exec forwarded exactly once; this line names
            // every kernel-side open (id/owner/rect) so the serial log can
            // attribute it. One line per real open, klog-gated (silent in
            // host tests / pre-boot).
            {
                var kbuf: [80]u8 = undefined;
                const s = std.fmt.bufPrint(&kbuf, "open: id={d} owner={d} rect={d},{d} {d}x{d} ws={d}\n", .{ id, owner, x, y, w, h, da.current_workspace }) catch "open: ?\n";
                klog.line(s);
            }
            _ = focus(id);
            // M32 WMS5: the registered WM must see the new window to hit-test it.
            wm_mirror(id);
            return .{ .opened = id };
        }
        if (!reap_phantom()) break;
    }
    return .full;
}

/// #2865: evict the bottom-most restored phantom (a WINDOWS.SAV replay —
/// no live owner, never re-serialized) so a real `user_open` can take its
/// slot. Runs through `remove_user_at` like every release path: the pool
/// buffer frees, the owner and the WM each get their one notification.
/// Returns false when the registry holds no phantom.
fn reap_phantom() bool {
    var i: usize = 0;
    while (i < da.win_count) : (i += 1) {
        if (da.windows[i].kind == .user and da.windows[i].phantom) {
            remove_user_at(i);
            return true;
        }
    }
    return false;
}

/// M97b-F3 (#2111): the bind-time invariant "a surface-backed window's rect
/// never exceeds its bound surface", re-validated on every resize. `reflow`
/// is the single writer of `win.w`/`win.h` post-open, so one check there
/// covers `user_resize`, `wm_apply_rect`, and the tile/maximize/fullscreen
/// paths — `user_fill`/`composite`/overview/`render_preview` all stride the
/// surface's `pa_base` by win.w×win.h×4, so letting the rect grow past
/// `surface_pages` is a kernel OOB write/read past the region. Refusal
/// keeps the window's old rect (the pool-exhaustion contract); an app that
/// wants a bigger canvas re-binds a bigger surface.
pub fn surface_covers(page_count: u32, w: u32, h: u32) bool {
    return @as(usize, page_count) * alloc.page_size >= kbuf_bytes(w, h);
}

/// WM1 (#707, claim 919): set a user window's rect, reallocating the
/// pool back-buffer when the size changes (the overlap is preserved row
/// by row — the static-buffer semantic that a resize reframes bytes
/// rather than discarding them; grown area reads back zero). Geometry +
/// buffer change atomically: on pool exhaustion returns false with the
/// window entirely unchanged. Same-size is a position-only fast path.
/// Callers: every post-open size writer (user_resize, wm_apply_rect,
/// tile/maximize/fullscreen/keyboard/restore) routes here so the buffer
/// can never disagree with win.w×win.h.
pub fn reflow(win: *Window, x: u32, y: u32, w: u32, h: u32) bool {
    if (w == 0 or h == 0) return false;
    if (w == win.w and h == win.h) {
        win.x = x;
        win.y = y;
        return true;
    }
    if (win.surface_handle != 0 and !surface_covers(win.surface_pages, w, h)) return false;
    const nbytes = kbuf_bytes(w, h);
    const npages = kbuf_pages_for(nbytes);
    const pa = alloc.alloc_pages(npages) orelse return false;
    var test_ptr: ?[*]u8 = null;
    if (builtin.is_test) {
        const backing = test_arena.fba.allocator().alloc(u8, nbytes) catch {
            _ = alloc.free_pages(pa, npages);
            return false;
        };
        @memset(backing, 0);
        test_ptr = backing.ptr;
    } else {
        @memset(@as([*]u8, @ptrFromInt(pa))[0..nbytes], 0);
    }
    const copy_w: usize = @min(win.w, w);
    const copy_h: usize = @min(win.h, h);
    const old_stride: usize = @as(usize, win.w) * 4;
    const new_stride: usize = @as(usize, w) * 4;
    const old_ptr = kbuf_ptr(win);
    const new_ptr: [*]u8 = if (builtin.is_test) test_ptr.? else @as([*]u8, @ptrFromInt(pa));
    var row: usize = 0;
    while (row < copy_h) : (row += 1) {
        @memcpy(new_ptr[row * new_stride ..][0 .. copy_w * 4], old_ptr[row * old_stride ..][0 .. copy_w * 4]);
    }
    if (win.kbuf_pa != 0) _ = alloc.free_pages(win.kbuf_pa, win.kbuf_pages);
    // The host-test arena needs no per-window free (per-test reset).
    win.x = x;
    win.y = y;
    win.w = w;
    win.h = h;
    win.kbuf_pa = pa;
    win.kbuf_pages = npages;
    win.kbuf_test = test_ptr;
    return true;
}

/// Fill a rect in the user window's back-buffer (local coordinates,
/// 0..window w/h) and mark it dirty. Returns false for an unknown id or a
/// rect outside the window bounds.
pub fn user_fill(id: u8, x: u32, y: u32, w: u32, h: u32, rgb: u32) bool {
    const win = find_user_window(id) orelse return false;
    if (w == 0 or h == 0) return false;
    if (x >= win.w or y >= win.h) return false;
    if (w > win.w - x or h > win.h - y) return false;
    // M33 SB3 (claim 9361): a surface-backed window FILLS ITS OWN SHARED
    // PAGES, not the kernel pool buffer — the frozen slot hands off to the
    // surface. fill_rect writes the same B8G8R8X8 bytes into the region the
    // WM mirrors RO, so parity between the migrated fill and the legacy path
    // is exact (identical pixel encoding). An unmigrated window is unchanged.
    const surface = user_surface(id);
    if (surface) |sf| {
        // #2111: a desynced rect would write past the surface's physical
        // pages — refuse the fill rather than corrupt adjacent memory.
        if (!surface_covers(sf.page_count, win.w, win.h)) return false;
        fill_rect(@as([*]u8, @ptrFromInt(sf.pa_base)), win.w * 4, x, y, w, h, rgb);
    } else {
        fill_rect(kbuf_ptr(win), win.w * 4, x, y, w, h, rgb);
    }
    // M33 SB4 (claim 2382): record the EXACT written rect as the surface's
    // damage, so composite (and, via COMPOSITE_TICK, the WM) repaints only
    // this region — the rect-granular gate (one rect writes -> one rect
    // repaints), not a whole-window present.
    _ = mark_damage(id, x, y, w, h);
    return true;
}

/// Mark a user window dirty so the compositor blits its back-buffer on the
/// next idle-loop pass (the deferred-present discipline — the syscall never
/// touches the gpu directly; the shell idle loop's `drain` composites).
/// Returns false for an unknown id.
pub fn user_present(id: u8) bool {
    const win = find_user_window(id) orelse return false;
    win.dirty = true;
    // #3731: a seatless boot's first user present ends the splash hold —
    // the app's own frame is the honest successor content, the same
    // one-frame handoff shape as the seat bind (splash -> app frame, zero
    // kernel-art frames between). The bound still owns windowless boots,
    // and a boot whose seat may still bind keeps holding for it.
    if (da.splash_hold and da.seat_impossible and !da.wm_owns_user_layer) da.splash_hold = false;
    // card #3034 (ADR 0007 X4): a whole-surface present is damage too — the
    // same seat backpressure edge mark_damage fires (the hook bounds it to
    // one pending tick).
    if (da.wm_damage_hook) |hook| hook();
    return true;
}

/// M33 SB3 (claim 9361): the surface info a surface-backed user window
/// holds (the kernel's composite source + the WM-mirror identity). Returns
/// null for an unknown / non-user / unmigrated window.
pub const SurfaceInfo = struct {
    handle: u32,
    pa_base: u64,
    page_count: u32,
};

/// Bind user window `id` to the shared surface described by `si`. The
/// caller (the `sys_mmap` window-tag path) has already created the region,
/// mapped the owner's writable leaves into the window owner's root, and
/// granted the WM its RO mirror; this records the identity on the window so
/// `composite()` blits from the surface's own pages. Returns false if the
/// window is unknown / not `.user` / already surface-backed (a window binds
/// ONE surface; unbind before rebind).
pub fn user_bind_surface(id: u8, si: SurfaceInfo) bool {
    const win = find_user_window(id) orelse return false;
    if (win.surface_handle != 0) return false; // already migrated
    win.surface_handle = si.handle;
    win.surface_pa = si.pa_base;
    win.surface_pages = si.page_count;
    win.dirty = true;
    return true;
}

/// The surface a user window is bound to, or null when unmigrated.
pub fn user_surface(id: u8) ?SurfaceInfo {
    const win = find_user_window(id) orelse return null;
    if (win.surface_handle == 0) return null;
    return .{ .handle = win.surface_handle, .pa_base = win.surface_pa, .page_count = win.surface_pages };
}

/// True when user window `id` is surface-backed (migrated). An unmigrated
/// window is `false` (the frozen kernel-buffer path, byte-identical).
pub fn user_is_surface_backed(id: u8) bool {
    const win = find_user_window(id) orelse return false;
    return win.surface_handle != 0;
}

/// M102d1 (#2166): the CPU-visible pixel destination for one user window —
/// the bound surface's own pages when migrated (identity-mapped into the
/// kernel root, exactly where `composite()`/`user_fill` already address it),
/// else the pool back-buffer. Surface capacity is re-validated HERE, at
/// copy time, against the CURRENT win.w×win.h — an undersized surface
/// refuses rather than painting past its pages (the #2111 invariant). Null
/// for an unknown/non-user window or an undersized surface; the returned
/// extent/stride are this moment's geometry so the caller clips and clears
/// against the live rect, never a captured one.
pub const PaintDest = struct {
    base: [*]u8,
    stride: usize,
    w: u32,
    h: u32,
};

pub fn user_paint_dest(id: u8) ?PaintDest {
    const win = find_user_window(id) orelse return null;
    if (win.surface_handle != 0) {
        if (!surface_covers(win.surface_pages, win.w, win.h)) return null;
        return .{ .base = @as([*]u8, @ptrFromInt(win.surface_pa)), .stride = @as(usize, win.w) * 4, .w = win.w, .h = win.h };
    }
    return .{ .base = kbuf_ptr(win), .stride = @as(usize, win.w) * 4, .w = win.w, .h = win.h };
}

/// M104f: render a window's KERNEL front-end (attached terminal grid) into
/// its bound surface — the half of paint() a migrated window still needs
/// while the seat owns the scanout blit (wm_owns_user_layer). No-op for
/// self-painting apps (no terminal screen) and for a rect that outgrew the
/// bound pages (the #2111 invariant — the seat clips to the last honest
/// extent until the owner re-shares).
pub fn render_frontend_to_surface(w: *const Window) void {
    if (w.kind != .user or w.surface_handle == 0) return;
    const scr = terminal.screenOf(w.id) orelse return;
    if (!surface_covers(w.surface_pages, w.w, w.h)) return;
    terminal.syncWindowCols(w.id, w.w);
    render_terminal_screen(@as([*]u8, @ptrFromInt(w.surface_pa)), w, scr);
}

/// Move a user window's top-left corner to (x, y), CLAMPED so the whole
/// window stays inside the scanout (a window is never allowed off-screen —
/// the honest bound). Marks the moved window dirty and the terminal dirty
/// (its old rect is revealed) so the next composite repaints over the old
/// position and blits at the new one. Returns false for an unknown id, a
/// non-user window (the terminal + clock are fixed), or an unarmed manager.
/// The EL0 `sys_win_move` enforces ownership on top of this.
pub fn user_move(id: u8, x: u32, y: u32) bool {
    const win = find_user_window(id) orelse return false;
    const max_x = virtio_gpu.fb_width - win.w;
    const max_y = virtio_gpu.fb_height - win.h;
    win.x = @min(x, max_x);
    win.y = @min(y, max_y);
    win.dirty = true;
    // Reveal whatever sat under the window's old rect (the terminal repaint).
    _ = mark_dirty(0);
    // M32 WMS5: the WM's mirror follows the clamped move (it proposes, the
    // kernel clamps — the mirror carries the clamped truth).
    wm_mirror(id);
    return true;
}

/// Raise a user window to the top of the z-order (the EL0 `sys_win_raise`
/// primitive — ownership is enforced at the syscall layer). Focus is
/// unchanged (tracked by id, the G5 discipline). Returns false for an
/// unknown id or a non-user window (the terminal + clock are fixed).
pub fn user_raise(id: u8) bool {
    if (find_user_window_index(id) == null) return false;
    return raise(id);
}

/// Arc4 #238 (slot 49): raise the caller's user window to the top of the
/// z-order (EL0-callable variant of `user_raise`). Owner-restricted like
/// `sys_win_raise`; refused on fixed layers. Returns false for unknown id,
/// non-user window, or unarmed manager.
pub fn user_raise_front(id: u8) bool {
    if (!da.armed_global) return false;
    if (find_user_window_index(id) == null) return false;
    return raise(id);
}

/// Arc4 #238 (slot 50): lower the caller's user window to the bottom of
/// the z-order — above fixed windows (wallpaper/taskbar/dock, ids 255/253)
/// but below all other user windows. Owner-restricted; refused on fixed
/// layers. Returns false for unknown id, non-user window, or unarmed.
pub fn user_lower_back(id: u8) bool {
    if (!da.armed_global) return false;
    const idx = find_user_window_index(id) orelse return false;
    // Move to index 0: shift everything above down by one.
    const moved = da.windows[idx];
    var j = idx;
    while (j > 0) : (j -= 1) da.windows[j] = da.windows[j - 1];
    da.windows[0] = moved;
    da.windows[0].dirty = true;
    return true;
}

/// Arc4 #241: move a user window to a different workspace.
pub fn user_move_to_workspace(id: u8, ws: u8) bool {
    if (!da.armed_global) return false;
    const w = find_user_window(id) orelse return false;
    w.workspace = ws;
    w.dirty = true;
    _ = mark_dirty(0);
    return true;
}

/// Close (release) a user window: remove it from the registry (the z-order
/// stays compact — later windows shift down), fall the focus back to the
/// terminal when the closed window held it, and mark the fixed windows
/// (terminal + clock) dirty so the next composite repaints over the
/// released window's pixels. Returns false for an unknown id or a non-user
/// window (the terminal + clock are fixed and never closable). The pool
/// back-buffer is freed for the next `user_open` (which re-clears it).
/// This is the PRIVILEGED release path (the monitor's `dui close <n>`); the
/// EL0 `sys_win_close` enforces ownership on top of it. The WM mirror goes
/// out from `remove_user_at` (the shared release primitive) — every release
/// path informs the WM exactly once.
pub fn user_close(id: u8) bool {
    const idx = find_user_window_index(id) orelse return false;
    remove_user_at(idx);
    return true;
}

/// The owner of a user window, or null for an unknown id / non-user window
/// (the syscall layer compares this to the caller to enforce ownership).
pub fn user_owner(id: u8) ?usize {
    const win = find_user_window(id) orelse return null;
    return win.owner;
}

/// Card G6 (claim 0487) follow-on (slot 18): the geometry of a user window
/// — the `sys_win_get` result. The syscall layer marshals this into the
/// caller's buffer (four u32 LE words) through uaccess so an EL0 program
/// can read its window's rect back after a CLAMPED move (the move is
/// silent; this is the read-back seam).
pub const WinRect = struct {
    x: u32,
    y: u32,
    w: u32,
    h: u32,
};

/// The rect of a user window, or null for an unknown id / non-user window
/// (the terminal + clock are fixed and never match).
pub fn user_rect(id: u8) ?WinRect {
    const win = find_user_window(id) orelse return null;
    return .{ .x = win.x, .y = win.y, .w = win.w, .h = win.h };
}

/// #1056 item 2: the best human-readable name for a user window — its
/// app-set title when one exists (sys_win_set_title / the tabapp declare
/// RPC), otherwise the OWNING PROCESS's name (the FAT executable name, e.g.
/// "WINLOOP.BIN"). This names the tabs of apps that never set a title, so
/// the WM's kind-20 mirror (which carries no title bytes) can resolve a
/// label. Null for a non-user window / unknown id / nameless owner.
pub fn user_display_name(id: u8) ?[]const u8 {
    const win = find_user_window(id) orelse return null;
    if (win.title_len > 0) return win.title_buf[0..win.title_len];
    const owner = win.owner orelse return null;
    const pinfo = process.info(owner) orelse return null;
    if (pinfo.name.len == 0) return null;
    return pinfo.name;
}

/// M32 WMS5 (issue #625): push ONE registry mirror (kind 20 WM_WINDOW)
/// through the `wm_window_hook` for window `id` — the kernel's window data
/// fanned out so the registered WM can hit-test. A no-op when no WM is
/// registered (hook null) or the window is gone. Called from the user-
/// window mutation points (open/close/move/resize/visibility/focus) so the
/// WM's mirror stays current.
pub fn wm_mirror(id: u8) void {
    const hook = da.wm_window_hook orelse return;
    const win = find_user_window(id) orelse return;
    hook(id, win.x, win.y, win.w, win.h, win.visible, win.id == da.focused_id, win.workspace, win.unsaved, false);
}

/// Card G6 (claim 0487) follow-on (slot 19): the FULL state of a user
/// window — the `sys_win_query` result. The rect (like `user_rect`) plus the
/// z-order rank (`z` = the registry index, 0 = bottom — the SAME number the
/// monitor's `dui` report prints), and the focus/visible/dirty flags (1/0).
/// The syscall layer marshals this into the caller's buffer (eight u32 LE
/// words) so an EL0 program can introspect its window end to end.
pub const WinQuery = struct {
    x: u32,
    y: u32,
    w: u32,
    h: u32,
    z: u32,
    focused: u32,
    visible: u32,
    dirty: u32,
};

/// The full state of a user window, or null for an unknown id / non-user
/// window. `z` is the registry index (0 = bottom of the z-order).
pub fn user_query(id: u8) ?WinQuery {
    const idx = find_user_window_index(id) orelse return null;
    const win = da.windows[idx];
    return .{
        .x = win.x,
        .y = win.y,
        .w = win.w,
        .h = win.h,
        .z = @intCast(idx),
        .focused = if (da.focused_id == id) 1 else 0,
        .visible = if (win.visible) 1 else 0,
        .dirty = if (win.dirty) 1 else 0,
    };
}

/// Card G6 (claim 0487) follow-on (slot 20): set a user window's visibility
/// (hide/show). Returns false for an unknown id or a non-user window (the
/// terminal + clock are fixed and never hideable). Hiding marks the window
/// hidden + dirty (inert while hidden) and marks the terminal dirty so the
/// next composite repaints over the hidden window's pixels; showing marks the
/// window dirty so it reappears. Idempotent (same-flag calls are a no-op — no
/// dirty churn). The EL0 `sys_win_set_visible` enforces ownership on top of
/// this; focus is unchanged (tracked by id, the G5 discipline).
pub fn user_set_visible(id: u8, visible: bool) bool {
    const win = find_user_window(id) orelse return false;
    if (win.visible == visible) return true;
    win.visible = visible;
    win.dirty = true;
    if (!visible) _ = mark_dirty(0); // reveal whatever sat under the hidden window
    wm_mirror(id); // M32 WMS5: visibility changes must reach the WM's mirror
    return true;
}

/// Arc2 W1 (claim 3589, #224): drag-to-resize — hit-test, clamp, and resize.
///
/// 6×6 bottom-right corner at (x+w-6, y+h-6). Pure — host-testable.
pub fn is_resize_hit(win: Window, px: u32, py: u32) bool {
    if (win.kind != .user or !win.visible) return false;
    if (win.w < resize_hit_size or win.h < resize_hit_size) return false;
    const rx = win.x + win.w - resize_hit_size;
    const ry = win.y + win.h - resize_hit_size;
    return px >= rx and px < win.x + win.w and py >= ry and py < win.y + win.h;
}

/// Clamp helpers — pure, host-testable. Buffer bounds 128×64..scanout
/// (WM1: the pool buffer follows the window up to the scanout) plus
/// on-scanout containment (so a resize never writes beyond the framebuffer).
pub fn clamp_resize_w(req_w: i32, win_x: u32) u32 {
    // Delegated to the shared wnd_core clamp rule (single source with the
    // WM server).
    return geom.clamp_resize_w(req_w, win_x, virtio_gpu.fb_width, resize_min_w, user_win_max_w);
}

pub fn clamp_resize_h(req_h: i32, win_y: u32) u32 {
    // Delegated to the shared wnd_core clamp rule.
    return geom.clamp_resize_h(req_h, win_y, virtio_gpu.fb_height, resize_min_h, user_win_max_h);
}

/// Resize a user window to (w, h), clamped to 128×64..scanout and
/// on-scanout, reallocating the pool buffer to the clamped size (WM1 —
/// content overlap preserved). Marks the window dirty + the terminal
/// dirty (reveal old rect + chrome repaint on next `composite()`), and
/// emits `WIN_RESIZE` to the owning pid. Returns false for an unknown
/// id / non-user window, or when the pool cannot satisfy the new size
/// (the window keeps its old rect). The EL0 `sys_win_resize` enforces
/// ownership on top of this; the compositor's pointer_tick calls it
/// directly.
pub fn user_resize(id: u8, w: u32, h: u32) bool {
    const win = find_user_window(id) orelse return false;
    const clamped_w = clamp_resize_w(@intCast(w), win.x);
    const clamped_h = clamp_resize_h(@intCast(h), win.y);
    if (!reflow(win, win.x, win.y, clamped_w, clamped_h)) return false;
    win.dirty = true;
    _ = mark_dirty(0);
    _ = mark_dirty(1);
    wm_mirror(id); // M32 WMS5: the WM's mirror follows the clamped resize
    if (win.owner) |owner| {
        events.push(owner, .{
            .kind = events.WIN_RESIZE,
            .flags = 0,
            .seq = 0,
            .arg0 = clamped_w,
            .arg1 = clamped_h,
        });
    }
    return true;
}

/// M32 WMS5 Gate 2 (claim 4278): apply a WM-proposed rect with LAYOUT
/// semantics — on-scanout clamping only, NO back-buffer clamp. The shim's
/// own tile/maximize/fullscreen write 837-wide rects directly (the blit
/// source-clamps to the app's 512x424 buffer, claim 8777); the WM must be
/// able to produce the SAME rects (the W1–W16 registered-matrix parity bar
/// of issue #625). `sys_win_move`/`sys_win_resize` keep the back-buffer
/// clamp (an APP asking its window to grow past its own buffer is refused);
/// this is the WM deciding LAYOUT, so the back-buffer is not the limit.
/// WM proposes, kernel clamps to the scanout, mirror follows.
pub fn wm_apply_rect(id: u8, x: u32, y: u32, w: u32, h: u32) bool {
    const win = find_user_window(id) orelse return false;
    // M42 SX2 (issue #983): remember the pre-layout size so the owner can be
    // told when the WM's LAYOUT decision grew/shrank its canvas — the seam
    // TABWM's full-viewport proposal rides. Pure moves emit nothing (no
    // event spam; parity with `user_resize`, which notifies on resize only).
    const old_w = win.w;
    const old_h = win.h;
    // Move FIRST so the size clamp sees the final position (the shim's
    // user_move-then-user_resize order, per the WMS4 comment). The max is
    // the SCANOUT, not the app's back buffer: the shim's own
    // tile/maximize/fullscreen write 837-wide rects directly, and the WM
    // must be able to produce the SAME rects (the W1–W16 registered-matrix
    // parity bar). WM proposes, kernel clamps on-scanout, mirror follows.
    const max_x = virtio_gpu.fb_width -| win.w;
    const max_y = virtio_gpu.fb_height -| win.h;
    const nx = @min(x, max_x);
    const ny = @min(y, max_y);
    const cw = geom.clamp_resize_w(@intCast(w), nx, virtio_gpu.fb_width, resize_min_w, virtio_gpu.fb_width);
    const ch = geom.clamp_resize_h(@intCast(h), ny, virtio_gpu.fb_height, resize_min_h, virtio_gpu.fb_height);
    // WM1: the pool buffer follows the layout size (content overlap
    // preserved); on pool exhaustion the window keeps its old rect.
    if (!reflow(win, nx, ny, cw, ch)) return false;
    if (da.wm_owns_user_layer and win.chrome_valid and win.chrome.kind == 0) {
        // The WM now owns this client's surrounding pixels. Finish the
        // native opening fade and replace the old scanout contents BEFORE
        // the seat can present: blending over them preserves the old frame.
        win.fade_phase = 0;
        win.fade_tick = 0;
        win.damaged = false;
        // Migrated clients are composed by the WM, not by this legacy blit —
        // but a kernel-rendered front-end (attached tty) still needs its grid
        // re-rendered into the resized surface, or the seat reads stale rows.
        if (win.surface_handle == 0) paint(win) else render_frontend_to_surface(win);
    }
    win.dirty = true;
    _ = mark_dirty(0);
    _ = mark_dirty(1);
    wm_mirror(id); // M32 WMS5: the WM's mirror follows the clamped layout
    // M42 SX2: the owner learns its new canvas when the clamped size differs
    // from the old one (same payload shape as `user_resize`: w in arg0,
    // h in arg1) — TABWM's full-viewport proposals reach the app.
    if (win.w != old_w or win.h != old_h) {
        if (win.owner) |owner| {
            events.push(owner, .{
                .kind = events.WIN_RESIZE,
                .flags = 0,
                .seq = 0,
                .arg0 = win.w,
                .arg1 = win.h,
            });
        }
    }
    return true;
}

/// True when a resize drag is active.
pub fn resize_active() bool {
    return da.resize_id != null;
}

pub fn resize_current_id() ?u8 {
    return da.resize_id;
}

/// Auto-close every user window owned by the process `owner` (the exit
/// path's teardown seam — `scheduler.exit_current` calls this with the
/// exited process's pid). Returns how many windows were released. Pure
/// BSS writes (safe in the exception context the exit path runs in).
pub fn close_owner(owner: usize) usize {
    // M111b (#2409): a dying owner's GPU3D compute session revokes here —
    // the one per-process teardown funnel the exit inventory reaches
    // (compute binds no window, so remove_user_at never sees it). Handle
    // invalidation first, the bounded device drain second.
    gpu3d_compute.owner_exit(owner);
    // M52 card 3 (#1240): a dead drag SOURCE leaves no live pointer capture.
    // The Arc4 #237 drag payload is process-scoped (`drag_source_pid`) and
    // its only clear sites are the drop path and `sys_drag_read`, so a
    // source that dies mid-drag used to strand `drag_active` forever: every
    // later pointer move kept crossing windows and every drop still named
    // the dead pid as the source. Cancel it with the source.
    if (da.drag_active and da.drag_source_pid == owner) drag_cancel();
    var closed: usize = 0;
    while (true) {
        var idx: ?usize = null;
        var i: usize = 0;
        while (i < da.win_count) : (i += 1) {
            if (da.windows[i].kind == .user and da.windows[i].owner == owner) {
                idx = i;
                break;
            }
        }
        const found = idx orelse break;
        remove_user_at(found);
        closed += 1;
    }
    return closed;
}

/// M52 card 3 (#1240): close every user window BOUND to shared-surface
/// `handle` (the zombie-close seam `shared_mmap.revoke_peer` calls when an
/// owner-side revoke frees the surface). A surface-backed window's pixels
/// live in the region's pages, so once the region is revoked the window has
/// no source left: `composite()` would blit from freed physical pages and a
/// frozen `sys_win_fill` would WRITE into them (the surface fill path uses
/// `surface_pa` directly, not a uaccess-checked va). Ending the window here
/// keeps the registry honest — the release runs through the same
/// `remove_user_at` primitive as every other path, so the WM gets its one
/// released mirror and the owner its WIN_CLOSE. Returns the windows closed.
/// Pure BSS writes (safe in the exit/exception context the revoke runs in).
pub fn surface_revoked(handle: u32) usize {
    if (handle == 0) return 0; // unmigrated windows hold no surface
    var closed: usize = 0;
    while (true) {
        var idx: ?usize = null;
        var i: usize = 0;
        while (i < da.win_count) : (i += 1) {
            if (da.windows[i].kind == .user and da.windows[i].surface_handle == handle) {
                idx = i;
                break;
            }
        }
        const found = idx orelse break;
        remove_user_at(found);
        closed += 1;
    }
    return closed;
}

/// Remove the user window at registry index `idx` (the shared release
/// primitive behind `user_close` and `close_owner`): compact the z-order,
/// fall the focus back to the terminal when the removed window held it,
/// and mark the fixed windows dirty so the next composite repaints over
/// the released window's pixels.
/// Card E4 (claim 0293): emits WIN_CLOSE to the user window owner.
/// M42 UX (2026-09-05): fans ONE "released" mirror to the registered WM
/// from the already-copied `removed_win` state (visible=false, released —
/// flags bit 13), so EVERY release path (user_close, close_owner/app
/// self-exit) informs the WM exactly once that the window left the
/// registry — not merely hid. Pure BSS writes (allocation-free), so the
/// exit/exception context contract of the WIN_CLOSE push holds for the
/// fan equally.
pub fn remove_user_at(idx: usize) void {
    const removed_win = da.windows[idx];
    const removed_id = removed_win.id;
    // M102d1 (#2166): a dying window revokes its GPU3D context FIRST — the
    // handle is invalidated before c1's bounded drain runs, so a late
    // publish can never reach this window's pixels. Non-blocking: a device
    // that cannot drain leaves the session quarantined (backing pinned for
    // late DMA). Runs in the exit path via close_owner — the same
    // bounded-exchange contract as the kbuf free below.
    gpu3d_broker.revoke_window(removed_id);
    // #1082 (ADR 0020 Amendment A, A6): closing the bound window auto-detaches
    // its terminal. The terminal survives, buffered, unattached, and the
    // serial console is NOT reclaimed (boot default unchanged). Pure BSS
    // writes — safe in the exit/exception context this runs in.
    terminal.detachWindow(removed_id);
    // M42 UX: the released mirror goes out FIRST (the pre-removal fan the
    // old user_close owned) — focused_id still holds the pre-removal truth
    // here, and the WM drops its hit-test target before anything mutates.
    if (da.wm_window_hook) |hook| {
        hook(removed_win.id, removed_win.x, removed_win.y, removed_win.w, removed_win.h, false, removed_win.id == da.focused_id, removed_win.workspace, removed_win.unsaved, true);
    }
    // WM1 (#707, claim 919): return the pool back-buffer to the allocator.
    // Runs in the exit path via close_owner — free_pages is a lock-free
    // bitmap op, so the exception-context contract holds. Unconditional: a
    // zero pa frees nothing (free_pages reports false). The host-test
    // arena needs no free (per-test reset in arm()).
    if (removed_win.kbuf_pa != 0) {
        _ = alloc.free_pages(removed_win.kbuf_pa, removed_win.kbuf_pages);
    }
    if (removed_win.kind == .user and removed_win.owner != null) {
        events.push(removed_win.owner.?, .{
            .kind = events.WIN_CLOSE,
            .flags = 0,
            .seq = 0,
            .arg0 = removed_id,
            .arg1 = 0,
        });
    }
    var j = idx;
    while (j + 1 < da.win_count) : (j += 1) da.windows[j] = da.windows[j + 1];
    da.win_count -= 1;
    // M37 DQ2 (issue #840): a closed container orphans its tabs — attached
    // children become standalone rather than pointing at a dead parent.
    var k: usize = 0;
    while (k < da.win_count) : (k += 1) {
        if (da.windows[k].tab_parent == removed_id) da.windows[k].tab_parent = 0;
    }
    if (da.focused_id == removed_id) {
        da.focused_id = 0; // fall back to the terminal
    }
    // M15 C2: overlay snapshot is stale after a close — dismiss honestly.
    if (da.overlay_active) {
        da.overlay_active = false;
        da.overlay_count = 0;
    }
    // M32 WMS8 Gate 6 (issue #628): the per-window snap-clear block is
    // DELETED (the snap_last_*/snap_snapped arrays are gone — the WM owns
    // snap). Tiling state clearing continues below.
    // M21 W1: clear tiling state if a tiled window is closed.
    if (da.tile_master_id) |mid| {
        if (mid == removed_id) {
            da.tile_master_id = null;
            if (da.tile_stack_id) |sid| {
                da.tile_master_id = sid;
                da.tile_stack_id = null;
            }
            da.tile_mode = da.tile_master_id != null;
        }
    }
    if (da.tile_stack_id) |sid| {
        if (sid == removed_id) {
            da.tile_stack_id = null;
        }
    }
    // M32 WMS8 Gate 6 (issue #628): the drag_id/snap_zone clear on window
    // close is DELETED (the title-bar drag + snap state is gone). Resize
    // state clearing stays.
    if (da.resize_id != null and da.resize_id.? == removed_id) {
        da.resize_id = null;
    }
    // M52 card 3 review nit (#1240): the Arc4 #237 drag target is capture
    // state too — if the pointer was over THIS window, the drag must stop
    // naming it. (Not the WMS8-deleted title-bar drag_id above: this is the
    // drag-and-drop hover. The DRAG_LEAVE/DROP fans are gated on drag_active,
    // so a removed target made them inert rather than absent — the same
    // "no stale capture state" rule the rest of the card enforces.)
    if (da.drag_over_id != null and da.drag_over_id.? == removed_id) {
        da.drag_over_id = null;
    }
    // Reveal whatever sat under the released window.
    _ = mark_dirty(0);
    _ = mark_dirty(1);
}

/// Helper: user window slot index (id - base) or null.
pub fn user_window_slot(id: u8) ?usize {
    if (id < user_window_id_base) return null;
    const s = @as(usize, id - user_window_id_base);
    if (s >= user_windows_max) return null;
    return s;
}
