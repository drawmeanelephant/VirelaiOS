//! Monitor commands: machine/system state (`handoff`/`mem`/`pages`/`timer`/`pci`/`fault`/`dmesg`/`time`/`random`/`crash`/`forensics`/`tty`/`settings`/`secrets`/`reboot`/`shutdown`).
//!
//! Split out of monitor.zig (#3755): declarations only — the
//! shared types, registry, dispatch, completion, and print/parse
//! helpers stay in the root and are aliased below so the moved
//! code is verbatim.

const std = @import("std");
const mon = @import("../monitor.zig");

const Monitor = mon.Monitor;
const ExecError = mon.ExecError;
const MachineResult = mon.MachineResult;
const random_max_bytes = mon.random_max_bytes;
const err_prefix = mon.err_prefix;
const print_usage = mon.print_usage;
const parseInt = mon.parseInt;
const kernel_vf_allowed = mon.kernel_vf_allowed;
const fmtDropLine = mon.fmtDropLine;
const lookup = mon.lookup;
const exec = mon.exec;
const alloc = mon.alloc;
const svclock = mon.svclock;
const exceptions = mon.exceptions;
const gic = mon.gic;
const handoff = mon.handoff;
const measure = mon.measure;
const memmap = mon.memmap;
const pci = mon.pci;
const process = mon.process;
const scheduler = mon.scheduler;
const timer = mon.timer;
const virtio_file = mon.virtio_file;
const csprng = mon.csprng;
const clipboard = mon.clipboard;
const terminal = mon.terminal;
const driving_award = mon.driving_award;
const settings = mon.settings;
const tombstone = mon.tombstone;
const forensics = mon.forensics;
const events = mon.events;
const serial_ring = mon.serial_ring;
const secret = mon.secret;
/// "  <label>: <hex bytes> bytes (<hex pages> pages)"
fn print_mem_row(m: *Monitor, label: []const u8, pages: u64) void {
    m.console.puts("  ");
    m.console.puts(label);
    m.console.puts(": ");
    m.console.print_hex(memmap.bytes_of(pages));
    m.console.puts(" bytes (");
    m.console.print_hex(pages);
    m.console.puts(" pages)\n");
}

fn report_machine(m: *Monitor, verb: []const u8, result: MachineResult) ExecError {
    switch (result) {
        .ok => {
            m.console.puts(verb);
            m.console.puts(": ok\n");
            return .none;
        },
        .not_implemented => {
            err_prefix(m);
            m.console.puts(verb);
            m.console.print_line(": not implemented - no proven post-ExitBootServices machine-control mechanism; terminal WFE loop continues");
            return .not_implemented;
        },
        .failed => {
            err_prefix(m);
            m.console.puts(verb);
            m.console.print_line(": failed");
            return .machine_failed;
        },
    }
}

pub fn cmd_handoff(m: *Monitor, args: []const []const u8) ExecError {
    _ = args;
    const h = &m.state.handoff;
    m.console.print_line("handoff v4");
    const fields = [_]struct { label: []const u8, value: u64 }{
        .{ .label = "magic", .value = h.magic },
        .{ .label = "version", .value = h.version },
        .{ .label = "kernel_base", .value = h.kernel_base },
        .{ .label = "kernel_size", .value = h.kernel_size },
        .{ .label = "system_table", .value = h.system_table },
        .{ .label = "image_handle", .value = h.image_handle },
        .{ .label = "stack_base", .value = h.stack_base },
        .{ .label = "stack_size", .value = h.stack_size },
        .{ .label = "flags", .value = h.flags },
        .{ .label = "boot_epoch", .value = h.boot_epoch_secs },
        .{ .label = "measured_ok", .value = h.measured_ok },
    };
    for (fields) |field| {
        m.console.puts("  ");
        m.console.puts(field.label);
        var pad: usize = field.label.len;
        while (pad < 12) : (pad += 1) m.console.putc(' ');
        m.console.puts(" ");
        m.console.print_hex(field.value);
        m.console.puts("\n");
    }
    // v4 (#2418, ADR 0054): the loader's boot-stage digests — the receipt
    // the kernel published at boot is these exact values.
    var digest_hex: [64]u8 = undefined;
    _ = measure.hex_digest(&digest_hex, &h.measured_loader);
    m.console.puts("  loader_sha256 ");
    m.console.puts(&digest_hex);
    m.console.puts("\n");
    _ = measure.hex_digest(&digest_hex, &h.measured_kernel);
    m.console.puts("  kernel_sha256 ");
    m.console.puts(&digest_hex);
    m.console.puts("\n");
    m.console.puts("  status");
    var pad: usize = "status".len;
    while (pad < 12) : (pad += 1) m.console.putc(' ');
    m.console.puts(" ");
    const err = handoff.validate(h);
    if (err == .none) {
        m.console.print_line("valid");
    } else {
        m.console.puts("invalid (");
        m.console.puts(handoff.error_name(err));
        m.console.puts(")\n");
    }
    return .none;
}

pub fn cmd_mem(m: *Monitor, args: []const []const u8) ExecError {
    _ = args;
    const view = m.state.map;
    const summary = memmap.summarize(view);
    m.console.puts("mem: descriptors=");
    m.console.print_hex(@intCast(view.count));
    m.console.puts(" size=");
    m.console.print_hex(@intCast(view.descriptor_size));
    m.console.puts(" version=");
    m.console.print_hex(view.descriptor_version);
    m.console.puts(" key=");
    m.console.print_hex(view.key);
    m.console.puts("\n");
    print_mem_row(m, "usable", summary.usable_pages);
    print_mem_row(m, "conventional", summary.conventional_pages);
    print_mem_row(m, "loader", summary.loader_pages);
    print_mem_row(m, "boot_services", summary.boot_services_pages);
    print_mem_row(m, "runtime", summary.runtime_pages);
    print_mem_row(m, "reserved", summary.reserved_pages);
    print_mem_row(m, "mmio", summary.mmio_pages);
    // Kernel image bounds come from the handoff record, not the map. The
    // end address is computed with a checked add so a hostile handoff can
    // never wrap (or panic a Debug build) — the same saturating policy as
    // the map summary.
    const h = &m.state.handoff;
    const kernel_end = std.math.add(u64, h.kernel_base, h.kernel_size) catch std.math.maxInt(u64);
    m.console.puts("  kernel: ");
    m.console.print_hex(h.kernel_base);
    m.console.puts("..");
    m.console.print_hex(kernel_end);
    m.console.puts(" (");
    m.console.print_hex(h.kernel_size);
    m.console.puts(" bytes)\n");
    return .none;
}

/// M22 D12 (issue #335): `dmesg` — print the serial ring buffer contents.
pub fn cmd_dmesg(m: *Monitor, args: []const []const u8) ExecError {
    _ = args;
    var buf: [serial_ring.capacity * 2]u8 = undefined;
    const n = serial_ring.snapshot(&buf);
    if (n == 0) {
        m.console.print_line("dmesg: log is empty");
        return .none;
    }
    m.console.puts(buf[0..n]);
    // Ensure trailing newline
    if (n > 0 and buf[n - 1] != '\n') m.console.putc('\n');
    return .none;
}

/// M22 D13 (issue #336): `time <command> [args...]` — measure elapsed
/// ticks and wall-clock time for any command.
pub fn cmd_time(m: *Monitor, args: []const []const u8) ExecError {
    const start_ticks = timer.ticks;
    // Canonical lock order (svclock.zig): the nested exec acquires
    // KERNEL|dom, and this command already runs under exec's KERNEL hold —
    // KERNEL is LAST in the order, so taking a lower-ranked domain under
    // it would invert the order and close a cycle with the domain->KERNEL
    // takes (uaccess write-page resolution inside socket/file syscalls).
    // Drop the outer hold, let the nested exec acquire canonically, then
    // re-take so exec's paired release stays balanced.
    const outer = svclock.held_bits();
    svclock.release_set(outer);
    const result = exec(m, args);
    svclock.acquire_set(outer);
    const end_ticks = timer.ticks;
    const elapsed = end_ticks - start_ticks;
    // Convert ticks to seconds: the GIC timer fires at 24 MHz, but the
    // kernel counts scheduler ticks (typically ~100 Hz). Use timer.freq
    // if available, else assume 100 ticks/sec.
    const ticks_per_sec: u64 = if (timer.freq > 0) timer.freq else 100;
    const secs = elapsed / ticks_per_sec;
    const rem_ms = (elapsed % ticks_per_sec) * 1000 / ticks_per_sec;
    m.console.puts("real    ");
    m.console.print_u64(secs / 60);
    m.console.puts("m");
    if (rem_ms < 100) m.console.putc('0');
    if (rem_ms < 10) m.console.putc('0');
    m.console.print_u64(rem_ms);
    m.console.print_line("s");
    m.console.puts("ticks   ");
    m.console.print_u64(elapsed);
    m.console.puts("\n");
    return result;
}

// ---------------------------------------------------------------------------
// Physical page allocator command (next-card milestone)
// ---------------------------------------------------------------------------

pub fn cmd_pages(m: *Monitor, args: []const []const u8) ExecError {
    if (args.len == 1 and !std.mem.eql(u8, args[0], "selftest")) {
        print_usage(m, lookup("pages").?);
        return .usage;
    }
    const s = alloc.stats();
    if (!s.armed) {
        err_prefix(m);
        if (args.len == 1) {
            m.console.print_line("allocator not armed (no poolable memory in span) — selftest refused");
        } else {
            m.console.print_line("allocator not armed (no poolable memory in span)");
        }
        return .none;
    }
    if (args.len == 1) return pages_selftest(m, s);
    // #2649: ONE write — the live-long-lived gate parses free=, and a
    // mid-token split by a concurrent write loses the hex run.
    m.console.linef("pages: armed=1 total=0x{x:0>16} free=0x{x:0>16} excluded=0x{x:0>16} regions=0x{x:0>16} span=0x{x:0>16}\n", .{
        s.total_pages, s.free_pages, s.excluded_pages, s.region_count, s.span_pages,
    });
    return .none;
}

/// Deterministic bounded alloc/free battery against the module allocator:
/// alloc 1 / free, alloc 8 / free, alloc 3 + alloc 5 (contiguous) / free
/// both, alloc the largest free run / free, alloc total+1 -> none. Leaves
/// the pool exactly as it found it. The pool may be fragmented across
/// regions (real VZ maps are), so the "big" step allocates the largest
/// contiguous free run — which is guaranteed to fit — rather than the
/// whole pool (which need not be contiguous).
fn pages_selftest(m: *Monitor, s: alloc.Stats) ExecError {
    const total = s.total_pages;
    const initial_free = s.free_pages;
    var failed = false;
    if (pages_alloc_line(m, 1)) |base| failed = failed or !pages_free_line(m, base, 1, "free");
    if (pages_alloc_line(m, 8)) |base| failed = failed or !pages_free_line(m, base, 8, "free");
    const a3 = pages_alloc_line(m, 3);
    const a5 = pages_alloc_line(m, 5);
    if (a3 != null and a5 != null) {
        const ok3 = alloc.free_pages(a3.?, 3);
        const ok5 = alloc.free_pages(a5.?, 5);
        m.console.print_line(if (ok3 and ok5) "pages selftest: free both ok" else "pages selftest: free both FAILED");
        failed = failed or !(ok3 and ok5);
    } else {
        failed = true;
    }
    const largest = alloc.largest_free_run();
    if (pages_alloc_line(m, largest)) |base| failed = failed or !pages_free_line(m, base, largest, "free");
    // Allocating one more than the whole pool must fail.
    if (pages_alloc_line(m, total + 1) != null) failed = true;
    const after = alloc.stats();
    // The pool is "restored" when free is back to where the battery found
    // it (with exclusions, free < total by design — claim 5162).
    const restored = after.free_pages == initial_free;
    m.console.puts("pages selftest: ");
    m.console.puts(if (failed or !restored) "FAILED" else "ok");
    m.console.puts(" free=");
    m.console.print_hex(after.free_pages);
    m.console.puts("\n");
    return .none;
}

/// Print "alloc <n> -> <base>" (or "-> none (out of memory)") and return
/// the base on success.
fn pages_alloc_line(m: *Monitor, n: u64) ?u64 {
    m.console.puts("pages selftest: alloc ");
    m.console.print_u64(n);
    m.console.puts(" -> ");
    const base = alloc.alloc_pages(n);
    if (base) |b| {
        m.console.print_hex(b);
    } else {
        m.console.puts("none (out of memory)");
    }
    m.console.puts("\n");
    return base;
}

fn pages_free_line(m: *Monitor, base: u64, n: u64, label: []const u8) bool {
    const ok = alloc.free_pages(base, n);
    m.console.puts("pages selftest: ");
    m.console.puts(label);
    m.console.puts(if (ok) " ok" else " FAILED");
    m.console.puts("\n");
    return ok;
}

/// M73f-2 (#1632): print both ADR 0020 D1 drop counters for every bound
/// terminal — cheap and on-demand, never per-tick. Reads the existing
/// fields as-is (no getter, no syscall); prints nothing per-tick and
/// `tty: none` when no terminal is allocated.
pub fn cmd_tty(m: *Monitor, args: []const []const u8) ExecError {
    _ = args;
    var printed: usize = 0;
    for (0..terminal.max_terminals) |h| {
        const t = terminal.get(h) orelse continue;
        var buf: [96]u8 = undefined;
        m.console.puts(fmtDropLine(&buf, h, t.out_dropped, t.in_dropped));
        printed += 1;
    }
    if (printed == 0) m.console.puts("tty: none\n");
    return .none;
}

/// `crash` — list recent crash tombstones from /data/crash/.
/// Shows process name, PID, exit status, and tick for each tombstone.
/// M22 D11 (issue #334): `crash` — enhanced crash report viewer with
/// symbol resolution, formatted report, and serial snapshot display.
pub fn cmd_crash(m: *Monitor, args: []const []const u8) ExecError {
    const n = tombstone.count();
    if (n == 0) {
        m.console.print_line("crash: no tombstones recorded");
        return .none;
    }
    // `crash <index>` shows the detailed report for one tombstone
    if (args.len == 1) {
        const idx = parseInt(args[0]) catch {
            err_prefix(m);
            m.console.puts("crash: invalid index: ");
            m.console.puts(args[0]);
            m.console.puts("\n");
            return .invalid_argument;
        };
        if (idx >= n) {
            err_prefix(m);
            m.console.puts("crash: index out of range (max ");
            m.console.print_u64(n - 1);
            m.console.puts(")\n");
            return .invalid_argument;
        }
        if (tombstone.get(@intCast(idx))) |t| {
            var buf: [tombstone.tombstone_max_bytes]u8 = undefined;
            const len = tombstone.format_tombstone(t, &buf);
            m.console.puts(buf[0..len]);
            if (len > 0 and buf[len - 1] != '\n') m.console.putc('\n');
        }
        return .none;
    }
    // Default: list all tombstones with summary
    m.console.puts("crash: ");
    m.console.print_u64(n);
    m.console.print_line(" tombstone(s) (use 'crash <index>' for detail): ");
    _ = tombstone.list_to_console(m.console);
    return .none;
}

/// `settings` — report and modify persistent configuration (claim 2649,
/// milestone eight card U8). Backed by `SETTINGS.TXT` on the DATA partition.
/// `settings` / `settings list`: print all active key-value pairs.
/// `settings get <key>`: look up a single key.
/// `settings set <key> <val>`: update in memory and persist immediately.
/// `settings reset`: restore default settings and persist.
pub fn cmd_settings(m: *Monitor, args: []const []const u8) ExecError {
    if (args.len == 0 or (args.len == 1 and std.mem.eql(u8, args[0], "list"))) {
        m.console.print_line("settings:");
        var i: usize = 0;
        while (i < settings.count()) : (i += 1) {
            if (settings.entry_at(i)) |e| {
                m.console.linef("  {s}={s}\n", .{ e.key, e.val });
            }
        }
        return .none;
    }
    if (std.mem.eql(u8, args[0], "get")) {
        if (args.len != 2) {
            print_usage(m, lookup("settings").?);
            return .usage;
        }
        if (settings.get(args[1])) |val| {
            m.console.linef("settings: {s}={s}\n", .{ args[1], val });
            return .none;
        }
        err_prefix(m);
        m.console.puts("unknown setting: ");
        m.console.puts(args[1]);
        m.console.puts("\n");
        return .invalid_argument;
    }
    if (std.mem.eql(u8, args[0], "set")) {
        if (args.len != 3) {
            print_usage(m, lookup("settings").?);
            return .usage;
        }
        const key = args[1];
        const val = args[2];
        const res = settings.set(key, val);
        if (res != .ok) {
            err_prefix(m);
            m.console.puts("invalid setting key or value: ");
            m.console.puts(key);
            m.console.puts("\n");
            return .invalid_argument;
        }
        const saved = settings.save_to_share();
        // #2649: ONE write — live-shell-default greps `settings: k=v (persisted)`.
        m.console.linef("settings: {s}={s} ({s})\n", .{ key, val, if (saved) "persisted" else "memory only" });
        return .none;
    }
    if (std.mem.eql(u8, args[0], "reset")) {
        if (args.len != 1) {
            print_usage(m, lookup("settings").?);
            return .usage;
        }
        settings.reset();
        const saved = settings.save_to_share();
        m.console.linef("settings: reset to defaults ({s})\n", .{if (saved) "persisted" else "memory only"});
        return .none;
    }
    print_usage(m, lookup("settings").?);
    return .usage;
}

/// `secrets` — list the secret store's key NAMES only (M50 TS5, issue
/// #1139, ADR 0024 D8). Values NEVER leave the store through this command
/// (the monitor may print names only), and the command never mutates — the
/// store is host-provisioned; there is no `sys_secret_set`.
pub fn cmd_secrets(m: *Monitor, args: []const []const u8) ExecError {
    if (args.len != 0) {
        print_usage(m, lookup("secrets").?);
        return .usage;
    }
    const n = secret.count();
    if (n == 0) {
        m.console.print_line("secrets: (none)");
        return .none;
    }
    m.console.print_line("secrets:");
    var i: usize = 0;
    while (i < n) : (i += 1) {
        if (secret.entry_at(i)) |e| {
            m.console.puts("  ");
            m.console.puts(e.key); // NAMES only — never values
            m.console.puts("\n");
        }
    }
    return .none;
}

// ---------------------------------------------------------------------------
// Machine control commands
// ---------------------------------------------------------------------------

pub fn cmd_reboot(m: *Monitor, args: []const []const u8) ExecError {
    _ = args;
    return report_machine(m, "reboot", m.machine.reboot());
}

pub fn cmd_shutdown(m: *Monitor, args: []const []const u8) ExecError {
    _ = args;
    m.console.print_line("shutdown: initiating graceful shutdown...");

    // Step 1: Post WIN_CLOSE event to every user window (apps can save
    // via the unsaved dialog from #242). The window stays alive until
    // the process exits or is killed below.
    m.console.print_line("shutdown: closing windows...");
    var i: usize = 0;
    while (i < process.max_processes) : (i += 1) {
        if (process.info(@intCast(i))) |info| {
            if (info.state == .running and info.task_id != null) {
                // Post WIN_CLOSE to each user window owned by this process.
                const pid_val: usize = @intCast(info.id);
                var win_id: u8 = driving_award.user_window_id_base;
                while (win_id < driving_award.user_window_id_base + @as(u8, @intCast(driving_award.user_windows_max))) : (win_id += 1) {
                    if (driving_award.user_owner(win_id)) |owner| {
                        if (owner == pid_val) {
                            events.push(pid_val, .{
                                .kind = events.WIN_CLOSE,
                                .flags = 0,
                                .seq = 0,
                                .arg0 = win_id,
                                .arg1 = 0,
                            });
                        }
                    }
                }
            }
        }
    }

    // Step 2: Wait 50 scheduler ticks (~5 seconds) for graceful exit
    m.console.print_line("shutdown: waiting for apps (50 ticks)...");
    _ = scheduler.sleep_current(50);

    // Step 3: Forcibly kill any remaining user processes
    m.console.print_line("shutdown: killing remaining processes...");
    i = 0;
    while (i < process.max_processes) : (i += 1) {
        if (process.info(@intCast(i))) |info| {
            if (info.state == .running) {
                _ = scheduler.request_kill(@intCast(i));
            }
        }
    }

    // Step 4: Flush clipboard to /data/clipboard.txt (persistence across reboot)
    m.console.print_line("shutdown: flushing clipboard...");
    flush_clipboard_to_disk();

    // Step 5: Write shutdown marker to /data/SHUTDOWN.TXT with tick + reason
    m.console.print_line("shutdown: writing marker...");
    write_shutdown_marker("graceful");

    // Step 6: Halt the CPU (WFI loop — no return)
    m.console.print_line("shutdown: halting...");
    return report_machine(m, "shutdown", m.machine.shutdown());
}

/// Write shutdown marker to SHUTDOWN.TXT on the HOST SHARE (M34 HF6,
/// issue #740: the DATA partition is gone). `reason` is a short label
/// ("graceful", "reboot", etc.).
fn write_shutdown_marker(reason: []const u8) void {
    if (!virtio_file.available()) return;

    // Format marker content: tick count + reason
    var buf: [128]u8 = undefined;
    var pos: usize = 0;

    const header = "VirelaiOS Shutdown\n";
    @memcpy(buf[0..header.len], header);
    pos = header.len;

    const tick_prefix = "Tick: ";
    @memcpy(buf[pos..][0..tick_prefix.len], tick_prefix);
    pos += tick_prefix.len;
    pos += write_u64_to(buf[pos..], timer.ticks);
    buf[pos] = '\n';
    pos += 1;

    const reason_prefix = "Reason: ";
    @memcpy(buf[pos..][0..reason_prefix.len], reason_prefix);
    pos += reason_prefix.len;
    const rlen = @min(reason.len, buf.len - pos);
    @memcpy(buf[pos..][0..rlen], reason[0..rlen]);
    pos += rlen;
    buf[pos] = '\n';
    pos += 1;

    // M50 TS2 (ADR 0024 D4): the kernel-actor gate (secret paths deny).
    if (!kernel_vf_allowed("SHUTDOWN.TXT", .write)) return;
    _ = virtio_file.write_whole("SHUTDOWN.TXT", buf[0..pos]);
}

/// Flush the shared clipboard to clipboard.txt on the HOST SHARE for
/// persistence across reboot (Arc5 #244; HF6 moved it from /data).
fn flush_clipboard_to_disk() void {
    if (!virtio_file.available()) return;
    const clip_len = clipboard.current_len();
    if (clip_len == 0) return;
    var buf: [clipboard.capacity]u8 = undefined;
    const n = clipboard.get(&buf);
    if (n == 0) return;
    // M50 TS2 (ADR 0024 D4): the kernel-actor gate (secret paths deny).
    if (!kernel_vf_allowed("clipboard.txt", .write)) return;
    _ = virtio_file.write_whole("clipboard.txt", buf[0..n]);
}

fn write_u64_to(out: []u8, val: u64) usize {
    if (val == 0) {
        if (out.len > 0) out[0] = '0';
        return 1;
    }
    var buf: [20]u8 = undefined;
    var v = val;
    var len: usize = 0;
    while (v > 0) : (len += 1) {
        buf[len] = '0' + @as(u8, @intCast(v % 10));
        v /= 10;
    }
    var i: usize = 0;
    while (i < len and i < out.len) : (i += 1) {
        out[i] = buf[len - 1 - i];
    }
    return @min(len, out.len);
}

// ---------------------------------------------------------------------------
// Last-words recorder command (#1278)
// ---------------------------------------------------------------------------

/// `forensics [on|off|dump|reset]` — the last-words recorder (claim #1278).
///
/// Off by default on purpose: a default boot must be byte-identical, so the
/// recorder is only ever armed by a gate (or a human) that is chasing
/// exactly the kind of death that reports nothing.
pub fn cmd_forensics(m: *Monitor, args: []const []const u8) ExecError {
    const sub = if (args.len > 0) args[0] else "status";
    if (std.mem.eql(u8, sub, "on")) {
        forensics.enabled = true;
        // Record this very line's arrival, so the trace opens with a mark the
        // reader can anchor on rather than an ambiguous empty prefix.
        forensics.note(.shot, 1);
        m.console.puts("forensics: on (records prefix the next console line)\n");
        return .none;
    }
    if (std.mem.eql(u8, sub, "off")) {
        forensics.enabled = false;
        m.console.puts("forensics: off\n");
        return .none;
    }
    if (std.mem.eql(u8, sub, "reset")) {
        forensics.reset();
        m.console.puts("forensics: reset\n");
        return .none;
    }
    if (std.mem.eql(u8, sub, "dump")) {
        // Force the pending tail out even with recording off, so the last
        // capture can be read after the fact.
        var buf: [forensics.max_per_drain * 64]u8 = undefined;
        const n = forensics.format_pending(buf[0..]);
        m.console.puts("forensics: dump wrote=");
        m.console.print_u64(n);
        m.console.puts("\n");
        if (n > 0) m.console.puts(buf[0..n]);
        return .none;
    }
    m.console.puts("forensics: enabled=");
    m.console.print_u64(if (forensics.enabled) 1 else 0);
    m.console.puts(" pending=");
    m.console.print_u64(forensics.pending());
    m.console.puts(" emitted=");
    m.console.print_u64(forensics.emitted);
    m.console.puts(" truncated=");
    m.console.print_u64(forensics.truncated);
    m.console.puts("\n");
    return .none;
}

// ---------------------------------------------------------------------------
// Interrupt-controller + timer command (claim 7948)
// ---------------------------------------------------------------------------

/// Report the GIC + generic-timer state. Hardware-free: reads the discovery
/// globals (pre-exit ACPI values) and the timer counters, so it runs
/// unchanged in a host test process (armed=0, gic=none there).
/// `timer probe <ms>` (M170a #3033, ADR 0060) arms one task-less kernel
/// deadline on this core's comparator; the fire's result line
/// (`timer probe ms=<n> late_us=<m>`) prints from the main-context drain
/// once the deadline IRQ has been observed — never from the command.
pub fn cmd_timer(m: *Monitor, args: []const []const u8) ExecError {
    if (args.len != 0) {
        if (!std.mem.eql(u8, args[0], "probe") or args.len != 2) return .usage;
        const ms = std.fmt.parseInt(u64, args[1], 10) catch return .invalid_argument;
        if (!scheduler.probe_deadline_ms(ms)) {
            m.console.puts("timer probe refused\n");
        }
        return .none;
    }
    m.console.puts("timer: armed=");
    m.console.print_u64(if (gic.armed() and timer.armed()) 1 else 0);
    m.console.puts(" gic=");
    m.console.puts(gic.kind_name());
    m.console.puts(" dist=");
    m.console.print_hex_min(gic.dist_base);
    m.console.puts(" ppi=");
    m.console.print_hex_min(timer.ppi);
    m.console.puts(" freq=");
    m.console.print_hex_min(timer.freq);
    m.console.puts(" ticks=");
    m.console.print_u64(timer.ticks);
    m.console.puts(" irq=");
    m.console.print_u64(timer.irq_ticks);
    m.console.puts(" poll=");
    m.console.print_u64(timer.poll_ticks);
    // M170a #3033 (ADR 0060): comparator fires that were NOT the heartbeat
    // boundary — the deadline overlay's own counter (flat with no deadlines).
    m.console.puts(" deadline_irqs=");
    m.console.print_u64(timer.deadline_irqs);
    // WMP card 3 (#1274): the reschedule DEMAND. `requests` counts wakes that
    // became runnable while another task was executing — i.e. how often the 1 Hz
    // period tick is the only thing standing between a woken task and running;
    // `coalesced` is the surplus a wake burst absorbed rather than arming again;
    // `discharged` counts the rotations that served one. Nothing here pulls the
    // comparator — these are the numbers that say what such a pull would buy.
    m.console.puts(" resched_requests=");
    m.console.print_u64(scheduler.resched_requests);
    m.console.puts(" resched_coalesced=");
    m.console.print_u64(scheduler.resched_coalesced);
    m.console.puts(" resched_discharged=");
    m.console.print_u64(scheduler.resched_discharged);
    m.console.puts(" acked=");
    m.console.print_u64(gic.acked_total()); // claim 7339: summed across cores
    m.console.puts(" first=");
    m.console.print_hex_min(gic.first_intid[0]); // PE-0 first (SPIs are IROUTER-pinned there)
    m.console.puts("\n");
    return .none;
}

// ---------------------------------------------------------------------------
// Randomness command (milestone four, claim 2665)
// ---------------------------------------------------------------------------

/// Print `n` (1..256, default 16) random bytes from the seeded CSPRNG as
/// lowercase hex on one grep-able line: `random: n=<count> hex=<2n hex>`.
/// The CSPRNG is seeded at boot from the REAL virtio entropy device; the
/// class-B gate proves the real path by requiring two boots to produce
/// different output.
pub fn cmd_random(m: *Monitor, args: []const []const u8) ExecError {
    var count: usize = 16; // fixed-size sample when no argument
    if (args.len == 1) {
        count = @intCast(parseInt(args[0]) catch {
            m.console.puts("random: invalid count: ");
            m.console.puts(args[0]);
            m.console.puts("\n");
            return .invalid_argument;
        });
        if (count < 1 or count > random_max_bytes) {
            m.console.puts("random: count must be between 1 and ");
            m.console.print_u64(random_max_bytes);
            m.console.puts("\n");
            return .invalid_argument;
        }
    }
    var buf: [random_max_bytes]u8 = undefined;
    csprng.random_bytes(buf[0..count]);
    m.console.puts("random: n=");
    m.console.print_u64(count);
    m.console.puts(" hex=");
    const hex = "0123456789abcdef";
    var i: usize = 0;
    while (i < count) : (i += 1) {
        m.console.putc(hex[@as(usize, buf[i] >> 4)]);
        m.console.putc(hex[@as(usize, buf[i] & 0xf)]);
    }
    m.console.puts("\n");
    return .none;
}

// ---------------------------------------------------------------------------
// PCI enumeration command (macOS 27 custom-virtio spike, audit step 3)
// ---------------------------------------------------------------------------

/// Live bus-0 PCI enumeration through the ECAM window discovered pre-exit
/// (claim 0013). Post-exit ECAM reads work — the virtio console transport
/// does config-space reads post-MMU (claims 1517/6684); the historical
/// "pre-exit only" note predates the T0SZ=16 + TLBI fix. Aligned u32 reads
/// only (claim 0013: byte reads of config space return shifted/garbage
/// fields on VZ). This is the guest-side evidence for the custom virtio
/// spike device at DID 0x1082 (deviceID 0x42; host runner: `zig build
/// spike-virtio`).
pub fn cmd_pci(m: *Monitor, args: []const []const u8) ExecError {
    _ = args;
    const ecam = pci.pci_ecam;
    m.console.puts("pci: ecam=");
    m.console.print_hex(ecam);
    m.console.puts("\n");
    if (ecam == 0 or ecam > 4 * 1024 * 1024 * 1024) {
        m.console.print_line("pci: no ECAM (not discovered pre-exit)");
        return .none;
    }
    var found: usize = 0;
    var dev: u32 = 0;
    while (dev < 32 and found < 48) : (dev += 1) {
        var func: u32 = 0;
        var funcs: u32 = 1;
        while (func < funcs and found < 48) : (func += 1) {
            const id = pci.pci_read32(ecam, 0, dev, func, 0);
            const vid = id & 0xffff;
            if (vid == 0xffff) continue;
            const hdr = pci.pci_read32(ecam, 0, dev, func, 0x0c);
            const ht = (hdr >> 8) & 0xff;
            if (func == 0 and (ht & 0x80) != 0) funcs = 8;
            const did = id >> 16;
            m.console.puts("PCI D=");
            m.console.print_hex(@intCast(dev));
            m.console.puts(" F=");
            m.console.print_hex(@intCast(func));
            m.console.puts(" VID=");
            m.console.print_hex(vid);
            m.console.puts(" DID=");
            m.console.print_hex(did);
            m.console.puts(" CLS=");
            m.console.print_hex((pci.pci_read32(ecam, 0, dev, func, 8) >> 8) & 0xffffff);
            var b: u32 = 0;
            while (b < 6) : (b += 1) {
                m.console.puts(" B");
                m.console.print_hex(@intCast(b));
                m.console.puts("=");
                m.console.print_hex(pci.pci_read32(ecam, 0, dev, func, 0x10 + b * 4));
            }
            m.console.puts("\n");
            found += 1;
        }
    }
    m.console.puts("pci: found=");
    m.console.print_hex(@intCast(found));
    m.console.puts("\n");
    return .none;
}

// ---------------------------------------------------------------------------
// Exception-vector diagnostic command (claim 9746)
// ---------------------------------------------------------------------------

/// Deliberately trigger a synchronous exception (`udf`) with the resume
/// flag armed: the kernel's exception handler (installed at boot, claim
/// 9746) reports the fault, skips the faulting instruction, and the shell
/// resumes. Gates on `exceptions.installed()` — a host test process has no
/// vectors (even on an aarch64 host, where executing `udf` would SIGILL),
/// so the command honestly reports that instead.
pub fn cmd_fault(m: *Monitor, args: []const []const u8) ExecError {
    _ = args;
    if (!exceptions.installed()) {
        m.console.print_line("fault: exception vectors not installed; nothing to trigger");
        return .not_implemented;
    }
    m.console.print_line("fault: triggering udf (synchronous exception)...");
    exceptions.trigger_test_fault();
    m.console.print_line("fault: handled, resumed after faulting instruction");
    return .none;
}
