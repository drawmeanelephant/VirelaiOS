const policy = @import("control.zig");
var token: u8 = 0;
fn counter(_: *anyopaque) u64 {
    return asm volatile ("mrs %[result], cntvct_el0"
        : [result] "=r" (-> u64),
    );
}
pub fn native() policy.Error!policy.Clock {
    if (@import("builtin").cpu.arch != .aarch64) return error.ClockUnavailable;
    const frequency = asm volatile ("mrs %[result], cntfrq_el0"
        : [result] "=r" (-> u64),
    );
    const result: policy.Clock = .{ .context = &token, .frequency = frequency, .counter = counter };
    try result.validate();
    return result;
}
