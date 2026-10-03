# ADR 0002: Kernel handoff format and ABI (milestone one)

Status: **accepted** · Date: 2026-08-05 · Milestone: one

## Context

Milestone one requires loading a *separate* AArch64 kernel image from the
ESP and transferring control to its entry point. The loader (`boot/`) and
the kernel (`kernel/`) are compiled separately: the loader is an AArch64
UEFI application, the kernel is an `aarch64-freestanding` blob with no
libc/POSIX/OS. The two must agree on an image format and a register-level
handoff contract, and the milestone must be provable from the host using
the project's observed-vs-inferred evidence rule.

## Decisions

### D1. The kernel image is a flat binary (format v1, magic "DSK1"), not a raw ELF

The on-ESP file `\KERNEL.BIN` is a 24-byte header followed by the raw
loadable content:

| offset | size | field |
|--------|------|-------|
| 0      | u32  | magic `0x314B5344` ("DSK1") |
| 4      | u32  | flags (0) |
| 8      | u64  | `entry_offset` — bytes from the **start of the file** to the entry point (i.e. includes the 24-byte header) |
| 16     | u64  | `image_size` — total file size including the header |
| 24     | …    | loadable content |

`tools/elf2bin.py` converts the linked freestanding ELF into this format:
it places the PT_LOAD segments at their `vaddr` relative to the lowest
segment vaddr (preserving the linker's exact relative layout so PC-relative
`adr/adrp` addressing stays valid at any 4K-aligned load base) and
zero-fills `memsz > filesz` tails (BSS).

Rationale: the kernel is position-independent by construction (see D4), so
a flat image avoids the loader needing an ELF parser, a relocation pass, or
a dynamic section — a smaller, more auditable loader. The header keeps the
loader honest (magic, size cap, explicit entry) and lets host tools
(`elf2bin.py --info`, `mkfat32.py --cat-file`) verify the image on disk.

The loader parses the 24-byte header but does **not** load it into RAM: the
content is placed at `base+0`, so ELF VMA `V` sits at RAM `base+V`. This is
load-bearing — with the content at `base+24` (file loaded verbatim),
ADRP+ADD references to `.rodata` dropped the +24 and read every literal 24
bytes early (see the resolved known issue below).

### D2. No `ExitBootServices` in milestone one

The kernel keeps running with the full UEFI Boot Services environment
(and on the loader's stack). It uses the EFI System Table and the Simple
File System protocol for its evidence write, exactly like the loader.
Consequences for the hardware contract: the kernel may not touch MMU,
interrupts, timers, or any device directly until a later milestone records
an ExitBootServices design in the contract. This is the same posture the
loader already has, so milestone one adds no new firmware dependencies.

### D3. The loader allocates the kernel image as `EfiLoaderCode` via `AllocatePages`

The loader reads the image header, sanity-checks magic/size, allocates
`ceil(image_size / 4096)` pages of type `EfiLoaderCode` (not
`EfiLoaderData`), and copies the image **content** (`image_size - 24` bytes)
into them at offset 0 — the header is parsed but not loaded into RAM, so
the content starts exactly at `base+0` (see D1). `EfiLoaderCode` is used
because firmware may map data-type memory as execute-never, and the loader
is about to execute this memory — the same choice real UEFI bootloaders
make. `AllocatePages` returns 4K-aligned pages, which is the alignment the
kernel's PC-relative addressing relies on. `image_size` is capped at 16 MiB
as a sanity bound for this milestone.

### D4. The kernel is always built `ReleaseSmall`, with a dense linker script

`build.zig` pins the kernel to `ReleaseSmall` regardless of the loader's
build mode. Debug mode pulls in the safety runtime (`ubsan_rt` etc.) which
bloats the flat blob and emits absolute-address `movk` chains, violating the
load-anywhere contract. `kernel/linker.ld` lays the loadable sections out
densely from address 0; without it lld's default 64 KiB max-page-size
padding inflates the image ~100x (66 KiB → 0.7 KiB for the same code).

Position independence is verified before each change by disassembling the
linked ELF: all internal references must be `adr`/`adrp` (PC-relative), with
no absolute `movk` chains.

PC-relative references resolve against the content-at-`base+0` layout that
the loader guarantees (D1/D3): `adr` carries the content offset inside the
PC (so it also worked under the old `base+24` layout), while `adrp`+
`add` computes `(PC page) + VMA offset` and only resolves correctly when the
content starts at `base+0`. Both forms are therefore correct with the
current loader, and this is the invariant any future load-address scheme
must preserve.

### D5. The loader performs cache maintenance before the jump

The image bytes are written by data accesses, so before jumping the loader
cleans the D-cache to the point of unification and invalidates the I-cache
over the image (`dc cvau` + `ic ivau`, per byte, then `dsb ish` and `isb`)
— the same maintenance the firmware's own `LoadImage` performs. Without it
the instruction stream can be stale on ARMv8.

### D6. Handoff ABI (AAPCS64, register arguments on entry)

| register | value |
|----------|-------|
| x0 | kernel image base address (4K-aligned) |
| x1 | kernel image size in bytes |
| x2 | pointer to the EFI System Table |
| x3 | pointer to the already-open ESP root directory (`EFI_FILE_PROTOCOL`) |
| x0 (return) | u64 status; 0 = success |

The kernel runs on the loader's stack and returns to the loader, which
returns to the firmware.

## Evidence (observed on Apple M4 / macOS 27 / Zig 0.16.0)

`zig build`, `zig build image`, and `zig build run` complete. After a VZ
boot the ESP contains (all host-readable via `python3 image/mkfat32.py
--cat-file`):

- `BOOTED.TXT` — loader evidence; exact expected content. **Observed.**
- `LOADER.TXT` — loader-observed kernel placement: `base=`, `size=`,
  `entry_offset=0x18`, `first8=0x314b5344` (magic) and `second8` (entry)
  from the file header, plus `ram_first8` (the first 8 bytes that landed at
  the image base — the kernel's first instructions, since the header is not
  loaded into RAM). **Observed.**
- `RC.TXT` — `kernel_rc=0x0000000000000000` — the kernel executed and
  returned 0 to the loader. **Observed.** This is the primary handoff proof.
- `MEMMAP.TXT` — the EFI memory map (27+ descriptors, all RAM `xp=0 wb=1`);
  the kernel allocation sits in ordinary cacheable RAM. **Observed.**
- `KERNEL.BIN` — the flat image, verified by `elf2bin.py --info`
  (`entry_offset=0x18`, size, magic). **Observed.**
- `KERNEL.TXT` — the kernel's own marker, byte-perfect and byte-identical
  across repeated boots (the scramble known issue below is **resolved**);
  `zig build run` gates on its content. **Observed.**

## Known issue (observed, RESOLVED 2026-08-05): kernel-side file writes were scrambled on VZ

The kernel's own write to `\KERNEL.TXT` executed (the file was created with
the write's exact size and the write path returned normally), but the stored
bytes were scrambled: shifted slices of the kernel image's own `.rodata`
rather than the intended content. Observed across kernel load addresses
(0x7e55f000 and 0x7f328000 both corrupt) and with or without caller-side
`dc cvac` cleaning of the write buffer.

### Root cause (observed)

The loader loaded `KERNEL.BIN` verbatim, placing the 24-byte DSK1 header at
`base+0..23` and the loadable content at `base+24`. The kernel is linked
with VMA 0 == content start, so a literal at ELF VMA `V` sat at RAM
`base+24+V`. LLVM emits some data references as `adrp` + `add`: `adrp`
yields `(PC & ~0xfff) + page-relative immediate` (the image base, for a
sub-page image) and `add` adds the VMA low-12 offset — the +24 header
offset is silently dropped, so those references read every `.rodata`
literal 24 bytes early. `adr`-based references were unaffected because the
+24 rides inside the PC. The kernel's file writes therefore stored shifted
slices of its own `.rodata`; the run-varying `base=`/`size=`/`st=` hex
digits made the scramble offset appear to vary run to run. The loader's own
writes were byte-perfect because the loader is a PE image whose addressing
is set up by the firmware's `LoadImage`, with no hand-rolled offset.

Evidence for this mechanism (`artifacts/m1-baseline-run.txt`,
`artifacts/m1-exp1b-run.txt`, `artifacts/m1-exp1/` probe files,
`artifacts/m1-fix-run{1,2,3}.txt`, `artifacts/m1-fix-gated-run.txt`):
- Experiment 1 (loader-only): the loader wrote byte-perfect files **from the
  flushed kernel allocation** (`PROBE-A.TXT`, `PROBE-C.TXT`, `IMGPROBE.TXT`)
  with the kernel never jumping — the region + cache flush alone is clean;
  the trigger is kernel execution.
- Experiment 1b/1c: the kernel echoed its assembled write buffer into its
  own page; the loader's post-return dump showed the buffer scrambled with
  `.rodata` bytes even with **zero firmware calls** from the kernel.
- Disassembly: `adrp x8, #0; add x8, x8, #0x188` loads the "entry reached
  via handoff" literal 24 bytes before its RAM offset; the `adr` references
  are correct. A loader-side dump of the allocation shows RAM is byte-
  identical to the `KERNEL.BIN` file (0 differing bytes) — the data in RAM
  was right; the kernel's ADRP-based loads targeted the wrong offset.

### Fix (observed byte-perfect)

The loader now parses the header but does **not** load it into RAM: the
content is placed at `base+0` (ELF VMA `V` at RAM `base+V`) and control
transfers to `base + (entry_offset - 24)`. Both `adr` and `adrp`+`add`
references then resolve to the correct bytes. Verified over repeated
`zig build run` boots: `KERNEL.TXT` is byte-perfect and byte-identical
across runs, and `zig build run` now **gates** on its content
(`DIPSHITOS KERNEL`, `entry reached via handoff`) in addition to
`BOOTED.TXT` and `RC.TXT`.

## Consequences

- The loader stays ~600 lines of pure UEFI services; the kernel is a few
  hundred bytes of freestanding Zig.
- Adding ELF loading, relocations, or ExitBootServices later is a new ADR,
  not a patch to this one.
- Any change to the flat format or ABI must update this ADR, `elf2bin.py`,
  the loader, and the kernel together.

## Amendment (2026-10-02, B7 / #1874): preserve the image ceiling

The production PCM binding retains the **16 MiB KRN2 loader ceiling**.
Current main (`e48be7a7`) built a 16,775,848-byte image, leaving 1,368
bytes. The kernel linker now discards unused `.eh_frame`/`.eh_frame_hdr`:
there is no kernel DWARF unwinder or language exception path consuming them.
Frame-pointer diagnostics and host-test unwind metadata are unchanged.
This changes neither the flat format, relocations, nor the handoff ABI.

PCM storage is one fixed `ceil(sizeof(Playback)/4096)` page-pool charge
on first open, reused thereafter and retained even on failed-reset
quarantine. The default soundless boot allocates none. There is no
per-period allocation or unbounded queue growth.
