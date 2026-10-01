//! The host checker is narrower than the kernel, never a substitute for it.
const std = @import("std");
const elf = @import("elf");
const cases = @import("cases");

test "SDK artifact corpus against current kernel parse_head" {
    for (cases.fixtures) |fixture| {
        const result = elf.parse_head(fixture.bytes[0..@min(16 * 1024, fixture.bytes.len)], fixture.bytes.len, elf.text_base);
        if (fixture.accepted) {
            const image = result catch |err| {
                std.debug.print("{s}: unexpected kernel refusal {s}\n", .{ fixture.name, @errorName(err) });
                return err;
            };
            try std.testing.expect(image.gap_layout);
            try std.testing.expectEqual(@as(usize, 2), image.segment_count);
        } else {
            if (result) |_| {
                std.debug.print("{s}: kernel unexpectedly accepted\n", .{fixture.name});
                return error.ExpectedKernelRefusal;
            } else |_| {}
        }
    }
}
