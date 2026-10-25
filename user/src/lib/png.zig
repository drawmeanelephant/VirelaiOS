//! VirelaiOS Conforming 8-bit PNG Decoder.
//!
//! Milestone 23 / Issue #824 (IMG3).
//! Freestanding, zero-heap PNG decoder.
//! Supports Grayscale (type 0), Truecolor RGB (type 2), and Truecolor+Alpha RGBA (type 6).
//! Reconstructs scanlines across all 5 PNG filter types (None, Sub, Up, Average, Paeth)
//! and decodes directly into 32-bpp B8G8R8X8_UNORM format.
//!
//! IMG5 follow-on (claim 7317): the decoder is **workspace-driven** — IDAT
//! chunks are walked, CRC-checked, and streamed one at a time into a
//! caller-provided staging buffer (`decode_with_buffers`); the decompressed
//! scanline stream goes into a second caller-provided buffer. `scan` sizes
//! both workspaces exactly from the file (no fixed 128 KiB IDAT / 256 KiB
//! decompression BSS landmines — that static pair pushed every binary that
//! linked the PNG path past the kernel's DSK3 256 KiB `data_mem` exec cap).
//! The convenience `decode()` keeps small default workspaces for
//! fixture/small-image use; larger images call `scan` then
//! `decode_with_buffers` with caller memory (e.g. an M29 mmap region).

/// Product version — the `ver M.m.p` the component-registry lib row
/// shows and the drift check resolves (docs/component-registry.md).
/// The registry's bump rule says when it moves.
pub const version = "1.0.0";

const std = @import("std");
const crc32 = @import("crc32.zig");
const flate = @import("flate.zig");

pub const PngError = error{
    HeaderMagic,
    TruncatedInput,
    BufferTooSmall,
    InvalidCrc,
    UnsupportedBitDepth,
    UnsupportedColorType,
    UnsupportedInterlace,
    CorruptStream,
    InvalidBlockType,
    InvalidHuffmanTree,
    InvalidDistance,
    InvalidZlibHeader,
    Adler32Mismatch,
};

pub const PngHeader = struct {
    width: u32,
    height: u32,
    bit_depth: u8,
    color_type: u8,
    compression_method: u8,
    filter_method: u8,
    interlace_method: u8,
};

pub const PNG_SIGNATURE = [_]u8{ 0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A };

/// Maximum row bytes the default row buffers support (e.g. up to 1024
/// pixels * 4 = 4096 bytes). `decode_with_buffers` accepts larger caller
/// row buffers; the default `decode()` is bounded by this.
pub const MAX_ROW_BYTES: usize = 4096;
/// Convenience-workspace caps for the default `decode()` (small images).
/// Sizing workspaces from `scan` instead removes every cap for big images.
pub const DEFAULT_IDAT_CAP: usize = 32 * 1024;
pub const DEFAULT_DECOMP_CAP: usize = 96 * 1024;
var bss_idat_buf: [DEFAULT_IDAT_CAP]u8 = undefined;
var bss_decomp_buf: [DEFAULT_DECOMP_CAP]u8 = undefined;
var bss_prev_row: [MAX_ROW_BYTES]u8 = undefined;
var bss_cur_row: [MAX_ROW_BYTES]u8 = undefined;

/// Result of `scan`: everything a caller needs to size exact IDAT + scanline
/// workspaces before calling `decode_with_buffers`, with no fixed caps.
pub const ScanInfo = struct {
    width: u32,
    height: u32,
    bit_depth: u8,
    color_type: u8,
    /// Total bytes of all IDAT chunk payloads (the concatenated zlib stream).
    idat_total: usize,
    /// Exact decompressed scanline size: height * (row_bytes + 1 filter byte).
    decomp_total: usize,

    pub fn row_bytes(self: ScanInfo) usize {
        const bpp: usize = switch (self.color_type) {
            0 => 1,
            2 => 3,
            6 => 4,
            else => 0,
        };
        return @as(usize, self.width) * bpp;
    }
};

inline fn paeth_predictor(a: u8, b: u8, c: u8) u8 {
    const p: i32 = @as(i32, a) + @as(i32, b) - @as(i32, c);
    const pa = @abs(p - @as(i32, a));
    const pb = @abs(p - @as(i32, b));
    const pc = @abs(p - @as(i32, c));
    if (pa <= pb and pa <= pc) return a;
    if (pb <= pc) return b;
    return c;
}

/// Convenience decode with the default (bounded) workspaces — suitable for
/// small images and host tests. `scan` + `decode_with_buffers` covers any
/// size with caller-provided memory.
pub fn decode(bytes: []const u8, out_pixels: []align(1) u32) PngError!PngHeader {
    return decode_with_buffers(
        bytes,
        out_pixels,
        &bss_idat_buf,
        &bss_decomp_buf,
        &bss_prev_row,
        &bss_cur_row,
    );
}

/// Validate the signature and walk every chunk (per-chunk CRC included)
/// without buffering: returns the IHDR geometry plus the exact IDAT and
/// decompressed-scanline sizes a decode workspace needs. Errors mirror
/// `decode_with_buffers` (structure/CRC/content support), so a caller can
/// scan first and then decode the same bytes with an exactly-sized
/// workspace.
pub fn scan(bytes: []const u8) PngError!ScanInfo {
    if (bytes.len < 8) return error.TruncatedInput;
    if (!std.mem.eql(u8, bytes[0..8], &PNG_SIGNATURE)) return error.HeaderMagic;

    var in_pos: usize = 8;
    var header_opt: ?PngHeader = null;
    var idat_total: usize = 0;

    while (in_pos + 8 <= bytes.len) {
        const chunk_len = std.mem.readInt(u32, bytes[in_pos..][0..4], .big);
        const chunk_type = bytes[in_pos + 4 .. in_pos + 8];
        in_pos += 8;

        if (in_pos + chunk_len + 4 > bytes.len) return error.TruncatedInput;
        const chunk_data = bytes[in_pos .. in_pos + chunk_len];
        in_pos += chunk_len;

        const expected_crc = std.mem.readInt(u32, bytes[in_pos..][0..4], .big);
        in_pos += 4;

        var running_crc = crc32.update(0xFFFFFFFF, chunk_type);
        running_crc = crc32.update(running_crc, chunk_data);
        if ((running_crc ^ 0xFFFFFFFF) != expected_crc) return error.InvalidCrc;

        if (std.mem.eql(u8, chunk_type, "IHDR")) {
            if (chunk_len != 13) return error.CorruptStream;
            const w = std.mem.readInt(u32, chunk_data[0..4], .big);
            const h = std.mem.readInt(u32, chunk_data[4..8], .big);
            const bit_depth = chunk_data[8];
            const color_type = chunk_data[9];
            const comp = chunk_data[10];
            const filter = chunk_data[11];
            const interlace = chunk_data[12];

            if (bit_depth != 8) return error.UnsupportedBitDepth;
            if (color_type != 0 and color_type != 2 and color_type != 6) return error.UnsupportedColorType;
            if (interlace != 0) return error.UnsupportedInterlace;
            if (comp != 0 or filter != 0) return error.CorruptStream;

            header_opt = .{
                .width = w,
                .height = h,
                .bit_depth = bit_depth,
                .color_type = color_type,
                .compression_method = comp,
                .filter_method = filter,
                .interlace_method = interlace,
            };
        } else if (std.mem.eql(u8, chunk_type, "IDAT")) {
            idat_total += chunk_len;
        } else if (std.mem.eql(u8, chunk_type, "IEND")) {
            break;
        }
    }

    const header = header_opt orelse return error.CorruptStream;
    const bpp: usize = switch (header.color_type) {
        0 => 1,
        2 => 3,
        6 => 4,
        else => return error.UnsupportedColorType,
    };
    return .{
        .width = header.width,
        .height = header.height,
        .bit_depth = header.bit_depth,
        .color_type = header.color_type,
        .idat_total = idat_total,
        .decomp_total = @as(usize, header.height) * (@as(usize, header.width) * bpp + 1),
    };
}

pub fn decode_with_buffers(
    bytes: []const u8,
    out_pixels: []align(1) u32,
    idat_buf: []u8,
    decomp_buf: []u8,
    prev_row: []u8,
    cur_row: []u8,
) PngError!PngHeader {
    if (bytes.len < 8) return error.TruncatedInput;
    if (!std.mem.eql(u8, bytes[0..8], &PNG_SIGNATURE)) return error.HeaderMagic;

    var in_pos: usize = 8;
    var header_opt: ?PngHeader = null;
    var idat_len: usize = 0;

    // Parse PNG Chunks
    while (in_pos + 8 <= bytes.len) {
        const chunk_len = std.mem.readInt(u32, bytes[in_pos..][0..4], .big);
        const chunk_type = bytes[in_pos + 4 .. in_pos + 8];
        in_pos += 8;

        if (in_pos + chunk_len + 4 > bytes.len) return error.TruncatedInput;
        const chunk_data = bytes[in_pos .. in_pos + chunk_len];
        in_pos += chunk_len;

        const expected_crc = std.mem.readInt(u32, bytes[in_pos..][0..4], .big);
        in_pos += 4;

        // Verify CRC over chunk_type + chunk_data
        var running_crc = crc32.update(0xFFFFFFFF, chunk_type);
        running_crc = crc32.update(running_crc, chunk_data);
        if ((running_crc ^ 0xFFFFFFFF) != expected_crc) return error.InvalidCrc;

        if (std.mem.eql(u8, chunk_type, "IHDR")) {
            if (chunk_len != 13) return error.CorruptStream;
            const w = std.mem.readInt(u32, chunk_data[0..4], .big);
            const h = std.mem.readInt(u32, chunk_data[4..8], .big);
            const bit_depth = chunk_data[8];
            const color_type = chunk_data[9];
            const comp = chunk_data[10];
            const filter = chunk_data[11];
            const interlace = chunk_data[12];

            if (bit_depth != 8) return error.UnsupportedBitDepth;
            if (color_type != 0 and color_type != 2 and color_type != 6) return error.UnsupportedColorType;
            if (interlace != 0) return error.UnsupportedInterlace;
            if (comp != 0 or filter != 0) return error.CorruptStream;

            header_opt = .{
                .width = w,
                .height = h,
                .bit_depth = bit_depth,
                .color_type = color_type,
                .compression_method = comp,
                .filter_method = filter,
                .interlace_method = interlace,
            };
        } else if (std.mem.eql(u8, chunk_type, "IDAT")) {
            if (idat_len + chunk_len > idat_buf.len) return error.BufferTooSmall;
            @memcpy(idat_buf[idat_len .. idat_len + chunk_len], chunk_data);
            idat_len += chunk_len;
        } else if (std.mem.eql(u8, chunk_type, "IEND")) {
            break;
        }
    }

    const header = header_opt orelse return error.CorruptStream;
    const total_pixels = @as(usize, header.width) * header.height;
    if (out_pixels.len < total_pixels) return error.BufferTooSmall;

    const bpp: usize = switch (header.color_type) {
        0 => 1, // Grayscale
        2 => 3, // Truecolor RGB
        6 => 4, // Truecolor + Alpha
        else => return error.UnsupportedColorType,
    };

    const row_bytes = @as(usize, header.width) * bpp;
    if (row_bytes > prev_row.len or row_bytes > cur_row.len) return error.BufferTooSmall;

    // Decompress the IDAT stream through flate (zlib wrapped)
    const decompressed_size = flate.inflate(idat_buf[0..idat_len], decomp_buf, false) catch |err| switch (err) {
        error.InvalidBlockType => return error.InvalidBlockType,
        error.InvalidHuffmanTree => return error.InvalidHuffmanTree,
        error.InvalidDistance => return error.InvalidDistance,
        error.InvalidZlibHeader => return error.InvalidZlibHeader,
        error.Adler32Mismatch => return error.Adler32Mismatch,
        error.CorruptStream => return error.CorruptStream,
        error.BufferTooSmall => return error.BufferTooSmall,
    };

    const expected_decomp_size = @as(usize, header.height) * (row_bytes + 1);
    if (decompressed_size != expected_decomp_size) return error.CorruptStream;

    // Reconstruct scanlines
    @memset(prev_row[0..row_bytes], 0);

    var y: usize = 0;
    while (y < header.height) : (y += 1) {
        const row_offset = y * (row_bytes + 1);
        const filter_type = decomp_buf[row_offset];
        const raw_row = decomp_buf[row_offset + 1 .. row_offset + 1 + row_bytes];

        var i: usize = 0;
        while (i < row_bytes) : (i += 1) {
            const x = raw_row[i];
            const a = if (i >= bpp) cur_row[i - bpp] else 0;
            const b = prev_row[i];
            const c = if (i >= bpp) prev_row[i - bpp] else 0;

            const val: u8 = switch (filter_type) {
                0 => x,
                1 => x +% a,
                2 => x +% b,
                3 => x +% @as(u8, @truncate((@as(u16, a) + @as(u16, b)) / 2)),
                4 => x +% paeth_predictor(a, b, c),
                else => return error.CorruptStream,
            };
            cur_row[i] = val;
        }

        // Convert scanline to B8G8R8X8 destination pixels
        var x_idx: usize = 0;
        while (x_idx < header.width) : (x_idx += 1) {
            const out_idx = y * header.width + x_idx;
            if (header.color_type == 0) { // Grayscale
                const gray: u32 = cur_row[x_idx];
                out_pixels[out_idx] = 0xFF000000 | (gray << 16) | (gray << 8) | gray;
            } else if (header.color_type == 2) { // RGB
                const r: u32 = cur_row[x_idx * 3 + 0];
                const g: u32 = cur_row[x_idx * 3 + 1];
                const b: u32 = cur_row[x_idx * 3 + 2];
                out_pixels[out_idx] = 0xFF000000 | (r << 16) | (g << 8) | b;
            } else if (header.color_type == 6) { // RGBA
                const r: u32 = cur_row[x_idx * 4 + 0];
                const g: u32 = cur_row[x_idx * 4 + 1];
                const b: u32 = cur_row[x_idx * 4 + 2];
                const a: u32 = cur_row[x_idx * 4 + 3];
                out_pixels[out_idx] = (a << 24) | (r << 16) | (g << 8) | b;
            }
        }

        @memcpy(prev_row[0..row_bytes], cur_row[0..row_bytes]);
    }

    return header;
}

// ---------------------------------------------------------------------------
// Host Unit Tests
// ---------------------------------------------------------------------------

test "png: reject invalid signature and corrupt crc" {
    var out: [16]u32 = undefined;
    try std.testing.expectError(error.HeaderMagic, decode("NOT_A_PNG_FILE", &out));

    const fixture = @embedFile("fixtures/png/solid_rgb_4x4.png");
    var corrupt: [fixture.len]u8 = undefined;
    @memcpy(&corrupt, fixture);
    // Corrupt one byte inside IHDR data
    corrupt[16] ^= 0xFF;
    try std.testing.expectError(error.InvalidCrc, decode(&corrupt, &out));
}

test "png: decode solid rgb 4x4 fixture" {
    const fixture = @embedFile("fixtures/png/solid_rgb_4x4.png");
    var pixels: [16]u32 = undefined;
    const header = try decode(fixture, &pixels);

    try std.testing.expectEqual(@as(u32, 4), header.width);
    try std.testing.expectEqual(@as(u32, 4), header.height);
    try std.testing.expectEqual(@as(u8, 2), header.color_type); // RGB

    // Opaque Red in B8G8R8X8: 0xFFFF0000
    for (pixels) |px| {
        try std.testing.expectEqual(@as(u32, 0xFFFF0000), px);
    }
}

test "png: decode gradient rgba 8x8 fixture" {
    const fixture = @embedFile("fixtures/png/gradient_rgba_8x8.png");
    var pixels: [64]u32 = undefined;
    const header = try decode(fixture, &pixels);

    try std.testing.expectEqual(@as(u32, 8), header.width);
    try std.testing.expectEqual(@as(u32, 8), header.height);
    try std.testing.expectEqual(@as(u8, 6), header.color_type); // RGBA

    // Pixel (0, 0): r=0, g=0, b=0, a=255 -> 0xFF000000
    try std.testing.expectEqual(@as(u32, 0xFF000000), pixels[0]);

    // Pixel (1, 0): r=32, g=0, b=16, a=128 -> (128 << 24) | (32 << 16) | (0 << 8) | 16 = 0x80200010
    const expected_1_0: u32 = (128 << 24) | (32 << 16) | (0 << 8) | 16;
    try std.testing.expectEqual(expected_1_0, pixels[1]);
}

test "png: decode non-square 12x4 fixture" {
    const fixture = @embedFile("fixtures/png/rect_12x4.png");
    var pixels: [48]u32 = undefined;
    const header = try decode(fixture, &pixels);

    try std.testing.expectEqual(@as(u32, 12), header.width);
    try std.testing.expectEqual(@as(u32, 4), header.height);

    // Pixel (0, 0): r=0, g=0, b=100 -> 0xFF000064
    try std.testing.expectEqual(@as(u32, 0xFF000064), pixels[0]);
}

test "png: scan sizes the multi-chunk viewer fixture exactly" {
    const fixture = @embedFile("fixtures/png/viewer_160x120.png");
    const info = try scan(fixture);
    try std.testing.expectEqual(@as(u32, 160), info.width);
    try std.testing.expectEqual(@as(u32, 120), info.height);
    try std.testing.expectEqual(@as(u8, 2), info.color_type); // RGB
    // 2 IDAT chunks, 351 compressed bytes total; decompressed = 120 rows *
    // (160*3 + 1 filter byte) = 57720.
    try std.testing.expectEqual(@as(usize, 351), info.idat_total);
    try std.testing.expectEqual(@as(usize, 57720), info.decomp_total);
}

test "png: workspace decode of the split-IDAT viewer fixture" {
    const fixture = @embedFile("fixtures/png/viewer_160x120.png");
    const info = try scan(fixture);

    var pixels: [160 * 120]u32 = undefined;
    var idat: [351]u8 = undefined;
    var decomp: [57720]u8 = undefined;
    var prev_row: [160 * 3]u8 = undefined;
    var cur_row: [160 * 3]u8 = undefined;

    const header = try decode_with_buffers(
        fixture,
        &pixels,
        &idat,
        &decomp,
        &prev_row,
        &cur_row,
    );
    try std.testing.expectEqual(info.width, header.width);
    try std.testing.expectEqual(info.height, header.height);

    // Vertical split: x<80 amber (0xFFE87A3A), x>=80 gray (0xFF808080).
    try std.testing.expectEqual(@as(u32, 0xFFE87A3A), pixels[0]);
    try std.testing.expectEqual(@as(u32, 0xFFE87A3A), pixels[79]); // x=79 amber
    try std.testing.expectEqual(@as(u32, 0xFF808080), pixels[80]); // x=80 gray
    try std.testing.expectEqual(@as(u32, 0xFFE87A3A), pixels[119 * 160]); // row 119, col 0 = amber
}

test "png: scan rejects bad signature and bad crc" {
    try std.testing.expectError(error.HeaderMagic, scan("NOT_A_PNG_FILE"));

    const fixture = @embedFile("fixtures/png/solid_rgb_4x4.png");
    var corrupt: [fixture.len]u8 = undefined;
    @memcpy(&corrupt, fixture);
    corrupt[16] ^= 0xFF; // Corrupt one byte inside IHDR data
    try std.testing.expectError(error.InvalidCrc, scan(&corrupt));
}

test "png: workspace decode covers images past the convenience caps" {
    // 400x200 RGBA: 320200 decompressed scanline bytes > DEFAULT_DECOMP_CAP
    // (96 KiB) with a 1154-byte IDAT. The convenience decode() reports
    // BufferTooSmall; the scan-sized workspace path succeeds.
    const fixture = @embedFile("fixtures/png/big_rgba_400x200.png");
    const info = try scan(fixture);
    try std.testing.expectEqual(@as(usize, 320200), info.decomp_total);
    try std.testing.expect(info.decomp_total > DEFAULT_DECOMP_CAP);
    try std.testing.expect(info.idat_total <= DEFAULT_IDAT_CAP);

    const pixels = try std.testing.allocator.alloc(u32, 400 * 200);
    defer std.testing.allocator.free(pixels);
    const idat = try std.testing.allocator.alloc(u8, info.idat_total);
    defer std.testing.allocator.free(idat);
    const decomp = try std.testing.allocator.alloc(u8, info.decomp_total);
    defer std.testing.allocator.free(decomp);
    var prev_row: [400 * 4]u8 = undefined;
    var cur_row: [400 * 4]u8 = undefined;

    const header = try decode_with_buffers(fixture, pixels, idat, decomp, &prev_row, &cur_row);
    try std.testing.expectEqual(@as(u32, 400), header.width);
    try std.testing.expectEqual(@as(u32, 200), header.height);

    // RGBA color words: (a<<24)|(r<<16)|(g<<8)|b. Row 0 alpha 255: cyan
    // left (0,255,255), magenta right (255,0,255). Rows >= 100 alpha 128.
    try std.testing.expectEqual(@as(u32, 0xFF00FFFF), pixels[0]);
    try std.testing.expectEqual(@as(u32, 0xFF00FFFF), pixels[199]); // x=199 cyan
    try std.testing.expectEqual(@as(u32, 0xFFFF00FF), pixels[200]); // x=200 magenta
    try std.testing.expectEqual(@as(u32, 0x8000FFFF), pixels[100 * 400]);
    try std.testing.expectEqual(@as(u32, 0x80FF00FF), pixels[199 * 400 + 399]);
}

test "png: decode grayscale 8x8 fixture" {
    const fixture = @embedFile("fixtures/png/gray_8x8.png");
    var pixels: [64]u32 = undefined;
    const header = try decode(fixture, &pixels);

    try std.testing.expectEqual(@as(u32, 8), header.width);
    try std.testing.expectEqual(@as(u32, 8), header.height);
    try std.testing.expectEqual(@as(u8, 0), header.color_type); // Grayscale

    // Pixel (0, 0): gray = 0 -> 0xFF000000
    try std.testing.expectEqual(@as(u32, 0xFF000000), pixels[0]);

    // Pixel (1, 1): gray = (1+1)*16 = 32 (0x20) -> 0xFF202020
    try std.testing.expectEqual(@as(u32, 0xFF202020), pixels[1 * 8 + 1]);
}
