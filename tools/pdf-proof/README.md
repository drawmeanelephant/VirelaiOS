# M89-PDF1 proof products

Provision the Go fork separately (`tools/go/apply.sh`). `GO_FORK_DIR` must
identify the exact fork in the read-only engine lock, not stock Go. Ordinary
builds check both locks and never acquire or repair a toolchain:

```sh
bash tools/pdf-proof/build.sh
python3 -B tools/pdf-proof/oracle.py check
python3 -B tools/pdf-proof/oracle.py generate artifacts/m89-acceptance/reference
python3 -B -m unittest discover -s tools/pdf-proof -p 'test_*.py'
PDF_PROOF_HOST_OUT="$PWD/artifacts/m89-acceptance/host" \
  GOPROXY=off GOSUMDB=off GOTOOLCHAIN=local \
  "$GO_FORK_DIR/bin/go" -C user/go test ./pdfproof ./pdf
python3 tools/pdf-proof/check_host.py
just gate live-pdf-raster
```

The outside reference is Apple's host-only CoreGraphics PDF renderer, supplied
by macOS, not a downloaded dependency or guest fallback. `oracle.py` pins the
macOS product/build, system `swift --version`, `cgrender.swift` SHA-256 and
invocation contract. `generate` builds that tool offline with `/usr/bin/swiftc`
into ignored `.build/pdf-proof/` and verifies frozen reference hashes.
Only an intentional provenance review uses `freeze`; OS/compiler/source drift
fails rather than silently repinning. The A2.1 tool intersects CropBox with
MediaBox, scales by 96/72, fills an 8-bit DeviceRGB context opaque white, enables
antialiasing, disables image interpolation and emits top-down packed BGRA.
Each row records diagnostics with `CG_PDF_VERBOSE=1`; raw logs stay in artifacts.
Only known lifecycle/cache traces are excluded. Only the five blank
absent-Contents rows may emit the known invalid-Contents exception; any other
diagnostic fails.
Negatives and limit+1 sources never enter the outside reference.
Under ADR 0040 A1/A2, `corpus.py` declares independent geometry beside the byte
recipes. `analytic.py` maps CTM/font/text/line matrices, expands Type 3 outlines,
flattens cubic curves to 1/64 pixel, and uses four subrows with horizontal span
overlap, winding/parity and 8-bit rounding. It never parses PDF or reads Go or
tested output. Half-open clip/image extents and nearest-neighbor center samples
(ties to the higher source index, including reflection) remain exact.

Engine edge tolerances apply only to the analytic reference: 64/255 maximum,
1/255 whole-image mean and one-pixel analytic edge dilation. The outside reference
cross-checks geometry with mutual edge-mask dilation and exact pixels outside
the union dilation. Clip/image pixels more than one pixel from an extent or
source-cell boundary must match the outside reference too. `oracle.json` pins
disagreement counts and SHA-256 of lexicographically sorted `(x,y)` coordinates, encoded as
ASCII `x,y\n`. Frozen PPM/BGRA hashes still pin the actual edge values.
Reference preparation retains every product and records a `geometry_failure`
when a cross-check fails, then returns nonzero. Pinning that diagnostic never
makes it acceptance: `generate`, host comparisons and VZ assertions still fail.

`corpus.py generate OUTPUT` checks small pinned recipes and writes large
sources only to the requested ignored artifact directory. `author` is the
explicit fixture/recipe update operation, not a build step.
`split-streams` carries operands, a path/q frame and open BT across token-safe
boundaries. `negative-split-token` retains the original PDF bytes and expects
`MalformedContent`; it never enters the outside reference.

The proof ELF takes a bounded TSV plan, a fresh receipt path and a cycle count
(1 or 100). Plan fields are ID, native source path, fresh output path (or `-`),
zero-based page, expected fixed numeric code and render count. Expected refusals
are suite outcomes; any unexpected code or I/O error exits nonzero and withholds
completion. Accepted-page plans request one cold render and 20 repeats. Each
includes whole-document validation and sequential length/SHA-256 recheck before
publication. Admission is charged but outside the render deadline; serialization
is also charged but outside it. There is one hinted populated 9 MiB mapping.

Output is a `PDF1` header (little-endian width, height, packed stride) followed
by opaque BGRA, written directly from the borrowed page. Native short writes
advance by confirmed counts. Existing outputs are refused. Write/sync failure
withholds completion; a failed new file may be partial. Close is mandatory but
the SDK has no separately detectable close error.

The gate stages only verified offline products. It requires M90f's merge commit
in checkout ancestry before any final runtime/reclamation boot. Its independent
checks use `procs receipt` peaks, separately checked static ELF pages and the
free-page pool, never HeapAlloc or ELF memsz as dynamic-memory proof. Kernel
peaks conservatively bound every page in the boot. First-cycle and final peaks
must agree across 100 success/refusal/recovery cycles. Completion is printed
after writes/sync/close; a bounded runner tail captures the reap diagnostics.
`procs receipt` exposes running-process counters with `reaped=0` and retains
the same peaks after exit and task reap (`reaped=1`). Identity, counters and
reap state are read under the existing kernel-domain lock used by backing/
region updates. A primary-thread exit does not mark a live sibling as reaped.
Missing baseline counters still fail rather than substituting a final peak
or HeapAlloc estimate.
Runtime progress prints first-cycle and 50/100-cycle elapsed nanoseconds.
Baseline and cycle 50 wait for a one-byte host acknowledgment, bounded by the
existing 5,000 ms ceiling. The host publishes each acknowledgment only after
the complete live receipt is present in the serial log; order assertions
require each receipt before its resume marker. A loopback console client
requests the final reaped receipt. All three readings must agree on page peaks,
cumulative allocations and region peaks; no warm-up reading replaces baseline.
Pause I/O reuses existing staging outside the timed page transactions.
Receipts retain maxima across all cycles,
not only the last cycle. The 360-second runtime harness wait covers the full
100-cycle/forced-GC suite, not a single page. The 5,000 ms page deadline and
every work/memory limit remain unchanged.

All large sources, reference PPM/BGRA, host products and detailed checks stay
under ignored `artifacts/m89-acceptance/`. Standard gate evidence uses ignored
`artifacts/live-pdf-raster-*`. Never commit the fleet-inventory snapshot.
