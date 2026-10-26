//! Monitor commands: identity + help (`help`/`about`/`welcome`/`tour`/`sysinfo`/`version`/`uname`).
//!
//! Split out of monitor.zig (#3755): declarations only — the
//! shared types, registry, dispatch, completion, and print/parse
//! helpers stay in the root and are aliased below so the moved
//! code is verbatim.

const std = @import("std");
const mon = @import("../monitor.zig");

const Monitor = mon.Monitor;
const ExecError = mon.ExecError;
const category_order = mon.category_order;
const category_name = mon.category_name;
const err_prefix = mon.err_prefix;
const lookup = mon.lookup;
const ensure_registry = mon.ensure_registry;
const alloc = mon.alloc;
const gic = mon.gic;
const handoff = mon.handoff;
const memmap = mon.memmap;
const process = mon.process;
const scheduler = mon.scheduler;
const timer = mon.timer;
const virtio_file = mon.virtio_file;
const virtio_net = mon.virtio_net;
const virtio_gpu = mon.virtio_gpu;
const road_pops = mon.road_pops;
const xhci = mon.xhci;
const input = mon.input;
const driving_award = mon.driving_award;
const osver = @import("../osver.zig");
// ---------------------------------------------------------------------------
// Identity and inspection commands
// ---------------------------------------------------------------------------

/// `help <topic>` pages (ADR 0008 D1, M27 G28 #471). Bodies are returned as string
/// literals from code (PC-relative at any load base — the claim-0015
/// lesson), never stored in a const table of pointers. Topics are
/// non-command keywords; `syscalls` is a command, so its `help <cmd>` detail
/// is the syscall page rather than a shadowed topic.
fn topic_body(name: []const u8) ?[]const u8 {
    if (std.mem.eql(u8, name, "networking") or std.mem.eql(u8, name, "network")) {
        return "networking\n" ++
            "  virtio-net (DID 0x1041), flag-gated by the runner's --net mode: TX + RX,\n" ++
            "  ARP, IPv4/ICMP, UDP, a bounded DHCP client, and an outward-only bounded TCP\n" ++
            "  client with NAT. `net` drives it (recv/ip/arp/ping/udp/dhcp/tcp); `netsend`\n" ++
            "  sends a known frame. EL0 reaches UDP through ADR 0007 slots 9-11.\n" ++
            "  Bounds: static IP, no DNS, TCP is client-only (no listen or loopback).\n";
    }
    if (std.mem.eql(u8, name, "windows")) {
        return "windows\n" ++
            "  Driving Award (G5) owns the window registry: z-order, focus, hit-testing,\n" ++
            "  dirty-rect compositing. Road Pops is window 0; a 1 Hz clock is window 1.\n" ++
            "  `dui` inspects/focuses/raises/moves/closes; EL0 opens windows through ADR 0007\n" ++
            "  slots 12-20 (open/fill/present/close/move/raise/get/query/set_visible). Windows\n" ++
            "  are process-owned and auto-close when their process exits.\n";
    }
    if (std.mem.eql(u8, name, "storage")) {
        // M34 HF6 (issue #740): the FAT volumes are gone — the macOS host
        // share (custom-virtio queue 5, --cvc-file) is the only store.
        return "storage\n" ++
            "  The macOS host share (custom-virtio queue 5, --cvc-file) is the only\n" ++
            "  store. `vf ls/cat/mkdir/rm/mv` + `vf open/close/write/truncate/fsync`\n" ++
            "  read and mutate it; writes persist on the host disk across reboot.\n";
    }
    if (std.mem.eql(u8, name, "files")) {
        return "files\n" ++
            "  The host share filesystem: `ls`, `cat`, `write`, `find`, `stat`,\n" ++
            "  `du`, and `inventory`. Persistent file operations on the --cvc-file share.\n";
    }
    if (std.mem.eql(u8, name, "graphics")) {
        return "graphics\n" ++
            "  virtio-gpu (DID 0x1050) framebuffer: `screen` reports/fills the scanout,\n" ++
            "  `text` renders the 8x8 font, Road Pops is the on-screen terminal, and Driving\n" ++
            "  Award composites windows over it. 1280x720 B8G8R8X8, 2D blits only.\n";
    }
    if (std.mem.eql(u8, name, "editor")) {
        return "editor\n" ++
            "  Text editing in a TABWM tab: GOEDIT.ELF, and NOTE.ELF -- the dock's\n" ++
            "  editor. Both load a share file and save on Ctrl+S; printable keys insert,\n" ++
            "  backspace/delete remove, Return breaks the line, and NOTE.ELF also takes\n" ++
            "  the arrow keys and saves on close when dirty.\n";
    }
    if (std.mem.eql(u8, name, "calc")) {
        return "calc\n" ++
            "  64-bit integer calculator (GOCALC.ELF): digits, + - * /, equals, clear.\n" ++
            "  `calc history` prints saved calculation history from calc_hst.txt on the host share.\n";
    }
    if (std.mem.eql(u8, name, "system")) {
        return "system\n" ++
            "  Machine management and diagnostic commands: `sysinfo`, `uname`, `version`,\n" ++
            "  `settings` (theme, prompt, font, scrollback), `shutdown`, `reboot`,\n" ++
            "  `dmesg`, `time`, and crash tombstone viewing with `crash`.\n";
    }
    if (std.mem.eql(u8, name, "terminal")) {
        // M80k (#1727): the hygiene chords. They are kernel chrome, not
        // app behaviour — intercepted in input.zig before the tty queue,
        // so they work for ANY tty-bound window and never reach the app as
        // bytes. The clear/reset operations are the ED 3 / DECSTR / RIS
        // seams the parser itself uses, not a second implementation.
        return "terminal\n" ++
            "  A tty-bound window (GOTERM.ELF, GOSH) paints a grid with scrollback.\n" ++
            "  Chrome chords — consumed by the kernel, never typed into the app:\n" ++
            "    Ctrl+Shift+C / V      copy the selection / paste the clipboard\n" ++
            "    Ctrl+Shift+F           open the search bar (Esc closes it)\n" ++
            "    Ctrl+Shift+K           clear the scrollback (ED 3), snap to the tail\n" ++
            "    Ctrl+Shift+R           soft reset: styles and modes, grid intact\n" ++
            "    Ctrl+Shift+Alt+R       full reset (RIS): the grid goes too\n" ++
            "  Scrolling the view: PageUp/PageDown, Shift+Up/Down, Shift+Home/End.\n" ++
            "  The cure for a grid wedged by an app that died mid-escape: the two\n" ++
            "  reset chords, or just close the tab.\n";
    }
    if (std.mem.eql(u8, name, "shortcuts")) {
        return "shortcuts\n" ++
            "  Global: Ctrl+Shift+A (About), Ctrl+Shift+/ (Shortcuts), Alt+Tab (Switch window),\n" ++
            "  Alt+F4 (Close window), F11 (Fullscreen/Tile), Ctrl+Alt+Del (Reboot).\n" ++
            "  Navigation: Tab / Shift+Tab (Focus control), Enter/Space (Activate), Esc (Dismiss).\n" ++
            "  Editing: Ctrl+C (Copy), Ctrl+X (Cut), Ctrl+V (Paste), Ctrl+Z (Undo), Ctrl+S (Save).\n" ++
            "  Run `shortcuts` command for full list.\n";
    }
    if (std.mem.eql(u8, name, "dev") or std.mem.eql(u8, name, "diag")) {
        return "dev / diag\n" ++
            "  Developer tools and diagnostics: `strace` (syscall tracer), `sym` (symbol table),\n" ++
            "  `disas` (AArch64 disassembler), `asm` (assembler), `ps` (process table),\n" ++
            "  `crash` (tombstone viewer), `resources` (pool audit), `which` / `inventory`.\n";
    }
    return null;
}

pub fn cmd_help(m: *Monitor, args: []const []const u8) ExecError {
    if (args.len == 1) {
        if (std.mem.eql(u8, args[0], "--all")) {
            m.console.print_line("all monitor commands:");
            const reg = ensure_registry();
            for (reg) |cmd| {
                m.console.puts("  ");
                m.console.puts(cmd.name);
                m.console.puts(" - ");
                m.console.puts(cmd.help);
                m.console.puts("\n    usage: ");
                m.console.puts(cmd.usage);
                m.console.puts("\n");
            }
            return .none;
        }
        if (lookup(args[0])) |cmd| {
            m.console.puts(cmd.name);
            m.console.puts(" - ");
            m.console.puts(cmd.help);
            m.console.puts("\nusage: ");
            m.console.puts(cmd.usage);
            m.console.puts("\n");
            return .none;
        }
        if (topic_body(args[0])) |body| {
            m.console.puts(body);
            return .none;
        }
        err_prefix(m);
        m.console.puts("no such command or topic: ");
        m.console.puts(args[0]);
        m.console.puts("\n");
        return .invalid_argument;
    }
    m.console.print_line("available commands:");
    const reg = ensure_registry();
    var width: usize = 0;
    for (reg) |cmd| width = @max(width, cmd.name.len);
    for (category_order) |cat| {
        m.console.puts(category_name(cat));
        m.console.puts("\n");
        for (reg) |cmd| {
            if (cmd.category != cat) continue;
            m.console.puts("  ");
            m.console.puts(cmd.name);
            var pad: usize = cmd.name.len;
            while (pad < width) : (pad += 1) m.console.putc(' ');
            m.console.puts("  ");
            m.console.puts(cmd.help);
            m.console.puts("\n");
        }
    }
    m.console.print_line("type 'help <command>' for details on a single command.");
    m.console.print_line("type 'help <topic>' for a topic page (networking, windows, storage, graphics).");
    return .none;
}

pub fn cmd_about(m: *Monitor, args: []const []const u8) ExecError {
    _ = args;
    m.console.print_line("VirelaiOS is a from-scratch AArch64 operating system.");
    m.console.print_line("Written in freestanding Zig: no libc, no POSIX.");
    m.console.print_line("Hosted under Apple Virtualization.framework on Apple silicon.");
    m.console.print_line("Core subsystems: identity-map MMU, GICv3 PPI timer, round-robin");
    m.console.print_line("scheduler, per-task TTBR0 address spaces, EL0 processes & SVC (ADR 0007),");
    m.console.print_line("ESP/DATA FAT32 storage, virtio-net (ARP/ICMP/UDP/DHCP/TCP), Driving Award");
    m.console.print_line("window compositor + Road Pops terminal, and Apple xHCI USB HID input.");
    m.console.print_line("Type 'help' for the grouped command catalog, or 'welcome' for a tour.");
    return .none;
}

pub fn cmd_welcome(m: *Monitor, args: []const []const u8) ExecError {
    _ = args;
    m.console.print_line("Welcome to VirelaiOS! Here is a quick tour to get you oriented:");
    m.console.print_line("  1. Discovery: Type 'help' to see grouped commands, or 'help <cmd>' / 'help <topic>' for details.");
    m.console.print_line("  2. System Info: Type 'version', 'uname', or 'about' for architectural details.");
    m.console.print_line("  3. Tasks & Procs: Type 'procs' to view active processes or 'tasks' for scheduler states.");
    m.console.print_line("  4. Storage: Type 'ls' and 'cat <file>' to view files on the host share (--cvc-file).");
    m.console.print_line("  5. Windows & Graphics: Type 'win' to inspect window registry and z-order; 'dui cycle' to cycle focus.");
    m.console.print_line("  6. Networking: Type 'net' for device status, 'net ping <ip>' or 'net dhcp' to configure.");
    m.console.print_line("  7. Documentation: Architecture, decisions, and hardware contracts live in docs/.");
    m.console.print_line("Have fun and break things responsibly.");
    return .none;
}

pub fn cmd_sysinfo(m: *Monitor, args: []const []const u8) ExecError {
    _ = args;
    const h = &m.state.handoff;
    const map_summary = memmap.summarize(m.state.map);
    const p_stats = alloc.stats();
    const sched_rep = scheduler.stats();
    const win_armed = driving_award.armed();
    const rp_rep = road_pops.report();
    const in_rep = input.report();

    m.console.print_line("sysinfo: VirelaiOS AArch64 support snapshot");

    // System & Handoff
    m.console.puts("  system:     kernel=virelai-kernel handoff=v2 status=");
    if (handoff.validate(h) == .none) {
        m.console.print_line("valid");
    } else {
        m.console.print_line("invalid");
    }

    // OS identity — M115f (#2465): the build-stamped version label
    // (-Dos-version). An applied update carries a new label, so the
    // version line is the in-guest proof the base image changed.
    m.console.puts("  os:         version=");
    m.console.print_line(osver.label);

    // CPU & Timer
    m.console.puts("  cpu:        arch=aarch64 timer_armed=");
    m.console.puts(if (timer.armed()) "1" else "0");
    m.console.puts(" gic=");
    m.console.puts(gic.kind_name());
    m.console.puts(" ticks=");
    m.console.print_u64(timer.ticks);
    m.console.puts(" irq=");
    m.console.print_u64(timer.irq_ticks);
    m.console.puts("\n");

    // Memory & Allocator
    m.console.puts("  memory:     descriptors=");
    m.console.print_u64(m.state.map.count);
    m.console.puts(" usable=");
    m.console.print_hex(map_summary.usable_pages * memmap.page_size);
    m.console.puts(" (");
    m.console.print_hex(map_summary.usable_pages);
    m.console.puts(" pages)\n");

    m.console.puts("  allocator:  armed=");
    m.console.puts(if (p_stats.armed) "1" else "0");
    m.console.puts(" total=");
    m.console.print_hex(p_stats.total_pages);
    m.console.puts(" free=");
    m.console.print_hex(p_stats.free_pages);
    m.console.puts(" excluded=");
    m.console.print_hex(p_stats.excluded_pages);
    m.console.puts(" regions=");
    m.console.print_hex(@intCast(p_stats.region_count));
    m.console.puts("\n");

    // Tasks & Processes
    m.console.puts("  scheduler:  enabled=");
    m.console.puts(if (sched_rep.enabled) "1" else "0");
    m.console.puts(" tasks=");
    m.console.print_u64(sched_rep.count);
    m.console.puts("/");
    m.console.print_u64(scheduler.max_tasks);
    m.console.puts(" switches=");
    m.console.print_u64(sched_rep.switches);
    m.console.puts("\n");

    m.console.puts("  processes:  active=");
    m.console.print_u64(@intCast(process.count()));
    m.console.puts("/");
    m.console.print_u64(process.max_processes);
    m.console.puts("\n");

    // Storage — M34 HF6 (issue #740): the FAT volumes are gone; the host
    // share (--cvc-file) is the only file store.
    m.console.puts("  storage:    host_share=");
    m.console.puts(if (virtio_file.available()) "armed" else "absent");
    if (virtio_file.available()) {
        var lr = virtio_file.ListResult{};
        if (virtio_file.list("", &lr) == virtio_file.st_ok) {
            m.console.puts(" files=");
            m.console.print_u64(@intCast(lr.count));
        }
    }
    m.console.puts("\n");

    // Network
    m.console.puts("  network:    virtio-net=");
    m.console.puts(if (virtio_net.net_ready) "armed" else "unarmed");
    if (virtio_net.net_ready) {
        m.console.puts(" mac=");
        m.console.puts(&virtio_net.net_mac_text);
        m.console.puts(" ip=");
        var ipbuf: [15]u8 = undefined;
        const n = virtio_net.arp.format_ip(virtio_net.arp.own_ip, &ipbuf);
        m.console.puts(ipbuf[0..n]);
    }
    m.console.puts("\n");

    // Graphics & Windows
    m.console.puts("  graphics:   gpu=");
    m.console.puts(if (virtio_gpu.gpu_ready) "armed" else "unarmed");
    m.console.puts(" roadpops=");
    m.console.puts(if (rp_rep.armed) "armed" else "unarmed");
    m.console.puts(" windows=");
    if (win_armed) {
        m.console.print_u64(driving_award.count());
        m.console.puts(" focused=");
        m.console.print_u64(driving_award.focused_window_id());
    } else {
        m.console.puts("unarmed");
    }
    m.console.puts("\n");

    // Input
    m.console.puts("  input:      xhci=");
    m.console.puts(if (xhci.xhci_ready) "armed" else "unarmed");
    m.console.puts(" devices=");
    m.console.print_u64(xhci.enum_count);
    m.console.puts(" fifo=");
    m.console.puts(if (in_rep.armed) "armed" else "unarmed");
    m.console.puts("\n");

    // Uptime
    m.console.puts("  uptime:     ticks=");
    m.console.print_u64(timer.ticks);
    m.console.puts("\n");

    return .none;
}

pub fn cmd_version(m: *Monitor, args: []const []const u8) ExecError {
    _ = args;
    m.console.print_line("virelai-kernel");
    m.console.print_line("milestone-two kernel proper (ADR 0004)");
    m.console.print_line("handoff ABI v2");
    m.console.print_line("build label: m1.5 commands & personality (mock console)");
    return .none;
}

pub fn cmd_uname(m: *Monitor, args: []const []const u8) ExecError {
    _ = args;
    m.console.print_line("VirelaiOS aarch64");
    m.console.print_line("freestanding kernel; no POSIX compatibility");
    return .none;
}
