//! Monitor commands: the `usb` verb tree — XHCI transport, HID report, bulk engine, mass-storage BOT, FAT32 volume reads.
//!
//! Split out of monitor.zig (#3755): declarations only — the
//! shared types, registry, dispatch, completion, and print/parse
//! helpers stay in the root and are aliased below so the moved
//! code is verbatim.

const std = @import("std");
const mon = @import("../monitor.zig");

const Monitor = mon.Monitor;
const ExecError = mon.ExecError;
const parseInt = mon.parseInt;
const xhci = mon.xhci;
const usb_msc = mon.usb_msc;
const fat32_ro = mon.fat32_ro;
const file_table = mon.file_table;
pub fn cmd_usb(m: *Monitor, args: []const []const u8) ExecError {
    if (args.len > 0 and std.mem.eql(u8, args[0], "devices")) return cmd_usb_devices(m);
    if (args.len > 0 and std.mem.eql(u8, args[0], "report")) return cmd_usb_report(m, args);
    if (args.len > 0 and std.mem.eql(u8, args[0], "bulk")) return cmd_usb_bulk(m, args);
    if (args.len > 0 and std.mem.eql(u8, args[0], "msc")) return cmd_usb_msc(m, args);
    // M70f F1 (issue #1458): the read-only FAT32 volume surface.
    if (args.len > 0 and std.mem.eql(u8, args[0], "vol")) return cmd_usb_vol(m, args);
    if (args.len > 0 and std.mem.eql(u8, args[0], "ls")) return cmd_usb_ls(m, args);
    if (args.len > 0 and std.mem.eql(u8, args[0], "cat")) return cmd_usb_cat(m, args);
    if (args.len > 0 and std.mem.eql(u8, args[0], "rescan")) return cmd_usb_rescan(m);
    if (args.len > 0 and std.mem.eql(u8, args[0], "detach")) return cmd_usb_detach(m, args);
    if (!xhci.xhci_ready) {
        m.console.puts("usb: no XHCI device (");
        m.console.puts(if (xhci.xhci_fail.len > 0) xhci.xhci_fail else "DID 0x1a06 not found on bus 0");
        m.console.puts(")\n");
        return .none;
    }
    m.console.puts("usb: did=");
    m.console.print_hex(xhci.xhci_did);
    m.console.puts(" class=");
    m.console.print_hex(xhci.xhci_class);
    m.console.puts(" dev=");
    m.console.print_u64(xhci.xhci_dev);
    m.console.puts("\n");
    m.console.puts("usb: bar0=");
    m.console.print_hex(xhci.xhci_bar0);
    m.console.puts(" bar1=");
    m.console.print_hex(xhci.xhci_bar1);
    m.console.puts(" base=");
    m.console.print_hex(xhci.xhci_base);
    m.console.puts("\n");
    m.console.puts("usb: caplen=");
    m.console.print_hex(xhci.xhci_caplen);
    m.console.puts(" hciver=");
    m.console.print_hex(xhci.xhci_hciver);
    m.console.puts(" dboff=");
    m.console.print_hex(xhci.xhci_dboff);
    m.console.puts(" rtsoff=");
    m.console.print_hex(xhci.xhci_rtsoff);
    m.console.puts("\n");
    m.console.puts("usb: hcsparams1=");
    m.console.print_hex(xhci.xhci_hcsparams1);
    m.console.puts(" hcsparams2=");
    m.console.print_hex(xhci.xhci_hcsparams2);
    m.console.puts(" hcsparams3=");
    m.console.print_hex(xhci.xhci_hcsparams3);
    m.console.puts(" hccparams1=");
    m.console.print_hex(xhci.xhci_hccparams1);
    m.console.puts("\n");
    m.console.puts("usb: maxslots=");
    m.console.print_u64(xhci.hcsparams1_max_slots(xhci.xhci_hcsparams1));
    m.console.puts(" maxintrs=");
    m.console.print_u64(xhci.hcsparams1_max_intrs(xhci.xhci_hcsparams1));
    m.console.puts(" maxports=");
    m.console.print_u64(xhci.hcsparams1_max_ports(xhci.xhci_hcsparams1));
    m.console.puts("\n");
    m.console.puts("usb: pre-reset sts=");
    m.console.print_hex(xhci.xhci_pre_reset_usbsts);
    m.console.puts(" cmd=");
    m.console.print_hex(xhci.xhci_pre_reset_usbcmd);
    m.console.puts("\n");
    m.console.puts("usb: usbsts=");
    m.console.print_hex(xhci.xhci_usbsts());
    m.console.puts(" noop_cc=");
    m.console.print_hex(xhci.xhci_noop_cc);
    m.console.puts(" noop=");
    m.console.puts(if (xhci.xhci_noop_done) "ok" else "fail");
    m.console.puts("\n");
    // Live port status: one line per port, with the connect/enable/power
    // bits decoded (the I1 report — I2 turns these into enumerated devices).
    const max_ports = xhci.hcsparams1_max_ports(xhci.xhci_hcsparams1);
    var port: u8 = 1;
    while (port <= max_ports) : (port += 1) {
        const psc = xhci.xhci_port_status(port);
        m.console.puts("usb: port");
        m.console.print_u64(port);
        m.console.puts("=");
        m.console.print_hex(psc);
        m.console.puts(" ccs=");
        m.console.puts(if ((psc & 0x1) != 0) "1" else "0");
        m.console.puts(" ped=");
        m.console.puts(if ((psc & 0x2) != 0) "1" else "0");
        m.console.puts(" pp=");
        m.console.puts(if ((psc & 0x200) != 0) "1" else "0");
        m.console.puts(" ps=");
        m.console.print_u64((psc >> 10) & 0xf);
        m.console.puts("\n");
    }
    return .none;
}

/// `usb devices` — the enumerated HID device table (I2). One line per
/// enumerated device: slot/port/speed + VID/PID/class/protocol + the
/// interrupt-IN endpoint + the HID boot-protocol negotiation result.
fn cmd_usb_devices(m: *Monitor) ExecError {
    if (!xhci.xhci_ready) {
        m.console.puts("usb devices: no XHCI device\n");
        return .none;
    }
    if (!xhci.enum_done) {
        m.console.puts("usb devices: enumeration incomplete (");
        m.console.puts(if (xhci.enum_fail.len > 0) xhci.enum_fail else "no devices enumerated");
        m.console.puts(")\n");
        return .none;
    }
    m.console.puts("usb devices: count=");
    m.console.print_u64(xhci.enum_count);
    m.console.puts("\n");
    var i: usize = 0;
    while (i < xhci.EnumMax) : (i += 1) {
        const d = xhci.enum_devs[i];
        if (!d.present) continue;
        m.console.puts("usb dev");
        m.console.print_u64(i);
        m.console.puts(": slot=");
        m.console.print_u64(d.slot_id);
        m.console.puts(" port=");
        m.console.print_u64(d.port);
        m.console.puts(" speed=");
        m.console.print_u64(d.speed);
        m.console.puts(" vid=");
        m.console.print_hex_min(d.vid);
        m.console.puts(" pid=");
        m.console.print_hex_min(d.pid);
        m.console.puts(" class=");
        m.console.print_u64(d.class);
        m.console.puts(" protocol=");
        m.console.print_u64(d.protocol);
        m.console.puts(" epin=");
        m.console.print_u64(d.ep_in_num);
        m.console.puts(" maxpkt=");
        m.console.print_u64(d.ep_in_maxpkt);
        m.console.puts(" interval=");
        m.console.print_u64(d.ep_in_interval);
        m.console.puts(" boot=");
        m.console.puts(if (d.hid_boot) "1" else "0");
        m.console.puts("\n");
    }
    return .none;
}

/// `usb report [<dev>]` — poll the device's interrupt-IN endpoint for the
/// last HID report and print it raw + a boot-protocol decode (I2). Defaults
/// to device 0 (the keyboard, if present).
fn cmd_usb_report(m: *Monitor, args: []const []const u8) ExecError {
    if (!xhci.xhci_ready) {
        m.console.puts("usb report: no XHCI device\n");
        return .none;
    }
    var dev_idx: usize = 0;
    if (args.len > 1) {
        dev_idx = parseInt(args[1]) catch 0;
    }
    if (dev_idx >= xhci.EnumMax or !xhci.enum_devs[dev_idx].present) {
        m.console.puts("usb report: no device ");
        m.console.print_u64(dev_idx);
        m.console.puts("\n");
        return .none;
    }
    const d = xhci.enum_devs[dev_idx];
    if (d.ep_in_num == 0) {
        m.console.puts("usb report: device has no interrupt-IN endpoint\n");
        return .none;
    }
    const got = xhci.xhci_poll_intr(d.slot_id);
    if (!got) {
        m.console.puts("usb report: dev");
        m.console.print_u64(dev_idx);
        m.console.puts(" no report (timeout)\n");
        return .none;
    }
    const rep = xhci.xhci_report(d.slot_id);
    m.console.puts("usb report: dev");
    m.console.print_u64(dev_idx);
    m.console.puts(" seq=");
    m.console.print_u64(d.report_seq);
    m.console.puts(" len=");
    m.console.print_u64(rep.len);
    m.console.puts(" bytes=");
    var i: usize = 0;
    while (i < rep.len) : (i += 1) {
        if (i > 0) m.console.puts(" ");
        m.console.print_hex_min(rep.bytes[i]);
    }
    m.console.puts("\n");
    // Boot-protocol decode: keyboard = modifier byte + up to 6 keycodes;
    // mouse = buttons + X + Y. Raw bytes are the ground truth.
    if (xhci.hid_kind[dev_idx] == .keyboard and rep.len >= 8) {
        m.console.puts("usb report: kb mod=");
        m.console.print_hex_min(rep.bytes[0]);
        m.console.puts(" keys=");
        var k: usize = 2;
        while (k < 8) : (k += 1) {
            if (rep.bytes[k] != 0) {
                m.console.print_hex_min(rep.bytes[k]);
                m.console.puts(" ");
            }
        }
        m.console.puts("\n");
    } else if (xhci.hid_kind[dev_idx] == .mouse and rep.len >= 3) {
        m.console.puts("usb report: ptr btn=");
        m.console.print_u64(rep.bytes[0]);
        m.console.puts(" x=");
        m.console.print_u64(rep.bytes[1]);
        m.console.puts(" y=");
        m.console.print_u64(rep.bytes[2]);
        m.console.puts("\n");
    }
    return .none;
}

/// `usb bulk` — the U1 bulk-engine report: the bulk endpoint state of every
/// enumerated device + the last completed bulk IN payload. The U1 probe's
/// evidence verb (no protocol above the engine — the bytes on the wire are
/// the ground truth).
fn cmd_usb_bulk(m: *Monitor, args: []const []const u8) ExecError {
    if (!xhci.xhci_ready) {
        m.console.puts("usb bulk: no XHCI device\n");
        return .none;
    }
    // Optional subcommand `probe <hex byte> ...`: one bulk OUT with the
    // given payload followed by one bulk IN, printed raw. U1 stays BELOW
    // the protocol level — the probe's job is to observe what the device
    // does with arbitrary bulk bytes (the raw evidence for the contract).
    if (args.len > 1 and std.mem.eql(u8, args[1], "probe")) {
        const slot_id = xhci.usb_bulk_dev();
        if (slot_id == 0) {
            m.console.puts("usb bulk probe: no bulk-capable device\n");
            return .none;
        }
        var payload: [xhci.bulk_buf_len]u8 = undefined;
        var plen: usize = 0;
        var i: usize = 2;
        while (i < args.len and plen < xhci.bulk_buf_len) : (i += 1) {
            const v = std.fmt.parseInt(u16, args[i], 0) catch continue;
            if (v > 0xff) continue;
            payload[plen] = @intCast(v);
            plen += 1;
        }
        if (plen == 0) {
            m.console.puts("usb bulk probe: usage: usb bulk probe <hex byte> ...\n");
            return .none;
        }
        const out = xhci.xhci_bulk_transfer(slot_id, false, &payload, @intCast(plen));
        m.console.puts("usb bulk probe: OUT cc=");
        m.console.print_hex(out.cc);
        m.console.puts(" sent=");
        m.console.print_u64(if (out.remaining <= plen) plen - out.remaining else 0);
        m.console.puts("\n");
        var inbuf: [xhci.bulk_buf_len]u8 = undefined;
        const rin = xhci.xhci_bulk_transfer(slot_id, true, &inbuf, xhci.bulk_buf_len);
        m.console.puts("usb bulk probe: IN cc=");
        m.console.print_hex(rin.cc);
        m.console.puts(" got=");
        const got: usize = if (rin.remaining <= xhci.bulk_buf_len) xhci.bulk_buf_len - rin.remaining else 0;
        m.console.print_u64(got);
        m.console.puts(" bytes=");
        var k: usize = 0;
        while (k < got) : (k += 1) {
            if (k > 0) m.console.puts(" ");
            m.console.print_hex_min(inbuf[k]);
        }
        m.console.puts("\n");
        return .none;
    }
    // The state report: one line per enumerated device.
    m.console.puts("usb bulk: devices=");
    m.console.print_u64(xhci.enum_count);
    m.console.puts("\n");
    var di: usize = 0;
    while (di < xhci.EnumMax) : (di += 1) {
        const d = xhci.enum_devs[di];
        if (!d.present) continue;
        m.console.puts("usb bulk: dev");
        m.console.print_u64(di);
        m.console.puts(" slot=");
        m.console.print_u64(d.slot_id);
        m.console.puts(" vid=");
        m.console.print_hex(d.vid);
        m.console.puts(" pid=");
        m.console.print_hex(d.pid);
        m.console.puts(" class=");
        m.console.print_hex(d.class);
        m.console.puts(" bulk=");
        m.console.puts(if (d.bulk_ready) "yes" else "no");
        m.console.puts(" maxpkt=");
        m.console.print_u64(d.bulk_maxpkt);
        m.console.puts(" out_ep=");
        m.console.print_u64(d.bulk_out_num);
        m.console.puts(" in_ep=");
        m.console.print_u64(d.bulk_in_num);
        m.console.puts("\n");
        if (d.bulk_ready and xhci.bulk_recv_len[di] > 0) {
            m.console.puts("usb bulk: last-in len=");
            m.console.print_u64(xhci.bulk_recv_len[di]);
            m.console.puts(" bytes=");
            var k: usize = 0;
            while (k < xhci.bulk_recv_len[di]) : (k += 1) {
                if (k > 0) m.console.puts(" ");
                m.console.print_hex_min(xhci.bulk_last_in(di)[k]);
            }
            m.console.puts("\n");
        }
    }
    return .none;
}

/// `usb msc` — the U2 (M43 card U2) probe-and-record verb: TEST UNIT READY,
/// INQUIRY, READ CAPACITY(10), then a deterministic 512-byte sector
/// write/read-back. Every field is the wire's honest answer (the [observed]
/// rows for docs/hardware-contract.md) — a refused write is recorded, not
/// faked. Optional LBA: `usb msc probe <lba>` (default 100, a gap sector).
fn cmd_usb_msc(m: *Monitor, args: []const []const u8) ExecError {
    if (!xhci.xhci_ready) {
        m.console.puts("usb msc: no XHCI device\n");
        return .none;
    }
    var lba: u32 = usb_msc.probe_default_lba;
    for (args[1..]) |a| {
        if (std.mem.eql(u8, a, "probe")) continue;
        lba = std.fmt.parseInt(u32, a, 0) catch lba;
    }
    const info = usb_msc.probe(lba);
    if (!info.present) {
        m.console.puts("usb msc: no bulk-capable device\n");
        return .none;
    }
    m.console.puts("usb msc: dev slot=");
    m.console.print_u64(info.slot);
    m.console.puts(" tur_status=");
    m.console.print_u64(info.tur.status);
    m.console.puts(" tur_ok=");
    m.console.print_u64(if (info.tur.ok) 1 else 0);
    m.console.puts(" tur_stage=");
    m.console.puts(botStageName(info.tur.stage));
    m.console.puts("\n");
    m.console.puts("usb msc: inquiry ok=");
    m.console.print_u64(if (info.inquiry_ok) 1 else 0);
    m.console.puts(" vendor=");
    m.console.puts(&info.inquiry.vendor);
    m.console.puts(" product=");
    m.console.puts(&info.inquiry.product);
    m.console.puts(" rev=");
    m.console.puts(&info.inquiry.rev);
    m.console.puts("\n");
    m.console.puts("usb msc: capacity ok=");
    m.console.print_u64(if (info.capacity_ok) 1 else 0);
    m.console.puts(" last_lba=");
    m.console.print_u64(info.capacity.last_lba);
    m.console.puts(" block_len=");
    m.console.print_u64(info.capacity.block_len);
    m.console.puts("\n");
    m.console.puts("usb msc: rw lba=");
    m.console.print_u64(info.rw_lba);
    m.console.puts(" byte=");
    m.console.print_hex_min(info.rw_byte);
    m.console.puts(" write_ok=");
    m.console.print_u64(if (info.rw_write.ok) 1 else 0);
    m.console.puts(" read_ok=");
    m.console.print_u64(if (info.rw_read.ok) 1 else 0);
    m.console.puts(" diff=");
    m.console.print_u64(info.rw_diff);
    m.console.puts(" ok=");
    m.console.print_u64(if (info.rw_ok) 1 else 0);
    m.console.puts(" write_stage=");
    m.console.puts(botStageName(info.rw_write.stage));
    m.console.puts(" read_stage=");
    m.console.puts(botStageName(info.rw_read.stage));
    m.console.puts("\n");
    return .none;
}

fn botStageName(s: usb_msc.Stage) []const u8 {
    return switch (s) {
        .none => "none",
        .cbw => "cbw",
        .data => "data",
        .csw => "csw",
        .csw_signature => "csw_sig",
        .csw_tag => "csw_tag",
    };
}

// ---------------------------------------------------------------------------
// M70f F1 (issue #1458): read-only FAT32 volumes on the M43 block seam.
//
// `usb vol` enumerates the MBR and reports each slot's volume geometry,
// `usb ls` lists a volume directory, and `usb cat` reads a file printing a
// bounded prefix plus the FULL byte count and an FNV-1a 32 of every byte. The
// checksum is what makes a byte-exact claim for a file too large to dump: the
// gate's host-side builder stages the content and pins the same value. Nothing
// here writes to the volume.
// ---------------------------------------------------------------------------

/// One sector scratch for the `usb cat` chain walk: the monitor console path is
/// single-threaded (one command at a time), so a static is safe here.
var usb_cat_sector: [fat32_ro.sector_len]u8 align(64) = undefined;

/// `usb cat` bounds. A print bound keeps the serial readable; the scan bound
/// keeps a bogus file size (the FAT's size field is a u32 an image author
/// controls) from holding the shell for hours of BOT round trips — 1 MiB is
/// 2048 sector transfers. A scan that stops at the cap says `capped=1`
/// rather than presenting a partial read as the whole file.
const usb_cat_max_print_bytes: u64 = 4096;

const usb_cat_max_bytes: u64 = 1 << 20;

fn printHexByte(m: *Monitor, b: u8) void {
    const digits = "0123456789abcdef";
    m.console.putc(digits[b >> 4]);
    m.console.putc(digits[b & 0xf]);
}

/// The BPB's 11-byte volume label, trailing padding trimmed (`-` when blank).
fn printVolumeLabel(m: *Monitor, label: [11]u8) void {
    var n: usize = label.len;
    while (n > 0 and (label[n - 1] == ' ' or label[n - 1] == 0)) n -= 1;
    if (n == 0) {
        m.console.puts("-");
        return;
    }
    m.console.puts(label[0..n]);
}

/// Accept both `1/PROBE.TXT` and `usb1/PROBE.TXT` spellings of a volume path.
fn canonicalVolumePath(buf: *[file_table.max_path_len]u8, arg: []const u8) ?[]const u8 {
    if (arg.len == 0 or arg.len > buf.len) return null;
    if (file_table.usbVolumeForm(arg) != null) {
        @memcpy(buf[0..arg.len], arg);
        return buf[0..arg.len];
    }
    if (arg[0] < '1' or arg[0] > '4') return null;
    if (arg.len > 1 and arg[1] != '/') return null;
    const need = arg.len + 3;
    if (need > buf.len) return null;
    @memcpy(buf[0..3], "usb");
    @memcpy(buf[3..need], arg);
    return buf[0..need];
}

const UsbVolume = struct {
    src: fat32_ro.SectorSource = .{ .ctx = null, .readFn = unusableSource },
    parsed: file_table.ParsedPath = .{ .partition = .usb_fat, .path = @as([file_table.max_path_len]u8, @splat(@as(u8, 0))), .path_len = 0 },
    vol: fat32_ro.Volume = .{},

    fn unusableSource(_: ?*anyopaque, _: u32, _: *[fat32_ro.sector_len]u8) bool {
        return false;
    }
};

/// Resolve `<vol>[/path]` down to a mounted volume. Every failure mode is a
/// printed line plus `null` — the caller prints nothing of its own, so the
/// transcript always says exactly why.
fn openUsbVolume(m: *Monitor, arg: []const u8, out: *UsbVolume) bool {
    var path_buf: [file_table.max_path_len]u8 = undefined;
    const canonical = canonicalVolumePath(&path_buf, arg) orelse {
        m.console.print_line("usb: path must be <vol>[/<dir>] or usb<vol>[/<dir>] with vol 1..4");
        return false;
    };
    const parsed = file_table.parse_path(canonical) orelse {
        m.console.print_line("usb: path is not a valid volume path");
        return false;
    };
    if (parsed.partition != .usb_fat) {
        m.console.print_line("usb: path is not a volume path");
        return false;
    }
    if (!xhci.xhci_ready or !usb_msc.present()) {
        m.console.print_line("usb: no bulk-capable device");
        return false;
    }
    const cap = usb_msc.capacity() orelse {
        m.console.print_line("usb: READ CAPACITY(10) failed");
        return false;
    };
    if (cap.block_len != usb_msc.block_len) {
        m.console.print_line("usb: device does not use 512-byte sectors");
        return false;
    }
    const src = usb_msc.source();
    const row = fat32_ro.partitionAt(src, parsed.usb_index) catch |err| {
        switch (err) {
            error.NoPartition => m.console.print_line("usb: no such MBR partition"),
            error.NoMbr => m.console.print_line("usb: no MBR (no 0xaa55 signature at LBA 0)"),
            else => m.console.print_line("usb: LBA 0 read failed"),
        }
        return false;
    };
    if (@as(u64, row.start_lba) + row.sectors > @as(u64, cap.last_lba) + 1) {
        m.console.print_line("usb: partition extends past the end of the device");
        return false;
    }
    out.vol = fat32_ro.mount(src, row.start_lba, row.sectors) catch |err| {
        m.console.puts("usb: partition ");
        m.console.print_u64(parsed.usb_index);
        m.console.puts(" is not a readable FAT32 volume (");
        m.console.puts(switch (err) {
            error.NotFat32 => "not-fat32",
            error.BadBpb => "bad-bpb",
            else => "read-error",
        });
        m.console.print_line(")");
        return false;
    };
    out.src = src;
    out.parsed = parsed;
    return true;
}

/// `usb vol [<n>]` — the MBR walk and each slot's FAT32 geometry. Three
/// distinct honest answers: an empty slot, a slot whose type is not FAT32, and
/// a slot that declares FAT32 but does not parse.
fn cmd_usb_vol(m: *Monitor, args: []const []const u8) ExecError {
    _ = args;
    if (!xhci.xhci_ready) {
        m.console.print_line("usb vol: no XHCI device");
        return .none;
    }
    if (!usb_msc.present()) {
        m.console.print_line("usb vol: no bulk-capable device");
        return .none;
    }
    const src = usb_msc.source();
    var table: [fat32_ro.max_partitions]?fat32_ro.PartitionRow = .{ null, null, null, null };
    fat32_ro.readMbr(src, &table) catch |err| {
        switch (err) {
            error.NoMbr => m.console.print_line("usb vol: no MBR (no 0xaa55 signature at LBA 0)"),
            else => m.console.print_line("usb vol: LBA 0 read failed"),
        }
        return .none;
    };
    var populated: u64 = 0;
    for (table) |row| {
        if (row != null) populated += 1;
    }
    m.console.puts("usb vol: partitions=");
    m.console.print_u64(populated);
    m.console.puts("\n");
    for (table, 0..) |row, i| {
        if (row == null) {
            m.console.puts("usb vol: part=");
            m.console.print_u64(i + 1);
            m.console.puts(" absent\n");
            continue;
        }
        const r = row.?;
        m.console.puts("usb vol: part=");
        m.console.print_u64(i + 1);
        m.console.puts(" boot=");
        m.console.print_u64(if (r.bootable) 1 else 0);
        m.console.puts(" type=0x");
        printHexByte(m, r.part_type);
        m.console.puts(" start_lba=");
        m.console.print_u64(r.start_lba);
        m.console.puts(" sectors=");
        m.console.print_u64(r.sectors);
        if (!r.isFat32Type()) {
            m.console.puts(" fat=0 reason=type\n");
            continue;
        }
        const vol = fat32_ro.mount(src, r.start_lba, r.sectors) catch |err| {
            m.console.puts(" fat=0 reason=");
            m.console.print_line(switch (err) {
                error.NotFat32 => "bpb-not-fat32",
                error.BadBpb => "bpb-inconsistent",
                else => "read-error",
            });
            continue;
        };
        m.console.puts(" fat=1 label=");
        if (vol.has_label) printVolumeLabel(m, vol.label) else m.console.puts("-");
        m.console.puts(" cluster_bytes=");
        m.console.print_u64(vol.cluster_bytes);
        m.console.puts(" clusters=");
        m.console.print_u64(vol.cluster_count);
        m.console.puts(" root_cluster=");
        m.console.print_u64(vol.root_cluster);
        m.console.puts("\n");
    }
    return .none;
}

/// `usb ls <vol>[/<dir>]` — list a FAT32 directory (the volume root by
/// default). Names print whole; the frozen 40-byte `DirEntry` row the file ABI
/// hands to EL0 truncates at 31 bytes, which is stated in the docs, not hidden.
fn cmd_usb_ls(m: *Monitor, args: []const []const u8) ExecError {
    if (args.len < 2) {
        m.console.print_line("usb ls: usage: usb ls <vol>[/<dir>]");
        return .invalid_argument;
    }
    var v = UsbVolume{};
    if (!openUsbVolume(m, args[1], &v)) return .invalid_argument;
    var dir: fat32_ro.Entry = undefined;
    fat32_ro.lookup(v.src, v.vol, v.parsed.path[0..v.parsed.parsed_len()], &dir) catch |err| {
        m.console.puts("usb ls: ");
        m.console.print_line(switch (err) {
            error.NotFound => "not found",
            error.NotDir => "a path component is not a directory",
            error.BadChain => "the directory chain is unusable",
            else => "read failed",
        });
        return .invalid_argument;
    };
    if (!dir.isDir()) {
        m.console.print_line("usb ls: is a file (use `usb cat`)");
        return .invalid_argument;
    }
    m.console.puts("usb ls: vol=");
    m.console.print_u64(v.parsed.usb_index);
    m.console.puts(" path=");
    if (v.parsed.parsed_len() == 0) m.console.puts("/") else m.console.puts(v.parsed.path[0..v.parsed.parsed_len()]);
    m.console.puts(" label=");
    if (v.vol.has_label) printVolumeLabel(m, v.vol.label) else m.console.puts("-");
    m.console.puts("\n");
    var iter = fat32_ro.dirIter(v.src, v.vol, dir.first_cluster);
    var n: u64 = 0;
    while (iter.next()) |e| {
        n += 1;
        m.console.puts("usb ls: ");
        m.console.puts(e.nameSlice());
        m.console.puts(" size=");
        m.console.print_u64(e.size);
        m.console.puts(" dir=");
        m.console.print_u64(if (e.isDir()) 1 else 0);
        m.console.puts(" cluster=");
        m.console.print_u64(e.first_cluster);
        m.console.puts(" attr=0x");
        printHexByte(m, e.attr);
        m.console.puts("\n");
    }
    m.console.puts("usb ls: entries=");
    m.console.print_u64(n);
    m.console.puts(" broken=");
    m.console.print_u64(if (iter.broken) 1 else 0);
    m.console.puts("\n");
    return .none;
}

/// `usb cat <vol>/<path> [<max-print>]` — read the file, print at most
/// `max-print` bytes (default 256, clamped to 4096), and always report the byte
/// count plus an FNV-1a 32 of every byte read. A short read (broken chain) is
/// reported as `broken=1` with the bytes actually obtained — never padded — and
/// a scan stopped by the 1 MiB bound reports `capped=1`.
fn cmd_usb_cat(m: *Monitor, args: []const []const u8) ExecError {
    if (args.len < 2) {
        m.console.print_line("usb cat: usage: usb cat <vol>/<path> [<max-print>]");
        return .invalid_argument;
    }
    var max_print: u64 = 256;
    if (args.len > 2) max_print = std.fmt.parseInt(u64, args[2], 0) catch 256;
    if (max_print > usb_cat_max_print_bytes) max_print = usb_cat_max_print_bytes;
    var v = UsbVolume{};
    if (!openUsbVolume(m, args[1], &v)) return .invalid_argument;
    var e: fat32_ro.Entry = undefined;
    fat32_ro.lookup(v.src, v.vol, v.parsed.path[0..v.parsed.parsed_len()], &e) catch |err| {
        m.console.puts("usb cat: ");
        m.console.print_line(switch (err) {
            error.NotFound => "not found",
            error.NotDir => "a path component is not a directory",
            error.BadChain => "the directory chain is unusable",
            else => "read failed",
        });
        return .invalid_argument;
    };
    if (e.isDir()) {
        m.console.print_line("usb cat: is a directory (use `usb ls`)");
        return .invalid_argument;
    }
    m.console.puts("usb cat: vol=");
    m.console.print_u64(v.parsed.usb_index);
    m.console.puts(" path=");
    m.console.puts(v.parsed.path[0..v.parsed.parsed_len()]);
    m.console.puts(" size=");
    m.console.print_u64(e.size);
    m.console.puts("\n");
    var reader = fat32_ro.FileReader.init(v.vol, e);
    var buf: [256]u8 = undefined;
    var fnv = fat32_ro.Fnv{};
    var total: u64 = 0;
    var printed: u64 = 0;
    var ends_newline = true;
    while (total < usb_cat_max_bytes) {
        const want: usize = @intCast(@min(@as(u64, buf.len), usb_cat_max_bytes - total));
        const n = fat32_ro.readFile(v.src, &reader, buf[0..want], &usb_cat_sector);
        if (n == 0) break;
        fnv.update(buf[0..n]);
        total += n;
        var off: usize = 0;
        while (off < n and printed < max_print) {
            m.console.putc(buf[off]);
            ends_newline = buf[off] == '\n';
            off += 1;
            printed += 1;
        }
    }
    // A scan that stopped with bytes still claimed is a cap stop, not a clean
    // end — say so instead of presenting a partial read as the whole file.
    const capped = reader.remaining > 0 and !reader.broken;
    if (printed > 0 and !ends_newline) m.console.putc('\n');
    m.console.puts("usb cat: read=");
    m.console.print_u64(total);
    m.console.puts(" sum=");
    m.console.print_hex(fnv.digest());
    m.console.puts(" printed=");
    m.console.print_u64(printed);
    m.console.puts(" of ");
    m.console.print_u64(total);
    m.console.puts(" broken=");
    m.console.print_u64(if (reader.broken) 1 else 0);
    m.console.puts(" capped=");
    m.console.print_u64(if (capped) 1 else 0);
    m.console.puts("\n");
    return .none;
}

/// `usb rescan` — the U4 (M43 card U4) polled lifecycle rescan: diff PORTSC
/// CCS against the registry, enumerate arrivals with the boot path, quiesce
/// removals, reattach quiesced entries whose port still reports connected.
/// One observability row; the arrival path touches hardware like boot does.
fn cmd_usb_rescan(m: *Monitor) ExecError {
    if (!xhci.xhci_ready) {
        m.console.puts("usb rescan: no XHCI device\n");
        return .none;
    }
    const r = xhci.xhci_rescan();
    m.console.puts("usb rescan: added=");
    m.console.print_u64(r.added);
    m.console.puts(" removed=");
    m.console.print_u64(r.removed);
    m.console.puts(" reattached=");
    m.console.print_u64(r.reattached);
    m.console.puts(" count=");
    m.console.print_u64(r.count);
    m.console.puts(" rescans=");
    m.console.print_u64(xhci.rescan_count);
    m.console.puts("\n");
    return .none;
}

/// `usb detach [slot]` — the U4 administrative detach: quiesce the device so
/// every consumer fails cleanly (bulk refuses, BOT fails stage=cbw, `.usb`
/// is ENOENT, HID drain skips) instead of ghosting. No argument detaches the
/// bulk device (the lifecycle spec's subject); a numeric argument names the
/// slot. The HC slot, contexts, and rings stay allocated — a later `usb
/// rescan` with the port still connected reattaches the same entry.
fn cmd_usb_detach(m: *Monitor, args: []const []const u8) ExecError {
    if (!xhci.xhci_ready) {
        m.console.puts("usb detach: no XHCI device\n");
        return .none;
    }
    if (args.len > 1) {
        const slot: u8 = std.fmt.parseInt(u8, args[1], 0) catch {
            m.console.puts("usb detach: usage: usb detach [slot]\n");
            return .none;
        };
        if (slot == 0 or slot > xhci.EnumMax) {
            m.console.puts("usb detach: slot=");
            m.console.print_u64(slot);
            m.console.puts(" not present\n");
            return .none;
        }
        const idx: usize = slot - 1;
        const port = xhci.enum_devs[idx].port;
        if (!xhci.xhci_detach_slot(slot)) {
            m.console.puts("usb detach: slot=");
            m.console.print_u64(slot);
            m.console.puts(" not present\n");
            return .none;
        }
        m.console.puts("usb detach: slot=");
        m.console.print_u64(slot);
        m.console.puts(" port=");
        m.console.print_u64(port);
        m.console.puts(" ok=1\n");
        return .none;
    }
    const slot = xhci.xhci_detach_bulk();
    if (slot == 0) {
        m.console.puts("usb detach: no bulk-capable device\n");
        return .none;
    }
    const port = xhci.enum_devs[slot - 1].port;
    m.console.puts("usb detach: slot=");
    m.console.print_u64(slot);
    m.console.puts(" port=");
    m.console.print_u64(port);
    m.console.puts(" ok=1\n");
    return .none;
}
