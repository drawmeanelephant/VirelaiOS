//! ADR 0043 memstat wire contract. The read-only snapshot belongs to M94d.
const abi = @import("syscall_abi.zig");
const exceptions = @import("exceptions.zig");

pub const slot = abi.number("sys_memstat");
pub const implemented = false;
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

pub fn handle(_: [6]u64, _: *exceptions.VectorFrame) u64 {
    return @bitCast(@as(i64, -4));
}
