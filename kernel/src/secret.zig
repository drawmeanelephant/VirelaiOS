//! VirelaiOS M50 TS5 (issue #1139, ADR 0024 D8/D10): the secret-class store.
//!
//! `SECRETS.TXT` on the host share is the secret-class counterpart of
//! `SETTINGS.TXT`: the SAME bounded key/value engine semantics
//! (`max_key_len` = 32, `max_val_len` = 64, `#v1` header — 64 chars hold a
//! 32-byte key or Ed25519 seed in hex exactly), with `max_secret_entries` =
//! 8 and a DISTINCT file. Unlike settings, the FILE itself is secret-class:
//! it is denied through the file ABI and the monitor `vf` seam for every
//! actor (D8), and the ONLY in-guest reader is `sys_secret_get` (slot 70),
//! which returns the calling principal's entries into caller memory.
//!
//! ## Line format
//!
//! One entry per line: `key<TAB>uid<TAB>value[<TAB>apps]`. The `uid`
//! scopes each entry to a principal (ADR 0024 D1); `sys_secret_get`
//! returns only the caller's entries (today every EL0 process is
//! `uid_user`; `uid_system` is the kernel's authority and is served to
//! system-principal processes only). Malformed lines are SKIPPED (the file
//! is host-provisioned and the file ABI cannot read it; a bad host line is
//! a provisioning error, not guest reachable). A table at the 8-entry cap
//! refuses further entries (never eviction).
//!
//! ## App binding (M97g, issue #2083)
//!
//! uid scoping alone is vacuous — every ordinary EL0 app is `uid_user`.
//! The optional fourth field is a comma-separated allowlist of image
//! names (`GOSH.ELF,GOSSHD.ELF`): an entry carrying one is served only to
//! a caller whose recorded image name matches a listed name AND whose
//! image file is `uid_system`-owned in `OWNERS.TXT` with no non-owner
//! write bit (`trust.system_owned_key`). Both halves are needed: the name
//! alone is forgeable (EL0 can `exec` any string it names), and ownership
//! alone names no app — but an EL0 process can neither create a
//! `uid_system` row (runtime `set_mode` mints `uid_user` rows only) nor
//! write `OWNERS.TXT` to forge one (the metadata file is self-protected
//! by construction, `trust.ensure_meta_file`), so a matching name against
//! a protected file means the operator provisioned that binary. An entry
//! without the field keeps the legacy uid-scope behavior.
//!
//! ## Secret class BY CONSTRUCTION (not only hand-seeded OWNERS.TXT)
//!
//! The file ABI denial in `trust.check` keys off the metadata table. TS2
//! seeded the class through `OWNERS.TXT`; TS5 must guarantee it even when
//! no hand-written `OWNERS.TXT` names `SECRETS.TXT`. `init_from_share()`
//! therefore calls `trust.ensure_secret_file()` BEFORE it reads a single
//! byte: the class is registered by construction, and if the (bounded,
//! 64-entry) metadata table is so full that the class cannot be registered,
//! the store REFUSES to load (fail closed — an unclassified `SECRETS.TXT`
//! would be an ordinary 0644 file the file ABI could read).
//!
//! ## Never-logged contract (D8 acceptance)
//!
//! Secret VALUES must never appear in the serial transcript, shell history,
//! env, monitor `settings`/`vf` output, `sys_procs` snapshots, crash
//! tombstones, or syscall strace. The monitor may print secret NAMES only.
//! This module exposes names only through `entry_at`; the `sys_secret_get`
//! handler copies values into caller memory through uaccess and is excluded
//! from strace in `syscall.dispatch`.
//!
//! ## Provisioning (M196a, issue #3351, ADR 0024 D8 amendment)
//!
//! `sys_secret_set` (slot 96) is the write half, in the M116c consent
//! shape: ANY EL0 process may `ask` — it stages a parsed line in a bounded
//! pending-ask table with the asker's recorded identity, per-principal
//! scoped (the line's uid must equal the caller's). The seat-or-admin
//! gestures `list`/`commit`/`drop` answer the standing ask
//! (`grant_admin_authorized` shape — the registered WM seat or
//! CAP_PROC_ADMIN), and `set` is the seat's own direct provisioning
//! gesture. `commit`/`set` refuse a line whose apps allowlist names the
//! WRITER's image (the D11 analog: never the app the row names) and never
//! evict — the 8-entry cap is a refusal (`ENOSPC`), while a same-(uid,key)
//! overwrite costs no slot. An overwrite can never silently strip a second
//! access control (#3407): a line that OMITS `apps` preserves a bound
//! allowlist rather than reverting the entry to uid-scope (only an
//! explicit new list rewrites the binding), and the LIST marshal marks a
//! would-be overwrite `replace` so the consent gesture is asked with the
//! decisive fact. A landed write persists through
//! `save_to_share()`. The asker learns the verdict through `status`; the
//! value itself crosses only caller memory — the pending table keeps it
//! in the kernel, never on a channel another party reads.
//!
//! Boot default unchanged: `init_from_share()` is a no-op when no host
//! channel or no `SECRETS.TXT` exists; nothing reads a secret at boot on a
//! default share, and no boot transcript line is emitted.

const std = @import("std");
const virtio_file = @import("virtio_file.zig");
const trust = @import("trust.zig");
const process = @import("process.zig");
const file_table = @import("file_table.zig");

pub const filename = "SECRETS.TXT";

/// ADR 0024 D8: the secret store bounds. A 64-char value holds a 32-byte
/// key or Ed25519 seed in hex exactly.
pub const max_key_len: usize = 32;
pub const max_val_len: usize = 64;
pub const max_secret_entries: usize = 8;
/// M97g (#2083): the optional fourth field — a comma-separated allowlist
/// of image names — is kernel-side policy only, never on the wire.
pub const max_apps_len: usize = 64;

/// The share payload bound: 8 entries × (32-key + uid + 64-value +
/// 64-apps + separators) plus the `#v1\n` header, with headroom.
pub const file_max: usize = 8 * (max_key_len + 10 + max_val_len + max_apps_len + 4) + 8;

pub const Entry = struct {
    uid: u32 = process.uid_user,
    key: [max_key_len]u8 = @as([max_key_len]u8, @splat(@as(u8, 0))),
    key_len: u8 = 0,
    val: [max_val_len]u8 = @as([max_val_len]u8, @splat(@as(u8, 0))),
    val_len: u8 = 0,
    /// M97g (#2083): comma-separated image-name allowlist. Empty = the
    /// legacy uid-scope-only entry.
    apps: [max_apps_len]u8 = @as([max_apps_len]u8, @splat(@as(u8, 0))),
    apps_len: u8 = 0,
};

/// `load` outcome. `full` means the file carried more valid entries than
/// the 8-entry cap (what fit was kept, never evicted).
pub const LoadResult = enum { ok, full };

/// The `sys_secret_get` wire record (ADR 0007 slot 70): fixed-size so the
/// userland parser needs no heap. uid + key_len + val_len + key + val.
pub const record_bytes: usize = 4 + 4 + 4 + max_key_len + max_val_len;
pub const SecretRecord = extern struct {
    uid: u32,
    key_len: u32,
    val_len: u32,
    key: [max_key_len]u8,
    val: [max_val_len]u8,
};

/// Fixed BSS scratch for the share read (no allocation; the syscall and
/// boot paths run without a large stack frame).
var scratch: [file_max]u8 = undefined;

var entries: [max_secret_entries]Entry = @as([max_secret_entries]Entry, @splat(@as(Entry, .{})));
var entry_count: usize = 0;
var initialized: bool = false;

pub fn init() void {
    entries = @as([max_secret_entries]Entry, @splat(@as(Entry, .{})));
    entry_count = 0;
    asks = @as([max_pending_asks]Ask, @splat(@as(Ask, .{})));
    next_ask_id = 1;
    initialized = true;
}

pub fn ensure_init() void {
    if (!initialized) init();
}

pub fn count() usize {
    ensure_init();
    return entry_count;
}

/// The store is empty (the class may still be registered; the path is
/// denied regardless of whether any entry exists).
pub fn empty() bool {
    ensure_init();
    return entry_count == 0;
}

/// Read-only access to one entry's key NAME (never the value) for the
/// monitor `secrets` command and host tests.
pub fn entry_at(i: usize) ?struct { key: []const u8, uid: u32 } {
    ensure_init();
    if (i >= entry_count) return null;
    const e = &entries[i];
    return .{ .key = e.key[0..e.key_len], .uid = e.uid };
}

/// M97g (#2083): the `sys_secret_get` caller, as the kernel recorded it —
/// the principal's uid plus the exec'd image name (the process registry's
/// verbatim copy of the path handed to exec).
pub const Reader = struct {
    uid: u32,
    image: []const u8 = "",
};

/// M97g (#2083): an entry is visible to `r` when the uid scopes match and,
/// for an app-bound entry, the caller's recorded image is on the entry's
/// allowlist AND names an operator-provisioned file. The ownership half is
/// what makes the name honest: EL0 cannot manufacture a `uid_system` row.
fn visible(e: *const Entry, r: Reader) bool {
    if (e.uid != r.uid) return false;
    if (e.apps_len == 0) return true;
    const parsed = file_table.parse_path(r.image) orelse return false;
    if (parsed.partition != .host) return false;
    const key = parsed.path[0..parsed.parsed_len()];
    var matched = false;
    var it = std.mem.splitScalar(u8, e.apps[0..e.apps_len], ',');
    while (it.next()) |app| {
        if (app.len == 0) return false; // malformed list: fail closed
        if (std.ascii.eqlIgnoreCase(app, key)) matched = true;
    }
    return matched and trust.system_owned_key(key);
}

/// The number of entries `r` may read (the `sys_secret_get` filter).
pub fn count_for(r: Reader) usize {
    ensure_init();
    var n: usize = 0;
    for (entries[0..entry_count]) |*e| {
        if (visible(e, r)) n += 1;
    }
    return n;
}

fn set_internal(uid: u32, key: []const u8, val: []const u8, apps: []const u8) SetResult {
    if (key.len == 0 or key.len > max_key_len) return .invalid_key;
    if (val.len == 0 or val.len > max_val_len) return .invalid_value;
    if (apps.len > max_apps_len) return .invalid_value;
    var e: *Entry = undefined;
    for (entries[0..entry_count]) |*x| {
        if (x.uid == uid and std.mem.eql(u8, x.key[0..x.key_len], key)) {
            e = x;
            break;
        }
    } else {
        if (entry_count >= max_secret_entries) return .table_full;
        e = &entries[entry_count];
        e.uid = uid;
        @memcpy(e.key[0..key.len], key);
        e.key_len = @intCast(key.len);
        entry_count += 1;
    }
    @memcpy(e.val[0..val.len], val);
    e.val_len = @intCast(val.len);
    // #3407: an OMITTED apps field preserves a bound allowlist — an
    // overwrite can never silently revert an app-bound entry to
    // uid-scope (a widening to every uid_user reader). Only an explicit
    // new list rewrites the binding; unbinding is a host-side act.
    if (apps.len != 0) {
        e.apps_len = @intCast(apps.len);
        @memcpy(e.apps[0..apps.len], apps);
    }
    return .ok;
}

pub const SetResult = enum { ok, invalid_key, invalid_value, table_full };

/// Parse a `#v1\n` + one `key<TAB>uid<TAB>value` line per entry payload.
/// Malformed lines are skipped (host-provisioning errors are not guest
/// reachable). A VALID line that finds the 8-entry table full is `full`
/// (refused, never eviction). A non-`#v1` schema refuses the WHOLE file
/// (fail closed — a differently-schemed `SECRETS.TXT` is never partially
/// trusted). Returns `ok` even with zero valid entries.
pub fn parse(text: []const u8) LoadResult {
    init();
    var seen_header = false;
    var overflow = false;
    var it = std.mem.splitScalar(u8, text, '\n');
    while (it.next()) |raw| {
        const line = std.mem.trimEnd(u8, raw, "\r");
        if (line.len == 0) continue;
        if (!seen_header) {
            seen_header = true;
            if (std.mem.eql(u8, line, "#v1")) continue;
            return .ok; // unknown schema: refuse the whole file (already empty)
        }
        if (parseLine(line) == .table_full) overflow = true;
    }
    return if (overflow) .full else .ok;
}

/// The decoded `key<TAB>uid<TAB>value[<TAB>apps]` line (slices borrow the
/// input). M196a: factored out of `parseLine` so the slot-96 ask path runs
/// the SAME validation a host-provisioned line does.
const Fields = struct { uid: u32, key: []const u8, val: []const u8, apps: []const u8 };

fn parseFields(line: []const u8) ?Fields {
    var fields: [4][]const u8 = .{ "", "", "", "" };
    var nfields: usize = 0;
    var it = std.mem.splitScalar(u8, line, '\t');
    while (it.next()) |f| {
        if (nfields < 4) fields[nfields] = f;
        nfields += 1;
    }
    if (nfields != 3 and nfields != 4) return null;
    if (fields[1].len == 0) return null;
    var uid: u32 = 0;
    for (fields[1]) |c| {
        if (c < '0' or c > '9') return null;
        uid = uid * 10 + @as(u32, c - '0');
    }
    // M97g (#2083): the optional apps allowlist is a comma-separated set of
    // image names; an empty member makes the whole line invalid (fail
    // closed — a half-parsed policy is worse than a skipped one).
    if (nfields == 4) {
        if (fields[3].len == 0) return null;
        var ait = std.mem.splitScalar(u8, fields[3], ',');
        while (ait.next()) |app| {
            if (app.len == 0) return null;
        }
    }
    return .{ .uid = uid, .key = fields[0], .val = fields[2], .apps = if (nfields == 4) fields[3] else "" };
}

fn parseLine(line: []const u8) SetResult {
    const f = parseFields(line) orelse return .invalid_value;
    return set_internal(f.uid, f.key, f.val, f.apps);
}

/// Serialize the store as `#v1\n` + one `key<TAB>uid<TAB>value` line per
/// entry. Returns bytes written (0 when `out` is too small even for the
/// header).
pub fn serialize(out: []u8) usize {
    ensure_init();
    const header = "#v1\n";
    if (header.len > out.len) return 0;
    @memcpy(out[0..header.len], header);
    var pos: usize = header.len;
    for (entries[0..entry_count]) |*e| {
        const line_len = e.key_len + 1 + digitsOf(e.uid) + 1 + e.val_len + 1 +
            (if (e.apps_len != 0) e.apps_len + 1 else 0);
        if (pos + line_len > out.len) break;
        @memcpy(out[pos .. pos + e.key_len], e.key[0..e.key_len]);
        pos += e.key_len;
        out[pos] = '\t';
        pos += 1;
        var ubuf: [10]u8 = undefined;
        const ulen = std.fmt.bufPrint(&ubuf, "{d}", .{e.uid}) catch unreachable;
        @memcpy(out[pos .. pos + ulen.len], ulen);
        pos += ulen.len;
        out[pos] = '\t';
        pos += 1;
        @memcpy(out[pos .. pos + e.val_len], e.val[0..e.val_len]);
        pos += e.val_len;
        // M97g (#2083): an app-bound entry serializes its allowlist so a
        // host provisioning round-trip preserves the gate.
        if (e.apps_len != 0) {
            if (pos + e.apps_len + 2 > out.len) break;
            out[pos] = '\t';
            pos += 1;
            @memcpy(out[pos .. pos + e.apps_len], e.apps[0..e.apps_len]);
            pos += e.apps_len;
        }
        if (pos + 1 > out.len) break;
        out[pos] = '\n';
        pos += 1;
    }
    return pos;
}

fn digitsOf(v: u32) usize {
    var n: usize = 1;
    var t: u32 = v;
    while (t >= 10) : (t /= 10) n += 1;
    return n;
}

/// Marshal the CALLING principal's VISIBLE entries into a fixed
/// `SecretRecord` array (the `sys_secret_get` wire shape). Returns the
/// number of records filled; 0 when the caller owns nothing it may read.
/// Values travel ONLY through caller memory; nothing here is ever logged.
pub fn records_for(r: Reader, out: *[max_secret_entries]SecretRecord) usize {
    ensure_init();
    var n: usize = 0;
    for (entries[0..entry_count]) |*e| {
        if (!visible(e, r)) continue;
        if (n >= max_secret_entries) break;
        out[n].uid = e.uid;
        out[n].key_len = e.key_len;
        out[n].val_len = e.val_len;
        @memset(&out[n].key, 0);
        @memset(&out[n].val, 0);
        @memcpy(out[n].key[0..e.key_len], e.key[0..e.key_len]);
        @memcpy(out[n].val[0..e.val_len], e.val[0..e.val_len]);
        n += 1;
    }
    return n;
}

/// Boot + gate path: read `SECRETS.TXT` from the host share and parse it.
/// The secret class is registered BY CONSTRUCTION (`trust.ensure_secret_file`)
/// BEFORE the read; if the bounded metadata table cannot hold the class the
/// store refuses to load (fail closed). Returns true only when a file was
/// read and parsed; a default share (no file) is a silent no-op, and no boot
/// transcript line is emitted (boot-default-unchanged).
pub fn init_from_share() bool {
    init();
    if (!virtio_file.available()) return false;
    if (!trust.ensure_secret_file()) return false; // class must hold first
    // M97g (#2083): the app-binding trust check needs the metadata file
    // self-protected too — an EL0-writable OWNERS.TXT could mint the
    // uid_system row `system_owned_key` looks for.
    _ = trust.ensure_meta_file();
    const n = virtio_file.read_whole(filename, &scratch) orelse return false;
    _ = parse(scratch[0..n]);
    return true;
}

/// Serialize the store back to `SECRETS.TXT` on the share. There is no
/// guest path that calls this today (no `sys_secret_set`, per D8); it exists
/// for the host-side provisioning loop and round-trip verification.
pub fn save_to_share() bool {
    ensure_init();
    if (!virtio_file.available()) return false;
    if (!trust.ensure_secret_file()) return false;
    var buf: [file_max]u8 = undefined;
    const len = serialize(&buf);
    return virtio_file.write_whole(filename, buf[0..len]) == virtio_file.st_ok;
}

// ---------------------------------------------------------------------------
// M196a (issue #3351, ADR 0024 D8 amendment): the consent-gated pending-ask
// table behind `sys_secret_set` (slot 96)
// ---------------------------------------------------------------------------

/// Slot-96 op vocabulary (ADR 0007 amendment). `ask` is the unprivileged
/// half — staging is not consent; `list`/`commit`/`drop`/`set` are the
/// seat-or-admin gestures (`grant_admin_authorized` shape); `status` is
/// the asker's own verdict read.
pub const op_ask: u64 = 0;
pub const op_list: u64 = 1;
pub const op_commit: u64 = 2;
pub const op_drop: u64 = 3;
pub const op_status: u64 = 4;
pub const op_set: u64 = 5;

/// The ask verdicts `op_status` reports (the asker's own asks only).
pub const ask_pending: u64 = 1;
pub const ask_committed: u64 = 2;
pub const ask_dropped: u64 = 3;

/// The pending-ask table bound: a staged ask waits for a human gesture,
/// so the queue is small by design — an app flooding it gets ENOSPC, and
/// a dead asker's rows are reaped lazily at the next op.
pub const max_pending_asks: usize = 4;
/// The widest line an `ask`/`set` op accepts: 32-key + 10-uid + 64-value +
/// 64-apps + 3 tabs, rounded.
pub const ask_line_max: usize = max_key_len + 10 + max_val_len + max_apps_len + 4;
/// The widest LIST marshal: id + pid + uid + image + key + mode + apps + tabs.
pub const ask_meta_max: usize = 10 + 10 + 10 + process.name_max + max_key_len + 8 + max_apps_len + 6;

/// The ask lifecycle — `pending` stands until the seat gestures; the
/// terminal states stay recorded until the asker's `status` read learns
/// the verdict, which CONSUMES the row (a vanished ask reports ENOENT,
/// not a lie — #3406). A dead asker's row frees regardless of state.
pub const AskState = enum(u8) { pending = 1, committed = 2, dropped = 3 };

/// A staged ask: the parsed entry fields plus the asker's recorded
/// identity. The seat needs WHO is asking (`pid`/`image`) and the NAME
/// the entry would take (`key`) — the LIST marshal never carries the
/// value.
pub const Ask = struct {
    state: AskState = .pending,
    id: u32 = 0,
    pid: usize = 0,
    image: [process.name_max]u8 = @as([process.name_max]u8, @splat(@as(u8, 0))),
    image_len: u8 = 0,
    entry: Entry = .{},
};

var asks: [max_pending_asks]Ask = @as([max_pending_asks]Ask, @splat(@as(Ask, .{})));
var next_ask_id: u32 = 1;

/// The commit/set persistence call, wrapped so a host test can pin the
/// virtio-less boot (save unavailable → EIO, the ask stays pending) AND
/// the landed-write path without a fake device.
var test_persist: ?bool = null;

fn persist() bool {
    if (@import("builtin").is_test and test_persist != null) return test_persist.?;
    return save_to_share();
}

/// A dead asker's row frees regardless of state: a pending ask can never
/// be consented honestly, and a terminal verdict can never be read —
/// either way the slot is a leak, not a standing record (#3406). Lazy:
/// runs at the top of each op rather than hooking the exit path (a dead
/// asker between ops is a `list`/`commit`-visible row, not a leak).
fn reap_dead_asks() void {
    for (&asks) |*a| {
        if (a.id == 0) continue;
        const live = if (process.info(a.pid)) |pi| pi.state != .exited else false;
        if (!live) a.* = .{};
    }
}

/// The `apps` field names the caller's image? `caller_image` is the
/// writer's recorded exec image (the same verbatim path `visible` parses);
/// a row whose allowlist covers the WRITER is refused at commit/set — the
/// D11 analog: never the app the row names.
fn names_caller(apps: []const u8, caller_image: []const u8) bool {
    if (apps.len == 0 or caller_image.len == 0) return false;
    const parsed = file_table.parse_path(caller_image) orelse return false;
    if (parsed.partition != .host) return false;
    const key = parsed.path[0..parsed.parsed_len()];
    var it = std.mem.splitScalar(u8, apps, ',');
    while (it.next()) |app| {
        if (std.ascii.eqlIgnoreCase(app, key)) return true;
    }
    return false;
}

pub const AskResult = union(enum) { id: u32, err: enum { invalid, table_full } };

/// op 0 — `ask(line)`: stage a parsed line for the calling principal.
/// Unprivileged by design (staging is not consent), but per-principal
/// scoped: the line's uid must equal the caller's — an app may only ask
/// for itself. Returns the ask id; `invalid` for a malformed line or a
/// uid the caller does not hold; `table_full` at the ask cap.
pub fn ask(pid: usize, uid: u32, image: []const u8, line: []const u8) AskResult {
    ensure_init();
    reap_dead_asks();
    const f = parseFields(line) orelse return .{ .err = .invalid };
    if (f.key.len == 0 or f.key.len > max_key_len) return .{ .err = .invalid };
    if (f.val.len == 0 or f.val.len > max_val_len) return .{ .err = .invalid };
    if (f.uid != uid) return .{ .err = .invalid };
    for (&asks) |*a| {
        if (a.id != 0) continue;
        a.* = .{ .id = next_ask_id, .pid = pid, .entry = .{ .uid = f.uid } };
        next_ask_id +%= 1;
        if (next_ask_id == 0) next_ask_id = 1;
        const itake = @min(image.len, process.name_max);
        @memcpy(a.image[0..itake], image[0..itake]);
        a.image_len = @intCast(itake);
        @memcpy(a.entry.key[0..f.key.len], f.key);
        a.entry.key_len = @intCast(f.key.len);
        @memcpy(a.entry.val[0..f.val.len], f.val);
        a.entry.val_len = @intCast(f.val.len);
        @memcpy(a.entry.apps[0..f.apps.len], f.apps);
        a.entry.apps_len = @intCast(f.apps.len);
        return .{ .id = a.id };
    }
    return .{ .err = .table_full };
}

fn find_ask(id: u32) ?*Ask {
    for (&asks) |*a| {
        if (a.id == id and a.id != 0) return a;
    }
    return null;
}

/// Whether `(uid,key)` already holds an entry — the replace signal the
/// LIST marshal carries so an overwrite can never pose as an add (#3407).
fn exists(uid: u32, key: []const u8) bool {
    for (entries[0..entry_count]) |*e| {
        if (e.uid == uid and std.mem.eql(u8, e.key[0..e.key_len], key)) return true;
    }
    return false;
}

/// op 1 — `list(index)`: marshal the `index`-th PENDING ask as
/// `id\tpid\tuid\timage\tkey\tmode[\tapps]` — names and policy only, never
/// the value. `mode` is `new` or `replace` (#3407): `replace` when the
/// ask's (uid,key) already holds an entry, so the seat's consent is asked
/// with the add-vs-overwrite fact. Returns the byte count, or null past
/// the last pending row (the caller iterates until ENOENT, the grant-admin
/// op-0 shape).
pub fn ask_line(index: usize, out: []u8) ?usize {
    ensure_init();
    reap_dead_asks();
    var seen: usize = 0;
    for (&asks) |*a| {
        if (a.id == 0 or a.state != .pending) continue;
        if (seen == index) {
            const mode: []const u8 = if (exists(a.entry.uid, a.entry.key[0..a.entry.key_len])) "replace" else "new";
            var nbuf: [30]u8 = undefined;
            const nums = std.fmt.bufPrint(&nbuf, "{d}\t{d}\t{d}\t", .{ a.id, a.pid, a.entry.uid }) catch return null;
            var pos: usize = 0;
            const need = nums.len + a.image_len + 1 + a.entry.key_len + 1 + mode.len +
                (if (a.entry.apps_len != 0) a.entry.apps_len + 1 else 0);
            if (need > out.len) return null;
            @memcpy(out[pos .. pos + nums.len], nums);
            pos += nums.len;
            @memcpy(out[pos .. pos + a.image_len], a.image[0..a.image_len]);
            pos += a.image_len;
            out[pos] = '\t';
            pos += 1;
            @memcpy(out[pos .. pos + a.entry.key_len], a.entry.key[0..a.entry.key_len]);
            pos += a.entry.key_len;
            out[pos] = '\t';
            pos += 1;
            @memcpy(out[pos .. pos + mode.len], mode);
            pos += mode.len;
            if (a.entry.apps_len != 0) {
                out[pos] = '\t';
                pos += 1;
                @memcpy(out[pos .. pos + a.entry.apps_len], a.entry.apps[0..a.entry.apps_len]);
                pos += a.entry.apps_len;
            }
            return pos;
        }
        seen += 1;
    }
    return null;
}

pub const CommitResult = enum { ok, enoent, enospc, eacces, einval, eio };

/// op 2 — `commit(id)`: the consent gesture. Applies the pending ask into
/// the store and persists it; a persist failure leaves the ask standing
/// (the seat may retry — the in-memory set is idempotent by key). Refuses
/// a row whose apps list names the WRITER's image. An overwrite keeps a
/// bound allowlist when the staged line omits `apps` (#3407). A landed
/// verdict scrubs the staged plaintext (#3406).
pub fn commit(id: u32, caller_image: []const u8) CommitResult {
    ensure_init();
    reap_dead_asks();
    const a = find_ask(id) orelse return .enoent;
    if (a.state != .pending) return .enoent;
    const e = &a.entry;
    if (names_caller(e.apps[0..e.apps_len], caller_image)) return .eacces;
    switch (set_internal(e.uid, e.key[0..e.key_len], e.val[0..e.val_len], e.apps[0..e.apps_len])) {
        .ok => {},
        .table_full => return .enospc,
        else => return .einval,
    }
    a.state = .committed;
    if (!persist()) {
        a.state = .pending;
        return .eio;
    }
    // The verdict stands; the staged plaintext is residue — scrub it
    // (#3406). The retry path above keeps the entry for the retry.
    a.entry = .{};
    return .ok;
}

/// op 3 — `drop(id)`: the decline gesture. The staged line never lands;
/// the ask stays recorded so the asker's `status` reads `dropped`, and
/// the staged plaintext is scrubbed with the verdict (#3406).
pub fn drop(id: u32) CommitResult {
    ensure_init();
    reap_dead_asks();
    const a = find_ask(id) orelse return .enoent;
    if (a.state != .pending) return .enoent;
    a.state = .dropped;
    a.entry = .{};
    return .ok;
}

/// op 4 — `status(id)`: the asker's own verdict, and nobody else's —
/// another pid's ask reports ENOENT (an ask's existence is not a
/// broadcast). A terminal verdict is CONSUMED on read (#3406): the row
/// retires with the answer, so the bounded table is not consume-once
/// per boot — a second read reports ENOENT like any vanished ask.
/// `pending` never retires; the ask still waits on the seat. Returns
/// the AskState value.
pub fn status(id: u32, pid: usize) ?u64 {
    ensure_init();
    reap_dead_asks();
    const a = find_ask(id) orelse return null;
    if (a.pid != pid) return null;
    const verdict = @backingInt(a.state);
    if (a.state != .pending) a.* = .{};
    return verdict;
}

/// op 5 — `set(line)`: the seat's direct provisioning gesture (the
/// operator's own entry needs no machine asker). Same line grammar and
/// the same names-the-writer refusal as `commit`; a persist failure
/// reports EIO with the in-memory set applied. The same overwrite rule
/// holds (#3407): an omitted `apps` field preserves a bound allowlist.
pub fn set_direct(caller_image: []const u8, line: []const u8) CommitResult {
    ensure_init();
    const f = parseFields(line) orelse return .einval;
    if (names_caller(f.apps, caller_image)) return .eacces;
    switch (set_internal(f.uid, f.key, f.val, f.apps)) {
        .ok => {},
        .table_full => return .enospc,
        .invalid_key, .invalid_value => return .einval,
    }
    return if (persist()) .ok else .eio;
}

// ---------------------------------------------------------------------------
// Host tests (pure; no syscalls, no filesystem)
// ---------------------------------------------------------------------------

test "secret: defaults and the two principals" {
    init();
    try std.testing.expect(empty());
    try std.testing.expectEqual(@as(usize, 0), count());
    try std.testing.expectEqual(@as(usize, 0), count_for(.{ .uid = process.uid_user }));
    try std.testing.expectEqual(@as(usize, 0), count_for(.{ .uid = process.uid_system }));
}

test "secret: set/parse bounds — 32/64 field caps and 8-entry store cap" {
    init();
    var key: [max_key_len + 1]u8 = @as([(max_key_len + 1)]u8, @splat(@as(u8, 'k')));
    var val: [max_val_len + 1]u8 = @as([(max_val_len + 1)]u8, @splat(@as(u8, 'v')));
    try std.testing.expectEqual(SetResult.invalid_key, set_internal(process.uid_user, key[0..], "v", ""));
    try std.testing.expectEqual(SetResult.invalid_value, set_internal(process.uid_user, "k", val[0..], ""));
    // A 64-char value (a 32-byte Ed25519 seed in hex exactly) is accepted.
    const seed = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef";
    try std.testing.expectEqual(SetResult.ok, set_internal(process.uid_user, "netkey", seed, ""));
    // The 8-entry cap: 7 more distinct entries fill the table, the 9th is
    // refused, never evicts.
    var i: usize = 0;
    while (i < max_secret_entries - 1) : (i += 1) {
        var nb: [16]u8 = undefined;
        const name = std.fmt.bufPrint(&nb, "key{d}", .{i}) catch unreachable;
        try std.testing.expectEqual(SetResult.ok, set_internal(process.uid_user, name, "v", ""));
    }
    try std.testing.expectEqual(SetResult.table_full, set_internal(process.uid_user, "overflow", "v", ""));
    try std.testing.expectEqual(@as(usize, max_secret_entries), count());
    // The pre-existing entries were NOT evicted.
    var recs: [max_secret_entries]SecretRecord = undefined;
    try std.testing.expectEqual(@as(usize, max_secret_entries), records_for(.{ .uid = process.uid_user }, &recs));
}

test "secret: round-trip parse/serialize preserves values per uid" {
    const src = "#v1\nnetkey\t1000\t0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef\naudkey\t0\t000102030405060708090a0b0c0d0e0f101112131415161718191a1b1c1d1e1f\n";
    try std.testing.expectEqual(LoadResult.ok, parse(src));
    try std.testing.expectEqual(@as(usize, 2), count());
    var buf: [file_max]u8 = undefined;
    const n = serialize(&buf);
    try std.testing.expect(n > 0);
    const out = buf[0..n];
    try std.testing.expect(std.mem.startsWith(u8, out, "#v1\n"));
    try std.testing.expect(std.mem.indexOf(u8, out, "netkey\t1000\t0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef\n") != null);
    try std.testing.expect(std.mem.indexOf(u8, out, "audkey\t0\t000102030405060708090a0b0c0d0e0f101112131415161718191a1b1c1d1e1f\n") != null);
    // Re-loading the serialized form reproduces the same per-uid filter.
    try std.testing.expectEqual(LoadResult.ok, parse(out));
    var recs: [max_secret_entries]SecretRecord = undefined;
    try std.testing.expectEqual(@as(usize, 1), records_for(.{ .uid = process.uid_user }, &recs));
    try std.testing.expectEqualStrings("netkey", recs[0].key[0..recs[0].key_len]);
    try std.testing.expectEqualStrings("0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef", recs[0].val[0..recs[0].val_len]);
    try std.testing.expectEqual(@as(usize, 1), records_for(.{ .uid = process.uid_system }, &recs));
    try std.testing.expectEqualStrings("audkey", recs[0].key[0..recs[0].key_len]);
    // A third principal owns nothing.
    try std.testing.expectEqual(@as(usize, 0), records_for(.{ .uid = 2000 }, &recs));
}

test "secret: sys_secret_get wire records are fixed-size and value-carrying" {
    init();
    const seed = "abcdef0123456789abcdef0123456789abcdef0123456789abcdef0123456789";
    try std.testing.expectEqual(SetResult.ok, set_internal(process.uid_user, "seed", seed, ""));
    var recs: [max_secret_entries]SecretRecord = undefined;
    try std.testing.expectEqual(@as(usize, 1), records_for(.{ .uid = process.uid_user }, &recs));
    try std.testing.expectEqual(@as(u32, 4), recs[0].key_len);
    try std.testing.expectEqual(@as(u32, 64), recs[0].val_len);
    try std.testing.expectEqual(process.uid_user, recs[0].uid);
    try std.testing.expect(std.mem.eql(u8, "seed", recs[0].key[0..4]));
    try std.testing.expect(std.mem.eql(u8, seed, recs[0].val[0..64]));
}

test "secret: malformed lines are skipped; unknown schema refuses the file" {
    init();
    // Bad line shapes (missing field, bad uid) are skipped; the valid
    // entry survives. No cap was hit, so the load is `ok` (not `full`).
    const src = "#v1\ngood\t1000\tsomevalue\nmalformed-no-tabs\nbaduid\tzzz\tvalue\n";
    try std.testing.expectEqual(LoadResult.ok, parse(src));
    try std.testing.expectEqual(@as(usize, 1), count());
    var recs: [max_secret_entries]SecretRecord = undefined;
    try std.testing.expectEqual(@as(usize, 1), records_for(.{ .uid = process.uid_user }, &recs));
    try std.testing.expectEqualStrings("good", recs[0].key[0..recs[0].key_len]);

    // A 9th VALID entry is the cap refusal: `.full`, never eviction.
    init();
    var body: [1024]u8 = undefined;
    var pos: usize = 0;
    @memcpy(body[0..4], "#v1\n");
    pos = 4;
    var i: usize = 0;
    while (i < 9) : (i += 1) {
        const line = std.fmt.bufPrint(body[pos..], "k{d}\t1000\tv\n", .{i}) catch unreachable;
        pos += line.len;
    }
    try std.testing.expectEqual(LoadResult.full, parse(body[0..pos]));
    try std.testing.expectEqual(@as(usize, max_secret_entries), count());

    // A non-#v1 schema refuses the WHOLE file (fail closed).
    init();
    const v2 = "#v2\nnetkey\t1000\tvalue\n";
    try std.testing.expectEqual(LoadResult.ok, parse(v2));
    try std.testing.expect(empty());
}

test "secret: app-bound entries (#2083) need name match AND system-owned image" {
    trust.init();
    init();
    // Operator provisioning: GOSSHD.ELF is pinned to uid_system with no
    // non-owner write bit; EVIL.ELF is an ordinary uid_user file.
    try std.testing.expectEqual(trust.LoadResult.ok, trust.load(
        "#v1\nGOSSHD.ELF\t644\t0\t-\nEVIL.ELF\t644\t1000\t-\n",
    ));
    try std.testing.expectEqual(SetResult.ok, set_internal(process.uid_user, "ssh-host", "hv", "GOSSHD.ELF"));
    try std.testing.expectEqual(SetResult.ok, set_internal(process.uid_user, "open-key", "ov", ""));
    var recs: [max_secret_entries]SecretRecord = undefined;

    // The authorized app reads both its bound key and the unbound one.
    try std.testing.expectEqual(@as(usize, 2), records_for(.{ .uid = process.uid_user, .image = "GOSSHD.ELF" }, &recs));
    // A name-matching process whose image is NOT system-owned is refused.
    trust.init();
    try std.testing.expectEqual(trust.LoadResult.ok, trust.load("#v1\n"));
    try std.testing.expectEqual(@as(usize, 1), records_for(.{ .uid = process.uid_user, .image = "GOSSHD.ELF" }, &recs));
    try std.testing.expectEqualStrings("open-key", recs[0].key[0..recs[0].key_len]);
    // Restore the provisioning and prove the wrong app is refused.
    try std.testing.expectEqual(trust.LoadResult.ok, trust.load(
        "#v1\nGOSSHD.ELF\t644\t0\t-\nEVIL.ELF\t644\t1000\t-\n",
    ));
    try std.testing.expectEqual(@as(usize, 1), records_for(.{ .uid = process.uid_user, .image = "EVIL.ELF" }, &recs));
    try std.testing.expectEqualStrings("open-key", recs[0].key[0..recs[0].key_len]);
    // A uid mismatch is refused even for the authorized image.
    try std.testing.expectEqual(@as(usize, 0), records_for(.{ .uid = process.uid_system, .image = "GOSSHD.ELF" }, &recs));
    // The bound name compares case-insensitively (the APFS share does).
    try std.testing.expectEqual(@as(usize, 2), records_for(.{ .uid = process.uid_user, .image = "gosshd.elf" }, &recs));
    // A uid_user-WRITABLE image is never authoritative, even name-matched.
    try std.testing.expectEqual(@as(usize, 1), records_for(.{ .uid = process.uid_user, .image = "EVIL.ELF" }, &recs));
}

test "secret: app allowlist parse + serialize round-trip (#2083)" {
    trust.init();
    init();
    const src = "#v1\nhostkey\t1000\tdeadbeef\tGOSSHD.ELF,TOOL.ELF\nplain\t1000\tcafe\n";
    try std.testing.expectEqual(LoadResult.ok, parse(src));
    try std.testing.expectEqual(@as(usize, 2), count());
    var buf: [file_max]u8 = undefined;
    const n = serialize(&buf);
    try std.testing.expect(std.mem.indexOf(u8, buf[0..n], "hostkey\t1000\tdeadbeef\tGOSSHD.ELF,TOOL.ELF\n") != null);
    try std.testing.expect(std.mem.indexOf(u8, buf[0..n], "plain\t1000\tcafe\n") != null);
    // The round-trip keeps the allowlist, so the gate survives a re-load.
    try std.testing.expectEqual(LoadResult.ok, parse(buf[0..n]));
    var recs: [max_secret_entries]SecretRecord = undefined;
    try std.testing.expectEqual(@as(usize, 1), records_for(.{ .uid = process.uid_user, .image = "TOOL.ELF" }, &recs));
    // TOOL.ELF has no OWNERS.TXT row, so the bound key is refused even for
    // a listed name — the ownership half is mandatory.
    try std.testing.expectEqualStrings("plain", recs[0].key[0..recs[0].key_len]);
    // Malformed allowlists (empty member) are provisioning errors: skipped.
    init();
    try std.testing.expectEqual(LoadResult.ok, parse("#v1\nbad\t1000\tv\tGOSH.ELF,\n"));
    try std.testing.expectEqual(@as(usize, 0), count());
}

test "secret: the 8-entry cap counts the table, not one principal" {
    init();
    var i: usize = 0;
    while (i < max_secret_entries) : (i += 1) {
        var nb: [16]u8 = undefined;
        const name = std.fmt.bufPrint(&nb, "k{d}", .{i}) catch unreachable;
        // Alternate owners; the cap is on the table, not per uid.
        const owner: u32 = if (i % 2 == 0) process.uid_user else process.uid_system;
        try std.testing.expectEqual(SetResult.ok, set_internal(owner, name, "v", ""));
    }
    try std.testing.expectEqual(@as(usize, max_secret_entries), count());
    var recs: [max_secret_entries]SecretRecord = undefined;
    try std.testing.expectEqual(@as(usize, 4), records_for(.{ .uid = process.uid_user }, &recs));
    try std.testing.expectEqual(@as(usize, 4), records_for(.{ .uid = process.uid_system }, &recs));
}

test "secret: slot-96 ask/list/commit/drop/status — the consent lifecycle (#3351)" {
    init();
    test_persist = true;
    defer test_persist = null;
    const pid = process.create_as("ASKER.ELF", .{}, .{}, .{}, .{ .uid = process.uid_user }).?;
    const seat = "/host/GOTABWM.ELF";

    // Per-principal staging: the line's uid must equal the caller's.
    const r1 = ask(pid, process.uid_user, "ASKER.ELF", "mail-x\t1000\tpw\tGOMAIL.ELF");
    try std.testing.expectEqual(@as(u32, 1), r1.id);
    try std.testing.expectEqual(@as(u64, ask_pending), status(1, pid).?);
    try std.testing.expectEqual(AskResult{ .err = .invalid }, ask(pid, process.uid_user, "ASKER.ELF", "k\t0\tv"));
    // A second principal's malformed ask — oversized key and value.
    var kbuf: [max_key_len + 1]u8 = @splat('k');
    var vbuf: [max_val_len + 1]u8 = @splat('v');
    var linebuf: [ask_line_max]u8 = undefined;
    const bad = std.fmt.bufPrint(&linebuf, "{s}\t1000\t{s}", .{ kbuf[0..], vbuf[0..] }) catch unreachable;
    try std.testing.expectEqual(AskResult{ .err = .invalid }, ask(pid, process.uid_user, "ASKER.ELF", bad));

    // LIST marshals names and policy only — never the value; mode names
    // the row an add (`new`) or an overwrite (`replace`, #3407).
    var buf: [ask_meta_max]u8 = undefined;
    const n = ask_line(0, &buf).?;
    const want = std.fmt.bufPrint(&linebuf, "1\t{d}\t1000\tASKER.ELF\tmail-x\tnew\tGOMAIL.ELF", .{pid}) catch unreachable;
    try std.testing.expectEqualStrings(want, buf[0..n]);
    try std.testing.expect(ask_line(1, &buf) == null);
    try std.testing.expect(std.mem.indexOf(u8, buf[0..n], "pw") == null);

    // The seat's consent lands the entry; status now reports committed.
    try std.testing.expectEqual(CommitResult.ok, commit(1, seat));
    try std.testing.expectEqual(@as(u64, ask_committed), status(1, pid).?);
    try std.testing.expectEqual(@as(usize, 1), count());
    try std.testing.expectEqualStrings("mail-x", entry_at(0).?.key);
    try std.testing.expectEqual(CommitResult.enoent, commit(1, seat));

    // The decline gesture leaves a verdict, not a write.
    const r2 = ask(pid, process.uid_user, "ASKER.ELF", "irc\t1000\tsasl");
    try std.testing.expectEqual(@as(u32, 2), r2.id);
    try std.testing.expectEqual(CommitResult.ok, drop(2));
    try std.testing.expectEqual(@as(u64, ask_dropped), status(2, pid).?);
    try std.testing.expectEqual(@as(usize, 1), count());
    // Another pid's ask is invisible to status.
    const pid2 = process.create_as("NOSEY.ELF", .{}, .{}, .{}, .{ .uid = process.uid_user }).?;
    try std.testing.expect(status(2, pid2) == null);
}

test "secret: slot-96 refusals — writer-named rows, caps, dead askers (#3351)" {
    init();
    test_persist = true;
    defer test_persist = null;
    const pid = process.create_as("ASKER.ELF", .{}, .{}, .{}, .{ .uid = process.uid_user }).?;
    const seat = "/host/GOTABWM.ELF";

    // A row whose apps list names the WRITER is refused — commit and set.
    _ = ask(pid, process.uid_user, "ASKER.ELF", "mine\t1000\tv\tGOTABWM.ELF");
    try std.testing.expectEqual(CommitResult.eacces, commit(1, seat));
    try std.testing.expectEqual(CommitResult.eacces, set_direct(seat, "mine\t1000\tv\tGOTABWM.ELF"));
    try std.testing.expectEqual(@as(usize, 0), count());
    // The asker naming ITSELF in apps is fine — the gesture still lands.
    _ = ask(pid, process.uid_user, "ASKER.ELF", "mail\t1000\tv\tASKER.ELF");
    try std.testing.expectEqual(CommitResult.ok, commit(2, seat));

    // The store cap is a refusal, never eviction — commit reports ENOSPC.
    init();
    var i: usize = 0;
    while (i < max_secret_entries) : (i += 1) {
        var nb: [16]u8 = undefined;
        const nm = std.fmt.bufPrint(&nb, "k{d}", .{i}) catch unreachable;
        try std.testing.expectEqual(SetResult.ok, set_internal(process.uid_user, nm, "v", ""));
    }
    const r3 = ask(pid, process.uid_user, "ASKER.ELF", "late\t1000\tv");
    try std.testing.expectEqual(CommitResult.enospc, commit(r3.id, seat));
    try std.testing.expectEqual(CommitResult.enospc, set_direct(seat, "late\t1000\tv"));
    // An overwrite of an existing (uid,key) costs no slot — it lands.
    try std.testing.expectEqual(CommitResult.ok, set_direct(seat, "k0\t1000\tnewval"));

    // A dead asker's row is reaped: the commit reports ENOENT.
    init();
    const dead = ask(999, process.uid_user, "GHOST.ELF", "k\t1000\tv");
    try std.testing.expectEqual(@as(u32, 1), dead.id);
    var buf: [ask_meta_max]u8 = undefined;
    try std.testing.expect(ask_line(0, &buf) == null); // reaped on the next op
    try std.testing.expectEqual(CommitResult.enoent, commit(1, seat));

    // The ask table itself is bounded: four staged, the fifth refused.
    init();
    var a: usize = 0;
    while (a < max_pending_asks) : (a += 1) {
        var nb: [16]u8 = undefined;
        const nm = std.fmt.bufPrint(&nb, "a{d}", .{a}) catch unreachable;
        var lb: [32]u8 = undefined;
        const ln = std.fmt.bufPrint(&lb, "{s}\t1000\tv", .{nm}) catch unreachable;
        try std.testing.expect(ask(pid, process.uid_user, "ASKER.ELF", ln) == .id);
    }
    try std.testing.expectEqual(AskResult{ .err = .table_full }, ask(pid, process.uid_user, "ASKER.ELF", "fifth\t1000\tv"));
}

test "secret: overwrite preserves the apps binding; LIST marks replace (#3407)" {
    init();
    test_persist = true;
    defer test_persist = null;
    const pid = process.create_as("ASKER.ELF", .{}, .{}, .{}, .{ .uid = process.uid_user }).?;
    const seat = "/host/GOTABWM.ELF";

    // A host-provisioned app-bound entry stands; an ask that omits the
    // apps field targets the same (uid,key).
    try std.testing.expectEqual(SetResult.ok, set_internal(process.uid_user, "net-hmac", "hostseed", "GOSSHD.ELF"));
    const r = ask(pid, process.uid_user, "ASKER.ELF", "net-hmac\t1000\tattacker-val");
    try std.testing.expectEqual(@as(u32, 1), r.id);

    // The LIST marshal carries the replace signal — the overwrite cannot
    // pose as an ordinary add to the consenting seat.
    var buf: [ask_meta_max]u8 = undefined;
    const n = ask_line(0, &buf).?;
    var want: [ask_meta_max]u8 = undefined;
    const wl = std.fmt.bufPrint(&want, "1\t{d}\t1000\tASKER.ELF\tnet-hmac\treplace", .{pid}) catch unreachable;
    try std.testing.expectEqualStrings(wl, buf[0..n]);

    // Consent lands the new value but the omitted apps field does NOT
    // strip the binding — the entry stays app-bound, never reverts to
    // uid-scope. The serialized row still names GOSSHD.ELF.
    try std.testing.expectEqual(CommitResult.ok, commit(1, seat));
    var out: [file_max]u8 = undefined;
    const sn = serialize(&out);
    try std.testing.expect(std.mem.indexOf(u8, out[0..sn], "net-hmac\t1000\tattacker-val\tGOSSHD.ELF\n") != null);
    try std.testing.expect(std.mem.indexOf(u8, out[0..sn], "net-hmac\t1000\tattacker-val\n") == null);

    // The seat's own `set` follows the same rule: value rotates, the
    // binding survives an omitted field; an explicit list rewrites it.
    try std.testing.expectEqual(CommitResult.ok, set_direct(seat, "net-hmac\t1000\trotated"));
    const s2n = serialize(&out);
    try std.testing.expect(std.mem.indexOf(u8, out[0..s2n], "net-hmac\t1000\trotated\tGOSSHD.ELF\n") != null);
    try std.testing.expectEqual(CommitResult.ok, set_direct(seat, "net-hmac\t1000\trotated\tTOOL.ELF"));
    const s3n = serialize(&out);
    try std.testing.expect(std.mem.indexOf(u8, out[0..s3n], "net-hmac\t1000\trotated\tTOOL.ELF\n") != null);

    // An unbound entry stays unbound — omitting apps on its overwrite is
    // a plain uid-scope value swap, and LIST still calls it replace.
    const r2 = ask(pid, process.uid_user, "ASKER.ELF", "other\t1000\tv1");
    try std.testing.expectEqual(@as(u32, 2), r2.id);
    try std.testing.expectEqual(CommitResult.ok, commit(2, seat));
    const r3 = ask(pid, process.uid_user, "ASKER.ELF", "other\t1000\tv2");
    try std.testing.expectEqual(@as(u32, 3), r3.id);
    const n3 = ask_line(0, &buf).?;
    const wl3 = std.fmt.bufPrint(&want, "3\t{d}\t1000\tASKER.ELF\tother\treplace", .{pid}) catch unreachable;
    try std.testing.expectEqualStrings(wl3, buf[0..n3]);
    try std.testing.expectEqual(CommitResult.ok, commit(3, seat));
    const s4n = serialize(&out);
    try std.testing.expect(std.mem.indexOf(u8, out[0..s4n], "other\t1000\tv2\n") != null);
}

test "secret: slot-96 terminal rows retire — the verdict read frees the slot (#3406)" {
    init();
    test_persist = true;
    defer test_persist = null;
    const pid = process.create_as("ASKER.ELF", .{}, .{}, .{}, .{ .uid = process.uid_user }).?;
    const seat = "/host/GOTABWM.ELF";

    // The audit's consume-once shape: four asks fill the table and a
    // fifth is refused — and answering all four frees nothing by itself,
    // the verdict has to be readable by a live asker.
    var ids: [max_pending_asks]u32 = undefined;
    var i: usize = 0;
    while (i < max_pending_asks) : (i += 1) {
        var lb: [40]u8 = undefined;
        const ln = std.fmt.bufPrint(&lb, "k{d}\t1000\tsecretval{d}", .{ i, i }) catch unreachable;
        ids[i] = ask(pid, process.uid_user, "ASKER.ELF", ln).id;
    }
    try std.testing.expectEqual(AskResult{ .err = .table_full }, ask(pid, process.uid_user, "ASKER.ELF", "fifth\t1000\tv"));
    try std.testing.expectEqual(CommitResult.ok, commit(ids[0], seat));
    try std.testing.expectEqual(CommitResult.ok, drop(ids[1]));
    try std.testing.expectEqual(CommitResult.ok, commit(ids[2], seat));
    try std.testing.expectEqual(CommitResult.ok, drop(ids[3]));
    try std.testing.expectEqual(AskResult{ .err = .table_full }, ask(pid, process.uid_user, "ASKER.ELF", "fifth\t1000\tv"));

    // The terminal transitions scrubbed the staged plaintext — the asks
    // BSS no longer holds a copy of a landed or declined value.
    for (&asks) |*a| {
        try std.testing.expectEqual(@as(u8, 0), a.entry.val_len);
        try std.testing.expect(std.mem.allEqual(u8, &a.entry.val, 0));
    }

    // Each verdict is delivered exactly once — the read retires the row,
    // so a second read reports ENOENT like any vanished ask.
    try std.testing.expectEqual(@as(u64, ask_committed), status(ids[0], pid).?);
    try std.testing.expectEqual(@as(u64, ask_dropped), status(ids[1], pid).?);
    try std.testing.expect(status(ids[0], pid) == null);
    try std.testing.expectEqual(@as(u64, ask_committed), status(ids[2], pid).?);
    try std.testing.expectEqual(@as(u64, ask_dropped), status(ids[3], pid).?);

    // The fifth ask now fits: the table recovers inside the boot, and
    // the two consents landed in the store.
    const r5 = ask(pid, process.uid_user, "ASKER.ELF", "fifth\t1000\tv");
    try std.testing.expect(r5 == .id);
    try std.testing.expectEqual(@as(usize, 2), count());
}

test "secret: slot-96 persist failure keeps the ask standing (EIO) (#3351)" {
    init();
    test_persist = false;
    defer test_persist = null;
    const pid = process.create_as("ASKER.ELF", .{}, .{}, .{}, .{ .uid = process.uid_user }).?;
    _ = ask(pid, process.uid_user, "ASKER.ELF", "k\t1000\tv");
    try std.testing.expectEqual(CommitResult.eio, commit(1, "GOTABWM.ELF"));
    // The in-memory set landed; the ask stays pending for a retry, and
    // status still reads pending — a torn write is never reported landed.
    try std.testing.expectEqual(@as(usize, 1), count());
    try std.testing.expectEqual(@as(u64, ask_pending), status(1, pid).?);
    try std.testing.expectEqual(CommitResult.eio, set_direct("GOTABWM.ELF", "j\t1000\tw"));
}
