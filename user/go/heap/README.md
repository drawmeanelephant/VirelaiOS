# Bounded heap diagnostics

`heap.Publish("GOEDIT.ELF", 1)` starts an opt-in goroutine. Each iteration
forces GC, proves `ReadMemStats` with `heap: memstats ok`, and publishes the
newest 32 rows (at most 256 bytes per row) to `/host/HEAP/GOEDIT.ELF.TXT`.
It sleeps at least one second after publishing; GC can extend the interval.
One publisher owns each app label. The share is diagnostic, not an isolation
boundary. Rows are `H1 pid session seq live_bytes objects mallocs frees num_gc`.
The session identifies this publisher; a new publisher replaces old rows.
An `EBADF` publication is logged and retried from a fresh open at most twice,
with a timer sleep between attempts. Other file errors stop the publisher.

`HEAP.ELF -p <pid|name> [--polls 1..120] [--samples 1..32]` polls at most once per second
(16 polls by default). It prints the kernel's dynamic ownership count,
receipt peaks/totals, image aperture byte lengths and compacted mmap sizes.
When a matching publisher exists, it joins each new post-GC sample to the
current kernel snapshot. Missing samples, process identity or GC continuity
reset the trend; duplicate samples do not extend it.
`--samples` ends only after that many joined post-GC samples; failure to reach
the count by the finite poll deadline is an error. Acceptance requests N+2,
not a fixed wall interval that may finish before a slow GC returns.

A read crossing safe publication's delete/rename gap may see successful empty
or partial bytes through the stateless file API. The viewer reopens at most
three times with sleeps, and never accepts malformed rows. Persistent
corruption or permission failure is still an error, not a passing sample.

`heap.Suspected` requires three consecutive strict rises, at least 512 KiB
growth over that run, and rising kernel live pages. It ignores `HeapSys`,
GC targets, static image pages and cumulative allocation totals. This is
`leak suspected`, not proof: a deliberately growing retained working set
has the same shape. The constants reject editor-sized bookkeeping while
detecting `HEAPFIX.ELF leak`'s retained 512 KiB blocks.

`heap.View` is reusable by the combined OBSERVE tool. Publisher markers
report GC/ReadMemStats and complete publish durations separately from the
kernel snapshot, whose owner-approved budget is mean and p95 at most 100 us.
The maximum is always reported, including outliers. Both periodic loops use
Go's monotonic timer sleep, not a potentially fractional raw scheduler
tick that would also park an entire Go execution slot.

Memstat buffers are pinned before conversion to a syscall `uintptr`, because
Go stack growth can relocate an unpinned buffer even while it remains live.
`HEAPBUDG.ELF`, built by `build-heap.sh`, records 100 polls without a publisher
or allocation workload for the quiet-host budget boot in `go-stress`.
