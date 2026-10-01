//! Root modules select the native entry explicitly, never std's hosted startup.
const sdk = @import("runtime.zig");

pub fn exportEntry(comptime main: fn (*const sdk.startup.Startup) anyerror!void) void {
    const Entry = struct {
        fn start() callconv(.naked) noreturn {
            // Preserve argc/argv, pass initial SP in x2, and paint the stack
            // budget plus one guard page before creating any call frame.
            asm volatile (
                \\mov x2, sp
                \\sub x3, x2, #33, lsl #12
                \\mov w4, #0xa5a5
                \\movk w4, #0xa5a5, lsl #16
                \\1:
                \\str w4, [x3], #4
                \\cmp x3, x2
                \\b.lo 1b
                \\b zig_guest_start
            );
        }

        fn enter(argc: usize, argv: usize, sp: usize) callconv(.c) noreturn {
            const args = sdk.receive(argc, argv, sp) catch |err| sdk.fail(@errorName(err), 64);
            main(&args) catch |err| sdk.fail(@errorName(err), 70);
            if (sdk.stackHighWater() > sdk.stack_budget) sdk.fail("StackBudget", 70);
            sdk.finish(0);
        }
    };
    @export(&Entry.start, .{ .name = "_start" });
    @export(&Entry.enter, .{ .name = "zig_guest_start" });
}
