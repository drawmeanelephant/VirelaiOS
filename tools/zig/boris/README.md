# C3: guarded native Boris input integration, publication blocked

Boris stays at `08969742f85238443ce5cd1cd53ceab1b1f3f85a`, with its own
unchanged Oliver dependency `80d53b2118005b314d4c551d18a023f293eecc75`,
not C1's revision. Only `fetch` uses the network. Source SHA-256 inventories
and patch anchors fail closed; builds use private materializations/caches.

```sh
python3 tools/zig/boris/build.py fetch
python3 tools/zig/boris/build.py guest
python3 tools/zig/boris/build.py patched-test
python3 tools/zig/boris/build.py patched-upstream-test
python3 -m unittest discover -s tools/zig/boris -p 'test_*.py'
python3 tools/zig/boris/check.py
bash tools/gate/vgate.sh tools/gate/specs/live-boris.spec
```

`guest` emits a guarded **native input/compiler diagnostic**, not an accepted native site
compiler. `audit` retains an unguarded diagnostic candidate: do not boot it.
Both validate the static ELF SDK profile and record pinned inputs.
Only the guarded object is linked, with implicit compiler-rt disabled.
The three required memory helpers are inside that same guarded object.
Build/check commands accept `--cache PATH`; `BORIS_SDK_CACHE=PATH` selects
the live gate's cache. Use an isolated cache if Finder writes `.DS_Store`
into the default materialization. Additional files still fail closed; never
delete user-created files or loosen the archive inventory to make it pass.
`guest-oom`, `guest-resources` and `gate` are acceptance-only artifacts,
not installed apps.
The former uses a 4 KiB arena to exercise allocation refusal; the shipping
diagnostic retains the single 12 MiB arena.
The resource fixture fills all four combined file/cursor slots, verifies the
eight-resource ceiling including streams/cwd, then checks refusal and cleanup.

The image and launcher reserve platform register `x18` with
`baseline+reserve_x18`, without adding an ISA feature. The native exception
frame deliberately omits it. The first integrated VZ run exposed truncated
bundles when LLVM kept live loop bounds in that register across SVC.
The builder now rejects any emitted `x18`/`w18` instruction. No shared
SDK/kernel or native register-preservation guarantee was changed.

## Stack bound, independent of fixture size

Boris's stable block sorts formerly allocated large fixed arrays on the
stack. `workspace-patches.json` selects an iterative, stable merge sort with
scratch allocated from the existing guest arena and freed after each sort.
Equal keys retain input order. Allocation failure falls back to stable
insertion sort, never an unaccounted allocator. The parser keeps its public
return-value API, but failure sites write the caller's result directly,
avoiding repeated large return temporaries. Grammar, metadata, diagnostics
and Oliver code stay intact.

`stack.py` inventories every emitted body, resolves aliases and direct call
targets, rejects unknown/dynamic SP changes and backward control-flow edges
crossing frame allocations, and verifies live stack at non-tail calls.
Every non-exempt entry checks **prospective** SP (current SP minus its whole
frame) against initial SP minus 122,880 bytes **before allocating anything**.
Thus, inductively, every activation, including recursive and indirect
calls, remains within 122,880 bytes, below the unchanged 131,072-byte ADR
budget. Too-deep calls refuse through a naked, zero-stack terminal path.
Only the zero-stack entry trampoline and terminal refusal are exempt.

The current image's largest frame is 30,144 bytes; its smallest nonzero
frame and live non-tail call charge are 16 bytes. Consequently at most
7,680 stack-consuming/non-tail callers can be active, plus a zero-frame
leaf. Tail calls do not add non-tail depth. This bounds every input,
including rejected input, not just measured fixtures. `stack-proof.json`
records the full call inventory and numeric bounds.

Guard tests check aligned admission boundaries, recursive/indirect calls,
repeated allocations, unknown call targets and memory-helper aliases.
Compiler tests exercise real includes at depth 32 and refusal at depth 33.
`check.py` compares the patched and untouched compiler's entire 29-artifact
bundle byte-for-byte and builds two identical guarded no-libc probes with
independent compilation caches. `patched-upstream-test` checks the
materialized compiler, not only unchanged upstream.

## Compiler subset

The existing `embed.compileBundle` calls Boris's actual IR pipeline,
`compileHtmlToSink`, Oliver, graph validation, include/wiki resolution,
layout assembly, memory assets and target-local artifacts/checks/claims/
touches evidence. This HTML entry is explicitly sequential and rejects
`jobs != 1`; `compileHtmlSiteMulti` and its `Thread.spawn` branch are absent.
The compiler is not rewritten and Oliver is not replaced.

The normal hosted build/main, secp256k1, signing/authentication, network
publishing, preview/watch, editor hosting and child-process capture are
excluded. CLI policy refuses these features and jobs options explicitly.
`patches.json` excludes unavailable filesystem branches from the memory
entry, not parser/render semantics. No shared SDK, std, dependency cache,
allowlist, kernel, boot default, WASI or Linux changes are made.

## Native input diagnostic

PR #1892's compiler/stack work and PR #1901's shared SDK bridge are merged.
The Boris driver explicitly opts into `filesystem_b2`. Commands:

- `inspect ROOT`: native rich snapshot traversal, including empty directories,
  ignored entries, includes and asset trees. Directory cycle detection uses
  `(filesystem, inode)`, never inode alone or a pathname hash.
- `compile ROOT`: the same discovery, then B3 contained existing-file opens
  and sequential EOF reads with an extra-byte limit probe. The captured inputs
  enter the unchanged pinned compiler. All 29 artifacts are emitted as a
  length-framed **diagnostic bundle on stdout**, not installed as a site.
- `probe`: the retained compiler-only in-memory fixture.
- `build ROOT OUT`: refuses `NativePublicationUnavailable` before allocation,
  filesystem work, entropy or output mutation.

One cursor is materialized/closed before another directory is entered.
All traversal state, input and output use the same arena. Three streams plus
borrowed cwd plus one cursor/file peak at five SDK resources; the shared
backend still refuses a fifth combined file/cursor record (eight total).
Visited directories and ignored entries count toward the aggregate 256 limit,
including empty directories not represented in the compiler's file list.
Native relative paths use B3's eight-component ceiling, not the memory-only
fixture's implicit-directory counting as a replacement for native limits.

`live-boris` boots the guarded image on real VirtioFS/VZ and independently
compares every compiler artifact to the untouched pinned host compiler. Its
24-file fixture has 20 pages, 29 visited entries, eighteen >31-byte colliding
names, nested parents, include/wiki resolution and an asset. It compiles twice;
this is **not two publications**. The gate also checks boundary refusals,
symlinks, OOM, prior-site/input preservation and post-reap resources. The two
256-entry cases boot separately: B3's shared mount-lifetime 512-identity
ledger is not a per-process resource and closing cursors cannot clear it.
Its exact two-page retained charge is distinguished from leaked guest pages.
All receipts/captures stay under `artifacts/boris-native/`.

## Native audit and required amendment

This table identifies required operations, not approved new semantics.
Shared edits require coordinated ownership and ADR 0007 approval first.
Frozen ADR 0038 and Boris's compiler/publication contracts are untouched.

| Blocked operation | Why the bridge is insufficient | Smallest necessary native addition |
|---|---|---|
| Bind source reads to discovered identity and obtain size/fresh attributes from the actual open object | Fresh path metadata is not fd-stat. Snapshot rows and a later contained open are individually safe but can name different objects after replacement. Boris `source_io.readPageAlloc` requires `file.stat`. | Fallible metadata on a live pinned file handle, with `(filesystem,inode)`; compare the actual open identity to the discovered identity and revalidate after reading. Never query the old path. |
| Keep traversal/publication anchored to the originally supplied directory | Each current B3 operation resolves its supplied root afresh; a fresh directory-identity query before opening a snapshot is not an atomic expected-identity check. | A bounded no-follow root/directory token (charged to the existing eight records), usable by snapshot/open and contained mutation; expected-identity validation before use. |
| Create a new entropy-named staging file without truncating an existing entry or escaping a replaced parent | Legacy create is path-based/truncating; random names do not make creation exclusive. | Exclusive existing-parent-relative create under the pinned root, with `PathAlreadyExists` and no mutation on refusal. |
| Create nested staging directories, clean only owned staging entries, and publish to the validated parent | No contained mkdir/delete or pinned-parent rename exists. B4 is explicitly path-based. | Bounded mkdir/unlink and replace/preserve-existing rename using pinned parent tokens, same-share only, both-end authorization, no copy/delete fallback. |

No generic std stat, stat-then-open, old-path fd-stat, random-name-only
staging, destructive `Dir.commit(false)`, cross-device copy or rollback
emulation fills these gaps. The diagnostic deliberately does **not** claim
snapshot/open identity continuity or a coherent tree under replacement.
`TreeChanged` detects some directory replacements by fresh path identity,
but that check is supplementary, not a substitute for the missing token.

After approved additions, publication must obtain slot-72 entropy before the
first mutation, exclusively create/write/sync the owned stage, and commit
through contained rename. Short fills, errors and unavailable entropy must
be tested through that real naming/publication path, not an isolated RNG call.
Until then those acceptance steps, second publication, injected publication
failure and native file-identity replacement checks are **blocked**.

B4 promises per-entry replacement preservation, not a multi-file transaction.
An eventual serial publisher must document earlier successful replacements
and unknown transport-loss outcomes honestly; it cannot claim batch rollback.
If acceptance requires an all-files atomic site switch, that additionally
needs a native generation-switch contract, not copy/delete restoration.
#1877 remains open; local/native evidence and exact remaining acceptance live
on that card and the focused follow-up PR, not in a duplicated status log.
