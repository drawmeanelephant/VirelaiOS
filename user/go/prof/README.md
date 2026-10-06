# Bounded CPU profiler

Build with `bash tools/go/build-prof.sh` after provisioning the existing
Go 1.27.1 fork. Stage the exact unstripped target ELF under `/host`.

Commands:

- `PROF.ELF -p <pid|name> -d <seconds>`
- `PROF.ELF exec <file> [args]`

Outputs are `/host/PROF/<APP>.folded` and `<APP>.top`. Unknown PCs stay
hex addresses; zero samples is an error. `prof` is also an importable package
for the combined observer. No new dependencies: `debug/elf` is Go's
BSD-3-Clause standard library. It parses a bounded native-cursor read of the
ELF because the guest stdlib's positional shadow is limited to 16 MiB.

`live-observe` owns independent sample-count, overhead and compiler boots.
The compiler boot uses the pinned self-host hello with SSA checking enabled.
Its separately named `GOCMDPROFILE.ELF` has only the SSA package built with
`-N -l` to make the otherwise very short compiler phase observable at 100 Hz.
Its two existing read-only SSA invariant checks repeat 256 times. This is an
explicit sampling fixture, not a normal-compiler timing measurement.
The normal self-host compiler is not replaced.
After the owner releases M94b's measurement window, run the full gate under
`/tmp/virelai-vz.lock` with no competing VMRunner or Zig/Go build/test work.
Record `uptime` before and after. The fixture measures seven interleaved
off/on pairs and reports median/min/max; a spread crossing 2% is unresolved,
not a pass. There is no load-average floor under the 2026-10-06 ruling.
