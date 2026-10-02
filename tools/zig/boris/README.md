# C3: stack-safe serial offline Boris compiler probe, native port pending

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
```

`guest` emits a guarded **compiler probe**, not an accepted native site
compiler. `audit` retains an unguarded diagnostic candidate: do not boot it.
Both validate the static ELF SDK profile and record pinned inputs.
Only the guarded object is linked, with implicit compiler-rt disabled.
The three required memory helpers are inside that same guarded object.

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

## Unresolved native acceptance

B3's live kernel/backend/EL0 contract landed in `4ee1d281`. The separately
owned shared SDK filesystem bridge is still required. `build` currently
refuses `SharedSdkFilesystemBridgeUnavailable` before filesystem work.
No lexical check, pathname hash, zero inode, host shim or memory bundle
substitutes for scanner containment.

The checked 24-source, 20-page corpus has eighteen long colliding-prefix
names, nested parents, an include, wiki link, asset and ignored file.
These are compiler proofs, **not native B2/B3 discovery or republishing**.
B4 publication, failure preservation, slot-72 entropy and native cleanup
are not implemented or claimed. Destructive `Dir.commit(false)` and
cross-device overwrite-copy fallback remain excluded.

Policy enforces ADR input/output/path/tree ceilings and the single 12 MiB
arena. No VM arena peak, stack high-water, handle cleanup, entropy or
native publication result is inferred from class-A evidence. Measurements
and remaining blockers belong in draft PR #1892; #1877 remains open.
