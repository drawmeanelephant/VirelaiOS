# Private native QuickJS runtime

Implements ADR 0039 §§2–6 for M88b. This is not `QJS.BIN`, a shell, a
browser, or guest acceptance. M88c owns file/tty handling, receipts and
real-counter guest timing/cleanup proofs. The original MIT license is
`upstream/LICENSE`; retain it with binary distributions.

## Offline build and checks

The pinned SDK compiler archive must already exist. Only the SDK's explicit
`python3 tools/zig/sdk.py fetch` maintenance command acquires that archive.
No runtime build downloads anything.

```sh
python3 -B tools/quickjs-runtime/build.py objects --work .build/quickjs/objects
python3 -B tools/quickjs-runtime/build.py native --work .build/quickjs/native
python3 -B tools/quickjs-runtime/check.py all --work .build/quickjs/verification-1
```

Use a new verification directory for each complete run. The complete check
runs private bridge/source tests, hosted runtime contracts, fatal invariant
probes, two clean native links, the identical empty-runtime baseline,
all 134 refused-symbol build negatives and the current kernel ELF parser.
It records source/recipe hashes, exact remaining C external names, safety
coverage, frames and size deltas. Receipts/build output are ignored artifacts,
not committed evidence. Hosted fixtures use libc only for their host runner;
engine storage still uses the actual SDK arena. Native artifacts use none.

`tools/quickjs-runtime/build.py` exposes `objects(work, cache)` for the product
builder. It returns the checked C-object directory; link those objects with
the pinned SDK's ReleaseSafe, single-threaded, `reserve_x18`, gap-ELF recipe.
Use `prepare_runtime(cache, work)` to get the compiler, private library and
provenance; pass its library path as `--zig-lib-dir`. Do not link the
unmodified SDK compiler-rt, which lacks the reduction polls.
Import `runtime.zig` as `qjs`, supply an `arena` module exporting the SDK's
**same** `memory.Arena` type, and a `hooks` module with allocation-free
`invariant() noreturn` that emits `EngineInvariant` and exits 71. The native
fixture and `arena_alias.zig` show module wiring without changing the SDK.

## Runtime ownership

```zig
const runtime = try qjs.Runtime.create(sdk.currentArena(), try qjs.nativeClock());
defer runtime.destroy() catch |err| sdk.fail(@errorName(err), 70);
const result = try runtime.eval(source, filename, sink, emit_result);
if (result.failure) |failure| {
    // Non-success receipt/exit. Announce ContextReset when context_reset.
}
```

`create`, `eval`, `cancel`, `reset`, `destroy` allow one runtime and one active
eval. `eval` copies validated UTF-8 source plus NUL into charged storage.
Ordinary `JSException` preserves globals; resource/interrupt/unsupported
failures recreate the raw context. Failed recreation returns an error.
`Result` records logical output, charged peak, actual charged
`js_poll_interrupts` sites, largest observed poll gap and C-stack high-water.
The caller still proves the full SDK stack high-water and guest timing.

`Sink.write` confirms progress, receives at most 256 bytes, and may short-write.
Zero/invalid progress or an error refuses the run. `Sink.diagnostic` receives
at most 256 exception bytes. `Sink.poll` may call `cancel`; preserve other tty
bytes in a charged adapter buffer. Callbacks must not re-enter an engine
boundary, destroy/reset the runtime, block indefinitely or allocate directly
from the SDK.

Initialize/freeze SDK allocation state before `create`, leaving at most five
free-list nodes. While the runtime lives, use `allocate`/`release` for adapter
buffers. They survive context reset; release them before destroy. File/tty/
receipt handles stay in the adapter. Use `checkResources`, `chargeReceipt`
and `control.checkLine`/`checkIdle` for the shared numeric limits. The adapter
must enforce contained paths, argv/env, extra-byte EOF probes, handle lifecycle,
receipt exclusivity and I/O completion. Console/receipt copies count once.

## Safety boundary

The pinned preprocessor expands each enabled core unit. A checked C-statement
pass adds a propagating check at every compiled function entry and every loop
iteration, including header macros. Receipts enumerate all sites; missing or
unbalanced coverage refuses the build. Original files remain unchanged.
The interpreter's existing counter is 1,000. Each `js_poll_interrupts` call
charges the separate 10-million-site ceiling, not merely handler invocations.

Compiler-rt's large trigonometric argument reduction is separately patched
at all 20 loop sites in a private library copy. The original SDK distribution
and overlay remain unchanged. The linked compiler-rt call closure and its
frames are inspected, including stripped local function sections. Unused
arbitrary-width integer helpers are not part of the linked closure.

A private C-only AAPCS64 save/restore boundary propagates sticky refusal.
Callbacks return to C before restoring; no live Zig allocator frame is skipped.
Byte/math bridges return on refusal rather than nonlocally jumping through
Zig callers. Each dynamic `alloca` checks its proposed size first. Entry checks
reserve 8 KiB inside the 96 KiB C limit for checked prologues/bridge frames;
the builder rejects larger C frames. x18 is never saved, used or modified.

After refusal, do not traverse a partially mutated QuickJS graph. A separately
maintained, fully charged allocation registry reclaims engine blocks without
user finalizers/jobs and preserves adapter blocks. Ordinary destroy also runs
QuickJS teardown/GC, then verifies return to the pre-create arena accounting.
Charged 4 KiB allocation granules bound SDK free-list traversal to 1,021 nodes.

`pin.py` is an explicit implementation-maintenance operation, never a build
step. It refuses changed original source and updates private input hashes.
It cannot approve an upstream/compiler/SDK upgrade. Those changes require the
owner review and full verification specified by ADR 0039.
