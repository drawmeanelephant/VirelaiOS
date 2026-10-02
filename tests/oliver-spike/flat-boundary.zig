//! Test-only flat-image argv headroom probe. Not Oliver CLI coverage.
const std = @import("std");
pub const panic = std.debug.FullPanic(panicImpl);
fn panicImpl(_: []const u8, _: ?usize) noreturn {
    exit(71);
}
fn exit(code: u64) noreturn {
    asm volatile ("svc #0"
        :
        : [num] "{x8}" (@as(u64, 3)),
          [code] "{x0}" (code),
        : .{ .memory = true });
    unreachable;
}
export fn _start(argc: usize, argv: ?[*]const [256]u8) callconv(.c) noreturn {
    if (argc != 0) {
        if (argc != 2 or argv == null) exit(70);
        if (!std.mem.eql(u8, std.mem.sliceTo(&argv.?[0], 0), "alpha")) exit(70);
        if (!std.mem.eql(u8, std.mem.sliceTo(&argv.?[1], 0), "beta")) exit(70);
    }
    const msg = "oliver-flat: argv256 ok\n";
    asm volatile ("svc #0"
        :
        : [num] "{x8}" (@as(u64, 1)),
          [fd] "{x0}" (@as(u64, 1)),
          [ptr] "{x1}" (@intFromPtr(msg.ptr)),
          [len] "{x2}" (msg.len),
        : .{ .memory = true });
    exit(0);
}
