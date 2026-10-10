# ADR 0043: bounded debugger and observability ABI

Status: **proposed, awaiting Timothy's R1 review** · Date: 2026-10-05 ·
Milestone: M94 · Card: #2001 · Index: #2000

## Context and evidence

Observed in `origin/main` `0a222095`: the namespace is 128 slots, rows 0–80
are registered, the monitor assigns `syscall.strace_pid`, and tracing prints
three raw arguments synchronously. Slots 70/71 are entirely suppressed.
`timer.period_ns` is 1,000,000,000; IRQ dispatch acknowledges, handles the
physical timer, schedules and EOIs. `RuntimeReceipt` has five fields.
There are 16 tasks; the index budgets four per Go process.

M94a reserves interfaces, not features. All three new handlers return native
`ENOSYS` (-4), even for malformed requests. No timer, ring allocation, user
copy, observer process or boot-default change is enabled here. The existing
monitor trace bytes and `syscalls` report remain unchanged. The second
timer's delivery and all cost limits below are **unmeasured acceptance
requirements**, not observations. No timer probe ran on this card.

## D1. Three feature slots, four-register control

Use one slot per feature: **81 `sys_trace`**, **82 `sys_profile`**, and
**83 `sys_memstat`**. A shared `sys_observe` handler would put three concurrent
shards back in one file. Dispatch is registered from `syscall_abi.zig`; each
shard changes its own handler and `implemented` flag, not `syscall.zig`,
ADR 0007, the metadata table or `vi.go`.

Trace/profile take `(op, token, ptr, bytes)` in x0–x3. **x4/x5 are ignored**,
not required to be zero: existing four-argument Go gateways do not initialize
them. Memstat takes `(pid, ptr, bytes)`; x3–x5 are ignored. All wire integers
are little-endian, with 8-byte record alignment, no pointers except the
explicit input addresses in the arm-exec request. Version is **1**.

| Feature/op | x1 | x2/x3 | Result after implementation |
|---|---|---|---|
| Trace 0 ARM | 0 | 88-byte `TraceConfig` | positive session token |
| Trace 1 DISARM | token | 0/0 | 0; retain unread records |
| Trace 2 FILTER | token | 88-byte replacement config | 0 |
| Trace 3 READ | token | output buffer/capacity | whole record count |
| Trace 4 STATUS | token | output pointer/24 | 0, header only |
| Trace 5 ARM_EXEC | token | 32-byte exec request | child pid, already traced |
| Profile 0 ARM | 0 | 72-byte `ProfileConfig` | positive session token |
| Profile 1 DISARM | token | 0/0 | 0; retain unread samples |
| Profile 3 READ | token | output buffer/capacity | whole record count |
| Profile 4 STATUS | token | output pointer/24 | 0, header only |
| Memstat | pid | output pointer/**240** | **240** |

Each feature has **one session system-wide**. As originally built it was
scoped to the opening principal's **uid**; the M97g audit (#2086) showed
that bound nothing — every ordinary EL0 process is `uid_user`, so any app
could enumerate the sequential tokens and filter, read, replace or disarm
a peer's session. Session control is now scoped to the **arming pid**:
only that pid or a `cap_proc_admin` principal may filter, read, disarm or
replace it (`EACCES` otherwise). Authorized ARM still replaces the
previous session with a new token, and an ARM is additionally permitted
when the owner process has exited — a stale token must not brick the
feature for every other app (pid slots are cleared, never silently
rebound). Tokens are **CSPRNG mints**, not counters: nonzero, never equal
to the still-live token, and not derivable from observed tokens. ARM
refuses `EAGAIN` when only the deterministic boot fallback is keyed — the
shared lazy-reseed (`virtio_entropy.read_seed`, the `sys_getrandom`
primitive) is attempted first; no entropy means no session. Every
control/read rechecks current caller privilege.
DISARM retains unread records; replacement frees prior backing and
invalidates its token. There is no per-pid lifetime claim or nested session.

`TraceConfig` is `version:u32, pid_count:u32, pids:8*u64, slots:2*u64`
(88 bytes). Bits 0–127 of `slots` select syscall numbers. `ProfileConfig`
omits `slots` (72 bytes). PID count is **0–8** for trace (0 permits ARM
before ARM_EXEC), **1–8** for profile; duplicate pids, nonexistent targets,
nonzero unused pid words or an unknown version refuse `EINVAL`. An empty
slot mask is valid (no traced calls). FILTER atomically replaces the entire
trace target set and mask, never partially applies an unauthorized set.

`TraceExecRequest` is four u64 words: `path_ptr, path_len, argv_ptr, argc`
(32 bytes). It retains slot 28's path and packed argv bounds (eight
256-byte slots, at most 255 usable bytes/argument), preserves the caller's
principal, and adds the child to the session before any child syscall.
Refuse `ENOSPC` before spawn if the pid set is full. M94b uses the existing
`exec_file_pinned_as` seam, pins this opt-in child to the caller's core and
arms it before releasing the IRQ-masked FILE/KERNEL locks. This prevents
publication on another core from racing the arm. Ordinary slot 28 and the
monitor's existing exec path do not change.

Native errors: `EINVAL` invalid request/target/version/size/op; `EBADF`
stale token; `EFAULT` failed uaccess; `ENOSPC` pid/token capacity;
`ENOMEM` ring backing refusal; `EAGAIN` no real entropy for the token
mint (M97g #2086); `EACCES` ownership/privilege refusal.
Profile ARM additionally returns `ENOSYS` if
the selected timer cannot be delivered. Failed ARM/FILTER/copy-out leaves
prior state and unread records unchanged. Readers never park in a syscall.

## D2. Frozen record formats

The field order and byte widths below are version 1. Padding/reserved
fields, unused frames, unused string bytes and unused region sizes are zero.
No native Zig `usize`, pointer, enum or `RuntimeReceipt` struct layout
crosses the boundary. The Zig `extern` and Go layouts are pinned by tests.
Changing these layouts or slot/op conventions after this PR merges is a
stop-and-ask, not a shard-local amendment.

### Trace record: 896 bytes

| Offset | Field |
|---:|---|
| 0 | `version:u32=1, flags:u32` |
| 8 | `pid:u64, tid:u64, cntpct:u64, number:u64` |
| 40 | `args:6*u64` (x0–x5 at entry, including unused registers) |
| 88 | `result:i64` (raw x0 bit pattern) |
| 96 | `errno:u32, string_mask:u32, fault_mask:u32, truncated_mask:u32` |
| 112 | `string_lengths:6*u16, reserved:u32=0` |
| 128 | `strings:6*128 bytes` |

Flag **1 REDACTED**; **2 NO_RETURN**. Unknown flag bits are zero.
`errno` is the native negative result's positive magnitude, otherwise 0;
domain-specific aliases stay native (not POSIX translations).
For process/thread exit, record entry with NO_RETURN and result/errno 0;
do not attribute a staged successor's registers or pid to the exiting call.
Entry timestamp is **CNTPCT_EL0**, not wall time or nanoseconds.
Tid is the current kernel task ID, not a Go goroutine ID.

Each mask's bit i refers to argument i. A string capture contains at most
**128 bytes**, with explicit length, no implicit NUL terminator; the
renderer escapes bytes. Longer input sets truncated_mask. Failed uaccess
sets fault_mask and length 0, never attempts a raw dereference. Capture
strings at entry while the caller's root/apertures are current. Binary
buffers and output pointers are never copied as strings. Slot 35's
replacement bit is masked out before deriving its string byte bound.

### Sample record: 176 bytes

| Offset | Field |
|---:|---|
| 0 | `version:u32=1, flags:u32=0` |
| 8 | `pid:u64, tid:u64, cntpct:u64, pc:u64` |
| 40 | `depth:u32, reserved:u32=0` |
| 48 | `frames:16*u64` |

PC is the interrupted EL0 PC. Frames are caller return PCs, newest first,
and **do not repeat PC**. Depth is **0–16**. Follow saved R29, reading
`{previous_fp, lr}` through fault-safe user reads, at most **16 reads of
16 bytes**. Stop on zero, non-16-byte alignment, non-increasing FP, wrap,
outside-user aperture or copy fault. Skip EL1 samples completely. No kernel
stack walk, scheduler modification, interrupt printing or heap allocation.

### Memstat record: 240 bytes

| Offset | Field |
|---:|---|
| 0 | `version:u32=1, flags:u32` (bit 0 = EXITED) |
| 8 | `pid:u64, cntpct:u64` |
| 24 | `peak_pages:u64, peak_regions:u64, static_pages:u64, total_pages:u64, record_failures:u64` |
| 64 | `live_pages:u64, live_regions:u64` |
| 80 | `text_bytes:u64, ro_bytes:u64, data_bytes:u64, stack_bytes:u64` |
| 112 | `region_sizes:16*u64` |

The five receipt fields retain `RuntimeReceipt` meanings. `live_pages`
counts current **dynamic ownership records**, not static image backing,
page tables or globally shared physical RAM; it is comparable to
`peak_pages`. `live_regions` is the current mmap table count; each size is
the corresponding table entry's byte length, compacted in table order.
The four image-region sizes are user aperture lengths in bytes, not page
counts. An exited retained descriptor has EXITED, receipt peaks/totals
preserved, and live/image/region fields zero. Free/nonexistent pid refuses.
M94d takes one coherent read-only snapshot under the existing process lock.
The 4,096 inline ownership capacity is not a universal peak ceiling:
landed overflow storage can raise it; do not falsify that fact in memstat.

## D3. Rings, loss and synchronization

Trace has **256 × 896 bytes = 229,376 bytes (56 pool pages)**.
Profile has **1,024 × 176 bytes = 180,224 bytes (44 pool pages)**.
Allocate on session open, release on replacement; no permanent
BSS banks. M94a allocates neither. Overwrite oldest when full, increment a
**saturating u64 dropped counter** for each overwritten unread record.
Counters reset only on a new session, not on READ, FILTER or DISARM.
Store each record's capture-time target uid in a **u32 private sidecar**,
not in the frozen wire record. Reserve one additional pool page per ring
for that sidecar and bookkeeping: **57 trace + 45 profile = 102 pages**
maximum. This permits read authorization even after the target exits or
its pid is reused. Inactive sessions retain only this fixed reservation.

READ begins with a **24-byte header**:
`version:u32=1, record_bytes:u32, count:u32, reserved:u32=0, dropped:u64`.
Capacity must hold the header, otherwise `EINVAL`. Copy at most
**16 whole records** and no partial record. Return count, 0 if empty;
unused output capacity is untouched. STATUS has the same header with
count = unread records. Peek/copy/drop: failed copy-out consumes nothing.

M94b/c each own a distinct IRQ-masked spin lock; never acquire a syscall
service-domain lock from an IRQ. Filtered-out trace calls perform no
timestamp, string copy or ring operation. Enabled trace capture happens
outside the trace lock; publication/rechecks occur inside it. Sampler
producers and readers use the sampler lock. Readers stage a bounded batch
and commit consumption only after copy-out, handling concurrent overwrite
by sequence validation rather than consuming new records accidentally.
No console I/O under either ring lock.

M94g's controlled session requires **trace dropped ≤ 0, sample dropped ≤ 0**.
Deliberate wrap fixtures separately require the exact forced drop count;
they are not permission to loosen the normal-session loss budget.

## D4. Privilege and never-logged slots

| Caller/target relationship | Observation |
|---|---|
| same process | allowed, no capability |
| different processes, same uid | allowed, no capability |
| different uid, no `cap_proc_admin` | `EACCES` |
| different uid, caller has `cap_proc_admin` | allowed |
| non-process EL0 control caller | `EINVAL` |

Check target principal before exposing target state, memory, samples or
receipt; recheck at capture and read, not only at ARM. Readers authorize
each buffered record against its capture-time uid sidecar, never against
the present occupant of a reused pid. Numeric pid reuse
must not turn an authorized session into cross-uid observation. Session
control follows the arming-pid / `cap_proc_admin` rule above (M97g
#2086 — the row "different processes, same uid" still holds for which
TARGETS a session may observe, but no longer authorizes driving a
session another process armed).
This does not create a capability-elevation syscall.

Slots **70 `sys_secret_get` and 71 `sys_tty_net_auth`** have six redacted
arg kinds. In a ring record, retain only version, REDACTED, pid/tid/counter
and slot; **all args, result, errno, masks, lengths and string bytes are 0**.
Never copy user memory for these slots. Ring renderers print `<redacted>`.
The legacy monitor serial path continues suppressing the entire line.
No other output, exported file or error path may recover redacted values.

## D5. Second timer, not a changed scheduler clock

Select the **EL1 virtual timer CNTV_CVAL_EL0/CNTV_CTL_EL0, PPI 27**, at
**100 Hz**, armed only for an active profiler session, on both vCPUs.
The physical CNTP timer's **1 Hz** comparator/preemption tick is unchanged.
Read CNTPCT for record timestamps and compare the IRQ count with elapsed
counter seconds, not with poll calls. Advance the virtual deadline by
`CNTFRQ/100`, skipping missed periods rather than flooding catch-up IRQs.

Delivery under Apple VZ is **inferred, not observed**. M94c/C2 must prove
real PPI 27 ack/EOI delivery, with separate IRQ/poll counters and no poll
fallback. Over a **10 s** counter-timed window on each enabled core require
**950–1,050 IRQs (100 ± 5 Hz)**, stable 1 Hz preemption, and unchanged
`live-wm-pacing` assertions (800–1,200 ms). If delivery fails, stop and ask;
do not silently substitute the physical timer or PMU.

C2 adds exactly one `docs/hardware-contract.md` row named **“EL1 virtual
sampling timer (CNTV, PPI 27)”**, tagged `[observed]` only after that gate,
citing `artifacts/live-observe-*` evidence and measured per-core rates.
No `[observed]` tag for an unrun probe.

IRQ ownership: **M94c alone** edits `timer.zig`, `gic.zig` and
`exceptions.zig`. `exceptions.zig` provides the interrupted frame/ELR/SPSR
to the sampler before scheduling can change them; `gic.zig` routes PPI 27
and EOIs exactly once. `main.zig` and `scheduler.zig` are not needed and
remain outside every M94 shard's Touches.

## D6. Measurable overhead budgets

These are chosen acceptance limits, **not measured results**:

| Path | Budget and method |
|---|---|
| traced call, including 128-byte captures | added mean ≤ **10 µs/call** and p95 ≤ **25 µs/call**, CNTPCT/CNTFRQ |
| filtered-out call | added mean ≤ **100 ns/call**, ≥100,000 calls, same baseline/active mask |
| profiler at 100 Hz | fixed-work median overhead **<2%**, 5 off + 5 on runs |
| kernel heap snapshot | ≤ **100 µs/poll**, ≤ **1 poll/s/target** |
| combined filtered tracer + profiler + heap poll | fixed-work median overhead **<3%**, 5 off + 5 on runs |

Report baseline, enabled cost and timer frequency. Do not divide by a
zero/negative duration or claim sub-counter-tick precision. Profiler-only
and combined medians are separate checks. In-process forced-GC publishing
is opt-in and timed separately; the 100 µs budget is kernel memstat, not a
promise that Go GC finishes in that time. Pollers sleep, never busy-spin.

## D7. Combined-session task budget

Adopt **one combined OBSERVE process**, importing the three viewer packages.

| Participant | Tasks |
|---|---:|
| kernel | 3 |
| seat | 4 |
| windowed app (GOEDIT) | 4 |
| combined OBSERVE | 4 |
| **total / capacity** | **15 / 16** |

Three separate four-task STRACE/PROF/HEAP processes would total **23** and
are forbidden for this session. Heap.Publish is a goroutine in the target,
not another process. M94g proves the actual task count, not this estimate.

## D8. File split and ordering

These are the final Touches for concurrent shards. Declare the exact IRQ
file list, including `exceptions.zig`, on M94c's claim before editing.

| Card | Final Touches |
|---|---|
| M94b #2003 | `kernel/src/trace.zig`, `kernel/tests/trace_test.zig`, `user/go/vi/trace.go`, `user/go/strace/*`, `tools/go/build-strace.sh`, `tools/gate/specs/live-strace.spec` |
| M94c #2004 | `kernel/src/sampler.zig`, `kernel/tests/sampler_test.zig`, `kernel/src/timer.zig`, `kernel/src/gic.zig`, `kernel/src/exceptions.zig`, `docs/hardware-contract.md`, `user/go/vi/profile.go`, `user/go/prof/*`, `user/go/proffixture/*`, `tools/go/build-prof.sh`, `tools/gate/specs/live-observe.spec` |
| M94d #2005 | `kernel/src/memstat.zig`, `kernel/tests/memstat_test.zig`, `kernel/src/process.zig` (one read-only accessor only), `user/go/vi/heapstat.go`, `user/go/heap/*`, `user/go/heapfixture/*`, `user/go/edit/main.go`, `tools/go/build-heap.sh`, `tools/gate/specs/go-stress.spec`, `tools/gate/specs/go-edit.spec` |
| M94e #2006 | `user/go/vi/log.go`, `user/go/vi/log_test.go`, `user/go/crashview/*`, `user/go/crashfixture/*`, `tools/go/build-crashview.sh`, `tools/gate/specs/live-crash-viewer.spec` |
| M94f #2002 | `user/go/vi/log.go`, `user/go/vi/log_test.go`, `user/go/logview/*`, `user/go/logfixture/*`, `tools/go/build-logview.sh`, `tools/gate/specs/go-selftest.spec` |
| M94g #2007 | `user/go/observe/*`, `tools/go/build-observe.sh`, `tools/gate/specs/live-observe.spec` |

M94b/c/d start only after M94a merges and are pairwise disjoint. M94f is
independent and disjoint from them. M94e follows both M94b and M94f;
M94g follows M94b/c/d, so their reused files never have concurrent editors.
No shard edits ADR 0007, ADR 0043, `syscall.zig`, `syscall_abi.zig`,
`slots_gen.go`, `slots_gen_test.go`, `vi.go` or `build.zig`.
Metadata/layout drift is a stop-and-ask. No new dependencies or GPL input.

## Acceptance and owner review

M94a: host stubs and mirror red/green; `zig build test`, `go test ./vi/`,
portable gates, unchanged `live-strace`, `live-procs-syscall` and `go-panic`;
format/inventory/coordination/diff checks. Record baseline failures honestly.
For kernel-only table changes, also run `go test -count=1 ./vi/`: Go does
not invalidate cached results for files outside the `user/go` module.
M94b–g own live feature, timer, privilege, loss, overhead and combined-session
proofs in their cards. Timothy must approve the slot rows, D1 surface,
D4 privilege matrix, D5 timer and D6 budgets before this R1 PR is merged.
No owner verdict is inferred from an agent's proposal.
