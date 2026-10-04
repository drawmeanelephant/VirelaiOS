# M89b engine tools

`bash tools/pdf-engine/build.sh` builds the complete and equivalent empty
static adapters offline. It refuses missing/drifting toolchain inputs and
checks both complete file and initialized PT_LOAD deltas against 2,097,152 B,
including vector. It does not provision the fork or use stock Go as a fallback.
The lock records compiler/tool binaries, fork revision and source/overlay
hashes. Upstream Go/runtime notices remain in the provisioned distribution.
No decoder code was copied from third-party sources.

Provision/build the local Go 1.27.1 fork separately. An intentional pin update
uses `python3 tools/pdf-engine/lock.py record REPO FORK LOCK` only after source,
license, dependency and build review. Ordinary builds use `check`, never
`record`. Results go to ignored `artifacts/m89-engine/`.

Host checks:

```sh
go -C user/go test ./pdf/...
python3 -m unittest discover -s tools/pdf-engine -p 'test_*.py'
```

The adapters share a compiled-in copy of the authored empty fixture, one
populated 9 MiB mapping, native output writer and fixed staging. They accept a
fresh output filename and write PDF1 packed BGRA without a second page copy.
They are size/loader products, not M89c's native PDF source-window proof.
No adapter guest run, pixel comparison, guest timing or runtime fit is claimed.

## Consumer contract

`pdf.Render(Source, pageIndex, arena, ledger)` returns a complete borrowed
packed Page, numeric Stats and a by-value Failure. Source supplies its checked
immutable length and fills caller storage. Engine owns the single 64 KiB
window inside the arena. Native Source must charge read attempts, copied and
discarded bytes, backwards reopens, hashing and source recheck to this same
Ledger. Never retain a full file, an `os.File` shadow or a decoded stream.

Supply one 8-byte-aligned, populated 9,437,184-byte arena. POD records contain
no Go pointers. Successful Page expires at arena reuse. On failure, no Page is
returned, all written prefixes are invalid, decoder frames are released and
charged work remains. Supply a new Ledger per transaction, not per stream,
glyph, image or vector batch. Set `Now` to the monotonic guest clock and
`Deadline` to start+5,000,000,000 ns; nil clock is host diagnostic mode only.
Publication/source immutability and confirmed native writes belong to M89c.

`runtime_ledger.py` emits a pinned-source conservative recipe, not a passing
memory receipt. M89c must supply M90d high-water dynamic-page/region receipts,
including startup, GC, stacks, retained spans, 100-cycle failed-input reuse
and process-exit reclamation. Runtime must remain <=768 pages/11 regions;
arena+runtime <=3072 pages/12 regions. HeapAlloc or ELF memsz cannot prove it.
