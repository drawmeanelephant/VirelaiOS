//! Monitor commands: the `vf` host-file-channel verb tree and the /disk (VLFS) path ops.
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
const parseInt = mon.parseInt;
const kernel_vf_allowed = mon.kernel_vf_allowed;
const trust_denied = mon.trust_denied;
const virtio_file = mon.virtio_file;
const file_table = mon.file_table;
const vlfs = @import("../vlfs/vlfs.zig");
const vlfs_disk = @import("../vlfs/disk.zig");

/// Shared write-path scratch (host-share `write` and `vf write` disk
/// chunks both stage through it).
pub var cmd_write_buf: [virtio_file.reply_cap]u8 align(16) = undefined;
pub fn cmd_vf(m: *Monitor, args: []const []const u8) ExecError {
    if (args.len == 0) return cmd_vf_usage(m);
    // M100d (issue #2309): a /disk path routes to the VLFS volume whether
    // or not the host channel is attached — the mount stands alone, so
    // the availability check fires only for verbs with no disk operand.
    const disk_target = args.len > 1 and arg_is_disk(args[1]);
    if (!virtio_file.available() and !disk_target) {
        m.console.print_line("vf: no host file channel (boot the runner with --cvc-file <host-dir>)");
        return .none;
    }
    if (std.mem.eql(u8, args[0], "ls")) return cmd_vf_ls(m, args[1..]);
    if (std.mem.eql(u8, args[0], "cat")) return cmd_vf_cat(m, args[1..]);
    if (std.mem.eql(u8, args[0], "open")) return cmd_vf_open(m, args[1..]);
    if (std.mem.eql(u8, args[0], "close")) return cmd_vf_close(m, args[1..]);
    if (std.mem.eql(u8, args[0], "write")) return cmd_vf_write(m, args[1..]);
    if (std.mem.eql(u8, args[0], "truncate")) return cmd_vf_truncate(m, args[1..]);
    if (std.mem.eql(u8, args[0], "fsync")) return cmd_vf_fsync(m, args[1..]);
    if (std.mem.eql(u8, args[0], "mkdir")) return cmd_vf_mkdir(m, args[1..]);
    if (std.mem.eql(u8, args[0], "rm")) return cmd_vf_rm(m, args[1..]);
    if (std.mem.eql(u8, args[0], "mv")) return cmd_vf_mv(m, args[1..]);
    if (std.mem.eql(u8, args[0], "clone")) return cmd_vf_clone(m, args[1..]);
    return cmd_vf_usage(m);
}

/// Does this argument name a /disk path? (nil/parse failures are host-side.)
pub fn arg_is_disk(arg: []const u8) bool {
    const parsed = file_table.parse_path(arg) orelse return false;
    return parsed.partition == .disk;
}

fn cmd_vf_usage(m: *Monitor) ExecError {
    err_prefix(m);
    m.console.print_line("vf usage: vf ls [<path>] | cat <path> | mkdir <path> | rm <path> | mv <from> <to> | clone <from> <to> | open <path> [append] | close <h> | write <h> <n> | truncate <h> <n> | fsync <h>");
    return .invalid_argument;
}

pub fn vf_status_text(st: u8) []const u8 {
    return switch (st) {
        virtio_file.st_not_found => "not found on the host share",
        virtio_file.st_is_dir => "is a directory",
        virtio_file.st_exists => "already exists on the host share",
        virtio_file.st_handle => "bad handle or host handle table full (8 open handles)",
        virtio_file.st_truncated => "reply truncated (file larger than the 32 KiB per-round-trip cap)",
        else => "host file-channel error",
    };
}

fn vf_err(m: *Monitor, verb: []const u8, path: []const u8, st: u8) ExecError {
    err_prefix(m);
    m.console.puts("vf ");
    m.console.puts(verb);
    m.console.puts(": ");
    m.console.puts(path);
    m.console.puts(": ");
    m.console.print_line(vf_status_text(st));
    return .invalid_argument;
}

/// `vf open <path> [append]` — create-if-missing read+write handle.
fn cmd_vf_open(m: *Monitor, args: []const []const u8) ExecError {
    if (args.len == 0) return cmd_vf_usage(m);
    const path = args[0];
    if (arg_is_disk(path)) {
        // The disk has no monitor handle table — writes go through the
        // whole-file verbs (`write`, `vf write <path> <n>`) or the file ABI.
        err_prefix(m);
        m.console.puts("vf open: ");
        m.console.puts(path);
        m.console.print_line(": no handle form on /disk — use 'vf write <path> <n>'");
        return .invalid_argument;
    }
    if (!kernel_vf_allowed(path, .create)) return trust_denied(m, "vf open", path);
    var flags: u8 = virtio_file.open_flag_create;
    if (args.len > 1 and std.mem.eql(u8, args[1], "append")) flags |= virtio_file.open_flag_append;
    var handle: u16 = 0;
    const st = virtio_file.open(path, flags, &handle);
    if (st != virtio_file.st_ok) return vf_err(m, "open", path, st);
    m.console.puts("vf: open ");
    m.console.puts(path);
    m.console.puts(if ((flags & virtio_file.open_flag_append) != 0) " append" else "");
    m.console.puts(" h=");
    m.console.print_u64(handle);
    m.console.puts("\n");
    return .none;
}

/// `vf close <h>` — flush + free the host slot.
fn cmd_vf_close(m: *Monitor, args: []const []const u8) ExecError {
    if (args.len == 0) return cmd_vf_usage(m);
    const handle = parseInt(args[0]) catch return cmd_vf_usage(m);
    // M97d-F4 (#2089): the host handle is a u16 — a wider console argument
    // is a usage error, never a silent ReleaseSmall truncation onto another
    // process's slot in the host's GLOBAL handle table.
    if (handle > std.math.maxInt(u16)) return cmd_vf_usage(m);
    const st = virtio_file.close(@intCast(handle));
    if (st != virtio_file.st_ok) return vf_err(m, "close", args[0], st);
    m.console.puts("vf: close ");
    m.console.puts(args[0]);
    m.console.print_line(" ok");
    return .none;
}

/// `vf write <h> <n>` — stream n probe-pattern bytes through the cursor;
/// `vf write /disk/<path> <n>` — the same fill on the VLFS volume.
fn cmd_vf_write(m: *Monitor, args: []const []const u8) ExecError {
    if (args.len < 2) return cmd_vf_usage(m);
    if (arg_is_disk(args[0])) {
        const n = parseInt(args[1]) catch return cmd_vf_usage(m);
        if (n == 0) return cmd_vf_usage(m);
        const parsed = file_table.parse_path(args[0]).?;
        return cmd_disk_write(m, parsed.path[0..parsed.parsed_len()], n);
    }
    const handle = parseInt(args[0]) catch return cmd_vf_usage(m);
    const n = parseInt(args[1]) catch return cmd_vf_usage(m);
    if (n == 0) return cmd_vf_usage(m);
    if (handle > std.math.maxInt(u16)) return cmd_vf_usage(m); // #2089
    const res = virtio_file.write_pattern(@intCast(handle), n);
    if (res.status != virtio_file.st_ok) return vf_err(m, "write", args[0], res.status);
    m.console.puts("vf: write ");
    m.console.puts(args[0]);
    m.console.puts(" n=");
    m.console.print_u64(n);
    m.console.puts(" wrote=");
    m.console.print_u64(res.total);
    m.console.puts(" chunks=");
    m.console.print_u64(res.chunks);
    m.console.puts("\n");
    return .none;
}

/// `vf truncate <h> <n>` — set the file size through the handle;
/// `vf truncate /disk/<path> <n>` — the journaled resize on VLFS.
fn cmd_vf_truncate(m: *Monitor, args: []const []const u8) ExecError {
    if (args.len < 2) return cmd_vf_usage(m);
    if (arg_is_disk(args[0])) {
        const size = parseInt(args[1]) catch return cmd_vf_usage(m);
        const parsed = file_table.parse_path(args[0]).?;
        const subpath = parsed.path[0..parsed.parsed_len()];
        const found = vlfs_disk.lookup(subpath) catch |err| return disk_verb_err(m, "vf truncate", subpath, err);
        if (found.kind == vlfs.kind_dir) return disk_verb_err(m, "vf truncate", subpath, error.IsDir);
        vlfs_disk.truncate(found.inode, @intCast(@min(size, std.math.maxInt(u32)))) catch |err| return disk_verb_err(m, "vf truncate", subpath, err);
        m.console.puts("vf: truncate disk:");
        m.console.puts(subpath);
        m.console.puts(" size=");
        m.console.print_u64(size);
        m.console.print_line(" ok");
        return .none;
    }
    const handle = parseInt(args[0]) catch return cmd_vf_usage(m);
    const size = parseInt(args[1]) catch return cmd_vf_usage(m);
    if (handle > std.math.maxInt(u16)) return cmd_vf_usage(m); // #2089
    const st = virtio_file.truncate(@intCast(handle), size);
    if (st != virtio_file.st_ok) return vf_err(m, "truncate", args[0], st);
    m.console.puts("vf: truncate ");
    m.console.puts(args[0]);
    m.console.puts(" size=");
    m.console.print_u64(size);
    m.console.print_line(" ok");
    return .none;
}

/// `vf fsync <h>` — real durability on the host fd; `vf fsync /disk[...]` —
/// the virtio-blk FLUSH barrier on the mounted VLFS volume.
fn cmd_vf_fsync(m: *Monitor, args: []const []const u8) ExecError {
    if (args.len == 0) return cmd_vf_usage(m);
    if (arg_is_disk(args[0])) {
        vlfs_disk.sync() catch |err| return disk_verb_err(m, "vf fsync", args[0], err);
        m.console.puts("vf: fsync ");
        m.console.puts(args[0]);
        m.console.print_line(" ok (FLUSH completed)");
        return .none;
    }
    const handle = parseInt(args[0]) catch return cmd_vf_usage(m);
    if (handle > std.math.maxInt(u16)) return cmd_vf_usage(m); // #2089
    const st = virtio_file.fsync(@intCast(handle));
    if (st != virtio_file.st_ok) return vf_err(m, "fsync", args[0], st);
    m.console.puts("vf: fsync ");
    m.console.puts(args[0]);
    m.console.print_line(" ok");
    return .none;
}

/// `vf mkdir <path>` — one level; parents must exist.
fn cmd_vf_mkdir(m: *Monitor, args: []const []const u8) ExecError {
    if (args.len == 0) return cmd_vf_usage(m);
    const path = args[0];
    if (file_table.parse_path(path)) |parsed| {
        if (parsed.partition == .disk) {
            const subpath = parsed.path[0..parsed.parsed_len()];
            _ = vlfs_disk.create(subpath, vlfs.kind_dir) catch |err| return disk_verb_err(m, "vf mkdir", subpath, err);
            m.console.puts("vf: mkdir disk:");
            m.console.puts(subpath);
            m.console.print_line(" ok");
            return .none;
        }
    }
    if (!kernel_vf_allowed(path, .create)) return trust_denied(m, "vf mkdir", path);
    const st = virtio_file.mkdir(path);
    if (st != virtio_file.st_ok) return vf_err(m, "mkdir", path, st);
    m.console.puts("vf: mkdir ");
    m.console.puts(path);
    m.console.print_line(" ok");
    return .none;
}

/// `vf rm <path>` — delete a file or EMPTY directory.
fn cmd_vf_rm(m: *Monitor, args: []const []const u8) ExecError {
    if (args.len == 0) return cmd_vf_usage(m);
    const path = args[0];
    if (file_table.parse_path(path)) |parsed| {
        if (parsed.partition == .disk) {
            const subpath = parsed.path[0..parsed.parsed_len()];
            _ = vlfs_disk.remove(subpath) catch |err| return disk_verb_err(m, "vf rm", subpath, err);
            m.console.puts("vf: rm disk:");
            m.console.puts(subpath);
            m.console.print_line(" ok");
            return .none;
        }
    }
    if (!kernel_vf_allowed(path, .delete)) return trust_denied(m, "vf rm", path);
    const st = virtio_file.delete(path);
    if (st != virtio_file.st_ok) return vf_err(m, "rm", path, st);
    m.console.puts("vf: rm ");
    m.console.puts(path);
    m.console.print_line(" ok");
    return .none;
}

/// `vf mv <from> <to>` — rename on the host share.
fn cmd_vf_mv(m: *Monitor, args: []const []const u8) ExecError {
    if (args.len < 2) return cmd_vf_usage(m);
    const from = args[0];
    const to = args[1];
    const from_disk = arg_is_disk(from);
    const to_disk = arg_is_disk(to);
    if (from_disk or to_disk) {
        // Same-volume only — a cross-partition move has no rename op.
        if (!(from_disk and to_disk)) {
            err_prefix(m);
            m.console.puts("vf mv: ");
            m.console.puts(from);
            m.console.puts(" -> ");
            m.console.puts(to);
            m.console.print_line(": cross-partition rename refused");
            return .invalid_argument;
        }
        const fp = file_table.parse_path(from).?;
        const tp = file_table.parse_path(to).?;
        const fs = fp.path[0..fp.parsed_len()];
        const ts = tp.path[0..tp.parsed_len()];
        vlfs_disk.rename(fs, ts, false) catch |err| return disk_verb_err(m, "vf mv", fs, err);
        m.console.puts("vf: mv disk:");
        m.console.puts(fs);
        m.console.puts(" -> ");
        m.console.puts(ts);
        m.console.print_line(" ok");
        return .none;
    }
    if (!kernel_vf_allowed(from, .delete)) return trust_denied(m, "vf mv", from);
    if (!kernel_vf_allowed(to, .create)) return trust_denied(m, "vf mv", to);
    const st = virtio_file.rename(from, to);
    if (st != virtio_file.st_ok) return vf_err(m, "mv", from, st);
    m.console.puts("vf: mv ");
    m.console.puts(from);
    m.console.puts(" -> ");
    m.console.puts(to);
    m.console.print_line(" ok");
    return .none;
}

/// `vf clone <from> <to>` — APFS COW clone on the host share (HF7, issue
/// #741): the worktree workflow. `to` must not exist; N clones of one
/// repo share blocks until a worktree actually edits a file (the host
/// clones with clonefile/copyfile(COPYFILE_CLONE) — measured live by the
/// HF7 gate). File or whole directory tree.
fn cmd_vf_clone(m: *Monitor, args: []const []const u8) ExecError {
    if (args.len < 2) return cmd_vf_usage(m);
    const from = args[0];
    const to = args[1];
    if (!kernel_vf_allowed(from, .read)) return trust_denied(m, "vf clone", from);
    if (!kernel_vf_allowed(to, .create)) return trust_denied(m, "vf clone", to);
    const st = virtio_file.clone(from, to);
    if (st != virtio_file.st_ok) return vf_err(m, "clone", from, st);
    m.console.puts("vf: clone ");
    m.console.puts(from);
    m.console.puts(" -> ");
    m.console.puts(to);
    m.console.print_line(" ok");
    return .none;
}

fn cmd_vf_ls(m: *Monitor, args: []const []const u8) ExecError {
    const path: []const u8 = if (args.len > 0) args[0] else "";
    if (!kernel_vf_allowed(path, .list)) return trust_denied(m, "vf ls", path);
    if (file_table.parse_path(path)) |parsed| {
        if (parsed.partition == .disk) return cmd_disk_ls(m, parsed.path[0..parsed.parsed_len()]);
    }
    var res: virtio_file.ListResult = .{};
    const st = virtio_file.list(path, &res);
    if (st != virtio_file.st_ok) {
        err_prefix(m);
        m.console.puts("vf ls: ");
        m.console.puts(if (path.len > 0) path else "/");
        m.console.puts(": ");
        m.console.print_line(vf_status_text(st));
        return .invalid_argument;
    }
    var i: usize = 0;
    while (i < res.count) : (i += 1) {
        const e = res.entries[i];
        m.console.puts(e.name[0..e.name_len]);
        m.console.puts(if (e.type == virtio_file.dir_type_dir) "/ " else "  ");
        m.console.print_u64(e.size);
        m.console.puts("\n");
    }
    return .none;
}

fn cmd_vf_cat(m: *Monitor, args: []const []const u8) ExecError {
    if (args.len == 0) return cmd_vf_usage(m);
    const path = args[0];
    if (!kernel_vf_allowed(path, .read)) return trust_denied(m, "vf cat", path);
    if (file_table.parse_path(path)) |parsed| {
        if (parsed.partition == .disk) return cmd_disk_cat(m, parsed.path[0..parsed.parsed_len()]);
    }
    var st_res: virtio_file.StatResult = .{};
    const st = virtio_file.stat(path, &st_res);
    if (st != virtio_file.st_ok) {
        err_prefix(m);
        m.console.puts("vf cat: ");
        m.console.puts(path);
        m.console.puts(": ");
        m.console.print_line(vf_status_text(st));
        return .invalid_argument;
    }
    if (st_res.is_dir) {
        err_prefix(m);
        m.console.puts("vf cat: ");
        m.console.puts(path);
        m.console.print_line(": is a directory");
        return .invalid_argument;
    }
    // STAT first — the gate asserts the byte count before the stream.
    m.console.puts("vf: cat ");
    m.console.puts(path);
    m.console.puts(" size=");
    m.console.print_u64(st_res.size);
    m.console.puts("\n");
    var acc = virtio_file.StreamCksum{};
    var off: u64 = 0;
    var rts: usize = 0;
    var total: u64 = 0;
    while (true) {
        const rr = virtio_file.read(path, off);
        if (rr.status != virtio_file.st_ok) {
            if (rr.status == virtio_file.st_not_found) break; // raced delete: honest partial
            err_prefix(m);
            m.console.puts("vf cat: ");
            m.console.puts(path);
            m.console.puts(": ");
            m.console.print_line(vf_status_text(rr.status));
            return .invalid_argument;
        }
        if (rr.data.len == 0) break; // EOF
        rts += 1;
        total += rr.data.len;
        acc.add(rr.data);
        off += rr.data.len;
    }
    m.console.puts("vf: cat ok bytes=");
    m.console.print_u64(total);
    m.console.puts(" rts=");
    m.console.print_u64(rts);
    m.console.puts(" cksum=0x");
    m.console.puts(&vf_hex16(acc.finish()));
    m.console.puts("\n");
    return .none;
}

// ---------------------------------------------------------------------------
// M100c (issue #2308, ADR 0050): `vf ls`, `vf cat` and `stat` on /disk go to
// the mounted VLFS volume — the same reader the EL0 file ABI uses. A missing
// or refused disk prints the ADR refusal name (not_vlfs, io_error, ...)
// rather than leaking the path into the host share.
// ---------------------------------------------------------------------------

/// Readdir staging for `vf ls /disk...` — the sorted smallest-256 live
/// dirents, the same bound file_table's own listing uses.
var disk_ls_entries: [file_table.directory.entry_max]vlfs.Dirent = undefined;

/// `vf cat /disk/...` stream window (the kernel stack cannot hold one).
var disk_cat_buf: [vlfs.block_len]u8 = undefined;

pub fn disk_verb_err(m: *Monitor, verb: []const u8, path: []const u8, err: vlfs.Error) ExecError {
    err_prefix(m);
    m.console.puts(verb);
    m.console.puts(": disk:");
    m.console.puts(if (path.len == 0) "/" else path);
    m.console.puts(": ");
    m.console.print_line(vlfs.refusalName(err));
    return .invalid_argument;
}

pub fn cmd_disk_ls(m: *Monitor, subpath: []const u8) ExecError {
    const dir = vlfs_disk.lookup(subpath) catch |err| return disk_verb_err(m, "vf ls", subpath, err);
    if (dir.kind != vlfs.kind_dir) return disk_verb_err(m, "vf ls", subpath, error.NotDir);
    const n = vlfs_disk.listDir(dir.inode, &disk_ls_entries) catch |err| return disk_verb_err(m, "vf ls", subpath, err);
    for (disk_ls_entries[0..n]) |*d| {
        const st = vlfs_disk.statInode(d.inode) catch |err| return disk_verb_err(m, "vf ls", subpath, err);
        m.console.puts(d.nameSlice());
        m.console.puts(if (d.kind == vlfs.kind_dir) "/ " else "  ");
        m.console.print_u64(st.size);
        m.console.puts("\n");
    }
    return .none;
}

pub fn cmd_disk_cat(m: *Monitor, subpath: []const u8) ExecError {
    const found = vlfs_disk.lookup(subpath) catch |err| return disk_verb_err(m, "vf cat", subpath, err);
    if (found.kind == vlfs.kind_dir) {
        err_prefix(m);
        m.console.puts("vf cat: disk:");
        m.console.puts(subpath);
        m.console.print_line(": is a directory");
        return .invalid_argument;
    }
    const st = vlfs_disk.statInode(found.inode) catch |err| return disk_verb_err(m, "vf cat", subpath, err);
    m.console.puts("vf: cat disk:");
    m.console.puts(subpath);
    m.console.puts(" size=");
    m.console.print_u64(st.size);
    m.console.puts("\n");
    var acc = virtio_file.StreamCksum{};
    var off: u64 = 0;
    var rts: usize = 0;
    var total: u64 = 0;
    while (off < st.size) {
        const n = vlfs_disk.read(found.inode, off, &disk_cat_buf) catch |err| return disk_verb_err(m, "vf cat", subpath, err);
        if (n == 0) break; // defensive EOF
        rts += 1;
        total += n;
        acc.add(disk_cat_buf[0..n]);
        off += n;
    }
    m.console.puts("vf: cat ok bytes=");
    m.console.print_u64(total);
    m.console.puts(" rts=");
    m.console.print_u64(rts);
    m.console.puts(" cksum=0x");
    m.console.puts(&vf_hex16(acc.finish()));
    m.console.puts("\n");
    return .none;
}

/// `vf write /disk/<path> <n>` — write n bytes of a deterministic fill
/// pattern through the journaled write path in reply_cap-sized chunks
/// (each chunk is one committed transaction). A mid-fill refusal reports
/// the named reason AND the confirmed byte count — an honest short write.
pub fn cmd_disk_write(m: *Monitor, subpath: []const u8, n: u64) ExecError {
    var written: u64 = 0;
    var chunks: u64 = 0;
    while (written < n) {
        const take: usize = @intCast(@min(n - written, cmd_write_buf.len));
        for (cmd_write_buf[0..take], 0..) |*b, i| {
            b.* = @intCast((written + i) % 251);
        }
        vlfs_disk.writeAppend(subpath, cmd_write_buf[0..take]) catch |err| {
            err_prefix(m);
            m.console.puts("vf write: disk:");
            m.console.puts(subpath);
            m.console.puts(": ");
            m.console.puts(vlfs.refusalName(err));
            m.console.puts(" after ");
            m.console.print_u64(written);
            m.console.print_line(" bytes");
            return .invalid_argument;
        };
        written += take;
        chunks += 1;
    }
    vlfs_disk.sync() catch |err| return disk_verb_err(m, "vf write", subpath, err);
    m.console.puts("vf: write disk:");
    m.console.puts(subpath);
    m.console.puts(" n=");
    m.console.print_u64(n);
    m.console.puts(" wrote=");
    m.console.print_u64(written);
    m.console.puts(" chunks=");
    m.console.print_u64(chunks);
    m.console.puts("\n");
    return .none;
}

pub fn cmd_disk_stat(m: *Monitor, name: []const u8, subpath: []const u8) ExecError {
    const found = vlfs_disk.lookup(subpath) catch |err| return disk_verb_err(m, "stat", subpath, err);
    const st = vlfs_disk.statInode(found.inode) catch |err| return disk_verb_err(m, "stat", subpath, err);
    m.console.puts("  File:    ");
    m.console.print_line(name);
    m.console.puts("  Size:    ");
    m.console.print_u64(st.size);
    m.console.print_line(" bytes");
    m.console.puts("  Type:    ");
    m.console.print_line(if (st.kind == vlfs.kind_dir) "directory" else "regular file");
    m.console.puts("  Inode:   ");
    m.console.print_u64(st.ino);
    m.console.puts("\n");
    m.console.puts("  Volume:  ");
    m.console.print_line("vlfs");
    return .none;
}

/// Compact lowercase 4-digit hex (the vf gate's cksum needle style).
fn vf_hex16(value: u16) [4]u8 {
    const digits = "0123456789abcdef";
    var out: [4]u8 = undefined;
    out[0] = digits[@intCast((value >> 12) & 0xf)];
    out[1] = digits[@intCast((value >> 8) & 0xf)];
    out[2] = digits[@intCast((value >> 4) & 0xf)];
    out[3] = digits[@intCast(value & 0xf)];
    return out;
}
