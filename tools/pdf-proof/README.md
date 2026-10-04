# M89-PDF1 proof products

Provision the Go fork separately (`tools/go/apply.sh`). `GO_FORK_DIR` must
identify the exact fork in the read-only engine lock, not stock Go. Ordinary
builds check both locks and never acquire or repair a toolchain:

```sh
bash tools/pdf-proof/build.sh
python3 tools/pdf-proof/oracle.py check
python3 tools/pdf-proof/oracle.py generate artifacts/m89-acceptance/reference
python3 -m unittest discover -s tools/pdf-proof -p 'test_*.py'
PDF_PROOF_HOST_OUT="$PWD/artifacts/m89-acceptance/host" \
  "$GO_FORK_DIR/bin/go" -C user/go test ./pdfproof ./pdf
python3 tools/pdf-proof/check_host.py
just gate live-pdf-raster
```

`oracle.py` resolves the installed binary and checks its exact SHA-256, font/
colour library closure and system build. `generate` verifies frozen reference
hashes. Only an intentional provenance review uses `freeze`; another Poppler
build is never a fallback. Negatives and limit+1 sources never enter the oracle.
`corpus.py generate OUTPUT` checks small pinned recipes and writes large
sources only to the requested ignored artifact directory. `author` is the
explicit fixture/recipe update operation, not a build step.

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

All large sources, reference PPM/BGRA, host products and detailed checks stay
under ignored `artifacts/m89-acceptance/`. Standard gate evidence uses ignored
`artifacts/live-pdf-raster-*`. Never commit the fleet-inventory snapshot.
