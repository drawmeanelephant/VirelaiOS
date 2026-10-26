//! Monitor commands: shell-style utilities and personality (`echo`/`repeat`/`calc`/`clip`/`color`/`clear`/`compose`/`hex`/`which`/`type`/`sh`/`beans`/`elephant`/`sexiburger`).
//!
//! Split out of monitor.zig (#3755): declarations only — the
//! shared types, registry, dispatch, completion, and print/parse
//! helpers stay in the root and are aliased below so the moved
//! code is verbatim.

const std = @import("std");
const mon = @import("../monitor.zig");

const Monitor = mon.Monitor;
const ExecError = mon.ExecError;
const repeat_max_count = mon.repeat_max_count;
const repeat_max_bytes = mon.repeat_max_bytes;
const beans_max_count = mon.beans_max_count;
const err_prefix = mon.err_prefix;
const print_usage = mon.print_usage;
const parseInt = mon.parseInt;
const kernel_vf_allowed = mon.kernel_vf_allowed;
const trust_denied = mon.trust_denied;
const lookup = mon.lookup;
const exec = mon.exec;
const handoff = mon.handoff;
const virtio_file = mon.virtio_file;
const clipboard = mon.clipboard;
const input = mon.input;
const settings = mon.settings;
/// "  <label>: <value>"
fn print_plain_field(m: *Monitor, label: []const u8, value: []const u8) void {
    m.console.puts("  ");
    m.console.puts(label);
    m.console.puts(": ");
    m.console.puts(value);
}

/// Calculator utilities: 'calc history' reads and prints calc_hst.txt on
/// the HOST SHARE (M24 K5 — the persisted calculation history ring; HF6
/// moved it from /data to the share).
pub fn cmd_calc(m: *Monitor, args: []const []const u8) ExecError {
    if (args.len == 0 or std.mem.eql(u8, args[0], "history")) {
        const path = "calc_hst.txt";
        if (!kernel_vf_allowed(path, .read)) return trust_denied(m, "calc history", path);
        var buf: [2048]u8 = undefined;
        const got = virtio_file.read_whole(path, &buf) orelse {
            err_prefix(m);
            m.console.print_line("calc: no history file found (calc_hst.txt on the host share)");
            return .none;
        };
        if (got == 0) {
            m.console.print_line("calc: history is empty");
            return .none;
        }
        // Count lines for the header
        var lines: u32 = 0;
        for (buf[0..got]) |ch| {
            if (ch == '\n') lines += 1;
        }
        m.console.puts("calc history (");
        m.console.print_u64(lines);
        m.console.print_line(" entries):\n");
        m.console.puts(buf[0..got]);
        if (got > 0 and buf[got - 1] != '\n') m.console.puts("\n");
        return .none;
    }
    // M62h: Zig CALC.BIN (and its CLI `calc <expr>` path) is gone. GOCALC.ELF
    // is GUI-only and treats argv[1] as a result-file path, so do not exec it
    // with an expression. Fall through to the honest unknown-subcommand error.
    err_prefix(m);
    m.console.puts("unknown calc subcommand: ");
    m.console.print_line(args[0]);
    print_usage(m, lookup("calc").?);
    return .invalid_argument;
}

/// M22 D16 (issue #339): `which <name>` — locate a command by name.
/// Reports shell builtins, monitor commands, and ESP applications.
pub fn cmd_which(m: *Monitor, args: []const []const u8) ExecError {
    const name = args[0];
    // Check shell builtins first (the list from shell.zig dispatch_line)
    if (is_shell_builtin(name)) {
        m.console.puts(name);
        m.console.print_line(": shell builtin");
        return .none;
    }
    // Check monitor commands
    if (lookup(name)) |_| {
        m.console.puts(name);
        m.console.print_line(": monitor command");
        return .none;
    }
    // Check the host share listing (applications) — HF6: the ESP is gone.
    var res: virtio_file.ListResult = .{};
    if (virtio_file.list("", &res) == virtio_file.st_ok) {
        for (res.entries[0..res.count]) |e| {
            if (std.mem.eql(u8, e.name[0..e.name_len], name)) {
                m.console.puts(name);
                m.console.print_line(": host-share application");
                return .none;
            }
        }
    }
    // Not found
    m.console.puts(name);
    m.console.print_line(": not found");
    return .none;
}

/// Check if a name is a shell builtin (from shell.zig dispatch_line).
///
/// ADR 0005: a const []const u8 table would land in rodata holding
/// ABSOLUTE link-time pointers, which the flat kernel loader never
/// relocates — on real hardware the first comparison dereferenced a
/// garbage pointer and took a data abort (observed live 2026-08-25,
/// claim 5220: far=0x4129a on `which type`). `inline for` keeps the list
/// comptime so every literal is referenced PC-relative.
fn is_shell_builtin(name: []const u8) bool {
    inline for (.{
        "export", "set",     "unset", "env",   "printenv",
        "alias",  "unalias", "exit",  "sh",    "prompt",
        "type",   "true",    "false", "jobs",  "fg",
        "if",     "for",     "while", "break", "continue",
        "fn",     "cd",
    }) |b| {
        if (std.mem.eql(u8, name, b)) return true;
    }
    return false;
}

// ---------------------------------------------------------------------------
// Shell-style utility commands
// ---------------------------------------------------------------------------

pub fn cmd_echo(m: *Monitor, args: []const []const u8) ExecError {
    // #2649: ONE write for the whole echoed line — the fleet asserts the
    // marker whole-line (serial-exact / serial-contains), and a
    // per-argument composition lets a foreign write split it mid-token.
    var buf: [384]u8 = undefined;
    var len: usize = 0;
    for (args, 0..) |arg, index| {
        if (index > 0) {
            if (len >= buf.len) break;
            buf[len] = ' ';
            len += 1;
        }
        const n = @min(arg.len, buf.len - len);
        @memcpy(buf[len..][0..n], arg[0..n]);
        len += n;
        if (n < arg.len) break; // truncated honestly at the bound
    }
    if (len < buf.len) {
        buf[len] = '\n';
        len += 1;
    }
    m.console.puts(buf[0..len]);
    return .none;
}

/// Milestone fourteen card S1 (claim 0169): the terminal half of the shared
/// clipboard. With no args it pastes the current contents (the EL1h read of
/// the SAME buffer the slots 38/39 syscalls use); with args it joins them
/// (space-separated, the `echo` shape) and stores the result — the
/// M18 T5: toggle ANSI terminal colors on or off.
pub fn cmd_color(m: *Monitor, args: []const []const u8) ExecError {
    if (args.len == 0) {
        m.console.puts("color: ");
        m.console.print_line(if (settings.get_color()) "on" else "off");
        return .none;
    }
    if (std.mem.eql(u8, args[0], "on") or std.mem.eql(u8, args[0], "1") or std.mem.eql(u8, args[0], "true")) {
        _ = settings.set("color", "on");
        _ = settings.set("color", "on");
        m.console.print_line("color: on");
        return .none;
    }
    if (std.mem.eql(u8, args[0], "off") or std.mem.eql(u8, args[0], "0") or std.mem.eql(u8, args[0], "false")) {
        _ = settings.set("color", "off");
        m.console.print_line("color: off");
        return .none;
    }
    m.console.print_line("usage: color [on|off]");
    return .none;
}

/// bounded-copy proof from the shell. Truncation is honest: the staging
/// buffer is the clipboard capacity, so an over-long paste stores exactly
/// `clipboard.capacity` bytes and reports it.
pub fn cmd_clip(m: *Monitor, args: []const []const u8) ExecError {
    if (args.len == 0) {
        var buf: [clipboard.capacity]u8 = undefined;
        const n = clipboard.get(&buf);
        if (n == 0) {
            m.console.puts("clip: empty\n");
        } else {
            m.console.puts("clip: ");
            m.console.puts(buf[0..n]);
            m.console.puts("\n");
        }
        return .none;
    }
    var staging: [clipboard.capacity]u8 = undefined;
    var len: usize = 0;
    for (args, 0..) |arg, index| {
        if (index > 0) {
            if (len < staging.len) {
                staging[len] = ' ';
                len += 1;
            }
        }
        for (arg) |ch| {
            if (len >= staging.len) break;
            staging[len] = ch;
            len += 1;
        }
    }
    const stored = clipboard.set(staging[0..len]);
    m.console.puts("clip: stored ");
    m.console.print_u64(stored);
    m.console.puts(" bytes\n");
    return .none;
}

pub fn cmd_clear(m: *Monitor, args: []const []const u8) ExecError {
    _ = args;
    // ANSI erase-in-display + cursor home. On terminals without ANSI
    // support this sequence is ignored harmlessly; the fallback is a
    // documented no-op. Deterministic and testable at the byte level.
    m.console.puts("\x1b[2J\x1b[H");
    return .none;
}

/// `compose` — list available Alt+key compose sequences (Arc5 #245, ADR 0014).
pub fn cmd_compose(m: *Monitor, args: []const []const u8) ExecError {
    _ = args;
    input.compose_list(&m.console);
    return .none;
}

pub fn cmd_hex(m: *Monitor, args: []const []const u8) ExecError {
    for (args) |arg| {
        const value = parseInt(arg) catch {
            err_prefix(m);
            m.console.puts("invalid number: ");
            m.console.puts(arg);
            m.console.puts("\n");
            return .invalid_argument;
        };
        m.console.print_hex_min(value);
        m.console.puts("\n");
    }
    return .none;
}

pub fn cmd_repeat(m: *Monitor, args: []const []const u8) ExecError {
    const count = parseInt(args[0]) catch {
        err_prefix(m);
        m.console.puts("invalid count: ");
        m.console.puts(args[0]);
        m.console.puts("\n");
        return .invalid_argument;
    };
    if (count < 1 or count > repeat_max_count) {
        err_prefix(m);
        m.console.puts("count must be between 1 and ");
        m.console.print_u64(repeat_max_count);
        m.console.puts("\n");
        return .invalid_argument;
    }
    // Measure the joined text length (saturating) before printing anything.
    const parts = args[1..];
    var text_len: usize = 0;
    for (parts) |part| {
        text_len = @min(repeat_max_bytes, text_len +| part.len);
    }
    if (parts.len > 1) text_len = @min(repeat_max_bytes, text_len + parts.len - 1);
    const per_line = text_len + 1; // + the trailing newline
    if (per_line > repeat_max_bytes or count > @as(u64, repeat_max_bytes / per_line)) {
        err_prefix(m);
        m.console.puts("output too large (max ");
        m.console.print_u64(repeat_max_bytes);
        m.console.puts(" bytes)\n");
        return .invalid_argument;
    }
    var n: u64 = 0;
    while (n < count) : (n += 1) {
        for (parts, 0..) |part, index| {
            if (index > 0) m.console.putc(' ');
            m.console.puts(part);
        }
        m.console.puts("\n");
    }
    return .none;
}

/// `sh` — M18 T16 (issue #419): registered here for discovery (help,
/// tab completion, usage shape). Actual script execution lives in the
/// interactive shell (`shell.zig` intercepts `sh` before this registry
/// is reached, because scripts must execute through the shell's
/// handle_line). Direct `monitor.exec` of `sh` — host tests only — gets
/// an honest refusal rather than a silent no-op.
/// `type` — M19 P1 (issue #290). The real work is a shell builtin
/// (intercepted before monitor.exec); this registry entry exists for
/// help/usage/completion discovery and refuses direct execution the same
/// way `cmd_sh` does.
pub fn cmd_type(m: *Monitor, args: []const []const u8) ExecError {
    _ = args;
    err_prefix(m);
    m.console.print_line("type: echoes the pipe source; use it as the right half of `a | type`");
    return .invalid_argument;
}

pub fn cmd_sh(m: *Monitor, args: []const []const u8) ExecError {
    _ = args;
    // ADR 0008 D3 shape 2: a refusal wears the `error:` prefix.
    err_prefix(m);
    m.console.print_line("sh: script execution runs through the interactive shell");
    return .invalid_argument;
}

// ---------------------------------------------------------------------------
// Personality commands
// ---------------------------------------------------------------------------

/// Fixed mascot art (bounded, deterministic, no state). Runtime-built into
/// module storage (not a const table): a const array of string slices would
/// hold link-time absolute pointers, wrong at the kernel's runtime load
/// base (claim 0015 root cause — the flat loader applies no relocations).
var elephant_storage: [12][]const u8 = undefined;

var elephant_ready = false;

pub fn elephant_lines() []const []const u8 {
    if (!elephant_ready) {
        elephant_storage[0] = "      _    _";
        elephant_storage[1] = "     (o)  (o)";
        elephant_storage[2] = "       \\  /";
        elephant_storage[3] = "       _||_";
        elephant_storage[4] = "      /    \\";
        elephant_storage[5] = "     |  __  |";
        elephant_storage[6] = "     | |  | |";
        elephant_storage[7] = "     |_|  |_|";
        elephant_storage[8] = "    /  |  |  \\";
        elephant_storage[9] = "   /   |  |   \\";
        elephant_storage[10] = "  |    |  |    |";
        elephant_storage[11] = "  |____|  |____|";
        elephant_ready = true;
    }
    return &elephant_storage;
}

/// Fixed Sexiburger mascot art (Milestone 19, issue #677).
/// Sexipus wearing a 6-layer burger with 6 tentacles, under the Covenant of Six.
var sexiburger_diag_storage: [14][]const u8 = undefined;

var sexiburger_diag_ready = false;

pub fn sexiburger_lines() []const []const u8 {
    if (!sexiburger_diag_ready) {
        sexiburger_diag_storage[0] = "           .-------.";
        sexiburger_diag_storage[1] = "          /  *   *  \\    [Crown: System]";
        sexiburger_diag_storage[2] = "        (  .  *   .   ) ";
        sexiburger_diag_storage[3] = "     (~~~\\___________/~~~) [Lettuce: Apps]";
        sexiburger_diag_storage[4] = "      \\   [=========]   /  [Tomato: Active app]";
        sexiburger_diag_storage[5] = "       \\   (=======)   /   [Cheese: Windows & tabs]";
        sexiburger_diag_storage[6] = "        \\  |=======|  /    [Patty: Services]";
        sexiburger_diag_storage[7] = "      __ \\ \\_______/ / __  [Heel: Power]";
        sexiburger_diag_storage[8] = "     (  \\ \\_|_|_|_|_/ /  )";
        sexiburger_diag_storage[9] = "      \\  \\___     ___/  /";
        sexiburger_diag_storage[10] = "       )  (  |   |  )  (";
        sexiburger_diag_storage[11] = "      /  /   |   |   \\  \\";
        sexiburger_diag_storage[12] = "     (_ /    |   |    \\ _)";
        sexiburger_diag_storage[13] = "  [6 tentacles | 6 layers | Covenant invariant intact]";
        sexiburger_diag_ready = true;
    }
    return &sexiburger_diag_storage;
}

pub fn cmd_elephant(m: *Monitor, args: []const []const u8) ExecError {
    _ = args;
    for (elephant_lines()) |line| m.console.print_line(line);
    m.console.print_line("ELEPHANT ONLINE");
    print_plain_field(m, "trunk", "up");
    m.console.puts("\n");
    print_plain_field(m, "ears", "floppy");
    m.console.puts("\n");
    print_plain_field(m, "console", m.state.console_name);
    m.console.puts("\n");
    const err = handoff.validate(&m.state.handoff);
    if (err == .none) {
        print_plain_field(m, "handoff", "valid");
    } else {
        m.console.puts("  handoff: invalid (");
        m.console.puts(handoff.error_name(err));
        m.console.puts(")");
    }
    m.console.puts("\n");
    print_plain_field(m, "memory", "descriptors=");
    m.console.print_hex(@intCast(m.state.map.count));
    m.console.puts("\n");
    return .none;
}

pub fn cmd_sexiburger(m: *Monitor, args: []const []const u8) ExecError {
    _ = args;
    for (sexiburger_lines()) |line| m.console.print_line(line);
    m.console.print_line("SEXIBURGER ONLINE");
    print_plain_field(m, "mascot", "Sexipus (hexapus clade)");
    m.console.puts("\n");
    print_plain_field(m, "tentacles", "6 (3 left, 3 right, lower pair curling inward)");
    m.console.puts("\n");
    print_plain_field(m, "layers", "6 (Crown, Lettuce, Tomato, Cheese, Patty, Heel)");
    m.console.puts("\n");
    print_plain_field(m, "covenant", "the tentacle count is load-bearing");
    m.console.puts("\n");
    print_plain_field(m, "status", "all 6 invariants intact");
    m.console.puts("\n");
    return .none;
}

pub fn cmd_beans(m: *Monitor, args: []const []const u8) ExecError {
    var count: u64 = 42;
    if (args.len == 1) {
        count = parseInt(args[0]) catch {
            err_prefix(m);
            m.console.puts("beans: invalid count: ");
            m.console.puts(args[0]);
            m.console.puts("\n");
            return .invalid_argument;
        };
        if (count < 1 or count > beans_max_count) {
            err_prefix(m);
            m.console.puts("beans: count must be between 1 and ");
            m.console.print_u64(beans_max_count);
            m.console.puts("\n");
            return .invalid_argument;
        }
    }
    m.console.print_line("beans");
    m.console.puts("counting beans... ");
    m.console.print_u64(count);
    m.console.print_line(" beans in a trench coat.");
    m.console.print_line("that's it. that's the command.");
    return .none;
}
