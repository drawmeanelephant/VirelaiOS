//! ADR 0043: bounded uid-owned tracing. Service locks precede trace_lock;
//! capture and copy-out happen outside trace_lock. The legacy monitor path
//! remains synchronous and suppresses the two never-logged slots entirely.
const std = @import("std");
const builtin = @import("builtin");
const abi = @import("syscall_abi.zig");
const alloc = @import("alloc.zig");
const exec = @import("exec.zig");
const exceptions = @import("exceptions.zig");
const csprng = @import("csprng.zig"); // M97g (#2086): the session-token mint
const process = @import("process.zig");
const scheduler = @import("scheduler.zig");
const spinlock = @import("spinlock.zig");
const svclock = @import("svclock.zig");
const timer = @import("timer.zig");
const uaccess = @import("uaccess.zig");
const virtio_entropy = @import("virtio_entropy.zig"); // M97g (#2086): lazy reseed behind an unseeded mint
const virtio_file = @import("virtio_file.zig");

pub const slot = abi.number("sys_trace");
pub const implemented = true;
pub const version = 1;
pub const max_pids = 8;
pub const ring_records = 256;
pub const string_bytes = 128;
pub const max_read_records = 16;
pub const op_arm = 0;
pub const op_disarm = 1;
pub const op_filter = 2;
pub const op_read = 3;
pub const op_status = 4;
pub const op_arm_exec = 5;
pub const flag_redacted = 1;
pub const flag_no_return = 2;

pub const Config = extern struct {
    version: u32,
    pid_count: u32,
    pids: [max_pids]u64,
    slots: [2]u64,
};
pub const ExecRequest = extern struct {
    path_ptr: u64,
    path_len: u64,
    argv_ptr: u64,
    argc: u64,
};
pub const ReadHeader = extern struct {
    version: u32,
    record_bytes: u32,
    count: u32,
    reserved: u32,
    dropped: u64,
};
pub const Record = extern struct {
    version: u32,
    flags: u32,
    pid: u64,
    tid: u64,
    cntpct: u64,
    number: u64,
    args: [6]u64,
    result: i64,
    errno: u32,
    string_mask: u32,
    fault_mask: u32,
    truncated_mask: u32,
    string_lengths: [6]u16,
    reserved: u32,
    strings: [6][string_bytes]u8,
};

const Backing = extern struct {
    records: [ring_records]Record,
    uids: [ring_records]u32,
    unused: [3072]u8,
};
pub const backing_pages = 57;
comptime {
    if (@sizeOf(Backing) != backing_pages * 4096) @compileError("trace backing exceeds ADR 0043");
}

/// Sequence numbers are private, wrapping counters. At most 256 entries are
/// live, so wrapping subtraction remains unambiguous across counter rollover.
pub const Ring = struct {
    backing: *Backing,
    first: u64 = 0,
    next: u64 = 0,
    dropped: u64 = 0,

    pub fn count(self: *const Ring) usize {
        return @intCast(self.next -% self.first);
    }

    pub fn publish(self: *Ring, record: Record, uid: u32) void {
        if (self.count() == ring_records) {
            self.first +%= 1;
            self.dropped +|= 1;
        }
        const index: usize = @intCast(self.next % ring_records);
        self.backing.records[index] = record;
        self.backing.uids[index] = uid;
        self.next +%= 1;
    }

    /// Commit only the staged prefix still present. Concurrent overwrite has
    /// already advanced first; never consume records published after staging.
    pub fn consume(self: *Ring, staged_first: u64, staged_count: usize) void {
        const advanced = self.first -% staged_first;
        if (advanced < staged_count) self.first +%= staged_count - advanced;
    }
};

var trace_lock = spinlock.IrqSaveSpinlock{};
var ring: ?Ring = null;
var session_token: u64 = 0;
var owner_pid: usize = 0;
var owner_uid: u32 = 0;
var owner_admin: bool = false;
var active = false;
var filter: Config = std.mem.zeroes(Config);
// Only this fast rejection is consulted before touching the trace lock or
// reading CNTPCT/user strings. Publish changes after updating the locked state.
var fast_slots = [_]std.atomic.Value(u64){
    std.atomic.Value(u64).init(0), std.atomic.Value(u64).init(0),
};

fn fail(code: i64) u64 {
    return @bitCast(-code);
}

pub fn authorized(caller: process.Principal, uid: u32) bool {
    return caller.uid == uid or caller.has(process.cap_proc_admin);
}

/// M97g (#2086): session-CONTROL ownership. Only the arming pid (or a
/// CAP_PROC_ADMIN holder) may filter/read/disarm/replace the session —
/// uid equality is deliberately insufficient because every ordinary EL0
/// process is `uid_user`, so a uid check let any app enumerate the
/// sequential tokens and read or kill a peer's session.
fn session_owner(pid: usize, caller: process.Principal) bool {
    return pid == owner_pid or caller.has(process.cap_proc_admin);
}

/// A session whose arming process has exited is releasable: pids are
/// recycled only after the slot is cleared, so a null principal means the
/// owner is gone — otherwise a stale session would brick tracing for every
/// other uid_user app (there is no exit hook to run a cleanup under).
fn owner_retained() bool {
    return process.principal(owner_pid) != null;
}

/// M97g (#2086): mint the session token from the CSPRNG, not a counter —
/// a token must not be derivable from previously observed tokens. An
/// unseeded stream means the bytes are the deterministic boot fallback,
/// which IS derivable, so the mint fails closed after attempting the
/// shared lazy reseed. The mint masks bit 63: the slot ABI reports errors
/// as negative results, so a token with the high bit set would read as an
/// errno to every caller. Returns null on failure.
fn mint_token() ?u64 {
    if (!csprng.seeded()) {
        var seed_buf: [csprng.seed_len]u8 align(16) = undefined;
        if (!virtio_entropy.read_seed(&seed_buf)) return null;
        csprng.seed(&seed_buf);
    }
    var token = csprng.random_u64() & 0x7fff_ffff_ffff_ffff;
    while (token == 0 or token == session_token) token = csprng.random_u64() & 0x7fff_ffff_ffff_ffff;
    return token;
}

pub fn selected(config: *const Config, pid: u64, number: u64) bool {
    if (number >= abi.slot_count or config.pid_count > max_pids) return false;
    const word: usize = @intCast(number / 64);
    const bit: u6 = @intCast(number % 64);
    if (config.slots[word] & (@as(u64, 1) << bit) == 0) return false;
    for (config.pids[0..config.pid_count]) |target| if (target == pid) return true;
    return false;
}

fn validate_config(config: *const Config, caller: process.Principal) u64 {
    if (config.version != version or config.pid_count > max_pids) return fail(1);
    for (config.pids, 0..) |pid, index| {
        if (index >= config.pid_count) {
            if (pid != 0) return fail(1);
            continue;
        }
        if (pid >= process.max_processes) return fail(1);
        const principal = process.principal(@intCast(pid)) orelse return fail(1);
        if (!authorized(caller, principal.uid)) return fail(7);
        for (config.pids[0..index]) |previous| if (previous == pid) return fail(1);
    }
    return 0;
}

fn allocate_backing() ?*Backing {
    if (comptime builtin.is_test) return std.heap.page_allocator.create(Backing) catch null;
    const address = alloc.alloc_pages(backing_pages) orelse return null;
    return @ptrFromInt(address);
}

fn release_backing(backing: *Backing) void {
    if (comptime builtin.is_test) {
        std.heap.page_allocator.destroy(backing);
    } else {
        _ = alloc.free_pages(@intFromPtr(backing), backing_pages);
    }
}

fn publish_fast_slots() void {
    for (&fast_slots, 0..) |*word, index| word.store(if (active) filter.slots[index] else 0, .release);
}

fn check_session(token: u64, pid: usize, caller: process.Principal) u64 {
    if (session_token == 0 or token != session_token) return fail(2);
    if (!session_owner(pid, caller)) return fail(7);
    return 0;
}

pub fn handle(args: [6]u64, _: *exceptions.VectorFrame) u64 {
    // Hold FILE + KERNEL for the entire control operation, including
    // copy-out and the pinned spawn. Do not rely on the dispatch wrapper's
    // lock lifetime; direct callers must receive the same guarantee.
    const taken = svclock.acquire_missing(svclock.dom_bit(.file) | svclock.dom_bit(.kernel));
    defer svclock.release_set(taken);
    const pid = process.find_by_task(scheduler.current_id()) orelse return fail(1);
    const caller = process.principal(pid) orelse return fail(1);
    const op = args[0];
    if (op > op_arm_exec) return fail(1);
    if (op == op_arm or op == op_filter) {
        if (args[3] != @sizeOf(Config) or (op == op_arm and args[1] != 0)) return fail(1);
        var replacement: Config = undefined;
        // Reject an unauthorized replacement before copying its user input.
        {
            const saved = trace_lock.lock();
            defer trace_lock.unlock(saved);
            if (op == op_filter) {
                const checked = check_session(args[1], pid, caller);
                if (checked != 0) return checked;
            } else if (session_token != 0 and !session_owner(pid, caller) and owner_retained()) return fail(7);
        }
        if (uaccess.copy_in(std.mem.asBytes(&replacement), args[2], @sizeOf(Config)) != .ok) return fail(3);
        const checked = validate_config(&replacement, caller);
        if (checked != 0) return checked;
        const saved = trace_lock.lock();
        defer trace_lock.unlock(saved);
        if (op == op_filter) {
            const session_checked = check_session(args[1], pid, caller);
            if (session_checked != 0) return session_checked;
            filter = replacement;
            publish_fast_slots();
            return 0;
        }
        if (session_token != 0 and !session_owner(pid, caller) and owner_retained()) return fail(7);
        // M97g (#2086): the token is a CSPRNG mint — EAGAIN when no real
        // entropy is available rather than a derivable session handle.
        const token = mint_token() orelse return fail(11);
        // Reuse the fixed reservation on replacement. There is never a second
        // 57-page allocation transiently exceeding the frozen ring budget.
        const backing = if (ring) |existing| existing.backing else allocate_backing() orelse return fail(10);
        @memset(std.mem.asBytes(backing), 0);
        ring = .{ .backing = backing };
        session_token = token;
        owner_pid = pid;
        owner_uid = caller.uid;
        owner_admin = caller.has(process.cap_proc_admin);
        filter = replacement;
        active = true;
        publish_fast_slots();
        return session_token;
    }
    {
        const saved = trace_lock.lock();
        defer trace_lock.unlock(saved);
        const checked = check_session(args[1], pid, caller);
        if (checked != 0) return checked;
        if (op == op_disarm) {
            if (args[2] != 0 or args[3] != 0) return fail(1);
            active = false;
            publish_fast_slots();
            return 0;
        }
        if (op == op_arm_exec and (!active or filter.pid_count == max_pids)) return fail(if (active) 5 else 1);
    }
    if (op == op_arm_exec) return arm_exec(args, caller);
    return read_records(args, pid, caller, op == op_status);
}

fn arm_exec(args: [6]u64, caller: process.Principal) u64 {
    if (args[3] != @sizeOf(ExecRequest)) return fail(1);
    var request: ExecRequest = undefined;
    if (uaccess.copy_in(std.mem.asBytes(&request), args[2], @sizeOf(ExecRequest)) != .ok) return fail(3);
    if (request.path_len == 0 or request.path_len > virtio_file.path_max or request.argc > exec.max_exec_args) return fail(1);
    var path: [virtio_file.path_max]u8 = undefined;
    if (uaccess.copy_in(&path, request.path_ptr, @intCast(request.path_len)) != .ok) return fail(3);
    var argv: [exec.arg_block_bytes]u8 = undefined;
    var slices: [exec.max_exec_args][]const u8 = undefined;
    const argc: usize = @intCast(request.argc);
    if (argc != 0 and uaccess.copy_in(&argv, request.argv_ptr, argc * exec.arg_slot_bytes) != .ok) return fail(3);
    for (slices[0..argc], 0..) |*slice, i| {
        const word = argv[i * exec.arg_slot_bytes ..][0..exec.arg_slot_bytes];
        const end = std.mem.indexOfScalar(u8, word, 0) orelse return fail(1);
        slice.* = word[0..end];
    }
    // FILE/KERNEL are held with local IRQs masked. Pinned primary cannot run
    // on another core, and cannot create sibling threads until after arming.
    const result = exec.exec_file_pinned_as(path[0..request.path_len], slices[0..argc], svclock.core_id(), caller);
    if (result != .ok) return switch (result) {
        .not_found => fail(6),
        .out_of_memory => fail(10),
        .pool_full, .table_full, .process_full => fail(5),
        else => fail(1),
    };
    const child = exec.last_exec_pid() orelse return fail(1);
    const saved = trace_lock.lock();
    defer trace_lock.unlock(saved);
    // Control calls cannot interleave: their common FILE/KERNEL locks are
    // still held. No failure point is allowed after successfully spawning.
    filter.pids[filter.pid_count] = child;
    filter.pid_count += 1;
    return child;
}

fn read_records(args: [6]u64, pid: usize, caller: process.Principal, status_only: bool) u64 {
    if (args[3] < @sizeOf(ReadHeader) or (status_only and args[3] != @sizeOf(ReadHeader))) return fail(1);
    var staged: [@sizeOf(ReadHeader) + max_read_records * @sizeOf(Record)]u8 align(8) = undefined;
    var header = ReadHeader{ .version = version, .record_bytes = @sizeOf(Record), .count = 0, .reserved = 0, .dropped = 0 };
    var first: u64 = 0;
    {
        const saved = trace_lock.lock();
        defer trace_lock.unlock(saved);
        const checked = check_session(args[1], pid, caller);
        if (checked != 0) return checked;
        const source = &ring.?;
        first = source.first;
        const available = source.count();
        // STATUS also checks all sidecars, so the unread count does not expose
        // a foreign target's state to a same-uid but non-admin observer.
        const take = if (status_only) available else @min(available, @min(max_read_records, (args[3] - @sizeOf(ReadHeader)) / @sizeOf(Record)));
        for (0..take) |i| {
            const index: usize = @intCast((first +% i) % ring_records);
            if (!authorized(caller, source.backing.uids[index])) return fail(7);
            if (!status_only) {
                const offset = @sizeOf(ReadHeader) + i * @sizeOf(Record);
                @memcpy(staged[offset..][0..@sizeOf(Record)], std.mem.asBytes(&source.backing.records[index]));
            }
        }
        header.count = @intCast(take);
        header.dropped = source.dropped;
    }
    @memcpy(staged[0..@sizeOf(ReadHeader)], std.mem.asBytes(&header));
    const size = @sizeOf(ReadHeader) + if (status_only) @as(usize, 0) else @as(usize, header.count) * @sizeOf(Record);
    if (uaccess.copy_out(args[2], staged[0..size], size) != .ok) return fail(3);
    if (status_only) return 0;
    {
        const saved = trace_lock.lock();
        defer trace_lock.unlock(saved);
        // Session replacement cannot interleave under dispatch's FILE/KERNEL,
        // but producers may overwrite. Commit only the still-present prefix.
        if (session_token == args[1]) ring.?.consume(first, header.count);
    }
    return header.count;
}

/// Capture every wire field from the entering task, not a staged successor.
/// The caller has already authorized the target. No binary/output pointer
/// enters uaccess, and redaction precedes even the argument assignment.
pub fn capture(number: u64, args: [6]u64, pid: u64, tid: u64, counter: u64) Record {
    var record = std.mem.zeroes(Record);
    record.version = version;
    record.pid = pid;
    record.tid = tid;
    record.cntpct = counter;
    record.number = number;
    if (abi.redacted(number)) {
        record.flags = flag_redacted;
        return record;
    }
    record.args = args;
    if (number == abi.number("sys_exit") or (number == abi.number("sys_thread") and args[0] == 1)) record.flags = flag_no_return;
    const shape = abi.shape(number, args) orelse return record;
    for (shape.args[0..shape.arg_count], 0..) |arg, i| {
        if (arg.kind != .string) continue;
        const bit = @as(u32, 1) << @as(u5, @intCast(i));
        record.string_mask |= bit;
        const length = args[arg.length_arg] & arg.length_mask;
        const take: usize = @intCast(@min(length, string_bytes));
        if (uaccess.copy_in(&record.strings[i], args[i], take) != .ok) {
            record.fault_mask |= bit;
            @memset(&record.strings[i], 0);
            continue;
        }
        record.string_lengths[i] = @intCast(take);
        if (length > string_bytes) record.truncated_mask |= bit;
    }
    return record;
}

pub fn complete(record: *Record, result: u64) void {
    if (record.flags & (flag_redacted | flag_no_return) != 0) return;
    record.result = @bitCast(result);
    if (record.result < 0) {
        const magnitude = @as(u64, 0) -% result;
        record.errno = @intCast(@min(magnitude, std.math.maxInt(u32)));
    }
}

/// Host tests alone may release the session. Production replacement retains
/// the one fixed reservation, even after its observer dies.
pub fn reset_for_test() void {
    if (!builtin.is_test) @compileError("test-only trace reset");
    if (ring) |existing| release_backing(existing.backing);
    ring = null;
    session_token = 0;
    owner_pid = 0;
    owner_uid = 0;
    owner_admin = false;
    active = false;
    filter = std.mem.zeroes(Config);
    publish_fast_slots();
}

const Writer = *const fn ([]const u8) void;
var write_fn: ?Writer = null;
// syscall.strace_pid stays the monitor's assignable compatibility cell.
// The implementation lives here; no monitor.zig change or second copy.
var strace_control: ?*?usize = null;

pub fn init(writer: Writer, control: *?usize) void {
    write_fn = writer;
    strace_control = control;
}

fn target_pid() ?usize {
    const control = strace_control orelse return null;
    return control.*;
}

// M94b may extend this per-call value without editing dispatch. Its before
// hook receives all six arguments and the original vector frame; after gets
// the same value even when a scheduling handler stages a different task.
pub const Call = struct {
    token: u64 = 0,
    uid: u32 = 0,
    record: Record = undefined,
};

pub fn before(number: u64, args: [6]u64, _: *exceptions.VectorFrame) Call {
    // The legacy exit line precedes the handler, byte-for-byte.
    if (tracing_current() and !trace_excluded(number)) {
        if (number == abi.number("sys_exit")) {
            var buf: [96]u8 = undefined;
            var pos: usize = append_trace_str(buf[0..], "[strace ");
            pos += append_trace_dec(buf[pos..], @intCast(traced_pid_for_current()));
            pos += append_trace_str(buf[pos..], "] sys_exit(");
            pos += append_trace_hex(buf[pos..], args[0]);
            pos += append_trace_str(buf[pos..], ") = \xe2\x80\x94\n");
            if (write_fn) |wp| wp(buf[0..pos]);
        }
    }
    if (number >= abi.slot_count or fast_slots[@intCast(number / 64)].load(.acquire) & (@as(u64, 1) << @as(u6, @intCast(number % 64))) == 0) return .{};
    const taken = svclock.acquire_missing(svclock.dom_bit(.kernel));
    defer svclock.release_set(taken);
    const tid = scheduler.current_id();
    const pid = process.find_by_task(tid) orelse return .{};
    const principal = process.principal(pid) orelse return .{};
    var token: u64 = 0;
    {
        const saved = trace_lock.lock();
        defer trace_lock.unlock(saved);
        if (!active or !selected(&filter, pid, number)) return .{};
        if (principal.uid != owner_uid and !owner_admin) return .{};
        token = session_token;
    }
    var call = Call{ .token = token, .uid = principal.uid, .record = capture(number, args, pid, tid, timer.cntpct()) };
    if (call.record.flags & flag_no_return != 0) {
        publish_call(call);
        call.token = 0;
    }
    return call;
}

pub fn after(call: Call, number: u64, args: [6]u64, result: u64) void {
    if (!trace_excluded(number)) maybe_trace(number, args, result);
    if (call.token == 0) return;
    var finished = call;
    complete(&finished.record, result);
    publish_call(finished);
}

fn publish_call(call: Call) void {
    const taken = svclock.acquire_missing(svclock.dom_bit(.kernel));
    defer svclock.release_set(taken);
    const principal = process.principal(@intCast(call.record.pid)) orelse return;
    if (principal.uid != call.uid) return;
    const saved = trace_lock.lock();
    defer trace_lock.unlock(saved);
    if (!active or session_token != call.token or !selected(&filter, call.record.pid, call.record.number)) return;
    if (call.uid != owner_uid and !owner_admin) return;
    ring.?.publish(call.record, call.uid);
}

fn trace_excluded(number: u64) bool {
    return switch (number) {
        70, 71 => true,
        else => false,
    };
}

fn tracing_current() bool {
    const target = target_pid() orelse return false;
    const pid = process.find_by_task(scheduler.current_id()) orelse return false;
    return pid == target;
}

fn traced_pid_for_current() usize {
    return process.find_by_task(scheduler.current_id()) orelse 0;
}

fn maybe_trace(number: u64, args: [6]u64, result: u64) void {
    const target = target_pid() orelse return;
    const w = write_fn orelse return;
    const pid = process.find_by_task(scheduler.current_id()) orelse return;
    if (pid != target) return;
    var buf: [112]u8 = undefined;
    var pos: usize = 0;
    pos += append_trace_str(buf[pos..], "[strace ");
    pos += append_trace_dec(buf[pos..], @intCast(pid));
    pos += append_trace_str(buf[pos..], "] ");
    pos += append_trace_str(buf[pos..], abi.lookup(number).?.name());
    pos += append_trace_str(buf[pos..], "(");
    pos += append_trace_hex(buf[pos..], args[0]);
    pos += append_trace_str(buf[pos..], ", ");
    pos += append_trace_hex(buf[pos..], args[1]);
    pos += append_trace_str(buf[pos..], ", ");
    pos += append_trace_hex(buf[pos..], args[2]);
    pos += append_trace_str(buf[pos..], ") = ");
    pos += append_trace_hex(buf[pos..], result);
    if (pos < buf.len) {
        buf[pos] = '\n';
        pos += 1;
    }
    w(buf[0..pos]);
}

fn append_trace_str(buf: []u8, s: []const u8) usize {
    const take = @min(s.len, buf.len);
    @memcpy(buf[0..take], s[0..take]);
    return take;
}

fn append_trace_dec(buf: []u8, v_in: u64) usize {
    var v = v_in;
    var tmp: [20]u8 = undefined;
    var n: usize = 0;
    if (v == 0) {
        tmp[0] = '0';
        n = 1;
    }
    while (v > 0) : (v /= 10) {
        tmp[n] = @intCast('0' + v % 10);
        n += 1;
    }
    var i: usize = 0;
    while (i < n) : (i += 1) buf[i] = tmp[n - 1 - i];
    return n;
}

fn append_trace_hex(buf: []u8, v: u64) usize {
    const digits = "0123456789abcdef";
    buf[0] = '0';
    buf[1] = 'x';
    if (v == 0) {
        buf[2] = '0';
        return 3;
    }
    var tmp: [16]u8 = undefined;
    var n: usize = 0;
    var vv = v;
    while (vv > 0) : (vv /= 16) {
        tmp[n] = digits[@intCast(vv % 16)];
        n += 1;
    }
    var i: usize = 0;
    while (i < n) : (i += 1) buf[2 + i] = tmp[n - 1 - i];
    return 2 + n;
}
