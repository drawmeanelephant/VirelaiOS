# QJS.BIN product

ADR 0039 file/serial adapter. No root build step, installed command, menu
entry, network acquisition or guest libc. Consume the SDK and M88b read-only.
The pinned compiler archive must already be present in the SDK cache.

```sh
python3 -B tools/js-client/build.py build
python3 -B tools/js-client/build.py verify --work .build/js-client/verification
python3 -B -m unittest discover -s tools/js-client -p 'test_*.py'
just gate live-quickjs
```

`verify` requires a fresh directory and compares two complete clean links,
the identical empty-runtime adapter baseline, size deltas and the actual
kernel ELF parser. Builds also inspect C, Zig and linked compiler-rt frames.
Retain QuickJS's MIT notice from `user/zig/quickjs/upstream/LICENSE` with
binary distributions.

Stage QJS.BIN explicitly, then `exec QJS.BIN /host/FILE.js
--receipt=/host/OUT`, or `exec QJS.BIN --repl --receipt=/host/OUT`.
Receipts require contained exclusive creation, never replacement.
The serial line loop supports `:reset`, `:quit`, empty-line Ctrl-D and
Ctrl-C; a zero-byte tty read means no pending input, not EOF.

## Independent evidence

`live-quickjs` compares guest stdout and byte-framed receipts against
Node.js 26.10.0 / pinned V8 goldens. The refused-feature golden's host
context explicitly removes the ADR's denied globals. No host engine runs
in place of guest evaluation. Marker-driven inputs, real monotonic
deadlines, five accepted cold launches, painted stack high-water, kernel
`procs receipt QJS.BIN` and before/after `pages` are separate observations.
The gate fails on a missing marker or page mismatch. A harness timeout is
not a refusal. Evidence stays in ignored `artifacts/m88-acceptance/`.

The native bounds fixture labels direct policy checks separately from
evals. Some arithmetic ceilings cannot be reached through the smaller
product limits: 256 lines of at most 4096 bytes cannot consume 4 MiB of
session source. These counters are checked directly, not presented as
oversized user input successfully delivered through a narrower ABI.

## Receipt framing

Each frame is `K LENGTH` followed by LF and exactly LENGTH bytes. K is H
(version), O (confirmed logical output), D (diagnostic), R (eval fields),
T (post-diagnostic completion/cancel counters), or Z (session fields).
Output includes arbitrary bytes, including NUL.
`receipt.py` is an independent strict length reader, not a line grep.
R includes actual counter/frequency identity, heap/stack/handle peaks,
failure and context-reset state. Console and receipt copies count once
against the eval-output budget. Session status precedes sync/close;
the native process exit remains authoritative for those final failures.
