//! ADR 0043 sample wire contract. Timer delivery belongs to M94c.
const abi = @import("syscall_abi.zig");
const exceptions = @import("exceptions.zig");

pub const slot = abi.number("sys_profile");
pub const implemented = false;
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

pub fn handle(_: [6]u64, _: *exceptions.VectorFrame) u64 {
    return @bitCast(@as(i64, -4));
}
