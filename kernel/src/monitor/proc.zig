//! Monitor commands: task/process inspection (`tasks`/`smp`/`procs`/`kill`/`mbox`/`ps`/`syscalls`/`addrspaces`/`resources`/`uaccess`).
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
const lookup = mon.lookup;
const exec = mon.exec;
const svclock = mon.svclock;
const mailbox = mon.mailbox;
const mmu = mon.mmu;
const process = mon.process;
const scheduler = mon.scheduler;
const syscall = mon.syscall;
const uaccess = mon.uaccess;
const userspace = mon.userspace;
const driving_award = mon.driving_award;
const events = mon.events;
const file_table = mon.file_table;
const smp = mon.smp;
// ---------------------------------------------------------------------------
// Tick-driven task scheduler command (claim 5275)
// ---------------------------------------------------------------------------

/// Report the round-robin scheduler state: enabled/current/switches plus
/// per-task saves/resumes/advances. Deterministic and grep-able; runs
/// unchanged in a host test process (tasks may be registered but never
/// started there, so the counters read 0).
pub fn cmd_tasks(m: *Monitor, args: []const []const u8) ExecError {
    _ = args;
    const s = scheduler.stats();
    m.console.puts("tasks: enabled=");
    m.console.print_u64(if (s.enabled) 1 else 0);
    m.console.puts(" current=");
    m.console.print_u64(@intCast(s.current));
    m.console.puts(" switches=");
    m.console.print_u64(s.switches);
    // Claim 6729: the lifecycle's pool/zombie counts (registered slots vs
    // the fixed pool; zombies awaiting the idle task's reap).
    m.console.puts(" pool=");
    m.console.print_u64(@intCast(s.count));
    m.console.puts("/");
    m.console.print_u64(@intCast(scheduler.max_tasks));
    m.console.puts(" zombies=");
    m.console.print_u64(@intCast(s.zombies));
    m.console.puts("\n");
    // Scan the full pool: reaped (free) slots are skipped, so a freed mid
    // slot never hides later-registered tasks.
    var width: usize = 0;
    var i: usize = 0;
    while (i < scheduler.max_tasks) : (i += 1) {
        if (scheduler.task_info(i)) |t| width = @max(width, t.name.len);
    }
    i = 0;
    while (i < scheduler.max_tasks) : (i += 1) {
        const info = scheduler.task_info(i) orelse continue;
        m.console.puts("  ");
        m.console.puts(info.name);
        var pad: usize = info.name.len;
        while (pad <= width) : (pad += 1) m.console.putc(' ');
        m.console.puts("saves=");
        m.console.print_u64(info.saves);
        m.console.puts(" resumes=");
        m.console.print_u64(info.resumes);
        m.console.puts(" advances=");
        m.console.print_u64(info.advances);
        m.console.puts(" state=");
        m.console.puts(scheduler.state_name(info.state));
        m.console.puts("\n");
    }
    return .none;
}

pub fn cmd_smp(m: *Monitor, args: []const []const u8) ExecError {
    _ = args;
    // M70b (#1454): every line below is built into one buffer and written
    // once (the claim-881-slice-4 rule) — with wake targeting, secondary
    // cores print concurrently with the monitor, and a split line is
    // exactly what the live gates' per-core placement asserts grep for.
    var online_count: u64 = 0;
    var c: usize = 0;
    while (c < smp.max_cores) : (c += 1) {
        if (smp.core_online[c]) online_count += 1;
    }
    var buf: [160]u8 = undefined;
    m.console.puts(std.fmt.bufPrint(&buf, "smp: cores={d} online={d}\n", .{ smp.num_cores, online_count }) catch return .not_implemented);

    c = 0;
    while (c < smp.max_cores) : (c += 1) {
        if (c >= smp.num_cores and !smp.core_online[c]) continue;
        const cur_tid = scheduler.current_task_for_core(c);
        // The primary exec task has the generic TCB name "user-exec";
        // threads already carry the process name. Report the same identity
        // for either executor, independently of the scheduler report drain.
        const process_name: ?[]const u8 = if (process.find_by_task(cur_tid)) |pid|
            if (process.info(pid)) |info| info.name else null
        else
            null;
        const task_name = if (process_name) |name|
            name
        else if (scheduler.task_info(cur_tid)) |info|
            info.name
        else
            "none";
        const line = std.fmt.bufPrint(&buf, "  core {d}: {s} mpidr=0x{x:0>16} state={s} ticks={d} task={s}\n", .{
            c,
            if (c == 0) @as([]const u8, "bsp ") else @as([]const u8, "ap  "),
            smp.core_mpidr[c],
            if (smp.core_online[c]) @as([]const u8, "online") else @as([]const u8, "offline"),
            smp.core_ticks[c], // per-core tick counter (claim 8477 follow-up)
            task_name,
        }) catch return .not_implemented;
        m.console.puts(line);
    }
    // M70b (#1454): contention evidence for the wake/TCB sched_lock —
    // observed data read at the measurement point, never a threshold.
    m.console.puts(std.fmt.bufPrint(&buf, "smp: sched-lock acquires={d} contended={d} spins={d}\n", .{ scheduler.sched_lock_acquires, scheduler.sched_lock_contended, scheduler.sched_lock_spins }) catch return .not_implemented);
    // M70b (#1454): wake-placement evidence — of all blocked->ready
    // transitions, how many stayed on the calling core, how many landed
    // on a remote (targeted) ring, and how many of those nudged a parked
    // target with the RESCHEDULE SGI.
    m.console.puts(std.fmt.bufPrint(&buf, "smp: wakes={d} local={d} remote={d} nudges={d}\n", .{ scheduler.wake_local + scheduler.wake_remote, scheduler.wake_local, scheduler.wake_remote, scheduler.wake_nudges }) catch return .not_implemented);
    // M133b (#2756): per-task tick attribution + aging state. The
    // scheduler charges each core tick to that core's `current`; the
    // `run=` delta between two `smp` reads is the task's measured share
    // of the window, and `decay=`/`skip=` show the aging penalty the
    // claim scan applies. The hog-share leg diffs these rows.
    var i: usize = 0;
    while (i < scheduler.max_tasks) : (i += 1) {
        const info = scheduler.task_info(i) orelse continue;
        const run_name: []const u8 = if (process.find_by_task(i)) |pid|
            if (process.info(pid)) |pinfo| pinfo.name else info.name
        else
            info.name;
        m.console.puts(std.fmt.bufPrint(&buf, "smp: run task={d} name={s} ticks={d} decay={d} skip={d}\n", .{
            i, run_name, info.run_ticks, info.age_penalty, info.age_skip,
        }) catch return .not_implemented);
    }
    return .none;
}

// ---------------------------------------------------------------------------
// Process command (claim 3848)
// ---------------------------------------------------------------------------

/// Report the process table (claim 3848): one line per non-free process
/// descriptor — id, name, lifecycle state, the bound executor task ("reaped"
/// once exited), the user stack VA, and the exit status (only for exited
/// processes). The process owns the PROGRAM (image + address space + state
/// + exit status) above the task pool: after the idle task reaps the
/// executor slot, `procs` still shows the program's exit status — the
/// information the task lifecycle alone throws away. Deterministic and
/// grep-able.
pub fn cmd_procs(m: *Monitor, args: []const []const u8) ExecError {
    // Opt-in diagnostic: leave the catalog, help and ordinary table byte-exact.
    if (args.len > 0) return cmd_runtime_receipt(m, args);
    var buf: [256]u8 = undefined;
    m.console.puts(std.fmt.bufPrint(&buf, "procs: count={d}\n", .{process.count()}) catch return .not_implemented);
    var id: usize = 0;
    while (id < process.max_processes) : (id += 1) {
        const info = process.info(id) orelse continue;
        var task_buf: [20]u8 = undefined;
        var exit_buf: [20]u8 = undefined;
        // Claim 4613: an exited process keeps its executor slot until the
        // lifecycle reap frees its pages — the row still reads `reaped`
        // (the slot is a zombie, no longer executing the program).
        const task_text = if (info.state == .exited)
            @as([]const u8, "reaped")
        else if (info.task_id) |task_id|
            std.fmt.bufPrint(&task_buf, "{d}", .{task_id}) catch return .not_implemented
        else
            "-";
        const exit_text = if (info.state == .exited)
            std.fmt.bufPrint(&exit_buf, "{d}", .{info.exit_status}) catch return .not_implemented
        else
            "-";
        m.console.puts(std.fmt.bufPrint(&buf, "procs: id={d} name={s} uid={d} caps={d} state={s} task={s} stack=0x{x:0>16} exit={s}\n", .{
            info.id,   info.name,     info.uid,  info.caps, process.state_name(info.state),
            task_text, info.stack_va, exit_text,
        }) catch return .not_implemented);
    }
    return .none;
}

/// `procs receipt [pid|name]` reads live or retained final counters. One
/// kernel-domain hold covers identity, counters and reap state, matching the
/// update paths. Live rows always have reaped=0, even if the primary task
/// exited before its siblings. Retention is bounded by procs history.
fn cmd_runtime_receipt(m: *Monitor, args: []const []const u8) ExecError {
    if (!std.mem.eql(u8, args[0], "receipt")) {
        print_usage(m, lookup("procs").?);
        return .usage;
    }
    const taken = svclock.acquire_missing(svclock.dom_bit(.kernel));
    defer svclock.release_set(taken);
    var found = false;
    var id: usize = 0;
    while (id < process.max_processes) : (id += 1) {
        const info = process.info(id) orelse continue;
        if (args.len == 2) {
            const matches = if (parseInt(args[1])) |pid|
                pid == id
            else |_|
                std.mem.eql(u8, args[1], info.name);
            if (!matches) continue;
        }
        const receipt = process.runtime_receipt(id) orelse continue;
        var buf: [384]u8 = undefined;
        m.console.puts(std.fmt.bufPrint(&buf, "runtime-receipt: pid={d} name={s} peak_pages={d} page_cap={d} peak_regions={d} region_cap={d} static_pages={d} page_tracking=extensible page_saturated={d} total_pages={d} record_failures={d} unrecorded_pages=0 reaped={d}\n", .{
            id,                                                           info.name,
            receipt.peak_pages,                                           process.max_dynamic_pages,
            receipt.peak_regions,                                         process.max_mmap_regions,
            receipt.static_pages,                                         @intFromBool(receipt.peak_pages >= process.max_dynamic_pages),
            receipt.total_pages,                                          receipt.record_failures,
            @intFromBool(info.state == .exited and info.task_id == null),
        }) catch return .not_implemented);
        found = true;
    }
    if (!found) m.console.print_line("runtime-receipt: none");
    return .none;
}

// ---------------------------------------------------------------------------
// Kill command (milestone-four follow-on 3, card 3c — claim 7786)
// ---------------------------------------------------------------------------

/// `kill <pid|name>` — the kernel, not the program, owns process lifetime.
/// The target is looked up in the process registry (by `procs` id or by
/// process name), then ARMED for termination: `scheduler.request_kill`
/// marks its TCB, and the ring converts the target's NEXT selection into
/// the existing exit path with the reserved status 137 (the counter's
/// `exit=137` in `procs` / `tasks user-exec exited status=137`). No new
/// syscall — ADR 0007 stays frozen. Every refusal is clean and exact
/// (host-tested strings).
pub fn cmd_kill(m: *Monitor, args: []const []const u8) ExecError {
    const arg = args[0];
    // Resolve the target process: by numeric `procs` id, else by name.
    const pid: ?usize = if (parseInt(arg)) |value| blk: {
        if (value < process.max_processes and process.info(@as(usize, @intCast(value))) != null) {
            break :blk @as(usize, @intCast(value));
        }
        break :blk null;
    } else |_| blk: {
        var id: usize = 0;
        while (id < process.max_processes) : (id += 1) {
            const info = process.info(id) orelse continue;
            if (std.mem.eql(u8, info.name, arg)) break :blk id;
        }
        break :blk null;
    };
    const pid_value = pid orelse {
        err_prefix(m);
        m.console.puts("no such process: ");
        m.console.puts(arg);
        m.console.puts("\n");
        return .invalid_argument;
    };
    const info = process.info(pid_value).?;
    if (info.state == .exited) {
        err_prefix(m);
        m.console.puts(info.name);
        m.console.puts(" already exited\n");
        return .invalid_argument;
    }
    // A created-but-unbound process (exec's pre-spawn window) has no
    // executor to terminate; a running process names its executor slot.
    const task_id = info.task_id orelse {
        err_prefix(m);
        m.console.puts(info.name);
        m.console.puts(" not running\n");
        return .invalid_argument;
    };
    switch (scheduler.request_kill(task_id)) {
        .ok => {
            // The transport locks each write, not the whole command.
            // Process names are bounded by name_max; include the newline
            // in the same write so another core cannot split this receipt.
            var buf: ["kill: ".len + process.name_max + " armed\n".len]u8 = undefined;
            m.console.puts(std.fmt.bufPrint(&buf, "kill: {s} armed\n", .{info.name}) catch return .not_implemented);
            return .none;
        },
        .not_found => {
            err_prefix(m);
            m.console.puts(info.name);
            m.console.puts(" not found\n");
            return .invalid_argument;
        },
        .already_exited => {
            err_prefix(m);
            m.console.puts(info.name);
            m.console.puts(" already exited\n");
            return .invalid_argument;
        },
        .refused => {
            err_prefix(m);
            m.console.print_line("cannot kill the shell or scheduler-owned idle task");
            return .invalid_argument;
        },
    }
}

// ---------------------------------------------------------------------------
// IPC mailbox command (milestone-four follow-on 3, card 3f — claim 5965)
// ---------------------------------------------------------------------------

/// `mbox [<pid>]` — dump the per-process IPC mailboxes: for every live
/// process (or just the one named by `pid`), the pending message count
/// and the enqueue/dequeue counters, then the queued bytes themselves.
/// The counters are the draining proof: a process whose ring is drained
/// has `recv` tracking `sent` and `pending` back at 0. Deterministic and
/// grep-able (the live IPC gate greps `mbox: id=` and `pending=` rows).
pub fn cmd_mbox(m: *Monitor, args: []const []const u8) ExecError {
    const wanted: ?usize = if (args.len > 0) blk: {
        const value = parseInt(args[0]) catch {
            err_prefix(m);
            m.console.puts("mbox: invalid pid: ");
            m.console.puts(args[0]);
            m.console.puts("\n");
            return .invalid_argument;
        };
        if (value >= process.max_processes or process.info(@as(usize, @intCast(value))) == null) {
            err_prefix(m);
            m.console.puts("mbox: no such process: ");
            m.console.puts(args[0]);
            m.console.puts("\n");
            return .invalid_argument;
        }
        break :blk @as(usize, @intCast(value));
    } else null;
    var id: usize = 0;
    while (id < process.max_processes) : (id += 1) {
        const info = process.info(id) orelse continue;
        if (wanted) |w| if (w != id) continue;
        const mbox_info = mailbox.info(id);
        m.console.puts("mbox: id=");
        m.console.print_u64(@intCast(id));
        m.console.puts(" name=");
        m.console.puts(info.name);
        m.console.puts(" pending=");
        m.console.print_u64(@intCast(mbox_info.pending));
        m.console.puts(" sent=");
        m.console.print_u64(mbox_info.sent);
        m.console.puts(" recv=");
        m.console.print_u64(mbox_info.recv);
        m.console.puts("\n");
        // Raw dump of the queued bytes (messages are text here; the ring
        // bound caps the total at 8 × 64 B per process).
        var index: usize = 0;
        while (mailbox.message(id, index)) |bytes| : (index += 1) {
            m.console.puts("mbox:   ");
            m.console.print_u64(@intCast(index));
            m.console.puts(": ");
            m.console.puts(bytes);
            m.console.puts("\n");
        }
    }
    return .none;
}

/// M22 D6 (issue #329): `ps` — the process status table. One row per
/// non-free registry entry: PID, name, state, memory footprint (text +
/// data + stack + kernel-stack pages), CPU ticks (via the executor task
/// when bound), and the task id. `procs` remains the verbose view; this is
/// the scannable table.
pub fn cmd_ps(m: *Monitor, args: []const []const u8) ExecError {
    _ = args;
    m.console.puts("  PID  NAME                 STATE     MEM       CPU  TASK\n");
    var id: usize = 0;
    var rows: usize = 0;
    while (id < process.max_processes) : (id += 1) {
        const info = process.info(id) orelse continue;
        if (info.state == .free) continue;
        rows += 1;
        var line: [96]u8 = undefined;
        var pos: usize = 0;
        pos = pad_left(&line, pos, id, 5);
        pos = append_str(&line, pos, "  ");
        pos = pad_right(&line, pos, info.name, 20);
        pos = append_str(&line, pos, " ");
        pos = pad_right(&line, pos, @tagName(info.state), 9);
        pos = append_str(&line, pos, " ");
        // Memory: owned pages only (boot payload reports its static stack).
        const mem_kib = ((info.text_pages + info.data_pages + info.stack_pages + info.kernel_stack_pages) * 4);
        pos = pad_left(&line, pos, mem_kib, 5);
        line[pos] = 'K';
        pos += 1;
        pos = append_str(&line, pos, "   ");
        pos = pad_left_u64(&line, pos, info.cpu_usage, 6);
        pos = append_str(&line, pos, "  ");
        if (info.task_id) |tid| {
            pos = pad_left(&line, pos, tid, 4);
        } else {
            pos = append_str(&line, pos, "   -");
        }
        if (pos < line.len) {
            line[pos] = '\n';
            m.console.write(line[0 .. pos + 1]);
        } else {
            m.console.write(line[0..pos]);
            m.console.print_line("");
        }
    }
    if (rows == 0) m.console.print_line("ps: no processes");
    return .none;
}

fn append_str(buf: []u8, pos: usize, src: []const u8) usize {
    const take = @min(src.len, buf.len - pos);
    @memcpy(buf[pos..][0..take], src[0..take]);
    return pos + take;
}

fn pad_left(buf: []u8, pos: usize, v: usize, width: usize) usize {
    var tmp: [20]u8 = undefined;
    var n: usize = 0;
    var v2 = v;
    if (v2 == 0) {
        tmp[0] = '0';
        n = 1;
    }
    while (v2 > 0) : (v2 /= 10) {
        tmp[n] = @intCast('0' + v2 % 10);
        n += 1;
    }
    var written: usize = 0;
    while (n + written < width) : (written += 1) buf[pos + written] = ' ';
    var i: usize = 0;
    while (i < n) : (i += 1) buf[pos + written + i] = tmp[n - 1 - i];
    return pos + written + n;
}

fn pad_left_u64(buf: []u8, pos: usize, v_in: u64, width: usize) usize {
    var tmp: [20]u8 = undefined;
    var n: usize = 0;
    var v = v_in;
    if (v == 0) {
        tmp[0] = '0';
        n = 1;
    }
    while (v > 0) : (v /= 10) {
        tmp[n] = @intCast('0' + v % 10);
        n += 1;
    }
    var written: usize = 0;
    while (n + written < width) : (written += 1) buf[pos + written] = ' ';
    var i: usize = 0;
    while (i < n) : (i += 1) buf[pos + written + i] = tmp[n - 1 - i];
    return pos + written + n;
}

fn pad_right(buf: []u8, pos: usize, src: []const u8, width: usize) usize {
    const take = @min(src.len, width);
    @memcpy(buf[pos..][0..take], src[0..take]);
    var written = take;
    while (written < width) : (written += 1) buf[pos + written] = ' ';
    return pos + written;
}

pub fn cmd_syscalls(m: *Monitor, args: []const []const u8) ExecError {
    _ = args;
    syscall.report(&m.console);
    return .none;
}

// ---------------------------------------------------------------------------
// Per-task address-space command (claim 5804)
// ---------------------------------------------------------------------------

/// Report the address-space split (claim 5804, VZ fallback): TTBR1 must
/// be 0 (unused — the kernel is identity-mapped in TTBR0), every task's
/// TTBR0 root, and the user root's leaf inventory. The user root is the
/// identity clone + user leaves: `el0` leaves must be EXACTLY the
/// text+stack leaves, and `el0_device` must be 0 (MMIO excluded from EL0
/// by the EL1-only AP bits on the overlay's Device leaves). Deterministic
/// and grep-able; on host test processes the roots are never built, so it
/// honestly prints zeros for the register/root values and 0 leaves.
pub fn cmd_addrspaces(m: *Monitor, args: []const []const u8) ExecError {
    _ = args;
    var buf: [256]u8 = undefined;
    m.console.puts(std.fmt.bufPrint(&buf, "addrspaces: ttbr1=0x{x:0>16} root=0x{x:0>16} tcr=0x{x:0>16} t0sz=16\n", .{
        mmu.read_ttbr1(), mmu.kernel_root_phys(), mmu.read_tcr(),
    }) catch return .not_implemented);
    var i: usize = 0;
    while (i < scheduler.max_tasks) : (i += 1) {
        const info = scheduler.task_info(i) orelse continue;
        m.console.puts(std.fmt.bufPrint(&buf, "addrspaces: task {s} ttbr0=0x{x:0>16}\n", .{ info.name, scheduler.task_ttbr0(i) }) catch return .not_implemented);
    }
    // Claim 6729: the user task's root is a fixed MMU fact (built at boot),
    // not a task-table fact — the lifecycle's idle task reaps the exited
    // user task, so the `task user-el0` row above may legitimately be gone
    // by the time this command runs. Report the root directly so the
    // ownership assertion (user root != kernel root) survives the reap.
    m.console.puts(std.fmt.bufPrint(&buf, "addrspaces: user root=0x{x:0>16}\n", .{mmu.user_root_phys()}) catch return .not_implemented);
    // Claim 0826: the per-process-root budget — table pages consumed out of
    // the fixed 512-page carve-out (grown by claim 2714 for the M16
    // composition). Card 3g (claim 5795): FOUR live user roots (~15 each +
    // leaf tables) stay well inside it; the scale live gate reads this line
    // for the headroom assertion.
    m.console.puts(std.fmt.bufPrint(&buf, "addrspaces: tables={d}/{d}\n", .{ mmu.tables_used(), mmu.tables_capacity() }) catch return .not_implemented);
    const leaves = mmu.walk_leaves(mmu.user_root_phys());
    m.console.puts(std.fmt.bufPrint(&buf, "addrspaces: user text=0x{x:0>16} stack=0x{x:0>16} leaves={d} device={d} el0={d} el0_device={d}\n", .{
        userspace.text_va, userspace.user_stack_va(), leaves.leaves, leaves.device_leaves, leaves.el0_leaves, leaves.el0_device_leaves,
    }) catch return .not_implemented);
    return .none;
}

// ---------------------------------------------------------------------------
// Resources command (milestone sixteen C3, claim 0339)
// ---------------------------------------------------------------------------

/// The fixed-pool audit: one line per bounded kernel pool with its live
/// occupancy vs its comptime bound. This is the C3 "measure, then grow
/// only what the apps exhaust" evidence — the live gate reads these lines
/// BEFORE filling the pool and AFTER, so the growth is pinned in the serial
/// log rather than asserted from code alone. The pools left bounded
/// (windows, mailbox rings, event queues, file handles, app timers, the
/// single TCP client) are printed with their bounds so the audit records
/// that they were checked and NOT grown (no demo app exhausts them).
pub fn cmd_resources(m: *Monitor, args: []const []const u8) ExecError {
    _ = args;
    const s = scheduler.stats();
    var buf: [256]u8 = undefined;
    m.console.puts(std.fmt.bufPrint(&buf, "resources: tasks={d}/{d} zombies={d}\n", .{ s.count, scheduler.max_tasks, s.zombies }) catch return .not_implemented);
    m.console.puts(std.fmt.bufPrint(&buf, "resources: procs={d}/{d}\n", .{ process.count(), process.max_processes }) catch return .not_implemented);
    m.console.puts(std.fmt.bufPrint(&buf, "resources: windows={d}/{d}\n", .{ driving_award.count(), driving_award.max_windows }) catch return .not_implemented);
    m.console.puts(std.fmt.bufPrint(&buf, "resources: tables={d}/{d}\n", .{ mmu.tables_used(), mmu.tables_capacity() }) catch return .not_implemented);
    m.console.puts(std.fmt.bufPrint(&buf, "resources: events={d} mbox={d} fds={d} timers=1 tcp=1\n", .{ events.max_events, mailbox.max_messages, file_table.max_handles_per_process }) catch return .not_implemented);
    // Arc5 issue #246: per-process resource usage vs limits
    var id: usize = 0;
    while (id < process.max_processes) : (id += 1) {
        if (process.getrusage(id)) |ru| {
            if (ru.mem_limit != 0 or ru.cpu_limit != 0 or ru.mem_usage != 0 or ru.cpu_usage != 0) {
                const info = process.info(id).?;
                m.console.puts(std.fmt.bufPrint(&buf, "resources: pid={d} {s}\n", .{ id, info.name }) catch return .not_implemented);
                var mem_buf: [20]u8 = undefined;
                var cpu_buf: [20]u8 = undefined;
                const mem_limit = if (ru.mem_limit == 0) "unlimited" else std.fmt.bufPrint(&mem_buf, "{d}", .{ru.mem_limit}) catch return .not_implemented;
                const cpu_limit = if (ru.cpu_limit == 0) "unlimited" else std.fmt.bufPrint(&cpu_buf, "{d}", .{ru.cpu_limit}) catch return .not_implemented;
                m.console.puts(std.fmt.bufPrint(&buf, "  mem={d}/{s} cpu={d}/{s}\n", .{ ru.mem_usage, mem_limit, ru.cpu_usage, cpu_limit }) catch return .not_implemented);
            }
        }
    }
    return .none;
}

// ---------------------------------------------------------------------------
// Uaccess diagnostic command (claim 6120)
// ---------------------------------------------------------------------------

/// Prove both uaccess paths on live hardware: the validated copy from the
/// user text aperture succeeds, and a raw copy from an unmapped address
/// above the identity blanket takes a REAL EL1 data abort that the
/// exception path recovers into EFAULT (recoveries >= 1). Honest on host
/// test processes (no vectors): `recovered=0` there. Gates on the real
/// recovery count, so the class-B gate can assert `recovered=1` exactly
/// when it drives this command once.
pub fn cmd_uaccess(m: *Monitor, args: []const []const u8) ExecError {
    _ = args;
    const d = uaccess.diag();
    m.console.puts("uaccess: valid=");
    m.console.print_u64(if (d.valid_copy) 1 else 0);
    m.console.puts(" fault=");
    m.console.print_u64(if (d.fault_copy) 1 else 0);
    m.console.puts(" recovered=");
    m.console.print_u64(d.recoveries);
    m.console.puts(" copies=");
    m.console.print_u64(d.copies);
    m.console.puts(" validation_faults=");
    m.console.print_u64(d.validation_faults);
    // Issue #1391: kernel->user copies refused because a destination page
    // was not the process's to write. Always 0 in a healthy boot; a
    // non-zero count means a store would have been swallowed by the
    // EL1-only identity overlay (or a caller passed a foreign page).
    m.console.puts(" unbacked=");
    m.console.print_u64(d.unbacked);
    m.console.puts("\n");
    return .none;
}
