//! Native ADR 0007 audio wire; no kernel imports and no host audio glue.
const std = @import("std");
pub const Info = extern struct {
    ready: u32 = 0,
    format: u8 = 0xff,
    rate: u8 = 0xff,
    channels: u8 = 0,
    padding: u8 = 0,
    period_bytes: u32 = 0,
    max_len: u32 = 0,
};
pub const Layout = struct { hz: usize, width: usize, frames: usize, bytes: usize };
pub const max_bytes = 65_536;

pub fn layout(samples: usize, info: Info) !Layout {
    if (info.ready != 1) return error.NoSoundDevice;
    const hz: usize = switch (info.rate) {
        0 => 8000,
        2 => 16000,
        3 => 22050,
        5 => 32000,
        6 => 44100,
        7 => 48000,
        else => return error.UnsupportedAudioRate,
    };
    const width: usize = switch (info.format) {
        5 => 2,
        17, 19 => 4,
        else => return error.UnsupportedAudioFormat,
    };
    if (info.channels != 1 and info.channels != 2) return error.UnsupportedAudioChannels;
    if (samples == 0 or samples > 220_500) return error.InvalidPcm;
    // Ceil keeps a short final sample; integer nearest-neighbor conversion is
    // deterministic, with identical mono samples duplicated for stereo.
    const frames = (samples * hz + 44_099) / 44_100;
    const bytes = frames * width * info.channels;
    if (bytes > @min(info.max_len, max_bytes)) return error.PlaybackLimit;
    return .{ .hz = hz, .width = width, .frames = frames, .bytes = bytes };
}

pub fn convert(a: std.mem.Allocator, raw_s16: []const u8, info: Info) ![]u8 {
    if (raw_s16.len % 2 != 0) return error.InvalidPcm;
    const shape = try layout(raw_s16.len / 2, info);
    const out = try a.alloc(u8, shape.bytes);
    for (0..shape.frames) |frame| {
        const index = frame * 44_100 / shape.hz;
        const sample = std.mem.readInt(i16, raw_s16[index * 2 ..][0..2], .little);
        for (0..info.channels) |channel| {
            const offset = (frame * info.channels + channel) * shape.width;
            switch (info.format) {
                5 => std.mem.writeInt(i16, out[offset..][0..2], sample, .little),
                17 => std.mem.writeInt(i32, out[offset..][0..4], @as(i32, sample) * 65_536, .little),
                19 => {
                    const value: f32 = @as(f32, @floatFromInt(sample)) / 32768.0;
                    std.mem.writeInt(u32, out[offset..][0..4], @bitCast(value), .little);
                },
                else => unreachable,
            }
        }
    }
    return out;
}

pub fn query(comptime call: anytype) !Info {
    var info = Info{};
    if (call(42, .{ @intFromPtr(&info), 0, 0, 0, 0, 0 }) != 0) return error.AudioInfoFailed;
    if (info.ready != 1) return error.NoSoundDevice;
    return info;
}

/// Exactly ONE native submission, with converted sample bytes, never RIFF.
pub fn submit(comptime call: anytype, raw: []const u8) !void {
    if (raw.len == 0 or raw.len > max_bytes) return error.PlaybackLimit;
    const result = call(43, .{ @intFromPtr(raw.ptr), raw.len, 0, 0, 0, 0 });
    // Native ADR 0007 error numbers, deliberately not host/POSIX errno.
    if (result == -9) return error.AudioDeviceRefused; // ENXIO also covers driver refusal.
    if (result == -3) return error.AudioBufferFault;
    if (result < 0) return error.AudioPlayFailed;
    if (result != raw.len) return error.ShortPlayback;
}
