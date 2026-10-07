//! ADR 0043: bounded, IRQ-only EL0 CPU samples. No service lock in an IRQ.
const std = @import("std");
const builtin = @import("builtin");
const abi = @import("syscall_abi.zig");
const exceptions = @import("exceptions.zig");
const process = @import("process.zig");
const scheduler = @import("scheduler.zig");
const uaccess = @import("uaccess.zig");
const mmu = @import("mmu.zig");
const alloc = @import("alloc.zig");
const spinlock = @import("spinlock.zig");
const timer = @import("timer.zig");
const svclock = @import("svclock.zig");

pub const slot = abi.number("sys_profile");
pub const implemented = true;
pub const version = 1;
pub const max_pids = 8;
pub const frame_depth = 16;
pub const ring_records = 1024;
pub const max_read_records = 16;
pub const rate_hz = 100;
pub const virtual_timer_ppi = 27;
pub const op_arm = 0;
pub const op_disarm = 1;
pub const op_read = 3;
pub const op_status = 4;
pub const Config = extern struct {
    version: u32,
    pid_count: u32,
    pids: [max_pids]u64,
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
    pc: u64,
    depth: u32,
    reserved: u32,
    frames: [frame_depth]u64,
};

pub const backing_pages = 45;
const record_pages = 44;
const Lock = spinlock.IrqSaveSpinlock;
var lock: Lock = .{};
var ring = Ring{};
var session_token: u64 = 0;
var last_token: u64 = 0;
var owner = process.Principal{};
var config = std.mem.zeroes(Config);
var active = false;
var backing: u64 = 0;

/// Host tests borrow backing to exercise control copies without hardware or
/// pool allocation. This seam cannot be called in a guest build.
pub fn test_session(records: ?*[ring_records]Record, uids: ?*[ring_records]u32, principal: process.Principal) void {
    if (comptime !builtin.is_test) @compileError("host-only sampler session");
    ring = .{ .records = records, .uids = uids };
    owner = principal;
    session_token = if (records == null) 0 else 1;
    active = false;
}

fn err(value: i64) u64 {
    return @bitCast(value);
}

pub fn may_observe(actor: process.Principal, uid: u32) bool {
    return actor.uid == uid or actor.has(process.cap_proc_admin);
}

/// Sequence numbers distinguish a staged batch from records that overwrote it.
/// The capture uid sidecar fits exactly one pool page, never the wire record.
pub const Ring = struct {
    records: ?*[ring_records]Record = null,
    uids: ?*[ring_records]u32 = null,
    first: u64 = 0,
    next: u64 = 0,
    dropped: u64 = 0,

    pub fn count(self: *const Ring) usize {
        return @intCast(self.next - self.first);
    }

    pub fn push(self: *Ring, record: Record, uid: u32) void {
        if (self.records == null or self.uids == null) return;
        // Refuse an impossible sequence wrap rather than recycle identities.
        if (self.next == std.math.maxInt(u64)) return;
        if (self.count() == ring_records) {
            self.first += 1;
            self.dropped +|= 1;
        }
        const index = self.next % ring_records;
        self.records.?[index] = record;
        self.uids.?[index] = uid;
        self.next += 1;
    }

    pub fn consume(self: *Ring, first: u64, count_: usize) void {
        // A concurrent producer can overwrite staged records, but must not
        // cause READ to consume records that were not in the staged batch.
        self.first = @max(self.first, @min(self.next, first + count_));
    }
};

pub const ReadFrame = *const fn (u64, *[16]u8) bool;

/// At most sixteen fault-safe reads. FP and LR are untrusted wire integers.
pub fn walk(fp_: u64, pc: u64, read: ReadFrame, record: *Record) void {
    var fp = fp_;
    for (0..frame_depth) |_| {
        if (fp == 0 or fp & 15 != 0 or fp > std.math.maxInt(u64) - 16) break;
        var bytes: [16]u8 = undefined;
        if (!read(fp, &bytes)) break;
        const previous = std.mem.readInt(u64, bytes[0..8], .little);
        const lr = std.mem.readInt(u64, bytes[8..16], .little);
        if (lr == 0) break;
        if (lr != pc) {
            record.frames[record.depth] = lr;
            record.depth += 1;
        }
        if (previous <= fp) break;
        fp = previous;
    }
}

fn read_user_frame(address: u64, bytes: *[16]u8) bool {
    // EL1's identity overlay must never stand in for a missing EL0 page.
    // No demand population, allocation or service-domain lock from the IRQ.
    if (comptime builtin.is_test) return uaccess.copy_in(bytes, address, bytes.len) == .ok;
    const root = mmu.current_ttbr0();
    if (address >= (@as(u64, 1) << 48) or
        !mmu.leaf_el0_visible(root, address) or
        !mmu.leaf_el0_visible(root, address + 15)) return false;
    return exceptions.read_sample_frame(address, bytes);
}

/// Called only for an acknowledged virtual-timer IRQ, before any rotation.
pub fn on_irq(frame: *const exceptions.VectorFrame, pc: u64, spsr: u64) void {
    if (!exceptions.is_from_el0(spsr)) return;
    const saved = lock.lock();
    defer lock.unlock(saved);
    if (!active) return;
    const tid = scheduler.current_id();
    const pid = process.find_by_task(tid) orelse return;
    var selected = false;
    for (config.pids[0..config.pid_count]) |target| {
        if (target == pid) selected = true;
    }
    if (!selected) return;
    const principal = process.principal(pid) orelse return;
    if (!may_observe(owner, principal.uid)) return;
    var record = std.mem.zeroes(Record);
    record.version = version;
    record.pid = pid;
    record.tid = tid;
    record.cntpct = timer.cntpct();
    record.pc = pc;
    walk(exceptions.frame_read(frame, 29), pc, read_user_frame, &record);
    ring.push(record, principal.uid);
}

pub fn validate_config(value: Config, actor: process.Principal) i64 {
    if (value.version != version or value.pid_count == 0 or value.pid_count > max_pids) return -1;
    for (value.pids, 0..) |pid, index| {
        if (index >= value.pid_count) {
            if (pid != 0) return -1;
            continue;
        }
        if (pid >= process.max_processes) return -1;
        const target = process.info(@intCast(pid)) orelse return -1;
        if (target.state != .created and target.state != .running) return -1;
        const principal = process.principal(@intCast(pid)) orelse return -1;
        if (!may_observe(actor, principal.uid)) return -7;
        for (value.pids[0..index]) |earlier| {
            if (earlier == pid) return -1;
        }
    }
    return 0;
}

fn header(count_: usize) ReadHeader {
    return .{
        .version = version,
        .record_bytes = @sizeOf(Record),
        .count = @intCast(count_),
        .reserved = 0,
        .dropped = ring.dropped,
    };
}

pub fn handle(args: [6]u64, _: *exceptions.VectorFrame) u64 {
    // The frozen dispatcher's block-scoped defer releases its domain before
    // the handler call. Protect control-side registry reads and copy-out for
    // the entire operation, acquiring the service domain BEFORE our ring.
    // IRQ producers never acquire this service lock.
    const taken = svclock.acquire_missing(svclock.dom_bit(.kernel));
    defer svclock.release_set(taken);
    const pid = process.find_by_task(scheduler.current_id()) orelse return err(-1);
    const actor = process.principal(pid) orelse return err(-1);
    const op = args[0];
    if (op != op_arm and op != op_disarm and op != op_read and op != op_status) return err(-1);
    const saved = lock.lock();
    defer lock.unlock(saved);
    if (session_token != 0 and !may_observe(actor, owner.uid)) return err(-7);
    if (op == op_arm) {
        if (args[1] != 0 or args[3] != @sizeOf(Config)) return err(-1);
        var value: Config = undefined;
        if (uaccess.copy_in(std.mem.asBytes(&value), args[2], @sizeOf(Config)) != .ok) return err(-3);
        const valid = validate_config(value, actor);
        if (valid != 0) return err(valid);
        if (!timer.profile_available()) return err(-4);
        if (last_token == std.math.maxInt(i64)) return err(-5);
        // Reservation never exceeds 45 pages, including session replacement.
        // Reuse the fixed backing only after every fallible check succeeds.
        if (backing == 0) backing = alloc.alloc_pages(backing_pages) orelse return err(-10); // native ENOMEM
        ring = .{
            .records = @ptrFromInt(backing),
            .uids = @ptrFromInt(backing + record_pages * alloc.page_size),
        };
        config = value;
        owner = actor;
        last_token += 1;
        session_token = last_token;
        active = true;
        timer.profile_set_active(true);
        return session_token;
    }
    if (args[1] == 0 or args[1] != session_token) return err(-2);
    if (op == op_disarm) {
        if (args[2] != 0 or args[3] != 0) return err(-1);
        active = false;
        timer.profile_set_active(false);
        return 0;
    }
    if (op == op_status) {
        if (args[3] != @sizeOf(ReadHeader)) return err(-1);
        // STATUS exposes no record, but count also must not reveal a
        // capture that this caller cannot read.
        for (0..ring.count()) |offset| {
            if (!may_observe(actor, ring.uids.?[(ring.first + offset) % ring_records])) return err(-7);
        }
        var status = header(ring.count());
        if (uaccess.copy_out(args[2], std.mem.asBytes(&status), @sizeOf(ReadHeader)) != .ok) return err(-3);
        return 0;
    }
    if (args[3] < @sizeOf(ReadHeader)) return err(-1);
    const count_: usize = @intCast(@min(ring.count(), @min(max_read_records, (args[3] - @sizeOf(ReadHeader)) / @sizeOf(Record))));
    const Batch = extern struct { header: ReadHeader, records: [max_read_records]Record };
    var batch: Batch = undefined;
    batch.header = header(count_);
    const first = ring.first;
    const token = session_token;
    for (0..count_) |offset| {
        const index = (first + offset) % ring_records;
        if (!may_observe(actor, ring.uids.?[index])) return err(-7);
        batch.records[offset] = ring.records.?[index];
    }
    // Copy with the sampler lock released. A producer never waits behind
    // user-copy faults. This control call still holds its kernel domain.
    lock.unlock(saved);
    const bytes = std.mem.asBytes(&batch)[0 .. @sizeOf(ReadHeader) + count_ * @sizeOf(Record)];
    const copied = uaccess.copy_out(args[2], bytes, bytes.len) == .ok;
    _ = lock.lock();
    if (!copied) return err(-3);
    if (session_token == token) ring.consume(first, @intCast(count_));
    return count_;
}
