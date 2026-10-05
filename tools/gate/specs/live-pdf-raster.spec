# Bounded PDF source-to-bitmap acceptance, with pinned CoreGraphics outside references.
# One invocation per boot. Every boot ends on the producer's own marker.
# The runner's bounded tail captures the final reap and kernel receipts.
# Missing inputs, pins, counters or M90f are failures, never skips.
vgate_name live-pdf-raster "M89-PDF1 native PDF pixels, refusals, maxima and runtime/reclamation"
vgate_share arm
vgate_runner_flags -Xswiftc -DSPIKE

vgate_setup_python <<'PY'
import os, sys
from pathlib import Path
sys.path.insert(0, str(Path.cwd()/"tools/pdf-proof"))
from stage import stage
stage(Path(os.environ["RUN_DIR"]))
PY

vgate_file accepted.txt <<'EOF'
settings set wm none
pages
exec PDFPROOF.ELF /host/accepted.plan /host/PDF/accepted.receipt 1
EOF
vgate_file refusals.txt <<'EOF'
settings set wm none
pages
exec PDFPROOF.ELF /host/refusals.plan /host/PDF/refusals.receipt 1
EOF
vgate_file maxima.txt <<'EOF'
settings set wm none
pages
exec PDFPROOF.ELF /host/maxima.plan /host/PDF/maxima.receipt 1
EOF
vgate_file runtime.txt <<'EOF'
settings set wm none
pages
exec PDFPROOF.ELF /host/runtime.plan /host/PDF/runtime.receipt 100
EOF
vgate_file baseline.txt <<'EOF'
procs receipt PDFPROOF.ELF
EOF
vgate_file reaped.txt <<'EOF'
procs receipt PDFPROOF.ELF
pages
EOF

vgate_run accepted -- --script '$RUN_DIR/accepted.txt' --script2 '$RUN_DIR/reaped.txt' --script2-after 'procs PDFPROOF.ELF exited status=' --script-expect 'pdf-proof: complete' --script-expect-tail 5 --timeout 180
vgate_assert accepted serial-contains 'pdf-proof: complete'
vgate_assert accepted serial-absent '[EXC] parking:'
vgate_assert accepted python <<'PY'
import os, runpy
runpy.run_path("tools/pdf-proof/check_run.py", run_name="__main__")
PY

vgate_run refusals -- --script '$RUN_DIR/refusals.txt' --script2 '$RUN_DIR/reaped.txt' --script2-after 'procs PDFPROOF.ELF exited status=' --script-expect 'pdf-proof: complete' --script-expect-tail 5 --timeout 180
vgate_assert refusals serial-contains 'pdf-proof: complete'
vgate_assert refusals serial-absent '[EXC] parking:'
vgate_assert refusals python <<'PY'
import os, runpy
runpy.run_path("tools/pdf-proof/check_run.py", run_name="__main__")
PY

vgate_run maxima -- --script '$RUN_DIR/maxima.txt' --script2 '$RUN_DIR/reaped.txt' --script2-after 'procs PDFPROOF.ELF exited status=' --script-expect 'pdf-proof: complete' --script-expect-tail 5 --timeout 360
vgate_assert maxima serial-contains 'pdf-proof: complete'
vgate_assert maxima serial-absent '[EXC] parking:'
vgate_assert maxima python <<'PY'
import os, runpy
runpy.run_path("tools/pdf-proof/check_run.py", run_name="__main__")
PY

# Allow 100 forced-GC cycles plus startup/tail, not 100 page deadlines.
# This harness wait does not change the 5,000 ms per-page time budget.
vgate_run runtime -- --script '$RUN_DIR/runtime.txt' --script2 '$RUN_DIR/baseline.txt' --script2-after 'pdf-proof: baseline' --script3 '$RUN_DIR/reaped.txt' --script3-after 'procs PDFPROOF.ELF exited status=' --script-expect 'pdf-proof: complete' --script-expect-tail 5 --timeout 360
vgate_assert runtime serial-contains 'pdf-proof: complete'
vgate_assert runtime serial-absent '[EXC] parking:'
vgate_assert runtime python <<'PY'
import os, runpy
runpy.run_path("tools/pdf-proof/check_run.py", run_name="__main__")
PY
