# ADR 0036: VirtioFS for VZ-restorable host files

- Status: ACCEPTED; live guest verification passed 2026-09-27
- Issue: #1780
- Related: ADR 0010 (userland filesystem ABI), ADR 0035 (Go seat/toolchain),
  `docs/hardware-contract.md` (VZ device and save/restore observations)

## Context

The Go seat persists settings and tab/session state through the existing
`/host` file API. The API currently rides the project's custom virtio device,
which Virtualization.framework rejects during save/restore validation on this
host. Removing that channel would break existing consumers; changing the
`/host` API would expand the migration unnecessarily.

The issue owner selected standard VirtioFS/FUSE as the alternate transport,
with the existing `/host` API preserved. A host-only probe showed that a
standard VirtioFS device configuration passes VZ save/restore validation.
The live guest gate now measures discovery, FUSE initialization and I/O both
before save and after a cross-process restore.

## Decision

1. Keep `/host` and the `virtio_file` API as the guest-facing contract.
   Applications and Go filesystem clients do not select a transport.
2. Add an explicitly selected standard VZ VirtioFS device, using the
   `virelaios` share tag, and implement the guest-side virtio-fs/FUSE path
   behind that API.
3. Retain the custom-virtio implementation and its existing behavior as the
   fallback. Do not change the VM boot default; VirtioFS is selected through
   the runner's explicit `--virtio-fs` option.
4. Accept the restore path only after the `live-vz-restore` gate observes a
   cross-process save/load: GOTABWM loads the staged settings and session,
   the resumed guest reports that state after a presented composite tick,
   and a post-resume `/host` read succeeds through the restored FUSE queue.

## Evidence and limits

- **Observed:** VZ rejects the custom-virtio configuration at
  `validateSaveRestoreSupport`.
- **Observed:** the entitlement-signed host-only probe accepts the standard
  VirtioFS configuration at that validation call.
- **Observed:** the `live-vz-restore` gate passes 16/16 on macOS 27.2
  (26B5091g), arm64. Its `fs-save`/`fs-load` pair reports guest device
  `0x1af4:0x105a` ready with tag `virelaios`, FUSE 7.31 and a 2048-byte
  maximum write; GOTABWM loads the staged settings and session before save
  and emits the same witness after process B resumes.
- **Observed:** the restored guest reads `VZRESTORE.WITNESS` through `vf cat`
  after resume (`bytes=13`) and writes/syncs a 4,097-byte deterministic
  pattern in three FUSE writes; the gate byte-checks the new host file and
  confirms the staged fixture bytes remain unchanged. A focused live boot
  also exercised `/host` listing and an 85-byte file read before the
  cross-process gate.
- **Implementation fix measured by the gate:** the common configuration
  `device_feature_select` must read selector 0 before selector 1. The
  reversed order exposed low/high feature words swapped and stopped the
  guest before queue setup; with the spec order, the FUSE handshake and
  subsequent file operations pass.

This evidence is scoped to the observed Mac and OS build. It does not promise
cross-host or cross-version saved-state portability. VirtioFS remains an
explicit opt-in and does not change the boot default or `/host` contract.
