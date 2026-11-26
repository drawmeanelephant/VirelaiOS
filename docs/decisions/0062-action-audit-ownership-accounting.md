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
- **Some object identities are already fixed-size.** Settings keys are at
  most 32 bytes (`kernel/src/settings.zig`, `user/go/settings`). Kernel
  file metadata already has binary filesystem/inode identity, and the FUSE
  lookup wire format exposes u64 generation, but that generation is not yet
  carried through the adopted metadata object model; file lifecycle cannot
  be wired until it is. Socket handles already carry generation bits. Path
  names are not object identity: they can be long, nested, case-folded,
  and changed by rename. Fixed-size object evidence is therefore possible
  without a variable-length journal or interner, but only when each object
  kind uses its binary instance identity.

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
object ownership, durable state, or socket ownership:

- process creation/exit and image identity transitions;
- capability, grant, measurement, credential, and denial decisions;
- window/surface ownership transfer, capture, focus-seat routing, and
  clipboard or drag payload publication;
- socket ownership transitions: bind, listen, connect, accept, close, and
  the permission decision attached to them. Once created, the audited
  object is the socket instance. A pre-creation permission denial instead
  uses the fixed network-namespace object; there is no honest socket
  instance to name before the operation would have minted one. Peer
  address tuples are connection metadata, not a substitute for either
  identity;
- kernel-visible file-object mutations: create, delete, and rename. The
  journal records the kernel mutation that occurred. It does not infer
  that a rename is the logical completion of a userland helper such as
  `WriteFileSafe`; a future publication ABI or explicit convention would
  be required before claiming that higher-level meaning;
- settings mutation commits and their publication to subscribers;
- session restore, snapshot, resume, and machine-state lifecycle;
- privileged seat/service operations and host-integration requests.

A file write byte stream is activity; the audited file event is the
object-mutation boundary, not every write. Network payload
flow is activity; no send, receive, packet, or byte-counting path is in the
mandatory journal. Ordinary rendering, syscall frequency, memory traffic,
scheduling, input micro-events, and packet flow likewise remain in tracing,
profiling, counters, or bounded workload sessions.

Dedicated diagnostic/audit lanes are excluded from file-mutation auditing
when auditing them would recurse or duplicate an existing contract:
`/host/APPLOG`, `/host/CRASH`, `/host/AGENT/AUDIT`, the GOASSIST receipt
lane under either `/host/ASSIST/AUDIT` or `/disk/ASSIST/AUDIT`, and the
future journal export lane. Their own bounded receipt/log rules remain the
evidence contract for those writes.

Journal export is accounted by export watermarks and counters, not by
auditing the export file mutation as another journal event; otherwise
every batch would create the event the next batch had to export.

### D3. The record is a fixed-size ownership fact

The initial journal record is budgeted at **104 bytes**, encoded as packed
bytes rather than as a native struct layout:

- 8 bytes: global sequence;
- 8 bytes: timestamp in the kernel monotonic nanoseconds-since-boot clock;
- 8 bytes: actor principal token;
- 8 bytes: owner principal token;
- 8 bytes: packed authority kind + authority/grant/capability id;
- 8 bytes: object generation;
- 8 bytes: charge delta;
- 8 bytes: coalesced-input count, or 1 for an uncoalesced record;
- 8 bytes: packed plane, object kind, verb, result, charge kind, key
  length, and flags;
- 32 bytes: canonical object key.

The fields sum to 104 bytes. This is an encoding budget, not a syscall ABI
commitment. The envelope card may choose a tighter layout inside the same
cap, but it may not become variable-length.

Principal tokens are compact typed identities. Their high bits name the
principal kind and their low bits carry the kind-specific id or instance
bits. Actor and owner tokens may name a process instance, uid, kernel,
seat, service, or boot/system principal. That distinction matters because
file ownership is uid-shaped, window and session ownership is often
process-shaped, and boot, monitor, reaper, host-integration, and kernel
work may have no responsible EL0 process. The exact bit split belongs to
the envelope card.

The fields mean:

1. **Actor** — the kernel-attributed principal performing the action. For
   a process act this is a process-instance token. A process-instance token
   must be reuse-resistant; if the current pid namespace lacks a generation
   or instance sequence, the wiring card must add or derive one before
   claiming process identity evidence. Kernel, service, boot/system, and
   uid acts must use their own typed principal rather than being falsely
   attributed to whatever process happened to be nearby.
2. **Owner** — the lifecycle/charging principal for the object at the time
   of the event, derived from the kernel/service object graph. Costs are
   charged to this owner. The owner is never self-reported text.
3. **Authority** — the capability, grant, seat role, or kernel invariant
   that made the action legitimate, or the missing authority for a denial.
   Raw object handles are object identity, not authority; when a handle is
   the relevant object it appears in the object identity fields rather than
   needing a second full-width copy plus a kind discriminant in this packed
   field. No raw 64-bit object handle is an authority value; an authority
   type that cannot fit this compact representation is not wired until the
   encoding is explicitly amended.
4. **Object** — `(object kind, object generation, canonical object key)`.
   The key is binary or canonical bounded identity bytes, not a formatted
   diagnostic string:
   - settings keys use their exact key bytes, already ≤ 32 bytes;
   - file objects use the binary `(filesystem, inode)` identity in the
     key, with the object generation field carried separately. Path names
     are not object identity. A filesystem that cannot prove stable binary
     identity plus reuse-resistant generation is not wired;
   - socket objects use their generation-tagged binary socket handle.
     Local and peer address tuples do not fit the key and are not initial
     identity evidence. Pre-creation network permission denials use the
     fixed network-namespace object key instead;
   - process, window, surface, grant, session, and snapshot objects use
     their binary instance/id bytes;
   - content-addressed objects use their digest bytes.
5. **Action/result** — small enums for plane, verb, and outcome.
6. **Charge** — `(charge kind, charge delta)`, charged to the owner. The
   delta is zero when no meaningful cost unit exists.
7. **Coalescing** — the count of input events represented by this record.
   The count is 1 for a normal record and greater than 1 for a coalesced
   summary. Events may coalesce only when actor, owner, authority, plane,
   object, verb, result, and charge kind are identical. The charge delta is
   then the aggregate sum across the represented events and is updated with
   the count. If that sum would overflow, the events do not coalesce.
8. **Ordering** — one global journal sequence plus one named monotonic
   clock domain.

Object types whose identities can be deleted and reused must provide a
reuse-resistant generation representable in u64 before they are wired.
Reuse resistance is scoped to one boot/journal epoch unless the object
type provides a wider epoch; persistent export batch metadata must carry
the boot/journal epoch so exported records are not compared across boots
as if object keys were globally unique. A type that cannot provide
generation is not audited yet; guessing identity from a reused integer,
path, or name is worse than omitting that type.

A non-replacing rename is one mutation of the same file-object identity. A
replacing rename involves a second object whose deletion/replacement must
also be identified. If the filesystem cannot prove both object identities
atomically with the mutation, replacing rename is not wired for that
backend. If a later object type genuinely involves multiple audited
objects, it emits one fixed-size record per object; readers use the global
sequence and shared actor/authority to correlate them.

### D4. Kernel-owned bounded rings are the mandatory kernel write path

The kernel writes fixed-size records into per-plane rings — sharded per
core where concurrency requires it — without allocation, blocking,
formatting, or host I/O. Each ring has:

- a per-ring write sequence and overwrite counter;
- a coalesced-event counter;
- a suppressed/rate-limited-event counter when a writer is in overload;
- a bounded snapshot/export operation.

The first plane split is authority/grant decisions, denial decisions,
object lifecycle, durable effects, and input/window routing. Denial noise
therefore cannot overwrite grant/authority changes, and noise in one plane
cannot evict evidence from another plane. Export is batch/read-watermark
based, never one host write per event. Ring capacities in D7 are aggregate
across any per-core shards, not per core.

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

- denial noise is confined to the denial plane; it cannot overwrite
  authority/grant, lifecycle, or durable-effect evidence;
- repeated identical success events are the first candidates for
  coalescing;
- repeated denial events are coalesced or rate-limited per actor/verb/result
  so a denial storm cannot write one record per protest indefinitely, and
  denials live outside the authority/grant plane;
- ownership transfers, grant/authority changes, restore/snapshot lifecycle,
  and file-object mutations are never intentionally sampled for
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
already exists. Audit-reader denials are denial-plane events, subject to
D5's anti-flood coalescing.

These are initial acceptance ceilings, **not measured results**:

| Path | Budget and method |
|---|---|
| journal record encoding | ≤ **104 bytes/record** |
| default ring payload | ≤ **5 planes × 192 records × 104 bytes** = **99,840 bytes** (97.5 KiB), aggregate across per-core shards, plus bounded control state; total resident audit state ≤ **128 KiB** |
| recorded write | added mean ≤ **500 ns/event**, p95 ≤ **2 µs/event**, measured with CNTPCT/CNTFRQ on the reference host |
| non-event guard on an uncovered hot path | added mean ≤ **100 ns/call**, same measurement method |
| sustained 1,000 audited events/s during fixed work | median CPU-time overhead **<1%**, 5 off + 5 on runs; the cost gate must name the fixed workload before claiming this budget |
| batch export of up to 192 records from one plane | ≤ **1 ms** per drain on the reference host, measured separately from guest workload cost |

A slice that cannot measure against the relevant budget has not satisfied
the design. Budgets may be revised only by explicit amendment with measured
evidence, not silently missed.

## Rejected alternatives

- **Audit every syscall.** That confuses activity with authority, imposes
  cost on all software regardless of risk, and produces mostly noise. The
  syscall tracer already exists for scoped investigation.
- **Audit every network send or file write.** Those are byte/packet-flow
  activity. The journal covers socket ownership and file-object mutation
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

1. **#3979 — bounded ring mechanics only.** Generic fixed-size record
   storage, per-ring sequence/read watermarks, overwrite/drop counters,
   retained-byte accounting, and host/unit tests. No event taxonomy, object
   identity model, owner/charging model, coalescing policy, syscall ABI,
   reader UI, persistent export, or live writer. Touches:
   `kernel/src/audit_ring.zig`, `build.zig`, and directly related tests.
2. **Event envelope and taxonomy.** Define the ≤104-byte packed encoding,
   principal/object/authority tokens, plane/object/verb/result/charge enums,
   global sequence/clock insertion, coalesced-count semantics, and encoding
   tests. Depends on #3979. Touches the audit envelope module and its tests.
3. **Object identity and first live writers.** Add any missing
   process/object generation needed by D3, then wire exactly one or two
   low-frequency kernel lifecycle or authority/denial decisions whose object
   identity already fits D3. Depends on card 2. Touches the audit scaffold
   plus the specific decision sites only.
4. **Loss policy and denial-flood proof.** Implement coalescing/rate-limit
   policy and tests proving a denial flood is confined to the denial plane,
   counted, and unable to evict authority/lifecycle/durable evidence.
   Depends on card 3. Touches the audit writer/policy module and tests.
5. **Batch reader surface.** Add the audit-reader grant amendment and a
   bounded batch read into GOTOP or a small audit CLI. Depends on card 3.
   Touches the grant model, syscall ABI if required, and the chosen reader.
6. **Durable-effect planes.** Wire settings publication and selected file
   object lifecycle events only after their binary identity and generation
   semantics are proven. Depends on card 3. Touches settings/file mutation
   sites.
7. **Cost gate.** Add the synthetic writer/overhead benchmark and a
   declarative gate proving the D7 budgets. May start after card 2 but
   cannot close until the first live writer exists.
8. **Later lanes.** Socket ownership, clipboard/drag, snapshot/restore,
   input/window routing, host integration, richer endpoint metadata, and any
   bug-report bundle are separate cards only after the earlier reader
   evidence exists.

## Acceptance

This decision is accepted when, for each object type explicitly wired at
that point, the tree can answer from structured evidence rather than prose
inference:

- which typed actor principal acted;
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
