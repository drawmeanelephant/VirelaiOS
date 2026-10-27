//! Monitor commands: window management (`dui`, `wm`, `wnd`, `tabwm`).
//!
//! Split out of monitor.zig (#3755): declarations only — the
//! shared types, registry, dispatch, completion, and print/parse
//! helpers stay in the root and are aliased below so the moved
//! code is verbatim.

const std = @import("std");
const mon = @import("../monitor.zig");

const Monitor = mon.Monitor;
const ExecError = mon.ExecError;
const err_prefix = mon.err_prefix;
const print_usage = mon.print_usage;
const parseInt = mon.parseInt;
const parseSignedInt = mon.parseSignedInt;
const lookup = mon.lookup;
const exec = mon.exec;
const esp_exec = mon.esp_exec;
const process = mon.process;
const scheduler = mon.scheduler;
const driving_award = mon.driving_award;
const wm_server = mon.wm_server;
/// `dui` — report the Driving Award window manager (claim 1543, milestone
/// six G5): the registry (id/title/kind/rect/z-order/dirty/visible/owner),
/// the focused window, and the composite presents. `dui focus <n>` focuses
/// a window, `dui raise <n>` raises it to the top, `dui move <n> <x> <y>`
/// moves a user window (clamped on-scanout), `dui close <n>` releases a
/// user window, `dui list <pid>` filters the registry to one process's
/// windows, and `dui hit <x> <y>` hit-tests a point (focusing the topmost
/// window there).
pub fn cmd_dui(m: *Monitor, args: []const []const u8) ExecError {
    if (args.len > 0) {
        if (std.mem.eql(u8, args[0], "focus")) {
            if (args.len != 2) {
                print_usage(m, lookup("dui").?);
                return .usage;
            }
            const id = parseInt(args[1]) catch {
                err_prefix(m);
                m.console.puts("invalid id: ");
                m.console.puts(args[1]);
                m.console.puts("\n");
                return .invalid_argument;
            };
            if (id > 255) {
                err_prefix(m);
                m.console.print_line("id out of range");
                return .invalid_argument;
            }
            if (!driving_award.focus(@intCast(id))) {
                err_prefix(m);
                m.console.print_line("no such window");
                return .invalid_argument;
            }
            // The focus change may alter the clock's focus line — repaint.
            _ = driving_award.mark_dirty(0);
            _ = driving_award.mark_dirty(1);
            _ = driving_award.composite();
            m.console.puts("dui focus: focused=");
            m.console.print_u64(id);
            m.console.puts("\n");
            return .none;
        }
        if (std.mem.eql(u8, args[0], "raise")) {
            if (args.len != 2) {
                print_usage(m, lookup("dui").?);
                return .usage;
            }
            const id = parseInt(args[1]) catch {
                err_prefix(m);
                m.console.puts("invalid id: ");
                m.console.puts(args[1]);
                m.console.puts("\n");
                return .invalid_argument;
            };
            if (id > 255) {
                err_prefix(m);
                m.console.print_line("id out of range");
                return .invalid_argument;
            }
            if (!driving_award.raise(@intCast(id))) {
                err_prefix(m);
                m.console.print_line("no such window");
                return .invalid_argument;
            }
            _ = driving_award.composite();
            m.console.puts("dui raise: raised=");
            m.console.print_u64(id);
            m.console.puts("\n");
            return .none;
        }
        if (std.mem.eql(u8, args[0], "lower")) {
            if (args.len != 2) {
                print_usage(m, lookup("dui").?);
                return .usage;
            }
            const id2 = parseInt(args[1]) catch {
                err_prefix(m);
                m.console.puts("invalid id: ");
                m.console.puts(args[1]);
                m.console.puts("\n");
                return .invalid_argument;
            };
            if (id2 > 255) {
                err_prefix(m);
                m.console.print_line("id out of range");
                return .invalid_argument;
            }
            if (!driving_award.user_lower_back(@intCast(id2))) {
                err_prefix(m);
                m.console.print_line("no such user window");
                return .invalid_argument;
            }
            _ = driving_award.composite();
            m.console.puts("dui lower: lowered=");
            m.console.print_u64(id2);
            m.console.puts("\n");
            return .none;
        }
        if (std.mem.eql(u8, args[0], "move")) {
            if (args.len != 4) {
                print_usage(m, lookup("dui").?);
                return .usage;
            }
            const id = parseInt(args[1]) catch {
                err_prefix(m);
                m.console.puts("invalid id: ");
                m.console.puts(args[1]);
                m.console.puts("\n");
                return .invalid_argument;
            };
            const x = parseInt(args[2]) catch {
                err_prefix(m);
                m.console.print_line("invalid x");
                return .invalid_argument;
            };
            const y = parseInt(args[3]) catch {
                err_prefix(m);
                m.console.print_line("invalid y");
                return .invalid_argument;
            };
            if (id > 255 or x > std.math.maxInt(u32) or y > std.math.maxInt(u32)) {
                err_prefix(m);
                m.console.print_line("coordinate out of range");
                return .invalid_argument;
            }
            if (!driving_award.user_move(@intCast(id), @intCast(x), @intCast(y))) {
                err_prefix(m);
                m.console.print_line("no such user window (the terminal + clock are fixed)");
                return .invalid_argument;
            }
            _ = driving_award.composite();
            m.console.puts("dui move: moved=");
            m.console.print_u64(id);
            m.console.puts(" to ");
            m.console.print_u64(x);
            m.console.puts(",");
            m.console.print_u64(y);
            m.console.puts("\n");
            return .none;
        }
        if (std.mem.eql(u8, args[0], "close")) {
            if (args.len != 2) {
                print_usage(m, lookup("dui").?);
                return .usage;
            }
            const id = parseInt(args[1]) catch {
                err_prefix(m);
                m.console.puts("invalid id: ");
                m.console.puts(args[1]);
                m.console.puts("\n");
                return .invalid_argument;
            };
            if (id > 255) {
                err_prefix(m);
                m.console.print_line("id out of range");
                return .invalid_argument;
            }
            if (!driving_award.user_close(@intCast(id))) {
                err_prefix(m);
                m.console.print_line("no such user window (the terminal + clock are fixed)");
                return .invalid_argument;
            }
            // The close marked the fixed windows dirty — composite now to
            // reveal whatever sat under the released window.
            _ = driving_award.composite();
            m.console.puts("dui close: closed=");
            m.console.print_u64(id);
            m.console.puts("\n");
            return .none;
        }
        if (std.mem.eql(u8, args[0], "list")) {
            if (args.len != 2) {
                print_usage(m, lookup("dui").?);
                return .usage;
            }
            const pid = parseInt(args[1]) catch {
                err_prefix(m);
                m.console.puts("invalid pid: ");
                m.console.puts(args[1]);
                m.console.puts("\n");
                return .invalid_argument;
            };
            const want: usize = @intCast(pid); // aarch64: usize == u64, lossless
            var matches: usize = 0;
            var i: usize = 0;
            while (i < driving_award.count()) : (i += 1) {
                const w = driving_award.window_at(i).?;
                if (w.owner == want) matches += 1;
            }
            m.console.puts("dui list: pid=");
            m.console.print_u64(pid);
            m.console.puts(" matches=");
            m.console.print_u64(matches);
            m.console.puts("\n");
            i = 0;
            while (i < driving_award.count()) : (i += 1) {
                const w = driving_award.window_at(i).?;
                if (w.owner == want) print_win_row(m, i, w);
            }
            return .none;
        }
        if (std.mem.eql(u8, args[0], "cycle")) {
            if (args.len != 1) {
                print_usage(m, lookup("dui").?);
                return .usage;
            }
            if (driving_award.cycle_focus()) |id| {
                m.console.puts("dui: cycle focused=");
                m.console.print_u64(id);
                m.console.puts("\n");
                return .none;
            }
            err_prefix(m);
            m.console.print_line("no window to cycle");
            return .invalid_argument;
        }
        if (std.mem.eql(u8, args[0], "hit")) {
            if (args.len != 3) {
                print_usage(m, lookup("dui").?);
                return .usage;
            }
            const x = parseInt(args[1]) catch {
                err_prefix(m);
                m.console.print_line("invalid x");
                return .invalid_argument;
            };
            const y = parseInt(args[2]) catch {
                err_prefix(m);
                m.console.print_line("invalid y");
                return .invalid_argument;
            };
            if (x > std.math.maxInt(u32) or y > std.math.maxInt(u32)) {
                err_prefix(m);
                m.console.print_line("coordinate out of range");
                return .invalid_argument;
            }
            const hit = driving_award.hit_test(@intCast(x), @intCast(y));
            m.console.puts("dui hit: ");
            m.console.print_u64(x);
            m.console.puts(",");
            m.console.print_u64(y);
            m.console.puts(" -> ");
            if (hit) |hid| {
                m.console.print_u64(hid);
                _ = driving_award.focus(hid);
            } else {
                m.console.puts("none");
            }
            m.console.puts("\n");
            return .none;
        }
        if (std.mem.eql(u8, args[0], "tile")) {
            if (args.len != 2) {
                print_usage(m, lookup("dui").?);
                return .usage;
            }
            const id = parseInt(args[1]) catch {
                err_prefix(m);
                m.console.puts("invalid id: ");
                m.console.puts(args[1]);
                m.console.puts("\n");
                return .invalid_argument;
            };
            if (id > 255) {
                err_prefix(m);
                m.console.print_line("id out of range");
                return .invalid_argument;
            }
            if (!driving_award.focus(@intCast(id))) {
                err_prefix(m);
                m.console.print_line("no such window");
                return .invalid_argument;
            }
            // The EL1h half of M21 W1's Ctrl+T chord: focus the window,
            // then toggle it floating <-> tiled (driving_award owns the
            // max-2 constraint and the layout math).
            driving_award.toggle_tiling();
            _ = driving_award.composite();
            m.console.puts("dui tile: id=");
            m.console.print_u64(id);
            m.console.puts(" mode=");
            m.console.puts(if (driving_award.tile_mode) "on" else "off");
            if (driving_award.tile_master_id) |mid| {
                m.console.puts(" master=");
                m.console.print_u64(mid);
            }
            if (driving_award.tile_stack_id) |sid| {
                m.console.puts(" stack=");
                m.console.print_u64(sid);
            }
            m.console.puts("\n");
            return .none;
        }
        if (std.mem.eql(u8, args[0], "master")) {
            if (args.len != 1) {
                print_usage(m, lookup("dui").?);
                return .usage;
            }
            // The EL1h half of M21 W2's Ctrl+M chord: swap master/detail
            // (a no-op unless two windows are tiled).
            _ = driving_award.swap_master();
            _ = driving_award.composite();
            m.console.puts("dui master: side=");
            m.console.puts(if (driving_award.tile_master_side) "left" else "right");
            if (driving_award.tile_master_id) |mid| {
                m.console.puts(" master=");
                m.console.print_u64(mid);
            }
            if (driving_award.tile_stack_id) |sid| {
                m.console.puts(" stack=");
                m.console.print_u64(sid);
            }
            m.console.puts("\n");
            return .none;
        }
        if (std.mem.eql(u8, args[0], "minimize")) {
            if (args.len != 2) {
                print_usage(m, lookup("dui").?);
                return .usage;
            }
            const id = parseInt(args[1]) catch return .invalid_argument;
            if (id > 255) return .invalid_argument;
            if (!driving_award.minimize_window(@intCast(id))) {
                err_prefix(m);
                m.console.print_line("minimize failed: no such user window");
                return .invalid_argument;
            }
            _ = driving_award.composite();
            m.console.puts("dui minimize: minimized id=");
            m.console.print_u64(id);
            m.console.puts("\n");
            return .none;
        }
        if (std.mem.eql(u8, args[0], "restore")) {
            if (args.len != 2) {
                print_usage(m, lookup("dui").?);
                return .usage;
            }
            const id = parseInt(args[1]) catch return .invalid_argument;
            if (id > 255) return .invalid_argument;
            if (!driving_award.restore_from_dock(@intCast(id))) {
                err_prefix(m);
                m.console.print_line("restore failed: window not minimized or unknown");
                return .invalid_argument;
            }
            _ = driving_award.composite();
            m.console.puts("dui restore: restored id=");
            m.console.print_u64(id);
            m.console.puts("\n");
            return .none;
        }
        if (std.mem.eql(u8, args[0], "maximize")) {
            if (args.len != 2) {
                print_usage(m, lookup("dui").?);
                return .usage;
            }
            const id = parseInt(args[1]) catch return .invalid_argument;
            if (id > 255) return .invalid_argument;
            if (!driving_award.toggle_maximize(@intCast(id))) {
                err_prefix(m);
                m.console.print_line("maximize failed: no such user window");
                return .invalid_argument;
            }
            _ = driving_award.composite();
            const w = driving_award.find_user_window(@intCast(id));
            m.console.puts("dui maximize: id=");
            m.console.print_u64(id);
            m.console.puts(" max=");
            m.console.puts(if (w != null and w.?.maximized) "on" else "off");
            m.console.puts("\n");
            return .none;
        }
        if (std.mem.eql(u8, args[0], "fullscreen")) {
            if (args.len != 2) {
                print_usage(m, lookup("dui").?);
                return .usage;
            }
            const id = parseInt(args[1]) catch return .invalid_argument;
            if (id > 255) return .invalid_argument;
            if (!driving_award.toggle_fullscreen(@intCast(id))) {
                err_prefix(m);
                m.console.print_line("fullscreen failed: no such user window");
                return .invalid_argument;
            }
            _ = driving_award.composite();
            m.console.puts("dui fullscreen: id=");
            m.console.print_u64(id);
            m.console.puts(" on=");
            m.console.puts(if (driving_award.fullscreen_active) "yes" else "no");
            m.console.puts("\n");
            return .none;
        }
        if (std.mem.eql(u8, args[0], "aot")) {
            if (args.len != 2) {
                print_usage(m, lookup("dui").?);
                return .usage;
            }
            const id = parseInt(args[1]) catch return .invalid_argument;
            if (id > 255) return .invalid_argument;
            if (!driving_award.toggle_always_on_top(@intCast(id))) {
                err_prefix(m);
                m.console.print_line("always-on-top failed: no such user window");
                return .invalid_argument;
            }
            _ = driving_award.composite();
            const w = driving_award.find_user_window(@intCast(id));
            m.console.puts("dui always-on-top: id=");
            m.console.print_u64(id);
            m.console.puts(" flag=");
            m.console.puts(if (w != null and w.?.always_on_top) "on" else "off");
            m.console.puts("\n");
            return .none;
        }
        if (std.mem.eql(u8, args[0], "kmove")) {
            if (args.len != 4) {
                print_usage(m, lookup("dui").?);
                return .usage;
            }
            const id = parseInt(args[1]) catch return .invalid_argument;
            const dx = parseSignedInt(args[2]) catch return .invalid_argument;
            const dy = parseSignedInt(args[3]) catch return .invalid_argument;
            if (id > 255) return .invalid_argument;
            if (!driving_award.move_window_keyboard(@intCast(id), @intCast(dx), @intCast(dy))) {
                err_prefix(m);
                m.console.print_line("keyboard move failed: no such user window");
                return .invalid_argument;
            }
            _ = driving_award.composite();
            m.console.puts("dui kmove: id=");
            m.console.print_u64(id);
            m.console.puts("\n");
            return .none;
        }
        if (std.mem.eql(u8, args[0], "kresize")) {
            if (args.len != 4) {
                print_usage(m, lookup("dui").?);
                return .usage;
            }
            const id = parseInt(args[1]) catch return .invalid_argument;
            const dw = parseSignedInt(args[2]) catch return .invalid_argument;
            const dh = parseSignedInt(args[3]) catch return .invalid_argument;
            if (id > 255) return .invalid_argument;
            if (!driving_award.resize_window_keyboard(@intCast(id), @intCast(dw), @intCast(dh))) {
                err_prefix(m);
                m.console.print_line("keyboard resize failed: no such user window");
                return .invalid_argument;
            }
            _ = driving_award.composite();
            m.console.puts("dui kresize: id=");
            m.console.print_u64(id);
            m.console.puts("\n");
            return .none;
        }
        if (std.mem.eql(u8, args[0], "title")) {
            if (args.len != 3) {
                print_usage(m, lookup("dui").?);
                return .usage;
            }
            const id = parseInt(args[1]) catch return .invalid_argument;
            if (id > 255) return .invalid_argument;
            if (!driving_award.set_window_title(@intCast(id), args[2])) {
                err_prefix(m);
                m.console.print_line("title update failed: no such user window");
                return .invalid_argument;
            }
            _ = driving_award.composite();
            m.console.puts("dui title: id=");
            m.console.print_u64(id);
            m.console.puts(" title=");
            m.console.puts(args[2]);
            m.console.puts("\n");
            return .none;
        }
        if (std.mem.eql(u8, args[0], "modal")) {
            if (args.len != 3) {
                print_usage(m, lookup("dui").?);
                return .usage;
            }
            const id = parseInt(args[1]) catch return .invalid_argument;
            const flag = parseInt(args[2]) catch return .invalid_argument;
            if (id > 255) return .invalid_argument;
            if (!driving_award.set_modal(@intCast(id), flag != 0)) {
                err_prefix(m);
                m.console.print_line("modal failed: no such user window");
                return .invalid_argument;
            }
            _ = driving_award.composite();
            m.console.puts("dui modal: id=");
            m.console.print_u64(id);
            m.console.puts(" flag=");
            m.console.print_u64(flag);
            m.console.puts("\n");
            return .none;
        }
        if (std.mem.eql(u8, args[0], "transient")) {
            if (args.len != 3) {
                print_usage(m, lookup("dui").?);
                return .usage;
            }
            const id = parseInt(args[1]) catch return .invalid_argument;
            const timeout = parseInt(args[2]) catch return .invalid_argument;
            if (id > 255) return .invalid_argument;
            if (!driving_award.set_transient(@intCast(id), @intCast(timeout))) {
                err_prefix(m);
                m.console.print_line("transient failed: no such user window");
                return .invalid_argument;
            }
            _ = driving_award.composite();
            m.console.puts("dui transient: id=");
            m.console.print_u64(id);
            m.console.puts(" timeout=");
            m.console.print_u64(timeout);
            m.console.puts("\n");
            return .none;
        }
        if (std.mem.eql(u8, args[0], "notif")) {
            if (args.len != 2) {
                print_usage(m, lookup("dui").?);
                return .usage;
            }
            driving_award.notify_push(args[1], 0);
            _ = driving_award.composite();
            m.console.puts("dui notif: pushed=");
            m.console.puts(args[1]);
            m.console.puts("\n");
            return .none;
        }
        if (std.mem.eql(u8, args[0], "notif-center")) {
            if (args.len != 1) {
                print_usage(m, lookup("dui").?);
                return .usage;
            }
            driving_award.notif_center_toggle();
            _ = driving_award.composite();
            m.console.puts("dui notif-center: open=");
            m.console.puts(if (driving_award.notif_center_open) "yes" else "no");
            m.console.puts("\n");
            return .none;
        }
        if (std.mem.eql(u8, args[0], "notif-center-state")) {
            // WMS6 Gate B (issue #626): a PURE state report (does not toggle)
            // — the live gate reads the panel's open/closed state after an
            // injected tray click to prove the shim (boot A) or the WM
            // (boot B) opened it.
            if (args.len != 1) {
                print_usage(m, lookup("dui").?);
                return .usage;
            }
            m.console.puts("dui notif-center-state: open=");
            m.console.puts(if (driving_award.notif_center_open) "yes" else "no");
            m.console.puts("\n");
            return .none;
        }
        if (std.mem.eql(u8, args[0], "tooltip-state")) {
            // WMS6 Gate C (issue #626): a PURE state report (does not change
            // anything) — the live gate reads the tooltip's visibility + text
            // after an injected hover to prove the WM showed it.
            if (args.len != 1) {
                print_usage(m, lookup("dui").?);
                return .usage;
            }
            m.console.puts("dui tooltip-state: visible=");
            m.console.puts(if (driving_award.tooltip_visible) "yes" else "no");
            m.console.puts(" text=");
            m.console.puts(driving_award.tooltip_text[0..driving_award.tooltip_text_len]);
            m.console.puts("\n");
            return .none;
        }
        if (std.mem.eql(u8, args[0], "tray-state")) {
            // WMS6 Gate E (issue #626): a PURE state report (does not change
            // anything) — the live gate reads the effective tray widget
            // content after the WM's TRAY decision to prove the WM's values
            // (not the shim's) are what renders.
            if (args.len != 1) {
                print_usage(m, lookup("dui").?);
                return .usage;
            }
            m.console.puts("dui tray-state: clock=");
            m.console.puts(driving_award.wm_tray_clock_text[0..5]);
            m.console.puts(" clock_set=");
            m.console.puts(if (driving_award.wm_tray_clock_set) "yes" else "no");
            m.console.puts(" theme=");
            var tlet: [1]u8 = .{driving_award.wm_tray_theme};
            m.console.puts(tlet[0..1]);
            m.console.puts(" theme_set=");
            m.console.puts(if (driving_award.wm_tray_theme_set) "yes" else "no");
            m.console.puts(" clip=");
            m.console.puts(if (driving_award.wm_tray_clip) "yes" else "no");
            m.console.puts(" clip_set=");
            m.console.puts(if (driving_award.wm_tray_clip_set) "yes" else "no");
            m.console.puts("\n");
            return .none;
        }
        if (std.mem.eql(u8, args[0], "notif-dismiss")) {
            if (args.len != 2) {
                print_usage(m, lookup("dui").?);
                return .usage;
            }
            const idx = parseInt(args[1]) catch return .invalid_argument;
            const ok = driving_award.notif_center_dismiss(@intCast(idx));
            _ = driving_award.composite();
            m.console.puts("dui notif-dismiss: idx=");
            m.console.print_u64(idx);
            m.console.puts(" ok=");
            m.console.puts(if (ok) "1" else "0");
            m.console.puts("\n");
            return .none;
        }
        if (std.mem.eql(u8, args[0], "notif-clear")) {
            if (args.len != 1) {
                print_usage(m, lookup("dui").?);
                return .usage;
            }
            driving_award.notif_center_clear_all();
            _ = driving_award.composite();
            m.console.puts("dui notif-clear: cleared\n");
            return .none;
        }
        if (std.mem.eql(u8, args[0], "ws")) {
            if (args.len != 2) {
                print_usage(m, lookup("dui").?);
                return .usage;
            }
            const ws = parseInt(args[1]) catch return .invalid_argument;
            if (ws > 2) return .invalid_argument;
            driving_award.switch_workspace(@intCast(ws));
            _ = driving_award.composite();
            m.console.puts("dui ws: workspace=");
            m.console.print_u64(ws);
            m.console.puts("\n");
            return .none;
        }
        if (std.mem.eql(u8, args[0], "ws-cycle")) {
            if (args.len != 1) {
                print_usage(m, lookup("dui").?);
                return .usage;
            }
            driving_award.cycle_workspace();
            _ = driving_award.composite();
            m.console.puts("dui ws-cycle: workspace=");
            m.console.print_u64(driving_award.current_workspace);
            m.console.puts("\n");
            return .none;
        }
        if (std.mem.eql(u8, args[0], "taskbar")) {
            // WM3 (issue #707 card 3): report the taskbar ENTRY enumeration
            // — the exact id-ascending current-workspace list the `.taskbar`
            // render walks (the render iterates the SAME taskbar_entries()
            // snapshot), so a live gate can grep what the bar draws.
            if (args.len != 1) {
                print_usage(m, lookup("dui").?);
                return .usage;
            }
            _ = driving_award.composite();
            var tb: [driving_award.user_windows_max]driving_award.TaskbarEntry = undefined;
            const tn = driving_award.taskbar_entries(&tb);
            m.console.puts("dui taskbar: ws=");
            m.console.print_u64(driving_award.current_workspace);
            m.console.puts(" entries=");
            m.console.print_u64(tn);
            m.console.puts("\n");
            var ti: usize = 0;
            while (ti < tn) : (ti += 1) {
                m.console.puts("dui taskbar: entry=");
                m.console.print_u64(ti);
                m.console.puts(" id=");
                m.console.print_u64(tb[ti].id);
                m.console.puts(" focused=");
                m.console.print_u64(@intFromBool(tb[ti].focused));
                m.console.puts(" minimized=");
                m.console.print_u64(@intFromBool(tb[ti].minimized));
                m.console.puts("\n");
            }
            return .none;
        }
        if (std.mem.eql(u8, args[0], "unsaved")) {
            if (args.len != 3) {
                print_usage(m, lookup("dui").?);
                return .usage;
            }
            const id = parseInt(args[1]) catch return .invalid_argument;
            const flag = parseInt(args[2]) catch return .invalid_argument;
            if (id > 255) return .invalid_argument;
            _ = driving_award.user_set_unsaved(@intCast(id), flag != 0);
            _ = driving_award.composite();
            m.console.puts("dui unsaved: id=");
            m.console.print_u64(id);
            m.console.puts(" flag=");
            m.console.print_u64(flag);
            m.console.puts("\n");
            return .none;
        }
        if (std.mem.eql(u8, args[0], "dialog-show")) {
            if (args.len != 2) {
                print_usage(m, lookup("dui").?);
                return .usage;
            }
            const id = parseInt(args[1]) catch return .invalid_argument;
            if (id > 255) return .invalid_argument;
            driving_award.unsaved_dialog_show(@intCast(id));
            _ = driving_award.composite();
            m.console.puts("dui dialog-show: target=");
            m.console.print_u64(id);
            m.console.puts(" open=");
            m.console.puts(if (driving_award.unsaved_dialog_is_open()) "yes" else "no");
            m.console.puts("\n");
            return .none;
        }
        if (std.mem.eql(u8, args[0], "dialog-click")) {
            if (args.len != 2) {
                print_usage(m, lookup("dui").?);
                return .usage;
            }
            const choice = if (std.mem.eql(u8, args[1], "save"))
                driving_award.unsaved_dialog_click(640 - 100 + 40, 360 + 20)
            else if (std.mem.eql(u8, args[1], "dont_save") or std.mem.eql(u8, args[1], "discard"))
                driving_award.unsaved_dialog_click(640 - 100 + 110, 360 + 20)
            else
                driving_award.unsaved_dialog_click(640 - 100 + 175, 360 + 20);
            _ = driving_award.composite();
            m.console.puts("dui dialog-click: result=");
            m.console.puts(@tagName(choice));
            m.console.puts(" open=");
            m.console.puts(if (driving_award.unsaved_dialog_is_open()) "yes" else "no");
            m.console.puts("\n");
            return .none;
        }
        print_usage(m, lookup("dui").?);
        return .usage;
    }
    if (!driving_award.armed()) {
        m.console.print_line("dui: window manager not armed (no gpu device — the default VM)");
        return .none;
    }
    m.console.puts("dui: windows=");
    m.console.print_u64(driving_award.count());
    m.console.puts(" focused=");
    m.console.print_u64(driving_award.focused_window_id());
    m.console.puts(" presents=");
    m.console.print_u64(driving_award.presents_pushed());
    // M33 SB6 (claim 6864): the composite-cost observables — user windows
    // the kernel blitted vs surface-backed windows skipped (WM-owned).
    m.console.puts(" blits=");
    m.console.print_u64(driving_award.user_blits);
    m.console.puts(" skips=");
    m.console.print_u64(driving_award.migrated_skips);
    m.console.puts("\n");
    // M21 W1/W2 tiling state (the layout the tiled rects obey).
    m.console.puts("dui: tiling=");
    m.console.puts(if (driving_award.tile_mode) "on" else "off");
    if (driving_award.tile_master_id) |mid| {
        m.console.puts(" master=");
        m.console.print_u64(mid);
    }
    if (driving_award.tile_stack_id) |sid| {
        m.console.puts(" stack=");
        m.console.print_u64(sid);
    }
    m.console.puts(" side=");
    m.console.puts(if (driving_award.tile_master_side) "left" else "right");
    m.console.puts("\n");
    var i: usize = 0;
    while (i < driving_award.count()) : (i += 1) {
        print_win_row(m, i, driving_award.window_at(i).?);
    }
    return .none;
}

/// Print one registry row for the window at index `i` — the shared formatter
/// behind the `dui` report and `dui list <pid>`. The trailing `owner=` column
/// is the per-process-ownership visibility (claim 0487 follow-on): a pid for a
/// `.user` window, `-` for the fixed kernel-owned terminal + clock.
fn print_win_row(m: *Monitor, i: usize, w: *const driving_award.Window) void {
    m.console.puts("dui[");
    m.console.print_u64(i);
    m.console.puts("]: ");
    m.console.puts(w.title);
    m.console.puts(" ");
    m.console.puts(driving_award.kind_name(w.kind));
    m.console.puts(" rect=");
    m.console.print_u64(w.x);
    m.console.puts(",");
    m.console.print_u64(w.y);
    m.console.puts(",");
    m.console.print_u64(w.w);
    m.console.puts(",");
    m.console.print_u64(w.h);
    m.console.puts(" dirty=");
    m.console.puts(if (w.dirty) "1" else "0");
    m.console.puts(" visible=");
    m.console.puts(if (w.visible) "1" else "0");
    m.console.puts(" z=");
    m.console.print_u64(i);
    m.console.puts(" owner=");
    if (w.owner) |pid| {
        m.console.print_u64(@intCast(pid));
    } else {
        m.console.puts("-");
    }
    if (w.minimized) m.console.puts(" minimized=1");
    if (w.maximized) m.console.puts(" maximized=1");
    if (w.always_on_top) m.console.puts(" aot=1");
    if (w.modal) m.console.puts(" modal=1");
    if (w.transient) m.console.puts(" transient=1");
    if (w.kind == .user) {
        m.console.puts(" ws=");
        m.console.print_u64(w.workspace);
        // M33 SB4 (claim 2382): the rect-granular damage observable for the
        // damage-tracking gate. `full` when no partial damage is pending
        // (whole-window), else the EXACT pending rect.
        m.console.puts(" damage=");
        if (w.damaged) {
            m.console.print_u64(w.dx);
            m.console.puts(",");
            m.console.print_u64(w.dy);
            m.console.puts(",");
            m.console.print_u64(w.dw);
            m.console.puts(",");
            m.console.print_u64(w.dh);
        } else {
            m.console.puts("full");
        }
        // M33 SB4: the LAST rect composite repainted (consumed damage) —
        // observable even after the drain. 0,0,0,0 = no partial repaint yet.
        m.console.puts(" last=");
        m.console.print_u64(w.last_dx);
        m.console.puts(",");
        m.console.print_u64(w.last_dy);
        m.console.puts(",");
        m.console.print_u64(w.last_dw);
        m.console.puts(",");
        m.console.print_u64(w.last_dh);
    }
    m.console.puts("\n");
}

/// M32 WMS2 (issue #622): the render-server register report row. The
/// present-sequence counter is the parity-cards' observability primitive
/// (WMS4–WMS6 assert against it), so `wm` reports it from day one. When no
/// WM is registered it reports shim mode ("/ none").
pub fn cmd_wm(m: *Monitor, args: []const []const u8) ExecError {
    _ = args;
    const info = wm_server.info();
    if (info.pid) |pid| {
        m.console.puts("wm: registered pid=");
        m.console.print_u64(pid);
        m.console.puts(" present=");
        m.console.puts("\n");
        // present_seq is u32; report it as a fixed decimal for grep-ability.
        m.console.puts("wm: present_seq=");
        m.console.print_u64(info.present_seq);
        m.console.puts(" presents=");
        m.console.print_u64(info.present_count);
        m.console.puts(" ticks=");
        m.console.print_u64(info.tick_count);
        m.console.puts("\n");
        // M53 card 1 (#1247): the MEASURED cadence, and the two halves of the
        // input->scanout latency (WM loop turn | kernel+GPU present cost).
        // Integer pps is an average over the register's life; tick_pps is
        // printed beside it so the comparison is stated, not assumed.
        m.console.puts("wm: rate window_ms=");
        m.console.print_u64(info.present_window_ms);
        m.console.puts(" present_avg_ms=");
        m.console.print_u64(info.present_avg_ms);
        m.console.puts(" tick_avg_ms=");
        m.console.print_u64(info.tick_avg_ms);
        m.console.puts(" lat_n=");
        m.console.print_u64(info.lat_count);
        m.console.puts(" lat_avg_us=");
        m.console.print_u64(info.lat_avg_ns / 1000);
        m.console.puts(" lat_max_us=");
        m.console.print_u64(info.lat_max_ns / 1000);
        m.console.puts(" flush_n=");
        m.console.print_u64(info.flush_count);
        m.console.puts(" flush_avg_us=");
        m.console.print_u64(info.flush_avg_ns / 1000);
        m.console.puts(" flush_max_us=");
        m.console.print_u64(info.flush_max_ns / 1000);
        // WMP card 3 (#1274): the scheduling half of this same latency, on the
        // row that reports the latency. The pacing gate reads THIS row, so a
        // latency figure can now be attributed to the wake demand behind it
        // instead of standing on its own. `requests > 0` is the non-vacuity
        // proof: the demand is real and it is not being served by the tick.
        m.console.puts(" resched_requests=");
        m.console.print_u64(scheduler.resched_requests);
        m.console.puts(" resched_coalesced=");
        m.console.print_u64(scheduler.resched_coalesced);
        m.console.puts(" resched_discharged=");
        m.console.print_u64(scheduler.resched_discharged);
        m.console.puts("\n");
        // M32 WMS4 (issue #624): chrome observability — SET_WINDOW
        // submissions counted, the broadcast policy's chrome kind, and the
        // effective last chrome kind of every user window (0 = shim rules).
        m.console.puts("wm: chrome submissions=");
        m.console.print_u64(info.set_window_count);
        m.console.puts(" policy_kind=");
        m.console.print_hex_min(driving_award.wm_chrome_policy_kind());
        m.console.puts("\n");
        // M32 WMS5 (issue #625): the input-seam observability — how many
        // raw pointer samples (kind 19) and registry mirrors (kind 20) the
        // kernel has fanned out to the registered WM. The gate greps these
        // to prove the WM — not the kernel — received the stream.
        m.console.puts("wm: ptr_fan=");
        m.console.print_u64(info.pointer_fan_count);
        m.console.puts(" win_mirror=");
        m.console.print_u64(info.window_mirror_count);
        // M32 WMS5 Gate 2 (issue #625, claim 4278): the keyboard half of
        // the input seam — raw key fan-out (kind 21) and SET_STATE (cmd 4)
        // visibility/workspace changes applied by the registered WM.
        m.console.puts(" key_fan=");
        m.console.print_u64(info.key_fan_count);
        m.console.puts(" set_state=");
        m.console.print_u64(info.set_state_count);
        // M32 WMS6 Gate A (issue #626): the desktop-chrome observability —
        // ALT_TAB (cmd 5) decisions the WM made (activate/cycle/commit/
        // dismiss), applied by the kernel (the gate greps `alt_tab=`).
        m.console.puts(" alt_tab=");
        m.console.print_u64(info.alt_tab_apply_count);
        // M32 WMS6 Gate B (issue #626): the notification-center observability
        // — NOTIF_CENTER (cmd 6) and NOTIF_DISMISS (cmd 7) decisions applied.
        m.console.puts(" notif=");
        m.console.print_u64(info.notif_center_count);
        m.console.puts(" notif_dismiss=");
        m.console.print_u64(info.notif_dismiss_count);
        // #3759 (fourth item): NOTIF_GET (cmd 25) queue reads served to the
        // seat — the center/toasts' contents access, not a decision.
        m.console.puts(" notif_get=");
        m.console.print_u64(info.notif_get_count);
        // M32 WMS6 Gate C (issue #626): the tooltip observability — TOOLTIP
        // (cmd 8) show/hide decisions applied.
        m.console.puts(" tooltip=");
        m.console.print_u64(info.tooltip_count);
        // M32 WMS6 Gate D (issue #626): the dock observability — DOCK (cmd 9)
        // icon-click decisions applied.
        m.console.puts(" dock=");
        m.console.print_u64(info.dock_count);
        // M32 WMS6 Gate E (issue #626): the tray observability — TRAY (cmd 10)
        // widget-content decisions applied (the gate greps `tray=`).
        m.console.puts(" tray=");
        m.console.print_u64(info.tray_count);
        // M32 WMS8 Gate 2 (issue #628): the dialog observability — DIALOG
        // (cmd 11) modal-dialog decisions (about open/close/toggle) applied.
        m.console.puts(" dialog=");
        m.console.print_u64(info.dialog_count);
        // WM3 (issue #707 card 3): the taskbar observability — TASKBAR
        // (cmd 12) entry-click decisions applied (the gate greps
        // `taskbar=`).
        m.console.puts(" taskbar=");
        m.console.print_u64(info.taskbar_count);
        // WM2 mission-control overview (Self-hosting Lane 1, issue #707
        // card 2): the overview observability — OVERVIEW (cmd 21) grid
        // decisions (enter/exit/focus/move) applied (the gate greps
        // `overview=`).
        m.console.puts(" overview=");
        m.console.print_u64(info.overview_count);
        m.console.puts("\n");
        var rows: [driving_award.max_windows]driving_award.ChromeRow = undefined;
        const n = driving_award.wm_chrome_rows(&rows);
        var i: usize = 0;
        while (i < n) : (i += 1) {
            m.console.puts("wm: chrome window id=");
            m.console.print_u64(rows[i].id);
            m.console.puts(" kind=");
            m.console.print_hex_min(rows[i].kind);
            // WM4 (issue #707 card 4): the effective rest alpha (256 =
            // opaque; below = the at-rest unfocused client blend) — the
            // gate greps `rest=` for the policy proof.
            m.console.puts(" rest=");
            m.console.print_u64(rows[i].rest_alpha);
            m.console.puts("\n");
        }
    } else {
        m.console.print_line("wm: none (shim compositing)");
    }
    return .none;
}

/// M32 WMS3 (issue #623): the WM-server command. `wnd start` execs the
/// long-lived EL0 WND.BIN server (the DEFINED bootstrap — infrastructure,
/// not in APPS.TXT; the default VM stays shim-only because nothing
/// auto-starts it). Bare `wnd` reports the same seated-server state as
/// `wm` (registered pid + present/tick counters; `wnd: none` when the
/// shell idle shim is still compositing). Because WND.BIN never exits, the
/// crash story uses `kill <pid>` (claim 7786) + WMS2 exit-path teardown
/// (claim 0622) + a fresh `wnd start` — re-registering into the freed seat.
pub fn cmd_wnd(m: *Monitor, args: []const []const u8) ExecError {
    if (args.len >= 1 and std.mem.eql(u8, args[0], "start")) {
        // The bootstrap: launch the long-lived WM server. Go through the
        // exec seam DIRECTLY with a fixed file name — the same pattern as
        // `calc`'s old exec_file (a direct string literal,
        // no argv indirection): an argv handoff to cmd_exec showed garbage
        // in the exec error path under ReleaseSmall, so this is the honest,
        // proven shape. WND.BIN becomes process WND.BIN on the ESP.
        switch (esp_exec.exec_file("WND.BIN", &.{})) {
            .ok => {
                m.console.puts("wnd: starting ");
                const info = esp_exec.loaded().?;
                m.console.puts(info.name);
                m.console.puts("\n");
                return .none;
            },
            .no_disk => {
                err_prefix(m);
                m.console.print_line("no disk (ESP FAT volume unavailable)");
                return .not_implemented;
            },
            .not_found => {
                err_prefix(m);
                m.console.print_line("WND.BIN not found on the ESP (must be a DSK1 flat image)");
                return .invalid_argument;
            },
            else => {
                err_prefix(m);
                m.console.print_line("WND.BIN failed to load (see `exec WND.BIN` for the full diagnosis)");
                return .invalid_argument;
            },
        }
    }
    const info = wm_server.info();
    if (info.pid) |pid| {
        m.console.puts("wnd: registered pid=");
        m.console.print_u64(pid);
        m.console.puts(" present_seq=");
        m.console.print_u64(info.present_seq);
        m.console.puts(" presents=");
        m.console.print_u64(info.present_count);
        m.console.puts(" ticks=");
        m.console.print_u64(info.tick_count);
        m.console.puts("\n");
    } else {
        m.console.print_line("wnd: none (shim compositing)");
    }
    return .none;
}

/// M39 TWM1 tabbed window manager server command (issue #928).
/// 'tabwm' reports the current WM server; 'tabwm start' launches TABWM.BIN.
pub fn cmd_tabwm(m: *Monitor, args: []const []const u8) ExecError {
    if (args.len >= 1 and std.mem.eql(u8, args[0], "start")) {
        switch (esp_exec.exec_file("TABWM.BIN", &.{})) {
            .ok => {
                const info = esp_exec.loaded().?;
                var line: ["tabwm: starting ".len + process.name_max + 1]u8 = undefined;
                m.console.puts(std.fmt.bufPrint(&line, "tabwm: starting {s}\n", .{info.name}) catch return .not_implemented);
                return .none;
            },
            .no_disk => {
                err_prefix(m);
                m.console.print_line("no disk (ESP FAT volume unavailable)");
                return .not_implemented;
            },
            .not_found => {
                err_prefix(m);
                m.console.print_line("TABWM.BIN not found on the ESP (must be a DSK1 flat image)");
                return .invalid_argument;
            },
            else => {
                err_prefix(m);
                m.console.print_line("TABWM.BIN failed to load (see `exec TABWM.BIN` for the full diagnosis)");
                return .invalid_argument;
            },
        }
    }
    const info = wm_server.info();
    if (info.pid) |pid| {
        m.console.puts("tabwm: registered pid=");
        m.console.print_u64(pid);
        m.console.puts(" present_seq=");
        m.console.print_u64(info.present_seq);
        m.console.puts(" presents=");
        m.console.print_u64(info.present_count);
        m.console.puts(" ticks=");
        m.console.print_u64(info.tick_count);
        m.console.puts("\n");
    } else {
        m.console.print_line("tabwm: none (shim compositing)");
    }
    return .none;
}
