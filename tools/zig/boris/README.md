# C3: serial-offline Boris guest artifact

Boris is pinned at `08969742f85238443ce5cd1cd53ceab1b1f3f85a`, with its
own Oliver dependency `80d53b2118005b314d4c551d18a023f293eecc75`. Only
`fetch` uses the network. SHA-256 source inventories and patch anchors fail
closed; builds use private materializations and caches.

```sh
source tools/env-check.sh
python3 tools/zig/boris/build.py fetch
zig build boris-guest                 # zig-out/bin/BORIS.BIN
zig build boris-check
python3 tools/zig/boris/build.py patched-upstream-test
zig build test
bash tools/gate/vgate.sh tools/gate/specs/live-boris.spec
```

The shipping artifact is a static AArch64 VirelaiOS SDK ELF, not WASI,
Linux, or Boris's hosted executable. Its build selects the existing Zig
`embed.compileBundle` entry instead of the hosted `build.zig` that
unconditionally links libc-enabled secp256k1. Compiler/Oliver semantics
remain pinned. No C code, libc, signing library or replacement renderer is
linked. The closure, ELF check, input hashes and universal stack proof
accompany the binary as `BORIS.BIN.json` in the compiler work directory.
`audit` retains an **unguarded** candidate; do not boot it.

## Supported subset

```text
BORIS.BIN build /host/content /host/site
BORIS.BIN compile /host/content
BORIS.BIN inspect /host/content
BORIS.BIN probe
BORIS.BIN version
BORIS.BIN help
```

`build` requires existing, normalized, disjoint source and output directories
under `/host` on VirtioFS. It produces HTML and the compiler's IR,
manifest/graph/completion, report, assets and target-local evidence chain.
The actual IR pipeline, graph validation, include/wiki resolution, layout
assembly and Oliver run unchanged through the memory-source/sink seam.
The HTML entry is sequential and rejects `jobs != 1`; the parallel compiler
entry is not reachable. Hosted main, secp256k1, signing/authentication,
network publishing, watch/preview, editor hosting and child-process capture
are excluded. CLI requests for these surfaces or jobs options refuse
explicitly. The artifact's driver additionally refuses B6 networking.

`compile` emits the compiler's length-framed diagnostic bundle on stdout;
`inspect` reports the bounded native inventory. Neither publishes files.
This subset is not the whole Boris product.

## Source and publication contracts

Inputs must be **quiescent during a build**. B3 snapshots are immutable
directory membership/metadata, not a coherent all-files snapshot under
concurrent host mutation. Capture uses contained no-follow opens, compares
the actual handle's identity, size and mtime/ctime to the discovered row,
reads through EOF with an extra-byte limit probe, and revalidates the same
handle. Changed inputs refuse. Directory identities use `(filesystem,
inode)`, not inode alone or path hashes. Traversal counts ignored entries,
assets, includes and empty directories against the aggregate 256 limit;
one cursor is closed before entering the next directory.

Compilation and all output bounds complete **before mutation**. Publication:

1. Pin the existing output root. Obtain sixteen secure bytes from slot 72
   before the first mutation, handling short fills and refusing unavailable
   entropy. Exclusively create `.boris-stage-<32 hex digits>` under that pin.
   An occupied name refuses; it is never opened, truncated or removed.
2. Exclusively create each artifact under identity-checked staging pins,
   write it completely, file-sync and verify its actual handle's size.
3. Create missing output parents via pinned name operations and replace
   each artifact through one B5 contained rename. No delete/copy fallback.
4. Remove only the now-empty staging directories owned by this invocation,
   checking their identities. Unrelated prior output files remain untouched.

This honors the supported **per-entry** publication contract, not a site
transaction. Before the first rename, failure preserves prior artifacts.
Afterwards, earlier successful replacements remain. A submitted rename
failure may have an unknown outcome. The guest reports the retained stage
and completed replacement count; it does not claim rollback. Failed stages
remain for inspection, never recursive deletion through old paths. File sync
does not promise directory-fsync or power-loss durability. Output roots and
names must not be concurrently changed by another writer during publication.

## Bounds and evidence

The single arena is 12 MiB; total input is ≤1 MiB, each file ≤128 KiB,
output ≤2 MiB and ≤128 artifacts. Native paths/names keep B3/B5's existing
512/255-byte and depth bounds; staging adds one component and 46 bytes.
Three streams + borrowed cwd + four combined files/cursors/pins is the
eight-resource ceiling. The publisher walks parents with rolling pins,
not one retained resource per directory depth.

`workspace-patches.json` moves stable-sort scratch into that arena and
avoids large parser return temporaries without changing semantics.
`stack.py` inventories every emitted body and live non-tail call, rejects
unknown/dynamic SP changes, and guards prospective SP before any frame
allocation. Recursive/indirect calls therefore remain within 122,880 bytes,
below the unchanged 131,072-byte SDK stack budget. Entry and terminal
refusal are the only zero-stack exemptions. Guarded memory helpers are in
the same object; final linking disables implicit compiler-rt.
The complete image reserves `x18`; emitted use fails the build.

`boris-check` compares all 29 artifacts to the untouched pinned compiler
and rebuilds two byte-identical guarded no-libc artifacts in independent
caches. `live-boris` compiles/publishes the 24-file, 20-page nested fixture
with eighteen >31-byte colliding names, then verifies byte-identical
republish against the first publication and that independent oracle.
It also checks real short-entropy publication, entropy refusal without
mutation, failed compilation/publication preservation, source bounds,
symlinks, unsupported features, OOM, resource limits and post-reap cleanup.
Acceptance-only binaries are not installed apps. Build/check commands accept
`--cache PATH`; `BORIS_SDK_CACHE=PATH` selects the gate cache. Never delete
unexpected files from a compiler materialization to make its inventory pass.
Receipts, captures and exact kernel-size evidence stay under `artifacts/`.
`docs/status.md` and the generated gate inventory are intentionally untouched.
