//! Milestone seven, card I3 (claim 6050): keyboard/pointer event FIFO +
//! keycode decode → Road Pops.
//!
//! The XHCI interrupt-IN reports (I1/I2) land here as keyboard/pointer
//! events. The shell idle loop is the drain site (the card-3d shell-idle-
//! drain pattern, next to the net RX drain): `drain()` polls the armed
//! interrupt-IN endpoints, decodes keyboard HID boot reports (modifier +
//! 6-key rollover) into ASCII bytes through a pure keymap, and records
//! pointer reports (buttons + absolute X/Y, best-effort — raw bytes are
//! the ground truth). The decoded bytes sit in a bounded pure-BSS FIFO that
//! the Road Pops tee's read path (`pop_byte`) hands to the shell's line
//! editor — the FIRST screen-side keystrokes reach the terminal.
//!
//! The keymap covers the usable ASCII subset: letters (shift → caps,
//! ctrl → the ADR 0008 D2 editing chords 0x01..0x1a), digits (shift → the
//! shifted symbol), Enter, Backspace, Tab, Space, the common punctuation,
//! and the editing nav cluster (arrows, Home, End, Delete) encoded as
//! `ESC [ <final>` sequences so the line editor stays byte-driven. Anything
//! outside the subset is honestly refused (no byte is invented).
//!
//! No libc, no POSIX, no allocation. The FIFO and driver state are fixed
//! BSS (the card-3d pattern); a full FIFO drops the newest byte and counts
//! it (bounded, never wraps).
//!
//! Host tests exercise the pure surface (keymap, FIFO, keyboard-report
//! decode); the `drain()` path is hardware-gated (a no-op when unarmed).

const std = @import("std");
const builtin = @import("builtin");
const xhci = @import("xhci.zig"); // I1/I2: the XHCI transport + enumerated HID devices
const app_events = @import("events.zig"); // Milestone 9 (claim 7206): application event queues
const driving_award = @import("driving_award.zig"); // Milestone six G5: window focus query for event routing
const terminal = @import("terminal.zig"); // #1082 (ADR 0020 A5): window-front-end key routing
const svclock = @import("svclock.zig"); // claim 9498 follow-on: the keyboard decode interleaves WIN + EV state
const klog = @import("klog.zig"); // M49 SD5 (#1132): the terminal copy audit line

pub const max_fifo: usize = 64;

/// The longest key sequence the keymap can produce (a modified function key
/// or Insert, e.g. `ESC [ 15;5~`).
pub const max_key_bytes: usize = 7;

/// HID keyboard boot-protocol modifier bit masks.
const mod_lctrl: u8 = 0x01;
const mod_lshift: u8 = 0x02;
const mod_rctrl: u8 = 0x10;
const mod_rshift: u8 = 0x20;

/// The `input` monitor command's report shape.
pub const Report = struct {
    armed: bool,
    fifo_used: usize,
    fifo_max: usize,
    dropped: usize,
    events: usize,
    kb_mods: u8,
    kb_last_usage: u8,
    kb_last_byte: u8,
    ptr_buttons: u8,
    ptr_x: u16,
    ptr_y: u16,
    ptr_reports: usize,
};

// ---------------------------------------------------------------------------
// Driver state (pure BSS)
// ---------------------------------------------------------------------------

var armed_global: bool = false;

var fifo: [max_fifo]u8 = undefined;
var fifo_head: usize = 0;
var fifo_count: usize = 0;
var dropped: usize = 0;
var events: usize = 0;

var kb_mods: u8 = 0;
var kb_held: [6]u8 = [_]u8{0} ** 6;
var kb_last_usage: u8 = 0;
var kb_last_byte: u8 = 0;

var ptr_buttons: u8 = 0;
var ptr_x: u16 = 0;
var ptr_y: u16 = 0;
var ptr_reports: usize = 0;
/// Card U4 (claim 4993): the pointer became observable (at least one
/// report) and the click edge (buttons 0 -> nonzero) latches here until
/// the window manager consumes it via `take_click`.
var ptr_valid: bool = false;
var ptr_click_pending: bool = false;
/// M49 SD5 (#1132): the left-button press sample, latched even when a whole
/// injected drag is drained in one pass (last-write-wins state would lose
/// the down edge). Consumed by `take_press`.
var ptr_press_pending: bool = false;
var ptr_press_x: u16 = 0;
var ptr_press_y: u16 = 0;
/// Card U5 (claim 0935): the Alt+Tab chord (modifier 0x04/0x40 + Tab
/// usage 0x2b) latches here — the shell idle loop consumes it as a
/// focus-cycle request (ADR 0008 D4's keyboard cycling). M32 WMS8 Gate 5
/// (issue #628): the OTHER geometry chords (tile/master/min/max/ws/
/// fullscreen/aot) are DELETED — WMS5 Gate 2 drained them to the WM; the
/// applied primitives remain, driven by `dui`/SET_STATE, but the kernel no
/// longer self-decides them from the keyboard.
var alt_tab_pending: bool = false;
var alt_tab_shift: bool = false;
/// Arc4 #238: Ctrl+Shift+B lowers the focused window to back. KEPT — the WM
/// does not serve this chord yet (no WM coverage would regress shim mode).
var lower_back_pending: bool = false;
/// M21 W10: Alt+arrow movement. KEPT — the WM does not serve this chord yet.
var move_pending_dx: i32 = 0;
var move_pending_dy: i32 = 0;
var move_pending_fine: bool = false;

// M32 WMS8 Gate 5 (issue #628): the geometry chord pending flags for
// workspace switch/cycle, tile toggle, master swap, minimize, maximize,
// fullscreen, and always-on-top are DELETED — WMS5 Gate 2 drained the
// geometry-policy decision to the WM (kind-21 WM_KEY -> handle_wm_key ->
// SET_WINDOW/SET_STATE). With a WM registered the kernel's own consumers
// were provably dormant (gated behind !wm_owns_input); per WMS8's delete
// rule (the W5 matrix re-ran green while seated) they are removed. Shim
// end-state: the drained geometry chords now do nothing with no WM (the
// issue's "no compositing policy" end-state).

/// Diagnostic hooks (kernel/src/main.zig wires these to uart_puts/uart_hex
/// under `--input`; null in host tests and the default VM).
pub var debug: ?*const fn ([]const u8) void = null;
pub var debug_hex: ?*const fn (u64) void = null;

fn dbg(bytes: []const u8) void {
    if (debug) |d| d(bytes);
}
fn dbg_hex(v: u64) void {
    if (debug_hex) |d| d(v);
}

/// Arm the input path (called by kernel/src/main.zig after the XHCI
/// transport is up and the devices are enumerated). Until armed, `drain()`
/// is a no-op and `pop_byte` returns null.
pub fn arm() void {
    armed_global = true;
}

pub fn armed() bool {
    return armed_global;
}

// ---------------------------------------------------------------------------
// HID-usage → ASCII keymap (pure — host-testable)
// ---------------------------------------------------------------------------

/// M80k (#1727): the terminal hygiene chords. `ctrl-shift-K` clears the
/// scrollback (the ED 3 the user could not type), `ctrl-shift-R` is the
/// soft reset (styles and modes, grid intact) and `ctrl-shift-alt-R` the
/// full RIS. Split out as a pure classifier so the chord table is
/// pinnable without a window: the dispatch site stays a switch, and the
/// "alt is the RIS, not the soft reset" rule is testable rather than
/// eyeballed. Everything else is no chord.
pub const HygieneChord = enum { clear_scrollback, soft_reset, ris };

/// The chord for one key-DOWN edge, or null when this is not a hygiene
/// chord. The modifiers are the same booleans the chrome table tests, so
/// the two can never disagree about what a chord is.
pub fn hygieneChord(usage: u8, shift: bool, alt: bool, ctrl: bool) ?HygieneChord {
    if (!ctrl or !shift) return null;
    return switch (usage) {
        0x0e => .clear_scrollback, // 'k'
        0x15 => if (alt) .ris else .soft_reset, // 'r'
        else => null,
    };
}

/// Map a HID keyboard boot-protocol usage ID to an ASCII byte, applying
/// shift when set. Returns null for usages outside the usable subset (the
/// card's honest bound: no invented bytes).
pub fn hid_to_ascii(usage: u8, shift: bool) ?u8 {
    if (usage >= 0x04 and usage <= 0x1d) {
        // a..z
        return if (shift) 'A' + (usage - 0x04) else 'a' + (usage - 0x04);
    }
    if (usage >= 0x1e and usage <= 0x27) {
        // 1..0 (the top row)
        const unshifted = "1234567890";
        const shifted = "!@#$%^&*()";
        const i = usage - 0x1e;
        return if (shift) shifted[i] else unshifted[i];
    }
    return switch (usage) {
        0x28 => '\n', // Enter / Return
        0x2a => 0x08, // Backspace
        0x2b => '\t', // Tab
        0x2c => ' ', // Space
        0x2d => if (shift) '_' else '-',
        0x2e => if (shift) '+' else '=',
        0x2f => if (shift) '{' else '[',
        0x30 => if (shift) '}' else ']',
        0x31 => if (shift) '|' else '\\',
        0x33 => if (shift) ':' else ';',
        0x34 => if (shift) '"' else '\'',
        0x35 => if (shift) '~' else '`',
        0x36 => if (shift) '<' else ',',
        0x37 => if (shift) '>' else '.',
        0x38 => if (shift) '?' else '/',
        else => null,
    };
}

fn emitCsiTilde(out: *[max_key_bytes]u8, number: u8, modifier: u8) usize {
    out[0] = 0x1b;
    out[1] = '[';
    var i: usize = 2;
    if (number >= 10) {
        out[i] = '0' + number / 10;
        i += 1;
    }
    out[i] = '0' + number % 10;
    i += 1;
    if (modifier != 1) {
        out[i] = ';';
        out[i + 1] = '0' + modifier;
        i += 2;
    }
    out[i] = '~';
    return i + 1;
}

fn emitFunctionKey(out: *[max_key_bytes]u8, usage: u8, modifier: u8) usize {
    switch (usage) {
        0x3a...0x3d => { // F1-F4: xterm's unmodified SS3 form.
            const final = 'P' + (usage - 0x3a);
            if (modifier == 1) {
                out[0] = 0x1b;
                out[1] = 'O';
                out[2] = final;
                return 3;
            }
            out[0] = 0x1b;
            out[1] = '[';
            out[2] = '1';
            out[3] = ';';
            out[4] = '0' + modifier;
            out[5] = final;
            return 6;
        },
        0x3f => return emitCsiTilde(out, 15, modifier), // F5
        0x40 => return emitCsiTilde(out, 17, modifier), // F6
        0x41 => return emitCsiTilde(out, 18, modifier), // F7
        0x42 => return emitCsiTilde(out, 19, modifier), // F8
        0x43 => return emitCsiTilde(out, 20, modifier), // F9
        0x44 => return emitCsiTilde(out, 21, modifier), // F10
        0x46 => return emitCsiTilde(out, 23, modifier), // F11
        0x45 => return emitCsiTilde(out, 24, modifier), // F12
        else => return 0,
    }
}

/// Decode one HID keyboard usage (with shift/alt/ctrl modifiers) into 0-7
/// bytes of console input, written to `out`; returns the byte count (0 =
/// the usage is outside the usable subset — no bytes are invented). The
/// editing nav cluster arrives as `ESC [ <final>` sequences, modified nav
/// uses xterm's `CSI 1;<modifier><final>` form, and F1-F12/Insert use
/// xterm's SS3/CSI-tilde forms. Ctrl + a-z remains the ASCII control-code
/// mapping (ADR 0008 D2); printable keys still use `hid_to_ascii`.
pub fn hid_to_bytes(usage: u8, shift: bool, alt: bool, ctrl: bool, out: *[max_key_bytes]u8) usize {
    const modifier: u8 = 1 + @as(u8, if (shift) 1 else 0) +
        @as(u8, if (alt) 2 else 0) + @as(u8, if (ctrl) 4 else 0);

    const nav_final: u8 = switch (usage) {
        0x4f => 'C', // Right
        0x50 => 'D', // Left
        0x51 => 'B', // Down
        0x52 => 'A', // Up
        0x4a => 'H', // Home
        0x4d => 'F', // End
        else => 0,
    };
    if (nav_final != 0) {
        if (modifier == 1) {
            out[0] = 0x1b;
            out[1] = '[';
            out[2] = nav_final;
            return 3;
        }
        out[0] = 0x1b;
        out[1] = '[';
        out[2] = '1';
        out[3] = ';';
        out[4] = '0' + modifier;
        out[5] = nav_final;
        return 6;
    }

    // Function keys and Insert have xterm modifier forms too, so they are
    // decoded before the ctrl-letter fast path below.
    switch (usage) {
        0x3a...0x3d, 0x3f...0x46 => return emitFunctionKey(out, usage, modifier),
        0x70 => return emitCsiTilde(out, 2, modifier), // Insert
        else => {},
    }

    if (ctrl) {
        if (usage >= 0x04 and usage <= 0x1d) {
            out[0] = (usage - 0x04) + 0x01; // Ctrl-A..Ctrl-Z
            return 1;
        }
        // Preserve the original honest refusal for Ctrl+Enter, Ctrl+Space,
        // and every other non-letter/non-nav usage.
        return 0;
    }

    switch (usage) {
        0x29 => { // Escape (lone ESC — the line editor treats it as a no-op key)
            out[0] = 0x1b;
            return 1;
        },
        0x4b => { // PageUp (the shell's scroll interceptor consumes CSI 5 ~)
            out[0] = 0x1b;
            out[1] = '[';
            out[2] = '5';
            out[3] = '~';
            return 4;
        },
        0x4e => { // PageDown (the shell's scroll interceptor consumes CSI 6 ~)
            out[0] = 0x1b;
            out[1] = '[';
            out[2] = '6';
            out[3] = '~';
            return 4;
        },
        0x4c => { // Delete (forward)
            out[0] = 0x1b;
            out[1] = '[';
            out[2] = '3';
            out[3] = '~';
            return 4;
        },
        else => {},
    }
    if (hid_to_ascii(usage, shift)) |b| {
        out[0] = b;
        return 1;
    }
    return 0;
}

// ---------------------------------------------------------------------------
// Bounded byte FIFO (the line-editor feed)
// ---------------------------------------------------------------------------

fn push_byte(b: u8) void {
    if (fifo_count >= max_fifo) {
        dropped += 1;
        return;
    }
    fifo[(fifo_head + fifo_count) % max_fifo] = b;
    fifo_count += 1;
}

/// Pop the next decoded keyboard byte, or null when the FIFO is empty.
/// The Road Pops tee consults this before falling back to serial.
pub fn pop_byte() ?u8 {
    if (fifo_count == 0) return null;
    const b = fifo[fifo_head];
    fifo_head = (fifo_head + 1) % max_fifo;
    fifo_count -= 1;
    return b;
}

// ---------------------------------------------------------------------------
// Arc5 #245: Compose state machine (ADR 0014)
// ---------------------------------------------------------------------------

/// Compose state: IDLE (normal) or WAITING (Alt held, awaiting second key).
const ComposeState = enum { idle, waiting };

/// A single compose pair: two HID usage IDs → Unicode codepoint.
const ComposePair = struct {
    first: u8,
    second: u8,
    codepoint: u21,
};

/// Compile-time compose table: ~27 pairs for common accented characters.
/// Alt + first_key + second_key → Unicode codepoint.
const compose_table = [_]ComposePair{
    // Alt + e + vowel → acute accent
    .{ .first = 0x08, .second = 0x04, .codepoint = 0x00e1 }, // e+a → á
    .{ .first = 0x08, .second = 0x05, .codepoint = 0x00e9 }, // e+e → é
    .{ .first = 0x08, .second = 0x0c, .codepoint = 0x00ed }, // e+i → í
    .{ .first = 0x08, .second = 0x12, .codepoint = 0x00f3 }, // e+o → ó
    .{ .first = 0x08, .second = 0x18, .codepoint = 0x00fa }, // e+u → ú
    // Alt + u + vowel → umlaut
    .{ .first = 0x18, .second = 0x04, .codepoint = 0x00e4 }, // u+a → ä
    .{ .first = 0x18, .second = 0x05, .codepoint = 0x00eb }, // u+e → ë
    .{ .first = 0x18, .second = 0x0c, .codepoint = 0x00ef }, // u+i → ï
    .{ .first = 0x18, .second = 0x12, .codepoint = 0x00f6 }, // u+o → ö
    .{ .first = 0x18, .second = 0x18, .codepoint = 0x00fc }, // u+u → ü
    // Alt + ` + vowel → grave accent
    .{ .first = 0x35, .second = 0x04, .codepoint = 0x00e0 }, // `+a → à
    .{ .first = 0x35, .second = 0x05, .codepoint = 0x00e8 }, // `+e → è
    .{ .first = 0x35, .second = 0x0c, .codepoint = 0x00ec }, // `+i → ì
    .{ .first = 0x35, .second = 0x12, .codepoint = 0x00f2 }, // `+o → ò
    .{ .first = 0x35, .second = 0x18, .codepoint = 0x00f9 }, // `+u → ù
    // Alt + n + n → tilde
    .{ .first = 0x11, .second = 0x11, .codepoint = 0x00f1 }, // n+n → ñ
    // Alt + c → cedilla
    .{ .first = 0x06, .second = 0x06, .codepoint = 0x00e7 }, // c+c → ç
};

/// Current compose state (BSS, bounded, no allocation).
var compose_state: ComposeState = .idle;
/// The first key of a pending compose sequence (HID usage).
var compose_first: u8 = 0;

/// Look up a compose pair in the table. Returns the Unicode codepoint or null.
fn compose_lookup(first: u8, second: u8) ?u21 {
    for (compose_table) |pair| {
        if (pair.first == first and pair.second == second) return pair.codepoint;
    }
    return null;
}

/// Reset compose state (called on Alt release or focus change).
pub fn compose_reset() void {
    compose_state = .idle;
}

/// List all compose sequences for the `compose` monitor command.
pub fn compose_list(con: anytype) void {
    con.print_line("compose: Alt + key1 + key2 → Unicode codepoint");
    for (compose_table) |pair| {
        con.puts("  Alt+");
        con.puts(&[_]u8{hid_to_ascii(pair.first, false) orelse '?'});
        con.puts("+");
        con.puts(&[_]u8{hid_to_ascii(pair.second, false) orelse '?'});
        con.puts(" → U+");
        var buf: [4]u8 = undefined;
        var v = pair.codepoint;
        var i: usize = 4;
        while (i > 0) : (i -= 1) {
            const nib = @as(u8, @intCast(v & 0xf));
            buf[i - 1] = if (nib < 10) '0' + nib else 'a' + nib - 10;
            v >>= 4;
        }
        con.print_line(&buf);
    }
}

// ---------------------------------------------------------------------------
// Report decode (keyboard boot report + best-effort absolute pointer)
// ---------------------------------------------------------------------------

/// Map raw HID keyboard boot report modifier byte to ADR 0009 modifier flags.
pub fn hid_modifiers_to_flags(mods: u8) u16 {
    var flags: u16 = 0;
    if ((mods & mod_lshift) != 0 or (mods & mod_rshift) != 0) flags |= app_events.MOD_SHIFT;
    if ((mods & mod_lctrl) != 0 or (mods & mod_rctrl) != 0) flags |= app_events.MOD_CTRL;
    if ((mods & 0x04) != 0 or (mods & 0x40) != 0) flags |= app_events.MOD_ALT;
    if ((mods & 0x08) != 0 or (mods & 0x80) != 0) flags |= app_events.MOD_CMD;
    return flags;
}

/// Decode an 8-byte HID keyboard boot report: byte 0 = modifier, bytes 2-7
/// = up to six held keycodes (the boot protocol's rollover limit). A keycode
/// present now but not in the previously-held set is a key-DOWN; its ASCII
/// byte (shift applied) is pushed. The held set is then updated.
/// Card E2 (claim 7206): when a user window is focused, KEY_DOWN / KEY_UP
/// events are pushed to the owning process's event queue.
pub fn decode_keyboard_report(rep: []const u8) void {
    if (rep.len < 8) return;
    // Per-domain locks (claim 9498 follow-on): the decode interleaves WIN
    // state (driving_award focus/wm-ownership reads, the wm_key_hook) with
    // EV pushes (app_events) — take win+ev in canonical order for the whole
    // decode. acquire_missing: a nested call (input.drain under the idle
    // loop's own win+ev hold) takes nothing; a fresh entry (the custom-
    // virtio input hook, IRQ-masked) spins only against a completing
    // holder, which the IRQ-masking lock guarantees.
    const took = svclock.acquire_missing(svclock.dom_bit(.win) | svclock.dom_bit(.ev));
    defer svclock.release_set(took);
    const mods = rep[0];
    const flags = hid_modifiers_to_flags(mods);
    const shift = (flags & app_events.MOD_SHIFT) != 0;
    const ctrl = (flags & app_events.MOD_CTRL) != 0;
    const alt = (flags & app_events.MOD_ALT) != 0;
    kb_mods = mods;
    var keys: [6]u8 = [_]u8{0} ** 6;
    for (rep[2..8], 0..) |k, i| keys[i] = k;
    // Card U5 (ADR 0008 D4): Alt+Tab cycles window focus — the
    // chord is consumed as a window-manager signal across all windows.
    // C2 (M15): capture Shift for reverse cycling.
    for (keys) |k| {
        // M32 WMS5 Gate 2 (claim 4278): while a WM owns input, the raw
        // key stream fans out to it on key-DOWN edges (kind 21 WM_KEY) —
        // the WM, not the kernel, decides geometry from chords. The
        // kernel's own geometry pending flags below are gated behind
        // !wm_owns_input; plain KEY_DOWN/KEY_UP delivery to the focused
        // window (card E2) is unchanged.
        if (k != 0 and driving_award.wm_owns_input) {
            var edge = true;
            for (kb_held) |h| {
                if (h == k) {
                    edge = false;
                    break;
                }
            }
            if (edge) {
                if (driving_award.wm_key_hook) |hook| hook(k, flags);
            }
        }
        if (k == 0x2b and alt) {
            var held = false;
            for (kb_held) |h| {
                if (h == k) {
                    held = true;
                    break;
                }
            }
            if (!held and !driving_award.wm_owns_input) {
                alt_tab_pending = true;
                alt_tab_shift = shift;
            }
        }
        // M21 W4: Alt+` workspace-CYCLE is DELETED (M32 WMS8 Gate 5, issue
        // #628) — WMS5 Gate 2 drained it to the WM (kind-21 -> handle_wm_key
        // usage 0x35). The kernel no longer self-cycles workspaces.
        // Arc4 #238: Ctrl+Shift+B lowers focused window to back (KEPT — no
        // WM coverage yet).
        if (k == 0x05 and (flags & app_events.MOD_CTRL) != 0 and (flags & app_events.MOD_SHIFT) != 0) {
            var held = false;
            for (kb_held) |h| {
                if (h == k) {
                    held = true;
                    break;
                }
            }
            if (!held and !driving_award.wm_owns_input) {
                lower_back_pending = true;
            }
        }
        // M32 WMS8 Gate 5 (issue #628): the Ctrl chord consumers for
        // workspace-switch (Ctrl+F1/F2/F3), tile (Ctrl+T), master-swap
        // (Ctrl+M), minimize (Ctrl+N), maximize (Ctrl+Shift+M), always-
        // on-top (Ctrl+Shift+T), and F11 fullscreen are DELETED — WMS5
        // Gate 2 drained all of them to the WM (kind-21 -> handle_wm_key >
        // SET_WINDOW/SET_STATE). The Ctrl+Shift+A about chord was already
        // deleted in Gate 3. The WM serves these; the kernel no longer
        // self-decides them.
        // M21 W10: Alt+arrow for keyboard window movement (KEPT — no WM
        // coverage yet).
        if (alt) {
            var dx: i32 = 0;
            var dy: i32 = 0;
            if (k == 0x4f) dx = 16; // Right arrow
            if (k == 0x50) dx = -16; // Left arrow
            if (k == 0x52) dy = -16; // Up arrow
            if (k == 0x51) dy = 16; // Down arrow
            if (dx != 0 or dy != 0) {
                var held = false;
                for (kb_held) |h| {
                    if (h == k) {
                        held = true;
                        break;
                    }
                }
                if (!held and !driving_award.wm_owns_input) {
                    // Alt+Shift = fine movement (1px).
                    move_pending_dx = if (shift) @divTrunc(dx, 16) else dx;
                    move_pending_dy = if (shift) @divTrunc(dy, 16) else dy;
                    move_pending_fine = shift;
                }
            }
        }
    }

    if (driving_award.focused_owner()) |owner_pid| {
        // #1082 (ADR 0020 Amendment A, A5): when the focused window is a
        // terminal front-end, encode its keys with the SAME `hid_to_bytes`
        // encoder the console path uses and push them into the bound
        // terminal's input queue — the shell reads them via `/dev/tty`.
        // Focus is NOT stolen, and WIN_CLOSE/WIN_RESIZE/WIN_FOCUS are
        // delivered by their own paths (this only redirects key bytes).
        if (terminal.windowTerminal(driving_award.focused_window_id())) |tt| {
            const scr = terminal.screenForTerminal(tt);
            for (keys) |k| {
                if (k == 0) continue;
                var held = false;
                for (kb_held) |h| {
                    if (h == k) {
                        held = true;
                        break;
                    }
                }
                if (held) continue;
                kb_last_usage = k;
                // M49 SD5 (#1132): terminal-window chrome chords are consumed
                // by the kernel presentation, not typed into the shell:
                //   PageUp/PageDown + Shift+Up/Down  scroll the view
                //   Shift+Home / Shift+End           jump to oldest / tail
                //   Ctrl+Shift+C                     copy the selection
                //   Ctrl+Shift+V                     paste the clipboard into the tty
                if (k == 0x4b or k == 0x4e) { // PageUp / PageDown
                    const up = (k == 0x4b);
                    var wheel_ok = false;
                    if (scr) |s| {
                        // M73i (#1635): with mouse tracking on, PageUp/
                        // PageDown report `CSI 64/65` (xterm wheel) instead
                        // of scrolling the view; Shift — or modes off —
                        // keeps local scroll-by-8 byte-identically. The
                        // report sits at the cursor cell (pointer-cell
                        // coords need the WM geometry seam — future note).
                        if ((s.mouse_1000 or s.mouse_1002 or s.mouse_1003) and !shift) {
                            var enc: [24]u8 = undefined;
                            const n = terminal.encodeMouse(
                                s.mouse_1006,
                                .wheel,
                                if (up) 0 else 1,
                                @intCast(s.cursorCol() + 1),
                                @intCast(s.cursorLine() + 1),
                                @intCast(s.cols),
                                &enc,
                            );
                            if (n > 0) {
                                _ = tt.pushMouse(enc[0..n]);
                                wheel_ok = true;
                            }
                        }
                        if (!wheel_ok) s.scrollBy(if (up) 8 else -8);
                    }
                    events += 1;
                    continue;
                }
                if (shift and k == 0x52) { // Shift+Up
                    if (scr) |s| s.scrollBy(1);
                    events += 1;
                    continue;
                }
                if (shift and k == 0x51) { // Shift+Down
                    if (scr) |s| s.scrollBy(-1);
                    events += 1;
                    continue;
                }
                if (shift and k == 0x4a) { // Shift+Home
                    if (scr) |s| s.scrollBy(32767);
                    events += 1;
                    continue;
                }
                if (shift and k == 0x4d) { // Shift+End
                    if (scr) |s| s.scrollReset();
                    events += 1;
                    continue;
                }
                if (ctrl and shift and k == 0x06) { // Ctrl+Shift+C
                    const copied = terminal.copySelectionToClipboard(driving_award.focused_window_id());
                    var msg: [48]u8 = undefined;
                    const m = std.fmt.bufPrint(&msg, "tty: copy {d} bytes\n", .{copied}) catch "tty: copy\n";
                    klog.line(m);
                    events += 1;
                    continue;
                }
                if (ctrl and shift and k == 0x19) { // Ctrl+Shift+V (HID 'v')
                    // M73e (#1629): the reverse of copy — the clipboard
                    // lands in the bound tty's input queue in one push,
                    // wrapped in `\e[200~ … \e[201~` when the app asked
                    // for DECSET 2004 (GOSH requests it at startup).
                    const pasted = terminal.pasteFromClipboard(driving_award.focused_window_id());
                    var pmsg: [48]u8 = undefined;
                    const pm = std.fmt.bufPrint(&pmsg, "tty: paste {d} bytes\n", .{pasted}) catch "tty: paste\n";
                    klog.line(pm);
                    events += 1;
                    continue;
                }
                // M73k (#1637): Ctrl+Shift+F opens the kernel search bar —
                // same chrome-chord family as copy/paste (HID 'f' = 0x09).
                // Normal screen only: searchOpen refuses on the alt screen
                // (pinned class-A); the chord is still consumed there.
                if (ctrl and shift and k == 0x09) {
                    if (scr) |s| _ = s.searchOpen();
                    events += 1;
                    continue;
                }
                // M80k (#1727): the hygiene chords every terminal has and
                // this one did not — clear the scrollback, soft reset, full
                // reset. The cure for a grid wedged by an app that died
                // mid-escape, without closing the tab. Same chrome-chord
                // family (consumed here, before the tty queue, and never
                // injected as bytes), and the operations are the M80b /
                // M80e seams themselves rather than a second
                // implementation that could drift. Must sit ABOVE the
                // search-capture block below: a wedged grid is exactly
                // when the scan bar is most likely to be up, and a chord
                // swallowed as a search pattern is no cure at all.
                if (hygieneChord(k, shift, alt, ctrl)) |hyg| {
                    if (scr) |s| {
                        switch (hyg) {
                            .clear_scrollback => {
                                const before = s.historyCount();
                                // #1757: the count alone was unexplained (run
                                // 03 dropped 24 rows from a ~300-row fill —
                                // exactly one viewport). The ring-state
                                // figures AT READ TIME make the number
                                // diagnosable: `hist`+`used` is the unified
                                // depth, `pend` the tty output still queued
                                // (a non-zero pend means the chord raced the
                                // pump), `vis` the focused window's visible
                                // rows for the viewport-cap comparison.
                                const used_before = s.used;
                                const total_before = s.lineCount();
                                var vis: usize = 0;
                                if (driving_award.find_user_window(driving_award.focused_window_id())) |w| {
                                    vis = if (w.h > driving_award.user_title_h)
                                        (w.h - driving_award.user_title_h) / driving_award.font_metrics.cell_h
                                    else
                                        1;
                                }
                                const pend = tt.pendingOut();
                                s.clearScrollback();
                                // Snap to the tail even when the clear
                                // itself was a no-op (the alternate
                                // screen's ring belongs to the primary
                                // grid): "scroll to tail" is half the
                                // promise, and it is unconditional.
                                s.scrollReset();
                                var cmsg: [112]u8 = undefined;
                                const cm = std.fmt.bufPrint(&cmsg, "tty: clear {d} lines (hist={d} used={d} total={d} vis={d} pend={d})\n", .{
                                    before - s.historyCount(),
                                    before,
                                    used_before,
                                    total_before,
                                    vis,
                                    pend,
                                }) catch "tty: clear\n";
                                klog.line(cm);
                            },
                            .soft_reset => {
                                // The scan bar is presentation, not parser
                                // state: leave it up and the "grid intact"
                                // reset is invisible behind the overlay.
                                s.searchExit();
                                s.softReset();
                                klog.line("tty: reset soft\n");
                            },
                            .ris => {
                                // RIS wipes the grid itself, overlay and all
                                // — no searchExit needed (and none wanted:
                                // restoring a covered row into a grid that
                                // no longer exists would resurrect bytes).
                                s.hardReset();
                                klog.line("tty: reset full\n");
                            },
                        }
                        // #1757: composite the chord's effect NOW, under the
                        // win+ev hold this decode already runs in. Post-1747
                        // the chords decode on the idle task while the
                        // compositor drains in the shell loop, so the
                        // framebuffer lagged the marker by an unbounded
                        // number of passes — and the claim-0680 snapshot
                        // fired from this very marker raced it (observed on
                        // live-term-depth run 03: the RIS snapshot shipped
                        // the pre-RIS grid). Present-then-marker makes the
                        // frame fresh by construction: the request can only
                        // arrive after the marker, and the pixels it must
                        // capture are already on the scanout. The chord
                        // mutates the Screen directly, so the dirty flag
                        // needs marking first — the same user_present seam
                        // the tty write path uses — or paint_scene finds a
                        // clean scene and paints nothing (observed). Host
                        // builds skip the hardware half (the established
                        // !is_test gate: paint/composite reach MMU + virtio
                        // GPU MMIO that aborts a host test binary).
                        if (comptime !builtin.is_test) {
                            _ = driving_award.user_present(driving_award.focused_window_id());
                            _ = driving_award.composite();
                        }
                    }
                    events += 1;
                    continue;
                }
                // M73k: while the bar is up, its keys are captured by the
                // presentation and never reach the app's tty. Enter/next,
                // Shift+Enter/prev, Esc exits, backspace edits, everything
                // mappable types into the pattern; unmappable keys (arrows
                // etc.) keep falling through to the normal path below.
                if (scr) |s| {
                    if (s.searchActive()) {
                        var sk: ?terminal.Screen.SearchKey = null;
                        if (k == 0x29) {
                            sk = .esc;
                        } else if (k == 0x28) {
                            sk = if (shift) .prev else .enter;
                        } else if (k == 0x2a) {
                            sk = .backspace;
                        } else if (hid_to_ascii(k, shift)) |ch| {
                            sk = .{ .ch = ch };
                        }
                        if (sk) |key| {
                            if (s.searchFeed(key)) |n| {
                                var smsg: [48]u8 = undefined;
                                const sm = std.fmt.bufPrint(&smsg, "tty: search: {d} matches\n", .{n}) catch "tty: search\n";
                                klog.line(sm);
                            }
                            events += 1;
                            continue;
                        }
                    }
                }
                var kout: [max_key_bytes]u8 = undefined;
                const kn = hid_to_bytes(k, shift, alt, ctrl, &kout);
                if (kn > 0) {
                    kb_last_byte = kout[kn - 1]; // the sequence's final byte
                    _ = tt.pushInput(kout[0..kn]);
                    events += 1;
                }
            }
            kb_held = keys;
            return;
        }
        // Milestone 9 Card E2: Route keyboard events to focused user window process!
        // 1. Key DOWN: keys present now but not in kb_held
        for (keys) |k| {
            if (k == 0) continue;
            var held = false;
            for (kb_held) |h| {
                if (h == k) {
                    held = true;
                    break;
                }
            }
            if (!held) {
                kb_last_usage = k;
                const ascii_char: u32 = if (hid_to_ascii(k, shift)) |ch| ch else 0;
                // Arc5 #245: Compose state machine (ADR 0014).
                // When Alt is held, route through compose logic instead of
                // immediately dispatching KEY_DOWN.
                if (alt and k != 0 and compose_state == .idle) {
                    // Alt + key: start compose sequence (save pending event).
                    compose_state = .waiting;
                    compose_first = k;
                } else if (compose_state == .waiting) {
                    // Second key of compose sequence: look up the pair.
                    const cp = compose_lookup(compose_first, k);
                    compose_state = .idle;
                    if (cp) |codepoint| {
                        // Compose match: dispatch KEY_DOWN with Unicode codepoint.
                        app_events.push(owner_pid, .{
                            .kind = app_events.KEY_DOWN,
                            .flags = flags,
                            .seq = 0,
                            .arg0 = k,
                            .arg1 = codepoint,
                        });
                        events += 1;
                    } else {
                        // Compose miss: dispatch both keys as normal KEY_DOWN.
                        const first_ascii: u32 = if (hid_to_ascii(compose_first, shift)) |ch| ch else 0;
                        app_events.push(owner_pid, .{
                            .kind = app_events.KEY_DOWN,
                            .flags = flags,
                            .seq = 0,
                            .arg0 = compose_first,
                            .arg1 = first_ascii,
                        });
                        events += 1;
                        app_events.push(owner_pid, .{
                            .kind = app_events.KEY_DOWN,
                            .flags = flags,
                            .seq = 0,
                            .arg0 = k,
                            .arg1 = ascii_char,
                        });
                        events += 1;
                    }
                } else {
                    // Normal KEY_DOWN: no compose active.
                    app_events.push(owner_pid, .{
                        .kind = app_events.KEY_DOWN,
                        .flags = flags,
                        .seq = 0,
                        .arg0 = k,
                        .arg1 = ascii_char,
                    });
                    events += 1;
                }
                // Arc4 #236: translate PageUp/PageDown to MOUSE_SCROLL events.
                // On VZ the absolute pointer has no wheel byte, so this is the
                // working scroll path. ScrollView/HScrollBar consume kind 12.
                if (k == 0x4b) { // PageUp
                    // Packed arg0: magnitude=1, sign=0 (scroll up).
                    app_events.push(owner_pid, .{
                        .kind = app_events.MOUSE_SCROLL,
                        .flags = 0,
                        .seq = 0,
                        .arg0 = 1, // magnitude=1, bit15=0 (up)
                        .arg1 = 0,
                    });
                    events += 1;
                } else if (k == 0x4e) { // PageDown
                    // Packed arg0: magnitude=1, sign=1 (scroll down).
                    app_events.push(owner_pid, .{
                        .kind = app_events.MOUSE_SCROLL,
                        .flags = 0,
                        .seq = 0,
                        .arg0 = 0x8001, // magnitude=1, bit15=1 (down)
                        .arg1 = 0,
                    });
                    events += 1;
                }
            }
        }
        // 2. Key UP: keys in kb_held but not in keys now
        for (kb_held) |h| {
            if (h == 0) continue;
            var still_held = false;
            for (keys) |k| {
                if (k == h) {
                    still_held = true;
                    break;
                }
            }
            if (!still_held) {
                const ascii_char: u32 = if (hid_to_ascii(h, shift)) |ch| ch else 0;
                app_events.push(owner_pid, .{
                    .kind = app_events.KEY_UP,
                    .flags = flags,
                    .seq = 0,
                    .arg0 = h,
                    .arg1 = ascii_char,
                });
            }
        }
    } else {
        // Terminal / console route (existing behavior)
        for (keys) |k| {
            if (k == 0) continue;
            var held = false;
            for (kb_held) |h| {
                if (h == k) {
                    held = true;
                    break;
                }
            }
            if (!held) {
                kb_last_usage = k;
                if (alt and k == 0x2b) {
                    continue;
                }
                var out: [max_key_bytes]u8 = undefined;
                const n = hid_to_bytes(k, shift, false, ctrl, &out);
                if (n > 0) {
                    kb_last_byte = out[n - 1]; // the sequence's final byte
                    var i: usize = 0;
                    while (i < n) : (i += 1) push_byte(out[i]);
                    events += 1;
                }
            }
        }
    }
    kb_held = keys;
}

/// Decode one absolute-pointer report (best-effort: buttons + little-
/// endian X/Y). The raw bytes are the ground truth; the absolute report's
/// exact word order is a claim-time observation, recorded honestly.
///
/// Public since claim 9367: the custom-virtio INPUT channel's kind-2
/// pointer messages are handed here verbatim — the exact path an XHCI
/// pointer report takes — so injected pointers are ordinary pointers
/// downstream (cursor, `dui` click-to-focus, `input` ptr-* counters).
pub fn decode_pointer_report(rep: []const u8) void {
    if (rep.len < 3) return;
    const prev_buttons = ptr_buttons;
    ptr_buttons = rep[0];
    ptr_x = @as(u16, rep[1]) | (@as(u16, rep[2]) << 8);
    if (rep.len >= 5) {
        ptr_y = @as(u16, rep[3]) | (@as(u16, rep[4]) << 8);
    }
    ptr_reports += 1;
    ptr_valid = true;
    if (prev_buttons == 0 and (ptr_buttons & 0x01) != 0) {
        ptr_click_pending = true;
        ptr_press_pending = true;
        ptr_press_x = ptr_x;
        ptr_press_y = ptr_y;
    }
}

/// The shell-idle-loop drain: poll each enumerated device's interrupt-IN
/// endpoint for a completed report, decode it, and (for the keyboard) push
/// the decoded bytes. No-op when unarmed (default VM / host tests).
/// U4 (M43 card U4, issue #1035): the `!d.present` skip is the detach half
/// of the lifecycle contract — a quiesced HID device contributes no reports,
/// no FIFO bytes, and no pointer motion, while the surviving devices drain
/// exactly as before. No new code: the guard predates lifecycle and already
/// reads the registry every pass, so a mid-run detach takes effect on the
/// next idle loop without disturbing armed state.
pub fn drain() void {
    if (!armed_global) return;
    var i: usize = 0;
    while (i < xhci.EnumMax) : (i += 1) {
        const d = xhci.enum_devs[i];
        if (!d.present or d.ep_in_num == 0) continue;
        // Non-blocking: a no-pending-event poll is one cheap event-ring
        // read, so the idle loop is not slowed by the blocking budget
        // (`usb report` keeps the blocking path).
        if (xhci.xhci_poll_intr_nb(d.slot_id)) {
            const rep = xhci.xhci_report(d.slot_id);
            const bytes = rep.bytes[0..rep.len];
            switch (xhci.hid_kind[i]) {
                .keyboard => decode_keyboard_report(bytes),
                // The absolute pointer enumerates with bInterfaceProtocol 0
                // (not a boot mouse), so hid_kind is .unknown — the raw
                // report is still recorded best-effort.
                .mouse, .unknown => decode_pointer_report(bytes),
            }
        }
    }
}

pub fn report() Report {
    return .{
        .armed = armed_global,
        .fifo_used = fifo_count,
        .fifo_max = max_fifo,
        .dropped = dropped,
        .events = events,
        .kb_mods = kb_mods,
        .kb_last_usage = kb_last_usage,
        .kb_last_byte = kb_last_byte,
        .ptr_buttons = ptr_buttons,
        .ptr_x = ptr_x,
        .ptr_y = ptr_y,
        .ptr_reports = ptr_reports,
    };
}

/// Card U4 (claim 4993): the pointer snapshot for the window manager.
pub const PointerState = struct { x: u16, y: u16, buttons: u8, valid: bool };
pub fn pointer_state() PointerState {
    return .{ .x = ptr_x, .y = ptr_y, .buttons = ptr_buttons, .valid = ptr_valid };
}

/// Card U4: a consumed click edge (the pointer cell, HID logical units).
pub const Click = struct { x: u16, y: u16 };

/// M49 SD5 (#1132): the latched left-press position, consumed once.
pub fn take_press() ?Click {
    if (!ptr_press_pending) return null;
    ptr_press_pending = false;
    return .{ .x = ptr_press_x, .y = ptr_press_y };
}

/// Consume the latched click edge (buttons 0 -> nonzero). Returns the
/// click's pointer cell or null when no click is pending.
pub fn take_click() ?Click {
    if (!ptr_click_pending) return null;
    ptr_click_pending = false;
    return .{ .x = ptr_x, .y = ptr_y };
}

/// Card U5: consume the Alt+Tab chord edge.
pub fn take_alt_tab() bool {
    if (take_alt_tab_shift() != null) return true;
    return false;
}

/// C2 (M15): Alt still held (either left 0x04 or right 0x40).
pub fn alt_held() bool {
    return (kb_mods & 0x04) != 0 or (kb_mods & 0x40) != 0;
}

/// C2 (M15): consume the Alt+Tab edge with Shift direction — null if no edge, else `true` if Shift+Tab (reverse), `false` if plain Tab.
pub fn take_alt_tab_shift() ?bool {
    if (!alt_tab_pending) return null;
    alt_tab_pending = false;
    const s = alt_tab_shift;
    alt_tab_shift = false;
    return s;
}

/// Arc4 #238: consume the Ctrl+Shift+B chord edge.
pub fn take_lower_back() bool {
    if (!lower_back_pending) return false;
    lower_back_pending = false;
    return true;
}

/// M32 WMS8 Gate 5 (issue #628): the take_* accessors for the drained
/// geometry chords (workspace switch/cycle, tile toggle, master swap,
/// minimize, maximize, fullscreen, always-on-top) are DELETED — the WM owns
/// those decisions now; the kernel no longer self-consumes them.
pub const MoveDelta = struct { dx: i32, dy: i32 };

/// M21 W10: consume the Alt+arrow movement edge. Returns (dx, dy) or
/// null if no movement is pending.
pub fn take_move() ?MoveDelta {
    if (move_pending_dx == 0 and move_pending_dy == 0) return null;
    const result = MoveDelta{ .dx = move_pending_dx, .dy = move_pending_dy };
    move_pending_dx = 0;
    move_pending_dy = 0;
    return result;
}

// ---------------------------------------------------------------------------
// Tests (host-side; pure surface, no hardware)
// ---------------------------------------------------------------------------

test "input: hid_to_ascii maps the usable subset (unshifted + shifted)" {
    try std.testing.expectEqual(@as(?u8, 'a'), hid_to_ascii(0x04, false));
    try std.testing.expectEqual(@as(?u8, 'A'), hid_to_ascii(0x04, true));
    try std.testing.expectEqual(@as(?u8, 'z'), hid_to_ascii(0x1d, false));
    try std.testing.expectEqual(@as(?u8, 'Z'), hid_to_ascii(0x1d, true));
    try std.testing.expectEqual(@as(?u8, '1'), hid_to_ascii(0x1e, false));
    try std.testing.expectEqual(@as(?u8, '!'), hid_to_ascii(0x1e, true));
    try std.testing.expectEqual(@as(?u8, '0'), hid_to_ascii(0x27, false));
    try std.testing.expectEqual(@as(?u8, ')'), hid_to_ascii(0x27, true));
    try std.testing.expectEqual(@as(?u8, '\n'), hid_to_ascii(0x28, false));
    try std.testing.expectEqual(@as(?u8, ' '), hid_to_ascii(0x2c, false));
    try std.testing.expectEqual(@as(?u8, '-'), hid_to_ascii(0x2d, false));
    try std.testing.expectEqual(@as(?u8, '_'), hid_to_ascii(0x2d, true));
    try std.testing.expectEqual(@as(?u8, 0x08), hid_to_ascii(0x2a, false));
}

test "input: usages outside the usable subset are refused (no invented bytes)" {
    try std.testing.expectEqual(@as(?u8, null), hid_to_ascii(0x00, false));
    try std.testing.expectEqual(@as(?u8, null), hid_to_ascii(0x29, false)); // Escape
    try std.testing.expectEqual(@as(?u8, null), hid_to_ascii(0x39, false)); // Caps Lock
    try std.testing.expectEqual(@as(?u8, null), hid_to_ascii(0x4c, false)); // Pause
    try std.testing.expectEqual(@as(?u8, null), hid_to_ascii(0xe0, false)); // Left Ctrl
}

test "input: hid_to_bytes maps the nav cluster to ESC sequences" {
    var out: [max_key_bytes]u8 = undefined;
    // Up arrow -> ESC [ A (3 bytes).
    var n = hid_to_bytes(0x52, false, false, false, &out);
    try std.testing.expectEqual(@as(usize, 3), n);
    try std.testing.expectEqualSlices(u8, "\x1b[A", out[0..n]);
    // Left arrow -> ESC [ D.
    n = hid_to_bytes(0x50, false, false, false, &out);
    try std.testing.expectEqualSlices(u8, "\x1b[D", out[0..n]);
    // End -> ESC [ F; Delete -> ESC [ 3 ~ (4 bytes).
    n = hid_to_bytes(0x4d, false, false, false, &out);
    try std.testing.expectEqualSlices(u8, "\x1b[F", out[0..n]);
    n = hid_to_bytes(0x4c, false, false, false, &out);
    try std.testing.expectEqual(@as(usize, 4), n);
    try std.testing.expectEqualSlices(u8, "\x1b[3~", out[0..n]);
    // PageUp -> ESC [ 5 ~, PageDown -> ESC [ 6 ~ (4 bytes each) — the
    // shell's scroll interceptor consumes these (M18 T1).
    n = hid_to_bytes(0x4b, false, false, false, &out);
    try std.testing.expectEqual(@as(usize, 4), n);
    try std.testing.expectEqualSlices(u8, "\x1b[5~", out[0..n]);
    n = hid_to_bytes(0x4e, false, false, false, &out);
    try std.testing.expectEqual(@as(usize, 4), n);
    try std.testing.expectEqualSlices(u8, "\x1b[6~", out[0..n]);
    // Escape -> a lone ESC byte (the line editor treats it as a no-op).
    n = hid_to_bytes(0x29, false, false, false, &out);
    try std.testing.expectEqual(@as(usize, 1), n);
    try std.testing.expectEqual(@as(u8, 0x1b), out[0]);
    // Not a printable, not a nav key: no bytes invented.
    try std.testing.expectEqual(@as(usize, 0), hid_to_bytes(0x39, false, false, false, &out)); // Caps Lock
}

test "input: hid_to_bytes maps the full function-key and Insert vocabulary" {
    const cases = [_]struct { usage: u8, want: []const u8 }{
        .{ .usage = 0x3a, .want = "\x1bOP" }, // F1
        .{ .usage = 0x3b, .want = "\x1bOQ" }, // F2
        .{ .usage = 0x3c, .want = "\x1bOR" }, // F3
        .{ .usage = 0x3d, .want = "\x1bOS" }, // F4
        .{ .usage = 0x3f, .want = "\x1b[15~" }, // F5
        .{ .usage = 0x40, .want = "\x1b[17~" }, // F6
        .{ .usage = 0x41, .want = "\x1b[18~" }, // F7
        .{ .usage = 0x42, .want = "\x1b[19~" }, // F8
        .{ .usage = 0x43, .want = "\x1b[20~" }, // F9
        .{ .usage = 0x44, .want = "\x1b[21~" }, // F10
        .{ .usage = 0x46, .want = "\x1b[23~" }, // F11
        .{ .usage = 0x45, .want = "\x1b[24~" }, // F12
        .{ .usage = 0x70, .want = "\x1b[2~" }, // Insert
    };
    for (cases) |tc| {
        var out: [max_key_bytes]u8 = undefined;
        const n = hid_to_bytes(tc.usage, false, false, false, &out);
        try std.testing.expectEqualSlices(u8, tc.want, out[0..n]);
    }

    // Modified function keys use the same xterm modifier parameter as the
    // navigation keys; this also exercises the seven-byte bound.
    var out: [max_key_bytes]u8 = undefined;
    var n = hid_to_bytes(0x3a, true, false, false, &out); // Shift-F1
    try std.testing.expectEqualSlices(u8, "\x1b[1;2P", out[0..n]);
    n = hid_to_bytes(0x3f, false, false, true, &out); // Ctrl-F5
    try std.testing.expectEqualSlices(u8, "\x1b[15;5~", out[0..n]);
}

test "input: hid_to_bytes maps every modified navigation final" {
    const nav = [_]struct { usage: u8, final: u8 }{
        .{ .usage = 0x52, .final = 'A' }, // Up
        .{ .usage = 0x51, .final = 'B' }, // Down
        .{ .usage = 0x4f, .final = 'C' }, // Right
        .{ .usage = 0x50, .final = 'D' }, // Left
        .{ .usage = 0x4a, .final = 'H' }, // Home
        .{ .usage = 0x4d, .final = 'F' }, // End
    };
    const modifiers = [_]struct { shift: bool, alt: bool, ctrl: bool, code: u8 }{
        .{ .shift = true, .alt = false, .ctrl = false, .code = 2 },
        .{ .shift = false, .alt = true, .ctrl = false, .code = 3 },
        .{ .shift = false, .alt = false, .ctrl = true, .code = 5 },
        .{ .shift = true, .alt = true, .ctrl = true, .code = 8 },
    };
    for (nav) |key| {
        for (modifiers) |mod| {
            var out: [max_key_bytes]u8 = undefined;
            const n = hid_to_bytes(key.usage, mod.shift, mod.alt, mod.ctrl, &out);
            try std.testing.expectEqual(@as(usize, 6), n);
            try std.testing.expectEqual(@as(u8, 0x1b), out[0]);
            try std.testing.expectEqual(@as(u8, '['), out[1]);
            try std.testing.expectEqual(@as(u8, '1'), out[2]);
            try std.testing.expectEqual(@as(u8, ';'), out[3]);
            try std.testing.expectEqual(@as(u8, '0' + mod.code), out[4]);
            try std.testing.expectEqual(key.final, out[5]);
        }
    }
}

test "input: hid_to_bytes maps ctrl+a-z to ASCII control codes" {
    var out: [max_key_bytes]u8 = undefined;
    // Ctrl-A (usage 0x04) -> 0x01; Ctrl-C (usage 0x06) -> 0x03; Ctrl-Z -> 0x1a.
    try std.testing.expectEqual(@as(usize, 1), hid_to_bytes(0x04, false, false, true, &out));
    try std.testing.expectEqual(@as(u8, 0x01), out[0]);
    try std.testing.expectEqual(@as(usize, 1), hid_to_bytes(0x06, false, false, true, &out));
    try std.testing.expectEqual(@as(u8, 0x03), out[0]);
    try std.testing.expectEqual(@as(usize, 1), hid_to_bytes(0x1d, false, false, true, &out));
    try std.testing.expectEqual(@as(u8, 0x1a), out[0]);
    // Ctrl + non-letter is outside the usable subset (no invented bytes).
    try std.testing.expectEqual(@as(usize, 0), hid_to_bytes(0x28, false, false, true, &out)); // Ctrl+Enter
}

test "input: WMS8 Gate 5 — the drained geometry chords fan out but the kernel no longer consumes them (claim 9879)" {
    // WMS5 Gate 2 drained the geometry chords (tile/master/min/max/ws/
    // fullscreen/aot) to the WM; WMS8 Gate 5 DELETES the kernel's chord
    // consumers. The raw key stream STILL fans out to the WM (kind 21) on
    // key-DOWN edges, and the kernel has no pending-flag/take_* for those
    // chords anymore. Ctrl+Shift+B (lower-back) and Alt+arrows (move) stay
    // kernel-consumed until the WM covers them.
    _ = driving_award.focus(0);
    fifo_count = 0;
    fifo_head = 0;
    events = 0;
    kb_held = [_]u8{0} ** 6;
    var key_calls: usize = 0;
    var last_usage: u8 = 0;
    var last_flags: u16 = 0;
    const capture = struct {
        var calls: *usize = undefined;
        var usage: *u8 = undefined;
        var flags: *u16 = undefined;
        fn hook(u: u8, f: u16) void {
            calls.* += 1;
            usage.* = u;
            flags.* = f;
        }
    };
    capture.calls = &key_calls;
    capture.usage = &last_usage;
    capture.flags = &last_flags;

    // No WM (shim mode): the drained Ctrl+T does NOT set a pending flag
    // (the consumer is deleted) and does NOT fan out (no WM).
    driving_award.wm_owns_input = false;
    driving_award.wm_key_hook = capture.hook;
    decode_keyboard_report(&[_]u8{ 0x01, 0, 0x17, 0, 0, 0, 0, 0 }); // Ctrl+T
    try std.testing.expectEqual(@as(usize, 0), key_calls); // no fan-out without a WM
    decode_keyboard_report(&[_]u8{ 0, 0, 0, 0, 0, 0, 0, 0 }); // release

    // With a WM registered: the raw key fans out to the WM (kind 21); the
    // kernel has no chord consumer left to trigger.
    driving_award.wm_owns_input = true;
    decode_keyboard_report(&[_]u8{ 0x01, 0, 0x17, 0, 0, 0, 0, 0 }); // Ctrl+T
    try std.testing.expectEqual(@as(usize, 1), key_calls); // ...the WM got it
    try std.testing.expectEqual(@as(u8, 0x17), last_usage);
    try std.testing.expectEqual(@as(u16, app_events.MOD_CTRL), last_flags);
    decode_keyboard_report(&[_]u8{ 0, 0, 0, 0, 0, 0, 0, 0 }); // release

    // KEPT chord — Ctrl+Shift+B (lower-back): while no WM, the kernel still
    // self-consumes it (no WM coverage yet -> zero regression).
    driving_award.wm_key_hook = null;
    driving_award.wm_owns_input = false;
    decode_keyboard_report(&[_]u8{ 0x01 | 0x02, 0, 0x05, 0, 0, 0, 0, 0 }); // Ctrl+Shift+B
    try std.testing.expect(take_lower_back());
    decode_keyboard_report(&[_]u8{ 0, 0, 0, 0, 0, 0, 0, 0 }); // release
    // KEPT chord — Alt+Right (move): still self-consumed in shim mode.
    decode_keyboard_report(&[_]u8{ 0x04, 0, 0x4f, 0, 0, 0, 0, 0 }); // Alt+Right
    const mv = take_move().?;
    try std.testing.expectEqual(@as(i32, 16), mv.dx);
    try std.testing.expectEqual(@as(i32, 0), mv.dy);
    decode_keyboard_report(&[_]u8{ 0, 0, 0, 0, 0, 0, 0, 0 }); // release

    // Cleanup: WM mode restored (the raw fan-out stays available).
    driving_award.wm_owns_input = false;
}

test "input: keyboard report decode pushes a ctrl chord and an arrow sequence" {
    _ = driving_award.focus(0);
    fifo_count = 0;
    fifo_head = 0;
    events = 0;
    kb_held = [_]u8{0} ** 6;
    // Left Ctrl (0x01) held + 'a' (usage 0x04) pressed -> 0x01 (Ctrl-A).
    decode_keyboard_report(&[_]u8{ 0x01, 0, 0x04, 0, 0, 0, 0, 0 });
    try std.testing.expectEqual(@as(usize, 1), fifo_count);
    try std.testing.expectEqual(@as(u8, 0x01), pop_byte().?);
    // Release; then Up arrow (usage 0x52) -> ESC [ A (3 bytes).
    decode_keyboard_report(&[_]u8{ 0, 0, 0, 0, 0, 0, 0, 0 });
    decode_keyboard_report(&[_]u8{ 0, 0, 0x52, 0, 0, 0, 0, 0 });
    try std.testing.expectEqual(@as(usize, 3), fifo_count);
    try std.testing.expectEqual(@as(u8, 0x1b), pop_byte().?);
    try std.testing.expectEqual(@as(u8, '['), pop_byte().?);
    try std.testing.expectEqual(@as(u8, 'A'), pop_byte().?);
    try std.testing.expectEqual(@as(?u8, null), pop_byte());
    try std.testing.expectEqual(@as(usize, 2), events);
}

test "input: keyboard report decode pushes key-down bytes with shift" {
    // Reset the module state (host tests share the globals).
    _ = driving_award.focus(0);
    fifo_count = 0;
    fifo_head = 0;
    events = 0;
    dropped = 0;
    kb_held = [_]u8{0} ** 6;
    // Shift held + 'I' key (usage 0x0c) pressed: byte 0 = 0x02 (left shift),
    // byte 2 = 0x0c.
    decode_keyboard_report(&[_]u8{ 0x02, 0, 0x0c, 0, 0, 0, 0, 0 });
    try std.testing.expectEqual(@as(usize, 1), fifo_count);
    try std.testing.expectEqual(@as(u8, 'I'), pop_byte().?);
    try std.testing.expectEqual(@as(?u8, null), pop_byte());
    try std.testing.expectEqual(@as(usize, 1), events);
    // Key released: no new key-down, no new byte.
    decode_keyboard_report(&[_]u8{ 0x02, 0, 0, 0, 0, 0, 0, 0 });
    try std.testing.expectEqual(@as(usize, 0), fifo_count);
}

test "input: a held key does not re-fire (no repeat on unchanged report)" {
    _ = driving_award.focus(0);
    fifo_count = 0;
    fifo_head = 0;
    events = 0;
    kb_held = [_]u8{0} ** 6;
    const pressed = [_]u8{ 0, 0, 0x04, 0, 0, 0, 0, 0 }; // 'a' held
    decode_keyboard_report(&pressed);
    decode_keyboard_report(&pressed); // unchanged → no second push
    try std.testing.expectEqual(@as(usize, 1), fifo_count);
    try std.testing.expectEqual(@as(u8, 'a'), pop_byte().?);
    try std.testing.expectEqual(@as(?u8, null), pop_byte());
    try std.testing.expectEqual(@as(usize, 1), events);
}

test "input: FIFO is bounded and drops on overflow, never wrapping" {
    fifo_count = 0;
    fifo_head = 0;
    dropped = 0;
    var i: usize = 0;
    while (i < max_fifo + 3) : (i += 1) push_byte('x');
    try std.testing.expectEqual(@as(usize, max_fifo), fifo_count);
    try std.testing.expectEqual(@as(usize, 3), dropped);
    // Drains exactly the first max_fifo bytes in order.
    var n: usize = 0;
    while (pop_byte()) |b| {
        try std.testing.expectEqual(@as(u8, 'x'), b);
        n += 1;
    }
    try std.testing.expectEqual(@as(usize, max_fifo), n);
}

test "input: pointer report is recorded best-effort (buttons + LE X/Y)" {
    ptr_buttons = 0;
    ptr_x = 0;
    ptr_y = 0;
    ptr_reports = 0;
    decode_pointer_report(&[_]u8{ 0x01, 0x34, 0x12, 0x78, 0x56 });
    try std.testing.expectEqual(@as(u8, 0x01), ptr_buttons);
    try std.testing.expectEqual(@as(u16, 0x1234), ptr_x);
    try std.testing.expectEqual(@as(u16, 0x5678), ptr_y);
    try std.testing.expectEqual(@as(usize, 1), ptr_reports);
}

test "input: drain is a no-op when unarmed" {
    armed_global = false;
    const before = fifo_count;
    drain();
    try std.testing.expectEqual(before, fifo_count);
    try std.testing.expect(!report().armed);
    armed_global = true;
    try std.testing.expect(report().armed);
    armed_global = false;
}

test "input: hid_modifiers_to_flags maps modifiers to ADR 0009 bitmasks" {
    try std.testing.expectEqual(@as(u16, 0), hid_modifiers_to_flags(0));
    try std.testing.expectEqual(app_events.MOD_SHIFT, hid_modifiers_to_flags(0x02)); // left shift
    try std.testing.expectEqual(app_events.MOD_SHIFT, hid_modifiers_to_flags(0x20)); // right shift
    try std.testing.expectEqual(app_events.MOD_CTRL, hid_modifiers_to_flags(0x01)); // left ctrl
    try std.testing.expectEqual(app_events.MOD_CTRL, hid_modifiers_to_flags(0x10)); // right ctrl
    try std.testing.expectEqual(app_events.MOD_ALT, hid_modifiers_to_flags(0x04)); // left alt
    try std.testing.expectEqual(app_events.MOD_ALT, hid_modifiers_to_flags(0x40)); // right alt
    try std.testing.expectEqual(app_events.MOD_CMD, hid_modifiers_to_flags(0x08)); // left cmd
    try std.testing.expectEqual(app_events.MOD_CMD, hid_modifiers_to_flags(0x80)); // right cmd
    try std.testing.expectEqual(app_events.MOD_SHIFT | app_events.MOD_CTRL | app_events.MOD_ALT | app_events.MOD_CMD, hid_modifiers_to_flags(0x02 | 0x01 | 0x04 | 0x08));
}

test "input: keyboard routing delivers KEY_DOWN and KEY_UP events to focused user window" {
    app_events.init();
    driving_award.arm();
    // Open a user window owned by pid 2 (delivers WIN_FOCUS)
    const res = driving_award.user_open(10, 10, 100, 100, 2);
    try std.testing.expect(res == .opened);
    const win_id = res.opened;
    try std.testing.expectEqual(@as(?usize, 2), driving_award.focused_owner());
    _ = app_events.pop(2); // Consume WIN_FOCUS

    fifo_count = 0;
    fifo_head = 0;
    kb_held = [_]u8{0} ** 6;

    // Press 'a' (usage 0x04) with left shift (0x02)
    decode_keyboard_report(&[_]u8{ 0x02, 0, 0x04, 0, 0, 0, 0, 0 });
    // Terminal FIFO is untouched
    try std.testing.expectEqual(@as(usize, 0), fifo_count);

    // Event queue for pid 2 receives KEY_DOWN
    try std.testing.expectEqual(@as(usize, 1), app_events.pending(2));
    const ev1 = app_events.pop(2).?;
    try std.testing.expectEqual(app_events.KEY_DOWN, ev1.kind);
    try std.testing.expectEqual(app_events.MOD_SHIFT, ev1.flags);
    try std.testing.expectEqual(@as(u32, 0x04), ev1.arg0);
    try std.testing.expectEqual(@as(u32, 'A'), ev1.arg1);

    // Release 'a'
    decode_keyboard_report(&[_]u8{ 0x02, 0, 0, 0, 0, 0, 0, 0 });
    // Event queue for pid 2 receives KEY_UP
    try std.testing.expectEqual(@as(usize, 1), app_events.pending(2));
    const ev2 = app_events.pop(2).?;
    try std.testing.expectEqual(app_events.KEY_UP, ev2.kind);
    try std.testing.expectEqual(app_events.MOD_SHIFT, ev2.flags);
    try std.testing.expectEqual(@as(u32, 0x04), ev2.arg0);
    try std.testing.expectEqual(@as(u32, 'A'), ev2.arg1);

    // Clean up window
    _ = driving_award.user_close(win_id);
}

test "input: a window-bound terminal receives encoded keys, not app events (#1082 A5)" {
    app_events.init();
    driving_award.arm();
    for (&terminal.terminals) |*tt| tt.reset();
    const res = driving_award.user_open(10, 10, 100, 100, 2);
    try std.testing.expect(res == .opened);
    const win_id = res.opened;
    _ = app_events.pop(2); // Consume WIN_FOCUS
    const th = terminal.create(2) orelse return error.TestUnexpectedResult;
    const tt = terminal.get(th).?;
    try std.testing.expect(tt.attachWindow(win_id));

    fifo_count = 0;
    fifo_head = 0;
    kb_held = [_]u8{0} ** 6;
    // Space (usage 0x2c) with no modifiers: the bound terminal gets the byte,
    // the console FIFO stays empty, and no KEY_DOWN app event is pushed.
    decode_keyboard_report(&[_]u8{ 0, 0, 0x2c, 0, 0, 0, 0, 0 });
    try std.testing.expectEqual(@as(usize, 0), fifo_count);
    try std.testing.expectEqual(@as(usize, 0), app_events.pending(2));
    var buf: [8]u8 = undefined;
    try std.testing.expectEqual(@as(usize, 1), tt.readInput(&buf));
    try std.testing.expectEqual(@as(u8, ' '), buf[0]);

    // Closing the bound window auto-detaches the terminal (A6) and the
    // terminal survives, buffered, unattached.
    _ = driving_award.user_close(win_id);
    try std.testing.expect(!tt.isAttached());
    try std.testing.expect(terminal.windowTerminal(win_id) == null);
    for (&terminal.terminals) |*t2| t2.reset();
}

test "input: the M80k hygiene chords are exactly ctrl-shift-K, ctrl-shift-R and ctrl-shift-alt-R (#1727)" {
    // The chord TABLE, pinned without a window: a third modifier on 'r'
    // is the difference between a soft reset and a full RIS, so it has to
    // be a table fact rather than an if-ordering accident.
    try std.testing.expectEqual(HygieneChord.clear_scrollback, hygieneChord(0x0e, true, false, true).?);
    try std.testing.expectEqual(HygieneChord.soft_reset, hygieneChord(0x15, true, false, true).?);
    try std.testing.expectEqual(HygieneChord.ris, hygieneChord(0x15, true, true, true).?);
    // Alt alone must not buy a RIS: without ctrl+shift there is no chord.
    try std.testing.expect(hygieneChord(0x15, false, true, true) == null);
    try std.testing.expect(hygieneChord(0x15, true, false, false) == null);
    try std.testing.expect(hygieneChord(0x15, false, false, true) == null);
    try std.testing.expect(hygieneChord(0x15, true, true, false) == null);
    // Not letters: no accidental capture of the existing chrome chords
    // (ctrl-shift-C/V/F) or of a bare keystroke.
    try std.testing.expect(hygieneChord(0x06, true, false, true) == null);
    try std.testing.expect(hygieneChord(0x19, true, false, true) == null);
    try std.testing.expect(hygieneChord(0x09, true, false, true) == null);
    try std.testing.expect(hygieneChord(0x0e, false, false, false) == null);
    try std.testing.expect(hygieneChord(0x2c, false, false, false) == null); // Space
}

test "input: a hygiene chord is consumed before the tty queue — the app never sees the bytes (#1727)" {
    // The chord is a kernel presentation act, not typed input: whatever
    // the bound tty would have received for ctrl-shift-K (0x0b, the
    // kill-line control) must never reach it, and the app event queue
    // must stay empty exactly as a plain key does.
    app_events.init();
    driving_award.arm();
    for (&terminal.terminals) |*tt| tt.reset();
    const res = driving_award.user_open(10, 10, 100, 100, 2);
    try std.testing.expect(res == .opened);
    const win_id = res.opened;
    _ = app_events.pop(2); // Consume WIN_FOCUS
    const th = terminal.create(2) orelse return error.TestUnexpectedResult;
    const tt = terminal.get(th).?;
    try std.testing.expect(tt.attachWindow(win_id));
    const scr = terminal.screenForWindow(win_id).?;
    var b: [8]u8 = undefined;

    fifo_count = 0;
    fifo_head = 0;
    kb_held = [_]u8{0} ** 6;

    // Real scrollback first: 160 lines over the 128-row grid pushes rows
    // into the ring, so the clear chord has something to actually drop.
    var line_buf: [4 * 160]u8 = undefined;
    var w: usize = 0;
    for (0..160) |_| {
        @memcpy(line_buf[w..][0..4], "row\n");
        w += 4;
    }
    scr.feed(line_buf[0..w]);
    const before = scr.historyCount();
    const used_before = scr.used;
    try std.testing.expect(before > 0);
    try std.testing.expect(scr.lineCount() > scr.used); // grid+history is one space

    // ctrl-shift-k: LCTRL(0x01) + LSHIFT(0x02) + 'k' (usage 0x0e). The
    // ED 3 semantics: the ring goes, the live grid stays.
    decode_keyboard_report(&[_]u8{ 0x03, 0, 0x0e, 0, 0, 0, 0, 0 });
    try std.testing.expectEqual(@as(usize, 0), tt.readInput(&b));
    try std.testing.expectEqual(@as(usize, 0), app_events.pending(2));
    try std.testing.expectEqual(@as(usize, 0), fifo_count);
    try std.testing.expectEqual(@as(usize, 0), scr.historyCount());
    try std.testing.expectEqual(@as(usize, 0), scr.viewOffset());
    // The grid survives the clear (that is ED 3, not ED 2): the used row
    // count is untouched and the last written line is still its own text.
    try std.testing.expectEqual(used_before, scr.used);
    try std.testing.expectEqual(used_before, scr.lineCount());
    try std.testing.expect(std.mem.indexOf(u8, scr.line(used_before - 2), "row") != null);

    // ctrl-shift-r: LCTRL + LSHIFT + 'r' (usage 0x15) — no alt, so the
    // soft reset: no bytes, and the cursor stays where it was.
    const cur = scr.cursorLine();
    decode_keyboard_report(&[_]u8{ 0, 0, 0, 0, 0, 0, 0, 0 }); // release
    decode_keyboard_report(&[_]u8{ 0x03, 0, 0x15, 0, 0, 0, 0, 0 });
    try std.testing.expectEqual(@as(usize, 0), tt.readInput(&b));
    try std.testing.expectEqual(@as(usize, 0), app_events.pending(2));
    try std.testing.expectEqual(cur, scr.cursorLine());

    // ctrl-shift-alt-r: the full RIS. Still no bytes, still no app event,
    // and the grid itself is blanked.
    decode_keyboard_report(&[_]u8{ 0, 0, 0, 0, 0, 0, 0, 0 }); // release
    decode_keyboard_report(&[_]u8{ 0x07, 0, 0x15, 0, 0, 0, 0, 0 }); // ctrl+shift+alt
    try std.testing.expectEqual(@as(usize, 0), tt.readInput(&b));
    try std.testing.expectEqual(@as(usize, 0), app_events.pending(2));
    try std.testing.expectEqual(@as(usize, 0), scr.historyCount());
    try std.testing.expectEqual(@as(usize, 0), scr.lineCount() - 1);
    try std.testing.expect(std.mem.indexOf(u8, scr.line(0), "row") == null);

    _ = driving_award.user_close(win_id);
    for (&terminal.terminals) |*t2| t2.reset();
}
