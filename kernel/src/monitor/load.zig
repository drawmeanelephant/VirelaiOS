//! Monitor commands: load-and-run + debug (`spawn`/`fuzz`/`exec`/`sym`/`strace`).
//!
//! Split out of monitor.zig (#3755): declarations only — the
//! shared types, registry, dispatch, completion, and print/parse
//! helpers stay in the root and are aliased below so the moved
//! code is verbatim.

const std = @import("std");
const mon = @import("../monitor.zig");

const Monitor = mon.Monitor;
const ExecError = mon.ExecError;
const sym_file_max = mon.sym_file_max;
const err_prefix = mon.err_prefix;
const err_line = mon.err_line;
const parseInt = mon.parseInt;
const kernel_vf_allowed = mon.kernel_vf_allowed;
const trust_denied = mon.trust_denied;
const exec = mon.exec;
const esp_exec = mon.esp_exec;
const syscall_mod = mon.syscall_mod;
const process = mon.process;
const scheduler = mon.scheduler;
const virtio_file = mon.virtio_file;
const smp = mon.smp;
// ---------------------------------------------------------------------------
// Task lifecycle command (claim 6729)
// ---------------------------------------------------------------------------

/// Spawn the lifecycle demo task (the scheduler owns the demo stack and
/// refuses a second spawn). Proves a runtime spawn enters the ring: the
/// demo task's `tasks spawn-demo advances=N` report lines follow. The
/// EL0 task's own exit + the idle task's reap prove the rest of the
/// lifecycle.
pub fn cmd_spawn(m: *Monitor, args: []const []const u8) ExecError {
    _ = args;
    if (scheduler.spawn_demo()) |id| {
        m.console.puts("spawn: spawn-demo id=");
        m.console.print_u64(@intCast(id));
        m.console.puts("\n");
    } else {
        m.console.puts("spawn: pool full or demo already running\n");
    }
    return .none;
}

// ---------------------------------------------------------------------------
// M70a-live (#1466): seeded EL0 fuzz + live queue-5 wire corpus
// ---------------------------------------------------------------------------

pub fn cmd_fuzz(m: *Monitor, args: []const []const u8) ExecError {
    const token = args[0];
    const is_roster = std.mem.eql(u8, token, "roster");
    if (!is_roster) {
        _ = parseInt(token) catch {
            err_line(m, "fuzz: seed must be roster or an integer (0x… hex or decimal)");
            return .invalid_argument;
        };
    }

    if (!virtio_file.available()) {
        m.console.print_line("fuzz: wire skipped (no queue-5)");
    } else {
        var first_stat = true;
        const run_one = struct {
            fn go(mm: *Monitor, seed: u64, print_stat: *bool) void {
                const r = virtio_file.fuzz_live_wire(seed);
                if (print_stat.*) {
                    print_stat.* = false;
                    var sbuf: [96]u8 = undefined;
                    const sl = std.fmt.bufPrint(&sbuf, "fuzz: wire-stat USER.BIN ok={d} size={d}\n", .{
                        @intFromBool(r.stat_ok),
                        r.stat_size,
                    }) catch "fuzz: wire-stat USER.BIN\n";
                    mm.console.puts(sl);
                }
                var buf: [128]u8 = undefined;
                const line = std.fmt.bufPrint(&buf, "fuzz: wire seed=0x{x} decoded={d} violations={d}\n", .{
                    seed, r.decoded, r.violations,
                }) catch "fuzz: wire\n";
                mm.console.puts(line);
            }
        }.go;
        if (is_roster) {
            for (virtio_file.fuzz_seeds) |seed| run_one(m, seed, &first_stat);
        } else {
            const seed = parseInt(token) catch 0;
            run_one(m, seed, &first_stat);
        }
    }

    return cmd_exec(m, &.{ "EL0EXEC.BIN", "fuzz", token });
}

// ---------------------------------------------------------------------------
// ESP exec command (claim 6783 — milestone-three card 6)
// ---------------------------------------------------------------------------

/// Load a user program from the ESP and enter it at EL0. `exec [<file>
/// [arg...]]` defaults to `USER.BIN` (the image builder embeds it at the
/// volume root; it can also be written at runtime through the FAT `write`
/// path). The program must be a DSK1 flat image; the kernel reads it
/// through the claim-6420 FAT path, rebuilds the EL0 user root around its
/// page, and spawns it as an EL0t task. Flags: `-c<core>` pins the spawned
/// task (SMP, claim 2369); `-u<uid>` is the M50 TS3 (#1137, ADR 0024 D5)
/// ADMIN SPAWN — `-u0` assigns `uid_system` + `kernel_caps`, `-u1000` the
/// default `uid_user` + no caps. It is the only path that can name a
/// principal; EL0 `sys_exec` inherits the caller's (no elevation).
/// Arguments (card 3e, claim 4636)
/// are packed into the program's text page and passed at entry (argc in
/// x0, argv block VA in x1) — bounded to `max_exec_args`; more than that
/// is refused honestly. Every failure mode is reported honestly.
pub fn cmd_exec(m: *Monitor, args: []const []const u8) ExecError {
    // SMP user tasks (claim 2369): `exec -c<core> <file>` pins the spawned
    // task to one core (console TX is locked, so a pinned program may print
    // from there). Claim 907 (#858): `-c0` is now legal too — an explicit
    // pin to CORE 0 (secondary_ok off), so a fourth task can be pinned to
    // core 0 alongside the shell for the four-core four-domain gate. The
    // unpinned default (no flag) stays distinct: `pinned` tracks the flag's
    // presence because `pin == 0` is now a VALID explicit pin.
    //
    // M50 TS3 (#1137, ADR 0024 D5): `exec -u<uid> <file>` is the monitor's
    // ADMIN SPAWN — the EL1h monitor (and only the monitor; EL0 `sys_exec`
    // has no principal argument and always inherits the caller) may name a
    // spawn principal. The two ADR D1 principals are the whole vocabulary:
    // `-u0` assigns `uid_system` + `kernel_caps` (the administrative
    // principal), `-u1000` assigns `uid_user` + no caps (the default), and
    // any other uid is refused. This is a control surface on the raw
    // console — an EL0 process has no path to request a principal.
    var pinned = false;
    var pin: usize = 0;
    var spawn_principal = process.default_principal;
    var rest = args;
    while (rest.len >= 1 and rest[0].len > 2 and rest[0][0] == '-') {
        const flag = rest[0][1];
        if (flag == 'c') {
            var value: usize = 0;
            for (rest[0][2..]) |ch| {
                if (ch < '0' or ch > '9') {
                    err_prefix(m);
                    m.console.print_line("-c<core>: core must be a decimal number");
                    return .invalid_argument;
                }
                value = value * 10 + (ch - '0');
            }
            if (value >= smp.max_cores) {
                err_prefix(m);
                m.console.puts("-c<core>: core must be in 0..");
                m.console.print_u64(smp.max_cores - 1);
                m.console.puts("\n");
                return .invalid_argument;
            }
            // M70b review: an offline core is rejected up front — a task
            // pinned to an offline core's ring is unreachable (steal
            // rejects it everywhere else, and the core never claims).
            if (!smp.core_online[value]) {
                err_prefix(m);
                var cbuf: [96]u8 = undefined;
                m.console.puts(std.fmt.bufPrint(&cbuf, "-c<core>: core {d} is not online (smp: cores={d})\n", .{ value, smp.num_cores }) catch "-c<core>: core is not online\n");
                return .invalid_argument;
            }
            pinned = true;
            pin = value;
        } else if (flag == 'u') {
            var value: u32 = 0;
            for (rest[0][2..]) |ch| {
                if (ch < '0' or ch > '9') {
                    err_prefix(m);
                    m.console.print_line("-u<uid>: uid must be 0 (system), 1000 (user) or 2000 (agent)");
                    return .invalid_argument;
                }
                const digit: u32 = ch - '0';
                // Reject a decimal that cannot fit a u32 BEFORE it wraps
                // (an overflowed value must never be mistaken for a valid
                // principal).
                if (value > (0xffff_ffff - digit) / 10) {
                    err_prefix(m);
                    m.console.print_line("-u<uid>: uid must be 0 (system), 1000 (user) or 2000 (agent)");
                    return .invalid_argument;
                }
                value = value * 10 + digit;
            }
            if (value == process.uid_system) {
                spawn_principal = .{ .uid = process.uid_system, .caps = process.kernel_caps };
            } else if (value == process.uid_user) {
                spawn_principal = .{ .uid = process.uid_user, .caps = 0 };
            } else if (value == process.uid_agent) {
                spawn_principal = process.agent_principal;
            } else {
                err_prefix(m);
                m.console.print_line("-u<uid>: uid must be 0 (system), 1000 (user) or 2000 (agent)");
                return .invalid_argument;
            }
        } else break;
        rest = rest[1..];
    }
    const name = if (rest.len >= 1) rest[0] else esp_exec.default_name;
    const prog_args = if (rest.len >= 2) rest[1..] else &.{};
    const result = if (pinned)
        esp_exec.exec_file_pinned_as(name, prog_args, pin, spawn_principal)
    else
        esp_exec.exec_file_as(name, prog_args, spawn_principal);
    switch (result) {
        .ok => {
            const info = esp_exec.loaded().?;
            // One write per line (the claim-881-slice-4 discipline): with
            // M70b wake targeting the exec'd task starts on a remote core
            // the moment it is published, and its first output can
            // otherwise land between these puts and split the very line
            // the live gates byte-match (observed: `exec: loaded ` +
            // SMPNET's first heartbeat + ` size=`).
            const head = esp_exec.head();
            var head_value: u64 = 0;
            for (head, 0..) |byte, i| head_value |= @as(u64, byte) << @intCast(56 - i * 8);
            var buf: [256]u8 = undefined;
            const line = if (info.data_len > 0)
                // M171a (card #3032): datapages is the RESIDENT count at
                // exec, mappedpages the VIRTUAL span — the demand-zero
                // tail counts in the second only.
                std.fmt.bufPrint(&buf, "exec: loaded {s} size=0x{x:0>16} entry=0x{x:0>16} stack=0x{x:0>16} head=0x{x:0>16} data=0x{x:0>16} datapages={d} mappedpages={d}\n", .{ info.name, info.content_len, info.entry_va, info.stack_va, head_value, info.data_len, info.data_pages, info.data_map_pages }) catch return .not_implemented
            else
                std.fmt.bufPrint(&buf, "exec: loaded {s} size=0x{x:0>16} entry=0x{x:0>16} stack=0x{x:0>16} head=0x{x:0>16}\n", .{ info.name, info.content_len, info.entry_va, info.stack_va, head_value }) catch return .not_implemented;
            m.console.puts(line);
            return .none;
        },
        .no_disk => {
            err_prefix(m);
            m.console.print_line("no host file channel (boot the runner with --cvc-file <host-dir>)");
            return .not_implemented;
        },
        .not_found => {
            err_prefix(m);
            m.console.puts(name);
            m.console.print_line(": not found on the host share (must be a DSK1/DSK3/ELF image)");
            return .invalid_argument;
        },
        // M70c-K (issue #1504): the two size refusals name the bound each
        // one crossed, so a log says WHY the image was refused: a file past
        // the acceptance bound is refused whatever its shape, while a shape
        // that must be STAGED is capped by the staging buffer (the streamed
        // gap-layout path is not).
        .image_too_large => {
            err_prefix(m);
            m.console.puts(name);
            m.console.puts(": image too large (acceptance bound ");
            m.console.print_hex(esp_exec.exec_image_max);
            m.console.print_line(" bytes)");
            return .invalid_argument;
        },
        .staging_too_large => {
            err_prefix(m);
            m.console.puts(name);
            m.console.puts(": image too large for the ");
            m.console.print_hex(esp_exec.exec_program_max);
            m.console.print_line("-byte staging buffer (only a gap-layout static ELF streams past it)");
            return .invalid_argument;
        },
        // M72a (issue #1579): the third size refusal, and the one a large
        // zero-filled `.bss`/`.noptrbss` crosses. It says MEMORY, because
        // that is what the bound is: the loader allocates and zero-fills
        // every mapped page, so this image is refused for what it would
        // have taken out of the guest, not for its size on disk and not for
        // a buffer it would never have transited.
        .map_too_large => {
            err_prefix(m);
            m.console.puts(name);
            m.console.puts(": image maps too much memory (map bound ");
            m.console.print_hex(esp_exec.map_max);
            m.console.print_line(" bytes; every PT_LOAD page is allocated and zeroed)");
            return .invalid_argument;
        },
        .bad_magic => {
            err_prefix(m);
            m.console.puts(name);
            m.console.print_line(": not a DSK1 program image (bad magic)");
            return .invalid_argument;
        },
        .bad_entry => {
            err_prefix(m);
            m.console.puts(name);
            m.console.print_line(": bad entry offset (outside the loaded content)");
            return .invalid_argument;
        },
        .out_of_memory => {
            err_prefix(m);
            m.console.print_line("out of physical pages (text/stack/exception stack)");
            return .machine_failed;
        },
        .pool_full => {
            m.console.puts("error: no free scheduler pool slot\n");
            return .machine_failed;
        },
        .table_full => {
            err_prefix(m);
            m.console.print_line("page-table carve-out exhausted (too many user-root rebuilds)");
            return .machine_failed;
        },
        .process_full => {
            err_prefix(m);
            m.console.print_line("process registry exhausted (all processes live; wait for one to exit)");
            return .machine_failed;
        },
        .too_many_args => {
            err_prefix(m);
            m.console.puts("too many arguments (max ");
            m.console.print_u64(esp_exec.max_exec_args);
            m.console.print_line(")");
            return .invalid_argument;
        },
        .arg_too_long => {
            err_prefix(m);
            m.console.puts("argument longer than ");
            m.console.print_u64(esp_exec.arg_slot_bytes - 1);
            m.console.print_line(" bytes");
            return .invalid_argument;
        },
        .no_args_room => {
            err_prefix(m);
            m.console.print_line("image leaves no room for the argv block");
            return .invalid_argument;
        },
        // M70b review: defensive backstop for the -c parse above (the
        // exec-file choke point refuses an offline pin for every caller).
        .bad_core => {
            err_prefix(m);
            m.console.print_line("-c<core>: core is not online");
            return .invalid_argument;
        },
        // M22 D1 (issue #324): honest ELF refusals.
        .bad_elf => {
            err_prefix(m);
            m.console.puts(name);
            m.console.print_line(": not a valid ELF file");
            return .invalid_argument;
        },
        .unsupported_arch => {
            err_prefix(m);
            m.console.puts(name);
            m.console.print_line(": unsupported architecture (AArch64 only)");
            return .invalid_argument;
        },
        .no_pt_load => {
            err_prefix(m);
            m.console.puts(name);
            m.console.print_line(": no PT_LOAD segments");
            return .invalid_argument;
        },
        .too_many_segments => {
            err_prefix(m);
            m.console.puts(name);
            m.console.print_line(": too many segments (max 2)");
            return .invalid_argument;
        },
        .segment_too_large => {
            err_prefix(m);
            m.console.puts(name);
            m.console.print_line(": segment too large or bad segment layout");
            return .invalid_argument;
        },
        // M70c-K (issue #1504): the streamed load reads each segment's
        // bytes straight from the file, so a file that ends before its own
        // header promises is refused BY NAME instead of mapping a
        // half-filled segment (distinct from `too_large`, which is about
        // what the loader accepts, and from `not_found`).
        .image_truncated => {
            err_prefix(m);
            m.console.puts(name);
            m.console.print_line(": truncated image (file ends before the bytes its header promises)");
            return .invalid_argument;
        },
        // M113c (#2419, ADR 0054 D8): a grant row names this image but
        // the measured digest mismatched — refused by name, never
        // fell open.
        .grant_stale => {
            err_prefix(m);
            m.console.puts(name);
            m.console.print_line(": exec refused (grant_stale — a named grant row's sha256 does not match this image)");
            return .invalid_argument;
        },
        // R4-F1 (#3390): the recorded agent name is reserved for the
        // operator-provisioned image — OWNERS.TXT must pin it
        // uid_system-owned with no non-owner write bit.
        .name_unprovisioned => {
            err_prefix(m);
            m.console.puts(name);
            m.console.print_line(": exec refused (name_unprovisioned — the recorded agent image requires a uid_system OWNERS row)");
            return .invalid_argument;
        },
    }
}

// ---------------------------------------------------------------------------
// Syscall ABI command (claim 3594)
// ---------------------------------------------------------------------------

/// M22 D3 (issue #326): `sym` — list the crash-report symbol table loaded
/// by the most recent ELF exec, or parse an ELF's symtab straight from the
/// volume without loading it.
pub fn cmd_sym(m: *Monitor, args: []const []const u8) ExecError {
    const symbol = @import("../symbol.zig");
    const elf_mod = @import("../elf.zig");
    if (args.len == 0) {
        const n = symbol.count();
        if (n == 0) {
            m.console.print_line("sym: no symbols loaded (exec an ELF image)");
            return .none;
        }
        m.console.puts("sym: ");
        m.console.print_u64(n);
        m.console.print_line(" symbol(s):");
        var i: usize = 0;
        while (i < n) : (i += 1) {
            const sym = symbol.get(i).?;
            m.console.puts("  ");
            m.console.puts(sym.name_slice());
            m.console.puts(" addr=");
            m.console.print_hex(sym.addr);
            m.console.puts(" size=");
            m.console.print_hex(sym.size);
            m.console.print_line("");
        }
        return .none;
    }

    // File inspection: read up to sym_file_max bytes and walk sections
    // (HF6: from the host share — the FAT volume is gone).
    const name = args[0];
    if (!kernel_vf_allowed(name, .read)) return trust_denied(m, "sym", name);
    const got = virtio_file.read_whole(name, &sym_file_buf) orelse {
        err_prefix(m);
        m.console.puts(name);
        m.console.print_line(": not found on the host share");
        return .invalid_argument;
    };
    var infos: [symbol.max_symbols]elf_mod.SymInfo = undefined;
    const n = elf_mod.collect_symbols(sym_file_buf[0..got], &infos);
    if (n == 0) {
        m.console.puts(name);
        m.console.print_line(": no symbols (stripped or not an AArch64 ELF)");
        return .none;
    }
    m.console.puts(name);
    m.console.puts(": ");
    m.console.print_u64(n);
    m.console.print_line(" symbol(s):");
    for (infos[0..n]) |si| {
        m.console.puts("  ");
        m.console.puts(si.name);
        m.console.puts(" addr=");
        m.console.print_hex(si.addr);
        m.console.puts(" size=");
        m.console.print_hex(si.size);
        m.console.print_line("");
    }
    return .none;
}

/// Staging buffer for `sym <file>` disk inspection.
var sym_file_buf: [sym_file_max]u8 = undefined;

/// M22 D5 (issue #328): `strace exec <file> [args...]` arms the kernel
/// tracer for the process the exec spawns (the loader records its pid at
/// the success point, before the task first runs, so every syscall is
/// seen). `strace off` disarms. Anything else prints usage honestly.
pub fn cmd_strace(m: *Monitor, args: []const []const u8) ExecError {
    if (args.len == 1 and std.mem.eql(u8, args[0], "off")) {
        syscall_mod.strace_pid = null;
        m.console.print_line("strace: off");
        return .none;
    }
    if (args.len >= 2 and std.mem.eql(u8, args[0], "exec")) {
        const res = esp_exec.exec_file(args[1], args[2..]);
        if (res != .ok) {
            err_prefix(m);
            m.console.puts("strace: exec refused (");
            m.console.print_u64(@backingInt(res));
            m.console.print_line(")");
            return .invalid_argument;
        }
        syscall_mod.strace_pid = esp_exec.last_exec_pid();
        m.console.print_line("strace: armed");
        return .none;
    }
    err_prefix(m);
    m.console.print_line("usage: strace exec <file> [args...] | strace off");
    return .invalid_argument;
}
