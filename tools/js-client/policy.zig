const std = @import("std");

pub const Options = struct {
    repl: bool = false,
    script: ?[]const u8 = null,
    receipt: ?[]const u8 = null,
};

pub fn path(value: []const u8) !void {
    if (!std.mem.startsWith(u8, value, "/host/")) return error.AccessDenied;
    if (value.len > 64) return error.PathLimit;
    if (std.mem.indexOfAny(u8, value, "\x00\\:") != null) return error.InvalidPath;
    var parts = std.mem.splitScalar(u8, value[6..], '/');
    while (parts.next()) |part| {
        if (part.len == 0 or std.mem.eql(u8, part, ".") or std.mem.eql(u8, part, ".."))
            return error.InvalidPath;
        if (part.len > 31) return error.PathLimit;
    }
}

pub fn parse(args: []const []const u8, env: []const []const u8) !Options {
    if (args.len == 0 or args.len > 8) return error.ArgumentLimit;
    for (args) |arg| {
        if (arg.len > 255 or std.mem.indexOfScalar(u8, arg, 0) != null) return error.ArgumentLimit;
    }
    if (env.len > 16) return error.EnvironmentLimit;
    for (env) |entry| {
        if (entry.len > 127 or std.mem.indexOfScalar(u8, entry, 0) != null or
            std.mem.indexOfScalar(u8, entry, '=') == null) return error.EnvironmentLimit;
    }
    var result: Options = .{};
    for (args[1..]) |arg| {
        if (std.mem.eql(u8, arg, "--repl")) {
            if (result.repl or result.script != null) return error.Usage;
            result.repl = true;
        } else if (std.mem.startsWith(u8, arg, "--receipt=")) {
            if (result.receipt != null) return error.Usage;
            const name = arg["--receipt=".len..];
            try path(name);
            result.receipt = name;
        } else {
            if (result.repl or result.script != null or std.mem.startsWith(u8, arg, "--"))
                return error.Usage;
            try path(arg);
            result.script = arg;
        }
    }
    if (!result.repl and result.script == null) return error.Usage;
    return result;
}

test "product paths retain the approved narrow subset" {
    try path("/host/" ++ "a" ** 31 ++ "/" ++ "b" ** 26);
    try std.testing.expectError(error.PathLimit, path("/host/" ++ "a" ** 31 ++ "/" ++ "b" ** 27));
    try std.testing.expectError(error.PathLimit, path("/host/" ++ "a" ** 32));
    for ([_][]const u8{ "/host/../x", "/host//x", "/host/./x", "/host/x\x00y" }) |p|
        try std.testing.expectError(error.InvalidPath, path(p));
    try std.testing.expectError(error.AccessDenied, path("/dev/tty"));
}

test "usage never clips or invents argv and env" {
    const opts = try parse(&.{ "QJS.BIN", "--repl", "--receipt=/host/R" }, &.{"A=B"});
    try std.testing.expect(opts.repl and opts.script == null);
    try std.testing.expectError(error.Usage, parse(&.{ "QJS.BIN", "--repl", "/host/F" }, &.{}));
    try std.testing.expectError(error.ArgumentLimit, parse(&([_][]const u8{"x"} ** 9), &.{}));
    try std.testing.expectError(error.ArgumentLimit, parse(&.{ "QJS.BIN", "x" ** 256 }, &.{}));
    try std.testing.expectError(error.EnvironmentLimit, parse(&.{"QJS.BIN"}, &([_][]const u8{"A=B"} ** 17)));
    try std.testing.expectError(error.EnvironmentLimit, parse(&.{"QJS.BIN"}, &.{"A=" ++ "x" ** 126}));
    _ = try parse(&.{ "QJS.BIN", "/host/F" }, &([_][]const u8{"A=" ++ "x" ** 125} ** 16));
}
