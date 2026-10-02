# C3: serial offline Boris compiler closure, not a released guest port

The exact Boris pin is `08969742f85238443ce5cd1cd53ceab1b1f3f85a`.
Its own Oliver dependency stays at
`80d53b2118005b314d4c551d18a023f293eecc75`, not C1's revision.

Only `fetch` uses the network. It verifies tracked SHA-256 source inventories.
Every other command uses those inputs, the pinned A2 compiler and A3 std
materialization, and private compilation caches:

```sh
python3 tools/zig/boris/build.py fetch
python3 tools/zig/boris/build.py audit
python3 tools/zig/boris/build.py test
python3 tools/zig/boris/build.py upstream-test
python3 -m unittest discover -s tools/zig/boris -p 'test_*.py'
python3 tools/zig/boris/check.py
```

`audit` builds an **unguarded diagnostic candidate**, validates the static
ELF SDK profile, and records the compiler/library/source hashes and all
emitted frames. **Do not boot or ship this candidate.** `guest` refuses its
stack budget and does not emit `BORIS.BIN`. The guard strategy is inherited
from C1: check every emitted entry before its frame, reserve 8 KiB below a
120 KiB entry floor, and link only the guarded object without extra
compiler-rt. This is not a proven Boris release until the guard's frame
ceiling and the remaining native acceptance gates pass.

## Compiler subset

The existing `embed.compileBundle` calls Boris's actual IR pipeline,
`compileHtmlToSink`, Oliver, graph validation, include/wiki resolution,
layout assembly, memory assets, and target-local artifacts/checks/claims/
touches evidence. That HTML entry is explicitly sequential and rejects
`jobs != 1`; the normal `compileHtmlSiteMulti` worker branch is not called.
The compiler is not rewritten and Oliver is not replaced.

The guest build does **not** execute Boris's normal `build.zig` or import
its hosted `main`. Thus secp256k1's unconditional libc-enabled linkage,
signing/keys/auth, network publishing, preview/watch, editor hosting, and
child-process capture are absent from the selected executable graph.
The CLI policy refuses their commands and any jobs option explicitly.
Pure offline evidence must not be confused with publishing to a service.

The small `patches.json` only excludes filesystem branches from the memory
entry point. Each anchor is applied once against source hashes, in a private
Boris materialization. It does not change parser/render semantics, invent
metadata, implement a new filesystem, widen the allowlist, patch the shared
SDK, or use WASI/Linux. The current A3 std boundary supplies the native Io
types/vtable that Boris's upstream WASI embed build needed.

## Unresolved native acceptance

`build` refuses `FilesystemIdentityUnavailable:B3NotIntegrated` before any
filesystem operation. The landed B3 module is an isolated kernel core,
not an EL0/backend contract. No lexical check, pathname hash, zero inode,
host shim, or memory bundle substitutes for scanner containment.

Native traversal, B2/B3 long-name enumeration, B4 publication/republishing,
failure preservation and atomic-commit entropy are **not implemented or
claimed** here. The upstream destructive `Dir.commit(false)` and its
cross-device copy fallback are explicitly excluded, not retained as a
guest publication implementation.

The checked 24-source, 20-page corpus includes eighteen long colliding-prefix
names, nested parents, an include, wiki link, asset and ignored file.
Twenty-nine compiler artifacts are checked against small pinned hash vectors;
the two complete oracle bundles are byte-compared. These are compiler-only
proofs, **not B2/B3 traversal or site-republication proofs**.

The policy enforces the ADR input/output/path/tree ceilings and uses the
single 12 MiB SDK arena. The audited compiler currently exceeds the approved
128 KiB stack ceiling in one frame alone. No VM arena peak, handle cleanup,
entropy or native publication result is inferred from this class-A audit.
Measurements and frozen-ADR findings belong in the PR, not a status changelog.
