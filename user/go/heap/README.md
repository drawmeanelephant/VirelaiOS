# Bounded heap diagnostics

`heap.Publish("GOEDIT.ELF", 1)` starts an opt-in goroutine. Each second it
forces GC, proves `ReadMemStats` with `heap: memstats ok`, and publishes the
newest 32 rows (at most 256 bytes per row) to `/host/HEAP/GOEDIT.ELF.TXT`.
One publisher owns each app label. The share is diagnostic, not an isolation
boundary. Rows are `H1 pid session seq live_bytes objects mallocs frees num_gc`.
The session identifies this publisher; a new publisher replaces old rows.
An `EBADF` publication is logged and retried from a fresh open at most twice,
with a timer sleep between attempts. Other file errors stop the publisher.

`HEAP.ELF -p <pid|name> [--polls 1..32]` polls at most once per second
(16 polls by default). It prints the kernel's dynamic ownership count,
receipt peaks/totals, image aperture byte lengths and compacted mmap sizes.
When a matching publisher exists, it joins each new post-GC sample to the
current kernel snapshot. Missing samples, process identity or GC continuity
reset the trend; duplicate samples do not extend it.

`heap.Suspected` requires three consecutive strict rises, at least 512 KiB
growth over that run, and rising kernel live pages. It ignores `HeapSys`,
GC targets, static image pages and cumulative allocation totals. This is
`leak suspected`, not proof: a deliberately growing retained working set
has the same shape. The constants reject editor-sized bookkeeping while
detecting `HEAPFIX.ELF leak`'s retained 512 KiB blocks.

`heap.View` is reusable by the combined OBSERVE tool. Publisher markers
report GC/ReadMemStats and complete publish durations separately from the
kernel snapshot, whose ADR 0043 budget is 100 us. Both periodic loops use
Go's monotonic timer sleep, not a potentially fractional raw scheduler
tick that would also park an entire Go execution slot.
