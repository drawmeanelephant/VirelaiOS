# Oliver native proof and bounded CLI

C1 (#1875) refreshes, rather than replaces, the #1177/#1188 history.
The frozen ADR 0038 pin is
`3615e6253f0e17b410cf1b987507d30bfcde537c`, recorded with every original
source hash and adapter edit in `tools/zig/oliver.lock.json`.
`md-fixture.txt` and the 754-byte `expect.html` remain unchanged: the
independent pinned host CLI must freshly reproduce those exact bytes.

## Build and verification

```sh
python3 tools/zig/oliver.py fetch       # explicit network step, including A2 compiler
python3 tools/zig/oliver.py build       # offline; .build/oliver-build/OLIVER.BIN
python3 tools/zig/oliver.py test
python3 tools/zig/oliver_test.py
bash tools/gate/vgate.sh tools/gate/specs/live-oliver.spec
```

`--cache PATH` selects a private SDK cache; `ZIG_GUEST_CACHE=PATH` selects
it for the live gate. Do not place that cache in a Finder-watched directory:
unexpected `.DS_Store` files correctly trigger A2's materializer drift check.
Builds never fetch. Original source drift or missing inputs fail closed.
The native artifact is an ELF despite its `.BIN` filename, with RX text,
a full-page gap, and RW startup state. The SDK checker charges the rounded
argv/env tail. No libc, Linux ABI, installed-stdlib edits or kernel changes.
Old tracked images and the eight-32-byte-slot wrapper are retired; the gate
builds fresh images and keeps evidence only under `artifacts/`.

## What is supported

| Surface | Boundary |
|---|---|
| **CLI:** `render --from markdown\|textile\|cooklang` | HTML/XHTML/HTML4-strict, upstream render options and Markdown extensions; input from the real B1 stdin binding, normal bytes on stdout, `--diagnostics json` on stderr |
| **CLI:** `meta --from markdown\|textile\|cooklang --format json` | Pinned upstream seven-string YAML projection, pure JSON stdout |
| **Library-only:** `library INPUT OUTPUT` | Historical real `parse` + `html.render` proof; relative `/host` paths or absolute `/host/…`, conservative 64-byte paths / 31-byte components |
| **Deferred:** `wrap`, `plan`, `manifest`, `serialize`, `scale`, `menu` | `UnsupportedCommand` before input reads or file work; not contemporary CLI coverage |

Arguments include the program name at slot zero, with eight 256-byte slots.
An owned native launcher must provide B1 redirection using ADR 0007's
versioned slot-28 stream request. The kernel monitor does not provide stdin
redirection, and this card does not add shell integration or claim general
concurrent pipes. `oliver_probe.zig` is a test-only sequential launcher, not
a new shipped workload.

The library proof deliberately does not claim the CLI's link rewriting.
The host-side `tools/oliver-publish.sh` still emits the historical positional
commands; updating that host workflow is outside this guest-only card.
The CLI retains the pinned upstream argument parser, parse options, rewrite
pass, diagnostics serializer, metadata extractor and renderers. Adapter edits
only expose pure helpers and exclude C ABI exports from the guest graph.
The hosted `main` is not imported as a guest entry; ignored hosted flush
errors and unbounded hosted stdin buffering are not carried over.

## Resource and refusal policy

- One populated 8 MiB SDK arena charges buffers, allocator metadata, SDK
  state and parser/render allocations. Reusable frees stay within that arena.
- Input is at most 131,072 bytes, with a real extra-byte EOF probe.
  131,073 refuses `InputLimit`. Normal output is at most 524,288 bytes,
  buffered before any output write; the next byte refuses `OutputLimit`.
- Diagnostics have a 16 KiB aggregate cap and reserved refusal message.
  All panic, error and file-helper diagnostics use stderr without fallback.
  Confirmed short writes loop; zero progress, native failure, sync or close
  failure exits nonzero. Success is zero, not the output length.
- Grammar recursion is conservatively bounded at 32: Markdown counts even
  literal `[` tokens before parsing; YAML frontmatter limits indentation;
  TOML frontmatter counts structural tokens. The parsed document/recipe is
  checked iteratively before recursive rendering. Some otherwise-valid
  within-byte-limit inputs therefore refuse `DepthLimit`.
- The pinned assembly builder guards every emitted function entry before
  its frame. Entry SP may descend at most 120 KiB; every compiler frame must
  fit the reserved 8 KiB. Unknown label formats or oversized frames block
  the build. A stack-limit refusal writes an allocation-free stderr message
  and exits before the kernel guard page. This is an enforceable stack
  bound, not an inference from a small example.
  The five required memory/u128 compiler helpers are workload-owned and
  compiled through the same guards; the final link disables automatic
  compiler-runtime linkage so new support symbols fail closed.
- At most three stream identities and one explicit file are live at once.
  Gate-only instrumentation records actual arena/stack high-water and
  file-resource peak after each successful invocation. `--probe` enables
  that receipt and the panic/stack refusal probes; normal builds omit them.

The existing `live-oliver` spec compares both streams independently to the
fresh pinned host CLI, checks semantic/resource refusals publish no normal
bytes, preserves a prior library destination on path refusal, and checks
post-reap page recovery. Its separate flat fixtures prove exactly 2,048 bytes
of argv slack, one alignment step too little, next-page acceptance and the
same refused image without arguments. They are **loader probes**, not
Oliver CLI or library coverage.
