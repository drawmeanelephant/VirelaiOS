//! Monitor commands: graphics + input devices (`screen`/`screenshot`/`text`/`font`/`roadpops`/`input`/`shortcuts`/`sound`/`beep`).
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
const parseInt = mon.parseInt;
const parseHex = mon.parseHex;
const kernel_vf_allowed = mon.kernel_vf_allowed;
const lookup = mon.lookup;
const virtio_file = mon.virtio_file;
const terminal = mon.terminal;
const virtio_gpu = mon.virtio_gpu;
const virtio_snd = mon.virtio_snd;
const road_pops = mon.road_pops;
const fbtext = mon.fbtext;
const input = mon.input;
const driving_award = mon.driving_award;
const settings = mon.settings;
const font_metrics = mon.font_metrics;
/// `roadpops` — report the Road Pops tee console (claim 1574, milestone
/// six G3): armed (the framebuffer target is wired — the boot terminal is
/// on the screen), dirty (a console write batch is waiting for the idle
/// loop's drain-present), and the presents pushed since arm.
pub fn cmd_roadpops(m: *Monitor, args: []const []const u8) ExecError {
    _ = args;
    const r = road_pops.report();
    m.console.puts("roadpops: armed=");
    m.console.puts(if (r.armed) "1" else "0");
    m.console.puts(" dirty=");
    m.console.puts(if (r.dirty) "1" else "0");
    m.console.puts(" presents=");
    m.console.print_u64(r.presents);
    m.console.puts("\n");
    return .none;
}

/// `input` — report the keyboard/pointer event FIFO (claim 6050, milestone
/// seven I3): armed state, FIFO occupancy + drop count, the last decoded
/// keyboard event, and the last recorded pointer report.
pub fn cmd_input(m: *Monitor, args: []const []const u8) ExecError {
    _ = args;
    const r = input.report();
    m.console.puts("input: armed=");
    m.console.puts(if (r.armed) "1" else "0");
    m.console.puts(" fifo=");
    m.console.print_u64(r.fifo_used);
    m.console.puts("/");
    m.console.print_u64(r.fifo_max);
    m.console.puts(" dropped=");
    m.console.print_u64(r.dropped);
    m.console.puts(" events=");
    m.console.print_u64(r.events);
    m.console.puts(" kb-mods=");
    m.console.print_hex_min(r.kb_mods);
    m.console.puts(" kb-usage=");
    m.console.print_hex_min(r.kb_last_usage);
    m.console.puts(" kb-byte=");
    if (r.kb_last_byte != 0 and r.kb_last_byte >= 0x20 and r.kb_last_byte <= 0x7e) {
        m.console.putc(r.kb_last_byte);
    } else {
        m.console.print_hex_min(r.kb_last_byte);
    }
    m.console.puts(" ptr-btns=");
    m.console.print_u64(r.ptr_buttons);
    m.console.puts(" ptr-x=");
    m.console.print_u64(r.ptr_x);
    m.console.puts(" ptr-y=");
    m.console.print_u64(r.ptr_y);
    m.console.puts(" ptr-reports=");
    m.console.print_u64(r.ptr_reports);
    m.console.puts("\n");
    return .none;
}

// ---------------------------------------------------------------------------
// Milestone six card G1 (claim 6053) — `screen` / `screen fill`
// ---------------------------------------------------------------------------

fn print_cmd_result(m: *Monitor, r: virtio_gpu.CmdResult) void {
    m.console.puts(switch (r) {
        .ok => "ok",
        .not_ready => "not-ready",
        .timeout => "timeout",
        .bad_response => "bad-response",
    });
}

/// `screen` — report the virtio-gpu transport: the OBSERVED device DID +
/// class + bus slot, the negotiated feature bits (low/high), the device's
/// num_scanouts, the scanout mode GET_DISPLAY_INFO reported (width x
/// height, enabled), the device status + post-exit re-arm state, the 2D
/// setup state, and the command counters. Grep-able and deterministic
/// (the net/mbox/procs observability shape). Honest when the device is
/// absent: the default runner attaches no graphics device. `screen fill
/// <rrggbb>` is the card-G1 subcommand: fill the framebuffer with the
/// 0xRRGGBB color, then TRANSFER_TO_HOST_2D + RESOURCE_FLUSH — the first
/// non-blank framebuffer the host `--screenshot` captures.
pub fn cmd_screen(m: *Monitor, args: []const []const u8) ExecError {
    if (args.len > 0) {
        if (std.mem.eql(u8, args[0], "fill")) return cmd_screen_fill(m, args[1..]);
        if (std.mem.eql(u8, args[0], "peek")) return cmd_screen_peek(m);
        print_usage(m, lookup("screen").?);
        return .usage;
    }
    if (!virtio_gpu.gpu_ready) {
        m.console.puts("screen: no virtio-gpu device (");
        m.console.puts(if (virtio_gpu.gpu_fail.len > 0) virtio_gpu.gpu_fail else "DID 0x1050 not found on bus 0");
        m.console.puts(")\n");
        m.console.puts("screen: device-features=");
        m.console.print_hex(virtio_gpu.gpu_dev_feats_lo);
        m.console.puts("/");
        m.console.print_hex(virtio_gpu.gpu_dev_feats_hi);
        m.console.puts("\n");
        m.console.puts("screen: status=");
        m.console.print_hex(virtio_gpu.gpu_status_last);
        m.console.puts(" accepted=");
        m.console.print_hex(virtio_gpu.gpu_feats_lo);
        m.console.puts("/");
        m.console.print_hex(virtio_gpu.gpu_feats_hi);
        m.console.puts("\n");
        m.console.puts("screen: common=");
        m.console.print_hex(virtio_gpu.gpu_common);
        m.console.puts(" notify=");
        m.console.print_hex(virtio_gpu.gpu_notify);
        m.console.puts(" devcfg=");
        m.console.print_hex(virtio_gpu.gpu_devcfg);
        m.console.puts(" bar0=");
        m.console.print_hex(virtio_gpu.gpu_bar0);
        m.console.puts("\n");
        return .none;
    }
    m.console.puts("screen: did=");
    m.console.print_hex(virtio_gpu.gpu_did);
    m.console.puts(" class=");
    m.console.print_hex(virtio_gpu.gpu_class);
    m.console.puts(" dev=");
    m.console.print_u64(virtio_gpu.gpu_dev);
    m.console.puts("\n");
    m.console.puts("screen: devfeat=");
    m.console.print_hex(virtio_gpu.gpu_dev_feats_lo);
    m.console.puts("/");
    m.console.print_hex(virtio_gpu.gpu_dev_feats_hi);
    m.console.puts("\n");
    m.console.puts("screen: feat=");
    m.console.print_hex(virtio_gpu.gpu_feats_lo);
    m.console.puts("/");
    m.console.print_hex(virtio_gpu.gpu_feats_hi);
    m.console.puts(" scanouts=");
    m.console.print_hex(virtio_gpu.gpu_num_scanouts);
    m.console.puts("\n");
    m.console.puts("screen: scanout=");
    m.console.print_hex(virtio_gpu.gpu_scanout_w);
    m.console.puts("x");
    m.console.print_hex(virtio_gpu.gpu_scanout_h);
    m.console.puts(" enabled=");
    m.console.print_hex(virtio_gpu.gpu_scanout_enabled);
    m.console.puts("\n");
    m.console.puts("screen: status=");
    m.console.print_hex(virtio_gpu.gpu_status_last);
    m.console.puts(" rearm=");
    m.console.print_u64(if (virtio_gpu.gpu_rearmed) 1 else 0);
    m.console.puts(" setup=");
    m.console.print_u64(if (virtio_gpu.gpu_setup_ok) 1 else 0);
    m.console.puts(" cmds=");
    m.console.print_u64(virtio_gpu.gpu_cmds);
    m.console.puts(" errors=");
    m.console.print_u64(virtio_gpu.gpu_errors);
    m.console.puts(" timeouts=");
    m.console.print_u64(virtio_gpu.gpu_timeouts);
    m.console.puts("\n");
    // The timeout diagnostics (the claim-time record of a stuck queue).
    m.console.puts("screen: diag avail=");
    m.console.print_u64(virtio_gpu.gpu_diag_avail);
    m.console.puts(" used=");
    m.console.print_u64(virtio_gpu.gpu_diag_used);
    m.console.puts(" st=");
    m.console.print_hex(virtio_gpu.gpu_diag_st);
    m.console.puts(" qen=");
    m.console.print_u64(virtio_gpu.gpu_diag_qen);
    m.console.puts(" qoff=");
    m.console.print_u64(virtio_gpu.gpu_diag_qoff);
    m.console.puts(" mult=");
    m.console.print_u64(virtio_gpu.gpu_diag_mult);
    m.console.puts(" notify=");
    m.console.print_hex(virtio_gpu.gpu_diag_notify);
    m.console.puts("\n");
    m.console.puts("screen: rings desc=");
    m.console.print_hex(virtio_gpu.gpu_diag_desc_phys);
    m.console.puts(" avail=");
    m.console.print_hex(virtio_gpu.gpu_diag_avail_phys);
    m.console.puts(" used=");
    m.console.print_hex(virtio_gpu.gpu_diag_used_phys);
    m.console.puts("\n");
    m.console.puts("screen: diag cmd0=");
    m.console.print_hex(virtio_gpu.gpu_diag_cmd0);
    m.console.puts(" cmd1=");
    m.console.print_hex(virtio_gpu.gpu_diag_cmd1);
    m.console.puts(" isr=");
    m.console.print_hex(virtio_gpu.gpu_diag_isr);
    m.console.puts(" qsz=");
    m.console.print_u64(virtio_gpu.gpu_diag_qsz);
    m.console.puts(" bar0=");
    m.console.print_hex(virtio_gpu.gpu_bar0);
    m.console.puts(" common=");
    m.console.print_hex(virtio_gpu.gpu_common);
    m.console.puts(" devcfg=");
    m.console.print_hex(virtio_gpu.gpu_devcfg);
    m.console.puts(" fb=");
    m.console.print_hex(virtio_gpu.gpu_fb_phys);
    m.console.puts("\n");
    return .none;
}

/// `screen fill <rrggbb>` — fill the framebuffer with the 0xRRGGBB color
/// (out-of-range colors are refused honestly), then TRANSFER_TO_HOST_2D +
/// RESOURCE_FLUSH so the host scanout shows it. The live gate's marker:
/// the `--screenshot` pixels then match the fill.
fn cmd_screen_fill(m: *Monitor, args: []const []const u8) ExecError {
    if (args.len != 1) {
        print_usage(m, lookup("screen").?);
        return .usage;
    }
    const rgb = parseHex(args[0]) catch {
        err_prefix(m);
        m.console.puts("invalid color: ");
        m.console.puts(args[0]);
        m.console.puts("\n");
        return .invalid_argument;
    };
    if (rgb > 0xffffff) {
        err_prefix(m);
        m.console.print_line("color out of range (max 0xffffff)");
        return .invalid_argument;
    }
    if (!virtio_gpu.gpu_ready) {
        err_prefix(m);
        m.console.print_line("transport not ready (no virtio-gpu device)");
        return .none;
    }
    virtio_gpu.fill_framebuffer(@truncate(rgb));
    const tx = virtio_gpu.gpu_transfer();
    const fl = virtio_gpu.gpu_flush();
    m.console.puts("screen fill: fill=");
    m.console.print_hex(rgb);
    m.console.puts(" transfer=");
    print_cmd_result(m, tx);
    m.console.puts(" flush=");
    print_cmd_result(m, fl);
    m.console.puts("\n");
    return .none;
}

/// `screen peek` — dump the first 8 bytes of the framebuffer (B,G,R,X for
/// the first pixel) + a checksum of the first 4096 bytes, so the gate can
/// prove the GUEST-side fill landed before blaming the host display.
fn cmd_screen_peek(m: *Monitor) ExecError {
    if (!virtio_gpu.gpu_ready) {
        err_prefix(m);
        m.console.print_line("transport not ready (no virtio-gpu device)");
        return .none;
    }
    m.console.puts("screen peek: fb=");
    m.console.print_hex(virtio_gpu.gpu_fb_phys);
    m.console.puts(" bytes=");
    var sum: u32 = 0;
    var i: usize = 0;
    while (i < 4096 and i < virtio_gpu.fb_size) : (i += 1) {
        sum +%= virtio_gpu.gpu_fb[i];
    }
    m.console.print_hex(sum);
    m.console.puts(" p0=");
    m.console.print_hex(virtio_gpu.gpu_fb[0]);
    m.console.puts(" p1=");
    m.console.print_hex(virtio_gpu.gpu_fb[1]);
    m.console.puts(" p2=");
    m.console.print_hex(virtio_gpu.gpu_fb[2]);
    m.console.puts(" p3=");
    m.console.print_hex(virtio_gpu.gpu_fb[3]);
    m.console.puts("\n");
    return .none;
}

/// M34 HF5 (issue #739): stream the framebuffer as a BMP to the HOST
/// SHARE through queue 5 when the channel is armed (chunked WRITE round
/// trips ≤ 32 KiB; a 1280×720 BMP is ~2.7 MiB ≈ 86 chunks — assembled in
/// the module's BSS chunk_staging, never the 16 KiB boot stack). The 54-
/// byte header layout is byte-identical to fat.write_fb_bmp (the host
/// verifies the file on disk). Returns true on a fully-written file.
fn vf_write_fb_bmp(path: []const u8, width: u32, height: u32, fb: [*]const u8) bool {
    // M50 TS2 (ADR 0024 D4): the kernel-actor gate (secret paths deny).
    if (!kernel_vf_allowed(path, .write)) return false;
    const row_raw: usize = @as(usize, width) * 3;
    const row_pad: usize = (4 - (row_raw % 4)) % 4;
    const row_stride: usize = row_raw + row_pad;
    const total_pixel_bytes: usize = row_stride * @as(usize, height);
    const total_file_size: usize = 54 + total_pixel_bytes;

    var h: u16 = 0;
    if (virtio_file.open(path, virtio_file.open_flag_create, &h) != virtio_file.st_ok) return false;
    defer _ = virtio_file.close(h);
    _ = virtio_file.truncate(h, 0);

    const chunk = &virtio_file.chunk_staging;
    var off: usize = 0;
    // 54-byte standard BMP header (mirror of fat.write_fb_bmp).
    @memset(chunk[0..54], 0);
    chunk[0] = 'B';
    chunk[1] = 'M';
    std.mem.writeInt(u32, chunk[2..6], @intCast(total_file_size), .little);
    std.mem.writeInt(u32, chunk[10..14], 54, .little);
    std.mem.writeInt(u32, chunk[14..18], 40, .little);
    std.mem.writeInt(i32, chunk[18..22], @intCast(width), .little);
    std.mem.writeInt(i32, chunk[22..26], @intCast(height), .little);
    std.mem.writeInt(u16, chunk[26..28], 1, .little);
    std.mem.writeInt(u16, chunk[28..30], 24, .little);
    std.mem.writeInt(u32, chunk[30..34], 0, .little);
    std.mem.writeInt(u32, chunk[34..38], @intCast(total_pixel_bytes), .little);
    std.mem.writeInt(i32, chunk[38..42], 2835, .little);
    std.mem.writeInt(i32, chunk[42..46], 2835, .little);
    off = 54;

    const fb_stride = @as(usize, width) * 4;
    var cur_y: usize = height;
    while (cur_y > 0) {
        cur_y -= 1;
        const row_start = cur_y * fb_stride;
        var x: usize = 0;
        while (x < width) : (x += 1) {
            const px_off = row_start + x * 4;
            if (off + 3 > chunk.len) {
                if (!vf_flush_chunk(h, chunk, &off)) return false;
            }
            chunk[off] = fb[px_off];
            chunk[off + 1] = fb[px_off + 1];
            chunk[off + 2] = fb[px_off + 2];
            off += 3;
        }
        var pad: usize = 0;
        while (pad < row_pad) : (pad += 1) {
            if (off + 1 > chunk.len) {
                if (!vf_flush_chunk(h, chunk, &off)) return false;
            }
            chunk[off] = 0;
            off += 1;
        }
    }
    return vf_flush_chunk(h, chunk, &off);
}

/// WRITE the assembled chunk and reset the offset (a zero-length flush is
/// a no-op success).
fn vf_flush_chunk(h: u16, chunk: []u8, off: *usize) bool {
    if (off.* == 0) return true;
    var flushed: usize = 0;
    while (flushed < off.*) {
        const take = @min(off.* - flushed, virtio_file.write_chunk_limit());
        var written: u64 = 0;
        const st = virtio_file.write(h, chunk[flushed .. flushed + take], &written);
        if (st != virtio_file.st_ok or written == 0 or written > take) {
            off.* = 0;
            return false;
        }
        flushed += @intCast(written);
    }
    off.* = 0;
    return true;
}

/// M27 G27 (Issue #470): capture current framebuffer and save as BMP — to
/// the HOST SHARE when the channel is armed (M34 HF5, issue #739), else
/// to FAT (dual path until HF6).
pub fn cmd_screenshot(m: *Monitor, args: []const []const u8) ExecError {
    if (!virtio_gpu.gpu_ready) {
        err_prefix(m);
        m.console.print_line("screenshot: GPU framebuffer not ready");
        return .none;
    }
    const path = if (args.len > 0) args[0] else "SCREEN.BMP";
    const row_raw: usize = @as(usize, virtio_gpu.fb_width) * 3;
    const row_pad: usize = (4 - (row_raw % 4)) % 4;
    const row_stride: usize = row_raw + row_pad;
    const total_bytes: usize = 54 + row_stride * @as(usize, virtio_gpu.fb_height);

    // M34 HF6 (issue #740): the FAT BMP writer is gone — screenshots
    // always ride the host share.
    if (!vf_write_fb_bmp(path, virtio_gpu.fb_width, virtio_gpu.fb_height, @ptrCast(&virtio_gpu.gpu_fb))) {
        err_prefix(m);
        m.console.print_line("screenshot: host write failed");
        return .none;
    }
    m.console.puts("screenshot: saved ");
    m.console.print_u64(virtio_gpu.fb_width);
    m.console.puts("x");
    m.console.print_u64(virtio_gpu.fb_height);
    m.console.puts(" BMP to ");
    m.console.puts(path);
    m.console.puts(" (");
    m.console.print_u64(total_bytes);
    m.console.puts(" bytes)\n");
    return .none;
}

/// M27 G29 (Issue #472): display keyboard shortcuts reference table.
pub fn cmd_shortcuts(m: *Monitor, args: []const []const u8) ExecError {
    _ = args;
    m.console.print_line("Keyboard Shortcut Reference:");
    m.console.print_line("  Global:");
    m.console.print_line("    Ctrl+Shift+A   About VirelaiOS dialog");
    m.console.print_line("    Ctrl+Shift+/   Display this shortcut reference");
    m.console.print_line("    Alt+Tab        Cycle open windows (Hold Alt, press Tab/Shift+Tab)");
    m.console.print_line("    Alt+F4         Close active window");
    m.console.print_line("    F11            Toggle window fullscreen / tile");
    m.console.print_line("    Ctrl+Alt+Del   Reboot system");
    m.console.print_line("  Window & Navigation:");
    m.console.print_line("    Tab            Focus next control");
    m.console.print_line("    Shift+Tab      Focus previous control");
    m.console.print_line("    Enter / Space  Activate focused button or toggle control");
    m.console.print_line("    Escape         Dismiss modal dialog / cancel action");
    m.console.print_line("    Ctrl+Q / Ctrl+W Close current window / quit application");
    m.console.print_line("  Editing & Clipboard:");
    m.console.print_line("    Ctrl+C         Copy selected text to clipboard");
    m.console.print_line("    Ctrl+X         Cut selected text to clipboard");
    m.console.print_line("    Ctrl+V         Paste text from clipboard");
    m.console.print_line("    Ctrl+Z         Undo last action");
    m.console.print_line("    Ctrl+Y         Redo last undone action");
    m.console.print_line("    Ctrl+S         Save file");
    m.console.print_line("    Ctrl+O         Open file");
    m.console.print_line("    Ctrl+N         New file");
    m.console.print_line("    Ctrl+F         Find in file");
    m.console.print_line("  Shell / Console:");
    m.console.print_line("    Ctrl+L         Clear screen");
    m.console.print_line("    Ctrl+C         Interrupt current command");
    m.console.print_line("    Up / Down      Navigate command history");
    m.console.print_line("    Ctrl+R         Reverse search command history");
    m.console.print_line("  Terminal Window:");
    m.console.print_line("    Ctrl+Shift+C   Copy the selected text to clipboard");
    m.console.print_line("    Ctrl+Shift+V   Paste clipboard into the terminal");
    m.console.print_line("    Ctrl+Shift+F   Search the scrollback");
    m.console.print_line("    Ctrl+Shift+K   Clear the scrollback (see 'help terminal')");
    m.console.print_line("    Ctrl+Shift+R   Soft reset the terminal");
    m.console.print_line("    Ctrl+Shift+Alt+R  Full reset (RIS)");
    return .none;
}

/// `sound` — report the virtio-snd transport: the OBSERVED device ID and
/// class (a differing DID is a claim-time finding, recorded as the
/// hardware truth), the device status (0x0f = DRIVER_OK post-rearm; 0xff =
/// never discovered), the control-queue state, and the device-config
/// counts (jacks/streams/channel-maps — only when actually captured,
/// never guessed). Honest without the device: `ready=no` with the failure
/// reason.
pub fn cmd_sound(m: *Monitor, args: []const []const u8) ExecError {
    // Stream-state control (claim 9297): `sound volume <0-100>` sets the
    // bounded playback gain; `sound mute <on|off>` silences the stream.
    // Both are pure kernel state — they work without a device, and the
    // report below shows them. `sound` with no args is the transport
    // report.
    if (args.len == 2) {
        if (std.mem.eql(u8, args[0], "volume")) {
            const vol = parseInt(args[1]) catch {
                m.console.puts("sound: volume must be 0..100\n");
                return .none;
            };
            if (vol > 100) {
                m.console.puts("sound: volume must be 0..100\n");
                return .none;
            }
            const set = virtio_snd.snd_set_volume(@intCast(vol));
            m.console.puts("sound: volume=");
            m.console.print_u64(set);
            m.console.puts("\n");
            return .none;
        }
        if (std.mem.eql(u8, args[0], "mute")) {
            const on = std.mem.eql(u8, args[1], "on");
            const off = std.mem.eql(u8, args[1], "off");
            if (!on and !off) {
                m.console.puts("sound: mute must be 'on' or 'off'\n");
                return .none;
            }
            _ = virtio_snd.snd_set_mute(on);
            m.console.puts(if (on) "sound: mute=on\n" else "sound: mute=off\n");
            return .none;
        }
        print_usage(m, lookup("sound").?);
        return .usage;
    }
    if (args.len > 2) {
        print_usage(m, lookup("sound").?);
        return .usage;
    }
    if (!virtio_snd.snd_ready) {
        m.console.puts("sound: no virtio-snd device (");
        m.console.puts(if (virtio_snd.snd_fail.len > 0) virtio_snd.snd_fail else "DID 0x1059 not found on bus 0");
        m.console.puts(")\n");
    } else {
        m.console.puts("sound: ready\n");
    }
    m.console.puts("sound: did=");
    m.console.print_hex(virtio_snd.snd_did);
    m.console.puts(" cls=");
    m.console.print_hex(virtio_snd.snd_class);
    m.console.puts(" st=");
    m.console.print_hex(virtio_snd.snd_status());
    m.console.puts("\n");
    m.console.puts("sound: feats=");
    m.console.print_hex(virtio_snd.snd_feats);
    m.console.puts(" qsz=");
    m.console.print_hex(virtio_snd.queue_size);
    m.console.puts(" qoff=");
    m.console.print_hex(virtio_snd.snd_queue_notify_off);
    m.console.puts(" common=");
    m.console.print_hex(virtio_snd.snd_common);
    m.console.puts(" notify=");
    m.console.print_hex(virtio_snd.snd_notify);
    m.console.puts(" devcfg=");
    m.console.print_hex(virtio_snd.snd_devcfg);
    m.console.puts("\n");
    m.console.puts("sound: ctrl_armed=");
    m.console.print_u64(if (virtio_snd.ctrl_armed) 1 else 0);
    m.console.puts(" tx_armed=");
    m.console.print_u64(if (virtio_snd.tx_armed) 1 else 0);
    m.console.puts(" notify_mult=");
    m.console.print_hex(virtio_snd.snd_notify_mult);
    m.console.puts(" ctl_qoff=");
    m.console.print_hex(virtio_snd.snd_queue_notify_off);
    m.console.puts(" tx_qoff=");
    m.console.print_hex(virtio_snd.tx_queue_notify_off);
    m.console.puts(" fail_stage=");
    m.console.print_u64(virtio_snd.ctl_fail_stage);
    m.console.puts(" spins=");
    m.console.print_u64(virtio_snd.ctl_spins);
    m.console.puts(" used_idx=");
    m.console.print_u64(virtio_snd.ctl_used_idx);
    m.console.puts(" used_id=");
    m.console.print_u64(virtio_snd.ctl_used_id);
    m.console.puts(" used_len=");
    m.console.print_u64(virtio_snd.ctl_used_len);
    m.console.puts("\n");
    m.console.puts("sound: cfg=");
    if (virtio_snd.snd_cfg()) |cfg| {
        m.console.puts("jacks=");
        m.console.print_u64(cfg.jacks);
        m.console.puts(" streams=");
        m.console.print_u64(cfg.streams);
        m.console.puts(" chmaps=");
        m.console.print_u64(cfg.chmaps);
    } else {
        m.console.puts("not-captured (config read never landed)");
    }
    // Fresh post-exit read of the devcfg window (safe: the identity map
    // covers snd_bar0..+0x10000 and the devcfg capability resolved inside
    // it — 0x100001000 on the live runs). Pre-exit vs post-exit values
    // are compared so a firmware-mapping artifact is distinguishable from
    // the device's true report.
    m.console.puts(" fresh=");
    if (virtio_snd.snd_devcfg >= virtio_snd.snd_bar0 and
        virtio_snd.snd_devcfg + virtio_snd.cfg_bytes < virtio_snd.snd_bar0 + 0x10000)
    {
        m.console.puts("jacks=");
        m.console.print_u64(virtio_snd.snd_read32(virtio_snd.cfg_jacks_off));
        m.console.puts(" streams=");
        m.console.print_u64(virtio_snd.snd_read32(virtio_snd.cfg_streams_off));
        m.console.puts(" chmaps=");
        m.console.print_u64(virtio_snd.snd_read32(virtio_snd.cfg_chmaps_off));
    } else {
        m.console.puts("devcfg-outside-bar0 (not re-readable post-exit)");
    }
    // The bounded stream-state (claim 9297): vol=0..100, mute=0|1.
    m.console.puts("\nsound: vol=");
    m.console.print_u64(virtio_snd.stream_volume);
    m.console.puts(" mute=");
    m.console.print_u64(if (virtio_snd.stream_muted) 1 else 0);
    m.console.puts("\n");
    return .none;
}

/// `beep <freq> <ms>` — synthesize a sine and play it through the
/// virtio-snd PCM path (claim 5877, milestone fifteen card A2). The full
/// flow runs: PCM_INFO (the A1-finding workaround — VZ does not populate
/// the config counts, so the stream is enumerated by asking) →
/// SET_PARAMS → PREPARE → START → TX-queue submit → used-ring drain →
/// STOP → RELEASE. Every step's status + the submitted/drained accounting
/// are printed — the live gate's evidence. Honest without the device:
/// `beep` refuses with a clear reason.
pub fn cmd_beep(m: *Monitor, args: []const []const u8) ExecError {
    if (args.len != 2) {
        print_usage(m, lookup("beep").?);
        return .usage;
    }
    const freq = parseInt(args[0]) catch {
        err_prefix(m);
        m.console.puts("beep: invalid frequency: ");
        m.console.puts(args[0]);
        m.console.puts("\n");
        return .invalid_argument;
    };
    const ms = parseInt(args[1]) catch {
        err_prefix(m);
        m.console.puts("beep: invalid duration: ");
        m.console.puts(args[1]);
        m.console.puts("\n");
        return .invalid_argument;
    };
    if (freq < 1 or freq > 20000 or ms < 1 or ms > 650) {
        err_prefix(m);
        m.console.puts("beep: freq must be 1..20000 Hz and ms 1..650\n");
        return .invalid_argument;
    }
    if (!virtio_snd.snd_ready) {
        err_prefix(m);
        m.console.puts("beep: no virtio-snd device (");
        m.console.puts(if (virtio_snd.snd_fail.len > 0) virtio_snd.snd_fail else "DID 0x1059 not found on bus 0");
        m.console.puts(")\n");
        return .none;
    }
    const st = virtio_snd.snd_beep(@intCast(freq), @intCast(ms));
    m.console.puts("beep: reply=");
    var ri: usize = 0;
    while (ri < 9) : (ri += 1) {
        if (ri > 0) m.console.puts(" ");
        m.console.print_hex(std.mem.readInt(u32, virtio_snd.ctl_reply_buf[ri * 4 ..][0..4], .little));
    }
    m.console.puts("\n");
    m.console.puts("beep: info st=");
    m.console.print_hex(virtio_snd.beep_info_status);
    m.console.puts(" formats=");
    m.console.print_hex(virtio_snd.beep_obs_formats);
    m.console.puts(" rates=");
    m.console.print_hex(virtio_snd.beep_obs_rates);
    m.console.puts(" ch=");
    m.console.print_u64(virtio_snd.beep_obs_ch_min);
    m.console.puts("..");
    m.console.print_u64(virtio_snd.beep_obs_ch_max);
    m.console.puts(" dir=");
    m.console.print_u64(virtio_snd.beep_obs_dir);
    m.console.puts("\n");
    m.console.puts("beep: params fmt=");
    m.console.print_u64(virtio_snd.beep_format);
    m.console.puts(" rate=");
    m.console.print_u64(virtio_snd.beep_rate);
    m.console.puts(" ch=");
    m.console.print_u64(virtio_snd.beep_channels);
    m.console.puts(" st=");
    m.console.print_hex(virtio_snd.beep_params_status);
    m.console.puts(" prepare=");
    m.console.print_hex(virtio_snd.beep_prepare_status);
    m.console.puts(" start=");
    m.console.print_hex(virtio_snd.beep_start_status);
    m.console.puts("\n");
    m.console.puts("beep: tx submitted=");
    m.console.print_u64(virtio_snd.beep_submitted);
    m.console.puts(" drained=");
    m.console.print_u64(virtio_snd.beep_drained);
    m.console.puts(" frames=");
    m.console.print_u64(virtio_snd.beep_frames);
    m.console.puts(" pcm_status=");
    m.console.print_hex(virtio_snd.beep_last_status);
    m.console.puts(" latency=");
    m.console.print_u64(virtio_snd.beep_last_latency);
    m.console.puts("\n");
    m.console.puts("beep: stop=");
    m.console.print_hex(virtio_snd.beep_stop_status);
    m.console.puts(" release=");
    m.console.print_hex(virtio_snd.beep_release_status);
    m.console.puts("\n");
    if (st != virtio_snd.S_OK) {
        m.console.puts("beep: FAILED (");
        m.console.puts(if (virtio_snd.beep_fail.len > 0) virtio_snd.beep_fail else "unknown");
        m.console.puts(")\n");
    } else {
        m.console.puts("beep: ok\n");
    }
    return .none;
}

/// `font` — report or switch the terminal's font size (M20-U1). The
/// same setter slot 58 drives; the compositor repaints immediately.
/// M80i (#1725): one key, two ladders — the report shows both. They
/// can disagree at boot (the text layer's default is small, the grid's
/// MEDIUM), so neither is inferred from the other.
pub fn cmd_font(m: *Monitor, args: []const []const u8) ExecError {
    if (args.len == 0) {
        m.console.print_line("font:");
        m.console.puts("  text=");
        m.console.print_line(switch (fbtext.font_size) {
            .small => "small (8x8)",
            .medium => "medium (16x16)",
            .large => "large (24x24)",
        });
        m.console.puts("  grid=");
        m.console.print_line(switch (font_metrics.size) {
            .small => "small (7x13)",
            .medium => "medium (8x16)",
            .large => "large (10x21)",
        });
        return .none;
    }
    const target: ?fbtext.FontSize = if (std.mem.eql(u8, args[0], "small"))
        .small
    else if (std.mem.eql(u8, args[0], "medium"))
        .medium
    else if (std.mem.eql(u8, args[0], "large"))
        .large
    else
        null;
    if (target == null) {
        print_usage(m, lookup("font").?);
        return .usage;
    }
    // The store's font_size apply chain moves BOTH ladders in one pass
    // (settings.set -> apply_font_size -> text + driving_award grid; the
    // flats' single writer is set_size BY CONVENTION — see its note) and
    // publishes the row before the grid's WIN_RESIZE goes out (M80i).
    // When the store refuses the row, NOTHING moves — the ladders never
    // disagree.
    const res = settings.set("font_size", @tagName(target.?));
    if (res != .ok) {
        // Report the store's actual reason — table_full is the only one
        // this call site can reach today (the key and value lengths are
        // bounded by the table above), but SetResult has three failures
        // and the message must not assume which one arrived.
        err_prefix(m);
        m.console.puts("font_size not set: ");
        m.console.puts(switch (res) {
            .table_full => "settings table full",
            .invalid_key => "invalid key",
            .invalid_value => "value too long",
            .ok => "not set",
        });
        m.console.puts("\n");
        return .invalid_argument;
    }
    // Repaint through the compositor when the gpu is up.
    if (virtio_gpu.gpu_ready) {
        driving_award.mark_terminal_dirty();
        _ = driving_award.composite();
    }
    m.console.print_line("font: set (text + grid)");
    return .none;
}

/// `text` — report the framebuffer text layer: the region (rows/cols at
/// the cell size), the cursor (row/col), the scrollback depth, and the
/// colors. `text put <string...>` renders the string and pushes it to
/// the scanout (transfer + flush — the live gate's driver); `text clear`
/// clears + pushes. Honest without the gpu: the report works with no
/// device, `put`/`clear` refuse.
pub fn cmd_text(m: *Monitor, args: []const []const u8) ExecError {
    if (args.len == 0) {
        // M20-U1: rows/cols/cell report the LIVE font-size geometry
        // (visible grid at the current size), not the ring constants.
        m.console.puts("text: rows=");
        m.console.print_u64(fbtext.visible_rows());
        m.console.puts(" cols=");
        m.console.print_u64(fbtext.visible_cols());
        m.console.puts(" cell=");
        m.console.print_u64(fbtext.cur_cell_w());
        m.console.puts("x");
        m.console.print_u64(fbtext.cur_cell_h());
        m.console.puts(" cur=");
        m.console.print_u64(fbtext.cursor_row());
        m.console.puts(",");
        m.console.print_u64(fbtext.cursor_col());
        m.console.puts(" lines=");
        m.console.print_u64(fbtext.line_count());
        m.console.puts(" fg=");
        m.console.print_hex(fbtext.fg_rgb);
        m.console.puts(" bg=");
        m.console.print_hex(fbtext.bg_rgb);
        m.console.puts("\n");
        return .none;
    }
    if (std.mem.eql(u8, args[0], "put") or std.mem.eql(u8, args[0], "putraw")) {
        // M20-U14 gate seam: `putraw` skips the trailing newline so a
        // follow-up `text` report shows the exact landing column.
        const raw = std.mem.eql(u8, args[0], "putraw");
        if (args.len < 2) {
            print_usage(m, lookup("text").?);
            return .usage;
        }
        if (!virtio_gpu.gpu_ready) {
            err_prefix(m);
            m.console.print_line("transport not ready (no virtio-gpu device)");
            return .none;
        }
        var i: usize = 1;
        while (i < args.len) : (i += 1) {
            if (i > 1) fbtext.putc(' ');
            // M20-U10 gate seam: "\t" expands to a real TAB — raw TAB
            // bytes cannot survive the line editor's completion.
            const arg = args[i];
            var j: usize = 0;
            while (j < arg.len) : (j += 1) {
                if (arg[j] == '\\' and j + 1 < arg.len and arg[j + 1] == 't') {
                    fbtext.putc('\t');
                    j += 1;
                } else {
                    fbtext.putc(arg[j]);
                }
            }
        }
        if (!raw) fbtext.putc('\n');
        // Card G5 (claim 1543): present through the Driving Award
        // compositor so the clock overlay stays composited over the
        // repainted terminal.
        driving_award.mark_terminal_dirty();
        const r = driving_award.composite();
        m.console.puts("text put: ");
        print_cmd_result(m, r);
        m.console.puts("\n");
        return .none;
    }
    if (std.mem.eql(u8, args[0], "clear")) {
        if (!virtio_gpu.gpu_ready) {
            err_prefix(m);
            m.console.print_line("transport not ready (no virtio-gpu device)");
            return .none;
        }
        fbtext.clear();
        // Card G5 (claim 1543): present through the compositor (see put).
        driving_award.mark_terminal_dirty();
        const r = driving_award.composite();
        m.console.puts("text clear: ");
        print_cmd_result(m, r);
        m.console.puts("\n");
        return .none;
    }
    if (std.mem.eql(u8, args[0], "fontdebug")) {
        // M20-U11: toggle the missing-glyph dev setting and report stats.
        if (args.len > 1) {
            if (std.mem.eql(u8, args[1], "on")) {
                fbtext.debug_font = true;
            } else if (std.mem.eql(u8, args[1], "off")) {
                fbtext.debug_font = false;
            } else {
                print_usage(m, lookup("text").?);
                return .usage;
            }
        }
        m.console.print_line("fontdebug:");
        m.console.puts("  state=");
        m.console.print_line(if (fbtext.debug_font) "on" else "off");
        m.console.puts("  missing=");
        m.console.print_u64(fbtext.missing_glyph_count);
        // U+XXXX, four hex digits, matching the serial log format of the
        // missing-glyph hook ("text: no glyph for U+XXXX").
        m.console.puts(" last=U+");
        const shifts = [_]u4{ 12, 8, 4, 0 };
        for (shifts) |sh| {
            const d: u8 = @intCast((fbtext.last_missing_cp >> sh) & 0xf);
            m.console.putc(if (d < 10) '0' + d else 'a' + d - 10);
        }
        m.console.puts("\n");
        return .none;
    }
    print_usage(m, lookup("text").?);
    return .usage;
}
