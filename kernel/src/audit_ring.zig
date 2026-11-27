//! Fixed-size bounded ring mechanics for the ADR 0062 audit scaffold.
//!
//! This module deliberately carries no audit event taxonomy: object kinds,
//! actors, authorities, verbs, results, retention classes, and coalescing
//! policy are unstable until the amended ADR exists. What is stable — and
//! pinned here — is the mechanical storage contract every later envelope
//! needs:
//!
//!   - fixed-size records in bounded BSS-owned storage;
//!   - one monotonically advancing sequence number per stored record;
//!   - reader watermarks over that sequence;
//!   - explicit written/overwritten/dropped counters.
//!
//! The ring is single-writer and deliberately unsynchronized. A later plane
//! owner either serializes its writer or gives each writer/core its own ring;
//! neither choice requires a lock, allocation, formatting, or host I/O in the
//! write path. Readers must use the same synchronization/sharding discipline
//! as that owner. This module only provides the bounded storage primitive.
//!
//! No libc, no POSIX, no allocation.

const std = @import("std");

/// A bounded ring of fixed-size opaque records.
///
/// `payload_bytes` is the frozen record size chosen by a later audit envelope
/// owner; this primitive treats the bytes as opaque. `capacity` is the number
/// of whole records retained. There are no partial records.
///
/// Sequence numbers start at 1 and are assigned only when a record is stored.
/// A dropped write does not consume a sequence number: the sequence orders the
/// ring's stored write history, while `dropped` separately accounts input that
/// never entered that history.
pub fn Ring(comptime payload_bytes: usize, comptime capacity: usize) type {
    if (payload_bytes == 0) @compileError("audit ring payload must be non-empty");
    if (capacity == 0) @compileError("audit ring capacity must be non-zero");

    return struct {
        pub const record_bytes = payload_bytes;
        pub const record_capacity = capacity;

        pub const Payload = [payload_bytes]u8;

        /// One copied ring record. Returning whole fixed-size values keeps the
        /// read boundary free of partial records, pointers into ring storage,
        /// and allocation.
        pub const Entry = struct {
            sequence: u64,
            payload: Payload,
        };

        pub const Stats = struct {
            /// Accepted writes, including writes that later overwrite or are
            /// themselves overwritten. Dropped writes are not included.
            written: u64,
            /// Stored records evicted by overwrite.
            overwritten: u64,
            /// Writes rejected before entering the ring.
            dropped: u64,
            /// Whole records currently retained.
            retained: usize,
            /// Bytes currently retained (`retained * record_bytes`).
            retained_bytes: usize,
        };

        pub const WriteResult = enum {
            written,
            dropped,
        };

        pub const ReadResult = struct {
            /// Whole records copied into the caller's output slice.
            count: usize,
            /// Sequence of the last copied record, or the caller's input
            /// watermark when nothing copied.
            watermark: u64,
            /// Sequenced stored records unavailable before the oldest retained
            /// record when the input watermark is behind the live window.
            missed: u64,
            /// False when `out` filled before every currently retained record
            /// newer than the input watermark could be copied.
            complete: bool,
        };

        records: [capacity]Payload = @as([capacity]Payload, @splat(@as(Payload, @splat(@as(u8, 0))))),
        sequences: [capacity]u64 = @as([capacity]u64, @splat(@as(u64, 0))),
        oldest_index: usize = 0,
        len: usize = 0,
        next_sequence: u64 = 1,
        written_count: u64 = 0,
        overwritten_count: u64 = 0,
        dropped_count: u64 = 0,

        fn finish_store_at(self: *@This(), index: usize, bytes: *const Payload) void {
            const sequence = self.next_sequence;
            self.records[index] = bytes.*;
            self.sequences[index] = sequence;
            self.next_sequence +%= 1;
            self.written_count +|= 1;
        }

        /// Store `bytes`, evicting the oldest retained record when the ring
        /// is full. This is the overwrite primitive, not a retention policy:
        /// the later audit owner decides which events may use it.
        pub fn writeOverwrite(self: *@This(), bytes: *const Payload) void {
            var index: usize = undefined;
            if (self.len < capacity) {
                index = (self.oldest_index + self.len) % capacity;
                self.len += 1;
            } else {
                index = self.oldest_index;
                self.oldest_index = (self.oldest_index + 1) % capacity;
                self.overwritten_count +|= 1;
            }
            self.finish_store_at(index, bytes);
        }

        /// Store `bytes` only when the ring has room; otherwise reject it
        /// and account the rejection in `dropped`. This is the drop primitive,
        /// not a retention policy: the later audit owner decides which events
        /// may use it.
        pub fn writeDropNew(self: *@This(), bytes: *const Payload) WriteResult {
            if (self.len == capacity) {
                self.dropped_count +|= 1;
                return .dropped;
            }
            const index = (self.oldest_index + self.len) % capacity;
            self.len += 1;
            self.finish_store_at(index, bytes);
            return .written;
        }

        /// Copy every retained record whose sequence is newer than `watermark`
        /// into `out`, oldest first, stopping only at a whole-record boundary.
        /// The returned watermark is suitable for the next call.
        pub fn readSince(self: *const @This(), watermark: u64, out: []Entry) ReadResult {
            var count: usize = 0;
            var next_watermark = watermark;
            var missed: u64 = 0;
            if (self.len == 0) {
                return .{ .count = 0, .watermark = watermark, .missed = 0, .complete = true };
            }

            const oldest_sequence = self.sequences[self.oldest_index];
            if (watermark +% 1 < oldest_sequence) {
                missed = oldest_sequence - (watermark +% 1);
            }

            var i: usize = 0;
            while (i < self.len) : (i += 1) {
                const index = (self.oldest_index + i) % capacity;
                const sequence = self.sequences[index];
                if (sequence <= watermark) continue;
                if (count == out.len) {
                    return .{ .count = count, .watermark = next_watermark, .missed = missed, .complete = false };
                }
                out[count] = .{ .sequence = sequence, .payload = self.records[index] };
                count += 1;
                next_watermark = sequence;
            }
            return .{ .count = count, .watermark = next_watermark, .missed = missed, .complete = true };
        }

        pub fn stats(self: *const @This()) Stats {
            return .{
                .written = self.written_count,
                .overwritten = self.overwritten_count,
                .dropped = self.dropped_count,
                .retained = self.len,
                .retained_bytes = self.len * payload_bytes,
            };
        }

        /// The newest sequence ever stored, or zero before the first write.
        pub fn newestSequence(self: *const @This()) u64 {
            return self.next_sequence -% 1;
        }
    };
}

fn test_payload(comptime n: usize, seed: u8) [n]u8 {
    var out: [n]u8 = undefined;
    for (&out, 0..) |*b, i| {
        b.* = seed +% @as(u8, @intCast(i));
    }
    return out;
}

test "audit_ring: fixed-size entries preserve whole payloads and watermark order" {
    const R = Ring(8, 4);
    var ring: R = .{};

    ring.writeOverwrite(&test_payload(8, 10));
    ring.writeOverwrite(&test_payload(8, 20));
    ring.writeOverwrite(&test_payload(8, 30));

    var two: [2]R.Entry = undefined;
    const first = ring.readSince(0, &two);
    try std.testing.expectEqual(@as(usize, 2), first.count);
    try std.testing.expectEqual(@as(u64, 2), first.watermark);
    try std.testing.expectEqual(@as(u64, 0), first.missed);
    try std.testing.expect(!first.complete);
    try std.testing.expectEqual(@as(u64, 1), two[0].sequence);
    try std.testing.expectEqual(test_payload(8, 10), two[0].payload);
    try std.testing.expectEqual(@as(u64, 2), two[1].sequence);
    try std.testing.expectEqual(test_payload(8, 20), two[1].payload);

    var one: [1]R.Entry = undefined;
    const second = ring.readSince(first.watermark, &one);
    try std.testing.expectEqual(@as(usize, 1), second.count);
    try std.testing.expectEqual(@as(u64, 3), second.watermark);
    try std.testing.expect(second.complete);
    try std.testing.expectEqual(@as(u64, 3), one[0].sequence);
    try std.testing.expectEqual(test_payload(8, 30), one[0].payload);

    const empty = ring.readSince(second.watermark, &one);
    try std.testing.expectEqual(@as(usize, 0), empty.count);
    try std.testing.expectEqual(@as(u64, 3), empty.watermark);
    try std.testing.expect(empty.complete);
}

test "audit_ring: overwrite evicts oldest, counts loss, and reports the watermark gap" {
    const R = Ring(4, 4);
    var ring: R = .{};
    var i: u8 = 0;
    while (i < 7) : (i += 1) {
        ring.writeOverwrite(&test_payload(4, i * 16));
    }

    const s = ring.stats();
    try std.testing.expectEqual(@as(u64, 7), s.written);
    try std.testing.expectEqual(@as(u64, 3), s.overwritten);
    try std.testing.expectEqual(@as(u64, 0), s.dropped);
    try std.testing.expectEqual(@as(usize, 4), s.retained);
    try std.testing.expectEqual(@as(usize, 16), s.retained_bytes);
    try std.testing.expectEqual(@as(u64, 7), ring.newestSequence());

    var entries: [4]R.Entry = undefined;
    const read = ring.readSince(0, &entries);
    try std.testing.expectEqual(@as(usize, 4), read.count);
    try std.testing.expectEqual(@as(u64, 7), read.watermark);
    try std.testing.expectEqual(@as(u64, 3), read.missed);
    try std.testing.expect(read.complete);
    for (entries, 0..) |entry, index| {
        try std.testing.expectEqual(@as(u64, 4 + index), entry.sequence);
        try std.testing.expectEqual(test_payload(4, @as(u8, @intCast(3 + index)) * 16), entry.payload);
    }
}

test "audit_ring: drop-new refusal is counted without consuming sequence" {
    const R = Ring(4, 2);
    var ring: R = .{};
    try std.testing.expectEqual(R.WriteResult.written, ring.writeDropNew(&test_payload(4, 1)));
    try std.testing.expectEqual(R.WriteResult.written, ring.writeDropNew(&test_payload(4, 2)));
    try std.testing.expectEqual(R.WriteResult.dropped, ring.writeDropNew(&test_payload(4, 3)));
    try std.testing.expectEqual(R.WriteResult.dropped, ring.writeDropNew(&test_payload(4, 4)));

    const s = ring.stats();
    try std.testing.expectEqual(@as(u64, 2), s.written);
    try std.testing.expectEqual(@as(u64, 0), s.overwritten);
    try std.testing.expectEqual(@as(u64, 2), s.dropped);
    try std.testing.expectEqual(@as(usize, 2), s.retained);
    try std.testing.expectEqual(@as(u64, 2), ring.newestSequence());

    var entries: [2]R.Entry = undefined;
    const read = ring.readSince(0, &entries);
    try std.testing.expect(read.complete);
    try std.testing.expectEqual(@as(usize, 2), read.count);
    try std.testing.expectEqual(@as(u64, 0), read.missed);
    try std.testing.expectEqual(@as(u64, 1), entries[0].sequence);
    try std.testing.expectEqual(test_payload(4, 1), entries[0].payload);
    try std.testing.expectEqual(@as(u64, 2), entries[1].sequence);
    try std.testing.expectEqual(test_payload(4, 2), entries[1].payload);
}

test "audit_ring: short reads stop at a whole-record boundary and never skip" {
    const R = Ring(8, 4);
    var ring: R = .{};
    ring.writeOverwrite(&test_payload(8, 40));
    ring.writeOverwrite(&test_payload(8, 50));

    var empty_out: [0]R.Entry = .{};
    const none = ring.readSince(0, &empty_out);
    try std.testing.expectEqual(@as(usize, 0), none.count);
    try std.testing.expectEqual(@as(u64, 0), none.watermark);
    try std.testing.expect(!none.complete);

    var one: [1]R.Entry = undefined;
    const first = ring.readSince(none.watermark, &one);
    try std.testing.expectEqual(@as(usize, 1), first.count);
    try std.testing.expectEqual(@as(u64, 1), first.watermark);
    try std.testing.expectEqual(test_payload(8, 40), one[0].payload);
}
