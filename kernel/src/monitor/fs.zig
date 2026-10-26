//! Monitor commands: host-share filesystem (`ls`/`cat`/`mount`/`write`/`mktemp`/`stat`/`du`/`find`/`inventory`).
//!
//! Split out of monitor.zig (#3755): declarations only — the
//! shared types, registry, dispatch, completion, and print/parse
//! helpers stay in the root and are aliased below so the moved
//! code is verbatim.

const std = @import("std");
const mon = @import("../monitor.zig");

const Monitor = mon.Monitor;
const ExecError = mon.ExecError;
const err_prefix = mon.err_prefix;
const print_usage = mon.print_usage;
const kernel_vf_allowed = mon.kernel_vf_allowed;
const trust_denied = mon.trust_denied;
const lookup = mon.lookup;
const virtio_file = mon.virtio_file;
const csprng = mon.csprng;
const file_table = mon.file_table;
const vlfs_disk = @import("../vlfs/disk.zig");
const vf = @import("vf.zig");
const arg_is_disk = vf.arg_is_disk;
const vf_status_text = vf.vf_status_text;
const disk_verb_err = vf.disk_verb_err;
const cmd_disk_stat = vf.cmd_disk_stat;
// ---------------------------------------------------------------------------
// ESP file-window commands (claim 3475 hard gate 5; FAT-backed since
// claim 6420)
// ---------------------------------------------------------------------------

/// List files: no argument lists the ESP root window (unchanged behavior);
/// a `/`-path lists a subdirectory straight from the FAT volume (milestone
/// four card 2 Stage C). Deterministic and grep-able.
/// M22 D15 (issue #338): `ls [-l] [<dir>]` — list files with optional
/// long listing format showing type, permissions, owner, size, and name.
pub fn cmd_ls(m: *Monitor, args: []const []const u8) ExecError {
    var long_mode = false;
    var path_arg: ?[]const u8 = null;
    for (args) |arg| {
        if (std.mem.eql(u8, arg, "-l")) {
            long_mode = true;
        } else {
            path_arg = arg;
        }
    }
    if (path_arg) |path| return cmd_ls_path_long(m, path, long_mode);
    // Root listing — M34 HF6 (issue #740): the share is the only store.
    var res: virtio_file.ListResult = .{};
    const st = virtio_file.list("", &res);
    if (st != virtio_file.st_ok) {
        m.console.print_line("ls: no host file channel (boot the runner with --cvc-file <host-dir>)");
        return .none;
    }
    m.console.puts("ls: host=");
    m.console.print_hex(@intCast(res.count));
    m.console.puts("\n");
    if (res.count == 0) {
        m.console.print_line("ls: no files on the host share");
        return .none;
    }
    if (long_mode) {
        for (res.entries[0..res.count]) |e| {
            print_ls_long_entry(m, e.name[0..e.name_len], e.size, e.type == virtio_file.dir_type_dir);
        }
    } else {
        var width: usize = 0;
        for (res.entries[0..res.count]) |e| width = @max(width, e.name_len);
        for (res.entries[0..res.count]) |e| {
            m.console.puts("  ");
            m.console.puts(e.name[0..e.name_len]);
            var pad: usize = e.name_len;
            while (pad < width) : (pad += 1) m.console.putc(' ');
            m.console.puts("  ");
            m.console.print_hex(e.size);
            m.console.puts("  [");
            m.console.puts(if (e.type == virtio_file.dir_type_dir) "dir" else "host");
            m.console.puts("]\n");
        }
    }
    return .none;
}

/// List a directory by `/`-path, straight from the FAT volume (the window
/// only snapshots the root). Same row format as the root listing, with a
/// path-aware header. Honest diagnostics distinguish a file ("is a file")
/// from an absent/non-directory path ("not found").
fn cmd_ls_path(m: *Monitor, path: []const u8) ExecError {
    return cmd_ls_path_long(m, path, false);
}

fn cmd_ls_path_long(m: *Monitor, path: []const u8, long_mode: bool) ExecError {
    // M34 HF6 (issue #740): LIST the share directory through the channel.
    var res: virtio_file.ListResult = .{};
    const st = virtio_file.list(path, &res);
    if (st != virtio_file.st_ok) {
        err_prefix(m);
        m.console.puts(path);
        if (st == virtio_file.st_not_found) {
            m.console.print_line(": not found (no such directory on the host share)");
        } else {
            m.console.puts(": ");
            m.console.print_line(vf_status_text(st));
        }
        return .invalid_argument;
    }
    const n = res.count;
    m.console.puts("ls: ");
    m.console.puts(path);
    m.console.puts(" entries=");
    m.console.print_hex(@intCast(n));
    m.console.puts("\n");
    if (long_mode) {
        for (res.entries[0..n]) |e| {
            print_ls_long_entry(m, e.name[0..e.name_len], e.size, e.type == virtio_file.dir_type_dir);
        }
    } else {
        var width: usize = 0;
        for (res.entries[0..n]) |e| width = @max(width, e.name_len);
        for (res.entries[0..n]) |e| {
            m.console.puts("  ");
            m.console.puts(e.name[0..e.name_len]);
            var pad: usize = e.name_len;
            while (pad < width) : (pad += 1) m.console.putc(' ');
            m.console.puts("  ");
            m.console.print_hex(e.size);
            m.console.puts("  [");
            m.console.puts(if (e.type == virtio_file.dir_type_dir) "dir" else "host");
            m.console.puts("]\n");
        }
    }
    return .none;
}

/// M22 D15: print one entry in `ls -l` long format.
fn print_ls_long_entry(m: *Monitor, name: []const u8, size: u64, is_dir: bool) void {
    // Type + permissions
    m.console.putc(if (is_dir) 'd' else '-');
    if (is_dir) {
        m.console.print_line("rwx     1  root");
    } else {
        m.console.print_line("rw-     1  root");
    }
    // Indent + size + name
    m.console.puts("        ");
    m.console.print_u64(size);
    var pad_buf: [20]u8 = undefined;
    var sz_digits: usize = 0;
    var v = size;
    if (v == 0) {
        pad_buf[0] = '0';
        sz_digits = 1;
    }
    while (v > 0) : (v /= 10) {
        pad_buf[sz_digits] = @intCast('0' + v % 10);
        sz_digits += 1;
    }
    // Pad to 8 chars
    var i: usize = sz_digits;
    while (i < 8) : (i += 1) m.console.putc(' ');
    m.console.puts("  ");
    m.console.print_line(name);
}

/// Switch the active FAT volume (milestone four card 2): `mount esp`
/// re-mounts the EFI System Partition by its GPT type GUID; `mount data`
/// mounts the second FAT32 partition (the general-filesystem volume) by
/// its GUID. The window re-snapshots the newly active volume's root,
/// M34 HF6 (issue #740): the FAT volumes are GONE — `mount` reports the
/// host share's state (the command row stays for the storage-category
/// surface; there is nothing to switch).
pub fn cmd_mount(m: *Monitor, args: []const []const u8) ExecError {
    _ = args;
    if (!virtio_file.available()) {
        m.console.print_line("mount: no host file channel (boot the runner with --cvc-file <host-dir>)");
        return .none;
    }
    var res: virtio_file.ListResult = .{};
    const st = virtio_file.list("", &res);
    m.console.puts("mount: host share armed files=");
    m.console.print_hex(if (st == virtio_file.st_ok) @as(u64, @intCast(res.count)) else 0);
    m.console.puts("\n");
    return .none;
}

/// Print a file's content from the HOST SHARE (M34 HF6, issue #740: the
/// ESP window and FAT volume are gone — the share is the only store).
/// Honest diagnostics for directories and files larger than the bounded
/// read buffer.
pub fn cmd_cat(m: *Monitor, args: []const []const u8) ExecError {
    const name = args[0];
    // M50 TS2 (ADR 0024 D8): the monitor's own read of a secret-class path
    // is denied for every actor.
    if (!kernel_vf_allowed(name, .read)) return trust_denied(m, "cat", name);
    var st = virtio_file.StatResult{};
    if (virtio_file.stat(name, &st) != virtio_file.st_ok) {
        err_prefix(m);
        m.console.puts(name);
        m.console.print_line(": not found (no such file on the host share)");
        return .invalid_argument;
    }
    if (st.is_dir) {
        err_prefix(m);
        m.console.puts(name);
        m.console.print_line(": is a directory");
        return .invalid_argument;
    }
    var buf: [2048]u8 = undefined;
    if (st.size > @as(u64, @intCast(buf.len))) {
        err_prefix(m);
        m.console.puts(name);
        m.console.puts(": file is ");
        m.console.print_hex(st.size);
        m.console.puts(" bytes; direct read caps at ");
        m.console.print_hex(buf.len);
        m.console.print_line(" bytes");
        return .invalid_argument;
    }
    const got = virtio_file.read_into(name, st.size, &buf) orelse {
        err_prefix(m);
        m.console.puts(name);
        m.console.print_line(": not found (no such file on the host share)");
        return .invalid_argument;
    };
    m.console.puts(buf[0..got]);
    // A trailing newline keeps the shell prompt on its own line; a file
    // that already ends with one is printed verbatim (no double blank).
    if (got == 0 or buf[got - 1] != '\n') m.console.puts("\n");
    return .none;
}

/// Print a file by path, read from the HOST SHARE (kept as the `/`-path
/// entry point — the channel accepts share-relative paths with slashes).
fn cmd_cat_path(m: *Monitor, path: []const u8) ExecError {
    return cmd_cat(m, &.{path});
}

/// Write text to the ESP's FAT32 volume (claim 6420 — the real storage
/// driver replacing claim 3475's NVRAM variables): the file survives
/// reboot because it is on the disk itself. Capacity is checked before the
/// write; a failed sector write is reported honestly, never faked.
// M34 HF1+HF2 (issues #735/#736): the HOST FILE CHANNEL (`vf ls`/`vf cat`).
// The guest userland filesystem is a macOS folder served over custom-virtio
// queue 5 (runner flag --cvc-file <host-dir>); default boots print the
// honest "no host file channel" line and stay byte-identical.
//
// M34 HF3 (issue #737): mutation verbs. OPEN/CLOSE/WRITE/TRUNCATE/FSYNC
// ride the HOST's 8-slot handle table (cursors live there — parity with
// file_table.zig); `vf write <h> <n>` streams n deterministic probe-pattern
// bytes through the cursor (the gate's python verifies the same bytes on
// the host disk). `vf mkdir/rm/mv` are stateless path ops.
// ---------------------------------------------------------------------------

/// Write text to a file on the HOST SHARE (M34 HF5/HF6, issues
/// #739/#740): write_whole = open/create + truncate + chunked write +
/// close through the queue-5 channel — the file persists on the macOS
/// host disk, verified by the gates. Capacity is checked before the
/// write; a failed exchange is reported honestly, never faked.
/// Issue #803: 32 KiB staging can never live on the 16 KiB kernel stack
/// (ADR 0004 D5). The write path is reached from the shell's merged
/// boot_and_park frame; the local buffer overflowed the boot stack and
/// corrupted the console state mid-print. Module BSS, like vf_write_buf.
pub fn cmd_write(m: *Monitor, args: []const []const u8) ExecError {
    const name = args[0];
    const parts = args[1..];
    var len: usize = 0;
    for (parts, 0..) |p, i| len += p.len + (if (i > 0) @as(usize, 1) else 0);
    if (len > virtio_file.reply_cap) {
        err_prefix(m);
        m.console.puts("content too long (max ");
        m.console.print_hex(virtio_file.reply_cap);
        m.console.puts(" bytes, got ");
        m.console.print_hex(@intCast(len));
        m.console.print_line(")");
        return .invalid_argument;
    }
    const buf = &vf.cmd_write_buf;
    var n: usize = 0;
    for (parts, 0..) |p, i| {
        if (i > 0) {
            buf[n] = ' ';
            n += 1;
        }
        @memcpy(buf[n..][0..p.len], p);
        n += p.len;
    }
    // M100d (issue #2309): `write /disk/<path> <text>` is a journaled
    // whole-file replace + FLUSH on the mounted volume.
    if (arg_is_disk(name)) {
        const parsed = file_table.parse_path(name).?;
        const subpath = parsed.path[0..parsed.parsed_len()];
        vlfs_disk.writeWhole(subpath, buf[0..n]) catch |err| return disk_verb_err(m, "write", subpath, err);
        m.console.puts("write: ok (journaled ");
        m.console.print_u64(n);
        m.console.puts(" bytes to disk:");
        m.console.puts(subpath);
        m.console.puts(")\n");
        return .none;
    }
    if (!kernel_vf_allowed(name, .write)) return trust_denied(m, "write", name);
    const st = virtio_file.write_whole(name, buf[0..n]);
    if (st == virtio_file.st_ok) {
        m.console.puts("write: ok (persisted ");
        m.console.print_u64(n);
        m.console.puts(" bytes to the host share)\n");
        return .none;
    }
    err_prefix(m);
    m.console.puts(name);
    m.console.puts(": not persisted - ");
    m.console.print_line(vf_status_text(st));
    return .not_implemented;
}

/// M19 P16 (issue #305): create a temporary file with a unique name.
/// Usage: mktemp [prefix] - creates prefix_XXXX.BIN (default TMP_XXXX.BIN).
pub fn cmd_mktemp(m: *Monitor, args: []const []const u8) ExecError {
    const prefix = if (args.len > 0) args[0] else "TMP";
    // Generate 4 random hex digits.
    var rnd: [2]u8 = undefined;
    csprng.random_bytes(&rnd);
    const hex = "0123456789abcdef";
    var suffix: [4]u8 = undefined;
    suffix[0] = hex[(rnd[0] >> 4) & 0x0f];
    suffix[1] = hex[rnd[0] & 0x0f];
    suffix[2] = hex[(rnd[1] >> 4) & 0x0f];
    suffix[3] = hex[rnd[1] & 0x0f];
    // Build filename: PREFIX_XXXX.BIN
    var name_buf: [32]u8 = undefined;
    var n: usize = 0;
    for (prefix) |b| {
        if (n < name_buf.len - 10) {
            name_buf[n] = b;
            n += 1;
        }
    }
    if (n < name_buf.len - 10) {
        name_buf[n] = '_';
        n += 1;
    }
    @memcpy(name_buf[n..][0..4], &suffix);
    n += 4;
    @memcpy(name_buf[n..][0..4], ".BIN");
    n += 4;
    const name = name_buf[0..n];
    // Create the empty file on the host share (HF6: the ESP window is gone).
    if (!kernel_vf_allowed(name, .create)) return trust_denied(m, "mktemp", name);
    const st = virtio_file.write_whole(name, "");
    if (st == virtio_file.st_ok) {
        m.console.puts(name);
        m.console.puts("\n");
        return .none;
    }
    err_prefix(m);
    m.console.puts(name);
    m.console.puts(": ");
    m.console.print_line(vf_status_text(st));
    return .not_implemented;
}

// ---------------------------------------------------------------------------
// M22 Lane-D: filesystem inspection (D8), log viewer (D12), timing (D13),
// command locator (D16)
// ---------------------------------------------------------------------------

/// M22 D8 (issue #331): `stat <file|path>` — print file metadata from the
/// HOST SHARE (M34 HF6, issue #740: the FAT volume and ESP window are
/// gone): size, type (file/dir), and path.
pub fn cmd_stat(m: *Monitor, args: []const []const u8) ExecError {
    const name = args[0];
    if (!kernel_vf_allowed(name, .read)) return trust_denied(m, "stat", name);
    if (file_table.parse_path(name)) |parsed| {
        if (parsed.partition == .disk) return cmd_disk_stat(m, name, parsed.path[0..parsed.parsed_len()]);
    }
    var st = virtio_file.StatResult{};
    const sst = virtio_file.stat(name, &st);
    if (sst != virtio_file.st_ok or st.is_dir) {
        err_prefix(m);
        m.console.puts("stat: ");
        m.console.puts(name);
        m.console.print_line(": not found");
        return .invalid_argument;
    }
    m.console.puts("  File:    ");
    m.console.print_line(name);
    m.console.puts("  Size:    ");
    m.console.print_u64(st.size);
    m.console.print_line(" bytes");
    m.console.puts("  Type:    ");
    m.console.print_line("regular file");
    m.console.puts("  Volume:  ");
    m.console.print_line("host share");
    return .none;
}

/// M25 F4 (issue #384): `du [<path>]` — estimate file space usage recursively
/// (bounded: 3 levels deep).
/// M25 F4 (issue #384): `du [<path>]` — estimate file space usage
/// recursively on the HOST SHARE (bounded: 3 levels deep, like the FAT
/// walk it replaces — HF6, issue #740).
const du_max_depth: usize = 3;

fn du_walk(m: *Monitor, path: []const u8, depth: usize, bytes: *u64, dirs: *u64) void {
    var res: virtio_file.ListResult = .{};
    if (virtio_file.list(path, &res) != virtio_file.st_ok) return;
    for (res.entries[0..res.count]) |e| {
        if (e.type == virtio_file.dir_type_dir) {
            if (depth < du_max_depth) {
                dirs.* += 1;
                var sub: [128]u8 = undefined;
                const sub_path = if (path.len == 0)
                    e.name[0..@min(e.name_len, sub.len)]
                else blk: {
                    const take = @min(path.len, sub.len - 1 - e.name_len);
                    @memcpy(sub[0..take], path[0..take]);
                    var p = take;
                    if (p < sub.len and sub[p - 1] != '/') {
                        sub[p] = '/';
                        p += 1;
                    }
                    const nt = @min(e.name_len, sub.len - p);
                    @memcpy(sub[p..][0..nt], e.name[0..nt]);
                    p += nt;
                    break :blk sub[0..p];
                };
                du_walk(m, sub_path, depth + 1, bytes, dirs);
            }
        } else {
            bytes.* += e.size;
        }
    }
}

pub fn cmd_du(m: *Monitor, args: []const []const u8) ExecError {
    const path = if (args.len > 0) args[0] else "";
    var bytes: u64 = 0;
    var dirs: u64 = 0;
    du_walk(m, path, 0, &bytes, &dirs);
    m.console.puts("du: ");
    m.console.puts(if (path.len > 0) path else "/");
    m.console.puts(" ");
    m.console.print_u64(bytes);
    m.console.puts(" bytes (dirs=");
    m.console.print_u64(dirs);
    m.console.print_line(")");
    return .none;
}

/// M22 D8 (issue #331): `find <dir> -name <pattern>` — recursive file
/// search with simple glob patterns (* and ?). Bounded: 3 levels deep,
/// 256 results max.
pub fn cmd_find(m: *Monitor, args: []const []const u8) ExecError {
    const start_path = args[0];
    // args[1] should be "-name"
    if (!std.mem.eql(u8, args[1], "-name")) {
        err_prefix(m);
        m.console.print_line("find: expected '-name' keyword");
        print_usage(m, lookup("find").?);
        return .usage;
    }
    const pattern = args[2];
    var count: usize = 0;
    find_recursive(m, start_path, pattern, 0, &count);
    if (count == 0) {
        m.console.puts("find: no matches for '");
        m.console.puts(pattern);
        m.console.print_line("'");
    }
    return .none;
}

const find_max_depth: usize = 3;

const find_max_results: usize = 256;

fn find_recursive(m: *Monitor, dir_path: []const u8, pattern: []const u8, depth: usize, count: *usize) void {
    if (depth >= find_max_depth or count.* >= find_max_results) return;
    // M34 HF6 (issue #740): find walks the HOST SHARE (the FAT volume is gone).
    var res: virtio_file.ListResult = .{};
    if (virtio_file.list(dir_path, &res) != virtio_file.st_ok) return;
    for (res.entries[0..res.count]) |e| {
        if (count.* >= find_max_results) break;
        const name = e.name[0..e.name_len];
        // Match against pattern
        if (glob_match(name, pattern)) {
            // Build full path
            var path_buf: [128]u8 = undefined;
            var pos: usize = 0;
            const is_dir = e.type == virtio_file.dir_type_dir;
            if (std.mem.eql(u8, dir_path, "/")) {
                path_buf[0] = '/';
                pos = 1;
            } else {
                const take = @min(dir_path.len, path_buf.len - 1 - name.len);
                @memcpy(path_buf[0..take], dir_path[0..take]);
                pos = take;
                if (pos < path_buf.len and path_buf[pos - 1] != '/') {
                    path_buf[pos] = '/';
                    pos += 1;
                }
            }
            const name_take = @min(name.len, path_buf.len - pos);
            @memcpy(path_buf[pos..][0..name_take], name[0..name_take]);
            pos += name_take;
            m.console.puts(path_buf[0..pos]);
            if (is_dir) m.console.putc('/');
            m.console.putc('\n');
            count.* += 1;
        }
        // Recurse into directories
        if (e.type == virtio_file.dir_type_dir) {
            var sub_path: [128]u8 = undefined;
            var spos: usize = 0;
            if (std.mem.eql(u8, dir_path, "/")) {
                sub_path[0] = '/';
                spos = 1;
            } else {
                const take = @min(dir_path.len, sub_path.len - 1 - name.len);
                @memcpy(sub_path[0..take], dir_path[0..take]);
                spos = take;
                if (spos < sub_path.len and sub_path[spos - 1] != '/') {
                    sub_path[spos] = '/';
                    spos += 1;
                }
            }
            const name_take = @min(name.len, sub_path.len - spos);
            @memcpy(sub_path[spos..][0..name_take], name[0..name_take]);
            spos += name_take;
            find_recursive(m, sub_path[0..spos], pattern, depth + 1, count);
        }
    }
}

/// Simple glob pattern match: * matches any sequence, ? matches one char.
fn glob_match(name: []const u8, pattern: []const u8) bool {
    return glob_match_impl(name, 0, pattern, 0);
}

fn glob_match_impl(name: []const u8, ni: usize, pat: []const u8, pi: usize) bool {
    if (pi == pat.len) return ni == name.len;
    if (pat[pi] == '*') {
        // Try matching zero or more chars
        var j: usize = ni;
        while (j <= name.len) : (j += 1) {
            if (glob_match_impl(name, j, pat, pi + 1)) return true;
        }
        return false;
    }
    if (ni >= name.len) return false;
    if (pat[pi] == '?' or pat[pi] == name[ni]) {
        return glob_match_impl(name, ni + 1, pat, pi + 1);
    }
    return false;
}

/// M22 D16 (issue #339): `inventory` — list all installed applications
/// from the HOST SHARE with their sizes and types (HF6: the ESP is gone).
pub fn cmd_inventory(m: *Monitor, args: []const []const u8) ExecError {
    _ = args;
    var res: virtio_file.ListResult = .{};
    if (virtio_file.list("", &res) != virtio_file.st_ok or res.count == 0) {
        m.console.print_line("inventory: no applications on the host share");
        return .none;
    }
    m.console.puts("inventory: ");
    m.console.print_u64(@intCast(res.count));
    m.console.print_line(" application(s):\n");
    var width: usize = 0;
    for (res.entries[0..res.count]) |e| width = @max(width, e.name_len);
    for (res.entries[0..res.count]) |e| {
        m.console.puts("  ");
        m.console.puts(e.name[0..e.name_len]);
        var pad: usize = e.name_len;
        while (pad < width + 2) : (pad += 1) m.console.putc(' ');
        m.console.puts("  ");
        m.console.print_hex(e.size);
        m.console.puts(" bytes  [");
        m.console.puts(if (e.type == virtio_file.dir_type_dir) "dir" else "host");
        m.console.puts("]\n");
    }
    return .none;
}
