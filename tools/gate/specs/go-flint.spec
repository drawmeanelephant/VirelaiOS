# go-flint.spec -- M103a (#2253, ADR 0049): FLINT.ELF runs go/parser and
# the Go-native Zig declaration scanner IN-GUEST over the pinned corpus.
# The serial marker carries files/atoms/parse_errors; the guest's
# FLINTC.OUT on the share must be byte-equal to the host rendering of
# MANIFEST.json, and the boot must not park an exception.
#
# The corpus is stateful input for later runs in this spec (M103b-d add
# 02-10 against /host/FLINTW and journey scripts); run 01 is read-only on
# FLINTC and appends nothing to the share besides FLINTC.OUT.
#
# HOST PREREQUISITE: bash tools/go/build-flint.sh -> .build/go/FLINT.ELF

vgate_name go-flint "M103a #2253: FLINT.ELF parse-corpus - go/parser + Zig scanner in-guest"
vgate_share seed-virtiofs
vgate_runner_flags -Xswiftc -DSPIKE

vgate_file script-parse.txt <<'EOF'
exec FLINT.ELF parse-corpus /host/FLINTC
EOF

vgate_setup_python <<'PY'
import json, os, shutil, sys
run = os.environ["RUN_DIR"]
share = os.environ.get("VG_SHARE") or os.path.join(run, "share")
os.makedirs(share, exist_ok=True)

flint = os.path.join(".build", "go", "FLINT.ELF")
if not os.path.exists(flint):
    sys.exit("FLINT.ELF missing (expected " + flint + ") - build it first: "
             "bash tools/go/build-flint.sh")
shutil.copy(flint, os.path.join(share, "FLINT.ELF"))

corpus = os.path.join("user", "go", "flint", "testdata", "corpus")
dest = os.path.join(share, "FLINTC")
if os.path.exists(dest):
    shutil.rmtree(dest)
for sub in ("go", "zig"):
    shutil.copytree(os.path.join(corpus, sub), os.path.join(dest, sub))
shutil.copy(os.path.join(corpus, "MANIFEST.json"), dest)

# expect-corpus.txt is the host rendering of MANIFEST.json in exactly the
# parser.Render line shape the guest writes to FLINTC.OUT.
man = json.load(open(os.path.join(corpus, "MANIFEST.json")))
lines = []
for a in man["atoms"]:
    lines.append("%s\t%s\t%s\t%d\t%d\t%s\t%s" % (
        a["file"], a["kind"], a["name"], a["start"], a["end"],
        a["hash"], a["sig_hash"]))
open(os.path.join(run, "expect-corpus.txt"), "w").write("\n".join(lines) + "\n")
open(os.path.join(run, "atoms-count"), "w").write(str(len(man["atoms"])))
PY

# ---- 01: parse the corpus in-guest, byte-exact receipt -------------------
vgate_run 01 -- --script '$RUN_DIR/script-parse.txt' \
    --script-expect 'flint: parse files=100 atoms=245 parse_errors=0' --timeout 240

vgate_assert 01 serial-contains 'flint: parse files=100 atoms=245 parse_errors=0'
vgate_assert 01 share-equals FLINTC.OUT expect-corpus.txt
vgate_assert 01 serial-absent '[EXC] parking:'
vgate_assert 01 python <<'PY'
import os, sys
run = os.environ["RUN_DIR"]
want = int(open(os.path.join(run, "atoms-count")).read())
if want != 245:
    sys.exit("FAIL: MANIFEST atom count drifted from the pinned 245")
PY
