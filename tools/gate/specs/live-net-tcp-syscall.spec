# live-net-tcp-syscall.spec -- TCP.BIN (exec'd) drives the TCP
# syscall seam from EL0: connect, echo round trip, exit 18; the
# observation phase re-reads slots 30-33 with real call counts.
# The separate B6 fixture replays injected frames/time at EL0, not live
# socket routing. It uses only existing file/console/exit calls.
# exec-order: assert-proven -- the core observation script waits for reap;
# byte assertions require the fixture's own intake and computed packets.

vgate_name live-net-tcp-syscall "TCP.BIN drives slots 30-33 from EL0 on VZ"
vgate_share seed
vgate_runner_flags -Xswiftc -DSPIKE
vgate_note "B6 injected-core execution is not native socket integration or Boris preview"

vgate_setup_python <<'PY'
import json, os, pathlib, subprocess
rd = pathlib.Path(os.environ["RUN_DIR"])
share = pathlib.Path(os.environ.get("VG_SHARE") or rd / "share")
# Two independent build caches; private pinned compiler, no dependency download.
images = []
for n in (1, 2):
    output = rd / ("core-%d.BIN" % n)
    subprocess.run(["python3", "tools/zig/socket_core.py", "--work", str(rd / ("core-build-%d" % n)),
                    "--output", str(output)], check=True)
    images.append(output.read_bytes())
assert images[0] == images[1], "socket-core fixture is not reproducible"
(share / "SOCKCORE.BIN").write_bytes(images[0])
(share / "SOCKET.IN").write_bytes(b"host-seeded socket bytes, not embedded\n")
receipt = json.loads((rd / "core-1.BIN.json").read_text())
assert receipt["evidence"] == "injected-core-only"
print("socket-core: two identical offline builds sha256=" + receipt["artifact_sha256"])
PY

vgate_file script-1.txt <<'EOF'
net ip 10.0.0.1
net arp 10.0.0.2
exec TCP.BIN
echo tcp-syscall-ready
EOF

vgate_file script-2.txt <<'EOF'
syscalls
tasks
net
echo net-tcp-ok
EOF

vgate_run 01 -- --net '$RUN_DIR/cap.bin' --net-arp-respond 10.0.0.2 --net-tcp-respond 10.0.0.2:9999 --script '$RUN_DIR/script-1.txt' --script2 '$RUN_DIR/script-2.txt' --script2-after 'tcp: got echo hello' --script-expect 'tasks user-exec reaped' --timeout 90

vgate_assert 01 serial-contains 'net ip: ip=10.0.0.1'
vgate_assert 01 serial-contains 'tcp: connected'
vgate_assert 01 serial-contains 'tcp: got echo hello'
vgate_assert 01 serial-contains 'procs TCP.BIN exited status=18'
vgate_assert 01 serial-contains 'tasks user-exec exited status=18'
vgate_assert 01 serial-contains 'tasks user-exec reaped'
vgate_assert 01 serial-contains 'echo net-tcp-ok'
vgate_assert 01 output-contains "NET-TCP: answered the guest's SYN"
vgate_assert 01 output-contains "NET-TCP: echoed the guest's 5-byte data"
vgate_assert 01 output-contains "NET-TCP: answered the guest's FIN"
vgate_assert 01 output-contains 'net-tcp-respond: ENABLED'
vgate_assert 01 python <<'PY'
import os, re, sys
ser = open(os.environ["VG_SER"], errors="replace").read()
lines = ser.splitlines()
# Legacy -E: the syscall report shape (counts drift -- rows below).
if not re.search(r"syscalls: slots=[0-9]+ implemented=[0-9]+", ser):
    sys.exit("FAIL: no syscalls report")
# Markers IN ORDER: connected, echo, procs-exit.
pos = []
for m in ("tcp: connected", "tcp: got echo hello", "procs TCP.BIN exited status=18"):
    idx = next((i for i, l in enumerate(lines) if m in l), None)
    if idx is None:
        sys.exit("FAIL: marker absent: %s" % m)
    pos.append(idx)
if any(b <= a for a, b in zip(pos, pos[1:])):
    sys.exit("FAIL: markers out of order: %s" % list(zip(pos)))
# Seam rows 30-33 show real call counts (present, never "=0").
for row in ("  30 sys_tcp_connect calls=", "  31 sys_tcp_send calls=",
            "  32 sys_tcp_recv calls=", "  33 sys_tcp_close calls="):
    if row not in ser or (row + "0") in ser:
        sys.exit("FAIL: seam row missing or zero: %r" % row)
print("tcp-syscall order + rows ok")
PY

vgate_file core.txt <<'EOF'
exec SOCKCORE.BIN
EOF

vgate_file core-after.txt <<'EOF'
syscalls
procs
echo socket-core-observed
EOF

# No --net on this boot: injected frames must not be confused with live wire I/O.
vgate_run core -- --script '$RUN_DIR/core.txt' --script2 '$RUN_DIR/core-after.txt' \
    --script2-after 'tasks user-exec reaped' --script-expect 'socket-core-observed' --timeout 90

vgate_assert core serial-exact 'socket-core: injected-only done' 1
vgate_assert core serial-contains 'procs SOCKCORE.BIN exited status=0'
vgate_assert core serial-contains 'socket-core: ownership capacity refusal cleanup ok'
vgate_assert core serial-contains 'socket-core: partial reads FIN reset terminal charging ok'
vgate_assert core serial-contains 'socket-core: retry ceiling unexpected SYN-ACK guards ok'
vgate_assert core serial-contains 'socket-core: four deadline boundaries ok'
vgate_assert core serial-contains 'socket-core: five death states unsupported DNS connect ok'
vgate_assert core serial-absent 'socket-core: FAIL'
vgate_assert core serial-absent '[EXC] parking:'
vgate_assert core python <<'PY'
import os, pathlib, re, socket, struct
rd = pathlib.Path(os.environ["RUN_DIR"])
ser = pathlib.Path(os.environ["VG_SER"]).read_text()
seed = b"host-seeded socket bytes, not embedded\n"
def emitted(name):
    values = re.findall(r"^socket-core: " + name + r"=([0-9a-f]+)$", ser, re.M)
    assert len(values) == 1, "missing/duplicate " + name
    return bytes.fromhex(values[0])
def checksum(data):
    if len(data) % 2: data += b"\0"
    total = sum(struct.unpack("!%dH" % (len(data) // 2), data))
    while total >> 16: total = (total & 65535) + (total >> 16)
    return (~total) & 65535
def packet(dst_ip, dst_port, seq, ack, flags, body=b"", window=1460):
    header = struct.pack("!HHIIBBHHH", 8090, dst_port, seq, ack, 0x50, flags, window, 0, 0)
    pseudo = socket.inet_aton("10.0.0.1") + socket.inet_aton(dst_ip) + struct.pack("!BBH", 0, 6, len(header) + len(body))
    header = header[:16] + struct.pack("!H", checksum(pseudo + header + body)) + header[18:]
    return header + body
assert emitted("intake") == seed and emitted("read") == seed
assert emitted("synack") == packet("10.0.0.2", 5000, 100, 11, 0x12)
expected = packet("10.0.0.2", 5000, 101, 11 + len(seed), 0x10, seed)
assert emitted("data") == expected and emitted("retransmit") == expected
# Different IP, not just another port on the original peer host.
assert emitted("refusal") == packet("10.0.0.9", 6000, 0, 0, 0x14, window=4096)
assert 0 < int(re.search(r"socket-core: core\+scratch=(\d+)", ser)[1]) <= 16384
assert 0 < int(re.search(r"socket-core: stack-high-water=(\d+)", ser)[1]) <= 131072
assert int(re.search(r"socket-core: checks=(\d+)", ser)[1]) >= 50
for slot, name in ((30, "sys_tcp_connect"), (31, "sys_tcp_send"), (32, "sys_tcp_recv"),
                   (33, "sys_tcp_close"), (76, "sys_sock_ready")):
    assert re.search(r"^\s+%d %s calls=0$" % (slot, name), ser, re.M), "unexpected networking syscall"
print("injected core: independent packet/seed checks, bounds, and zero net syscall counts")
PY
