//! Only the guest policy, never a fork of the pinned synthesis algorithm.
const std = @import("std");
pub const synth = @import("upstream/synth.zig");
pub const arena_bytes = 4 * 1024 * 1024;
pub const max_samples = 220_500;
pub const max_output = 441_044;
pub const Input = union(enum) { phrase: []const u8, seed: u64 };

pub fn checkSamples(count: usize) error{DurationLimit}!void {
    if (count > max_samples) return error.DurationLimit;
}

pub fn render(a: std.mem.Allocator, input: Input) ![]u8 {
    switch (input) {
        .phrase => |phrase| {
            if (phrase.len > 255) return error.InputLimit;
            // The real planner is authoritative; preflight before PCM allocation.
            var plan = try synth.planPhrase(a, phrase);
            defer plan.deinit(a);
            if (plan.items.len == 0) return error.NoSyllables;
            try checkSamples(plan.total_samples);
        },
        // Upstream draws <=3 segments of <=340ms and <=2 gaps of <=70ms:
        // <=51,156 samples. No unbounded planning or recursion on this path.
        .seed => {},
    }
    const wav = switch (input) {
        .phrase => |phrase| try synth.renderPhraseWav(a, phrase),
        .seed => |seed| try synth.renderShuziWav(a, seed),
    };
    errdefer a.free(wav);
    if (wav.len > max_output) return error.OutputLimit;
    return wav;
}
