//! VirelaiOS terminal (vt) seam (ADR 0020, issue #1072).
//!
//! A **terminal** is a bounded, hardware-free session buffer: an output ring
//! the owner process writes to, an input queue a front-end fills, and an
//! exclusive attach state naming the front-end (the raw serial console, a
//! TABWM window, a TCP/SSH session). It is the thing `GOSH.ELF`, `TERM.BIN`,
//! and remote sessions all sit on — see ADR 0020.
//!
//! Pure by construction: fixed arrays, no allocation, no hardware, no
//! syscalls. The device/front-end wiring lives outside this module (the
//! `/dev/tty` routing in `file_table.zig` is the next tranche).
//!
//! Overflow policy is explicit and counted:
//!   * output full -> drop the OLDEST byte (`out_dropped`), so output flows
//!     (serial/net). The window path never hits this: `writeWindow` streams
//!     write→grid in ring-sized chunks (#1630, M73f-1), so a TUI frame
//!     larger than the ring cannot drop;
//!   * input full  -> drop the NEWEST byte (`in_dropped`), so a key burst
//!     never evicts keys the owner has not read yet.

const std = @import("std");
const cell_mod = @import("terminal/cell.zig");
const mouse_mod = @import("terminal/mouse.zig");
const net_mod = @import("terminal/net.zig");
const screen_mod = @import("terminal/screen.zig");
const serial_mod = @import("terminal/serial.zig");
const session_mod = @import("terminal/session.zig");
const window_mod = @import("terminal/window.zig");

/// How many concurrent terminals the kernel tracks.
pub const max_terminals: usize = 4;

// ---------------------------------------------------------------------------
// The kernel terminal registry (bounded; no allocation).
// ---------------------------------------------------------------------------
pub var terminals: [max_terminals]Terminal = @as([max_terminals]Terminal, @splat(@as(Terminal, .{})));

/// #1082 (ADR 0020 Amendment A): the per-terminal presentation grid,
/// parallel to `terminals` and keyed by the same registry handle. Kept out
/// of `Terminal` so the object stays a pure byte session (D1).
pub var screens: [max_terminals]Screen = @as([max_terminals]Screen, @splat(@as(Screen, .{})));

/// Allocate a free terminal for `owner`, or null when all slots are taken.
pub fn create(owner: ?usize) ?usize {
    for (&terminals, 0..) |*t, i| {
        if (!t.in_use) {
            t.reset();
            t.in_use = true;
            t.owner_pid = owner;
            screens[i].reset();
            return i;
        }
    }
    return null;
}

/// The terminal at `handle`, or null when out of range / free.
pub fn get(handle: usize) ?*Terminal {
    if (handle >= max_terminals) return null;
    if (!terminals[handle].in_use) return null;
    return &terminals[handle];
}

/// Release a terminal slot (e.g. owner death). Clears everything.
pub fn release(handle: usize) void {
    if (handle >= max_terminals) return;
    terminals[handle].reset();
    screens[handle].reset();
}

/// The registry handle of `t`, or null when it is not a live slot.
pub fn handleOf(t: *const Terminal) ?usize {
    for (&terminals, 0..) |*x, i| {
        if (@intFromPtr(x) == @intFromPtr(t)) return i;
    }
    return null;
}

pub const term_image = @import("term_image.zig");

pub const Point = cell_mod.Point;
pub const Cell = cell_mod.Cell;
pub const empty_cell = cell_mod.empty_cell;
pub const CellStyle = cell_mod.CellStyle;
pub const colour_slot = cell_mod.colour_slot;
pub const default_colour = cell_mod.default_colour;
pub const rgb_colour = cell_mod.rgb_colour;
pub const Rgb = cell_mod.Rgb;
pub const empty_rgb = cell_mod.empty_rgb;
pub const default_cell_style = cell_mod.default_cell_style;
pub const Charset = cell_mod.Charset;
pub const styleForeground = cell_mod.styleForeground;
pub const styleBackground = cell_mod.styleBackground;
pub const styleBold = cell_mod.styleBold;
pub const styleDim = cell_mod.styleDim;
pub const styleItalic = cell_mod.styleItalic;
pub const styleUnderline = cell_mod.styleUnderline;
pub const styleReverse = cell_mod.styleReverse;
pub const xterm256Rgb = cell_mod.xterm256Rgb;

pub const reply_max = screen_mod.reply_max;
pub const osc_max = screen_mod.osc_max;
pub const title_max = screen_mod.title_max;
pub const grid_cols = screen_mod.grid_cols;
pub const grid_lines = screen_mod.grid_lines;
pub const history_lines = screen_mod.history_lines;
pub const HistoryBank = screen_mod.HistoryBank;
pub const Screen = screen_mod.Screen;

pub const out_capacity = session_mod.out_capacity;
pub const in_capacity = session_mod.in_capacity;
pub const FrontEnd = session_mod.FrontEnd;
pub const Terminal = session_mod.Terminal;

pub const MouseKind = mouse_mod.MouseKind;
pub const mouseReports = mouse_mod.mouseReports;
pub const encodeMouse = mouse_mod.encodeMouse;

pub const windowTerminal = window_mod.windowTerminal;
pub const detachWindow = window_mod.detachWindow;
pub const anyWindowAttached = window_mod.anyWindowAttached;
pub const pumpWindowOutput = window_mod.pumpWindowOutput;
pub const writeWindow = window_mod.writeWindow;
pub const screenOf = window_mod.screenOf;
pub const screenForWindow = window_mod.screenForWindow;
pub const screenForTerminal = window_mod.screenForTerminal;
pub const syncWindowCols = window_mod.syncWindowCols;
pub const copySelectionToClipboard = window_mod.copySelectionToClipboard;
pub const pasteFromClipboard = window_mod.pasteFromClipboard;

pub const net_challenge_len = net_mod.net_challenge_len;
pub const net_auth_line_max = net_mod.net_auth_line_max;
pub const net_auth_deadline = net_mod.net_auth_deadline;
pub const net_auth_tag = net_mod.net_auth_tag;
pub const NetAuthScheme = net_mod.NetAuthScheme;
pub const NetSeam = net_mod.NetSeam;
pub const attachedNet = net_mod.attachedNet;
pub const netAuthFail = net_mod.netAuthFail;
pub const netAuthSendChallenge = net_mod.netAuthSendChallenge;
pub const netAuthSendChallengeSeam = net_mod.netAuthSendChallengeSeam;
pub const netAuthVerdict = net_mod.netAuthVerdict;
pub const netAuthVerdictSeam = net_mod.netAuthVerdictSeam;
pub const pumpNetInput = net_mod.pumpNetInput;
pub const pumpNetInputSeam = net_mod.pumpNetInputSeam;
pub const pumpNetOutput = net_mod.pumpNetOutput;
pub const pumpNetOutputSeam = net_mod.pumpNetOutputSeam;

pub const setRuntimeConsole = serial_mod.setRuntimeConsole;
pub const attachedSerial = serial_mod.attachedSerial;
pub const pumpInput = serial_mod.pumpInput;
pub const pumpOutput = serial_mod.pumpOutput;
pub const pumpRuntimeInput = serial_mod.pumpRuntimeInput;
pub const pumpRuntimeOutput = serial_mod.pumpRuntimeOutput;
pub const pushBackRuntimeInput = serial_mod.pushBackRuntimeInput;

test "terminal: registry creates, looks up, and releases bounded slots" {
    for (&terminals) |*t| t.reset();
    const a = create(7) orelse return error.TestUnexpectedResult;
    const b = create(8) orelse return error.TestUnexpectedResult;
    try std.testing.expect(a != b);
    try std.testing.expectEqual(@as(?usize, 7), get(a).?.owner_pid);
    try std.testing.expectEqual(@as(?usize, 8), get(b).?.owner_pid);
    // Fill the rest; overflow is an honest null.
    var made: usize = 2;
    while (made < max_terminals) : (made += 1) {
        _ = create(null) orelse return error.TestUnexpectedResult;
    }
    try std.testing.expectEqual(@as(?usize, null), create(null));
    // Release frees a slot; the freed handle is a fresh empty terminal.
    release(a);
    try std.testing.expectEqual(@as(?*Terminal, null), get(a));
    const c = create(9) orelse return error.TestUnexpectedResult;
    try std.testing.expectEqual(@as(?usize, 9), get(c).?.owner_pid);
    for (&terminals) |*t| t.reset();
}
