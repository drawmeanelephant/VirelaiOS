# live-user-fs.spec — userland storage ABI & utilities on VZ.
# Proves round-trip persistence across two boots sharing the host share:
# Boot A: headless GOSH redirection writes /host/hello.txt through the file ABI.
# Boot B: headless GOSH cat reads the payload back and GOFILES lists /host.
# B1's kernel-owned native probe independently checks redirected EOF,
# separate HTML/JSON destinations, inheritance, close/panic and short writes.
# exec-order: assert-proven -- stream boot waits for the probe's exit.

vgate_name live-user-fs "userland storage ABI & utilities on VZ"
vgate_share arm
vgate_runner_flags -Xswiftc -DSPIKE
vgate_repeat 1 PAIRS

vgate_setup_python <<'PY'
import os, pathlib, shutil, subprocess, sys
rd = os.environ["RUN_DIR"]
share = os.environ.get("VG_SHARE") or os.path.join(rd, "share")
for name, script in (("GOSH.ELF", "build-gosh.sh"),
                      ("GOFILES.ELF", "build-files.sh")):
    src = os.path.join(".build", "go", name)
    if not os.path.exists(src):
        sys.exit("%s missing (expected %s) - build it first: bash tools/go/%s"
                 % (name, src, script))
    shutil.copy(src, os.path.join(share, name))
    print("staged %s into share (%d bytes)"
          % (name, os.path.getsize(os.path.join(share, name))))
linker = pathlib.Path(rd) / "streams.ld"
linker.write_text("""ENTRY(_start)
PHDRS { text PT_LOAD FLAGS(5); data PT_LOAD FLAGS(6); }
SECTIONS {
 . = 0x00400000;
 .text : { *(.text .text.*) *(.rodata .rodata.*) } :text
 . = ALIGN(4096) + 4096;
 .data : { *(.data .data.*) *(.bss .bss.*) } :data
 /DISCARD/ : { *(.eh_frame*) *(.note*) }
}
""")
subprocess.run([
    "zig", "build-exe", "kernel/tests/streams_guest.zig",
    "-target", "aarch64-freestanding-none", "-O", "ReleaseSafe",
    "-fsingle-threaded", "-fno-PIE", "-fno-stack-check", "-fno-stack-protector",
    "-fno-unwind-tables", "--script", str(linker), "-fentry=_start",
    "-femit-bin=" + os.path.join(share, "STREAMS.BIN"),
], check=True)
for tag, n in (("A", 5003), ("B", 7001)):
    data = bytes(65 + ((i * 7 + ord(tag)) % 26) for i in range(n))
    pathlib.Path(share, "B1.IN." + tag).write_bytes(data)
    pathlib.Path(rd, "expected-" + tag + ".html").write_bytes(b"<html>\n" + data + b"</html>\n")
pathlib.Path(rd, "expected-diagnostic.json").write_bytes(b'{"kind":"b1","eof":true}\n')
pathlib.Path(rd, "expected-panic.txt").write_bytes(b"b1: panic\n")
pathlib.Path(rd, "expected-empty.txt").write_bytes(b"")
PY

vgate_file script-A.txt <<'EOF'
set GOMAXPROCS=1
exec GOSH.ELF -c "echo Hello from VirelaiOS EL0 Storage! > /host/hello.txt"
EOF

vgate_file script-A2.txt <<'EOF'
procs
echo done-user-fs-write
EOF

vgate_file script-B.txt <<'EOF'
set GOMAXPROCS=1
exec GOSH.ELF -c "cat /host/hello.txt"
EOF

vgate_file script-B2.txt <<'EOF'
exec GOFILES.ELF /host
EOF

vgate_file script-B3.txt <<'EOF'
procs
echo done-user-fs-read
EOF

vgate_run A -- --script '$RUN_DIR/script-A.txt' --script-after 'tasks user-el0 exited status=7' --script2 '$RUN_DIR/script-A2.txt' --script2-after 'tasks user-exec exited status=0' --script-expect 'done-user-fs-write' --timeout 60
vgate_run B -- --display --screen '$RUN_DIR/screen' --via-virtio --script '$RUN_DIR/script-B.txt' --script-after 'tasks user-el0 exited status=7' --script2 '$RUN_DIR/script-B2.txt' --script2-after 'tasks user-exec exited status=0' --input-chords 'q' --input-chords-after 'gofiles: ready' --script3 '$RUN_DIR/script-B3.txt' --script3-after 'gofiles OK' --script-expect 'done-user-fs-read' --timeout 120

vgate_file script-streams.txt <<'EOF'
exec STREAMS.BIN
EOF
vgate_file script-streams-after.txt <<'EOF'
procs
syscalls
echo done-user-fs-streams
EOF
vgate_run streams -- --script '$RUN_DIR/script-streams.txt' --script-after 'tasks user-el0 reaped' --script2 '$RUN_DIR/script-streams-after.txt' --script2-after 'b1: streams done' --script2-delay 1 --script-expect 'done-user-fs-streams' --timeout 90
vgate_assert streams serial-contains 'b1: streams done'
vgate_assert streams serial-absent 'b1: FAIL'
vgate_assert streams serial-absent '[EXC] parking'
vgate_assert streams serial-absent '{"kind":"b1","eof":true}'
vgate_assert streams serial-absent 'b1: panic'
vgate_assert streams share-equals B1.OUT.A expected-A.html
vgate_assert streams share-equals B1.OUT.B expected-B.html
vgate_assert streams share-equals B1.ERR.A expected-diagnostic.json
vgate_assert streams share-equals B1.ERR.B expected-diagnostic.json
vgate_assert streams share-equals B1.PANIC.OUT expected-empty.txt
vgate_assert streams share-equals B1.PANIC.ERR expected-panic.txt
vgate_assert streams python <<'PY'
import os, pathlib, re
rd = pathlib.Path(os.environ["RUN_DIR"])
share = pathlib.Path(os.environ["VG_SHARE"])
serial = pathlib.Path(os.environ["VG_SER"]).read_bytes()
assert b"b1-console:" + b"C" * 1103 + b"\n" in serial
assert serial.count(b"b1: streams done\n") == 1
statuses = re.findall(rb"procs STREAMS\.BIN exited status=(\d+)", serial)
assert statuses.count(b"0") == 4 and statuses.count(b"71") == 1, statuses
for tag, n in (("A", 5003), ("B", 7001)):
    source = share.joinpath("B1.IN." + tag).read_bytes()
    assert source == bytes(65 + ((i * 7 + ord(tag)) % 26) for i in range(n))
    assert share.joinpath("B1.OUT." + tag).read_bytes() == b"<html>\n" + source + b"</html>\n"
    assert share.joinpath("B1.ERR." + tag).read_bytes() == b'{"kind":"b1","eof":true}\n'
assert share.joinpath("B1.PANIC.OUT").read_bytes() == b""
assert share.joinpath("B1.PANIC.ERR").read_bytes() == b"b1: panic\n"
print("B1: 12004 input bytes, 12034 HTML bytes, 2 JSON destinations, "
      "1103 console bytes; inherited close and panic isolation verified")
PY

vgate_assert A serial-contains 'exec: loaded GOSH.ELF'
vgate_assert A serial-contains 'tasks user-exec exited status=0'
vgate_assert A serial-contains 'procs GOSH.ELF exited status=0'
vgate_assert A serial-contains 'done-user-fs-write'
vgate_assert A serial-absent '[EXC] parking'

vgate_assert B serial-contains 'Hello from VirelaiOS EL0 Storage!'
vgate_assert B serial-contains 'procs GOSH.ELF exited status=0'
vgate_assert B serial-contains 'exec: loaded GOFILES.ELF'
vgate_assert B serial-contains 'gofiles: list /host'
vgate_assert B serial-contains 'gofiles: entry hello.txt file'
vgate_assert B serial-contains 'gofiles: ready'
vgate_assert B serial-contains 'gofiles: close'
vgate_assert B serial-contains 'gofiles OK'
vgate_assert B serial-contains 'procs GOFILES.ELF exited status=0'
vgate_assert B serial-contains 'done-user-fs-read'
vgate_assert B serial-absent '[EXC] parking'

vgate_assert B python <<'PY'
import os, sys
ser = open(os.environ["VG_SER"], "rb").read().decode("latin1", errors="replace").lower()
if "hello.txt" not in ser:
    sys.exit("ERROR: hello.txt missing from GOFILES enumeration")
share = os.environ.get("VG_SHARE") or os.path.join(os.environ["RUN_DIR"], "share")
hpath = os.path.join(share, "hello.txt")
if not os.path.exists(hpath):
    sys.exit("ERROR: hello.txt missing from host share")
data = open(hpath, "r", errors="replace").read()
if "Hello from VirelaiOS EL0 Storage!" not in data:
    sys.exit("ERROR: hello.txt content mismatch on host share")
print("user-fs ok: GOSH round-trip and GOFILES enumeration verified")
PY
