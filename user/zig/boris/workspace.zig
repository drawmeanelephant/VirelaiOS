//! Materialized beside Boris, never in std or its pinned Oliver dependency.
const std = @import("std");
const builtin = @import("builtin");

fn allocator() std.mem.Allocator {
    if (builtin.os.tag == .freestanding) return @import("root").os.heap.page_allocator;
    if (builtin.is_test) return std.testing.allocator;
    return std.heap.page_allocator;
}

/// Stable bottom-up merge sort. Scratch is charged to the one guest arena.
/// Allocation failure uses stable insertion sort, not another allocator.
/// Equal keys always take the left element, preserving block-sort semantics.
pub fn sort(comptime T: type, items: []T, context: anytype, comptime less: fn (@TypeOf(context), T, T) bool) void {
    sortUsing(T, items, context, less, allocator());
}

pub fn sortUsing(comptime T: type, items: []T, context: anytype, comptime less: fn (@TypeOf(context), T, T) bool, gpa: std.mem.Allocator) void {
    if (items.len < 2) return;
    const scratch = gpa.alloc(T, items.len) catch {
        std.sort.insertion(T, items, context, less);
        return;
    };
    defer gpa.free(scratch);
    sortWithScratch(T, items, scratch, context, less);
}

pub fn sortWithScratch(comptime T: type, items: []T, scratch: []T, context: anytype, comptime less: fn (@TypeOf(context), T, T) bool) void {
    std.debug.assert(scratch.len >= items.len);
    var width: usize = 1;
    while (width < items.len) {
        var start: usize = 0;
        while (start < items.len) {
            const middle = start + @min(width, items.len - start);
            const end = middle + @min(width, items.len - middle);
            var left = start;
            var right = middle;
            for (start..end) |out| {
                if (right < end and (left == middle or less(context, items[right], items[left]))) {
                    scratch[out] = items[right];
                    right += 1;
                } else {
                    scratch[out] = items[left];
                    left += 1;
                }
            }
            start = end;
        }
        @memcpy(items, scratch[0..items.len]);
        if (width >= items.len - width) break;
        width *= 2;
    }
}

test "arena merge sort preserves stable equal-key ordering across merge boundaries" {
    const Item = struct { key: usize, ordinal: usize };
    const less = struct {
        fn call(_: void, a: Item, b: Item) bool {
            return a.key < b.key;
        }
    }.call;
    const items = try std.testing.allocator.alloc(Item, 1025);
    defer std.testing.allocator.free(items);
    const reference = try std.testing.allocator.alloc(Item, items.len);
    defer std.testing.allocator.free(reference);
    for (items, 0..) |*item, i| item.* = .{ .key = (items.len - i) % 17, .ordinal = i };
    @memcpy(reference, items);
    std.mem.sort(Item, reference, {}, less);
    sort(Item, items, {}, less);
    try std.testing.expectEqualSlices(Item, reference, items);
    for (items, 0..) |*item, i| item.* = .{ .key = (items.len - i) % 17, .ordinal = i };
    var unavailable = std.heap.FixedBufferAllocator.init(&.{});
    sortUsing(Item, items, {}, less, unavailable.allocator());
    try std.testing.expectEqualSlices(Item, reference, items);
}
