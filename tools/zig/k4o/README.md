# k4o native render CLI (C2)

The engine, parser, filters and format bytes come unchanged from k4o
`59f88233589d2643ba5b4f380e3db70664bba2b3`. `lock.json` pins its archive
and Git tree. The guest adapter owns CLI policy, sequential file intake,
allocation and diagnostics. It uses A2/A3's pinned compiler/stdlib and
B1's process streams, not libc, POSIX, Linux, threads, network or entropy.

```sh
python3 tools/zig/k4o/build.py fetch   # explicit network provisioning only
python3 tools/zig/k4o/check.py check   # offline; two fresh-cache byte comparisons
bash tools/gate/vgate.sh tools/gate/specs/live-k4o.spec
```

`check` builds `.build/k4o/guest/K4O.BIN`, the **unmodified host CLI**
oracle, the non-shipping acceptance launcher, and an instrumented CLI.
Stage `K4O.BIN` onto the guest share to use it. No boot/default/app-manifest
change is made. The declarative gate launches and waits for each invocation
with independently redirected stdout/stderr. It compares every supported
corpus output with host-engine bytes (and pinned golden files when supplied),
checks diagnostic isolation and resource refusals, and checks native
post-reap page recovery. The gate never fetches or silently provisions inputs.
It uses 18 boots of at most 12 sequential children: a single long boot
observed a launcher refusal after sixteen completed invocations despite prior
waits. The native cause is uncharacterised, not repaired by this guest card.

## Supported invocation and limits

```text
k4o render template --data=data.json --format=gfm --max-output=1m
k4o --help
k4o --version
```

Textile is the default; CommonMark (`markdown`) and GFM keep the upstream
format semantics. `--data`/`-d`, `--format`, `--max-output`/`-m` accept
both separate and `=` values; sizes accept case-insensitive `k`/`m`/`g`.
Data is optional and otherwise an empty JSON object. Help/version aliases
are `-h`/`-v`. Unknown commands/options, duplicate data/format options and
missing/invalid values refuse explicitly.

**Frozen ADR 0038, not the older host defaults, governs this port:**

| Resource | Guest ceiling |
|---|---|
| Template and JSON | 131,072 bytes **each**, plus a one-byte EOF probe |
| Rendered output | 1,048,576 bytes; `--max-output=0` or larger values refuse |
| Aggregate populated arena | One 8 MiB mapping, including input, AST, output, std state and allocator overhead |
| Stack | 128 KiB budget within the existing 192 KiB mapping |
| Arguments | Seven user arguments, 255 bytes each; use `=` forms to fit |
| Files | At most one open input at a time, within the two-file allowance; three B1 streams stay distinct |
| Paths | SDK `/host` cwd; 64 bytes/full path, 31 bytes/component; no clipping or lexical escapes |
| Diagnostics | 16 KiB including one reserved `DiagnosticLimit` message |

The issue's 16 MiB inputs/256 MiB output values conflict with the frozen
ADR. They are deliberately **not** imported. Within-limit inputs can still
exhaust the arena and return `OutOfMemory`.

An iterative, quote-aware preflight limits template blocks and JSON
containers to eight levels. It also limits condition complexity to eight
(conservatively counting unquoted parentheses, boolean operators and
negations, including those substrings in identifiers). This prevents the
pinned parser's unbounded unary recursion and the evaluator's left-skew
boolean trees. Over-budget syntax returns `TemplateDepthLimit`,
`JsonDepthLimit` or `ConditionLimit` before recursive engine work.
No engine code or output rules change.

The build receipt records compiler/archive/overlay/source/artifact hashes
and a conservative compiler-frame bound. `stack.py` charges every emitted
function once, then charges each approved data-recursive call group eight
extra times. The pinned allocator/Io callbacks do not re-enter the engine;
panic is a terminal allocation-free stderr path. New direct recursion or
unreviewed SP adjustments refuse the build. The instrumented acceptance
binary additionally measures stack high-water, arena peak and peak input
handles. Measurement is supplementary, not a proof for arbitrary recursion.

Rendering and all budget checks finish before stdout starts. Input/template
failures leave stdout empty; write failures propagate nonzero but cannot
undo a prefix already confirmed by the destination. Writes are synchronous,
chunked and unbuffered, so there is no ignored application flush. Normal exit
checks file/stream closes; process teardown reclaims the single arena.
The native SDK cannot expose backend close failures that slot 26 itself
discards (ADR 0038 §9). No stronger durability claim is made.

The workload-local driver binds SDK file/refusal diagnostics to B1 fd 2.
It does not use A3's pre-B1 console-only diagnostic hook or pretend that
A3's unbound `File.stdout()` token works. No shared SDK or kernel files
are changed.

Compiler/source materializations are private temporary trees checked byte
for byte. This avoids observed `.DS_Store` additions in desktop-watched
worktree directories without weakening A2's additional-file refusal.
Only pinned download archives persist in the cache.
