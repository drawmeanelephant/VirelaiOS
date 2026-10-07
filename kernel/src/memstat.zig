//! ADR 0043: a bounded, read-only page/region snapshot, not Go heap bytes.
const std = @import("std");
const abi = @import("syscall_abi.zig");
const exceptions = @import("exceptions.zig");
const process = @import("process.zig");
const scheduler = @import("scheduler.zig");
const svclock = @import("svclock.zig");
const timer = @import("timer.zig");
const uaccess = @import("uaccess.zig");

pub const slot = abi.number("sys_memstat");
pub const implemented = true;
pub const version = 1;
pub const max_regions = 16;
pub const flag_exited = 1;
pub const Record = extern struct {
    version: u32,
    flags: u32,
    pid: u64,
    cntpct: u64,
    peak_pages: u64,
    peak_regions: u64,
    static_pages: u64,
    total_pages: u64,
    record_failures: u64,
    live_pages: u64,
    live_regions: u64,
    text_bytes: u64,
    ro_bytes: u64,
    data_bytes: u64,
    stack_bytes: u64,
    region_sizes: [max_regions]u64,
};

fn err(value: i64) u64 {
    return @bitCast(value);
}

pub fn handle(args: [6]u64, _: *exceptions.VectorFrame) u64 {
    // The frozen dispatcher releases its service domain before calling us.
    // Hold it through caller identity, snapshot authorization and user copy.
    const taken = svclock.acquire_missing(svclock.dom_bit(.kernel));
    defer svclock.release_set(taken);
    const caller = process.find_by_task(scheduler.current_id()) orelse return err(-1);
    const actor = process.principal(caller) orelse return err(-1);
    if (args[2] != @sizeOf(Record)) return err(-1);
    const snapshot = process.memory_snapshot(args[0]) orelse return err(-1);
    if (actor.uid != snapshot.actor.uid and !actor.has(process.cap_proc_admin)) return err(-7);
    var record = Record{
        .version = version,
        .flags = if (snapshot.exited) flag_exited else 0,
        .pid = args[0],
        .cntpct = timer.cntpct(),
        .peak_pages = snapshot.receipt.peak_pages,
        .peak_regions = snapshot.receipt.peak_regions,
        .static_pages = snapshot.receipt.static_pages,
        .total_pages = snapshot.receipt.total_pages,
        .record_failures = snapshot.receipt.record_failures,
        .live_pages = snapshot.live_pages,
        .live_regions = snapshot.live_regions,
        .text_bytes = snapshot.text_bytes,
        .ro_bytes = snapshot.ro_bytes,
        .data_bytes = snapshot.data_bytes,
        .stack_bytes = snapshot.stack_bytes,
        .region_sizes = snapshot.region_sizes,
    };
    if (uaccess.copy_out(args[1], std.mem.asBytes(&record), @sizeOf(Record)) != .ok) return err(-3);
    return @sizeOf(Record);
}
