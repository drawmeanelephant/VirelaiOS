//! Compiler-only corpus. This does NOT exercise native B2/B3 discovery.
const std = @import("std");
const boris = @import("boris");
pub const files = make();

fn make() [24]boris.SourceFile {
    var result: [24]boris.SourceFile = undefined;
    result[0] = .{ .path = "index.md", .bytes = "---\ntitle: Home\nstatus: published\n---\n# Home\n\n{{include includes/tip.md}}\n\n[[guides/intro|Introduction]]\n" };
    result[1] = .{ .path = "guides/intro.md", .bytes = "---\ntitle: Introduction\nparent: index\nstatus: published\n---\n# Introduction\n\nReal **Oliver** rendering.\n" };
    result[2] = .{ .path = "includes/tip.md", .bytes = "A shared *tip*.\n" };
    result[3] = .{ .path = "layouts/main.html", .bytes = "<!DOCTYPE html>\n<html><head><title>{{title}}</title>{{head}}</head><body>{{content}}</body></html>\n" };
    result[4] = .{ .path = "index.assets/logo.svg", .bytes = "<svg xmlns=\"http://www.w3.org/2000/svg\"/>\n" };
    result[5] = .{ .path = "ignored.txt", .bytes = "Not a page.\n" };
    for (0..18) |i| {
        result[6 + i] = .{
            .path = std.fmt.comptimePrint("guides/nested/this-name-has-a-colliding-prefix-longer-than-thirty-one-bytes-{d:0>2}.md", .{i}),
            .bytes = std.fmt.comptimePrint("---\ntitle: Page {d}\nparent: guides/intro\nstatus: published\n---\n# Page {d}\n\nNested content.\n", .{ i, i }),
        };
    }
    return result;
}
