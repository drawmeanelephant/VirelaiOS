//! Virelai Monitor command layer (Milestone 1.5, commands & personality).
//!
//! A reusable, host-testable command registry for the future interactive
//! monitor (`virelai>`). It depends only on the console abstraction
//! (`console.zig`), the handoff-v2 contract (`handoff.zig`), and the
//! captured EFI map view (`memmap.zig`); it never touches hardware, so it
//! runs unchanged under `zig test` against a mock console and, later,
//! inside the kernel once the Console & Shell Core stream wires the real
//! serial transport.
//!
//! Constraints honored: no heap allocation, no libc, no POSIX, no dynamic
//! command registration, no unbounded recursion or output, and no hidden
//! global mutable state. All state lives in the caller-owned `Monitor`
//! value (console, system state, machine control).
//!
//! `kernel/src/main.zig` is intentionally untouched by this stream.

const std = @import("std");
pub const alloc = @import("alloc.zig");
pub const console = @import("console.zig");
pub const esp_exec = @import("exec.zig"); // claim 6783: load a user program from the host share and enter it at EL0
pub const svclock = @import("svclock.zig"); // claim 9498 follow-on: per-service-domain locks — commands take the kernel lock plus their domain(s)
pub const syscall_mod = @import("syscall.zig"); // the strace seam (sets syscall_mod.strace_pid)
pub const exceptions = @import("exceptions.zig");
pub const gic = @import("gic.zig");
pub const handoff = @import("handoff.zig");
pub const mailbox = @import("mailbox.zig"); // claim 5965: per-process IPC rings behind `mbox`
pub const measure = @import("measure.zig"); // #2418 (M113b): the digest hex the v4 handoff view prints
pub const memmap = @import("memmap.zig");
pub const mmu = @import("mmu.zig"); // claim 5804: per-task TTBR0 roots + user-root inventory
pub const pci = @import("pci.zig");
pub const process = @import("process.zig"); // milestone four (claim 3848): the process registry behind `procs`
pub const scheduler = @import("scheduler.zig"); // claim 5275: tick-driven round-robin tasks
pub const syscall = @import("syscall.zig"); // claim 3594: syscall table + counters
pub const timer = @import("timer.zig");
pub const uaccess = @import("uaccess.zig"); // claim 6120: fault-safe copy-in/copy-out
pub const userspace = @import("userspace.zig"); // claim 5804: user-VA layout

pub const virtio_file = @import("virtio_file.zig"); // M34 HF1+HF2 (issues #735/#736): the host file channel behind `vf`
pub const csprng = @import("csprng.zig"); // milestone four (claim 2665): the seeded CSPRNG behind `random`
pub const clipboard = @import("clipboard.zig"); // milestone fourteen (claim 0169): the shared kernel clipboard behind `clip`
pub const terminal = @import("terminal.zig"); // M73f-2 (#1632): the tty seam behind `tty` (ADR 0020 D1 drop counters)
pub const virtio_net = @import("virtio_net.zig"); // milestone five card N1 (claim 1373): the net transport behind `net`/`netsend`
pub const virtio_gpu = @import("virtio_gpu.zig"); // milestone six card G1 (claim 6053): the gpu transport + framebuffer behind `screen`
pub const virtio_snd = @import("virtio_snd.zig"); // milestone fifteen card A1 (claim 6140): the virtio-snd transport behind `sound`
pub const virtio_vsock = @import("virtio_vsock.zig"); // M115a (issue #2460): the virtio-vsock transport behind `vsock`
pub const road_pops = @import("road_pops.zig"); // milestone six card G3 (claim 1574): the Road Pops tee console behind `roadpops`
pub const fbtext = @import("text.zig"); // milestone six card G2 (claim 3194): framebuffer text rendering behind `text`
pub const xhci = @import("xhci.zig"); // milestone seven card I1 (claim 4272): the XHCI host-controller transport behind `usb`
pub const input = @import("input.zig"); // milestone seven card I3 (claim 6050): the keyboard/pointer event FIFO behind `input`
pub const usb_msc = @import("usb_msc.zig"); // M43 card U2 (issue #1033): BOT + minimal SCSI over the U1 bulk engine (`usb msc`)
pub const fat32_ro = @import("fat32_ro.zig"); // M70f F1 (issue #1458): the read-only FAT32 volume reader behind `usb vol|ls|cat`
pub const driving_award = @import("driving_award.zig"); // milestone six card G5 (claim 1543): Driving Award, the window manager behind `dui`
pub const wm_server = @import("wm_server.zig"); // M32 WMS2 (issue #622): the render-server register behind `wm`
pub const settings = @import("settings.zig"); // milestone eight card U8 (claim 2649): persistent settings engine
pub const font_metrics = @import("font_metrics.zig"); // M80i (#1725): the grid's zoom ladder, reported by `font`
pub const tombstone = @import("tombstone.zig"); // Arc5 issue #243: crash tombstone engine
pub const forensics = @import("forensics.zig"); // #1278: the last-words recorder behind `forensics`
pub const dns = @import("dns.zig"); // milestone twelve card N2 (claim 7566): DNS resolver
pub const events = @import("events.zig"); // milestone sixteen C3 (claim 0339): per-process event queue bound behind `resources`
pub const file_table = @import("file_table.zig"); // milestone sixteen C3 (claim 0339): per-process handle bound behind `resources`
pub const serial_ring = @import("serial_ring.zig"); // Arc5 issue #243: serial output ring buffer behind `dmesg`
pub const smp = @import("smp.zig");
pub const trust = @import("trust.zig"); // M50 TS2 (#1136, ADR 0024 D4): the kernel-actor file policy for direct consumers
pub const secret = @import("secret.zig"); // M50 TS5 (#1139, ADR 0024 D8): the SECRETS.TXT store behind the `secrets` command

// The command handlers live in seam-split files under monitor/
// (#3755); the registry below references them qualified.
const meta = @import("monitor/meta.zig");
const fs = @import("monitor/fs.zig");
const vf = @import("monitor/vf.zig");
const misc = @import("monitor/misc.zig");
const gfx = @import("monitor/gfx.zig");
const win = @import("monitor/win.zig");
const proc = @import("monitor/proc.zig");
const load = @import("monitor/load.zig");
const net = @import("monitor/net.zig");
const net_tcp = @import("monitor/net_tcp.zig");
const net6 = @import("monitor/net6.zig");
const usb = @import("monitor/usb.zig");
const sys = @import("monitor/sys.zig");

// Re-exported for the external API (shell.zig, monitor_test.zig):
pub const net_ndp_autonomous = net.net_ndp_autonomous;
pub const net_dhcp_autonomous = net.net_dhcp_autonomous;
pub const elephant_lines = misc.elephant_lines;
pub const sexiburger_lines = misc.sexiburger_lines;

// ---------------------------------------------------------------------------
// Limits (fixed-size, explicit bounds)
// ---------------------------------------------------------------------------

/// Maximum arguments after the command name for any single invocation.
pub const max_args_limit: u8 = 16;
/// `repeat` refuses counts outside 1..repeat_max_count.
pub const repeat_max_count: u64 = 64;
/// `repeat` refuses outputs larger than this many bytes.
pub const repeat_max_bytes: usize = 4096;
/// `beans` refuses counts outside 1..beans_max_count.
pub const beans_max_count: u64 = 100;
/// `random` refuses counts outside 1..random_max_bytes (milestone four,
/// claim 2665).
pub const random_max_bytes: usize = 256;

// ---------------------------------------------------------------------------
// Execution result
// ---------------------------------------------------------------------------

pub const ExecError = enum {
    none,
    unknown_command,
    usage,
    invalid_argument,
    not_implemented,
    machine_failed,
};

// ---------------------------------------------------------------------------
// System state handed to the monitor
// ---------------------------------------------------------------------------

/// Everything the commands may read. Caller-owned, immutable from the
/// command layer's point of view.
pub const SystemState = struct {
    handoff: handoff.HandoffV2,
    map: memmap.MapView,
    /// Human-readable transport name ("mock", later "virtio-console",
    /// "pl011", ...). Reported by `elephant` diagnostics.
    console_name: []const u8,
};

// ---------------------------------------------------------------------------
// Machine control (reboot / shutdown)
// ---------------------------------------------------------------------------

pub const MachineResult = enum { ok, not_implemented, failed };

/// Interface behind which `reboot`/`shutdown` live, so command behavior can
/// be host-tested without rebooting the test process. The kernel's real
/// post-ExitBootServices mechanism (Runtime Services `ResetSystem`) is not
/// proven by any gate, so the default is `MachineControl.disabled()`, which
/// honestly reports `not_implemented`; a later stream that proves a
/// mechanism supplies a real implementation.
pub const MachineControl = struct {
    ctx: *anyopaque,
    vtable: *const VTable,

    pub const VTable = struct {
        reboot: *const fn (ctx: *anyopaque) MachineResult,
        shutdown: *const fn (ctx: *anyopaque) MachineResult,
    };

    pub fn reboot(self: MachineControl) MachineResult {
        return self.vtable.reboot(self.ctx);
    }

    pub fn shutdown(self: MachineControl) MachineResult {
        return self.vtable.shutdown(self.ctx);
    }

    /// Honest default: no proven post-exit machine-control mechanism.
    /// The vtable is built at runtime (not a const table) so its function
    /// pointers resolve PC-relatively at the kernel's runtime load base
    /// (claim 0015 root cause).
    pub fn disabled() MachineControl {
        ensure_disabled_vtable();
        return .{
            .ctx = @ptrCast(@constCast(&NoopDisabled)),
            .vtable = &disabled_vtable,
        };
    }
};

/// Storage + builder for `MachineControl.disabled()`'s vtable (module-level
/// so the pointer outlives the call).
const NoopDisabled = struct {
    fn reboot(_: *anyopaque) MachineResult {
        return .not_implemented;
    }
    fn shutdown(_: *anyopaque) MachineResult {
        return .not_implemented;
    }
};
var disabled_vtable: MachineControl.VTable = undefined;
var disabled_vtable_ready = false;
fn ensure_disabled_vtable() void {
    if (!disabled_vtable_ready) {
        disabled_vtable = .{ .reboot = NoopDisabled.reboot, .shutdown = NoopDisabled.shutdown };
        disabled_vtable_ready = true;
    }
}

/// Host-test double: records calls and returns scripted results.
pub const MockMachineControl = struct {
    reboot_result: MachineResult = .ok,
    shutdown_result: MachineResult = .ok,
    reboot_calls: usize = 0,
    shutdown_calls: usize = 0,

    /// Runtime-built vtable (claim 0015 root cause): a const table would
    /// hold link-time absolute addresses, wrong at the runtime load base.
    /// Module-level storage so the returned pointer outlives the call.
    pub fn control(self: *MockMachineControl) MachineControl {
        ensure_mock_vtable();
        return .{ .ctx = self, .vtable = &mock_vtable };
    }

    fn rebootFn(ctx: *anyopaque) MachineResult {
        const self: *MockMachineControl = @ptrCast(@alignCast(ctx));
        self.reboot_calls += 1;
        return self.reboot_result;
    }

    fn shutdownFn(ctx: *anyopaque) MachineResult {
        const self: *MockMachineControl = @ptrCast(@alignCast(ctx));
        self.shutdown_calls += 1;
        return self.shutdown_result;
    }
};

/// Storage + builder for `MockMachineControl.control()`'s vtable.
var mock_vtable: MachineControl.VTable = undefined;
var mock_vtable_ready = false;
fn ensure_mock_vtable() void {
    if (!mock_vtable_ready) {
        mock_vtable = .{ .reboot = MockMachineControl.rebootFn, .shutdown = MockMachineControl.shutdownFn };
        mock_vtable_ready = true;
    }
}

// ---------------------------------------------------------------------------
// Monitor context
// ---------------------------------------------------------------------------

pub const Monitor = struct {
    console: console.Console,
    state: SystemState,
    machine: MachineControl,

    pub fn init(con: console.Console, state: SystemState, machine: MachineControl) Monitor {
        // M173b (#3041, ADR 0052 amendment): this is the first in-module
        // boot hook that runs after OWNERS.TXT loads — mint the kernel's
        // uid_agent ownership rows for AGENT/ and AGENT/AUDIT/ here so the
        // agent's roots exist before any EL0 task runs.
        _ = trust.ensure_agent_roots();
        return .{ .console = con, .state = state, .machine = machine };
    }
};

// ---------------------------------------------------------------------------
// Command registry
// ---------------------------------------------------------------------------

/// Command category (ADR 0008 D1): the seven groups `help` lists.
pub const Category = enum {
    machine_identity,
    memory_state,
    tasks_processes,
    storage,
    networking,
    graphics_input,
    system,
};

/// The fixed group order `help` lists (ADR 0008 D1). Enum values only — no
/// string-literal pointers in a const table, because the kernel's flat loader
/// applies no relocations (the claim-0015 lesson): `category_name` returns the
/// display strings PC-relatively at the call site instead.
pub const category_order = [_]Category{
    .machine_identity,
    .memory_state,
    .tasks_processes,
    .storage,
    .networking,
    .graphics_input,
    .system,
};

pub fn category_name(cat: Category) []const u8 {
    return switch (cat) {
        .machine_identity => "machine / identity",
        .memory_state => "memory / machine state",
        .tasks_processes => "tasks / processes",
        .storage => "storage",
        .networking => "networking",
        .graphics_input => "graphics / input",
        .system => "system",
    };
}

pub const Command = struct {
    name: []const u8,
    help: []const u8,
    usage: []const u8,
    category: Category,
    min_args: u8 = 0,
    max_args: u8 = max_args_limit,
    /// Service-domain locks the command's handler touches, as an svclock
    /// bitmask (claim 9498 follow-on): exec() holds the kernel lock (the
    /// command's registry/lifecycle reads) plus these domains, in
    /// canonical order (kernel last).
    dom: u5 = 0,
    handler: *const fn (m: *Monitor, args: []const []const u8) ExecError,
};

/// Number of commands. The registry is built at runtime (not a const
/// table): see `ensure_registry`.
/// Number of commands. The registry is built at runtime (not a const
/// table): see `ensure_registry`. Milestone five card N1 (claim 1373)
/// grows it 32 -> 34 (`net` + `netsend`). Milestone seven card I1
/// (claim 4272) grows it 37 -> 38 (`usb`). Milestone seven card I3
/// (claim 6050) grows it 38 -> 39 (`input`). Milestone six card G5
/// (claim 1543) grows it 39 -> 40 (`dui`). Milestone eight card U6
/// (claim 8323) grows it 40 -> 42 (`welcome`, `tour`). Milestone eight card U7
/// (claim 2990) grows it 42 -> 43 (`sysinfo`). Milestone eight card U8
/// (claim 2649) grows it 43 -> 44 (`settings`). Milestone fourteen card S1
/// (claim 0169) grows it 44 -> 45 (`clip`). Milestone fifteen card A1
/// (claim 6140) grows it 45 -> 46 (`sound`). Milestone fifteen card A2
/// (claim 5877) grows it 46 -> 47 (`beep`). Milestone sixteen card C3
/// (claim 0339) grows it 47 -> 48 (`resources`). Milestone eighteen T5
/// (claim 0163) grows it 50 -> 51 (`color`). Milestone eighteen T16
/// (issue #419) grows it 51 -> 52 (`sh`). Milestone twenty-four K5
/// grows it 52 -> 53 (`calc`). Milestone twenty U1 (issue #307, claim
/// 5127) grows it 53 -> 54 (`font`). Milestone twenty-two D3 (issue #326)
/// grows it 54 -> 55 (`sym`). Milestone twenty-two D5 (issue #328)
/// grows it 55 -> 56 (`strace`). Milestone twenty-two D6 (issue #329)
/// grows it 56 -> 57 (`ps`). M115a (issue #2460) grows it 78 -> 79
/// (`vsock`).
pub const registry_count: usize = 79; // 78 + `vsock` (M115a #2460)

/// `sym <file>` reads at most this many bytes for on-disk symtab inspection
/// (M22 D3). ELF symbol tables live near the file tail; 64 KiB covers every
/// program the D1 loader can run that still fits its own staging contract.
pub const sym_file_max: usize = 64 * 1024;

/// Command registry, built at runtime into BSS. A `const` table would hold
/// link-time absolute addresses for BOTH the string slices and the handler
/// function pointers, which are wrong at the kernel's runtime-chosen load
/// base (claim 0015 root cause: the first vtable dispatch crashed; the
/// flat loader applies no relocations, unlike macOS for host tests). Built
/// once here in RAM so every pointer resolves PC-relatively (ADRP) and is
/// correct at any load base. `help` derives its listing from this table,
/// so the two cannot drift.
var registry_storage: [registry_count]Command = undefined;
var registry_ready = false;

pub fn ensure_registry() []const Command {
    if (!registry_ready) {
        registry_storage = .{
            .{ .name = "addrspaces", .help = "per-task user address spaces: per-task TTBR0, EL1-only kernel overlay, user-root contents", .usage = "addrspaces", .category = .memory_state, .handler = proc.cmd_addrspaces },
            .{ .name = "beep", .help = "synthesize + play a sine through the virtio-snd PCM path ('beep <freq> <ms>' — reports the full control flow + submit/drain accounting)", .usage = "beep <freq> <ms>", .category = .system, .min_args = 2, .max_args = 2, .handler = gfx.cmd_beep },
            .{ .name = "about", .help = "explain this questionable system", .usage = "about", .category = .machine_identity, .handler = meta.cmd_about },
            .{ .name = "beans", .help = "count beans, probably", .usage = "beans [count]", .category = .machine_identity, .max_args = 1, .handler = misc.cmd_beans },
            .{ .name = "calc", .dom = svclock.dom_bit(.file), .help = "calculator utilities: 'calc history' shows saved calculation history from /data/calc_hst.txt", .usage = "calc [history]", .category = .system, .max_args = 1, .handler = misc.cmd_calc },
            .{ .name = "cat", .dom = svclock.dom_bit(.file), .help = "print a file from the host share (by name or path)", .usage = "cat <file|path>", .category = .storage, .min_args = 1, .max_args = 1, .handler = fs.cmd_cat },
            .{ .name = "clear", .help = "clean up the crime scene", .usage = "clear", .category = .system, .handler = misc.cmd_clear },
            .{ .name = "font", .dom = svclock.dom_bit(.win), .help = "terminal font size: small 8x8 (default), medium 16x16, large 24x24 (M20-U1); the same rung zooms the bound terminal grid 7x13/8x16/10x21 (M80i)", .usage = "font [small|medium|large]", .category = .graphics_input, .min_args = 0, .max_args = 1, .handler = gfx.cmd_font },
            .{ .name = "compose", .help = "list available Alt+key compose sequences for accented characters", .usage = "compose", .category = .system, .handler = misc.cmd_compose },
            .{ .name = "crash", .dom = svclock.dom_bit(.file), .help = "list recent crash tombstones from /data/crash/", .usage = "crash", .category = .system, .handler = sys.cmd_crash },
            .{ .name = "clip", .help = "copy/paste the shared kernel clipboard ('clip <text...>' sets it, 'clip' prints it)", .usage = "clip [<text...>]", .category = .system, .handler = misc.cmd_clip },
            .{ .name = "color", .help = "toggle ANSI terminal colors ('color on'/'color off'; 'color' shows current)", .usage = "color [on|off]", .category = .system, .max_args = 1, .handler = misc.cmd_color },
            .{ .name = "echo", .help = "repeat your regrettable decisions", .usage = "echo <text...>", .category = .system, .handler = misc.cmd_echo },
            .{ .name = "elephant", .help = "operational mascot diagnostics", .usage = "elephant", .category = .machine_identity, .handler = misc.cmd_elephant },
            .{ .name = "sexiburger", .help = "operational mascot diagnostics (the Sexipus burger)", .usage = "sexiburger", .category = .machine_identity, .handler = misc.cmd_sexiburger },
            .{ .name = "exec", .dom = svclock.dom_bit(.file), .help = "load a user program from the host share and enter it at EL0", .usage = "exec [-c<core>] [-u<uid>] [<file> [arg...]]", .category = .tasks_processes, .max_args = 2 + esp_exec.max_exec_args, .handler = load.cmd_exec },
            .{ .name = "fuzz", .dom = svclock.dom_bit(.file), .help = "seeded EL0 syscall sweep plus live HF-wire corpus (M70a #1466)", .usage = "fuzz <seed|roster>", .category = .tasks_processes, .min_args = 1, .max_args = 1, .handler = load.cmd_fuzz },
            .{ .name = "fault", .help = "trigger a synchronous exception (diagnostic)", .usage = "fault", .category = .memory_state, .handler = sys.cmd_fault },
            .{ .name = "handoff", .help = "display boot-to-kernel ABI data", .usage = "handoff", .category = .memory_state, .handler = sys.cmd_handoff },
            .{ .name = "help", .help = "grouped command catalog and per-command/per-topic help", .usage = "help [<command>|<topic>]", .category = .system, .max_args = 1, .handler = meta.cmd_help },
            .{ .name = "hex", .help = "format an integer in hexadecimal", .usage = "hex <number>...", .category = .memory_state, .min_args = 1, .handler = misc.cmd_hex },
            .{ .name = "input", .help = "keyboard/pointer event FIFO: armed state, occupancy, drop count, last keyboard + pointer events", .usage = "input", .category = .graphics_input, .handler = gfx.cmd_input },
            .{ .name = "kill", .help = "terminate a running process (kernel-owned lifetime)", .usage = "kill <pid|name>", .category = .tasks_processes, .min_args = 1, .max_args = 1, .handler = proc.cmd_kill },
            .{ .name = "ls", .dom = svclock.dom_bit(.file), .help = "list files on the host share (or a directory by path); '-l' for long format (D15)", .usage = "ls [-l] [<dir>]", .category = .storage, .max_args = 2, .handler = fs.cmd_ls },
            .{ .name = "mem", .help = "summarize the EFI memory map", .usage = "mem", .category = .memory_state, .handler = sys.cmd_mem },
            .{ .name = "mbox", .dom = svclock.dom_bit(.ev), .help = "per-process IPC mailbox: pending messages and drain counters", .usage = "mbox [<pid>]", .category = .tasks_processes, .max_args = 1, .handler = proc.cmd_mbox },
            .{ .name = "mount", .dom = svclock.dom_bit(.file), .help = "report the host-share file store (HF6: the FAT volumes are gone)", .usage = "mount", .category = .storage, .max_args = 1, .handler = fs.cmd_mount },
            .{ .name = "net", .dom = svclock.dom_bit(.net), .help = "virtio-net transport + RX + ARP + ICMP + UDP + DHCP + TCP + DNS + IPv6/NDP: device DID, MAC, queues, feature bits, RX counters ('net recv' prints received frames; 'net ip <a.b.c.d>' sets the static IP; 'net arp [<a.b.c.d>]' shows/resolves the ARP table; 'net ping <a.b.c.d>' sends an ICMP echo request; 'net udp [listen <port>|close <port>|send <addr> <port> <len>|recv [<port>]]' drives UDP; 'net dhcp' runs the bounded DHCP client one step per invocation; 'net tcp [connect <addr> <port>|send <len>|recv|close|reset]' drives the bounded TCP client; 'net dns <hostname> [<server>]' resolves DNS A-records; 'net ip6' shows the SLAAC address set; 'net ndp [<v6-addr>]' shows/resolves the NDP table; 'net ping6 <v6-addr>' sends an ICMPv6 echo request)", .usage = "net [recv|ip <addr>|arp [<addr>]|ping <addr>|udp [listen <port>|close <port>|send <addr> <port> <len>|recv [<port>]]|dhcp|tcp [connect <addr> <port>|send <len>|recv|close|reset]|dns <host> [<server>]|ip6|ndp [<v6-addr>]|ping6 <v6-addr>]", .category = .networking, .max_args = 5, .handler = net.cmd_net },
            .{ .name = "netsend", .dom = svclock.dom_bit(.net), .help = "send a known Ethernet frame (bounded staging, TX + used-ring drain)", .usage = "netsend <bytes>", .category = .networking, .min_args = 1, .max_args = 1, .handler = net.cmd_netsend },
            .{ .name = "pages", .help = "physical page allocator pool", .usage = "pages [selftest]", .category = .memory_state, .max_args = 1, .handler = sys.cmd_pages },
            .{ .name = "pci", .help = "enumerate PCI devices on the bus", .usage = "pci", .category = .memory_state, .handler = sys.cmd_pci },
            .{ .name = "vsock", .help = "virtio-vsock transport (M115a/b): 'vsock' reports the arm state, 'vsock <port>' connects to the host listener and reports connected/refused/timeout, 'vsock ctl <verb>' runs one control-channel session", .usage = "vsock [<port>] | vsock ctl [badauth|replay|dup] <verb> [name]", .category = .networking, .max_args = 4, .handler = net.cmd_vsock },
            .{ .name = "procs", .help = "process registry: image, address space, lifecycle, exit status", .usage = "procs", .category = .tasks_processes, .max_args = 2, .handler = proc.cmd_procs },
            .{ .name = "random", .help = "print n random bytes from the seeded CSPRNG (hex)", .usage = "random [n]", .category = .system, .max_args = 1, .handler = sys.cmd_random },
            .{ .name = "resources", .help = "fixed-pool audit: scheduler tasks, process registry, windows, page-table carve-out, and per-process ring bounds", .usage = "resources", .category = .memory_state, .handler = proc.cmd_resources },
            .{ .name = "reboot", .help = "restart the machine", .usage = "reboot", .category = .system, .handler = sys.cmd_reboot },
            .{ .name = "repeat", .help = "repeat text, safely bounded", .usage = "repeat <count> <text...>", .category = .system, .min_args = 1, .handler = misc.cmd_repeat },
            .{ .name = "sh", .help = "run a script file of shell commands ('sh <script>' executes it line by line; 64 lines max, 256 chars per line; '#' comments; 'exit' stops early)", .usage = "sh <script>", .category = .system, .min_args = 1, .max_args = 1, .handler = misc.cmd_sh },
            .{ .name = "roadpops", .help = "Road Pops framebuffer console: armed/dirty/present counters (the boot terminal on the screen)", .usage = "roadpops", .category = .graphics_input, .handler = gfx.cmd_roadpops },
            .{ .name = "screen", .help = "virtio-gpu transport + framebuffer: device DID, features, scanout, status, re-arm ('screen fill <rrggbb>' fills the framebuffer and flushes it to the scanout)", .usage = "screen [fill <rrggbb>]", .category = .graphics_input, .max_args = 2, .handler = gfx.cmd_screen },
            .{ .name = "screenshot", .dom = svclock.dom_bit(.file), .help = "capture the current framebuffer (1280x720) and save as BMP to disk (G27)", .usage = "screenshot [<file>]", .category = .graphics_input, .max_args = 1, .handler = gfx.cmd_screenshot },
            .{ .name = "secrets", .dom = svclock.dom_bit(.file), .help = "list the secret store's key NAMES only (never values; ADR 0024 D8)", .usage = "secrets", .category = .system, .handler = sys.cmd_secrets },
            .{ .name = "settings", .dom = svclock.dom_bit(.file) | svclock.dom_bit(.win), .help = "persistent configuration: `settings [list]`, `settings get <key>`, `settings set <key> <val>`, `settings reset`", .usage = "settings [list|get <key>|set <key> <val>|reset]", .category = .system, .max_args = 3, .handler = sys.cmd_settings },
            .{ .name = "shortcuts", .help = "keyboard shortcut reference card (G29)", .usage = "shortcuts", .category = .system, .handler = gfx.cmd_shortcuts },
            .{ .name = "sound", .help = "virtio-snd transport: device DID, class, status, control-queue state, device-config counts (jacks/streams/channel-maps), re-arm; stream-state control: 'sound volume <0-100>' and 'sound mute <on|off>'", .usage = "sound [volume <0-100> | mute <on|off>]", .category = .system, .min_args = 0, .max_args = 2, .handler = gfx.cmd_sound },
            .{ .name = "text", .dom = svclock.dom_bit(.win), .help = "framebuffer text: text region, cursor, scrollback ('text put <string...>' renders + flushes to the scanout; 'text clear' clears; 'text putraw' skips the trailing newline; 'text fontdebug [on|off]' missing-glyph stats)", .usage = "text [put <string...>|putraw <string...>|clear|fontdebug [on|off]]", .category = .graphics_input, .min_args = 0, .max_args = 9, .handler = gfx.cmd_text },
            .{ .name = "shutdown", .help = "request power-off", .usage = "shutdown", .category = .system, .handler = sys.cmd_shutdown },
            .{ .name = "ps", .help = "process status table: PID, name, state, memory footprint, CPU ticks, and executor task per live/exited process (M22 D6)", .usage = "ps", .category = .tasks_processes, .handler = proc.cmd_ps },
            .{ .name = "spawn", .help = "spawn the lifecycle demo task", .usage = "spawn", .category = .tasks_processes, .handler = load.cmd_spawn },
            .{ .name = "sysinfo", .help = "comprehensive system and subsystem diagnostic snapshot", .usage = "sysinfo", .category = .machine_identity, .handler = meta.cmd_sysinfo },
            .{ .name = "strace", .dom = svclock.dom_bit(.file), .help = "trace a program's syscalls: 'strace exec APP.BIN [args]' arms the tracer around an exec and prints one line per syscall; 'strace off' disarms", .usage = "strace exec <file> [args...] | off", .category = .tasks_processes, .handler = load.cmd_strace },
            .{ .name = "sym", .dom = svclock.dom_bit(.file), .help = "crash-report symbol table: 'sym' lists symbols loaded from the last ELF exec; 'sym <file>' parses an ELF's symtab from disk", .usage = "sym [<file>]", .category = .tasks_processes, .max_args = 1, .handler = load.cmd_sym },
            .{ .name = "syscalls", .help = "numbered syscall table and counters", .usage = "syscalls", .category = .tasks_processes, .handler = proc.cmd_syscalls },
            .{ .name = "wm", .dom = svclock.dom_bit(.win) | svclock.dom_bit(.ev), .help = "M32 WMS2/WMS4/WMS5 render-server register: the registered WM server pid, present-sequence counter, presents, COMPOSITE_TICK count, SET_WINDOW chrome submissions + SET_STATE visibility/workspace calls, and the WMS5 input-seam fan-out counters (ptr_fan = raw pointer samples, win_mirror = registry mirrors, key_fan = raw keyboard samples; 'wm none' means the shell idle shim is compositing)", .usage = "wm", .category = .graphics_input, .handler = win.cmd_wm },
            .{ .name = "wnd", .dom = svclock.dom_bit(.file) | svclock.dom_bit(.ev), .help = "M32 WMS3 WM server: 'wnd' reports the registered WM server (pid, present seq/count, tick count; 'wnd: none' = shell-shim compositing); 'wnd start' launches the long-lived EL0 WND.BIN server (infrastructure — not in APPS.TXT; the default VM stays shim-only)", .usage = "wnd [start]", .category = .graphics_input, .max_args = 1, .handler = win.cmd_wnd },
            .{ .name = "tabwm", .dom = svclock.dom_bit(.file) | svclock.dom_bit(.ev), .help = "M39 TWM1 WM server: 'tabwm' reports the registered WM server; 'tabwm start' launches the long-lived EL0 TABWM.BIN server (left-sidebar browser-style window manager)", .usage = "tabwm [start]", .category = .graphics_input, .max_args = 1, .handler = win.cmd_tabwm },
            .{ .name = "tasks", .help = "tick-driven task scheduler status", .usage = "tasks", .category = .tasks_processes, .handler = proc.cmd_tasks },
            .{ .name = "smp", .help = "multiprocessor topology, online CPU cores, and per-core task state", .usage = "smp", .category = .tasks_processes, .handler = proc.cmd_smp },
            .{ .name = "type", .help = "echo stdin (the pipe source) to stdout — the right half of `a | type`", .usage = "type", .category = .system, .handler = misc.cmd_type },
            .{ .name = "tty", .help = "terminal drop counters (ADR 0020 D1): out_dropped/in_dropped per bound tty ('tty' prints one line per terminal)", .usage = "tty", .category = .system, .handler = sys.cmd_tty },
            .{ .name = "timer", .help = "interrupt controller + timer status", .usage = "timer [probe <ms>]", .category = .memory_state, .handler = sys.cmd_timer },
            .{ .name = "forensics", .help = "last-words recorder: on|off|dump|reset (off by default)", .usage = "forensics [on|off|dump|reset]", .category = .system, .max_args = 1, .handler = sys.cmd_forensics },
            .{ .name = "tour", .help = "guided tour of the system for new users", .usage = "tour", .category = .machine_identity, .handler = meta.cmd_welcome },
            .{ .name = "uaccess", .help = "user-memory copy diagnostics (valid, fault, recovery)", .usage = "uaccess", .category = .memory_state, .handler = proc.cmd_uaccess },
            .{ .name = "usb", .help = "XHCI host controller: `usb` transport report, `usb devices` enumerated devices, `usb report` last HID report, `usb bulk [probe ...]` bulk engine (U1), `usb msc [probe] [lba]` mass-storage BOT/SCSI probe (U2), `usb vol` MBR partition + FAT32 volume enumeration, `usb ls <vol>[/<dir>]` volume directory listing, `usb cat <vol>/<path> [<max>]` read-only file read (full byte count + FNV-1a 32 of every byte) (M70f F1), `usb rescan` polled lifecycle rescan (U4), `usb detach [slot]` administrative detach (U4)", .usage = "usb [devices|report|bulk [probe ...]|msc [probe] [lba]|vol|ls <vol>[/<dir>]|cat <vol>/<path> [<max>]|rescan|detach [slot]]", .category = .graphics_input, .handler = usb.cmd_usb },
            .{ .name = "uname", .help = "compact system identity", .usage = "uname", .category = .machine_identity, .handler = meta.cmd_uname },
            .{ .name = "version", .help = "display build information", .usage = "version", .category = .machine_identity, .handler = meta.cmd_version },
            .{ .name = "vf", .dom = svclock.dom_bit(.file), .help = "host file channel (M34): 'vf ls/cat/mkdir/rm/mv <path>' read + mutate a macOS share over custom-virtio queue 5; 'vf open/close/write/truncate/fsync <h>' manage write handles (8-slot host cursor table)", .usage = "vf [ls [<path>]|cat <path>|mkdir <path>|rm <path>|mv <from> <to>|open <path> [append]|close <h>|write <h> <n>|truncate <h> <n>|fsync <h>]", .category = .storage, .max_args = 4, .handler = vf.cmd_vf },
            .{ .name = "welcome", .help = "guided tour of the system for new users", .usage = "welcome", .category = .machine_identity, .handler = meta.cmd_welcome },
            .{ .name = "dui", .dom = svclock.dom_bit(.win), .help = "Driving Award window manager: registry (with owner pids), z-order, focus, hit-testing ('dui focus <n>' focuses; 'dui raise <n>' raises; 'dui lower <n>' lowers to back; 'dui move <n> <x> <y>' moves a user window; 'dui close <n>' releases a user window; 'dui list <pid>' filters by owner; 'dui hit <x> <y>' hit-tests; 'dui cycle' cycles focus like Alt+Tab; 'dui tile <n>' toggles a user window floating/tiled (M21 W1); 'dui master' swaps master/detail (M21 W2))", .usage = "dui [focus <n>|raise <n>|lower <n>|move <n> <x> <y>|close <n>|list <pid>|hit <x> <y>|cycle|tile <n>|master]", .category = .graphics_input, .max_args = 4, .handler = win.cmd_dui },
            .{ .name = "write", .dom = svclock.dom_bit(.file), .help = "write text to a file on the host share", .usage = "write <file> <text...>", .category = .storage, .min_args = 1, .handler = fs.cmd_write },
            .{ .name = "mktemp", .dom = svclock.dom_bit(.file), .help = "create a temporary file (empty, unique name)", .usage = "mktemp [prefix]", .category = .storage, .max_args = 1, .handler = fs.cmd_mktemp },
            .{ .name = "stat", .dom = svclock.dom_bit(.file), .help = "file metadata: size, type, cluster, path (D8)", .usage = "stat <file|path>", .category = .storage, .min_args = 1, .max_args = 1, .handler = fs.cmd_stat },
            .{ .name = "du", .dom = svclock.dom_bit(.file), .help = "recursive directory disk usage (M25 F4)", .usage = "du [<path>]", .category = .storage, .max_args = 1, .handler = fs.cmd_du },
            .{ .name = "find", .dom = svclock.dom_bit(.file), .help = "recursive file search with glob patterns ('find / -name \"*.BIN\"' — bounded 3 levels, 256 results)", .usage = "find <dir> -name <pattern>", .category = .storage, .min_args = 3, .max_args = 3, .handler = fs.cmd_find },
            .{ .name = "dmesg", .help = "system log viewer: last bytes of serial output (D12)", .usage = "dmesg", .category = .system, .handler = sys.cmd_dmesg },
            .{ .name = "time", .help = "command timing: measure elapsed ticks and wall-clock time (D13)", .usage = "time <command> [args...]", .category = .system, .min_args = 1, .handler = sys.cmd_time },
            .{ .name = "which", .dom = svclock.dom_bit(.file), .help = "locate a command: shell builtin, monitor command, or host-share application (D16; HF6: the ESP is gone)", .usage = "which <name>", .category = .system, .min_args = 1, .max_args = 1, .handler = misc.cmd_which },
            .{ .name = "inventory", .dom = svclock.dom_bit(.file), .help = "list all installed applications from APPS.TXT with sizes and types (D16)", .usage = "inventory", .category = .storage, .handler = fs.cmd_inventory },
        };
        registry_ready = true;
    }
    return &registry_storage;
}

pub fn lookup(name: []const u8) ?*const Command {
    for (ensure_registry()) |*cmd| {
        if (std.mem.eql(u8, cmd.name, name)) return cmd;
    }
    return null;
}

/// Execute an already-tokenized command line: `argv[0]` is the command
/// name, the rest are its arguments. Tokenization and line editing belong
/// to the later Console & Shell Core stream.
pub fn exec(m: *Monitor, argv: []const []const u8) ExecError {
    if (argv.len == 0) {
        // ADR 0008 D3 shape 2: a dispatch-level refusal wears the `error:`
        // prefix like any other. An empty line is not an unknown VERB (there
        // is no verb to quote), so shape 3 would be a lie.
        err_line(m, "no command given; type 'help' for a list of commands");
        return .usage;
    }
    if (argv.len > max_args_limit + 1) {
        err_line(m, too_many_arguments_message);
        return .usage;
    }
    const cmd = lookup(argv[0]) orelse {
        // ADR 0008 D3 shape 3: unknown verb -> `unknown command '<x>' --
        // try 'help'` (the two hyphens are the byte-safe rendering of the
        // ADR's typographic em dash — the framebuffer text layer renders
        // only 0x20..0x7e, so a multi-byte dash would render as blanks).
        m.console.puts("unknown command '");
        m.console.puts(argv[0]);
        m.console.print_line("' -- try 'help'");
        return .unknown_command;
    };
    const args = argv[1..];
    if (args.len < cmd.min_args or args.len > cmd.max_args) {
        print_usage(m, cmd);
        return .usage;
    }
    // Service-domain locks (claim 9498 follow-on): every EL1h command
    // holds the kernel lock (registry/lifecycle state) plus the domain
    // lock(s) of the subsystems it touches — in canonical order, kernel
    // last — so a same-domain core-1 syscall serializes with the command
    // while unrelated-domain syscalls proceed. The holds are IRQ-masked
    // (a command can never be preempted mid-hold). Nested commands
    // (`time`/`sh` re-enter exec) skip re-acquisition via `held_set`.
    const doms = svclock.dom_bit(.kernel) | cmd.dom;
    const taken = if (!svclock.held_set(doms)) svclock.acquire_missing(doms) else 0;
    defer if (taken != 0) svclock.release_set(taken);
    return cmd.handler(m, args);
}

// ---------------------------------------------------------------------------
// Tab completion (ADR 0008 D2)
// ---------------------------------------------------------------------------

/// Given the line being edited and the cursor, return the suffix to insert
/// (a long-lived slice of a string literal), or null when there is no unique
/// completion. The first token completes against command names; a later
/// token completes against the leading command's fixed sub-verb vocabulary.
pub fn complete(line: []const u8, cursor: usize) ?[]const u8 {
    if (cursor > line.len) return null;
    var start = cursor;
    while (start > 0 and line[start - 1] != ' ' and line[start - 1] != '\t') start -= 1;
    const prefix = line[start..cursor];
    if (prefix.len == 0) return null;
    // The first token (the command name) has only whitespace before it.
    var first = true;
    var i: usize = 0;
    while (i < start) : (i += 1) {
        if (line[i] != ' ' and line[i] != '\t') {
            first = false;
            break;
        }
    }
    if (first) return complete_command(prefix);
    // Sub-verb completion: find the leading command (the first token).
    var cmd_start: usize = 0;
    while (cmd_start < line.len and (line[cmd_start] == ' ' or line[cmd_start] == '\t')) cmd_start += 1;
    var cmd_end = cmd_start;
    while (cmd_end < line.len and line[cmd_end] != ' ' and line[cmd_end] != '\t') cmd_end += 1;
    if (cmd_end <= cmd_start) return null;
    return sub_verb_complete(line[cmd_start..cmd_end], prefix);
}

fn complete_command(prefix: []const u8) ?[]const u8 {
    var found: ?[]const u8 = null;
    for (ensure_registry()) |*cmd| {
        if (std.mem.startsWith(u8, cmd.name, prefix)) {
            if (found != null) return null; // ambiguous
            found = cmd.name;
        }
    }
    return if (found) |name| name[prefix.len..] else null;
}

fn match_one(prefix: []const u8, verbs: []const []const u8) ?[]const u8 {
    var found: ?[]const u8 = null;
    for (verbs) |v| {
        if (std.mem.startsWith(u8, v, prefix)) {
            if (found != null) return null; // ambiguous
            found = v;
        }
    }
    return if (found) |v| v[prefix.len..] else null;
}

fn sub_verb_complete(cmd: []const u8, prefix: []const u8) ?[]const u8 {
    if (std.mem.eql(u8, cmd, "net"))
        return match_one(prefix, &.{ "recv", "ip", "arp", "ping", "udp", "udp6", "dhcp", "tcp", "dns", "ip6", "ndp", "ping6", "status", "route", "log" });
    if (std.mem.eql(u8, cmd, "dui"))
        return match_one(prefix, &.{ "focus", "raise", "move", "close", "list", "hit", "cycle" });
    if (std.mem.eql(u8, cmd, "usb"))
        return match_one(prefix, &.{ "devices", "report" });
    if (std.mem.eql(u8, cmd, "screen"))
        return match_one(prefix, &.{"fill"});
    if (std.mem.eql(u8, cmd, "text"))
        return match_one(prefix, &.{ "put", "clear" });
    if (std.mem.eql(u8, cmd, "mount"))
        return match_one(prefix, &.{ "esp", "data" });
    if (std.mem.eql(u8, cmd, "help"))
        return match_one(prefix, &.{ "networking", "windows", "storage", "graphics" });
    return null;
}

// ---------------------------------------------------------------------------
// Shared helpers
// ---------------------------------------------------------------------------

/// The one dispatch-level too-many-tokens message, shared by the shell's
/// tokenizer refusal and `exec`'s own guard so the two can never drift.
pub const too_many_arguments_message = "too many arguments (max 17 tokens)";

/// ADR 0008 D3 shape 1: misuse -> `usage: <cmd> <args>` PLUS a one-line
/// hint. The hint is the registry's own blurb for the command, so there is
/// exactly one source for it and no per-handler copy can drift. Sub-verb
/// misuse reuses the command's full usage, so a fourth shape can never slip
/// in on a bad invocation.
pub fn print_usage(m: *Monitor, cmd: *const Command) void {
    m.console.puts("usage: ");
    m.console.puts(cmd.usage);
    m.console.puts("\n");
    m.console.puts(cmd.help);
    m.console.puts("\n");
}

/// ADR 0008 D3 shape 2: failure -> `error: <actionable message>`. The
/// prefix only; the caller appends the actionable text.
pub fn err_prefix(m: *Monitor) void {
    m.console.puts("error: ");
}

/// ADR 0008 D3 shape 2 for a fixed message: `error: <text>` on one line.
pub fn err_line(m: *Monitor, text: []const u8) void {
    err_prefix(m);
    m.console.print_line(text);
}

/// Parse a u64 from an optional "0x"/"0X"-prefixed hexadecimal string or a
/// bare decimal string. Explicit bounds: empty input, invalid digits, and
/// overflow are all errors; nothing wraps silently.
pub fn parseInt(text: []const u8) error{ Empty, InvalidDigit, Overflow }!u64 {
    if (text.len == 0) return error.Empty;
    var index: usize = 0;
    const radix: u64 = if (text.len > 2 and text[0] == '0' and (text[1] == 'x' or text[1] == 'X')) blk: {
        index = 2;
        break :blk 16;
    } else 10;
    if (index >= text.len) return error.Empty;
    var value: u64 = 0;
    while (index < text.len) : (index += 1) {
        const c = text[index];
        const digit: u64 = switch (c) {
            '0'...'9' => c - '0',
            'a'...'f' => c - 'a' + 10,
            'A'...'F' => c - 'A' + 10,
            else => return error.InvalidDigit,
        };
        if (radix == 10 and digit >= 10) return error.InvalidDigit;
        value = std.math.mul(u64, value, radix) catch return error.Overflow;
        value = std.math.add(u64, value, digit) catch return error.Overflow;
    }
    return value;
}

pub fn parseSignedInt(text: []const u8) error{ Empty, InvalidDigit, Overflow }!i64 {
    if (text.len == 0) return error.Empty;
    if (text[0] == '-') {
        const u = try parseInt(text[1..]);
        if (u > @as(u64, @intCast(std.math.maxInt(i64))) + 1) return error.Overflow;
        return -@as(i64, @intCast(u));
    }
    const u = try parseInt(text);
    if (u > @as(u64, @intCast(std.math.maxInt(i64)))) return error.Overflow;
    return @as(i64, @intCast(u));
}

/// Hex-only parse (an optional 0x/0X prefix is accepted; the documented
/// `screen fill <rrggbb>` form is bare hex) — used for the color argument.
pub fn parseHex(text: []const u8) error{ Empty, InvalidDigit, Overflow }!u64 {
    if (text.len == 0) return error.Empty;
    var start: usize = 0;
    if (text.len > 2 and text[0] == '0' and (text[1] == 'x' or text[1] == 'X')) start = 2;
    if (start >= text.len) return error.Empty;
    var value: u64 = 0;
    for (text[start..]) |c| {
        const digit: u64 = switch (c) {
            '0'...'9' => c - '0',
            'a'...'f' => c - 'a' + 10,
            'A'...'F' => c - 'A' + 10,
            else => return error.InvalidDigit,
        };
        value = std.math.mul(u64, value, 16) catch return error.Overflow;
        value = std.math.add(u64, value, digit) catch return error.Overflow;
    }
    return value;
}

/// M50 TS2 (issue #1136, ADR 0024 D4/D8): the monitor's explicit
/// `kernel_actor()` gate. The monitor is uid_system + CAP_FS_ANY, so ordinary
/// paths pass and secret-class paths are denied for EVERY actor. An empty
/// path is the share root (list); an unparseable/over-long path fails closed.
pub fn kernel_vf_allowed(path: []const u8, want: trust.Want) bool {
    if (path.len == 0) return trust.check(trust.kernel_actor(), .host, "", want) == .allow;
    const parsed = file_table.parse_path(path) orelse return false;
    if (parsed.partition == .tty) return true;
    return trust.check(trust.kernel_actor(), parsed.partition, parsed.path[0..parsed.parsed_len()], want) == .allow;
}

/// The honest refusal line for a policy-denied direct-consumer file op.
pub fn trust_denied(m: *Monitor, verb: []const u8, path: []const u8) ExecError {
    err_prefix(m);
    m.console.puts(verb);
    m.console.puts(": ");
    m.console.puts(path);
    m.console.print_line(": permission denied");
    return .invalid_argument;
}

/// M73f-2 (#1632): the `tty` line — a pure fn so host tests pin the shape
/// (`tty[<n>]: out_dropped=<n> in_dropped=<n>`), which the class-B serial
/// assert then matches after real activity. No EL0 ABI (D5): the counters
/// stay kernel-side and the monitor is their surface.
pub fn fmtDropLine(buf: []u8, index: usize, out_dropped: u64, in_dropped: u64) []const u8 {
    return std.fmt.bufPrint(buf, "tty[{d}]: out_dropped={d} in_dropped={d}\n", .{ index, out_dropped, in_dropped }) catch "tty: fmt overflow\n";
}

// ---------------------------------------------------------------------------
// Boot-message personality and banner (step 18)
// ---------------------------------------------------------------------------

pub const BootMessages = struct {
    /// Runtime-built message table into module storage (not a const array
    /// of string slices: those hold link-time absolute pointers, wrong at
    /// the kernel's runtime load base — claim 0015 root cause).
    var storage: [6][]const u8 = undefined;
    var ready = false;
    pub fn messages() []const []const u8 {
        if (!ready) {
            storage[0] = "VirelaiOS: the elephant has left the building.";
            storage[1] = "VirelaiOS: 42 beans, zero dignity.";
            storage[2] = "VirelaiOS: memory is a map, not a territory.";
            storage[3] = "VirelaiOS: no libc was harmed in the making of this kernel.";
            storage[4] = "VirelaiOS: terminal loop, meet the monitor.";
            storage[5] = "VirelaiOS: from scratch, with love and beans.";
            ready = true;
        }
        return &storage;
    }

    /// Deterministic, stateless "rotation": the choice depends only on the
    /// boot's image handle, so it is stable within a boot, varies across
    /// boots, and needs no hidden mutable state.
    pub fn pick(image_handle: u64) []const u8 {
        const msgs = messages();
        return msgs[@as(usize, @intCast(image_handle % msgs.len))];
    }
};

/// Banner text the later shell-core stream prints at boot. Deliberately
/// does not print "VIRELAIOS 0.1" (the repository defines no release
/// number) or a hardcoded "256 MiB detected" (memory must be derived from
/// the captured map — `mem` does exactly that).
/// Arc5 #244: read SHUTDOWN.TXT from the HOST SHARE (HF6: the DATA
/// partition is gone) and print the last shutdown reason on boot. Silent
/// no-op when the file is absent or empty.
fn show_last_shutdown(m: *Monitor) void {
    if (!virtio_file.available()) return;
    var file_buf: [256]u8 = undefined;
    if (!kernel_vf_allowed("SHUTDOWN.TXT", .read)) return;
    const n = virtio_file.read_whole("SHUTDOWN.TXT", &file_buf) orelse return;
    const bytes = file_buf[0..n];
    // Extract the Reason: line if present
    var found_reason = false;
    var pos: usize = 0;
    while (pos < bytes.len) {
        var end = pos;
        while (end < bytes.len and bytes[end] != '\n') : (end += 1) {}
        const line = file_buf[pos..end];
        if (std.mem.startsWith(u8, line, "Reason: ")) {
            m.console.puts("Last shutdown: ");
            m.console.print_line(line[8..]);
            found_reason = true;
            break;
        }
        pos = end + 1;
    }
    if (!found_reason and bytes.len > 0) {
        m.console.print_line("Last shutdown: unknown (marker present)");
    }
}

pub fn banner(m: *Monitor) void {
    m.console.print_line("VirelaiOS - AArch64 firmware-assisted kernel monitor");
    m.console.puts(BootMessages.pick(m.state.handoff.image_handle));
    m.console.puts("\n");
    m.console.print_line("motd: aarch64 el1 kernel live; scheduler, uaccess, fs, net, gfx, xhci armed.");
    // Arc5 #244: display last shutdown reason on boot if SHUTDOWN.TXT exists
    show_last_shutdown(m);
    m.console.print_line("Type 'help' before touching anything expensive.");
}
