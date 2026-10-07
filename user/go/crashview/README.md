# Crash receipts

Build `CRASHVIEW.ELF` and `CRASHFIX.ELF` with
`bash tools/go/build-crashview.sh` using the provisioned Go 1.27.1 fork.
No boot default or app catalog changes are made.

`CRASHVIEW.ELF` opens an appkit window. Up/Down selects a receipt, Page Up/Down
scrolls, **R** reopens its recorded basename under the in-process M94b tracer,
and Escape closes. Original arguments are unavailable in the frozen receipts,
so reopening supplies none. Tracing filters console/file calls and exit,
retains the last 64 rendered rows, reports loss, and stops after exit or 120 s.
No separate `STRACE.ELF` process is spawned.

`-text` mirrors records to serial without opening a window; `-polls N` limits
that mode. Polling reads bounded content once per second, including the stack
sidecar, rather than relying on name/size Watch snapshots. Records are newest
**observed** first: old receipts have no comparable cross-format wall timestamp.
Both `/host/CRASH` and `/host/crash` are read unless a unique probe proves they
alias. The 16-entry directory ABI window is reported when full, not presented
as an exhaustive listing.

Apps opt into a current-goroutine panic stack with:

```go
defer vi.CrashGuard("APP.ELF")
```

The guard keeps `.TXT` receipt bytes unchanged, adds a bounded 16 KiB `.STK`
sibling, and exits 2. The versioned sidecar header binds the receipt's FNV-64a
fingerprint to a monotonic generation and marks truncation. Fingerprints detect
changes, not authenticity. Each goroutine needs its own guard. Exit-only
receipts have no stack; kernel tombstones expose one recorded symbolic PC,
not an unwound stack.

`live-crash-viewer` keeps the monitor boot and adds Go/kernel latency, equal-size
replacement, traced reopening and the window R action. `-exercise` is the
self-sequencing gate drill, not the ordinary launch path. Host parser fixtures
in `testdata/goself.txt` and `kernel.txt` are copied from the unmodified
`go-panic`/`live-crash-viewer` baseline at `0c8281a1`; the 1024-byte truncation
test derives its serial tail from that real tombstone.
`crashfix.txt`/`crashfix.stk` are the first real guarded panic captured by
the expanded gate, not a host-synthesized traceback.
