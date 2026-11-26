# ADR 0062: Action audit as ownership accounting, not a logging firehose

- Date: 2026-11-25; amended 2026-11-26 after independent review of #3978
- Status: **Proposed** — no audit behavior is accepted yet. Card **#3979**
  covers only the first mechanical scaffold slice; the split below is the arc.
- Related: ADR 0024 (trust and isolation), ADR 0043 (observability),
  ADR 0047 (`sys_procs` v2 accounting), ADR 0051 (host integration),
  ADR 0052 (agent identity and the existing agent/assist receipt lanes),
  ADR 0054 (measurement and grants), `docs/diagnostics.md` (the
  diagnostics-surface census), and `docs/app-authoring.md` §8/§9/§11
  (settings, chord ownership, and bounded app receipts).

> Ownership means every object has exactly one authority responsible for
> its lifecycle and mutations. Accounting means actions and costs are
> charged to that owner. Auditing is the evidence trail proving those two
> claims — not a universal record of everything that happened.

## Grounding

The tree already owns several fragments of this model, but not the unified
kernel-anchored action journal:

- **Apps own bounded diagnostic rings and crash receipts.** `vi.Log`
  appends to `/host/APPLOG/<APP>.LOG` (32 lines of at most 256 bytes), and
  failure paths publish `/host/CRASH/<APP>.TXT` with a frozen log tail
  (`docs/app-authoring.md` §11). These are deliberately small diagnostic
  records, not authorization or audit state.
- **AGENTD and GOASSIST already have receipt lanes.** ADR 0052 D4 requires
  MAC'd file receipts for every agent-channel dispatch/session, allowed or
  refused; its 2026-10-28 amendment requires a parallel lane for every
  resolved GOASSIST act. GOTOP reads both lanes. ADR 0052 also names the
  residual: those receipts are tamper-evident files on the share, not a
  kernel-anchored audit lane.
- **The kernel already distinguishes point-in-time accounting from an
  armed trace.** GOTOP reads process, grant, denial, mount, network, and
  measurement snapshots; OBSERVE/STRACE/PROF arm bounded sessions for one
  workload. `docs/diagnostics.md` keeps those planes split because their
  audiences and costs differ.
- **Security decisions already have a bounded refusal surface.** The
  grant table and denial ring answer “what posture does this process have”
  and “what was refused”; they do not answer the broader action question
  “which owner changed which object, under what authority, with what
  result”.
- **Some ownership rules are already explicit.** The chord registry names
  one owner and dispatch point per chord (`docs/app-authoring.md` §9).
  That is evidence for one-owner-per-object as a design rule, not proof
  that ordinary input routing already has such a registry.
- **Object names are bounded in the places this ADR first cares about.**
  Settings keys are at most 32 bytes (`kernel/src/settings.zig`,
  `user/go/settings`), and flat host-share names fit the existing NAME
  grammar. Fixed-size object evidence is therefore possible without a
  variable-length journal or interner for those object classes.

The missing abstraction is not “more logging”. It is a bounded,
structured, kernel-anchored record of meaningful ownership transitions and
security-relevant decisions, with cost accounting of its own.

## Decision

### D1. The kernel journal is parallel to, not a replacement for, existing receipt lanes

The AGENTD `/host/AGENT/AUDIT/` and GOASSIST `/host/ASSIST/AUDIT/` lanes
remain authoritative for their control-plane acts. They carry channel- and
act-specific metadata, payload digests, and HMAC verification that the
kernel journal deliberately does not duplicate. They are neither superseded
nor migrated by this ADR.

The new journal is the **only mandatory kernel write path for kernel-audited
events**. That phrase does not mean it is the only audit lane in the system:
kernel event writers must not substitute ad-hoc serial text or self-reported
app prose for a covered kernel transition, but channel-specific receipt lanes
continue in parallel. GOTOP or a bug-report bundle may correlate lanes at
read time; correlation is a reader concern, not journal ownership.

### D2. Audit ownership boundaries, not activity

The audit stream records transitions and decisions that change authority,
object ownership, durable state, or externally visible endpoint ownership:

- process creation/exit and image identity transitions;
- capability, grant, measurement, credential, and denial decisions;
- window/surface ownership transfer, capture, focus-seat routing, and
  clipboard or drag payload publication;
- endpoint ownership transitions: bind, listen, connect, accept, close,
  and the permission decision attached to them;
- durable publication boundaries for files: create, delete, rename, and a
  crash-safe publish/commit such as `WriteFileSafe` completing;
- settings mutation commits and their publication to subscribers;
- session restore, snapshot, resume, and machine-state lifecycle;
- privileged seat/service operations and host-integration requests.

A file write byte stream is activity; the audited file event is the
object-lifecycle or publication boundary, not every write. Network payload
flow is activity; no send, receive, packet, or byte-counting path is in the
mandatory journal. Ordinary rendering, syscall frequency, memory traffic,
scheduling, input micro-events, and packet flow likewise remain in tracing,
profiling, counters, or bounded workload sessions.

Dedicated diagnostic/audit lanes are excluded from file-mutation auditing
when auditing them would recurse or duplicate an existing contract:
`/host/APPLOG`, `/host/CRASH`, `/host/AGENT/AUDIT`, and
`/host/ASSIST/AUDIT`. Their own bounded receipt/log rules remain the
evidence contract for those writes.

### D3. The record is a fixed-size ownership fact

The initial journal record is budgeted at **96 bytes**, conceptually:

- 8 bytes: global sequence;
- 8 bytes: one named kernel monotonic clock domain;
- 8 bytes: actor process-instance token;
- 8 bytes: owner process-instance token or a small kernel/seat/service
  sentinel;
- 8 bytes: packed authority kind + authority/handle/grant id;
- 8 bytes: charge delta;
- 4 bytes: object generation;
- 8 bytes: packed plane, object kind, verb, result, charge kind, key
  length, and flags;
- 32 bytes: canonical object key;
- 4 bytes: padding/alignment reserve.

This is an encoding budget, not a syscall ABI commitment. The first card
may choose a tighter layout inside the same cap, but it may not become
variable-length.

The fields mean:

1. **Actor** — the kernel process-instance performing the action. A
   process-instance token must be reuse-resistant; if the current pid
   namespace lacks a generation or instance sequence, the wiring card must
   add or derive one before claiming process identity evidence.
2. **Owner** — the lifecycle/charging principal for the object at the time
   of the event, derived from the kernel/service object graph. Costs are
   charged to this owner. The owner is never self-reported text. Kernel,
   seat, and system-service ownership use explicit sentinel values.
3. **Authority** — the capability, grant, handle, seat role, or kernel
   invariant that made the action legitimate, or the missing authority for
   a denial.
4. **Object** — `(object kind, object generation, canonical object key)`.
   The key is binary or canonical bounded identity bytes, not a formatted
   diagnostic string:
   - settings keys use their exact key bytes (already ≤ 32 bytes);
   - flat host-share names use canonical NAME bytes;
   - endpoints use packed binary address/port/scope data;
   - process, window, surface, grant, session, and snapshot objects use
     their binary instance/id bytes;
   - content-addressed objects use their digest bytes.
5. **Action/result** — small enums for plane, verb, and outcome.
6. **Charge** — `(charge kind, charge delta)`, charged to the owner. The
   delta is zero when no meaningful cost unit exists.
7. **Ordering** — one global journal sequence plus one named monotonic
   clock domain.

Object types whose identities can be deleted and reused must provide a
monotonically increasing generation before they are wired. A type that
cannot provide generation is not audited yet; guessing identity from a
reused integer or name is worse than omitting that type. Compound actions
such as rename emit one fixed-size record per object identity involved;
readers use the global sequence and shared actor/authority to correlate
them.

### D4. Kernel-owned bounded rings are the mandatory kernel write path

The kernel writes fixed-size records into per-plane rings — sharded per
core where concurrency requires it — without allocation, blocking,
formatting, or host I/O. Each ring has:

- a per-ring write sequence and overwrite counter;
- a coalesced-event counter;
- a suppressed/rate-limited-event counter when a writer is in overload;
- a bounded snapshot/export operation.

The first plane split is security decisions, object lifecycle, durable
effects, and input/window routing. Noise in one plane cannot evict evidence
from another plane. Export is batch/read-watermark based, never one host
write per event.

Every accepted record receives a global sequence from one atomic journal
counter. That sequence is the total order of **journal acceptance**, not a
claim of causal ordering between concurrent cores. Cross-plane readers order
records by global sequence; the clock is ancillary evidence and must name a
single kernel monotonic domain rather than mixing tick, wall-clock, and
raw-counter units.

### D5. Loss policy is explicit, containment-based, and mathematically honest

No bounded journal can preserve all important evidence under unbounded
adversarial noise. This design does not claim that impossible property. It
requires containment, preferential retention, and visible loss:

- security-decision noise is confined to the security plane; it cannot
  overwrite lifecycle or durable-effect evidence;
- repeated identical success events are the first candidates for
  coalescing;
- repeated denial events are coalesced or rate-limited per actor/verb/result
  so a denial storm cannot write one record per protest indefinitely;
- ownership transfers, grant/authority changes, restore/snapshot lifecycle,
  and durable publication commits are never intentionally sampled for
  convenience;
- every overwrite, coalescing action, suppression, and rate-limit decision
  increments an explicit counter;
- because records carry a global sequence, readers can detect and size
  missing sequence ranges. Once the only copy of a record has been evicted,
  its contents are unknowable; the journal must say that honestly rather
  than claiming it can identify the lost action.

The detailed coalescing/rate-limit algorithm is implementation scope, but
its acceptance test must include a denial flood and must prove that the
flood is confined, counters account for it, and other planes retain their
records.

High-frequency detail is obtained by arming a tracer or observer session,
not by widening the always-on audit ring until it becomes a performance or
memory tax.

### D6. Audit, logs, traces, channel receipts, and crash receipts remain separate planes

The existing split is retained:

- **Kernel audit journal:** structured evidence of covered ownership and
  security transitions.
- **Agent/assist receipt lanes:** channel-specific, MAC'd file receipts
  under ADR 0052.
- **Diagnostic logs:** bounded human prose owned by each app.
- **Tracing/profiling:** high-frequency optional instrumentation, armed
  against one workload.
- **Crash receipts:** frozen post-mortem tails and tombstones.
- **Counters/snapshots:** point-in-time accounting such as `sys_procs` v2.

A crash receipt may embed the relevant audit tail, but the receipt is not
the journal. GOTOP/OBSERVE may read the journal, but they do not own its
write path. This prevents the unified model from collapsing the audiences
`docs/diagnostics.md` deliberately separated.

### D7. The audit system is itself accounted, with numeric first budgets

The journal publishes events written, coalesced, overwritten, suppressed,
exported, bytes retained, and writer-cost measurements. Reader access
requires a new audit-reader grant/capability added through the ADR 0054
grant-model amendment path; this ADR does not pretend that capability
already exists. Audit-reader denials are security-plane events, subject to
D5's anti-flood coalescing.

These are initial acceptance ceilings, **not measured results**:

| Path | Budget and method |
|---|---|
| journal record encoding | ≤ **96 bytes/record** |
| default ring payload | ≤ **4 planes × 256 records × 96 bytes** (≤ 96 KiB), plus bounded control state; total resident audit state ≤ **128 KiB** |
| recorded write | added mean ≤ **500 ns/event**, p95 ≤ **2 µs/event**, measured with CNTPCT/CNTFRQ on the reference host |
| non-event guard on an uncovered hot path | added mean ≤ **100 ns/call**, same measurement method |
| sustained 1,000 audited events/s during fixed work | median overhead **<1%**, 5 off + 5 on runs |
| batch export of 256 records | ≤ **1 ms** per drain on the reference host, measured separately from guest workload cost |

A slice that cannot measure against the relevant budget has not satisfied
the design. Budgets may be revised only by explicit amendment with measured
evidence, not silently missed.

## Rejected alternatives

- **Audit every syscall.** That confuses activity with authority, imposes
  cost on all software regardless of risk, and produces mostly noise. The
  syscall tracer already exists for scoped investigation.
- **Audit every network send or file write.** Those are byte/packet-flow
  activity. The journal covers endpoint ownership and durable publication
  boundaries instead.
- **Replace ADR 0052 receipts.** The agent/assist lanes carry authenticated
  channel-specific evidence and are already accepted. The kernel journal is
  a parallel trust-depth lane, not a migration or repeal.
- **Serial logging as the audit journal.** Serial is invaluable evidence
  for gates, but it is a debug transport: interleaved, textual, lossy under
  volume, and expensive in hot paths.
- **Unbounded persistence to `/host`.** Host writes are boundary-crossing
  effects themselves; making every audited action synchronously cross that
  boundary recurses the cost and amplification problem. Persistence is a
  batch export decision.
- **Application self-certification.** An app can annotate why it acted,
  but authority and ownership are established by the kernel/service object
  graph. Self-reported text cannot prove either.
- **A variable-length string journal or silent interner.** Variable payloads
  make writer cost and retention unbounded; an interner whose entries can
  be evicted separately makes historical object identity ambiguous. The
  fixed canonical-key rule avoids both for the first object classes.
- **One merged diagnostics mega-surface.** The diagnostics census already
  rejected that shape: post-mortem, live-follow, channel-receipt, and
  scoped-workload readers ask different questions at different costs.

## Card split

1. **#3979 — fixed ownership event envelope and bounded rings.** Define
   the fixed event envelope, plane/object/verb/result enums, global
   sequence/clock fields, ring overwrite/coalesce/suppression counters,
   and host/unit tests for encoding, ordering, overwrite accounting, and
   denial-flood containment. No syscall ABI, reader UI, persistent export,
   or live security semantics. Touches should stay inside kernel audit
   scaffolding/tests.
2. **Object identity and first live writers.** Add any missing
   process/object generation needed by D3, then wire exactly one or two
   low-frequency kernel lifecycle or grant/denial decisions. Depends on
   #3979. Touches the audit scaffold plus the specific decision sites only.
3. **Batch reader surface.** Add the audit-reader grant amendment and a
   bounded batch read into GOTOP or a small audit CLI. Depends on card 2.
   Touches the grant model, syscall ABI if required, and the chosen reader.
4. **Durable-effect planes.** Wire settings publication and selected
   host-file/object lifecycle events after their generation semantics are
   defined. Depends on card 2. Touches settings/file publication sites.
5. **Cost gate.** Add the synthetic writer/overhead benchmark and a
   declarative gate proving the D7 budgets. May start after #3979 but cannot
   close until the first live writer exists.
6. **Later lanes.** Clipboard/drag, snapshot/restore, endpoint ownership,
   input/window routing, host integration, and any bug-report bundle are
   separate cards only after the earlier reader evidence exists.

## Acceptance

This decision is accepted when, for each object type explicitly wired at
that point, the tree can answer from structured evidence rather than prose
inference:

- which actor process-instance acted;
- which owner was charged;
- which authority or grant permitted or refused the action;
- which typed object key and generation was acted on;
- whether the action was allowed, refused, coalesced, suppressed, exported,
  or overwritten after first being recorded;
- what cost/unit delta the action charged;
- where the record sits in the global journal-acceptance order;
- what the audit machinery itself cost against D7's numeric budgets.

The journal must also answer whether its visible tail is complete. When it
is incomplete, counters and global-sequence gaps must bound and expose that
incompleteness; the design does not claim to recover the contents of
evicted records.
