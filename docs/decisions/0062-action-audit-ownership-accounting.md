# ADR 0062: Action audit as ownership accounting, not a logging firehose

- Date: 2026-11-25
- Status: **Proposed**
- Related: ADR 0024 (trust and isolation), ADR 0043 (observability),
  ADR 0047 (`sys_procs` v2 accounting), ADR 0051 (host integration),
  ADR 0052 (agent identity), ADR 0054 (measurement and grants),
  `docs/diagnostics.md` (the diagnostics-surface census), and
  `docs/app-authoring.md` §9/§11 (ownership and bounded app receipts).

> Ownership means every object has exactly one authority responsible for
> its lifecycle and mutations. Accounting means actions and costs are
> charged to that owner. Auditing is the evidence trail proving those two
> claims — not a universal record of everything that happened.

## Grounding

The tree already owns several fragments of this model, but not the unified
action journal:

- **Apps own bounded diagnostic rings and crash receipts.** `vi.Log`
  appends to `/host/APPLOG/<APP>.LOG` (32 lines of at most 256 bytes), and
  failure paths publish `/host/CRASH/<APP>.TXT` with a frozen log tail
  (`docs/app-authoring.md` §11). These are deliberately small diagnostic
  records, not authorization or audit state.
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
- **Input ownership is explicit rather than emergent.** The chord registry
  names one owner and dispatch point per chord (`docs/app-authoring.md`
  §9). The same structural rule is needed for auditable actions: the owner
  must come from the object/capability/handle being used, never from a
  self-authored log string.
- **The seat and host-integration lanes already emit receipts after real
  effects.** Session writes, pane writes, settings notices, host outbox
  deliveries, window lifecycle changes, and restore operations all have
  marker/receipt vocabulary. Those are useful evidence but are per-feature
  and inconsistent as an audit model.

The missing abstraction is therefore not “more logging”. It is a bounded,
structured record of meaningful ownership transitions and security-relevant
decisions, with cost accounting of its own.

## Decision

### D1. Audit ownership boundaries, not activity

The audit stream records transitions and decisions that change authority,
access, durable state, or externally visible effects:

- process creation/exit and image identity transitions;
- capability, grant, measurement, credential, and denial decisions;
- window/surface ownership, capture, focus-seat routing, and clipboard or
  drag payload publication;
- host-share file mutations that cross the guest/host boundary;
- settings mutations and their propagation to subscribers;
- network bind/connect/listen ownership and externally visible sends where
  the endpoint is the audited object, not every packet;
- session restore, snapshot, resume, and machine-state lifecycle;
- privileged seat/service operations and host-integration requests.

Ordinary rendering, syscall frequency, memory traffic, scheduling, input
micro-events, and packet flow are **activity**. They remain in tracing,
profiling, counters, or bounded workload sessions. A high-frequency path
must not become mandatory audit cost.

### D2. The record is an ownership triple plus outcome

Every audit record is fixed-size and answers:

1. **Actor** — the process/image principal performing the action.
2. **Authority** — the capability, grant, handle, seat role, or kernel
   invariant that made the action legitimate (or the missing authority for
   a denial).
3. **Object** — the typed object acted on: process, window, surface, file
   capability, endpoint, settings key, clipboard generation, snapshot, or
   session.
4. **Action/result** — small enums for verb and outcome.
5. **Charge** — the cost or unit the action consumed when meaningful
   (bytes, rows, frames, notices, quota, or zero when not meaningful).
6. **Ordering** — ring sequence and the kernel tick/counter needed to order
   it against other planes.

Strings are not stored in the hot path. Readers resolve enums and ids to
names. A self-reported app string can be diagnostic evidence, but it is not
the authority or object owner.

### D3. Kernel-owned bounded rings are the only mandatory write path

The kernel writes fixed-size records into per-plane rings — sharded per core
where concurrency requires it — without allocation, blocking, formatting, or
host I/O. Each ring has:

- a monotonically increasing write sequence;
- a reader-visible overwrite/drop counter;
- a coalesced-event counter;
- a bounded snapshot/export operation gated by the audit capability.

Rings are split by contention and audience rather than by implementation
convenience. A first split can be security decisions, object lifecycle,
host/file effects, and input/window routing. A ring may be per-core when
the writer is concurrent; ownership of each ring/sequence must still be
singular. Export is batch/read-watermark based, never one host write per
event.

### D4. Loss policy is explicit and asymmetric

Audit correctness does not require unlimited retention. It requires that
loss is visible and that important evidence survives noise:

- denials, authority changes, ownership transfers, restore/snapshot state
  transitions, and host-visible mutations are never intentionally sampled;
- repeated identical success events may coalesce to `(first_seq,last_seq,
  count)`;
- when pressure forces a choice, success noise is lost before denial or
  ownership evidence;
- every ring publishes overwritten/coalesced counters so a reader cannot
  mistake a partial tail for a complete journal.

High-frequency detail is obtained by arming a tracer or observer session,
not by widening the always-on audit ring until it becomes a performance or
memory tax.

### D5. Audit, logs, traces, and receipts remain separate planes

The existing split is retained:

- **Audit journal:** structured evidence of meaningful ownership and
  security transitions.
- **Diagnostic logs:** bounded human prose owned by each app.
- **Tracing/profiling:** high-frequency optional instrumentation, armed
  against one workload.
- **Crash receipts:** frozen post-mortem tails and tombstones.
- **Counters/snapshots:** point-in-time accounting such as `sys_procs` v2.

A crash receipt may embed the relevant audit tail, but the receipt is not
the journal. GOTOP/OBSERVE may read the journal, but they do not own its
write path. This prevents the unified model from collapsing the audiences
`docs/diagnostics.md` deliberately separated.

### D6. The audit system is itself accounted

The journal publishes its own overhead and integrity counters: events
written, coalesced, overwritten, dropped, bytes retained, export count,
and measured writer cost where the timer/counter is already available.
Audit access requires an explicit capability; the presence or absence of
that capability is itself auditable. The design fails if accounting the
system meaningfully slows the ordinary paths it exists to explain.

## Rejected alternatives

- **Audit every syscall.** That confuses activity with authority, imposes
  cost on all software regardless of risk, and produces mostly noise. The
  syscall tracer already exists for scoped investigation.
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
- **One merged diagnostics mega-surface.** The diagnostics census already
  rejected that shape: post-mortem, live-follow, and scoped-workload
  readers ask different questions at different costs.

## Rollout shape

1. Define the fixed event envelope, plane ids, object/verb/result enums,
   and host tests for encoding/loss accounting.
2. Add the kernel rings behind an audit capability, initially wired to
   existing denial/grant and process/window lifecycle decisions.
3. Add a batch reader surface in GOTOP or a small audit CLI before any
   persistent export.
4. Wire host-file, settings, clipboard/drag, snapshot/restore, and
   host-integration transitions in independent verifiable slices.
5. Only then consider a bug-report bundle that joins audit tail + log tail
   + crash receipt + posture snapshot.

Each slice must end in a gate that proves both evidence and bounded cost.
A slice that cannot name its overhead budget has not satisfied the design.

## Acceptance

This decision is accepted when the tree can answer, from structured
evidence rather than prose inference:

- which actor changed a named object;
- under which authority or grant;
- whether the action was allowed, refused, coalesced, or lost;
- what the action charged or consumed;
- what the audit machinery itself cost.

The answer must remain bounded under a noisy workload, and the loss
counters must make incompleteness visible instead of silent.
