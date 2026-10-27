//! Milestone six card G5 (claim 1543) — Driving Award, the window manager.
//!
//! A bounded window registry (fixed BSS, no heap) with z-order, focus,
//! hit-testing, and a dirty-rect compositor that blits window buffers into
//! the G1 framebuffer (kernel/src/virtio_gpu.zig). Road Pops — the G3 boot
//! terminal (kernel/src/text.zig, kernel/src/road_pops.zig) — is the FIRST
//! window (window 0, a full-screen terminal); a second demo window (a 1 Hz
//! clock) overlaps its top-right corner and proves multiple windows with
//! distinct content + focus.
//!
//! Z-order is the registry array order (index 0 = bottom, the last index =
//! top). `raise` moves a window to the top; focus is tracked by window id
//! (stable across raises), and `hit_test` returns the TOPMOST visible window
//! containing a point. The compositor repaints from the lowest dirty window
//! upward — a genuine dirty-rect redraw: a window whose content did not
//! change is never repainted, and the scanout transfer + flush run once per
//! dirty batch (the G1/G2/G3 full-frame present discipline).
//!
//! Window 0 (the terminal) renders the shared text layer straight into the
//! scanout framebuffer (its "buffer" IS the framebuffer — a terminal
//! redraws its full client area, which here is the whole screen). Window 1
//! (the clock) renders into its own fixed BSS back-buffer, which the
//! compositor blits over the terminal. Because the terminal's repaint
//! covers the clock's region, the compositor repaints from the lowest dirty
//! window UP, so the clock is always re-blitted over a freshly repainted
//! terminal — the classic overlay-preserving order.
//!
//! Input routing (the I3 path): keyboard bytes reach the Road Pops line
//! editor only while the terminal window is focused (`terminal_focused`);
//! when another window holds focus the keystrokes simply queue in the input
//! FIFO until the terminal regains focus. Serial (the scripted evidence
//! channel) stays an always-wired fallback, unchanged.
//!
//! No libc, no POSIX, no allocation, no function pointers in any const
//! table (the claim-0015 discipline — the registry is a BSS array built at
//! runtime).
//!
//! Host tests pin the pure contracts: hit-test, z-order/raise, focus, the
//! repaint-from-lowest-dirty plan, the clock rendering, and the blit.
//!
//! Layout (#3754): this root file owns the module's identity — the imports,
//! the window registry's shared BSS state and types, the arm/query
//! lifecycle, and a flat `pub const` re-export of every decl moved into
//! the seam files under kernel/src/driving_award/. Callers still see one
//! namespace; each seam file reaches the shared state through `da` (an
//! @import of this file) so state stays single-sourced.

pub const std = @import("std");
pub const builtin = @import("builtin");
pub const alloc = @import("alloc.zig"); // WM1 (#707, claim 919): pool-backed user back-buffers
pub const memmap = @import("memmap.zig"); // WM1: the test-pool descriptor (is_test only)
pub const font = @import("font8x8.zig");
pub const font_unicode = @import("font_unicode.zig"); // M73a-2 (#1631): the shared codepoint→glyph table + U+FFFD fallback art
pub const font_metrics = @import("font_metrics.zig"); // M73l (#1661): THE cell geometry — no terminal geometry literal says 8
pub const font_atlas = @import("font_atlas_data.zig"); // M73l (#1661): the generated FiraCode cell atlas
pub const input = @import("input.zig"); // card U4 (claim 4993): the pointer reports
pub const virtio_gpu = @import("virtio_gpu.zig");
pub const fbtext = @import("text.zig");
pub const events = @import("events.zig"); // Milestone 9 (claim 9228): application events
pub const clipboard = @import("clipboard.zig"); // Arc2 W3 (claim 1264): tray clipboard indicator
pub const virtio_snd = @import("virtio_snd.zig"); // M27 G5 (#448): action sound feedback
pub const settings = @import("settings.zig");
pub const klog = @import("klog.zig"); // issue #990 (claim #997): the serial-log seam (hook armed by main.zig)
pub const geom = @import("wnd_core.zig"); // M32 WMS3 (issue #623): the shared pure rules (hit-test / workspace / clamps / title-layout) — compiled by the kernel AND the WM server so they cannot drift
pub const process = @import("process.zig"); // #1056 item 2: resolve a window's owning process name (the non-tabapp tab-title fallback)
pub const terminal = @import("terminal.zig"); // #1082 (ADR 0020 A4/A6): window-bound terminal grid + close auto-detach
pub const gpu3d_broker = @import("gpu3d_broker.zig"); // M102d1 (#2166): window death revokes its GPU3D context (remove_user_at)
const gpu3d_compute = @import("gpu3d_compute.zig"); // M111b (#2409): owner death revokes its compute session (close_owner)

/// M27 G13: Focus-follows-mouse configuration and dialog previous-focus tracking
pub var focus_follows_mouse: bool = false;
pub var previous_focus: ?u8 = null;
// ---------------------------------------------------------------------------

/// Bounded window registry size (fixed BSS: 4 fixed windows + the WM1
/// user ceiling of 8 + one spare, mirroring the pre-WM1 4+4+1 sizing).
pub const max_windows: usize = 13;

/// The clock window: an overlay in the terminal's top-right corner. 304x192
/// at (960,16) — inside the 1280x720 scanout, overlapping the terminal.
pub const clock_x: u32 = 960;
pub const clock_y: u32 = 16;
pub const clock_w: u32 = 304;
pub const clock_h: u32 = 192;

/// Clock palette (0xRRGGBB) — deliberately distinct from the terminal's
/// green-on-dark so the live gate can tell the two windows apart by pixel.
pub const clock_border_rgb: u32 = 0x8899aa;
pub const clock_title_bg_rgb: u32 = 0xb58900; // amber title bar
pub const clock_title_fg_rgb: u32 = 0x141414;
pub const clock_bg_rgb: u32 = 0x0a1a2e; // dark navy body
pub const clock_fg_rgb: u32 = 0xd8dee9; // light body text
pub const clock_accent_rgb: u32 = 0xffaa00; // amber accent (mascot line)

/// Card U5 (claim 0935, ADR 0008 D4): the HIG chrome palette. The focus
/// ring is white (distinct from every window fill); the user title bar
/// reuses the window-manager's dark blue with white text.
pub const focus_ring_rgb: u32 = 0xffffff;
pub const focus_ring_w: usize = 3;
/// M32 WMS5: the title-bar band height — single source is wnd_core (the
/// WM server's drag hit-test uses the SAME number). Re-export, not a
/// second constant.
pub const user_title_h: usize = geom.title_bar_h;
pub const user_title_bg_rgb: u32 = 0x1a2b3c;
pub const user_title_fg_rgb: u32 = 0xffffff;
pub const cursor_rgb: u32 = 0xffffff;
pub const cursor_outline_rgb: u32 = 0x101018;
pub const cursor_w: usize = 16;
pub const cursor_h: usize = 24;
/// Pointer coordinates and hit tests name the arrow tip, not its bitmap origin.
pub const cursor_hotspot = struct { x: u32, y: u32 }{ .x = 1, .y = 1 };
/// M203 (#3348): the f-artcraft #1249 cursor set — all ten delivered shapes,
/// mask-reduced by tools/art/mkcursormask.py (canonical copy in
/// image/art/cursors/MASKS.TXT; hotspots are the delivered ship coords).
/// `cursor_shape` below selects the drawn mask; the seat drives it through
/// wmctl cmd 24 (`wmctl_cursor_shape`). The enum's index order IS the wire
/// order — do not renumber.
pub const CursorShape = enum(u8) {
    ptr, // CURS_PTR — the default arrow
    txt, // CURS_TXT — i-beam, editable text
    lnk, // CURS_LNK — hand, link-like surface
    grb, // CURS_GRB — open hand, grabbable surface
    grg, // CURS_GRG — closed hand, drag in flight
    rsx, // CURS_RSX — horizontal resize (left/right)
    rsy, // CURS_RSY — vertical resize (top/bottom)
    xhr, // CURS_XHR — crosshair
    bsy, // CURS_BSY — busy
    no, // CURS_NO — refused (a drop the target declines)
};
pub const cursor_shape_count: usize = 10;
/// One staged shape: its ship hotspot and its 16x24 two-level mask.
pub const CursorDef = struct {
    hx: u32,
    hy: u32,
    mask: *const [cursor_h][]const u8,
};
/// #3348: the shape the kernel stamps. The arrow default keeps the shim and
/// every pre-seat gate byte-identical; a seat's last pick dies with it
/// (wm_server.unregister restores `ptr`).
pub var cursor_shape: CursorShape = .ptr;
pub const cursor_set: [cursor_shape_count]CursorDef = .{
    .{ .hx = 1, .hy = 1, .mask = &cursor_mask },
    .{ .hx = 8, .hy = 12, .mask = &cursor_txt_mask },
    .{ .hx = 6, .hy = 2, .mask = &cursor_lnk_mask },
    .{ .hx = 8, .hy = 14, .mask = &cursor_grb_mask },
    .{ .hx = 8, .hy = 14, .mask = &cursor_grg_mask },
    .{ .hx = 8, .hy = 12, .mask = &cursor_rsx_mask },
    .{ .hx = 8, .hy = 12, .mask = &cursor_rsy_mask },
    .{ .hx = 8, .hy = 12, .mask = &cursor_xhr_mask },
    .{ .hx = 8, .hy = 12, .mask = &cursor_bsy_mask },
    .{ .hx = 8, .hy = 12, .mask = &cursor_no_mask },
};
const cursor_txt_mask = [_][]const u8{
    "................",
    "................",
    "...##########...",
    "..############..",
    "..#oooooooooo#..",
    "..#oooooooooo#..",
    "..#####oo#####..",
    "....###oo###....",
    ".....##oo##.....",
    ".....##oo##.....",
    ".....##oo##.....",
    ".....##oo##.....",
    ".....##oo##.....",
    ".....##oo##.....",
    ".....##oo##.....",
    ".....##oo##.....",
    "....###oo###....",
    "..#####oo#####..",
    "..#oooooooooo#..",
    "..#oooooooooo#..",
    "..############..",
    "...##########...",
    "................",
    "................",
};
const cursor_lnk_mask = [_][]const u8{
    "................",
    "....#..#........",
    "...######.......",
    "...######.......",
    "...##oo##.##....",
    "...##oo#####....",
    "...##oo#######..",
    "...##oo##o####..",
    "...##oo##o##o#..",
    "...##oo##o##o#..",
    "...#########o#..",
    "...###########..",
    "..##o####oo###..",
    ".###o#oooooo##..",
    ".####ooooooo##..",
    "...##ooooooo##..",
    "...##ooooooo##..",
    "...##ooooooo##..",
    "....#########...",
    ".....#######....",
    "................",
    "................",
    "................",
    "................",
};
const cursor_grb_mask = [_][]const u8{
    "................",
    "................",
    "....#######.....",
    "..#########.....",
    "..####o#######..",
    "..####o#oo####..",
    "..#oo#o#oo#o##..",
    "..#oo#o#oo#o##..",
    "..#oo#o####o##..",
    "..#o##########..",
    "..####ooooo###..",
    "..###ooooooo##..",
    "..##oooooooo##..",
    "..##ooooooooo#..",
    ".###ooooooooo##.",
    ".###ooooooooo#..",
    "..##oooooooo##..",
    "...##ooooooo##..",
    "...###ooooo##...",
    "....########....",
    "......#####.....",
    "................",
    "................",
    "................",
};
const cursor_grg_mask = [_][]const u8{
    "................",
    "................",
    "................",
    "................",
    "................",
    "...##########...",
    "..############..",
    "..############..",
    "..###ooooooo##..",
    "...#oooooooo##..",
    "..##oooooooo##..",
    "..##oooooooo##..",
    "..##ooo###oo##..",
    "..#######ooo##..",
    "..####oo##oo##..",
    "..########oo##..",
    "..#####ooooo##..",
    "...#oooooooo##..",
    "...#oooooooo##..",
    "...##oooooo##...",
    "....#########...",
    ".......###......",
    "................",
    "................",
};
const cursor_rsx_mask = [_][]const u8{
    "................",
    "................",
    "................",
    "................",
    "................",
    "................",
    ".....##..##.....",
    "....###..###....",
    "....########....",
    "...##########...",
    "..##oooooooo##..",
    ".##oooooooooo##.",
    ".##oooooooooo##.",
    "..##oooooooo##..",
    "...##########...",
    "....########....",
    "....###..###....",
    ".....##..##.....",
    "................",
    "................",
    "................",
    "................",
    "................",
    "................",
};
const cursor_rsy_mask = [_][]const u8{
    "................",
    "................",
    ".......##.......",
    "......####......",
    ".....##oo##.....",
    "...###oooo###...",
    "..####oooo####..",
    "..####oooo####..",
    "....##oooo##....",
    "....##oooo##....",
    "....##oooo##....",
    "....##oooo##....",
    "....##oooo##....",
    "....##oooo##....",
    "....##oooo##....",
    "....##oooo##....",
    "..####oooo####..",
    "..####oooo####..",
    "...###oooo###...",
    ".....##oo##.....",
    "......####......",
    ".......##.......",
    "................",
    "................",
};
const cursor_xhr_mask = [_][]const u8{
    "................",
    ".......##.......",
    "......####......",
    "......####......",
    "......####......",
    "......####......",
    "......####......",
    "......####......",
    "......####......",
    "......####......",
    "..############..",
    ".##############.",
    ".##############.",
    "..############..",
    "......####......",
    "......####......",
    "......####......",
    "......####......",
    "......####......",
    "......####......",
    "......####......",
    "......####......",
    ".......##.......",
    "................",
};
const cursor_bsy_mask = [_][]const u8{
    "................",
    "....########....",
    "...##########...",
    "...#oooooooo#...",
    "...##########...",
    "....##oooo##....",
    "....##oooo##....",
    "....##oooo##....",
    "....##oooo##....",
    ".....##oo##.....",
    ".....######.....",
    "......####......",
    "......####......",
    ".....######.....",
    ".....##oo##.....",
    "....##oooo##....",
    "....##oooo##....",
    "....##oooo##....",
    "....##oooo##....",
    "...##########...",
    "...#oooooooo#...",
    "...##########...",
    "....########....",
    "................",
};
const cursor_no_mask = [_][]const u8{
    "................",
    "................",
    "................",
    "................",
    "................",
    "................",
    "......oooo......",
    ".....o####o.....",
    "....########....",
    "...o###ooo##o...",
    "..o#####ooo##o..",
    "..o##o###oo##o..",
    "..o##oo###o##o..",
    "..o##ooo#####o..",
    "...o##ooo###o...",
    "....########....",
    ".....o####o.....",
    "......oooo......",
    "................",
    "................",
    "................",
    "................",
    "................",
    "................",
};
/// M203 (#3338): the f-artcraft #1249 arrow refresh — the same white-fill /
/// dark-outline read, redrawn by the artist at 32x48 and mask-reduced by
/// tools/art/mkcursormask.py (the full set + hotspots land in
/// image/art/cursors/MASKS.TXT; context selection is the followup card).
pub const cursor_mask = [_][]const u8{
    "##..............",
    "###.............",
    "####............",
    "##o##...........",
    "##oo##..........",
    "##ooo##.........",
    "##oooo##........",
    "##ooooo##.......",
    "##oooooo###.....",
    "##ooooooo###....",
    "##oooooooo###...",
    "##ooooooooo###..",
    "##oooooo######..",
    "##ooo#oo#####...",
    "##o####oo#......",
    "#######oo##.....",
    "####..#oo##.....",
    "##....##oo#.....",
    ".......#oo##....",
    ".......#####....",
    "........###.....",
    "................",
    "................",
    "................",
};

/// Card G6 (claim 0487) + WM1 (#707, claim 919): user windows — the
/// draw/window syscall seam. EIGHT user windows (ids 2..9,
/// `user_window_id_base`), each with a per-window B8G8R8X8 back-buffer
/// exactly win.w × win.h, carved from the kernel page pool at
/// `sys_win_open` and freed at close/`close_owner` (near-zero standing
/// BSS — the fixed `user_bufs` BSS is gone). An EL0 program renders into
/// its buffer through `sys_win_fill` / `sys_win_present`; the kernel owns
/// the pages. Zero new slots.
pub const user_window_id_base: u8 = 2;
pub const user_windows_max: usize = 8;
/// WM1: the absolute per-window geometry cap is the scanout itself (a
/// window must fit on screen anyway — `user_open` enforces x+w/y+h). The
/// binding memory constraint is pool availability (`.nomem`), not BSS.
/// (The 512×424 cap died with the fixed BSS buffers; the WM-side mirror
/// `wnd_core.user_buf_w/h` proposal clamp is WM2/WM3 follow-up.)
pub const user_win_max_w: u32 = virtio_gpu.fb_width;
pub const user_win_max_h: u32 = virtio_gpu.fb_height;

/// Step 8 (Issue #211): the system taskbar at the bottom of the scanout.
/// M32 WMS5 Gate 2 (claim 4278): single-sourced with the WM server —
/// wnd_core owns the number; this re-exports it (the drift guard).
pub const taskbar_h: u32 = geom.taskbar_h;
pub const taskbar_y: u32 = virtio_gpu.fb_height - taskbar_h;
pub const taskbar_bg_rgb: u32 = 0x0f172a;
pub const taskbar_entry_active_rgb: u32 = 0x3b82f6;
pub const taskbar_entry_dimmed_rgb: u32 = 0x1e293b;

/// Arc2 W3 (claim 1264, #226): system tray — right 80px of 20px taskbar at y=700.
/// HH:MM from tick, D/L/A theme letter in accent, filled/empty clipboard rect.
/// Clock ticks on composite() without timer; migrates Kind.clock id 1 (no duplicate).
pub const tray_w: u32 = 80;
pub const tray_x: u32 = virtio_gpu.fb_width - tray_w;
pub const tray_h: u32 = taskbar_h;
pub const tray_y: u32 = taskbar_y;

/// M15 C4 (Dock, #229): 24 px left dock, topmost fixed layer. M32 WMS5
/// Gate 2 (claim 4278): single-sourced with the WM server via wnd_core.
pub const dock_w: u32 = geom.dock_w;
pub const dock_x: u32 = 0;
pub const dock_y: u32 = 0;
pub const dock_h: u32 = virtio_gpu.fb_height - taskbar_h;
pub const dock_bg_rgb: u32 = 0x0f172a;
pub const dock_icon_bg_rgb: u32 = 0x1e293b;
pub const dock_icon_active_rgb: u32 = 0x3b82f6;

/// Step 9 (Issue #212): the desktop wallpaper gradient.
pub const wallpaper_top_rgb: u32 = 0x1a1a2e;
pub const wallpaper_bot_rgb: u32 = 0x0a0a14;

/// The window-kind tags (the terminal and the demo window).
pub const Kind = enum {
    terminal,
    clock,
    user,
    taskbar,
    wallpaper,
    dock,
};

/// One window in the registry. Value type; the registry is a fixed BSS
/// array. `title` is a static string literal (data, never a function
/// pointer — safe at any load base).
pub const Window = struct {
    id: u8,
    title: []const u8,
    x: u32,
    y: u32,
    w: u32,
    h: u32,
    kind: Kind,
    visible: bool,
    dirty: bool,
    /// M33 SB4 (claim 2382): rect-granular damage. `damaged` means the
    /// tracked `dx/dy/dw/dh` union is valid; when false the whole window
    /// is treated as dirty (the pre-SB4 whole-window repaint), so every
    /// existing `.dirty = true` site keeps its current behavior. `user_fill`
    /// sets the EXACT written rect so composite repaints only that region.
    damaged: bool = false,
    dx: u32 = 0,
    dy: u32 = 0,
    dw: u32 = 0,
    dh: u32 = 0,
    /// M33 SB4 (claim 2382): the LAST damage rect composite consumed (what
    /// paint() actually repainted) — populated with the exact rect for a
    /// partial repaint; 0,0,0,0 means no partial repaint was recorded yet
    /// (whole-window or never). Makes the rect-granular gate observable on
    /// serial even AFTER the drain consumes the pending damage (no race).
    last_dx: u32 = 0,
    last_dy: u32 = 0,
    last_dw: u32 = 0,
    last_dh: u32 = 0,
    /// Card G6 teardown follow-on (per-process ownership): the owning
    /// process id for a `.user` window (null for the fixed terminal +
    /// clock, which are kernel-owned). The syscall layer records the
    /// caller here at `sys_win_open`, restricts fill/present/close to the
    /// owner, and the exit path auto-closes the owner's windows via
    /// `close_owner` — a real teardown semantic instead of a window that
    /// leaks until reboot.
    owner: ?usize = null,
    /// #2865: a WINDOWS.SAV replay — `restore_state` recreated this user
    /// window from the previous boot's records under the sentinel owner,
    /// so no live process will ever fill or close it. A phantom is a live
    /// window in every respect except persistence: `serialize_state`
    /// skips it (the save file describes LIVE windows — re-serializing a
    /// replay would stack its record on top of the app's fresh windows
    /// every boot until all eight slots were ghosts, the observed leak)
    /// and a real `user_open` evicts the bottom-most one before reporting
    /// `.full`.
    phantom: bool = false,
    /// Arc4 #239: animated window fade-in. 0=none (fully opaque),
    /// 1=fade-in active. The paint path applies per-pixel alpha
    /// blending during the fade frames.
    fade_phase: u8 = 0,
    fade_tick: u8 = 0,
    /// Arc4 #241: workspace assignment. 0..2 = workspace, fixed layers
    /// are always workspace 0 (visible on all).
    workspace: u8 = 0,
    /// Arc4 #242: unsaved-changes flag. The compositor shows a
    /// confirmation dialog when the user clicks close on a dirty window.
    unsaved: bool = false,
    /// M21 W3: minimized flag. Minimized windows don't paint, don't
    /// receive events, and are skipped by Alt+Tab. The dock icon shows
    /// the restore action when minimized.
    minimized: bool = false,
    /// M21 W6: maximized flag. Maximised windows fill the workspace area
    /// (screen minus taskbar and dock). Title bar stays visible.
    maximized: bool = false,
    /// M21 W8: always-on-top flag. Always-on-top windows render above
    /// all normal windows in the z-order, regardless of focus.
    always_on_top: bool = false,
    /// M21 W15: modal flag. Modal windows block input to windows below.
    /// Used by dialogs, context menus, dropdowns, popups.
    modal: bool = false,
    /// M21 W16: transient flag. Transient windows are short-lived popups
    /// that auto-close on timeout or click-outside.
    transient: bool = false,
    transient_timeout: u32 = 0,
    /// M21 W12: dynamic title buffer. Apps set this via sys_win_set_title.
    /// `title` points here when a dynamic title is set; otherwise it
    /// points to a static string literal. 64 bytes — enough for "NOTEPAD -
    /// filename.txt (*)" plus room.
    title_buf: [64]u8 = @as([64]u8, @splat(@as(u8, 0))),
    title_len: u8 = 0,
    /// M37 DQ2 (issue #840): tab-group fact mirrored from the WM's
    /// validated ATTACH_TAB/DETACH_TAB calls (the WM owns grouping and
    /// layout; the kernel records which container a window is attached to
    /// so draw_chrome can paint the strip — facts, not policy, exactly
    /// like the tooltip text the WM pushes for the kernel to blit).
    /// 0 = standalone/container. Orphans reset here on close.
    tab_parent: u8 = 0,
    /// M32 WMS4 (issue #624): the WM server's chrome descriptor for THIS
    /// window (a per-window SET_WINDOW override). When invalid, the draw
    /// path falls back to the broadcast policy, then to the shim's own
    /// rules — the two paths coexist behind the presence of WM chrome.
    chrome_valid: bool = false,
    chrome: geom.ChromeDesc = undefined,
    /// M33 SB3 (claim 9361): when a `.user` window is SURFACE-BACKED, its
    /// rendering lives in a shared-anonymous region (M33_MAP_SHARED) instead
    /// of the kernel `user_bufs[id]` copy. The kernel records the region's
    /// handle + physical base here so `composite()` blits from the surface's
    /// OWN pages (identity-mapped into the kernel root) and the registered WM
    /// mirrors the same region RO. 0 = unmigrated (frozen kernel-buffer path,
    /// byte-identical to pre-SB3). The surface is owned by the WINDOW's owner
    /// pid; close/owner-exit drops the binding and revokes the WM mirror.
    surface_handle: u32 = 0,
    /// Physical base of the shared surface (for composite's direct blit).
    surface_pa: u64 = 0,
    /// Page count of the shared surface (for composite's source bounds).
    surface_pages: u32 = 0,
    /// WM1 (#707, claim 919): the pool-backed kernel back-buffer, exactly
    /// win.w × win.h B8G8R8X8. `kbuf_pa` is the pool physical base (the
    /// kernel is identity-mapped, so it is directly dereferenceable on
    /// device); `kbuf_pages` is the accounted span for the free path.
    /// pa == 0 means no buffer (fixed windows, or a `.user` between struct
    /// init and `user_open` storing the allocation — never observable).
    kbuf_pa: u64 = 0,
    kbuf_pages: u32 = 0,
    /// WM1 host-test seam: `zig test` cannot dereference pool physical
    /// addresses, so the CPU-visible bytes live in the test-arena bump
    /// allocation instead (freed implicitly by the per-test reset in
    /// arm()). Production builds never read it; `kbuf_ptr` selects.
    /// 8 B × 13 windows of BSS — noise against the removed 3.3 MB.
    kbuf_test: ?[*]u8 = null,
};

/// Arc4 #239: fade-in constants. The window is at 25% opacity for
/// `fade_half_frames` composites, then 50% for the same count, then
/// fully opaque. Total fade-in = 2 × fade_half_frames composites.
pub const fade_half_frames: u8 = 2;

// ---------------------------------------------------------------------------
// Registry state (fixed BSS)
// ---------------------------------------------------------------------------

pub var windows: [max_windows]Window = undefined;
pub var win_count: usize = 0;
/// Focused window id (stable across raises); 0xff = none.
pub var focused_id: u8 = 0xff;
pub var armed_global: bool = false;
/// M32 WMS5 (issue #625, claim 9849): while a WM is registered it owns
/// pointer geometry — `pointer_tick` keeps tracking the cursor (a kernel
/// blit surface) but skips ALL geometry consumption (drag, resize, snap,
/// focus-at, minimize/close buttons) and instead fans the raw stream out
/// through `wm_pointer_hook`. Flipped by `wm_server.register/unregister`;
/// false in shim mode (the default VM) → byte-identical to pre-WMS5.
pub var wm_owns_input: bool = false;
/// M33 SB5 (claim 7397): the registered WM owns the migrated USER layer —
/// true once the WM has bound the scanout (wm_server.scanout_bind) and false
/// after teardown/unbind. While true, paint_scene() SKIPS surface-backed
/// (migrated) user windows: their bytes are already in the scanout via the
/// WM's compose-N stores (written between the COMPOSITE_TICK and the final
/// present), so a kernel blit would double-draw them.
pub var wm_owns_user_layer: bool = false;
/// M33 SB6 (claim 6864): how many `.user` windows paint_scene actually
/// BLITTED (the pre-seam-B composite cost) vs how many surface-backed
/// (migrated) windows were SKIPPED while the WM owns the user layer (the
/// seam-B saving). Monotonic per boot; the gate diffs snapshots.
pub var user_blits: u64 = 0;
pub var migrated_skips: u64 = 0;
/// The raw-pointer fan-out (set by wm_server at REGISTER time; null when
/// no WM — the hook style of `events.on_event_pushed`). Callback gets the
/// mapped fb pixels + the raw HID button byte + the keyboard modifier
/// byte live at the sample's decode (the capture-time snapshot the seat
/// echoes back through slot-65 cmd 15 — #2896); the WM edge-detects.
pub var wm_pointer_hook: ?*const fn (x: u32, y: u32, buttons: u8, mods: u8) void = null;
/// The window-registry mirror fan-out (set by wm_server at REGISTER time;
/// null when no WM). Callback gets the id + full rect + state; wm_server
/// packs kind 20 WM_WINDOW. Called from the user-window mutation points.
/// M42 UX (2026-09-05): `released` marks a RELEASE mirror — the window has
/// left the registry (app self-exit / close_owner / user_close), not a
/// mere visibility change; wm_server encodes it as flags bit 13 (additive;
/// every existing bit is unchanged).
pub var wm_window_hook: ?*const fn (id: u8, x: u32, y: u32, w: u32, h: u32, visible: bool, focused: bool, workspace: u8, unsaved: bool, released: bool) void = null;
/// WMS5 Gate 2 (claim 4278): the raw-keyboard fan-out (set by wm_server at
/// REGISTER time; null when no WM — the hook style of the pointer/window
/// hooks). Callback gets the raw HID keyboard usage byte + the ADR 0009
/// modifier bits on key-DOWN edges; wm_server packs kind 21 WM_KEY. The
/// kernel's own keyboard geometry consumers are gated behind
/// `wm_owns_input` (shell idle), so the WM — not the kernel — decides.
pub var wm_key_hook: ?*const fn (usage: u8, flags: u16) void = null;
/// card #3034 (ADR 0007 X4): the user-surface damage fan-out (set by
/// wm_server at REGISTER time; null when no WM — the hook style of the
/// pointer/key/window seams). Fires on each user-surface damage-marking
/// edge (`mark_damage` on a `.user` window, `user_present`); wm_server
/// decides whether the seat is owed a kind-18 flags-bit0 damage tick —
/// at most one pending per seat, cleared when its REQUEST_PRESENT lands.
pub var wm_damage_hook: ?*const fn () void = null;
/// #3759 (final item — the drag.zig drain): the drag-ARM mirror fan-out
/// (set by wm_server at REGISTER time; null when no WM — the hook style of
/// the pointer/key/window seams). While a seat owns input an app's
/// `sys_drag_start` stages into `drag_armed_*` and this hook fires once
/// with the staged size + source pid; wm_server packs kind 22 WM_DRAG.
/// The seat TAKEs the record (slot-65 DRAG cmd 27) and owns it end to end.
pub var wm_drag_hook: ?*const fn (payload_len: usize, source_pid: usize) void = null;
/// M32 WMS4: the WM's broadcast chrome policy (SET_WINDOW with a0 = ALL).
/// When set, every user window without its own override renders chrome
/// from this descriptor; null = no WM chrome (shim rules — the default
/// VM, zero-regression). New windows inherit it automatically via the
/// draw-time fallback.
pub var wm_chrome_policy: ?geom.ChromeDesc = null;
/// Scanout presents pushed by the compositor since arm.
pub var presents: usize = 0;

/// Arc4 #241: virtual desktop workspaces. Three workspaces (0, 1, 2).
/// The current workspace controls which user windows are visible.
/// Fixed layers (terminal, wallpaper, taskbar, dock) are visible on all.
pub const workspace_max: u8 = 3;
pub var current_workspace: u8 = 0;

/// The clock's back-buffer (fixed BSS, contiguous B8G8R8X8). The
/// compositor blits it over the terminal.
pub var clock_buf: [clock_w * clock_h * 4]u8 = undefined;
/// Card G6 (claim 0487): the user windows' back-buffers are WM1
/// pool-backed (see the `kbuf_*` Window fields) — no fixed BSS here.
/// WM1 host-test arena (compiled out on device): `zig test` cannot
/// dereference pool physical addresses, so the CPU-visible window bytes
/// live here instead. A bump allocator — free is a no-op and `arm()`
/// resets it per test, so cross-test leaks are impossible by
/// construction (std.testing.allocator would panic on free-after-leak-
/// report, verified empirically). 16 MB fits the hungriest single test
/// (8 windows + a fullscreen one ≈ 7 MB). Pool ACCOUNTING stays real —
/// alloc_pages/free_pages run in both builds and the close tests assert
/// the free-count round-trips.
pub const test_arena = if (builtin.is_test) struct {
    pub var buf: [16 * 1024 * 1024]u8 = undefined;
    pub var fba: std.heap.FixedBufferAllocator = undefined;
    pub var ready: bool = false;
} else struct {};

/// WM1: the CPU-visible base of a `.user` window's kernel back-buffer —
/// the pool pages (the kernel identity-maps physical RAM, so `kbuf_pa`
/// dereferences directly) on device, the test-arena bytes on host. All
/// readers (fill, composite, preview) go through this so the host tests
/// prove the same pixel path the device executes.
pub fn kbuf_ptr(win: *const Window) [*]u8 {
    if (builtin.is_test) return win.kbuf_test orelse unreachable;
    return @as([*]u8, @ptrFromInt(win.kbuf_pa));
}

/// WM1: the byte length of a window's kernel back-buffer (w×h×4).
pub fn kbuf_bytes(w: u32, h: u32) usize {
    return @as(usize, w) * @as(usize, h) * 4;
}

/// WM1: pages spanned by `nbytes` (round up to the 4 KiB page).
pub fn kbuf_pages_for(nbytes: usize) u32 {
    return @intCast((nbytes + alloc.page_size - 1) / alloc.page_size);
}
/// The clock's displayed tick (so it only repaints when the second changes).
pub var clock_shown_tick: u64 = 0;
pub var clock_has_tick: bool = false;

/// Card U4 (claim 4993): the pointer cursor's framebuffer position. Hidden
/// until the first pointer report arrives (the default VM has no pointer).
pub var cursor_x: u32 = 0;
pub var cursor_y: u32 = 0;
pub var cursor_shown: bool = false;
pub const CursorRect = struct { x: u32, y: u32, w: u32, h: u32 };
pub var cursor_old_damage: ?CursorRect = null;
pub var cursor_new_damage: ?CursorRect = null;
pub var prev_ptr_buttons: u8 = 0;

/// M32 WMS8 Gate 6 (issue #628): title-bar drag + snap-on-drop state is
/// DELETED — WMS5 proved the WM owns pointer GEOMETRY: while registered it
/// fans the raw pointer (kind 19), hit-tests the title bar, and issues
/// SET_WINDOW rects (`user_move`); on release it snaps via its own mirror
/// + the shared wnd_core rule. With a WM seated the kernel's drag_id /
/// snap_zone tracking below was provably dormant (pointer_tick returns
/// early), so it is removed. RESIZE stays (the WM has no resize path).
/// Arc2 W1 (claim 3589, #224): drag-to-resize — 6×6 bottom-right corner.
/// Bounds + hit size are the shared wnd_core policy (single source with
/// the WM server).
pub const resize_min_w: u32 = geom.resize_min_w;
pub const resize_min_h: u32 = geom.resize_min_h;
pub const resize_hit_size: u32 = geom.resize_hit_size;
pub var resize_id: ?u8 = null;
pub var resize_start_x: u32 = 0;
pub var resize_start_y: u32 = 0;
pub var resize_origin_w: u32 = 0;
pub var resize_origin_h: u32 = 0;

/// Arc4 #237: drag-and-drop state — BSS, no heap.
/// The drag payload is copied from the source app via sys_drag_start,
/// then delivered to the target window on DROP via uaccess.
pub const drag_payload_max: usize = 512;
pub var drag_payload: [drag_payload_max]u8 = undefined;
pub var drag_payload_len: usize = 0;
pub var drag_active: bool = false;
pub var drag_source_pid: usize = 0;
pub var drag_over_id: ?u8 = null; // window currently under pointer during drag
/// #3759 (final item): the ARM staging — the source's `sys_drag_start`
/// record while a seat owns input. The seat TAKEs it (slot-65 DRAG cmd 27
/// act 0) and owns the record end to end from there; the delivery staging
/// above is written again only at the seat's DROP decision. Two stagings,
/// one direction each — a fresh arm never clobbers a pending delivery.
pub var drag_armed: bool = false;
pub var drag_armed_payload: [drag_payload_max]u8 = undefined;
pub var drag_armed_len: usize = 0;
pub var drag_armed_source_pid: usize = 0;

/// Arc4 #242: unsaved-changes confirmation dialog state — BSS, no heap.
/// M32 WMS8 Gate 4 (issue #628): the WM — not the kernel — decides WHEN the
/// dialog appears (a dirty window's close) and WHICH button applies; the
/// kernel applies through the primitives below (cmd-11 DIALOG actions
/// 3-6). The 5-tick auto-close timeout is DELETED (the WM decides duration).
pub var unsaved_dialog_open: bool = false;
pub var unsaved_dialog_target: u8 = 0; // window id being closed

/// M15 C2 (Alt+Tab overlay, #225): hold-Alt cycling UI state — BSS, no heap.
/// #3759 (third item): the no-seat SHIM's switcher bookkeeping (the kernel
/// fallback). A bound seat holds its own snapshot/ordering and commits
/// through `alt_tab_wm_commit`; nothing seat-side writes this state.
pub var overlay_active: bool = false;
pub var overlay_selected: usize = 0;
pub var overlay_ids: [max_windows]u8 = undefined;
pub var overlay_count: usize = 0;

/// M32 WMS8 Gate 6 (issue #628): the drag-snap state (snap_zone, the
/// snap_last_*/snap_snapped per-window arrays, and the applied
/// snap_window/snap_restore primitives) is DELETED — the WM owns snap-on-drop
/// via its own mirror + the shared wnd_core rule (wnd_core.snap_zone_for_point/
/// snap_zone_bounds stay, single-source for the WM). With a WM seated this
/// kernel snap state was dormant behind the early-returning pointer_tick.
/// M21 W1/W2: tiling state — BSS, no heap.
/// `tile_mode` is true when the focused workspace uses tiling layout.
/// `tile_master_id` / `tile_stack_id` are the window ids assigned to
/// master and stack roles. When tiling is toggled, the compositor
/// recalculates window rects. The split ratio is 2/3 master, 1/3 detail.
pub var tile_mode: bool = false;
pub var tile_master_id: ?u8 = null;
pub var tile_stack_id: ?u8 = null;
/// M21 W2: master side — true = master on left (default), false = right.
pub var tile_master_side: bool = true;
/// M21 W2: master/detail split ratio (comptime). 2/3 : 1/3.
pub const tile_master_pct: u32 = 667; // per mille (66.7%)
/// M21 W3: per-window minimized state. When a window is minimized,
/// its previous rect is saved here for restore.
pub var minimize_prev_x: [user_windows_max]u32 = @as([user_windows_max]u32, @splat(@as(u32, 0)));
pub var minimize_prev_y: [user_windows_max]u32 = @as([user_windows_max]u32, @splat(@as(u32, 0)));
pub var minimize_prev_w: [user_windows_max]u32 = @as([user_windows_max]u32, @splat(@as(u32, 0)));
pub var minimize_prev_h: [user_windows_max]u32 = @as([user_windows_max]u32, @splat(@as(u32, 0)));
pub var minimize_prev_valid: [user_windows_max]bool = @as([user_windows_max]bool, @splat(@as(bool, false)));

/// M21 W6: per-window pre-maximize rect storage.
pub var pre_max_x: [user_windows_max]u32 = @as([user_windows_max]u32, @splat(@as(u32, 0)));
pub var pre_max_y: [user_windows_max]u32 = @as([user_windows_max]u32, @splat(@as(u32, 0)));
pub var pre_max_w: [user_windows_max]u32 = @as([user_windows_max]u32, @splat(@as(u32, 0)));
pub var pre_max_h: [user_windows_max]u32 = @as([user_windows_max]u32, @splat(@as(u32, 0)));
pub var pre_max_valid: [user_windows_max]bool = @as([user_windows_max]bool, @splat(@as(bool, false)));

/// M21 W7: fullscreen state — BSS, no heap.
pub var fullscreen_active: bool = false;
pub var fullscreen_window_id: ?u8 = null;

/// Arc2 W3 (claim 1264, #226): system tray state — fixed BSS, ~32B, zero heap.
/// Right 80px of 20px taskbar at y=700: HH:MM from tick, D/L/A theme letter
/// in accent, filled/empty clipboard rect. Clock ticks on composite() without
/// timer; migrates Kind.clock id 1 (no duplicate tray-vs-clock).
pub var tray_tick: u64 = 0;
pub var tray_has_tick: bool = false;
pub var tray_last_theme: u8 = 0xff;
pub var tray_last_clip_len: usize = 0;
/// M32 WMS6 Gate E (issue #626): WM-declared tray widget content — the
/// clock string, theme letter, and clipboard indicator the registered WM
/// owns (the final WMS6 chrome gate). When a field's `_set` flag is true
/// the renderer draws the WM's value; otherwise it falls back to the
/// shim's derived value (no-WM mode is byte-identical). Reset by
/// clear_wm_chrome on WM teardown.
pub var wm_tray_clock_text: [5]u8 = .{ '0', '0', ':', '0', '0' };
pub var wm_tray_clock_set: bool = false;
pub var wm_tray_theme: u8 = 'D';
pub var wm_tray_theme_set: bool = false;
pub var wm_tray_clip: bool = false;
pub var wm_tray_clip_set: bool = false;

/// Arc4 #240/M21 W5: desktop notification toasts — bounded BSS FIFO,
/// 10 entries (expanded from 4 for the notification center). Follows the
/// exit-report FIFO pattern (M3 claim 1014). Apps post via sys_notify;
/// the compositor renders top-right and auto-dismisses. The notification
/// center (W5) shows the last 10 in a pull-out panel.
pub const notify_max: usize = 10;
pub const notify_text_max: usize = 280;
pub const notify_dismiss_ticks: u32 = 5;
pub var notify_texts: [notify_max][notify_text_max]u8 = undefined;
pub var notify_lens: [notify_max]usize = @as([notify_max]usize, @splat(@as(usize, 0)));
pub var notify_levels: [notify_max]u8 = @as([notify_max]u8, @splat(@as(u8, 0)));
pub var notify_head: usize = 0;
pub var notify_count: usize = 0;

/// M21 W5: notification center panel state — BSS, no heap.
/// The panel opens on tray clock click and shows the last 10 notifications.
pub var notif_center_open: bool = false;
/// #3759 (fourth item): the panel geometry is single-sourced in wnd_core —
/// the seat's hit-test rule and draw_chrome's rects cannot drift.
pub const notif_center_w: u32 = geom.notif_center_w;
pub const notif_center_h: u32 = 400;
pub var notify_ticks: [notify_max]u32 = @as([notify_max]u32, @splat(@as(u32, 0)));

/// M27 G6: tooltip state — BSS, no heap. M32 WMS8 Gate 1 (claim 4270): the
/// dwell-decision is gone — the WM owns WHEN a tooltip shows via TOOLTIP
/// (cmd 8); the kernel keeps only the clamp + place + blit surface (visible,
/// position, text). No kernel hover timer remains.
pub var tooltip_visible: bool = false;
pub var tooltip_x: u32 = 0;
pub var tooltip_y: u32 = 0;
pub var tooltip_text: [32]u8 = undefined;
pub var tooltip_text_len: u8 = 0;

/// M27 G2: about dialog state — BSS, no heap.
pub var about_dialog_open: bool = false;

/// M27 G3: alt-tab preview buffer — 64×48 B8G8R8X8 (12,288 bytes).
/// Nearest-neighbor scaled from each window's framebuffer content.
pub const preview_w: u32 = 64;
pub const preview_h: u32 = 48;
pub var preview_buf: [preview_w * preview_h * 4]u8 = undefined;

/// Step 7 (Issue #207): theme selection. 0=dark, 1=light, 2=amber.
/// Read by the compositor chrome pass for title bars, clock, focus ring.
/// M73m (#1662): 3 = `custom` — the user's own palette, resolved from the
/// three fields below (store keys `palette_fg`/`palette_bg`/`palette_accent`,
/// applied by settings.zig's apply chain). Every chrome accessor still
/// switches 1/2/else, so an unknown id lands on the dark arm — a custom
/// palette can never paint garbage, and a fresh image (theme "dark") never
/// reads these fields at all.
pub var theme_id: u8 = 0;

/// M73m (#1662): the custom palette the resolver prefers when
/// `theme_id == theme_id_custom`. Compiled defaults ARE the dark values
/// (`fbtext.fg_rgb`/`bg_rgb`, dark's accent), so a `theme=custom` boot with
/// nothing of its own written resolves byte-identically to the boot theme.
pub const theme_id_custom: u8 = 3;
pub var custom_fg: u32 = fbtext.fg_rgb;
pub var custom_bg: u32 = fbtext.bg_rgb;
/// Dark's focus-ring / active taskbar-entry accent (#207, #1662).
pub var custom_accent: u32 = 0x3b82f6;

// ---------------------------------------------------------------------------
// Arm / query
// ---------------------------------------------------------------------------

/// Arm the window manager: register the terminal (window 0, full screen)
/// and the fixed chrome (wallpaper, taskbar, dock). The clock window
/// (Kind.clock id 1) is migrated to the taskbar tray (Arc2 W3) — no duplicate
/// window. Focus the terminal, mark dirty so first composite paints the scene.
pub fn arm() void {
    // WM1 (#707, claim 919) host-test scaffolding (compiled out on
    // device — production arm() runs once at boot on a fresh pool):
    // reset the test arena (bump allocator — frees are no-ops, so every
    // test starts with a full arena and cross-test leaks are impossible),
    // then re-arm a generous fake pool (host writes never touch these
    // phys addresses — `kbuf_test` carries the CPU-visible bytes — so the
    // span costs nothing; 65536 pages covers 8 fullscreen windows).
    if (builtin.is_test) {
        if (!test_arena.ready) {
            test_arena.fba = std.heap.FixedBufferAllocator.init(&test_arena.buf);
            test_arena.ready = true;
        }
        test_arena.fba.reset();
        var desc = [_]memmap.MemoryDescriptor{
            .{ .type = .conventional_memory, .physical_start = 0x100000, .virtual_start = 0, .number_of_pages = 65536, .attribute = 0 },
        };
        const view = memmap.MapView.init(std.mem.asBytes(&desc), @sizeOf(memmap.MemoryDescriptor), desc.len);
        _ = alloc.init(view, &.{});
    }
    win_count = 0;
    // M102d1 (#2166): a registry reset strands no GPU3D context — the
    // broker's records bind to window ids, so a fresh window registry
    // means a fresh context pool too (pure BSS wipe, mirrors c1's
    // reset_state contract; nothing is live at boot).
    gpu3d_broker.reset_state();
    // #3759 (fifth item): a fresh boot gets a fresh replay budget — the
    // once-per-boot WINDOWS.SAV consumption guard re-arms here (arm()
    // runs once at boot on device and per simulated boot in host tests).
    persist_mod.replay_done = false;
    windows[win_count] = .{
        .id = 0,
        .title = "roadpops",
        .x = 0,
        .y = 0,
        .w = virtio_gpu.fb_width,
        .h = virtio_gpu.fb_height,
        .kind = .terminal,
        .visible = true,
        .dirty = true,
    };
    win_count += 1;
    // Arc2 W3: Kind.clock id 1 deprecated — tray clock in taskbar replaces it.
    // No window created for clock; tray state holds HH:MM.
    // Step 9: wallpaper window (id 254) — gradient background between terminal and taskbar.
    windows[win_count] = .{
        .id = 254,
        .title = "wallpaper",
        .x = 0,
        .y = 0,
        .w = virtio_gpu.fb_width,
        .h = virtio_gpu.fb_height,
        .kind = .wallpaper,
        .visible = true,
        .dirty = true,
    };
    win_count += 1;
    // Step 8: taskbar window (id 255) — always topmost.
    windows[win_count] = .{
        .id = 255,
        .title = "taskbar",
        .x = 0,
        .y = taskbar_y,
        .w = virtio_gpu.fb_width,
        .h = taskbar_h,
        .kind = .taskbar,
        .visible = true,
        .dirty = true,
    };
    win_count += 1;
    // M15 C4: dock window (id 253) — 24 px left bar, always visible.
    windows[win_count] = .{
        .id = 253,
        .title = "dock",
        .x = dock_x,
        .y = dock_y,
        .w = dock_w,
        .h = dock_h,
        .kind = .dock,
        .visible = true,
        .dirty = true,
    };
    win_count += 1;
    focused_id = 0;
    armed_global = true;
    presents = 0;
    clock_shown_tick = 0;
    clock_has_tick = false;
    tray_tick = 0;
    tray_has_tick = false;
    tray_last_theme = 0xff;
    tray_last_clip_len = 0;
    // M32 WMS6 Gate E: no WM-declared tray content at boot.
    wm_tray_clock_set = false;
    wm_tray_theme_set = false;
    wm_tray_clip_set = false;
    cursor_shown = false;
    pointer_mod.cursor_drawn = false;
    cursor_shape = .ptr;
    cursor_old_damage = null;
    cursor_new_damage = null;
    prev_ptr_buttons = 0;
    pointer_mod.wm_fan_valid = false;
    resize_id = null;
    // M52 card 3 (#1240): a fresh registry holds no drag — the Arc4 #237
    // capture is process-scoped BSS and must not survive a re-arm (a stuck
    // capture would otherwise outlive the process it named).
    drag_active = false;
    drag_payload_len = 0;
    drag_source_pid = 0;
    drag_over_id = null;
    // #3759 (final item): the seat-side arm staging clears with the same
    // stroke — a record waiting for a seat's TAKE is process-scoped BSS
    // too and must not survive a re-arm either.
    drag_armed = false;
    drag_armed_len = 0;
    drag_armed_source_pid = 0;
    overlay_active = false;
    overlay_count = 0;
    overlay_selected = 0;
    presentation_trace_arm();
}

pub fn armed() bool {
    return armed_global;
}

pub fn count() usize {
    return win_count;
}

pub fn presents_pushed() usize {
    return presents;
}

pub fn window_at(i: usize) ?*const Window {
    if (i >= win_count) return null;
    return &windows[i];
}

pub fn kind_name(kind: Kind) []const u8 {
    return switch (kind) {
        .terminal => "terminal",
        .clock => "clock",
        .user => "user",
        .taskbar => "taskbar",
        .wallpaper => "wallpaper",
        .dock => "dock",
    };
}

// ---------------------------------------------------------------------------
// WM2 mission-control overview (Self-hosting Lane 1, issue #707 card 2).
// A hotkey-summoned grid of the current workspace's user windows: click a
// card to focus+raise it, drag a card onto a workspace-strip button to move
// it there (and switch to it), Esc/hotkey to exit. The GRID GEOMETRY is the
// shared wnd_core rule (geom.overview_grid / card_rect / ws_button_rect) —
// the seat renders these rects and hit-tests the SAME rects, so the two
// cannot disagree. Policy (which card was clicked) arrives over slot-65
// OVERVIEW (cmd 21); the kernel only applies the primitives.
//
// #3759 (second item): the grid's render + bookkeeping are the seat's own —
// cmd 21 was always seat-mandatory (ENOSYS unregistered, no shim chord), so
// the kernel needed no fallback and holds NO overview state: no card
// snapshot, no open flag, no grid paint. What stays are the kernel facts the
// seat's decisions apply (focus+raise, move-to-workspace + switch) and the
// M27 G3 dock-preview painter (a kernel painter striding pool/surface pages
// the seat cannot map).
// ---------------------------------------------------------------------------

/// Whether the last paint_scene() actually repainted anything (drives the
/// clean-scene no-flush decision in composite()). Read by composite() only.
pub var scene_dirty: bool = false;

/// Opt-in, guest-only presentation evidence. The exact fixture consent is
/// required; ordinary boots neither record scanouts nor perform capture IO.
pub var presentation_trace: bool = false;

/// One transport seam for completed shim/seat frames, with injectable hardware
/// results in host tests. Success means BOTH transfer and flush completed.
pub var transfer_hook: ?*const fn () virtio_gpu.CmdResult = null;
pub var flush_hook: ?*const fn () virtio_gpu.CmdResult = null;

// ---------------------------------------------------------------------------
// M76a (#1674): the splash-to-seat handoff hold.
//
// render_splash leaves `splash_hold` standing: while it stands,
// composite() refuses every kernel present (no transfer+flush), so the
// splash frame stays exactly where the last flush put it — the tee keeps
// writing the text layer into the framebuffer underneath, but the pixels
// do not move until the handoff resolves. The hold ends on the first of:
//
//   * the seat binding the scanout (`wm_owns_user_layer`, set by
//     wm_server.scanout_bind) — the seat paints through its OWN present
//     path (wm_server.request_present), which is never suppressed, so the
//     screen goes splash -> seat in one frame with zero kernel-art frames
//     between. The serial ordering for that happy path is carried by the
//     seat's own markers (go-dogfood: `wm: autostart` ->
//     `gotabwm: registered` -> `gotabwm: seat-taken`).
//   * the first user present in a boot whose seat question already
//     settled-negative (#3731, `seat_impossible`): on a shim-only boot
//     the app's own frame is the successor content — the same silent
//     one-frame handoff as the seat bind. Without it an app whose first
//     present lands inside the window (routine since card #3036 made
//     sub-second sleeps precise) stays invisible until the bound.
//   * the BOUND: `splash_bound_ticks` timer ticks (1 Hz) after the first
//     idle evaluation — one honest klog line naming what was waited for,
//     then Road Pops presents as before. The bound is stated in (and
//     gated by) tools/gate/specs/live-roadpops.spec. Every no-seat
//     outcome lands exactly as today, honestly and on time: share mode
//     `none` (`GOTABWM.ELF not on the share`), `settings wm none`, and a
//     seat that loads but never binds all reach the bound and say so.
//
// Headless boots never engage the hold: render_splash early-returns when
// the gpu never armed, and the hold is only set on the path that actually
// pushed a splash frame. No timing is tuned by feel — the bound is one
// named constant, and the spec asserts both its arithmetic and its line.
// The M59 invariant is untouched: this changes pixels only; the `wm` and
// `shell` compiled defaults, the monitor, and the serial seam are all
// exactly where they were (every line still reaches klog/uart first).
// ---------------------------------------------------------------------------

/// Whether the splash frame is still holding the scanout.
pub var splash_hold: bool = false;
/// #3731: the seat question resolved to "no seat can bind this boot" —
/// share mode `none`, `settings wm none`, or the seat program missing /
/// failing to load (set by the shell's autostart path). While a seat may
/// still come the hold keeps the one-frame handoff; once the question is
/// settled-negative, a user window's first present IS the successor
/// content the bound was waiting for.
pub var seat_impossible: bool = false;

// ---------------------------------------------------------------------------
// Seam re-exports (#3754): the module root keeps the public surface flat —
// every decl below moved verbatim into kernel/src/driving_award/<seam>.zig.
// ---------------------------------------------------------------------------
const focus_mod = @import("driving_award/focus.zig");
pub const switch_workspace = focus_mod.switch_workspace;
pub const cycle_workspace = focus_mod.cycle_workspace;
pub const workspace_visible = focus_mod.workspace_visible;
pub const focused_index = focus_mod.focused_index;
pub const focused_window_id = focus_mod.focused_window_id;
pub const terminal_focused = focus_mod.terminal_focused;
pub const focused_window = focus_mod.focused_window;
pub const focused_owner = focus_mod.focused_owner;
pub const focus = focus_mod.focus;
pub const raise = focus_mod.raise;
pub const to_geom = focus_mod.to_geom;
pub const hit_test = focus_mod.hit_test;
pub const focus_at = focus_mod.focus_at;
pub const cycle_focus = focus_mod.cycle_focus;
pub const alt_tab_is_active = focus_mod.alt_tab_is_active;
pub const alt_tab_selected_id = focus_mod.alt_tab_selected_id;
pub const alt_tab_count = focus_mod.alt_tab_count;
pub const alt_tab_activate = focus_mod.alt_tab_activate;
pub const alt_tab_cycle = focus_mod.alt_tab_cycle;
pub const alt_tab_commit = focus_mod.alt_tab_commit;
pub const alt_tab_dismiss = focus_mod.alt_tab_dismiss;
pub const alt_tab_wm_commit = focus_mod.alt_tab_wm_commit;
const drag_mod = @import("driving_award/drag.zig");
pub const drag_start = drag_mod.drag_start;
pub const drag_is_active = drag_mod.drag_is_active;
pub const drag_get_payload = drag_mod.drag_get_payload;
pub const drag_cancel = drag_mod.drag_cancel;
pub const drag_armed_record = drag_mod.drag_armed_record;
pub const drag_armed_cancel = drag_mod.drag_armed_cancel;
pub const drag_enter = drag_mod.drag_enter;
pub const drag_leave = drag_mod.drag_leave;
pub const drag_drop = drag_mod.drag_drop;
const dialogs_mod = @import("driving_award/dialogs.zig");
pub const unsaved_dialog_show = dialogs_mod.unsaved_dialog_show;
pub const unsaved_dialog_is_open = dialogs_mod.unsaved_dialog_is_open;
pub const unsaved_dialog_save = dialogs_mod.unsaved_dialog_save;
pub const unsaved_dialog_dont_save = dialogs_mod.unsaved_dialog_dont_save;
pub const unsaved_dialog_cancel = dialogs_mod.unsaved_dialog_cancel;
pub const unsaved_dialog_click = dialogs_mod.unsaved_dialog_click;
pub const user_set_unsaved = dialogs_mod.user_set_unsaved;
pub const tooltip_clear = dialogs_mod.tooltip_clear;
pub const tooltip_show = dialogs_mod.tooltip_show;
pub const about_dialog_open_dialog = dialogs_mod.about_dialog_open_dialog;
pub const about_dialog_close = dialogs_mod.about_dialog_close;
pub const about_dialog_toggle = dialogs_mod.about_dialog_toggle;
const notify_mod = @import("driving_award/notify.zig");
pub const notify_push = notify_mod.notify_push;
pub const notify_dismiss = notify_mod.notify_dismiss;
pub const notify_advance_ticks = notify_mod.notify_advance_ticks;
pub const notify_count_visible = notify_mod.notify_count_visible;
pub const notify_entry = notify_mod.notify_entry;
pub const notif_center_toggle = notify_mod.notif_center_toggle;
pub const notif_center_set_open = notify_mod.notif_center_set_open;
pub const notif_center_dismiss = notify_mod.notif_center_dismiss;
pub const notif_center_clear_all = notify_mod.notif_center_clear_all;
const tray_mod = @import("driving_award/tray.zig");
pub const tray_rect = tray_mod.tray_rect;
pub const format_hhmm = tray_mod.format_hhmm;
pub const theme_letter = tray_mod.theme_letter;
pub const tray_theme_accent = tray_mod.tray_theme_accent;
pub const tray_clipboard_filled = tray_mod.tray_clipboard_filled;
pub const tray_set = tray_mod.tray_set;
pub const tray_current_tick = tray_mod.tray_current_tick;
pub const tray_has_clock = tray_mod.tray_has_clock;
const damage_mod = @import("driving_award/damage.zig");
pub const mark_dirty = damage_mod.mark_dirty;
pub const mark_damage = damage_mod.mark_damage;
pub const DamageRect = damage_mod.DamageRect;
pub const user_damage = damage_mod.user_damage;
pub const user_damage_mask = damage_mod.user_damage_mask;
pub const mark_terminal_dirty = damage_mod.mark_terminal_dirty;
pub const mark_palette_dirty = damage_mod.mark_palette_dirty;
pub const apply_grid_font_size = damage_mod.apply_grid_font_size;
const user_mod = @import("driving_award/user.zig");
pub const UserOpenResult = user_mod.UserOpenResult;
pub const find_user_window = user_mod.find_user_window;
pub const find_user_window_index = user_mod.find_user_window_index;
pub const user_open = user_mod.user_open;
pub const reflow = user_mod.reflow;
pub const user_fill = user_mod.user_fill;
pub const user_present = user_mod.user_present;
pub const SurfaceInfo = user_mod.SurfaceInfo;
pub const user_bind_surface = user_mod.user_bind_surface;
pub const user_surface = user_mod.user_surface;
pub const user_is_surface_backed = user_mod.user_is_surface_backed;
pub const PaintDest = user_mod.PaintDest;
pub const user_paint_dest = user_mod.user_paint_dest;
pub const user_move = user_mod.user_move;
pub const user_raise = user_mod.user_raise;
pub const user_raise_front = user_mod.user_raise_front;
pub const user_lower_back = user_mod.user_lower_back;
pub const user_move_to_workspace = user_mod.user_move_to_workspace;
pub const user_close = user_mod.user_close;
pub const user_owner = user_mod.user_owner;
pub const WinRect = user_mod.WinRect;
pub const user_rect = user_mod.user_rect;
pub const user_display_name = user_mod.user_display_name;
pub const wm_mirror = user_mod.wm_mirror;
pub const WinQuery = user_mod.WinQuery;
pub const user_query = user_mod.user_query;
pub const user_set_visible = user_mod.user_set_visible;
pub const is_resize_hit = user_mod.is_resize_hit;
pub const clamp_resize_w = user_mod.clamp_resize_w;
pub const clamp_resize_h = user_mod.clamp_resize_h;
pub const user_resize = user_mod.user_resize;
pub const wm_apply_rect = user_mod.wm_apply_rect;
pub const resize_active = user_mod.resize_active;
pub const resize_current_id = user_mod.resize_current_id;
pub const close_owner = user_mod.close_owner;
pub const surface_revoked = user_mod.surface_revoked;
pub const remove_user_at = user_mod.remove_user_at;
pub const user_window_slot = user_mod.user_window_slot;
const overview_mod = @import("driving_award/overview.zig");
pub const overview_enter = overview_mod.overview_enter;
pub const overview_focus = overview_mod.overview_focus;
pub const overview_move = overview_mod.overview_move;
pub const overview_move_refusal = overview_mod.overview_move_refusal;
pub const render_preview = overview_mod.render_preview;
const layout_mod = @import("driving_award/layout.zig");
pub const tile_x_start = layout_mod.tile_x_start;
pub const tile_y_start = layout_mod.tile_y_start;
pub const toggle_tiling = layout_mod.toggle_tiling;
pub const toggle_tiling_for = layout_mod.toggle_tiling_for;
pub const swap_master = layout_mod.swap_master;
pub const apply_tile_layout = layout_mod.apply_tile_layout;
pub const minimize_window = layout_mod.minimize_window;
pub const restore_from_dock = layout_mod.restore_from_dock;
pub const dock_icon_click = layout_mod.dock_icon_click;
pub const TaskbarEntry = layout_mod.TaskbarEntry;
pub const taskbar_entries = layout_mod.taskbar_entries;
pub const taskbar_click = layout_mod.taskbar_click;
pub const toggle_maximize = layout_mod.toggle_maximize;
pub const toggle_fullscreen = layout_mod.toggle_fullscreen;
pub const toggle_always_on_top = layout_mod.toggle_always_on_top;
pub const set_window_title = layout_mod.set_window_title;
pub const move_window_keyboard = layout_mod.move_window_keyboard;
pub const resize_window_keyboard = layout_mod.resize_window_keyboard;
pub const set_modal = layout_mod.set_modal;
pub const modal_active = layout_mod.modal_active;
pub const topmost_modal_id = layout_mod.topmost_modal_id;
pub const set_transient = layout_mod.set_transient;
pub const transient_advance_tick = layout_mod.transient_advance_tick;
pub const dismiss_transients = layout_mod.dismiss_transients;
pub const release_seat_layout = layout_mod.release_seat_layout;
const persist_mod = @import("driving_award/persist.zig");
pub const persist_record_bytes = persist_mod.persist_record_bytes;
pub const persist_max_records = persist_mod.persist_max_records;
pub const persist_max_bytes = persist_mod.persist_max_bytes;
pub const serialize_state = persist_mod.serialize_state;
pub const restore_state = persist_mod.restore_state;
pub const persist_save = persist_mod.save_now;
pub const persist_restore = persist_mod.restore_now;
pub const read_u32_le = persist_mod.read_u32_le;
const render_mod = @import("driving_award/render.zig");
pub const put_px = render_mod.put_px;
pub const fill_rect = render_mod.fill_rect;
pub const draw_glyph = render_mod.draw_glyph;
pub const draw_string = render_mod.draw_string;
pub const rune_cell_rows = render_mod.rune_cell_rows;
pub const draw_cell_glyph = render_mod.draw_cell_glyph;
pub const draw_glyph_16 = render_mod.draw_glyph_16;
pub const draw_string_16 = render_mod.draw_string_16;
pub const fmt_decimal = render_mod.fmt_decimal;
pub const blit_rect = render_mod.blit_rect;
pub const blit_rect_alpha = render_mod.blit_rect_alpha;
pub const fb_canvas = render_mod.fb_canvas;
pub const render_clock_content = render_mod.render_clock_content;
pub const terminalThemeDefaults = render_mod.terminalThemeDefaults;
pub const Rendition = render_mod.Rendition;
pub const terminalRendition = render_mod.terminalRendition;
pub const render_terminal_screen = render_mod.render_terminal_screen;
pub const paint = render_mod.paint;
pub const repaint_start = render_mod.repaint_start;
const compositor_mod = @import("driving_award/compositor.zig");
pub const paint_scene = compositor_mod.paint_scene;
pub const composite_pending = compositor_mod.composite_pending;
pub const presentation_trace_arm = compositor_mod.presentation_trace_arm;
pub const presentation_begin = compositor_mod.presentation_begin;
pub const presentation_end = compositor_mod.presentation_end;
pub const present_completed = compositor_mod.present_completed;
pub const composite = compositor_mod.composite;
pub const splash_bound_ticks = compositor_mod.splash_bound_ticks;
pub const render_splash = compositor_mod.render_splash;
pub const drain = compositor_mod.drain;
const chrome_mod = @import("driving_award/chrome.zig");
pub const clock_title_bg = chrome_mod.clock_title_bg;
pub const clock_bg = chrome_mod.clock_bg;
pub const chrome_border_w = chrome_mod.chrome_border_w;
pub const user_border = chrome_mod.user_border;
pub const user_border_unfocused = chrome_mod.user_border_unfocused;
pub const chrome_shadow_off = chrome_mod.chrome_shadow_off;
pub const shadow_color = chrome_mod.shadow_color;
pub const chrome_title_layout = chrome_mod.chrome_title_layout;
pub const user_title_bg = chrome_mod.user_title_bg;
pub const focus_ring = chrome_mod.focus_ring;
pub const taskbar_bg = chrome_mod.taskbar_bg;
pub const taskbar_entry_active = chrome_mod.taskbar_entry_active;
pub const taskbar_entry_dimmed = chrome_mod.taskbar_entry_dimmed;
pub const taskbar_entry_minimized = chrome_mod.taskbar_entry_minimized;
pub const taskbar_indicator_rgb = chrome_mod.taskbar_indicator_rgb;
pub const taskbar_min_dot_rgb = chrome_mod.taskbar_min_dot_rgb;
pub const taskbar_min_title_rgb = chrome_mod.taskbar_min_title_rgb;
pub const wallpaper_top = chrome_mod.wallpaper_top;
pub const wallpaper_bot = chrome_mod.wallpaper_bot;
pub const clear_wm_chrome = chrome_mod.clear_wm_chrome;
pub const set_window_chrome = chrome_mod.set_window_chrome;
pub const wm_chrome_policy_kind = chrome_mod.wm_chrome_policy_kind;
pub const wm_chrome_kind = chrome_mod.wm_chrome_kind;
pub const ChromeRow = chrome_mod.ChromeRow;
pub const note_tab_attach = chrome_mod.note_tab_attach;
pub const note_tab_detach = chrome_mod.note_tab_detach;
pub const tab_parent_of = chrome_mod.tab_parent_of;
pub const tab_group_count = chrome_mod.tab_group_count;
pub const tab_group_members = chrome_mod.tab_group_members;
pub const wm_chrome_rows = chrome_mod.wm_chrome_rows;
pub const wm_rest_alpha = chrome_mod.wm_rest_alpha;
pub const strip_fill = chrome_mod.strip_fill;
pub const strip_text = chrome_mod.strip_text;
pub const strip_glyph = chrome_mod.strip_glyph;
pub const paint_tab_strip = chrome_mod.paint_tab_strip;
pub const effective_chrome = chrome_mod.effective_chrome;
pub const draw_chrome = chrome_mod.draw_chrome;
const pointer_mod = @import("driving_award/pointer.zig");
pub const wm_fan_pointer = pointer_mod.wm_fan_pointer;
pub const TermHit = pointer_mod.TermHit;
pub const terminalHitAt = pointer_mod.terminalHitAt;
pub const mouse_buttons_to_flags = pointer_mod.mouse_buttons_to_flags;
pub const wm_content_pointer = pointer_mod.wm_content_pointer;
pub const pointer_tick = pointer_mod.pointer_tick;
pub const map_pointer_axis = pointer_mod.map_pointer_axis;
pub const cursor_pos = pointer_mod.cursor_pos;
pub const cursor_rect = pointer_mod.cursor_rect;
pub const cursor_pixel = pointer_mod.cursor_pixel;
pub const cursor_draw = pointer_mod.cursor_draw;
pub const cursor_restore = pointer_mod.cursor_restore;
pub const cursor_damage_clear = pointer_mod.cursor_damage_clear;
pub const cursor_shape_apply = pointer_mod.cursor_shape_apply;
test {
    // #3754: the seam files' own test blocks are analyzed only when the file
    // is referenced from the test root — one named edge per seam.
    _ = focus_mod;
    _ = drag_mod;
    _ = dialogs_mod;
    _ = notify_mod;
    _ = tray_mod;
    _ = damage_mod;
    _ = user_mod;
    _ = overview_mod;
    _ = layout_mod;
    _ = persist_mod;
    _ = render_mod;
    _ = compositor_mod;
    _ = chrome_mod;
    _ = pointer_mod;
}
