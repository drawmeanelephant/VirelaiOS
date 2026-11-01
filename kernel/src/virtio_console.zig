//! VirelaiOS virtio-pci console transport (extracted verbatim from the
//! former kernel/src/main.zig junk drawer; claim 0023 mechanical split —
//! no behavior change).
//!
//! Modern virtio-pci capabilities, feature negotiation, split-virtqueue
//! setup (queue 1 = transmit), the TX flush path, and the build-gated
//! claim-0017/0020 TX diagnostics that drive the same transport. Discovery
//! and transport setup run PRE-EXIT (config-space and BAR MMIO are
//! firmware-identity-mapped and deterministic); post-exit only the notify
//! MMIO + queue RAM are touched for TX, and every VA used sits below the
//! 4 GiB blanket (mapped Device), so no post-switch fault is possible.
//!
//! The transport is the VZ serial attachment: the runner's
//! VZVirtioConsoleDeviceSerialPortConfiguration appears to the guest as a
//! modern virtio-pci console (VID 0x1af4, DID 0x1043) on bus 0, found via
//! the MCFG ECAM base — not the EFI MMIO windows (which the DSDT proves
//! hold only PCI0 + the efivars store; the 0x20050000 PrimeCell UART is
//! Apple's internal debug console, unconnected to the serial pipe).
//!
//! No libc, no POSIX, no allocation.

const std = @import("std");
const SystemTable = std.os.uefi.tables.SystemTable;
const build_options = @import("build_options");
const mmio = @import("mmio.zig");
const mmu = @import("mmu.zig");
const pci = @import("pci.zig");
const evidence = @import("evidence.zig");
const spinlock = @import("spinlock.zig");
const klog = @import("klog.zig");

// Claim 0020: TX-transition matrix phases. Each option is default off; a
// default build is byte-identical. A diagnostic build enables exactly ONE
// phase, which runs a single controlled TX attempt at its named location
// (A pre-EBS, B post-EBS/pre-MMU, C post-MMU, D final location).
pub const tx_transition_a = build_options.tx_transition_a; // pre-ExitBootServices
pub const tx_transition_b = build_options.tx_transition_b; // post-EBS, firmware translation still active
pub const tx_transition_c = build_options.tx_transition_c; // post identity-map install, before unrelated work
pub const tx_transition_d = build_options.tx_transition_d; // normal final location (banner site)
pub const tx_transition_enabled = tx_transition_a or tx_transition_b or tx_transition_c or tx_transition_d;
// Claim 0020: the matrix is only meaningful with ONE phase per build — a
// second experiment in the same boot would contaminate the transport state
// of the later phase (its TX attempt would no longer be the first on the
// armed transport). Reject combined phase options at compile time.
comptime {
    if (tx_transition_enabled) {
        const phases = @as(usize, @intFromBool(tx_transition_a)) +
            @as(usize, @intFromBool(tx_transition_b)) +
            @as(usize, @intFromBool(tx_transition_c)) +
            @as(usize, @intFromBool(tx_transition_d));
        if (phases != 1) @compileError("tx-transition: enable EXACTLY ONE phase (-Dtx-transition-{a,b,c,d})");
    }
}

// Marker + dump aliases (definitions live in evidence.zig).
const marker_vpscan = evidence.marker_vpscan;
const marker_vpbar = evidence.marker_vpbar;
const marker_vpcap = evidence.marker_vpcap;
const marker_vpcapr = evidence.marker_vpcapr;
const marker_vpwalk = evidence.marker_vpwalk;
const marker_vpdev = evidence.marker_vpdev;
const marker_vptx = evidence.marker_vptx;
const marker_vpok = evidence.marker_vpok;
const marker_pext = evidence.marker_pext;
const marker_pexd = evidence.marker_pexd;
const marker_txst = evidence.marker_txst;
const marker_txnt = evidence.marker_txnt;
const marker_txpl = evidence.marker_txpl;
const marker_txfl = evidence.marker_txfl;
const marker_txda = evidence.marker_txda;
const marker_txcc = evidence.marker_txcc;
const marker_txbr = evidence.marker_txbr;
const marker_txar = evidence.marker_txar;
const marker_txbn = evidence.marker_txbn;
const marker_txan = evidence.marker_txan;
const marker_txup = evidence.marker_txup;
const marker_txuc = evidence.marker_txuc;
const marker_txfr = evidence.marker_txfr;
const marker_tra1 = evidence.marker_tra1;
const marker_tra2 = evidence.marker_tra2;
const marker_trau = evidence.marker_trau;
const marker_trb1 = evidence.marker_trb1;
const marker_trb2 = evidence.marker_trb2;
const marker_trbu = evidence.marker_trbu;
const marker_trc1 = evidence.marker_trc1;
const marker_trc2 = evidence.marker_trc2;
const marker_trcu = evidence.marker_trcu;
const marker_trd1 = evidence.marker_trd1;
const marker_trd2 = evidence.marker_trd2;
const marker_trdu = evidence.marker_trdu;
const marker_trnx = evidence.marker_trnx;

const VirtqDesc = extern struct {
    addr: u64,
    len: u32,
    flags: u16,
    next: u16,
};
const VirtqAvail = extern struct {
    flags: u16,
    idx: u16,
    ring: [1]u16,
};
const VirtqUsedElem = extern struct {
    id: u32,
    len: u32,
};
const VirtqUsed = extern struct {
    flags: u16,
    idx: u16,
    ring: [1]VirtqUsedElem,
};
pub var virtio_desc: [1]VirtqDesc align(64) = undefined;
pub var virtio_avail: VirtqAvail align(64) = undefined;
pub var virtio_used: VirtqUsed align(64) = undefined;
/// CPU-owned staging: `uart_putc` appends here at any time, so the device
/// never reads it (#1972). A flush copies the staged unit into
/// `virtio_tx_dma`.
pub var virtio_tx: [128]u8 align(64) = undefined;
/// The only buffer the TX descriptor names. Written only while no
/// descriptor is outstanding, so bytes the device may still read are never
/// rewritten.
var virtio_tx_dma: [128]u8 align(64) = undefined;
var virtio_last_used: u16 = 0;
// Split-ring size (must be a power of 2, Virtio 1.3 §4.1.4.3): one
// descriptor, no chaining. The number of outstanding buffers
// (avail.idx - used.idx) must never exceed this (§2.7).
const virtio_queue_size: u16 = 1;

// Transport state. Discovery and setup run pre-exit; post-exit the TX path
// reads only the notify MMIO + queue RAM (main.zig reads vp_ready/vp_bar0
// for the identity map's extra Device window and the fw-mmu-capture diag).
pub var vp_dev: u32 = 0; // console PCI device number
pub var vp_ready: bool = false; // transport initialized, TX path armed
pub var vp_common: u64 = 0; // common-config struct address (BAR + cap offset)
pub var vp_notify: u64 = 0; // notify region base (BAR + cap offset)
pub var vp_notify_mult: u32 = 0; // notify_off_multiplier
pub var vp_queue_notify_off: u16 = 0; // queue 1 notify offset
pub var vp_common_off: u32 = 0; // common cfg offset within the BAR
pub var vp_notify_off: u32 = 0; // notify cfg offset within the BAR
pub var vp_bar0: u64 = 0; // console BAR0 base (the SEL record)
pub var vp_tx_len: usize = 0; // bytes staged in virtio_tx
pub var st_tx: ?*const SystemTable = null; // for post-exit flush stage markers

// Claim 6684: virtio-console receive queue (queue 0) + polled RX. The
// console device delivers host input by writing into driver-supplied
// buffers on queue 0 (virtio-console spec: queue 0 = receive, queue 1 =
// transmit). One 256-byte WRITE descriptor; the used ring names returned
// buffers, which are drained into a bounded FIFO for readByte.
// Issue #843 (Shape A): virtqueue descriptors, rings, and buffers are aligned
// to 64 bytes (the AArch64 D-cache line size) so device DMA writes and CPU
// cache invalidations (dc ivac) never share a cache line with CPU-written
// variables or avail rings.
pub var virtio_rx_desc: [1]VirtqDesc align(64) = undefined;
pub var virtio_rx_avail: VirtqAvail align(64) = undefined;
pub var virtio_rx_used: VirtqUsed align(64) = undefined;
pub var virtio_rx_buf: [256]u8 align(64) = undefined;
var virtio_rx_last_used: u16 = 0;
var virtio_rx_queue_notify_off: u16 = 0;
var virtio_rx_fifo: [512]u8 = undefined;
var virtio_rx_fifo_head: usize = 0;
var virtio_rx_fifo_len: usize = 0;
var virtio_rx_armed: bool = false;
// Handed-back front-end input: bytes a detaching serial front-end pumped
// but never consumed return here (sys_tty_attach selector 0), read BEFORE
// the device FIFO so the monitor sees the type-ahead before newer host
// arrivals. Plain guest RAM, so the lane rides the suspend image like the
// rings it fronts.
var virtio_rx_inject: [256]u8 = undefined;
var virtio_rx_inject_head: usize = 0;
var virtio_rx_inject_len: usize = 0;

fn vp_read8(off: u32) u8 {
    return mmio.mmio_read8(vp_common + off);
}

fn vp_write8(off: u32, value: u8) void {
    mmio.mmio_write8(vp_common + off, value);
}

fn vp_read16(off: u32) u16 {
    return mmio.mmio_read16(vp_common + off);
}

fn vp_write16(off: u32, value: u16) void {
    mmio.mmio_write16(vp_common + off, value);
}

fn vp_read32(off: u32) u32 {
    return mmio.mmio_read32(vp_common + off);
}

fn vp_write32(off: u32, value: u32) void {
    mmio.mmio_write32(vp_common + off, value);
}

/// Claim 0013: initialize the modern virtio-pci console as the kernel
/// console. Walks the device's virtio capabilities (ID 0x09, cfg types
/// common/notify), programs the modern transport (features, queue 1 =
/// transmit), and arms TX. PRE-EXIT only; evidence is dumped to the probe
/// buffer so the host sees the state either way.
pub fn virtio_pci_init(st: *const SystemTable) bool {
    if (pci.pci_ecam == 0) {
        evidence.dump_str("VP: no ECAM\n");
        return false;
    }

    // Locate the console: modern DID 0x1043, legacy 0x1003 (this milestone
    // drives the modern transport only). Bus 0, function 0.
    var console_dev: u32 = 32;
    var dev: u32 = 0;
    while (dev < 32) : (dev += 1) {
        const id = pci.pci_read32(pci.pci_ecam, 0, dev, 0, 0);
        if ((id & 0xffff) == 0xffff) continue;
        const did = id >> 16;
        if (did == 0x1043 or did == 0x1003) {
            console_dev = dev;
            break;
        }
    }
    if (console_dev == 32) {
        evidence.dump_str("VP: no virtio-console PCI device\n");
        return false;
    }
    vp_dev = console_dev;
    evidence.dump_str("VP dev=");
    evidence.dump_hex(console_dev);
    evidence.dump_str("\n");
    evidence.write_marker_var(st, marker_vpscan);

    // BAR bases (memory BARs; 64-bit pairs merged).
    var bar_base: [6]u64 = @splat(0);
    var bi: usize = 0;
    while (bi < 6) : (bi += 1) {
        const low = pci.pci_read32(pci.pci_ecam, 0, console_dev, 0, 0x10 + @as(u32, @intCast(bi)) * 4);
        if ((low & 1) != 0) continue; // I/O space — ignored
        const base: u64 = low & ~@as(u32, 0xf);
        bar_base[bi] = base;
        if (((low >> 1) & 0x3) == 2 and bi + 1 < 6) { // 64-bit BAR
            const high = pci.pci_read32(pci.pci_ecam, 0, console_dev, 0, 0x10 + @as(u32, @intCast(bi + 1)) * 4);
            bar_base[bi] |= @as(u64, high) << 32;
            bi += 1;
        }
    }
    evidence.write_marker_var(st, marker_vpbar);

    // Walk the capability list for the virtio vendor-specific caps (ID 0x09):
    // each carries cfg_type + bar + offset; the notify cap adds a multiplier.
    evidence.write_marker_var(st, marker_vpcap);
    const cap_ptr = pci.pci_read32(pci.pci_ecam, 0, console_dev, 0, 0x34) & 0xff;
    evidence.write_marker_var(st, marker_vpcapr);
    var common_bar: u32 = 0;
    var common_off: u32 = 0;
    var notify_bar: u32 = 0;
    var notify_off: u32 = 0;
    var notify_mult: u32 = 0;
    var found_common = false;
    var found_notify = false;
    var c: u32 = cap_ptr;
    var caps: usize = 0;
    // The virtio_pci_cap header packs id/next/len/cfg_type into one aligned
    // u32; offset/length are the next two aligned u32s. Cap bases are 4-byte
    // aligned (0x40/0x50/0x60/0x74 on VZ), so every read below is aligned —
    // unaligned accesses fault on Device memory, and byte reads return
    // garbage on VZ (claim 0013); aligned u32 reads are the coherent view.
    while (c != 0 and c < 0x100 and (c & 3) == 0 and caps < 16) : (caps += 1) {
        const head = pci.pci_read32(pci.pci_ecam, 0, console_dev, 0, c);
        const id = head & 0xff;
        const next = (head >> 8) & 0xff;
        if (id == 0x09) {
            const cfg_type = (head >> 24) & 0xff;
            const bar = pci.pci_read32(pci.pci_ecam, 0, console_dev, 0, c + 4) & 0xff;
            const off = pci.pci_read32(pci.pci_ecam, 0, console_dev, 0, c + 8);
            switch (cfg_type) {
                1 => {
                    common_bar = bar;
                    common_off = off;
                    found_common = true;
                },
                2 => {
                    notify_bar = bar;
                    notify_off = off;
                    notify_mult = pci.pci_read32(pci.pci_ecam, 0, console_dev, 0, c + 16);
                    found_notify = true;
                },
                else => {},
            }
        }
        c = next;
    }
    evidence.write_marker_var(st, marker_vpwalk);

    // NOTE: no BAR rebase pre-exit — moving the window makes the device
    // unreachable pre-exit (observed: after rebasing to 0x10000, reads of
    // BOTH the old firmware base and 0x10000 hang — the firmware never maps
    // the low address). The firmware-assigned base is used for setup and
    // mapped in place post-switch (mmu.zig); no post-exit rebase runs — the
    // attempt was abandoned because post-exit config writes cannot move the
    // BAR on VZ (claims 0013/0020, docs/hardware-contract.md).
    // Offset 0 is a legitimate common-cfg offset (VZ: common @ BAR0+0x00);
    // the caps must simply both be present.
    if (!found_common or !found_notify or common_bar >= 6 or notify_bar >= 6) {
        evidence.dump_str("VP: missing capability structs\n");
        return false;
    }
    vp_common = bar_base[common_bar] + common_off;
    vp_notify = bar_base[notify_bar] + notify_off;
    vp_notify_mult = notify_mult;
    vp_common_off = common_off;
    vp_notify_off = notify_off;
    vp_bar0 = bar_base[0];
    evidence.dump_str("VP common=");
    evidence.dump_hex(vp_common);
    evidence.dump_str(" notify=");
    evidence.dump_hex(vp_notify);
    evidence.dump_str(" mult=");
    evidence.dump_hex(notify_mult);
    evidence.dump_str("\n");
    // Stage marker: device found, BARs + capabilities resolved. If the ladder
    // stops here, a config-space read in the walk is the death site.
    evidence.write_marker_var(st, marker_vpdev);
    // Persist the walk results now: if a transport write below stalls, the
    // host still sees the resolved common/notify addresses. Claim 3475: only
    // in `-Dprobe-var` diagnostic builds — VZ's append-per-write variable
    // store fills fast, and the ~32 KiB persist per boot (16 chunks × 2 KiB)
    // starved the store so the ESP file window's `write` always failed with
    // EFI_OUT_OF_RESOURCES (observed, claim 3475). The serial log carries
    // the probe records; claim 0015 gated the persist off in nvram-console
    // builds for the same starvation.
    if (comptime build_options.probe_var) evidence.write_probe_var(st);

    // Modern transport init: reset, ACKNOWLEDGE|DRIVER, accept
    // VIRTIO_F_VERSION_1, FEATURES_OK.
    vp_write8(0x14, 0); // reset
    // Virtio 1.3 §4.1.4.3: "After writing 0 to device_status, the driver
    // MUST wait for a read of device_status to return 0 before reinitializing
    // the device." Bounded poll so a stuck device fails honestly instead of
    // hanging, and the ACKNOWLEDGE write below can never race a device that
    // is still mid-reset.
    var reset_spins: usize = 0;
    while (vp_read8(0x14) != 0) : (reset_spins += 1) {
        if (reset_spins >= 1_000_000) {
            evidence.dump_str("VP: reset timeout\n");
            return false;
        }
    }
    vp_write8(0x14, 1 | 2); // ACKNOWLEDGE | DRIVER
    const features_lo = vp_read32(0x04);
    // device_feature_select lives at 0x00 (0x08 is the DRIVER's select).
    vp_write32(0x00, 1);
    const features_hi = vp_read32(0x04);
    vp_write32(0x00, 0);
    evidence.dump_str("VP feats=");
    evidence.dump_hex(features_lo);
    evidence.dump_str("/");
    evidence.dump_hex(features_hi);
    evidence.dump_str("\n");
    if ((features_hi & 1) == 0) {
        evidence.dump_str("VP: no VIRTIO_F_VERSION_1\n");
        return false;
    }
    vp_write32(0x08, 1);
    vp_write32(0x0c, 1); // accept VIRTIO_F_VERSION_1
    vp_write32(0x08, 0);
    vp_write32(0x0c, 0);
    vp_write8(0x14, 1 | 2 | 8); // FEATURES_OK
    if ((vp_read8(0x14) & 8) == 0) {
        evidence.dump_str("VP: FEATURES_OK failed\n");
        return false;
    }

    // Queue 1 = console transmit. Size 1 (one descriptor, no chaining).
    vp_write16(0x16, 1); // queue_select
    const qsz = vp_read16(0x18);
    if (qsz == 0) {
        evidence.dump_str("VP: queue 1 absent\n");
        return false;
    }
    vp_write16(0x18, virtio_queue_size); // queue_size = 1 (power of 2, §4.1.4.3)
    virtio_desc[0] = .{ .addr = 0, .len = 0, .flags = 0, .next = 0 };
    virtio_avail = .{ .flags = 0, .idx = 0, .ring = .{0} };
    virtio_used = .{ .flags = 0, .idx = 0, .ring = .{.{ .id = 0, .len = 0 }} };
    // Clean the used ring's init write to RAM now: BSS is not trusted zeroed
    // here, and the flush's dc ivac on this line (the ring-full guard and the
    // used-poll) would otherwise discard this dirty write and re-read stale
    // RAM garbage as used.idx — which the guard would then treat as "ring
    // full" and drop TX forever.
    mmu.clean_dcache_range(@intFromPtr(&virtio_used), @sizeOf(VirtqUsed));
    virtio_last_used = 0;
    vp_tx_len = 0;
    // Queue GPA registers are le64; VZ's common-cfg emulation accepts 32-bit
    // accesses (claim 0013: byte reads of config space return garbage — the
    // emulation has access-size quirks), so write each half as a 32-bit store
    // rather than a single 64-bit one that may be dropped.
    // Claim 5804: the device reads these queue GPAs as guest PHYSICAL
    // addresses, so translate the post-jump kernel VAs back to phys.
    const qd = mmu.to_phys(@intFromPtr(&virtio_desc));
    mmio.mmio_write32(vp_common + 0x20, @truncate(qd));
    mmio.mmio_write32(vp_common + 0x24, @truncate(qd >> 32));
    const qa = mmu.to_phys(@intFromPtr(&virtio_avail));
    mmio.mmio_write32(vp_common + 0x28, @truncate(qa));
    mmio.mmio_write32(vp_common + 0x2c, @truncate(qa >> 32));
    const qu = mmu.to_phys(@intFromPtr(&virtio_used));
    mmio.mmio_write32(vp_common + 0x30, @truncate(qu));
    mmio.mmio_write32(vp_common + 0x34, @truncate(qu >> 32));
    vp_write16(0x1c, 1); // queue_enable
    vp_queue_notify_off = vp_read16(0x1e);
    // Claim 6684: queue 0 = console receive. Same split-ring shape as TX
    // (size 1, one WRITE descriptor pointing at the fixed BSS buffer). The
    // device fills this buffer with host input bytes and names the return
    // in the used ring. Configured pre-exit alongside queue 1 and the
    // initial avail entry is supplied BEFORE DRIVER_OK (the driver may
    // populate the avail ring; it must only NOT notify until DRIVER_OK);
    // the kick happens right after DRIVER_OK below so the device can accept
    // host bytes as soon as the runner forwards them.
    vp_write16(0x16, 0); // queue_select = 0 (receive)
    const qsz_rx = vp_read16(0x18);
    if (qsz_rx == 0) {
        evidence.dump_str("VP: queue 0 (RX) absent — console output-only\n");
        virtio_rx_armed = false;
    } else {
        vp_write16(0x18, virtio_queue_size); // queue_size = 1 (power of 2)
        virtio_rx_desc[0] = .{ .addr = mmu.to_phys(@intFromPtr(&virtio_rx_buf)), .len = virtio_rx_buf.len, .flags = 2, .next = 0 }; // VIRTQ_DESC_F_WRITE
        virtio_rx_avail = .{ .flags = 0, .idx = 0, .ring = .{0} };
        virtio_rx_used = .{ .flags = 0, .idx = 0, .ring = .{.{ .id = 0, .len = 0 }} };
        mmu.clean_dcache_range(@intFromPtr(&virtio_rx_used), @sizeOf(VirtqUsed));
        virtio_rx_last_used = 0;
        virtio_rx_fifo_head = 0;
        virtio_rx_fifo_len = 0;
        // Queue GPA registers: same 32-bit-half pattern as queue 1 (claim
        // 0013 access-size quirk).
        const qdr = mmu.to_phys(@intFromPtr(&virtio_rx_desc));
        mmio.mmio_write32(vp_common + 0x20, @truncate(qdr));
        mmio.mmio_write32(vp_common + 0x24, @truncate(qdr >> 32));
        const qar = mmu.to_phys(@intFromPtr(&virtio_rx_avail));
        mmio.mmio_write32(vp_common + 0x28, @truncate(qar));
        mmio.mmio_write32(vp_common + 0x2c, @truncate(qar >> 32));
        const qur = mmu.to_phys(@intFromPtr(&virtio_rx_used));
        mmio.mmio_write32(vp_common + 0x30, @truncate(qur));
        mmio.mmio_write32(vp_common + 0x34, @truncate(qur >> 32));
        vp_write16(0x1c, 1); // queue_enable
        virtio_rx_queue_notify_off = vp_read16(0x1e);
        virtio_rx_avail.ring[0] = 0;
        virtio_rx_avail.idx +%= 1;
        mmu.clean_dcache_range(@intFromPtr(&virtio_rx_desc), @sizeOf(VirtqDesc));
        mmu.clean_dcache_range(@intFromPtr(&virtio_rx_avail), @sizeOf(VirtqAvail));
        virtio_rx_armed = true;
        evidence.dump_str("VP RX qsz=");
        evidence.dump_hex(vp_read16(0x18));
        evidence.dump_str(" qen=");
        evidence.dump_hex(vp_read16(0x1c));
        evidence.dump_str(" qoff=");
        evidence.dump_hex(virtio_rx_queue_notify_off);
        evidence.dump_str("\n");
    }
    vp_write8(0x14, 1 | 2 | 8 | 4); // DRIVER_OK
    if ((vp_read8(0x14) & 4) == 0) {
        evidence.dump_str("VP: DRIVER_OK failed\n");
        return false;
    }
    // Claim 6684: kick queue 0 so the device knows the first RX buffer is
    // available. The notification value is the queue index (0) —
    // VIRTIO_F_NOTIFICATION_DATA is not negotiated, so a 16-bit store per
    // §4.1.5.2.1.
    if (virtio_rx_armed) {
        mmio.mmio_write16(vp_notify + @as(u64, virtio_rx_queue_notify_off) * vp_notify_mult, 0);
    }
    // Stage marker: transport armed. If the ladder stops here, a queue-setup
    // write (mmio_write64 at vp_common+0x20..0x30) is the death site.
    evidence.write_marker_var(st, marker_vptx);
    // Pre-exit readback of the queue setup: qsz/qen/qoff + the GPA halves
    // written above + device status. Verifies the 32-bit queue writes landed.
    evidence.dump_str("VP qsz=");
    evidence.dump_hex(vp_read16(0x18));
    evidence.dump_str(" qen=");
    evidence.dump_hex(vp_read16(0x1c));
    evidence.dump_str(" qoff=");
    evidence.dump_hex(vp_read16(0x1e));
    evidence.dump_str(" qd=");
    evidence.dump_hex(mmio.mmio_read32(vp_common + 0x20));
    evidence.dump_str(" qa=");
    evidence.dump_hex(mmio.mmio_read32(vp_common + 0x28));
    evidence.dump_str(" qu=");
    evidence.dump_hex(mmio.mmio_read32(vp_common + 0x30));
    evidence.dump_str(" st=");
    evidence.dump_hex(vp_read8(0x14));
    evidence.dump_str("\n");
    vp_ready = true;
    evidence.dump_str("VP ready qoff=");
    evidence.dump_hex(vp_queue_notify_off);
    evidence.dump_str("\n");
    evidence.write_marker_var(st, marker_vpok);
    return true;
}

/// Transmit the staged TX bytes through queue 1: copy them into the DMA
/// buffer, post it in the desc/avail rings, clean the D-cache (the device
/// reads guest RAM directly), kick via the notify region, and wait for the
/// used ring. Runs POST-EXIT on the pre-exit-captured VAs. A stuck device
/// times out and costs dropped lines instead of a hung kernel (TX remains
/// honest: the serial log is the gate, and M2_TXOK! records that the path
/// returned); `tx_unit` below says what a drop is.
/// Claim 3475: the coarse TXST!/TXNT!/TXPL! stage markers (and the post-exit
/// probe tail) are written only for the FIRST flush. They exist to name the
/// hang site of the historically fatal first post-exit TX (claim 0013/0018
/// era) — every later flush was writing 3 more ~88-byte variables into VZ's
/// append-only variable store: ~113 flushes/boot ≈ 30 KiB of pure marker
/// traffic that exhausted the store and broke `write` (EFI_OUT_OF_RESOURCES,
/// claim 3475). Diagnostic builds (`-Dtx-diag`) keep their own full
/// per-operation bracketing; the first-flush markers preserve the original
/// hang-site evidence path for every gate that reads the ladder (preexit-tx,
/// tx-transition, t0sz16 all analyze only the first/transition flush).
var vp_first_flush: bool = true;

/// WM2 (#707 card 2 port, claim #980): the TX critical section runs on BOTH
/// cores (core 0's shell/WM prints and the secondary's app prints), with NO
/// synchronization — two concurrent flushes raced the 1-deep TX ring: one
/// saw `outstanding >= queue_size` and DROPPED the line, so an EL0 app's
/// console writes intermittently never reached the serial (the WM2 gate
/// keys its script phases on the demo's prints and lost ~50% of WM-mode
/// boots to it). Serialize flushes with an IRQ-save spinlock, and give the
/// full-guard a bounded retry (`tx_reap_polls` re-polls of the used ring)
/// before the honest drop — a truly stuck device still drops, a contended
/// one now gets through. Default-build byte-difference: only lines that were
/// previously DROPPED can now appear; no existing line changes shape.
var vp_tx_lock = spinlock.IrqSaveSpinlock{};

pub fn virtio_pci_flush() void {
    if (!vp_ready or vp_tx_len == 0) return;
    // WM2 (claim #980): serialize concurrent flushers (see vp_tx_lock above).
    const daif = vp_tx_lock.lock();
    defer vp_tx_lock.unlock(daif);
    // Claim 0018 (build-gated -Dtx-diag): ten ordered 8-byte NVRAM markers
    // bracket each potentially fatal operation of the (first) post-exit
    // transmission — the ladder's last marker names the smallest confirmed
    // failure interval. The diag flush also drops the large post-exit
    // probe-tail SetVariable (a big post-exit write, the class claim 0013
    // proved hangs on VZ) and the logging-only status dump; the status read
    // itself stays, bracketed by TXBR!/TXAR!. Default builds are
    // byte-identical (coarse TXST!/TXNT!/TXPL! evidence path unchanged).
    if (comptime build_options.tx_diag) {
        if (st_tx != null) evidence.write_marker_var(st_tx.?, marker_txfl); // 1 entered virtio flush
    }
    var hw: HwTx = .{};
    tx_unit(&hw, &tx_state, virtio_tx[0..vp_tx_len]);
    vp_tx_len = 0;
}

// ---------------------------------------------------------------------------
// #1972: TX buffer ownership. The unit of transfer is one flush: a line, or
// a newline-less piece of one (a full staging buffer, a prompt). A post
// whose completion outlived the poll bound used to be forgotten: the next
// line was staged into the buffer the device still had to read, and the
// device later sent the earlier line with its first bytes replaced (the M91
// splice signature). Now:
//   - The device reads only `virtio_tx_dma`, which is written only while the
//     used ring shows no outstanding descriptor.
//   - A flush that finds the descriptor still outstanding (an earlier post
//     timed out) re-polls `tx_reap_polls` times, then drops its unit whole.
//     It does not wait again, so a device that never completes costs one
//     bounded wait in total, not one per line.
//   - A line that loses a unit loses the rest of it too, through its
//     newline, and counts once. Once the device returns the buffer, one
//     `virtio-console: tx-dropped lines=<boot total>` line precedes the next
//     unit, on a fresh line if the host was left mid-line. A boot without
//     drops prints nothing new.
// Lines are those of the transmitted stream. Writers on different cores
// interleave at `uart_puts` granularity, so the newline that ends a dropped
// run can belong to another writer's line.
// The logic is generic over the transport: `HwTx` here, a simulated device
// in the host tests.
// ---------------------------------------------------------------------------

/// How long one post waits for the device to return the buffer.
const tx_complete_wait_ms: u64 = 1000;
/// Completion-wait bound in used-ring looks when CNTFRQ_EL0 reads zero (the
/// pre-#1972 bound).
const tx_complete_wait_polls: usize = 2_000_000;
/// Used-ring looks before a flush drops a unit because the device still
/// owns the DMA buffer.
const tx_reap_polls: usize = 8;

pub const tx_drop_notice = "virtio-console: tx-dropped lines=";
const tx_notice_max = 64;

const TxState = struct {
    /// The line in progress lost a unit: drop its remaining units through
    /// the newline.
    dropping: bool = false,
    /// The last unit handed to the device ended a line.
    host_at_bol: bool = true,
    /// Lines that lost at least one unit this boot, and the total the last
    /// notice announced.
    dropped: u32 = 0,
    reported: u32 = 0,
};
var tx_state: TxState = .{};

fn tx_unit(hw: anytype, s: *TxState, unit: []const u8) void {
    std.debug.assert(unit.len > 0 and unit.len <= virtio_tx_dma.len);
    const ends_line = unit[unit.len - 1] == '\n';
    if (s.dropping) {
        s.dropping = !ends_line;
        return;
    }
    if (!tx_reap(hw)) return tx_drop(s, ends_line);
    if (s.dropped != s.reported) {
        var buf: [tx_notice_max]u8 = undefined;
        tx_send(hw, s, tx_notice(&buf, s.dropped, s.host_at_bol));
        s.reported = s.dropped;
        if (!tx_reap(hw)) return tx_drop(s, ends_line);
    }
    tx_send(hw, s, unit);
}

fn tx_drop(s: *TxState, ends_line: bool) void {
    s.dropped +|= 1;
    s.dropping = !ends_line;
}

/// True when no descriptor is outstanding, so `hw.dma()` may be written.
/// Split-ring invariant (Virtio 1.3 §2.7): outstanding buffers
/// (avail.idx - used.idx) never exceed the queue size, or a new entry would
/// overwrite a ring slot the device has not consumed.
fn tx_reap(hw: anytype) bool {
    var looks: usize = 1;
    while (hw.outstanding() != 0) : (looks += 1) {
        if (looks >= tx_reap_polls) return false;
    }
    hw.reaped();
    return true;
}

fn tx_send(hw: anytype, s: *TxState, bytes: []const u8) void {
    @memcpy(hw.dma()[0..bytes.len], bytes);
    hw.post(bytes.len);
    hw.finish(tx_wait(hw));
    s.host_at_bol = bytes[bytes.len - 1] == '\n';
}

/// Bounded completion wait: `tx_complete_wait_ms` of the counter, or
/// `tx_complete_wait_polls` looks when the counter frequency is unknown.
fn tx_wait(hw: anytype) bool {
    const bound = hw.wait_ticks();
    const start = hw.now();
    var looks: usize = 1;
    while (hw.outstanding() != 0) : (looks += 1) {
        if (bound == 0) {
            if (looks >= tx_complete_wait_polls) return false;
        } else if (hw.now() -% start >= bound) return false;
    }
    return true;
}

fn tx_notice(buf: *[tx_notice_max]u8, total: u32, at_bol: bool) []const u8 {
    const lead: []const u8 = if (at_bol) "" else "\n";
    return std.fmt.bufPrint(buf, "{s}" ++ tx_drop_notice ++ "{d}\n", .{ lead, total }) catch unreachable;
}

/// Queue 1 behind `tx_unit`, with the claim 0013/0018/3475 evidence ladder
/// at its pre-#1972 points.
const HwTx = struct {
    fn dma(_: *HwTx) *[128]u8 {
        return &virtio_tx_dma;
    }

    /// Descriptors the device has not returned. The used ring is
    /// device-written: invalidate its line before reading it.
    fn outstanding(_: *HwTx) u16 {
        mmu.invalidate_dcache_range(@intFromPtr(&virtio_used), @sizeOf(VirtqUsed));
        asm volatile ("dmb ishld" ::: .{ .memory = true });
        return virtio_avail.idx -% virtio_used.idx;
    }

    fn reaped(_: *HwTx) void {
        virtio_last_used = virtio_used.idx;
    }

    fn now(_: *HwTx) u64 {
        return asm volatile ("mrs %[v], cntvct_el0"
            : [v] "=r" (-> u64),
        );
    }

    fn wait_ticks(_: *HwTx) u64 {
        const freq = asm volatile ("mrs %[v], cntfrq_el0"
            : [v] "=r" (-> u64),
        );
        return freq * tx_complete_wait_ms / 1000;
    }

    fn post(_: *HwTx, len: usize) void {
        vp_tx_post(len);
    }

    fn finish(_: *HwTx, completed: bool) void {
        vp_tx_finish(completed);
    }
};

fn vp_tx_post(len: usize) void {
    const st = st_tx;
    virtio_desc[0] = .{ .addr = mmu.to_phys(@intFromPtr(&virtio_tx_dma)), .len = @intCast(len), .flags = 0, .next = 0 };
    virtio_avail.ring[0] = 0; // descriptor index 0
    asm volatile ("dmb ishst" ::: .{ .memory = true });
    virtio_avail.idx +%= 1;
    if (comptime build_options.tx_diag) {
        if (st != null) evidence.write_marker_var(st.?, marker_txda); // 2 descriptor/avail buffers prepared
    }
    mmu.clean_dcache_range(@intFromPtr(&virtio_desc), @sizeOf(VirtqDesc));
    mmu.clean_dcache_range(@intFromPtr(&virtio_avail), @sizeOf(VirtqAvail));
    mmu.clean_dcache_range(@intFromPtr(&virtio_tx_dma), len);
    if (comptime build_options.tx_diag) {
        if (st != null) evidence.write_marker_var(st.?, marker_txcc); // 3 DMA cache clean completed
    }
    if (comptime build_options.tx_diag) {
        // 4/5: the first post-exit BAR/common-config access (device status
        // read). Bracketed so a hang here is distinguishable from a hang at
        // the notify.
        if (st != null) evidence.write_marker_var(st.?, marker_txbr);
        _ = vp_read8(0x14);
        if (st != null) evidence.write_marker_var(st.?, marker_txar);
    } else {
        // Claim 0013 evidence path (default build): coarse markers + post-exit
        // status probe + probe tail — markers/tail on the FIRST flush only
        // (claim 3475; see vp_first_flush above).
        if (st_tx != null and vp_first_flush) evidence.write_marker_var(st_tx.?, marker_txst);
        evidence.dump_str("VP pst=");
        evidence.dump_hex(vp_read8(0x14));
        evidence.dump_str("\n");
        // Claim 0015: the post-exit probe tail is skipped in nvram-console
        // builds for the same store-budget reason as the full dump. Claim
        // 0020: it is also skipped in TX-transition builds — a 512-byte
        // post-exit SetVariable inside the bracketed window would be a
        // confound (the class of write claim 0013 proved hangs post-exit;
        // claim 0018 removed it in diag builds for the same reason), leaving
        // only MMIO accesses + marker writes in the window.
        if (comptime (!build_options.nvram_console and !tx_transition_enabled)) {
            if (st_tx != null and vp_first_flush) evidence.write_probe_tail(st_tx.?);
        }
    }
    if (comptime build_options.tx_diag) {
        if (st != null) evidence.write_marker_var(st.?, marker_txbn); // 6 before queue notify MMIO write
    }
    // Virtio 1.3 §4.1.5.2.1: VIRTIO_F_NOTIFICATION_DATA is NOT negotiated
    // (only VERSION_1 was accepted above), so the driver notification MUST be
    // a 16-bit write whose value is the virtqueue index (1 = TX queue); the
    // address is cap.offset + queue_notify_off * notify_off_multiplier
    // (§4.1.4.4). A 32-bit store is a MUST violation even though many devices
    // tolerate it; a device not offering NOTIFICATION_DATA MUST accept 2-byte
    // accesses (§4.1.4.4.1). (Claim 0013's "16-bit store may be dropped"
    // belief has no documented evidence; the report flags it as inference.)
    mmio.mmio_write16(vp_notify + @as(u64, vp_queue_notify_off) * vp_notify_mult, 1);
    if (comptime build_options.tx_diag) {
        if (st != null) evidence.write_marker_var(st.?, marker_txan); // 7 after notify
    } else {
        if (st_tx != null and vp_first_flush) evidence.write_marker_var(st_tx.?, marker_txnt);
    }
    if (comptime build_options.tx_diag) {
        if (st != null) evidence.write_marker_var(st.?, marker_txup); // 8 entered used-ring poll
    }
}

fn vp_tx_finish(completed: bool) void {
    const st = st_tx;
    if (comptime build_options.tx_diag) {
        if (completed and st != null) evidence.write_marker_var(st.?, marker_txuc); // 9 device changed used.idx
    }
    virtio_last_used = virtio_used.idx;
    if (comptime build_options.tx_diag) {
        if (st != null) evidence.write_marker_var(st.?, marker_txfr); // 10 flush returned
    } else {
        if (st_tx != null and vp_first_flush) evidence.write_marker_var(st_tx.?, marker_txpl);
    }
    vp_first_flush = false;
}

// ---------------------------------------------------------------------------
// Claim 6684: polled receive path (virtio-console queue 0).
// ---------------------------------------------------------------------------

/// Pop one byte from the drained-input FIFO (caller guarantees non-empty).
fn rx_fifo_pop() u8 {
    const b = virtio_rx_fifo[virtio_rx_fifo_head];
    virtio_rx_fifo_head +%= 1;
    if (virtio_rx_fifo_head >= virtio_rx_fifo.len) virtio_rx_fifo_head = 0;
    virtio_rx_fifo_len -= 1;
    return b;
}

/// Polled receive: one host-input byte, or null when no input is available
/// right now. Polls queue 0's used ring; when the device returns the buffer
/// (used.idx advanced) the bytes are drained into the bounded FIFO, the
/// buffer is re-supplied (clean + kick), and the first FIFO byte is
/// returned. Never blocks, never allocates. Runs post-MMU on the
/// pre-exit-captured VAs — the transport is reachable since claim 1517.
pub fn virtio_read_byte() ?u8 {
    if (!virtio_rx_armed) return null;
    // Handed-back front-end input first: it is older than anything the
    // device has delivered since.
    if (virtio_rx_inject_len > 0) {
        const b = virtio_rx_inject[virtio_rx_inject_head];
        virtio_rx_inject_head = (virtio_rx_inject_head + 1) % virtio_rx_inject.len;
        virtio_rx_inject_len -= 1;
        return b;
    }
    // Drain bytes already received on an earlier poll first.
    if (virtio_rx_fifo_len > 0) return rx_fifo_pop();
    // Poll the used ring (device-written RAM; invalidate its line first).
    mmu.invalidate_dcache_range(@intFromPtr(&virtio_rx_used), @sizeOf(VirtqUsed));
    if (virtio_rx_used.idx == virtio_rx_last_used) return null;
    // VirtIO 1.3 §2.7.8: memory barrier after reading used.idx before reading descriptor entry.
    asm volatile ("dmb ishld" ::: .{ .memory = true });
    // The device returned the buffer: read what it wrote, cache-correct.
    const elem = virtio_rx_used.ring[0];
    const n = @min(elem.len, virtio_rx_buf.len);
    mmu.invalidate_dcache_range(@intFromPtr(&virtio_rx_buf), n);
    var i: usize = 0;
    while (i < n) : (i += 1) {
        if (virtio_rx_fifo_len >= virtio_rx_fifo.len) break; // FIFO full: drop the rest, honestly
        virtio_rx_fifo[(virtio_rx_fifo_head + virtio_rx_fifo_len) % virtio_rx_fifo.len] = virtio_rx_buf[i];
        virtio_rx_fifo_len += 1;
    }
    virtio_rx_last_used = virtio_rx_used.idx;
    // Re-supply the buffer: the descriptor is unchanged, but the avail ring
    // gained an entry — clean the rings, then kick queue 0 again.
    virtio_rx_avail.ring[0] = 0;
    asm volatile ("dmb ishst" ::: .{ .memory = true });
    virtio_rx_avail.idx +%= 1;
    mmu.clean_dcache_range(@intFromPtr(&virtio_rx_desc), @sizeOf(VirtqDesc));
    mmu.clean_dcache_range(@intFromPtr(&virtio_rx_avail), @sizeOf(VirtqAvail));
    mmio.mmio_write16(vp_notify + @as(u64, virtio_rx_queue_notify_off) * vp_notify_mult, 0);
    // Issue #843 Shape A underflow guard: if the completion had len=0 and the FIFO
    // was empty, rx_fifo_pop() must not be called (it guarantees non-empty FIFO).
    if (virtio_rx_fifo_len == 0) return null;
    return rx_fifo_pop();
}

/// True when the receive queue is configured and the initial buffer was
/// supplied (main.zig's `rx_wired` uses this to run the live shell loop
/// instead of parking).
pub fn rx_armed() bool {
    return virtio_rx_armed;
}

/// Queue a byte for a FUTURE `virtio_read_byte` — the serial front-end's
/// detach path returns the input its dying session pumped but never read.
/// Returns false (byte refused) when unarmed or the inject lane is full;
/// the caller then keeps its usual drop accounting. Never blocks.
var rx_pushback_refused_reported = false;

pub fn rx_pushback(b: u8) bool {
    if (!virtio_rx_armed or virtio_rx_inject_len >= virtio_rx_inject.len) {
        if (!rx_pushback_refused_reported) {
            rx_pushback_refused_reported = true;
            klog.line(if (virtio_rx_armed) "pushback: refused (inject full)\n" else "pushback: refused (unarmed)\n");
        }
        return false;
    }
    virtio_rx_inject[(virtio_rx_inject_head + virtio_rx_inject_len) % virtio_rx_inject.len] = b;
    virtio_rx_inject_len += 1;
    return true;
}

// ---------------------------------------------------------------------------
// M213d (issue #3879): per-writer line assembly — the serial line discipline
// above the byte sinks. #1972 made the DEVICE side whole-line (tx_unit
// above); the residual it left is cross-WRITER interleave: a whole kernel
// line from another core landed inside a process's multi-`puts` write (the
// heartbeat's 7 puts; process_stdout's own "\n"), and a kernel line split a
// prompt drawn in pieces. Every gate that matches markers reads that stream.
//
// Here a "writer" is (owner, core): the kernel writes as `.kernel` with ONE
// stage per core (a kernel line from another core is a different writer — the
// `open: id=4` line that split the prompt draw), a process writes as
// `.proc = pid` (a pid runs on one core at a time, so its line follows it
// across migration). Writes append to the writer's stage; a line emits to the
// wire ONLY when it completes — atomically, under the caller's serial lock —
// so a foreign write can land BETWEEN the writer's lines, never inside one.
// A staged partial (the prompt, typed echo) is published whole by the
// idle-seam `flush_partials` — never by a foreign write — and becomes the
// wire's open line under the same ownership rule as before (a different
// owner's next emission still closes it with a newline first).
//
// The staging bound is `line_stage_cap` per writer; a line that outgrows it
// is dropped whole through its newline and counted — the #1972 contract,
// "whole lines or counted drops". The boot-total is announced once per
// change by one `serial: line-dropped total=<n>` line on a fresh line before
// the next emitted content, mirroring `tx_drop_notice` below.
//
// The unit holds no lock and no device state: the caller (main.zig's uart
// path) serializes it under serial_lock and supplies `emit`, which pushes an
// emitted burst to the byte sink. Host tests drive it over a recording sink.
// ---------------------------------------------------------------------------

/// Writer identity for line assembly — the same partition the old
/// `serial_line_owner` rule used for wire ownership, now also the staging
/// key (kernel writers are additionally split per core).
pub const LineOwner = union(enum) {
    kernel,
    proc: usize,
};

pub const LineWriter = struct {
    owner: LineOwner,
    core: usize,
};

/// The named staging bound: one writer's line may stage at most this many
/// bytes before it is counted-dropped whole. Sized for the longest kernel
/// report lines (~110 B for the `prof:` row); 512 B leaves headroom without
/// turning the BSS table into a liability.
pub const line_stage_cap: usize = 512;
/// The greppable drop notice prefix — distinct from `tx_drop_notice`, which
/// counts device-side unit drops (a different mechanism).
pub const line_drop_notice = "serial: line-dropped total=";
const line_notice_cap = 64;

/// The emit callback: pushes one emitted burst to the wire sink. Called with
/// the caller's serial lock held, so each call's bytes are contiguous on the
/// wire — a whole staged line, a partial published at the seam, the closing
/// newline of a foreign open line, or a drop notice.
pub const SerialEmit = *const fn (ctx: ?*anyopaque, bytes: []const u8) void;

fn line_owner_is(a: ?LineOwner, b: LineOwner) bool {
    const o = a orelse return false;
    if (std.meta.activeTag(o) != std.meta.activeTag(b)) return false;
    return switch (o) {
        .kernel => true,
        .proc => |p| p == b.proc,
    };
}

pub fn SerialLines(comptime cores: usize, comptime proc_writers: usize, comptime cap: usize) type {
    return struct {
        const Self = @This();

        const Stage = struct {
            buf: [cap]u8 = undefined,
            len: usize = 0,
            /// The in-progress line overflowed the bound: discard bytes
            /// through its newline, then clear (#1972's "a line that loses
            /// a unit loses the rest of it" rule, moved up into staging).
            dropping: bool = false,
        };
        const ProcStage = struct {
            pid: usize = 0,
            stage: Stage = .{},
            fn free(s: *const ProcStage) bool {
                return s.stage.len == 0 and !s.stage.dropping;
            }
        };

        kernel: [cores]Stage = @splat(.{}),
        proc: [proc_writers]ProcStage = @splat(.{}),
        /// Pids whose in-progress line is being counted-dropped because no
        /// proc slot was free; their bytes discard through the line's '\n'.
        blocked: [proc_writers]usize = @splat(0),
        blocked_len: usize = 0,
        /// Wire state — what the emitted stream looks like, not what is
        /// staged. `open_owner` is the owner of an unterminated wire line.
        at_bol: bool = true,
        open_owner: ?LineOwner = null,
        /// Lines counted-dropped this boot, and the total the last notice
        /// announced (`line_drop_notice`).
        dropped: u32 = 0,
        reported: u32 = 0,

        fn stage_for(self: *Self, key: LineWriter) ?*Stage {
            switch (key.owner) {
                .kernel => return &self.kernel[key.core % self.kernel.len],
                .proc => |pid| {
                    var free: ?*ProcStage = null;
                    for (&self.proc) |*ps| {
                        if (ps.free()) {
                            if (free == null) free = ps;
                            continue;
                        }
                        if (ps.pid == pid) return &ps.stage;
                    }
                    if (free) |ps| {
                        ps.pid = pid;
                        return &ps.stage;
                    }
                    return null;
                },
            }
        }

        fn blocked_has(self: *const Self, pid: usize) bool {
            for (self.blocked[0..self.blocked_len]) |p| {
                if (p == pid) return true;
            }
            return false;
        }

        fn blocked_remove(self: *Self, pid: usize) void {
            for (self.blocked[0..self.blocked_len], 0..) |p, i| {
                if (p == pid) {
                    self.blocked_len -= 1;
                    self.blocked[i] = self.blocked[self.blocked_len];
                    return;
                }
            }
        }

        /// A proc line that could not claim a staging slot: drop it whole
        /// through its newline and count it once. When the blocked table
        /// itself is full the count grows per discarded write instead —
        /// still a visible, honest count.
        fn note_blocked(self: *Self, key: LineWriter) void {
            self.dropped +|= 1;
            if (self.blocked_len < self.blocked.len) {
                self.blocked[self.blocked_len] = key.owner.proc;
                self.blocked_len += 1;
            }
        }

        /// Push `bytes` to the wire honoring the open-line ownership rule
        /// (the claim-1714/#2649 contract, unchanged): emission while a
        /// DIFFERENT writer's line is open closes that line with a newline
        /// first, so foreign bytes can never fuse into it.
        fn emit_chunk(self: *Self, owner: LineOwner, bytes: []const u8, ctx: ?*anyopaque, emit: SerialEmit) void {
            if (bytes.len == 0) return;
            if (!self.at_bol and !line_owner_is(self.open_owner, owner)) emit(ctx, "\n");
            emit(ctx, bytes);
            self.at_bol = switch (bytes[bytes.len - 1]) {
                '\n', '\r' => true,
                else => false,
            };
            self.open_owner = if (self.at_bol) null else owner;
        }

        /// The drop notice is a line of its own for the gates that grep it:
        /// always opened on a fresh line even when the wire's open line is
        /// kernel-owned (where emit_chunk would fuse it).
        fn emit_notice(self: *Self, ctx: ?*anyopaque, emit: SerialEmit) void {
            if (self.dropped == self.reported) return;
            var buf: [line_notice_cap]u8 = undefined;
            const line = std.fmt.bufPrint(&buf, "{s}{d}\n", .{ line_drop_notice, self.dropped }) catch unreachable;
            if (!self.at_bol) emit(ctx, "\n");
            emit(ctx, line);
            self.at_bol = true;
            self.open_owner = null;
            self.reported = self.dropped;
        }

        /// Emit one completed stage (its last byte is the line terminator).
        fn emit_stage(self: *Self, st: *Stage, owner: LineOwner, ctx: ?*anyopaque, emit: SerialEmit) void {
            self.emit_notice(ctx, emit);
            self.emit_chunk(owner, st.buf[0..st.len], ctx, emit);
            st.len = 0;
        }

        /// Append `text` to the writer's stage; each completed line emits
        /// atomically before this call returns. A partial tail STAYS staged
        /// — it is published whole by `flush_partials`, never by a foreign
        /// write, which is what keeps another core's line from landing
        /// inside this writer's composition. Bytes of a line that outgrew
        /// `cap` discard through its '\n' and count once.
        pub fn write(self: *Self, key: LineWriter, text: []const u8, ctx: ?*anyopaque, emit: SerialEmit) void {
            var rest = text;
            while (rest.len > 0) {
                if (key.owner == .proc and self.blocked_has(key.owner.proc)) {
                    const nl = std.mem.indexOfScalar(u8, rest, '\n') orelse return;
                    self.blocked_remove(key.owner.proc);
                    rest = rest[nl + 1 ..];
                    continue;
                }
                const st = self.stage_for(key) orelse {
                    self.note_blocked(key);
                    continue;
                };
                var i: usize = 0;
                while (i < rest.len) : (i += 1) {
                    const b = rest[i];
                    if (st.dropping) {
                        if (b == '\n') {
                            st.dropping = false;
                            st.len = 0;
                        }
                        continue;
                    }
                    if (st.len == st.buf.len) {
                        self.dropped +|= 1;
                        st.dropping = true;
                        continue;
                    }
                    st.buf[st.len] = b;
                    st.len += 1;
                    if (b == '\n' or b == '\r') self.emit_stage(st, key.owner, ctx, emit);
                }
                return;
            }
        }

        /// Publish every staged partial as the wire's (open) line — the
        /// idle-seam flush: the prompt, typed echo, and any process's
        /// unterminated write reach the host whole instead of never. The
        /// caller may exclude ONE kernel stage by core: the idle task uses
        /// this to leave the shell's own core-0 composition untouched — a
        /// core-0 kernel partial flushes only from the shell task's own
        /// readByte/exit seams, which can never run while that task is
        /// mid-composition (it is the same call stack).
        pub fn flush_partials(self: *Self, skip_kernel_core: ?usize, ctx: ?*anyopaque, emit: SerialEmit) void {
            self.emit_notice(ctx, emit);
            for (&self.kernel, 0..) |*st, c| {
                if (skip_kernel_core != null and c == skip_kernel_core.?) continue;
                self.flush_stage(st, .kernel, ctx, emit);
            }
            for (&self.proc) |*ps| {
                if (ps.stage.len == 0) continue;
                self.emit_chunk(.{ .proc = ps.pid }, ps.stage.buf[0..ps.stage.len], ctx, emit);
                ps.stage.len = 0;
            }
        }

        fn flush_stage(self: *Self, st: *Stage, owner: LineOwner, ctx: ?*anyopaque, emit: SerialEmit) void {
            if (st.len == 0) return;
            self.emit_chunk(owner, st.buf[0..st.len], ctx, emit);
            st.len = 0;
        }
    };
}

// ---------------------------------------------------------------------------
// Claim 0017 diagnostic: PRE-EXIT virtio-pci TX experiment. Answers whether
// the current transport can transmit a known string while Boot Services and
// the firmware address space are still active. Uses the SAME discovered
// device, BAR, capability-decoded common/notify addresses, negotiated
// features, TX queue, desc/avail/used rings and 16-bit notify mechanism as
// the post-exit path — it stages a fixed line into `virtio_tx` and calls
// `virtio_pci_flush()` verbatim. Build-gated by `-Dpreexit-tx` (default off:
// every existing gate is byte-identical). Bracketed by the NVRAM marker
// ladder (claim 0009 channel): M2_PEXT! before the flush, the flush's own
// M2_TXST!/M2_TXNT!/M2_TXPL! stage markers (persisted because st_tx is
// set), M2_PEXD! after. If the experiment hangs, the ladder's last marker
// names the death site. NOTE: the flush also appends "VP pst=" to
// probe_dump — the probe variable is already persisted at this point, so
// that line stays in the in-RAM buffer only (harmless; the placement after
// persistence is deliberate so a hang cannot lose the probe evidence).
// ---------------------------------------------------------------------------
const preexit_tx_line = "VIRELAIOS PREEXIT VIRTIO TX\n";
pub fn preexit_tx_experiment(st: *const SystemTable) void {
    if (!vp_ready or vp_tx_len != 0) return;
    st_tx = st; // the flush's TXST/TXNT/TXPL stage markers now persist pre-exit
    evidence.write_marker_var(st, marker_pext);
    var i: usize = 0;
    while (i < preexit_tx_line.len and i < virtio_tx.len) : (i += 1) {
        virtio_tx[i] = preexit_tx_line[i];
    }
    vp_tx_len = i;
    virtio_pci_flush();
    evidence.write_marker_var(st, marker_pexd);
    st_tx = null;
}

// ---------------------------------------------------------------------------
// Claim 0020 diagnostic: TX-transition matrix. One phase per build (selected
// by -Dtx-transition-{a,b,c,d}), each running ONE controlled TX attempt at
// its named location with the SAME fixed payload, the SAME armed transport
// (armed pre-exit by virtio_pci_init, never transmitted before the phase's
// experiment) and the SAME virtio_pci_flush() as the production path. The
// attempt is bracketed by persistent NVRAM markers (claim-0009 channel):
// M2_TRx1! before the flush, M2_TRx2! when the flush returns, M2_TRxU! if
// used.idx advanced (the device consumed the buffer). The flush's own
// M2_TXST!/M2_TXNT!/M2_TXPL! stage markers (persisted because st_tx is set)
// name the hang site inside the flush. M2_TRNX! records a skipped attempt
// (transport not armed). The FIRST failed phase names the transition that
// destroys console access (see the claim file's interpretation table).
// ---------------------------------------------------------------------------
const transition_tx_line = "VIRELAIOS TRANSITION TX\n";
const TxPhase = enum { a, b, c, d };

pub fn transition_tx_experiment(st: *const SystemTable, phase: TxPhase) void {
    const enter: u64 = switch (phase) {
        .a => marker_tra1,
        .b => marker_trb1,
        .c => marker_trc1,
        .d => marker_trd1,
    };
    const returned: u64 = switch (phase) {
        .a => marker_tra2,
        .b => marker_trb2,
        .c => marker_trc2,
        .d => marker_trd2,
    };
    const used_adv: u64 = switch (phase) {
        .a => marker_trau,
        .b => marker_trbu,
        .c => marker_trcu,
        .d => marker_trdu,
    };
    if (!vp_ready or vp_tx_len != 0) {
        evidence.write_marker_var(st, marker_trnx);
        return;
    }
    evidence.write_marker_var(st, enter);
    st_tx = st; // flush stage markers (TXST!/TXNT!/TXPL!) persist
    var i: usize = 0;
    while (i < transition_tx_line.len and i < virtio_tx.len) : (i += 1) {
        virtio_tx[i] = transition_tx_line[i];
    }
    vp_tx_len = i;
    const used_before = virtio_last_used;
    virtio_pci_flush(); // may hang — the ladder's last marker names the site
    evidence.write_marker_var(st, returned);
    mmu.invalidate_dcache_range(@intFromPtr(&virtio_used), @sizeOf(VirtqUsed));
    if (virtio_used.idx != used_before) evidence.write_marker_var(st, used_adv);
    st_tx = null;
}

/// Legacy virtio-MMIO transport init. Present but never selected by the
/// current probe (the console is a virtio-PCI device); kept verbatim.
fn virtio_init(base: u64) bool {
    // This implementation uses only the modern virtio-mmio register path.
    // A version-1 (legacy) device is not selected because its PFN queue
    // layout would require a separate setup path.
    if (mmio.mmio_read32(base + 0x04) != 2) return false;
    mmio.mmio_write32(base + 0x14, 0);
    const device_features_low = mmio.mmio_read32(base + 0x10);
    mmio.mmio_write32(base + 0x14, 1);
    const device_features_high = mmio.mmio_read32(base + 0x10);
    if ((device_features_high & 1) == 0) return false; // VIRTIO_F_VERSION_1 (bit 32)

    mmio.mmio_write32(base + 0x70, 0);
    mmio.mmio_write32(base + 0x70, 1 | 2); // ACKNOWLEDGE | DRIVER
    mmio.mmio_write32(base + 0x24, 0);
    mmio.mmio_write32(base + 0x20, 0); // no optional low-word features
    mmio.mmio_write32(base + 0x24, 1);
    mmio.mmio_write32(base + 0x20, 1); // negotiate VIRTIO_F_VERSION_1
    _ = device_features_low;
    mmio.mmio_write32(base + 0x70, 1 | 2 | 8); // FEATURES_OK
    if ((mmio.mmio_read32(base + 0x70) & 8) == 0) return false;

    virtio_desc[0] = .{ .addr = 0, .len = 0, .flags = 0, .next = 0 };
    virtio_avail = .{ .flags = 0, .idx = 0, .ring = .{0} };
    virtio_used = .{ .flags = 0, .idx = 0, .ring = .{.{ .id = 0, .len = 0 }} };
    virtio_last_used = 0;
    mmio.mmio_write32(base + 0x30, 1); // queue 1: console transmit queue
    const max = mmio.mmio_read32(base + 0x34);
    if (max == 0) return false;
    mmio.mmio_write32(base + 0x38, 1);
    mmio.mmio_write32(base + 0x80, @truncate(mmu.to_phys(@intFromPtr(&virtio_desc))));
    mmio.mmio_write32(base + 0x84, @truncate(mmu.to_phys(@intFromPtr(&virtio_desc)) >> 32));
    mmio.mmio_write32(base + 0x90, @truncate(mmu.to_phys(@intFromPtr(&virtio_avail))));
    mmio.mmio_write32(base + 0x94, @truncate(mmu.to_phys(@intFromPtr(&virtio_avail)) >> 32));
    mmio.mmio_write32(base + 0xa0, @truncate(mmu.to_phys(@intFromPtr(&virtio_used))));
    mmio.mmio_write32(base + 0xa4, @truncate(mmu.to_phys(@intFromPtr(&virtio_used)) >> 32));
    mmio.mmio_write32(base + 0x44, 1);
    mmio.mmio_write32(base + 0x70, 1 | 2 | 8 | 4); // DRIVER_OK
    return (mmio.mmio_read32(base + 0x70) & 4) != 0;
}

// ---------------------------------------------------------------------------
// Host tests (#1972): `tx_unit` over a simulated device that completes late
// or never. Each used-ring look is one tick of the simulated clock; the
// device reads the DMA buffer when it completes, not when it is posted.
// ---------------------------------------------------------------------------

const SimTx = struct {
    /// Ticks from each post to its completion, in post order (the last
    /// entry repeats); null never completes.
    latency: []const ?u64,
    bound: u64 = 1000,
    dma_buf: [128]u8 = undefined,
    clock: u64 = 0,
    avail: u16 = 0,
    used: u16 = 0,
    posts: usize = 0,
    due: ?u64 = null,
    posted: [128]u8 = undefined,
    posted_len: usize = 0,
    host: [16384]u8 = undefined,
    host_len: usize = 0,
    /// Completions whose bytes differ from what was posted: a splice.
    rewrites: usize = 0,
    /// Posts made while a descriptor was outstanding (queue size 1).
    overruns: usize = 0,
    timeouts: usize = 0,

    fn dma(self: *SimTx) *[128]u8 {
        return &self.dma_buf;
    }

    fn now(self: *SimTx) u64 {
        return self.clock;
    }

    fn wait_ticks(self: *SimTx) u64 {
        return self.bound;
    }

    fn reaped(_: *SimTx) void {}

    fn outstanding(self: *SimTx) u16 {
        self.idle(1);
        return self.avail -% self.used;
    }

    fn post(self: *SimTx, len: usize) void {
        if (self.avail != self.used) self.overruns += 1;
        @memcpy(self.posted[0..len], self.dma_buf[0..len]);
        self.posted_len = len;
        self.avail +%= 1;
        const lat = self.latency[@min(self.posts, self.latency.len - 1)];
        self.posts += 1;
        self.due = if (lat) |l| self.clock + l else null;
    }

    fn finish(self: *SimTx, completed: bool) void {
        if (!completed) self.timeouts += 1;
    }

    /// Time passes (one look, or the CPU working between flushes).
    fn idle(self: *SimTx, ticks: u64) void {
        self.clock += ticks;
        const due = self.due orelse return;
        if (self.clock < due) return;
        const got = self.dma_buf[0..self.posted_len];
        if (!std.mem.eql(u8, got, self.posted[0..self.posted_len])) self.rewrites += 1;
        @memcpy(self.host[self.host_len..][0..got.len], got);
        self.host_len += got.len;
        self.used +%= 1;
        self.due = null;
    }

    fn received(self: *SimTx) []const u8 {
        return self.host[0..self.host_len];
    }
};

fn sim_flush(sim: *SimTx, s: *TxState, text: []const u8) void {
    // Stage through a separate buffer, as uart_putc stages into virtio_tx.
    var staging: [128]u8 = undefined;
    @memcpy(staging[0..text.len], text);
    tx_unit(sim, s, staging[0..text.len]);
    @memset(&staging, '#');
}

test "virtio_console: the simulated device sees a buffer rewritten under it (the #1972 splice)" {
    var sim: SimTx = .{ .latency = &.{2000} };
    var s: TxState = .{};
    sim_flush(&sim, &s, "gotabwm: launcher presented\n");
    try std.testing.expectEqual(@as(usize, 1), sim.timeouts);
    // Pre-#1972: the next line was staged into the buffer still posted.
    @memcpy(sim.dma_buf[0..14], "gotabwm: tick\n");
    sim.idle(2000);
    try std.testing.expectEqualStrings("gotabwm: tick\nher presented\n", sim.received());
    try std.testing.expectEqual(@as(usize, 1), sim.rewrites);
}

test "virtio_console: a prompt device gets every unit whole and no notice" {
    var sim: SimTx = .{ .latency = &.{3} };
    var s: TxState = .{};
    sim_flush(&sim, &s, "m91: frame=13 phase=complete result=ok\n");
    sim_flush(&sim, &s, "gotabwm: tick\n");
    sim_flush(&sim, &s, "virelai> ");
    sim_flush(&sim, &s, "ls\n");
    try std.testing.expectEqualStrings("m91: frame=13 phase=complete result=ok\ngotabwm: tick\nvirelai> ls\n", sim.received());
    try std.testing.expectEqual(@as(u32, 0), s.dropped);
    try std.testing.expectEqual(@as(usize, 4), sim.posts);
    try std.testing.expectEqual(@as(usize, 0), sim.timeouts);
    try std.testing.expectEqual(@as(usize, 0), sim.rewrites);
}

test "virtio_console: a late completion keeps its bytes and the next line drops whole" {
    var sim: SimTx = .{ .latency = &.{ 1050, 1 } };
    var s: TxState = .{};
    sim_flush(&sim, &s, "gotabwm: launcher presented\n");
    try std.testing.expectEqual(@as(usize, 1), sim.timeouts);
    sim_flush(&sim, &s, "gotabwm: tick\n"); // the device still owns the buffer
    try std.testing.expectEqual(@as(u32, 1), s.dropped);
    try std.testing.expectEqual(@as(usize, 1), sim.posts);
    sim.idle(100);
    sim_flush(&sim, &s, "m91: frame=13 phase=complete result=ok\n");
    try std.testing.expectEqualStrings("gotabwm: launcher presented\n" ++
        tx_drop_notice ++ "1\n" ++
        "m91: frame=13 phase=complete result=ok\n", sim.received());
    try std.testing.expectEqual(@as(usize, 0), sim.rewrites);
    try std.testing.expectEqual(@as(usize, 0), sim.overruns);
    try std.testing.expectEqual(s.dropped, s.reported);
}

test "virtio_console: a completion inside the reap looks loses nothing" {
    var sim: SimTx = .{ .latency = &.{ 1003, 1 } };
    var s: TxState = .{};
    sim_flush(&sim, &s, "gotabwm: notify dnd=on via=seat persisted=1\n");
    try std.testing.expectEqual(@as(usize, 1), sim.timeouts);
    sim_flush(&sim, &s, "gotabwm: tick\n");
    try std.testing.expectEqualStrings("gotabwm: notify dnd=on via=seat persisted=1\ngotabwm: tick\n", sim.received());
    try std.testing.expectEqual(@as(u32, 0), s.dropped);
    try std.testing.expectEqual(@as(usize, 0), sim.rewrites);
}

test "virtio_console: a device that never completes costs one bounded wait" {
    var sim: SimTx = .{ .latency = &.{null} };
    var s: TxState = .{};
    sim_flush(&sim, &s, "exec NOTE.ELF\n");
    try std.testing.expectEqual(@as(u64, 1 + sim.bound), sim.clock);
    sim_flush(&sim, &s, "a\n");
    sim_flush(&sim, &s, "b\n");
    sim_flush(&sim, &s, "c\n");
    try std.testing.expectEqual(@as(u64, 1 + sim.bound + 3 * tx_reap_polls), sim.clock);
    try std.testing.expectEqual(@as(u32, 3), s.dropped);
    try std.testing.expectEqual(@as(u32, 0), s.reported); // never recovered: no notice
    try std.testing.expectEqual(@as(usize, 1), sim.posts);
    try std.testing.expectEqualStrings("", sim.received());
    try std.testing.expectEqualStrings("exec NOTE.ELF\n", sim.dma_buf[0..14]);
}

test "virtio_console: the completion wait falls back to a look bound without a counter frequency" {
    var sim: SimTx = .{ .latency = &.{null}, .bound = 0 };
    var s: TxState = .{};
    sim_flush(&sim, &s, "x\n");
    try std.testing.expectEqual(@as(u64, 1 + tx_complete_wait_polls), sim.clock);
    try std.testing.expectEqual(@as(usize, 1), sim.timeouts);
}

test "virtio_console: a line that loses a unit loses the rest of it" {
    var sim: SimTx = .{ .latency = &.{ 1050, 1 } };
    var s: TxState = .{};
    sim_flush(&sim, &s, "first\n");
    sim_flush(&sim, &s, "abc"); // a full-buffer or prompt flush: no newline
    sim_flush(&sim, &s, "def\n"); // the rest of the same line
    try std.testing.expectEqual(@as(u32, 1), s.dropped);
    sim.idle(100);
    sim_flush(&sim, &s, "next\n");
    try std.testing.expectEqualStrings("first\n" ++ tx_drop_notice ++ "1\nnext\n", sim.received());
}

test "virtio_console: a notice after a mid-line stall starts on a fresh line" {
    var sim: SimTx = .{ .latency = &.{ 1050, 1 } };
    var s: TxState = .{};
    sim_flush(&sim, &s, "virelai> ");
    sim_flush(&sim, &s, "ls\n");
    sim.idle(100);
    sim_flush(&sim, &s, "x\n");
    try std.testing.expectEqualStrings("virelai> \n" ++ tx_drop_notice ++ "1\nx\n", sim.received());
}

test "virtio_console: drops count per boot across stalls, including a notice that times out" {
    // Posts: A, notice 1, C, D, notice 2, (E is dropped), notice 3, G.
    var sim: SimTx = .{ .latency = &.{ 1050, 1, 1, 1050, 1050, 1, 1 } };
    var s: TxState = .{};
    sim_flush(&sim, &s, "A\n");
    sim_flush(&sim, &s, "B\n"); // dropped: 1
    sim.idle(100);
    sim_flush(&sim, &s, "C\n"); // notice lines=1, then C
    sim_flush(&sim, &s, "D\n"); // times out
    sim_flush(&sim, &s, "E\n"); // dropped: 2
    sim.idle(100);
    sim_flush(&sim, &s, "F\n"); // notice lines=2 times out, so F drops: 3
    sim.idle(1100);
    sim_flush(&sim, &s, "G\n"); // notice lines=3, then G
    try std.testing.expectEqualStrings("A\n" ++
        tx_drop_notice ++ "1\nC\nD\n" ++
        tx_drop_notice ++ "2\n" ++
        tx_drop_notice ++ "3\nG\n", sim.received());
    try std.testing.expectEqual(@as(u32, 3), s.dropped);
    try std.testing.expectEqual(@as(usize, 0), sim.rewrites);
    try std.testing.expectEqual(@as(usize, 0), sim.overruns);
}

test "virtio_console: random device latency never splices and accounts for every line" {
    var prng = std.Random.DefaultPrng.init(0x1972);
    const r = prng.random();
    var lat: [4096]?u64 = undefined;
    for (&lat) |*l| {
        l.* = switch (r.uintLessThan(u8, 16)) {
            0 => 1000 + r.uintLessThan(u64, 3000), // later than the wait
            1 => 990 + r.uintLessThan(u64, 20), // straddles the wait + reap looks
            else => 1 + r.uintLessThan(u64, 50),
        };
    }
    var sim: SimTx = .{ .latency = &lat };
    var s: TxState = .{};
    const lines = 1500;
    var i: usize = 0;
    while (i < lines) : (i += 1) {
        var buf: [32]u8 = undefined;
        sim_flush(&sim, &s, std.fmt.bufPrint(&buf, "line-{d}\n", .{i}) catch unreachable);
        sim.idle(r.uintLessThan(u64, 200));
    }
    sim.idle(10_000);
    try std.testing.expectEqual(@as(usize, 0), sim.rewrites);
    try std.testing.expectEqual(@as(usize, 0), sim.overruns);
    try std.testing.expect(s.dropped > 0); // the seed exercises drops
    // Every received line is whole: an original line in order, or a notice
    // whose growing total covers at least the lines already missing.
    var next: usize = 0;
    var missing: u32 = 0;
    var notices: usize = 0;
    var last_total: u32 = 0;
    var it = std.mem.splitScalar(u8, sim.received(), '\n');
    while (it.next()) |line| {
        if (line.len == 0) continue;
        if (std.mem.startsWith(u8, line, tx_drop_notice)) {
            const total = try std.fmt.parseInt(u32, line[tx_drop_notice.len..], 10);
            try std.testing.expect(total > last_total and total >= missing);
            last_total = total;
            notices += 1;
            continue;
        }
        try std.testing.expect(std.mem.startsWith(u8, line, "line-"));
        const n = try std.fmt.parseInt(usize, line["line-".len..], 10);
        try std.testing.expect(n >= next);
        missing += @intCast(n - next);
        next = n + 1;
    }
    missing += @intCast(lines - next);
    try std.testing.expectEqual(s.dropped, missing);
    try std.testing.expect(notices > 0);
    try std.testing.expectEqual(s.reported, last_total);
    try std.testing.expect(std.mem.endsWith(u8, sim.received(), "\n"));
}

// ---------------------------------------------------------------------------
// Host tests (#3879): SerialLines over a recording sink. The corpus shapes
// come straight out of #1972's closing comment ("the branch's 7
// interleaves"): the prompt split by `open: id=4`, the 7-`puts` heartbeat
// cut by process_stdout's own "\n", and `uaccess:` inside a multi-put
// kernel line.
// ---------------------------------------------------------------------------

const LineSink = struct {
    host: [1 << 20]u8 = undefined,
    host_len: usize = 0,
    burst_start: [1 << 16]usize = undefined,
    bursts: usize = 0,

    fn emit(ctx: ?*anyopaque, bytes: []const u8) void {
        const self: *LineSink = @ptrCast(@alignCast(ctx.?));
        self.burst_start[self.bursts] = self.host_len;
        self.bursts += 1;
        @memcpy(self.host[self.host_len..][0..bytes.len], bytes);
        self.host_len += bytes.len;
    }

    fn received(self: *const LineSink) []const u8 {
        return self.host[0..self.host_len];
    }

    fn burst(self: *const LineSink, i: usize) []const u8 {
        const end = if (i + 1 < self.bursts) self.burst_start[i + 1] else self.host_len;
        return self.host[self.burst_start[i]..end];
    }
};

const TestLines = SerialLines(4, 4, line_stage_cap);
const k0: LineWriter = .{ .owner = .kernel, .core = 0 };
const k1: LineWriter = .{ .owner = .kernel, .core = 1 };
const p7: LineWriter = .{ .owner = .{ .proc = 7 }, .core = 1 };
const p8: LineWriter = .{ .owner = .{ .proc = 8 }, .core = 2 };

test "serial_lines: the heartbeat's pieces emit whole between foreign lines" {
    // The corpus shape: maybe_heartbeat's 7 puts with whole process lines
    // arriving between them. The heartbeat emits as ONE burst at its
    // newline; the foreign lines land between lines, never inside.
    var lines: TestLines = .{};
    var sink: LineSink = .{};
    lines.write(k0, "timer heartbeat ticks=", &sink, LineSink.emit);
    lines.write(p7, "gosh: tty\n", &sink, LineSink.emit);
    lines.write(k0, "5 irq=", &sink, LineSink.emit);
    lines.write(p7, "gosh: attached\n", &sink, LineSink.emit);
    lines.write(k0, "5 poll=0\n", &sink, LineSink.emit);
    try std.testing.expectEqualStrings(
        "gosh: tty\ngosh: attached\ntimer heartbeat ticks=5 irq=5 poll=0\n",
        sink.received(),
    );
    try std.testing.expectEqualStrings(
        "timer heartbeat ticks=5 irq=5 poll=0\n",
        sink.burst(sink.bursts - 1),
    );
}

test "serial_lines: the prompt draw publishes whole after a foreign kernel line" {
    // The corpus shape: `open: id=4 …` landed between the prompt's pieces.
    // The pieces stage; the foreign line emits at beginning-of-line; the
    // draw publishes as ONE open wire line at the idle seam.
    var lines: TestLines = .{};
    var sink: LineSink = .{};
    lines.write(k0, "\x1b[32m", &sink, LineSink.emit);
    lines.write(k1, "open: id=4 owner=3 rect=0,0 64x23 ws=0\n", &sink, LineSink.emit);
    lines.write(k0, "virelai> ", &sink, LineSink.emit);
    lines.write(k0, "\x1b[0m", &sink, LineSink.emit);
    lines.flush_partials(null, &sink, LineSink.emit);
    try std.testing.expectEqualStrings(
        "open: id=4 owner=3 rect=0,0 64x23 ws=0\n\x1b[32mvirelai> \x1b[0m",
        sink.received(),
    );
    try std.testing.expectEqualStrings("\x1b[32mvirelai> \x1b[0m", sink.burst(sink.bursts - 1));
    try std.testing.expect(!lines.at_bol);
    try std.testing.expect(std.meta.activeTag(lines.open_owner.?) == .kernel);
}

test "serial_lines: a multi-put kernel line survives a process write inside it" {
    // The corpus shape: `uaccess: efault ok n=8` inside the multi-put
    // `userspace:` line.
    var lines: TestLines = .{};
    var sink: LineSink = .{};
    lines.write(k0, "userspace: el0=1 svc=", &sink, LineSink.emit);
    lines.write(k0, "2 roundtrips=", &sink, LineSink.emit);
    lines.write(p7, "uaccess: efault ok n=8\n", &sink, LineSink.emit);
    lines.write(k0, "1 arg=2\n", &sink, LineSink.emit);
    try std.testing.expectEqualStrings(
        "uaccess: efault ok n=8\nuserspace: el0=1 svc=2 roundtrips=1 arg=2\n",
        sink.received(),
    );
}

test "serial_lines: process output opens a fresh line over a foreign open line" {
    // The process_stdout ordering contract: a process write while the wire
    // sits mid-line on a DIFFERENT owner closes that line first.
    var lines: TestLines = .{};
    var sink: LineSink = .{};
    lines.write(k0, "virelai> ", &sink, LineSink.emit);
    lines.flush_partials(null, &sink, LineSink.emit);
    lines.write(p7, "gosh: tty\n", &sink, LineSink.emit);
    try std.testing.expectEqualStrings("virelai> \ngosh: tty\n", sink.received());
    try std.testing.expect(lines.at_bol);
}

test "serial_lines: a writer continues its own published open line" {
    var lines: TestLines = .{};
    var sink: LineSink = .{};
    lines.write(k0, "virelai> ", &sink, LineSink.emit);
    lines.flush_partials(null, &sink, LineSink.emit);
    lines.write(k0, "ls\n", &sink, LineSink.emit);
    try std.testing.expectEqualStrings("virelai> ls\n", sink.received());
}

test "serial_lines: same-owner kernel lines still fuse onto an open line" {
    // The pre-existing owner rule, unchanged: kernel is ONE wire owner, so
    // another core's line continues a kernel-open wire line without a
    // closing newline.
    var lines: TestLines = .{};
    var sink: LineSink = .{};
    lines.write(k0, "virelai> ", &sink, LineSink.emit);
    lines.flush_partials(null, &sink, LineSink.emit);
    lines.write(k1, "open: id=4\n", &sink, LineSink.emit);
    try std.testing.expectEqualStrings("virelai> open: id=4\n", sink.received());
}

test "serial_lines: a line over the staging bound drops whole and counts once" {
    var lines: SerialLines(4, 4, 32) = .{};
    var sink: LineSink = .{};
    lines.write(k0, "this line is far too long to", &sink, LineSink.emit);
    lines.write(k0, " ever fit the stage\n", &sink, LineSink.emit);
    try std.testing.expectEqual(@as(u32, 1), lines.dropped);
    try std.testing.expectEqualStrings("", sink.received());
    try std.testing.expect(!lines.kernel[0].dropping);
    lines.write(k1, "next ok\n", &sink, LineSink.emit);
    try std.testing.expectEqualStrings(
        line_drop_notice ++ "1\nnext ok\n",
        sink.received(),
    );
    try std.testing.expectEqual(@as(u32, 1), lines.reported);
}

test "serial_lines: a dropped line's continuation is discarded through its newline" {
    var lines: SerialLines(4, 4, 16) = .{};
    var sink: LineSink = .{};
    lines.write(k0, "0123456789abcdef", &sink, LineSink.emit); // fills the stage exactly
    lines.write(k0, "GARBAGE", &sink, LineSink.emit); // over the bound: drop
    lines.write(k0, "more\n", &sink, LineSink.emit); // same line: all discarded
    lines.write(k0, "ok\n", &sink, LineSink.emit); // the next line survives
    try std.testing.expectEqual(@as(u32, 1), lines.dropped);
    try std.testing.expectEqualStrings(
        line_drop_notice ++ "1\nok\n",
        sink.received(),
    );
}

test "serial_lines: proc slot exhaustion drops the line whole, once" {
    var lines: SerialLines(4, 2, 64) = .{};
    var sink: LineSink = .{};
    const p9: LineWriter = .{ .owner = .{ .proc = 9 }, .core = 0 };
    lines.write(p7, "p7 open ", &sink, LineSink.emit); // claims proc slot 0
    lines.write(p8, "p8 open ", &sink, LineSink.emit); // claims proc slot 1
    lines.write(p9, "lost ", &sink, LineSink.emit); // no free slot: drop
    lines.write(p9, "still lost\n", &sink, LineSink.emit); // same line: silent
    try std.testing.expectEqual(@as(u32, 1), lines.dropped);
    try std.testing.expectEqualStrings("", sink.received());
    lines.flush_partials(null, &sink, LineSink.emit); // publishes + frees slots
    lines.write(p9, "now fits\n", &sink, LineSink.emit);
    try std.testing.expectEqualStrings(
        line_drop_notice ++ "1\np7 open \np8 open \nnow fits\n",
        sink.received(),
    );
    try std.testing.expectEqual(@as(u32, 1), lines.reported);
}

test "serial_lines: the idle seam can leave the shell's core-0 stage untouched" {
    // A core-0 kernel writer preempted mid-composition must NOT have its
    // partial published by the idle task — the flush excludes that one
    // stage and the line still emits whole when the writer resumes.
    var lines: TestLines = .{};
    var sink: LineSink = .{};
    lines.write(k0, "timer heartbeat ticks=5 irq=", &sink, LineSink.emit);
    lines.write(p7, "p7 partial", &sink, LineSink.emit);
    lines.flush_partials(0, &sink, LineSink.emit); // the idle task's pass
    try std.testing.expectEqualStrings("p7 partial", sink.received());
    lines.write(k0, "5 poll=0\n", &sink, LineSink.emit);
    try std.testing.expectEqualStrings(
        "p7 partial\ntimer heartbeat ticks=5 irq=5 poll=0\n",
        sink.received(),
    );
}

test "serial_lines: the drop notice always opens on a fresh line" {
    var lines: SerialLines(4, 4, 16) = .{};
    var sink: LineSink = .{};
    lines.write(k0, "virelai> ", &sink, LineSink.emit);
    lines.flush_partials(null, &sink, LineSink.emit); // kernel open line
    lines.write(k0, "0123456789abcdefOVER\n", &sink, LineSink.emit); // drop over bound
    lines.write(k1, "next\n", &sink, LineSink.emit); // notice precedes it
    try std.testing.expectEqualStrings(
        "virelai> \n" ++ line_drop_notice ++ "1\nnext\n",
        sink.received(),
    );
}

test "serial_lines: random writers never mix owners inside one wire line" {
    var prng = std.Random.DefaultPrng.init(0x3879);
    const r = prng.random();
    var lines: SerialLines(4, 4, 96) = .{};
    var sink: LineSink = .{};
    const writers = [_]LineWriter{ k0, k1, p7, p8 };
    // Disjoint byte alphabets per writer key; the kernel writers share ONE
    // owner, so the legal sets are {k0,k1}, {p7}, {p8}.
    const alpha = [4][]const u8{ "AB", "CD", "g", "k" };
    var issued: [4][16384]u8 = undefined;
    var issued_len: [4]usize = .{ 0, 0, 0, 0 };
    var composing: [4]bool = .{ false, false, false, false };
    var steps: usize = 0;
    while (steps < 4000) : (steps += 1) {
        switch (r.uintLessThan(u8, 10)) {
            0 => lines.flush_partials(null, &sink, LineSink.emit),
            1 => lines.flush_partials(0, &sink, LineSink.emit),
            else => {
                const w = r.uintLessThan(u8, 4);
                var piece: [24]u8 = undefined;
                var n: usize = 0;
                const count = 1 + r.uintLessThan(u8, 8);
                var i: usize = 0;
                while (i < count) : (i += 1) {
                    const a = alpha[w];
                    piece[n] = a[r.uintLessThan(u8, @intCast(a.len))];
                    n += 1;
                }
                composing[w] = true;
                if (r.uintLessThan(u8, 4) == 0) {
                    piece[n] = '\n';
                    n += 1;
                    composing[w] = false;
                }
                @memcpy(issued[w][issued_len[w]..][0..n], piece[0..n]);
                issued_len[w] += n;
                lines.write(writers[w], piece[0..n], &sink, LineSink.emit);
            },
        }
    }
    lines.flush_partials(null, &sink, LineSink.emit);
    // Invariant one: no physical wire line mixes two owners' alphabets.
    var it = std.mem.splitScalar(u8, sink.received(), '\n');
    while (it.next()) |line| {
        if (line.len == 0) continue;
        if (std.mem.startsWith(u8, line, line_drop_notice)) continue;
        var has_kernel = false;
        var has_p7 = false;
        var has_p8 = false;
        for (line) |b| {
            if (b == '\n' or b == '\r') continue;
            const k = (b >= 'A' and b <= 'D') or (b >= 'a' and b <= 'f');
            const q7 = b >= 'g' and b <= 'j';
            const q8 = b >= 'k' and b <= 'o';
            if (!k and !q7 and !q8) continue; // close-newline artifacts: none expected
            has_kernel = has_kernel or k;
            has_p7 = has_p7 or q7;
            has_p8 = has_p8 or q8;
        }
        try std.testing.expect(@as(u8, @intFromBool(has_kernel)) +
            @as(u8, @intFromBool(has_p7)) +
            @as(u8, @intFromBool(has_p8)) <= 1);
    }
    // Invariant two: per writer key, every issued byte arrived in order
    // (drops are impossible under this test's bound — asserted here so the
    // equivalence stays honest rather than assumed).
    try std.testing.expectEqual(@as(u32, 0), lines.dropped);
    for (0..4) |w| {
        var want: [16384]u8 = undefined;
        var wlen: usize = 0;
        for (issued[w][0..issued_len[w]]) |b| {
            if (b != '\n') {
                want[wlen] = b;
                wlen += 1;
            }
        }
        var got: [16384]u8 = undefined;
        var glen: usize = 0;
        for (sink.received()) |b| {
            const owned = switch (w) {
                0 => b >= 'A' and b <= 'B',
                1 => b >= 'C' and b <= 'D',
                2 => b == 'g',
                else => b == 'k',
            };
            if (owned) {
                got[glen] = b;
                glen += 1;
            }
        }
        try std.testing.expectEqualStrings(want[0..wlen], got[0..glen]);
    }
}
