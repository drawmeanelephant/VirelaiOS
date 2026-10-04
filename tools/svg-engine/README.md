# SVG engine build and source audit

Offline, pinned Go 1.27.1 fork, normal optimized build:

```sh
source tools/env-check.sh
bash tools/svg-engine/build.sh
go -C user/go test ./svg ./vector ./ttf ./draw
go -C user/go test ../../tools/svg-engine/adapter.go ../../tools/svg-engine/engine.go ../../tools/svg-engine/adapter_test.go
python3 -m unittest discover -s tools/svg-engine -p 'test_*.py'
```

Provision the local fork with `tools/go/apply.sh` and its local `make.bash`
pass first, or set `GO_FORK_DIR`. The recipe never installs/downloads
modules. Outputs are ignored under `artifacts/m90-engine/`: empty/full
stripped ELFs, dependency list, `elf.json`, and `runtime-ledger.json`.
The empty adapter removes only the engine packages/calls. Both use the
same SDK, transport, writer, fixed 8 MiB mapping and build flags.

The guest adapter takes an input name and output name and writes raw
top-down BGRA words. It does not publish a window, measure latency or
establish acceptance pixels. Source cap+1 is detected through the bounded
2048-byte staging read before parsing. Setup/transport/serialization are
outside render work; the destination clear costs one unit per pixel.

The arena follows ADR 0041 §5.1 exactly, including unused margin. Scene
records are pointer-free, aligned 56-byte commands and 96-byte paints.
Vector scratch is 417,792 bytes at maximum paint count inside the fixed
524,288-byte workspace. It contains edges, one in-place heapsort crossing
array, one float64 coverage row, and paint plans. No second output exists.
Preflight validates the entire scene, flattens and dry-samples it, then
reserves the exact expansion/sample replay cost before writing pixels.
Both passes and validation count against the same caller Budget.

`ledger.py` is a fail-closed **source audit**, not a guest memory receipt.
It exits 2 while §5.1 lacks a proved runtime upper bound. In the pinned
sbrk runtime, virelai's fresh aligned growth links already-zero padding
with only `memHdr` writes. The audit still requires the literal
`memFree(r, l)` in the unchanged plan9/wasm branch. Ordinary frees,
free-list alignment trims and shrinking still clear; removed headers
are cleared on allocation. It checks the page-rounded ELF/argv floor,
demand-zero faults and mmap collision checks as well. Restoring the old
sweep or losing an audited zero-invariant term fails closed. Padding is
still header-touched, not wholly untouched or a measured runtime bound.
M90f's 4,096-page field is inline tracking capacity, not a working-set cap;
no renderer budget changes. Re-provision the fork after #1958. Free/unneeded
mappings remain retained, each growth consumes another region, and the
L2 heap index mapping is explicitly absent from MemStats accounting.
Neither `HeapAlloc`, virtual heap reservation, nor ELF `memsz` proves
demand backing or the eleven-region runtime allowance.

Acceptance requires separately owned mapping/touch diagnostics covering
startup, stack/collector growth, and 100 success/refusal cycles. No runtime,
kernel or SDK changes are made here. Do not turn this source audit into
a guessed passing ledger or claim M90c guest/timing evidence.
