# tagshift.spec — #3128 DIAGNOSTIC spec (not a fleet gate; lives outside
# tools/gate/specs/ so fleet.sh never discovers it). Cut-down copy of
# live-vlfs-crash's sealed kill pair: four consecutive kill-mid-stream
# legs on ONE sealed v2 image. Each leg re-unseals, replays whatever the
# last kill stranded, keeps streaming TAIL.BIN, and is hard-stopped
# mid-write — the write-frontier churn the shifted-tag tear needs. The
# post-leg python copies each killed image aside so the tagscan
# dissector can grade it offline.
#
# Usage: bash tools/gate/vgate.sh tools/tagshift/tagshift.spec
#        (VGATE_NO_BUILD=1 after the first build; VIRELAI_KEEP_RUN=1 keeps
#         the run dir; BOOTS=N repeats the spec body N times)

vgate_name tagshift-repro "#3128 diagnostic: repeated sealed kill legs to harvest torn tag homes"
vgate_fmt kernel/src/vlfs/*.zig kernel/src/seal.zig kernel/src/virtio_blk.zig
vgate_repeat 4 BOOTS
vgate_note "Diagnostic only — asserts never fail on the tear; harvest is the point."

vgate_setup_python <<'PY'
import os, subprocess, sys
from pathlib import Path

run = Path(os.environ["RUN_DIR"])
subprocess.check_call(["go", "-C", "tools/go/vlfs", "build",
                       "-o", str(run / "mkfs.vlfs"), "."])
(run / "pass.txt").write_text("swordfish\n")
subprocess.check_call([str(run / "mkfs.vlfs"), "--seal",
                       "--passphrase-file", str(run / "pass.txt"),
                       "-mib", "16", "-kdf-iters", "4000",
                       str(run / "vlfs-s.img")])
(run / "script-pass.txt").write_text("swordfish\n")
(run / "script-ws1.txt").write_text(
    "vf rm /disk/TAIL.BIN\n"
    "write /disk/ANCHOR.TXT virelai vlfs crash anchor\n"
    "vf fsync /disk\n"
    "echo vlfs-committed-anchor-s1\n"
    "vf write /disk/TAIL.BIN 12000000\n")
(run / "script-ws2.txt").write_text(
    "echo tagshift-streaming-s2\n"
    "vf write /disk/TAIL2.BIN 12000000\n")
print("tagshift setup: sealed vlfs-s.img + scripts ready")
PY

# Four kill legs on the same image; each boot re-unseals and replays.
vgate_run 01 -- --data-disk '$RUN_DIR/vlfs-s.img' \
    --script '$RUN_DIR/script-pass.txt' --script-after 'vlfs: sealed — passphrase: ' \
    --script2 '$RUN_DIR/script-ws1.txt' --script2-after 'virelai> ' \
    --script-expect 'vlfs-committed-anchor-s1' --script-expect-tail 1.5 --timeout 120

vgate_assert 01 serial-contains 'vlfs: sealed — unsealed'
vgate_assert 01 serial-contains 'vlfs: mounted /disk'
vgate_assert 01 python <<'PY'
import os, shutil, subprocess
run = os.environ["RUN_DIR"]
shutil.copyfile(run + "/vlfs-s.img", run + "/killed-01.img")
out = subprocess.run([run + "/mkfs.vlfs", "--check", "--passphrase-file",
                      run + "/pass.txt", run + "/vlfs-s.img"],
                     capture_output=True, text=True)
print("killed-01 fsck:", out.stdout.strip().replace("\n", " | ") or "(no output)")
PY

vgate_run 02 -- --data-disk '$RUN_DIR/vlfs-s.img' \
    --script '$RUN_DIR/script-pass.txt' --script-after 'vlfs: sealed — passphrase: ' \
    --script2 '$RUN_DIR/script-ws2.txt' --script2-after 'virelai> ' \
    --script-expect 'tagshift-streaming-s2' --script-expect-tail 1.5 --timeout 120

vgate_assert 02 serial-contains 'vlfs: sealed — unsealed'
vgate_assert 02 python <<'PY'
import os, shutil, subprocess
run = os.environ["RUN_DIR"]
shutil.copyfile(run + "/vlfs-s.img", run + "/killed-02.img")
out = subprocess.run([run + "/mkfs.vlfs", "--check", "--passphrase-file",
                      run + "/pass.txt", run + "/vlfs-s.img"],
                     capture_output=True, text=True)
print("killed-02 fsck:", out.stdout.strip().replace("\n", " | ") or "(no output)")
PY

vgate_run 03 -- --data-disk '$RUN_DIR/vlfs-s.img' \
    --script '$RUN_DIR/script-pass.txt' --script-after 'vlfs: sealed — passphrase: ' \
    --script2 '$RUN_DIR/script-ws2.txt' --script2-after 'virelai> ' \
    --script-expect 'tagshift-streaming-s2' --script-expect-tail 1.5 --timeout 120

vgate_assert 03 serial-contains 'vlfs: sealed — unsealed'
vgate_assert 03 python <<'PY'
import os, shutil, subprocess
run = os.environ["RUN_DIR"]
shutil.copyfile(run + "/vlfs-s.img", run + "/killed-03.img")
out = subprocess.run([run + "/mkfs.vlfs", "--check", "--passphrase-file",
                      run + "/pass.txt", run + "/vlfs-s.img"],
                     capture_output=True, text=True)
print("killed-03 fsck:", out.stdout.strip().replace("\n", " | ") or "(no output)")
PY

vgate_run 04 -- --data-disk '$RUN_DIR/vlfs-s.img' \
    --script '$RUN_DIR/script-pass.txt' --script-after 'vlfs: sealed — passphrase: ' \
    --script2 '$RUN_DIR/script-ws2.txt' --script2-after 'virelai> ' \
    --script-expect 'tagshift-streaming-s2' --script-expect-tail 1.5 --timeout 120

vgate_assert 04 serial-contains 'vlfs: sealed — unsealed'
vgate_assert 04 python <<'PY'
import os, shutil, subprocess
run = os.environ["RUN_DIR"]
shutil.copyfile(run + "/vlfs-s.img", run + "/killed-04.img")
out = subprocess.run([run + "/mkfs.vlfs", "--check", "--passphrase-file",
                      run + "/pass.txt", run + "/vlfs-s.img"],
                     capture_output=True, text=True)
print("killed-04 fsck:", out.stdout.strip().replace("\n", " | ") or "(no output)")
PY
