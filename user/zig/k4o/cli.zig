//! Guest policy around the pinned, unmodified k4o engine.
const std = @import("std");
const kt = @import("k4o");

pub const input_limit = 128 * 1024;
pub const output_limit = 1024 * 1024;
pub const arena_bytes = 8 * 1024 * 1024;
pub const depth_limit = 8;
pub const condition_limit = 8;
pub const help =
    \\k4o render <template> [--data FILE] [--format textile|markdown|gfm]
    \\  --data, -d FILE (or =FILE); --format FORMAT (or =FORMAT)
    \\  --max-output, -m BYTES (or =BYTES); k/m/g suffixes accepted
    \\  --help, -h; --version, -v
    \\Guest limits: 131072 bytes/input, 1048576 bytes/output, 8388608-byte arena.
    \\--max-output must be 1..1048576; 0 is NOT unlimited.
    \\Seven user arguments, 255 bytes/argument; use = forms to fit.
    \\Template/JSON nesting <=8; condition complexity <=8.
    \\Paths: /host only, <=64 bytes/full path, <=31 bytes/component.
    \\
;

pub const Render = struct {
    template: []const u8,
    data: ?[]const u8 = null,
    format: kt.Format = .textile,
    max_output: usize = output_limit,
};
pub const Command = union(enum) { help, version, render: Render };

pub fn parse(args: []const []const u8) !Command {
    if (args.len > 7) return error.ArgumentLimit;
    for (args) |arg| {
        if (arg.len > 255 or std.mem.indexOfScalar(u8, arg, 0) != null)
            return error.ArgumentLimit;
    }
    if (args.len == 0) return error.MissingCommand;
    if (equal(args[0], "--help") or equal(args[0], "-h")) return .help;
    if (equal(args[0], "--version") or equal(args[0], "-v")) return .version;
    if (!equal(args[0], "render")) return error.UnknownCommand;
    var result: Render = .{ .template = "" };
    var seen_format = false;
    var i: usize = 1;
    while (i < args.len) : (i += 1) {
        const arg = args[i];
        if (equal(arg, "--help") or equal(arg, "-h")) return .help;
        if (equal(arg, "--data") or equal(arg, "-d")) {
            i += 1;
            if (i == args.len or args[i].len == 0) return error.MissingDataValue;
            if (result.data != null) return error.DuplicateData;
            result.data = args[i];
        } else if (std.mem.startsWith(u8, arg, "--data=") or std.mem.startsWith(u8, arg, "-d=")) {
            const value = arg[std.mem.indexOfScalar(u8, arg, '=').? + 1 ..];
            if (value.len == 0) return error.MissingDataValue;
            if (result.data != null) return error.DuplicateData;
            result.data = value;
        } else if (equal(arg, "--format") or std.mem.startsWith(u8, arg, "--format=")) {
            if (seen_format) return error.DuplicateFormat;
            const value = if (equal(arg, "--format")) blk: {
                i += 1;
                if (i == args.len) return error.MissingFormatValue;
                break :blk args[i];
            } else arg["--format=".len..];
            result.format = if (equal(value, "textile")) .textile else if (equal(value, "markdown")) .markdown else if (equal(value, "gfm")) .gfm else return error.InvalidFormat;
            seen_format = true;
        } else if (equal(arg, "--max-output") or equal(arg, "-m") or
            std.mem.startsWith(u8, arg, "--max-output=") or std.mem.startsWith(u8, arg, "-m="))
        {
            const value = if (std.mem.indexOfScalar(u8, arg, '=')) |at| arg[at + 1 ..] else blk: {
                i += 1;
                if (i == args.len) return error.MissingOutputValue;
                break :blk args[i];
            };
            result.max_output = try size(value);
            if (result.max_output == 0 or result.max_output > output_limit) return error.OutputLimit;
        } else if (arg.len > 1 and arg[0] == '-') {
            return error.UnknownOption;
        } else {
            if (result.template.len != 0) return error.MultipleTemplates;
            result.template = arg;
        }
    }
    if (result.template.len == 0) return error.MissingTemplate;
    return .{ .render = result };
}

fn equal(a: []const u8, b: []const u8) bool {
    return std.mem.eql(u8, a, b);
}

fn size(text: []const u8) !usize {
    if (text.len == 0) return error.MissingOutputValue;
    if (text[0] == '-') return error.NegativeOutputLimit;
    var digits = text;
    var scale: usize = 1;
    switch (std.ascii.toLower(text[text.len - 1])) {
        'k', 'm', 'g' => |suffix| {
            digits = text[0 .. text.len - 1];
            scale = switch (suffix) {
                'k' => 1024,
                'm' => 1024 * 1024,
                else => 1024 * 1024 * 1024,
            };
        },
        else => {},
    }
    const n = std.fmt.parseInt(usize, digits, 10) catch return error.InvalidOutputLimit;
    return std.math.mul(usize, n, scale) catch error.InvalidOutputLimit;
}

/// Iterative lexical preflight BEFORE recursive upstream parsing/evaluation.
/// Quotes/escapes follow the pinned parser. Invalid syntax is still diagnosed
/// by the engine, but cannot make its recursion exceed these guest bounds.
pub fn templatePreflight(src: []const u8) !void {
    if (src.len > input_limit) return error.InputLimit;
    var depth: usize = 0;
    var at: usize = 0;
    while (at + 1 < src.len) {
        if (src[at] != '{' or std.mem.indexOfScalar(u8, "{%#", src[at + 1]) == null) {
            at += 1;
            continue;
        }
        const marker = src[at + 1];
        const end = if (marker == '#')
            if (std.mem.indexOf(u8, src[at + 2 ..], "#}")) |n| at + 2 + n else return
        else
            closing(src, at + 2, if (marker == '%') "%}" else "}}") orelse return;
        if (marker == '%') {
            const content = std.mem.trim(u8, src[at + 2 .. end], " \t\r\n");
            // Match Scanner.word/isNameChar, not whitespace tokenization:
            // compact `if(true)` and `if!!!true` are valid upstream forms.
            const word_end = std.mem.indexOfAny(u8, content, " \t\n\r.[]|:()!<>=&'\"{}%,#\\") orelse content.len;
            const keyword = content[0..word_end];
            if (equal(keyword, "if") or equal(keyword, "for")) {
                depth += 1;
                if (depth > depth_limit) return error.TemplateDepthLimit;
            } else if (equal(keyword, "endif") or equal(keyword, "endfor")) {
                depth -|= 1;
            }
            if (equal(keyword, "if") or equal(keyword, "elseif"))
                try conditionPreflight(content[keyword.len..]);
        }
        at = end + 2;
    }
}

fn closing(src: []const u8, start: usize, end: []const u8) ?usize {
    var quote: u8 = 0;
    var at = start;
    while (at < src.len) : (at += 1) {
        const c = src[at];
        if (quote != 0) {
            if (c == '\\') {
                at += 1;
            } else if (c == quote) quote = 0;
        } else if (c == '\'' or c == '"') {
            quote = c;
        } else if (std.mem.startsWith(u8, src[at..], end)) return at;
    }
    return null;
}

fn conditionPreflight(src: []const u8) !void {
    var quote: u8 = 0;
    var complexity: usize = 0;
    var at: usize = 0;
    while (at < src.len) : (at += 1) {
        const c = src[at];
        if (quote != 0) {
            if (c == '\\') {
                at += 1;
            } else if (c == quote) quote = 0;
            continue;
        }
        if (c == '\'' or c == '"') {
            quote = c;
            continue;
        }
        if (std.mem.indexOfScalar(u8, "(!&|", c) != null) complexity += 1;
        // Count every occurrence conservatively, even within identifiers.
        for ([_][]const u8{ "not", "and", "or" }) |op|
            if (std.mem.startsWith(u8, src[at..], op)) {
                complexity += 1;
            };
        if (complexity > condition_limit) return error.ConditionLimit;
    }
}

pub fn jsonPreflight(src: []const u8) !void {
    if (src.len > input_limit) return error.InputLimit;
    var depth: usize = 0;
    var quoted = false;
    var escaped = false;
    for (src) |c| {
        if (quoted) {
            if (escaped) {
                escaped = false;
            } else if (c == '\\') {
                escaped = true;
            } else if (c == '"') quoted = false;
        } else switch (c) {
            '"' => quoted = true,
            '{', '[' => {
                depth += 1;
                if (depth > depth_limit) return error.JsonDepthLimit;
            },
            '}', ']' => depth -|= 1,
            else => {},
        }
    }
}

pub fn render(alloc: std.mem.Allocator, template: []const u8, json: ?[]const u8, options: Render, diagnostic: *kt.Diagnostic) ![]const u8 {
    if (options.max_output == 0 or options.max_output > output_limit) return error.OutputLimit;
    try templatePreflight(template);
    var root: std.json.Value = .{ .object = .empty };
    if (json) |bytes| {
        try jsonPreflight(bytes);
        root = std.json.parseFromSliceLeaky(std.json.Value, alloc, bytes, .{}) catch |err|
            return if (err == error.OutOfMemory) error.OutOfMemory else error.InvalidJson;
        if (root != .object) return error.JsonObjectRequired;
    }
    return kt.renderFormatWithLimit(alloc, template, root, diagnostic, options.format, options.max_output) catch |err| {
        if (err == error.Template and std.mem.startsWith(u8, diagnostic.detail, "output exceeded the "))
            return error.OutputLimit;
        return err;
    };
}
