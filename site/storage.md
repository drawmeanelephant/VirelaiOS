---
title: Storage & filesystem
parent: capabilities
status: published
tags: [capabilities, storage, fat]
---

# Storage & filesystem

Files live on the macOS **host share**, exposed to the guest through the
existing `/host` API. The runner can use the retained custom-virtio channel or
the explicitly selected standard VirtioFS/FUSE backend (`--virtio-fs`); the
guest-facing API is the same. Files are not stored in a guest FAT volume.
Since M34 HF6 (issue #740) the boot image is a boot volume only: it embeds
`EFI/BOOT/BOOTAA64.EFI` + `KERNEL.BIN` and nothing else; applications, data,
and evidence all ride the share.

## The stack

- **`virtio_file.zig`** — the guest-facing host-file API and backend
  selection. The legacy custom-virtio channel uses queue 5
  (`--cvc-file <host-dir>`); standard VirtioFS/FUSE is an explicit
  alternative for VZ save/restore (`--virtio-fs <host-dir>`). Both feed the
  same `/host` file API.
  The custom-virtio request/reply wire includes
  (`PROBE`/`LIST`/`READ`/`STAT` plus the HF3 mutation set — OPEN/CLOSE/
  WRITE/TRUNCATE/FSYNC/RENAME/MKDIR/DELETE — and `CLONE` dedup) pinned
  byte-for-byte by the class-A channel fixtures.
- **`file_table.zig`** — the per-process 8-handle file table behind the file
  syscalls (open/read/write/close/dir at slots 23–27, the mutating slots
  34–37, `file_sync` at 77, and `fs_metadata` at 79), with paths
  canonicalized onto `/host/...`.
- **`fat32_ro.zig`** — read-only FAT32 parsing for images attached through
  the USB mass-storage seam (`--usb-msd <image>`): the guest can read files
  off a USB disk image. The MSC block layer itself can write raw sectors
  (the `usb msc probe` writes a gap sector — `live-usb-msc`), but there is
  no writeable FAT filesystem; the guest's own writeable FAT driver went
  with M34 HF6.
- **Commands** — `ls [<dir>]`, `cat <file|path>`, `write <file> <text...>`,
  `mount` (reports the armed host-share store).

## One store: the share

There is one writable store — the host folder the runner was pointed at.
`mount` reports it, `write` appends to it, and the host disk is the ground
truth: a file written in-guest is a file on the Mac, byte for byte, across a
reboot.

## The userland file ABI

Milestone ten opened storage to EL0: a per-process file-handle table
(8 static handles, reset at process lifecycle) behind
`sys_file_open`/`read`/`write`/`close` and `sys_dir_list` (slots 23–27).
The original `SAVETEXT.BIN`, `TYPE.BIN`, and `DIR.BIN` demos (retired M78c)
proved the seam; current `GOSH.ELF` redirection/`cat` and `GOFILES.ELF`
listing exercise it, while `NOTE.ELF` (the editor since M66c) uses it for
real work. Milestone
thirteen's B1 card extended it with `sys_file_delete`/`rename`/`truncate`/
`free` (slots 34–37); the original `FSTEST.BIN` proof was deleted with the
second volume (M34 HF6 #740), and the wire it exercised is now pinned
byte-for-byte by the channel fixtures. The current file manager is
`GOFILES.ELF` (gate `go-fileman`: list/open).

## Loading programs

`exec <file> [args...]` streams the flat image out of the share — the
stateless `READ` op, at the honest 2 KiB EL0 read cap — strips its header,
rebuilds the user root around its pages, and spawns it at EL0. Program
images seed the share at build and gate time; they are not embedded in the
boot image.

<Aside kind="info">

**VERIFIED.** `live-fs` (write/ls/cat persisting across a reboot onto the
host disk) and `live-gfs` (the general store *is* the share) gate the
storage path; `live-exec` gates program loading, and `live-user-fs` gates
the userland file syscall ABI across two boots.

</Aside>

<Aside kind="warning">

**LIMITATION.** One host-backed store: no guest-side volume management, no
journaling, no block cache, and a bounded direct-read cap (2 KiB per EL0
call — larger reads loop). The ABI covers delete, rename, truncate, free,
fsync, and metadata operations (slots 34–37, 77, and 79), but it is a
bounded file API, not a POSIX filesystem. VirtioFS remains opt-in and its
save/restore observation is limited to the documented macOS 27.2 host.

</Aside>
