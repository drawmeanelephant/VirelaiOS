# ADR 0042 — Init and service manifests

- Date: 2026-10-05
- Status: Proposed; owner approval in design sitting 1 is pending.
- Cards: M92 index #1985; #1986–#1991.

## 1. Format

Choose JSON v1, syntax-checked by Go's standard `encoding/json.Valid`
(Go license: BSD-3-Clause), then read by a bounded schema reader we own.
No third-party parser or dependency is added. TOML would
require owning a parser, including its escaping, arrays and type rules;
there is no in-tree TOML parser (observed). JSON supplies the syntax rules
and lets GOSET emit indented, human-readable objects with a schema writer.
GOSET need not preserve comments, which JSON intentionally does not allow.

The existing settings `key=value` and app pipe-row formats are not chosen:
dependency and argv arrays need an additional escaping grammar, nested
restart policy needs new conventions, and unknown keys must fail closed,
not be ignored as app-manifest tails are. This is a configuration format,
not a shell command language.

Observed throwaway builds use the same pinned Go 1.27.1 Virelai fork,
`GOOS=virelai GOARCH=arm64 CGO_ENABLED=0`, `-ldflags '-s -w'` and the
2,097,152-byte app ceiling:

| Probe pair | Baseline ELF | JSON ELF | Added bytes | Ceiling |
|---|---:|---:|---:|---|
| `fmt` + typed `json.Decoder` | 1,441,952 | 2,359,456 | 917,504 | Refused, 262,304 over |
| `println` + `json.Valid` | 1,179,808 | 1,704,096 | 524,288 | Fits, 393,056 spare |
| SDK file-read + complete `svcmanifest` parse/format probe | 1,245,344 | 1,704,096 | 458,752 | Fits, 393,056 spare |

A minimal typed `json.Unmarshal` also produces 2,359,456 bytes, even
without explicit `fmt`. Therefore choose the scanner plus owned schema
reader, **not** the reflect-based decoder. The reader handles strings,
arrays and objects after syntax validation, caps nesting, and admits only
the v1 fields. GOSET must use the bounded schema writer, not pull in the
oversized decoder/encoder closure. We own 173 physical lines in `json.go`
(bounded JSON value/string reader), 78 in `format.go` (schema writer), and
319 in `manifest.go` (public types, named refusals and schema validation).
Counts include blanks/comments. Standard `json.Valid` syntax scanning is
not copied or counted as owned parser code. The complete probe ran under
VZ: all 21 fixtures were accepted/refused by their expected names and the
three valid documents survived format/parse round trips (observed).
That does not prove the eventual INIT.ELF plus graph and supervisor fits.
No TOML implementation is adopted, so TOML bytes and owned lines are
unmeasured, not zero.

## 2. Location

Choose one bounded document, `/host/INIT/SERVICES.JSON`. GOSET publishes the
whole document through `vi.WriteFileSafe`; init polls it once per tick,
validates a complete replacement before applying anything, and retains the
last valid running configuration on reload refusal. No per-service setting
keys, directory discovery or new syscall is needed.

| Option | Implementation cost | Runtime cost / reload |
|---|---|---|
| ESP | New bootloader read, length/bounds in ADR 0002 handoff, kernel exposure to EL0, image staging and tests; EL0 cannot currently read it (observed). | Boot-only immutable snapshot; GOSET would still need a `/host` overlay and two-source precedence. |
| `/host/INIT/` | Existing file ABI plus bounded read, whole-document validation and crash-safe write; configs already use `/host` (observed). | One read per second by a windowless init; mutable, no handoff or image changes. |

## 3. Input contracts (published before implementation)

M92b defines these locally in `svcgraph`; M92c defines its policy locally in
`supervise`. Neither imports `svcmanifest`. M92e explicitly adapts the
validated manifest into these types. Seconds are monotonic whole seconds,
not nanoseconds or wall-clock epochs.

```go
// package svcgraph
const MaxNodes = 16
const MaxEdges = 64 // total requires + wants references, before optional drops

type Node struct {
    Name     string
    Requires []string
    Wants    []string
}
type Plan struct {
    Order         []string
    Prerequisites map[string][]string
}
type Diagnostic struct {
    From, To string
    Reason   string // "missing-want" or "cyclic-want"
}
func Resolve(nodes []Node) (Plan, []Diagnostic, error)

// package supervise
type Policy struct {
    Restart      string // "never", "always", "on-failure"
    BackoffBaseS uint64
    BackoffCapS  uint64
    MaxRestarts  uint32
    Window       uint64 // whole seconds
}
```

Graph order uses lexical names for ties. Required edges must resolve; a
missing want is diagnostic only. Admit wants in lexical `(From, To)` order,
dropping an optional edge if it would close a cycle, and report the edge.
Prerequisites contain retained required and optional edges, sorted by name.
Counts include duplicate references: duplicates are invalid, not free
edges. A service cannot repeat a dependency across requires and wants.
Enabled services alone enter the graph; a required disabled service is
missing, not silently enabled. Required cycles refuse boot by name.
These are algorithm bounds, not resident-process admission: the v1 manifest
is stricter at three records and six references (§4). The graph's independent
16/64 bounds allow M92b's required four-node diamond test and headless graph
tests without falsely granting more desktop task capacity.

For `never`, all four numeric policy fields are zero. For `always` and
`on-failure`: `1 <= BackoffBaseS <= BackoffCapS <= 3600`,
`1 <= MaxRestarts <= 32`, and `BackoffCapS <= Window <= 86400`.
Restart attempt 1 uses exponent 0, so its delay is base plus integer jitter
in `[0, base)`. Saturate doubling at cap before adding jitter; never shift
or multiply unchecked. `MaxRestarts` counts restart attempts in the
rolling `Window`; the next failure after that allowance is exhausted enters
`failed` without another spawn. A stable run lasting `Window` resets the
backoff streak. Supervisor stops are not failures, external status 137 is.
The counter starts at zero at supervisor startup, not at manifest reload;
unchanged services keep their counters. A failed spawn is a failure, not a
successful restart; it consumes an attempt when retrying.

Readiness in v1 means a successful exec pid present as running in the
process table on the next poll, not an application-specific serial marker
or proof of a usable window. Before first start, hold dependents until all
resolved prerequisites are running. If a prerequisite dies after a
dependent starts, keep that dependent running; hold subsequent dependent
starts/restarts until the prerequisite returns. A completed `never`
one-shot with status 0 remains satisfied. Failed/disabled required
prerequisites do not become satisfied by disappearance.

## 4. Document and fields

Top-level JSON object: `{"version":1,"services":[...]}`. The document is
at most 8192 bytes, with 1–3 service records and at most 6 total dependency
references. Unknown keys, duplicate JSON object keys, null fields,
unsupported versions, trailing documents and malformed JSON refuse the
whole document. No partial parse or partial activation.
Reader nesting is at most eight levels; raw UTF-8 and escaped surrogate
pairs must encode Unicode scalar values. Non-scalar escapes refuse rather
than silently replacing bytes in configuration.

Each service requires `name`, `argv`, `restart` and `class`. `name` is the
receipt label: `[A-Za-z0-9._-]{1,28}`, excluding `.` and `..`, unique and
case-sensitive. `argv` is the complete argument vector: element 0 is the
share-root binary basename, ending in `.ELF` or `.BIN`. There are 1–8
elements, each at most 255 UTF-8 bytes with no NUL; init calls
`vi.Exec(argv[0], argv[1:]...)`. The kernel adds argv[0], so passing eight
extra arguments would overflow even though `vi.Exec` accepts eight
(observed source).

`requires`, `wants` and `caps` default to empty arrays. Dependency labels
obey the name grammar. `restart` is an object with `restart`,
`backoff_base_s`, `backoff_cap_s`, `max_restarts` and `window` matching §3;
the four numeric keys default to zero for `never` and are required positive
for restarting policies. `class` is `boot` or `on-demand`. `enabled`
defaults to true; GOSET persists it in this same document. Init starts
enabled services of either class; on-demand means live-toggleable, not
socket activation.

`caps` uses `file window audio timer memory process debug net term`, without
duplicates. It is declarative only: slot 28 preserves the creator's uid and
caps and cannot drop either (observed). It grants no authority. `env` is a
v1 non-goal and is refused as an unknown key. The existing monitor envp
path can seed init itself, but `vi.Exec` cannot pass child environments.

The host/guest API is `Parse([]byte) (Manifest, error)`,
`Validate(Manifest) error`, and `Format(Manifest) ([]byte, error)`.
`Format` writes explicit enabled state and stable indented field order,
preserves decoded argv bytes, and refuses an output over 8192 bytes.
Consumers use `*svcmanifest.Error.Code` for named refusal, not a
substring of its message. Errors expose field names, not config contents.
The 18 refusal classes each have a negative fixture; three valid fixtures
cover seat, completed one-shot, and restarting/missing-optional scenarios.
Missing required nodes and required cycles are `svcgraph` refusals after
schema validation, never an accidental second graph implementation here.

## 5. Task budget

The kernel limit remains 16. Source records primary + sysmon + template as
three tasks at `GOMAXPROCS=1`, and four at the default 2 (observed comments
and runtime setup; the M92 cold-boot footprint is still inferred).
M92e seeds **init only** with `GOMAXPROCS=1` before runtime startup via the
existing kernel `exec.set_envp` path. Calling `runtime.GOMAXPROCS(1)` later
does not prove an already-created M has disappeared. Child env propagation
is not assumed.

| Resident | Tasks |
|---|---:|
| Kernel monitor + worker + idle | 3 |
| Init, startup GOMAXPROCS=1 | 3 |
| GOTABWM, unchanged default GOMAXPROCS=2 | 4 |
| Its GOSH, unchanged default GOMAXPROCS=2 | 4 |
| N additional native, single-task services | N |
| Total | 14 + N ≤ 16 |

Thus **N ≤ 2 single-task native services; N = 0 additional default Go
services** on the seated desktop. The schema cap is 3 (seat + two services);
GOSH remains seat-owned, never duplicated by init. This is not a promise
that any arbitrary three binaries fit. Init must conservatively reserve
the seat's shell and admit `.ELF` at four tasks, `.BIN` at one only for
known single-task fixtures; refuse activation as `task-budget` before
spawn otherwise. Disabled services consume no tasks. A no-seat gate can
exercise Go supervisor/child fixtures within its separate budget. Dynamic
desktop clients share the same finite pool, so a spawn refusal is handled
honestly, never by raising `max_tasks`.

## 6. Enable key, fallback and reload

M92e alone adds the boolean `init` settings key, compiled default on at
that card's boot flip. `init=off` retains today's `wm` autostart unchanged.
Keep `wm=gotabwm|tabwm|none` as the reachable fallback, including the
explicit shim-only `none` used by gates. This ADR/card does not flip boot.

Missing, oversized, invalid, required-cyclic, budget-invalid or unstartable
boot configuration prints `init: refuse <named-reason>` and falls back to
the configured direct `wm` path. Init must relinquish tasks before the
monitor starts fallback; never start two seats. Failure of init itself
before a live seat also returns ownership to the monitor. M92e proves this
handshake, rather than inferring success from an exec pid.

On reload, refuse an invalid replacement and retain the last valid set.
Persist boot-class changes but name them as applying next boot; only
on-demand changes apply live. Stop dependents before prerequisites for
explicit disable; unchanged service identity/policy retains counters.
No reboot or settings pub/sub window is required.

## 7. Receipts

On each failure call `vi.WriteCrashReceipt(service.Name, outcome)` with
`restart=<k>/<max> status=<code> backoff_s=<d>`. `k` is the number of
restart attempts consumed in the current rolling window, not lifetime
process launches; the initial launch is 0. The fifth scheduled retry is
5, and exhaustion keeps that value without pretending a sixth was started.
`d` is the chosen jittered delay, or zero when no retry is scheduled.
`never` failures use `restart=0/0`. Record failure of the write on serial.

Retention is latest-only per service name, not a receipt per attempt.
Both logs and receipt discovery have a 16-entry directory window
(observed); at most three stable service labels consume it. Renaming
services must not generate unbounded labels: do not rotate names on
restart, do not prune other apps' files, and report directory/write errors
honestly. This does not reserve all 16 labels for init.

## 8. Card split and acceptance

Final coordination Touches, with no recursive globs:

| Card | Touches |
|---|---|
| M92a #1986 | `docs/decisions/0042-init-and-services.md, user/go/svcmanifest/*, tests/fixtures/init/manifests/*` |
| M92b #1987 | `user/go/svcgraph/*` |
| M92c #1988 | `user/go/supervise/*, user/go/svfixture/*, tools/go/build-svfixture.sh, tools/gate/specs/live-supervise.spec` |
| M92e #1989 | `user/go/init/*, tools/go/build-init.sh, kernel/src/shell.zig, kernel/src/settings.zig, kernel/tests/shell_test.zig, user/go/settings/*, tests/fixtures/init/boot/*, tools/gate/specs/live-boot-order.spec` |
| M92d #1990 | `user/go/goset/*, user/go/init/*, tools/gate/specs/live-supervise.spec` |
| M92f #1991 | `tools/gate/specs/live-boot-order.spec` |

M92a/b/c run concurrently with disjoint files. M92e waits for all three
merges. M92d waits for M92a/c/e; it does not use per-service settings keys.
M92f waits for M92c/e and #1978's merged scheduler repair. M92d and M92f
are disjoint. No shared staging-tool changes are hidden in these lists.

Acceptance belongs to the cards: manifest fixture refusals and host
regression (a); deterministic graph tests and five-minute fuzz (b);
policy tests plus `live-supervise` (c); GOSET live/deferred/corrupt reload
runs (d); declarative cold boot, fallback and seat regressions (e);
mid-boot kill/order/receipt runs on main (f). The owner approves design
here and acceptance on #1985; agents never infer a human verdict.

### Design sitting 1 prompts

**Format:** JSON, standard syntax scanner plus a bounded owned schema reader.
Approve JSON and its measured app-size cost, or state the objection?

**Location:** one crash-safe `/host/INIT/SERVICES.JSON`, polling reload.
Approve `/host` rather than an immutable ESP plus mutable overlay?

**Budget:** 14 baseline tasks, at most two single-task native services.
Approve this bounded v1, with no extra default Go daemon on the desktop?
