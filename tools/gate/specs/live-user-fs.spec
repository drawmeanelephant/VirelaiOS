# live-user-fs.spec — userland storage ABI & utilities on VZ.
# Proves round-trip persistence across two boots sharing the host share:
# Boot A: headless GOSH redirection writes /host/hello.txt through the file ABI.
# Boot B: headless GOSH cat reads the payload back and GOFILES lists /host.
# B1's kernel-owned native probe independently checks redirected EOF,
# separate HTML/JSON destinations, inheritance, close/panic and short writes.
# exec-order: assert-proven -- stream boot waits for the probe's exit.
# B2 runs one raw native fixture on both backends, with independent
# share-byte validation, real continuation/EOF and post-death page recovery.
# B4: one raw native-ABI probe runs against each backend. The host compares
# both publications, retained stages, failure receipts and trust metadata.

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
pages
exec STREAMS.BIN
EOF
vgate_file script-streams-after.txt <<'EOF'
pages
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
pages = re.findall(rb"^pages: armed=1 total=(0x[0-9a-f]+) free=(0x[0-9a-f]+)", serial, re.M)
assert len(pages) >= 2 and pages[0] == pages[-1], ("stream/EL0 page leak", pages)
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

vgate_setup_python <<'PY'
import os, pathlib, subprocess
rd = pathlib.Path(os.environ["RUN_DIR"])
# Keep the wide B2 tree out of GOFILES' intentionally clamped legacy listing.
share = rd / "b2-legacy-share"
share.mkdir()
# Raw ELF exec loads RX and RW PT_LOADs. Include constants in the RX segment;
# the DSK3 converter's separate read-only-segment coalescing is not used here.
linker = rd / "b2.ld"
linker.write_text("""ENTRY(_start)
SECTIONS {
  . = 0x00400000;
  .text : ALIGN(16) { *(.text .text.*) *(.rodata .rodata.*) }
  . = ALIGN(4096) + 4096;
  .data : ALIGN(16) { *(.data .data.*) }
  .bss : ALIGN(16) { *(.bss .bss.*) }
  /DISCARD/ : { *(.note.*) *(.eh_frame .eh_frame_hdr) *(.ARM.exidx*) }
}
""")
subprocess.run(["zig", "build-exe", "-target", "aarch64-freestanding-none",
    "-O", "ReleaseSafe", "-fentry=_start", "-fno-PIE",
    "-fsingle-threaded", "-T", str(linker),
    "--dep", "directory", "-Mroot=tests/dir-enum.zig",
    "-Mdirectory=kernel/src/directory.zig", "-femit-bin=" + str(share / "B2.ELF")], check=True)
names = [f"entry-{i:02d}" for i in range(35)] + [
    "p" * 31 + "-one", "p" * 31 + "-two", "x" * 255, ".hidden", "last"]
expected = "".join(n + "\n" for n in sorted(names)).encode()
(rd / "b2-expected.txt").write_bytes(expected)
# Each backend gets its own identically seeded tree and mutation fixture.
virtio = rd / "virtio-share"
virtio.mkdir()
for root in (share, virtio):
    if root != share:
        (root / "B2.ELF").write_bytes((share / "B2.ELF").read_bytes())
    (root / "B2").mkdir()
    for name in names:
        (root / "B2" / name).write_bytes(b"entry-data")
    for dirname, count in (("B2LIMIT", 256), ("B2OVER", 257)):
        (root / dirname).mkdir()
        for i in range(count):
            (root / dirname / f"f-{i:03d}").write_bytes(b"")
    (root / ("a" * 250) / ("b" * 255)).mkdir(parents=True)
    (root / ("a" * 250) / ("b" * 255) / "leaf").write_bytes(b"leaf")
    (root / "d/e/e/e/e/e/e/e").mkdir(parents=True)
    (root / "B2DENIED").mkdir()
    (root / "B2MUTATE").mkdir()
    (root / "B2MUTATE/old").write_bytes(b"old")
    (root / "OWNERS.TXT").write_text("#v1\nB2DENIED\t600\t0\t-\n")
PY

vgate_file b2-start.txt <<'EOF'
pages
exec B2.ELF
EOF

vgate_file b2-after.txt <<'EOF'
pages
procs
echo done-b2
EOF

vgate_run b2-legacy -- --cvc-file '$RUN_DIR/b2-legacy-share' --script '$RUN_DIR/b2-start.txt' --script-after 'tasks user-el0 reaped' --script2 '$RUN_DIR/b2-after.txt' --script2-after 'tasks user-exec reaped' --script-expect 'done-b2' --timeout 120
# VirtioFS wins backend selection when both devices exist. Stage an
# independent identical share; the harness still owns the legacy device.
vgate_run b2-virtiofs -- --virtio-fs '$RUN_DIR/virtio-share' --script '$RUN_DIR/b2-start.txt' --script-after 'tasks user-el0 reaped' --script2 '$RUN_DIR/b2-after.txt' --script2-after 'tasks user-exec reaped' --script-expect 'done-b2' --timeout 120

vgate_assert b2-legacy serial-exact 'b2: lossless count=40 pages=6 eof=1' 1
vgate_assert b2-legacy serial-exact 'b2: entries 256 accepted 257 refused' 1
vgate_assert b2-legacy serial-exact 'b2: path 512 accepted 513 refused depth 8 accepted 9 refused acl denied' 1
vgate_assert b2-legacy serial-exact 'b2: stable snapshot mutation ok' 1
vgate_assert b2-legacy serial-exact 'b2: capacity close ok death cursors=8' 1
vgate_assert b2-legacy serial-exact 'tasks user-exec exited status=0' 1
vgate_assert b2-legacy serial-absent 'b2: FAIL'
vgate_assert b2-legacy serial-absent '[EXC]'
vgate_assert b2-legacy python <<'PY'
import os, pathlib, re, shutil
rd = pathlib.Path(os.environ["RUN_DIR"])
actual = rd / "b2-legacy-share/B2.RECEIPT"
assert actual.read_bytes() == (rd / "b2-expected.txt").read_bytes()
shutil.copyfile(actual, pathlib.Path("artifacts/live-user-fs-b2-legacy-receipt.txt"))
lines = pathlib.Path(os.environ["VG_SER"]).read_text()
pages = re.findall(r"^pages: armed=1 total=(0x[0-9a-f]+) free=(0x[0-9a-f]+)", lines, re.M)
assert len(pages) >= 2 and pages[0] == pages[-1], ("cursor/EL0 page leak", pages)
PY

vgate_assert b2-virtiofs serial-exact 'b2: lossless count=40 pages=6 eof=1' 1
vgate_assert b2-virtiofs serial-exact 'b2: entries 256 accepted 257 refused' 1
vgate_assert b2-virtiofs serial-exact 'b2: path 512 accepted 513 refused depth 8 accepted 9 refused acl denied' 1
vgate_assert b2-virtiofs serial-exact 'b2: stable snapshot mutation ok' 1
vgate_assert b2-virtiofs serial-exact 'b2: capacity close ok death cursors=8' 1
vgate_assert b2-virtiofs serial-exact 'tasks user-exec exited status=0' 1
vgate_assert b2-virtiofs serial-absent 'b2: FAIL'
vgate_assert b2-virtiofs serial-absent '[EXC]'
vgate_assert b2-virtiofs python <<'PY'
import os, pathlib, re, shutil
rd = pathlib.Path(os.environ["RUN_DIR"])
actual = rd / "virtio-share/B2.RECEIPT"
assert actual.read_bytes() == (rd / "b2-expected.txt").read_bytes()
shutil.copyfile(actual, pathlib.Path("artifacts/live-user-fs-b2-virtiofs-receipt.txt"))
lines = pathlib.Path(os.environ["VG_SER"]).read_text()
pages = re.findall(r"^pages: armed=1 total=(0x[0-9a-f]+) free=(0x[0-9a-f]+)", lines, re.M)
assert len(pages) >= 2 and pages[0] == pages[-1], ("cursor/EL0 page leak", pages)
PY

# This is a gate-only ABI probe, not an additional Zig product/SDK adapter.
vgate_file publish.ld <<'EOF'
ENTRY(_start)
PHDRS {
    text PT_LOAD FLAGS(5);
    data PT_LOAD FLAGS(6);
}
SECTIONS {
    . = 0x00400000;
    .text : { *(.text .text.*) } :text
    .rodata : { *(.rodata .rodata.*) } :text
    . = ALIGN(4096) + 4096;
    .data : { BYTE(0); *(.data .data.*) } :data
    .bss (NOLOAD) : { *(.bss .bss.*) } :data
    /DISCARD/ : { *(.note.*) *(.eh_frame .eh_frame_hdr) }
}
EOF

vgate_file publish.zig <<'EOF'
const replace_bit: u64 = @as(u64, 1) << 63;

fn svc(number: u64, a0: u64, a1: u64, a2: u64, a3: u64) i64 {
    var result: i64 = undefined;
    asm volatile ("svc #0"
        : [result] "={x0}" (result),
        : [number] "{x8}" (number),
          [a0] "{x0}" (a0),
          [a1] "{x1}" (a1),
          [a2] "{x2}" (a2),
          [a3] "{x3}" (a3),
          [unused4] "{x4}" (@as(u64, 0x1234)),
          [unused5] "{x5}" (@as(u64, 0x5678)),
        : .{ .memory = true });
    return result;
}

fn say(message: []const u8) void {
    _ = svc(1, 1, @intFromPtr(message.ptr), message.len, 0);
}

fn expect(result: i64, wanted: i64) void {
    if (result != wanted) {
        say("b4: FAIL native result\n");
        _ = svc(3, 1, 0, 0, 0);
        while (true) {}
    }
}

fn write(path: []const u8, body: []const u8) void {
    const fd = svc(23, @intFromPtr(path.ptr), path.len, 6, 0);
    if (fd < 0) expect(fd, 0);
    expect(svc(25, @intCast(fd), @intFromPtr(body.ptr), body.len, 0), @intCast(body.len));
    expect(svc(77, @intCast(fd), 0, 0, 0), 0);
    expect(svc(26, @intCast(fd), 0, 0, 0), 0);
}

fn read(path: []const u8, body: []const u8) void {
    read_checked(path, body, null);
}

fn read_checked(path: []const u8, body: []const u8, copy: ?[]const u8) void {
    const fd = svc(23, @intFromPtr(path.ptr), path.len, 1, 0);
    if (fd < 0) expect(fd, 0);
    var bytes: [128]u8 = undefined;
    expect(svc(24, @intCast(fd), @intFromPtr(&bytes), bytes.len, 0), @intCast(body.len));
    for (body, 0..) |byte, i| expect(bytes[i], byte);
    expect(svc(26, @intCast(fd), 0, 0, 0), 0);
    if (copy) |copy_path| write(copy_path, bytes[0..body.len]);
}

fn rename(from: []const u8, to: []const u8, replace: bool) i64 {
    const length = from.len | (if (replace) replace_bit else @as(u64, 0));
    return svc(35, @intFromPtr(from.ptr), length, @intFromPtr(to.ptr), to.len);
}

export fn b4_main() noreturn {
    write("/host/B4/stage", "first publication\n");
    expect(rename("/host/B4/stage", "/host/B4/output", true), 0);
    read_checked("/host/B4/output", "first publication\n", "/host/B4/first.copy");
    write("/host/B4/stage", "second publication\n");
    expect(rename("/host/B4/stage", "/host/B4/output", true), 0);
    read("/host/B4/output", "second publication\n");
    say("b4: two publications ok\n");

    write("/host/B4/stage", "retained stage\n");
    expect(rename("/host/B4/stage", "/host/B4/output", false), -9);
    read("/host/B4/output", "second publication\n");
    read("/host/B4/stage", "retained stage\n");
    expect(rename("/host/B4/stage", "/host/B4/fresh", false), 0);
    read("/host/B4/fresh", "retained stage\n");
    say("b4: no-overwrite EEXIST and fresh rename ok\n");

    // Inject a missing-stage failure, then an actual backend type conflict.
    expect(rename("/host/B4/missing", "/host/B4/output", true), -6);
    expect(rename("/host/B4/directory", "/host/B4/output", true), -1);
    read("/host/B4/output", "second publication\n");
    say("b4: replacement ENOENT and EINVAL preserve output\n");

    // Four both-end policy failures, through the actual native syscall.
    expect(rename("/host/B4/denied-src", "/host/B4/output", true), -7);
    write("/host/B4/stage", "retained stage\n");
    expect(rename("/host/B4/stage", "/host/B4/denied-dst", true), -7);
    expect(rename("/host/B4/secret-src", "/host/B4/output", true), -7);
    expect(rename("/host/B4/stage", "/host/B4/secret-dst", true), -7);
    // The preserve-existing operation has the same policy, not a bypass.
    expect(rename("/host/B4/denied-src", "/host/B4/output", false), -7);
    expect(rename("/host/B4/stage", "/host/B4/denied-dst", false), -7);
    read("/host/B4/stage", "retained stage\n");
    read("/host/B4/output", "second publication\n");
    say("b4: both-end permission and secret EACCES ok\n");

    const source = "/host/B4/stage";
    const destination = "/host/B4/output";
    expect(svc(35, @intFromPtr(source.ptr), source.len | replace_bit | (@as(u64, 1) << 62),
        @intFromPtr(destination.ptr), destination.len), -1);
    expect(svc(35, 0xfffffffffffff000, source.len | replace_bit,
        @intFromPtr(destination.ptr), destination.len), -3);
    expect(svc(35, @intFromPtr(source.ptr), source.len | replace_bit,
        0xfffffffffffff000, destination.len), -3);
    read("/host/B4/output", "second publication\n");
    write("/host/B4/receipt", "publications=2 refusals=12 failed=0\n");
    say("b4: PASS publications=2 refusals=12\n");
    _ = svc(3, 0, 0, 0, 0);
    while (true) {}
}

export fn _start() callconv(.naked) noreturn {
    asm volatile (
        \\bl b4_main
    );
}
EOF

vgate_file script-publish.txt <<'EOF'
settings set hostname b4-first
settings set hostname b4-second
exec B4PUB.ELF
EOF

vgate_file script-publish-done.txt <<'EOF'
echo done-b4-publication
EOF

vgate_file b4-first.txt <<'EOF'
first publication
EOF
vgate_file b4-second.txt <<'EOF'
second publication
EOF
vgate_file b4-stage.txt <<'EOF'
retained stage
EOF
vgate_file b4-receipt.txt <<'EOF'
publications=2 refusals=12 failed=0
EOF

vgate_setup_python <<'PY'
import os, subprocess
rd = os.environ["RUN_DIR"]
share = os.environ.get("VG_SHARE") or os.path.join(rd, "share")
subprocess.run([
    "zig", "build-exe", "-target", "aarch64-freestanding-none",
    "-O", "ReleaseSafe", "-fstrip", "-fno-stack-check", "-T", os.path.join(rd, "publish.ld"),
    os.path.join(rd, "publish.zig"), "-femit-bin=" + os.path.join(share, "B4PUB.ELF")
], check=True)
# Disjoint per-backend fixtures: the VirtioFS run also attaches the legacy
# share, so wrong backend dispatch cannot pass the independent host checks.
policy = ("#v1\n"
          "B4/denied-src\t400\t1000\t-\n"
          "B4/denied-dst\t400\t1000\t-\n"
          "B4/secret-src\t600\t0\tsecret\n"
          "B4/secret-dst\t600\t0\tsecret\n"
          "B4/stage\t600\t1000\t-\n"
          "B4/output\t606\t0\t-\n")
for root in (share, os.path.join(rd, "virtiofs")):
    os.makedirs(os.path.join(root, "B4", "directory"), exist_ok=True)
    if root != share:
        import shutil
        shutil.copy(os.path.join(share, "B4PUB.ELF"), os.path.join(root, "B4PUB.ELF"))
    for name, body in (("output", "original publication\n"),
                       ("denied-src", "denied source\n"),
                       ("denied-dst", "denied destination\n"),
                       ("secret-src", "secret source\n"),
                       ("secret-dst", "secret destination\n")):
        open(os.path.join(root, "B4", name), "w").write(body)
    open(os.path.join(root, "OWNERS.TXT"), "w").write(policy)
print("B4 native probe bytes=%d; no SDK dependencies or extra handles" %
      os.path.getsize(os.path.join(share, "B4PUB.ELF")))
PY

vgate_run publish-legacy -- --script '$RUN_DIR/script-publish.txt' \
    --script-after 'tasks user-el0 exited status=7' \
    --script2 '$RUN_DIR/script-publish-done.txt' \
    --script2-after 'tasks user-exec exited status=0' \
    --script-expect 'done-b4-publication' --timeout 60
vgate_assert publish-legacy serial-contains 'b4: PASS publications=2 refusals=12'
vgate_assert publish-legacy serial-contains 'settings: hostname=b4-first (persisted)'
vgate_assert publish-legacy serial-contains 'settings: hostname=b4-second (persisted)'
vgate_assert publish-legacy serial-absent 'b4: FAIL'
vgate_assert publish-legacy serial-absent '[EXC] parking'
vgate_assert publish-legacy output-contains 'VF-FILE: REPLACE B4/stage'
vgate_assert publish-legacy share-equals B4/first.copy $'first publication\n'
vgate_assert publish-legacy share-equals B4/output $'second publication\n'
vgate_assert publish-legacy share-equals B4/stage $'retained stage\n'
vgate_assert publish-legacy share-equals B4/fresh $'retained stage\n'
vgate_assert publish-legacy share-equals B4/receipt b4-receipt.txt
vgate_assert publish-legacy share-equals B4/denied-src $'denied source\n'
vgate_assert publish-legacy share-equals B4/denied-dst $'denied destination\n'
vgate_assert publish-legacy share-equals B4/secret-src $'secret source\n'
vgate_assert publish-legacy share-equals B4/secret-dst $'secret destination\n'
vgate_assert publish-legacy share-contains OWNERS.TXT $'B4/secret-dst\t600\t0\tsecret'
vgate_assert publish-legacy share-contains SETTINGS.TXT 'hostname=b4-second'
vgate_assert publish-legacy python <<'PY'
import os
root = os.environ["VG_SHARE"]
assert os.path.isdir(os.path.join(root, "B4", "directory"))
assert open(os.path.join(os.environ["RUN_DIR"], "virtiofs", "B4", "output"), "rb").read() == b"original publication\n"
body = open(os.path.join(root, "OWNERS.TXT")).read()
for entry in ("B4/denied-src\t400\t1000\t-", "B4/denied-dst\t400\t1000\t-",
              "B4/secret-src\t600\t0\tsecret", "B4/secret-dst\t600\t0\tsecret"):
    assert entry in body, entry
assert "B4/stage\t" not in body
assert "B4/output\t" not in body  # Second stage had implicit/default metadata.
print("legacy: both publications, retained failure source, policies and backend dispatch verified")
PY

vgate_run publish-virtiofs -- --virtio-fs '$RUN_DIR/virtiofs' \
    --script '$RUN_DIR/script-publish.txt' \
    --script-after 'tasks user-el0 exited status=7' \
    --script2 '$RUN_DIR/script-publish-done.txt' \
    --script2-after 'tasks user-exec exited status=0' \
    --script-expect 'done-b4-publication' --timeout 60
vgate_assert publish-virtiofs serial-contains 'b4: PASS publications=2 refusals=12'
vgate_assert publish-virtiofs serial-contains 'settings: hostname=b4-first (persisted)'
vgate_assert publish-virtiofs serial-contains 'settings: hostname=b4-second (persisted)'
vgate_assert publish-virtiofs serial-absent 'b4: FAIL'
vgate_assert publish-virtiofs serial-absent '[EXC] parking'
vgate_assert publish-virtiofs serial-contains 'virtio-fs: ready did=0x105a'
vgate_assert publish-virtiofs python <<'PY'
import os, shutil
rd = os.environ["RUN_DIR"]
root = os.path.join(rd, "virtiofs")
expected = {
    "first.copy": b"first publication\n", "output": b"second publication\n",
    "stage": b"retained stage\n", "fresh": b"retained stage\n",
    "receipt": b"publications=2 refusals=12 failed=0\n",
    "denied-src": b"denied source\n", "denied-dst": b"denied destination\n",
    "secret-src": b"secret source\n", "secret-dst": b"secret destination\n",
}
for name, want in expected.items():
    path = os.path.join(root, "B4", name)
    assert open(path, "rb").read() == want, name
    shutil.copy(path, os.path.join(rd, "virtiofs-" + name))
assert os.path.isdir(os.path.join(root, "B4", "directory"))
body = open(os.path.join(root, "OWNERS.TXT")).read()
assert "B4/output\t" not in body
assert "B4/stage\t" not in body
for entry in ("B4/denied-src\t400\t1000\t-", "B4/denied-dst\t400\t1000\t-",
              "B4/secret-src\t600\t0\tsecret", "B4/secret-dst\t600\t0\tsecret"):
    assert entry in body, entry
shutil.copy(os.path.join(root, "OWNERS.TXT"), os.path.join(rd, "virtiofs-owners"))
assert "hostname=b4-second\n" in open(os.path.join(root, "SETTINGS.TXT")).read()
print("VirtioFS: 9 independent byte comparisons, directory/source preservation and both-end policy verified")
PY
vgate_assert publish-virtiofs capture-equals virtiofs-first.copy b4-first.txt
vgate_assert publish-virtiofs capture-equals virtiofs-output b4-second.txt
vgate_assert publish-virtiofs capture-equals virtiofs-stage b4-stage.txt
vgate_assert publish-virtiofs capture-equals virtiofs-fresh b4-stage.txt
vgate_assert publish-virtiofs capture-equals virtiofs-receipt b4-receipt.txt
