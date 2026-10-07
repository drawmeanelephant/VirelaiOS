# go-net.spec -- live File/Conn, DNS, loopback and fail-closed TCP waits.
# Runs 01/02 prove GET/read refusal with a surviving goroutine heartbeat.
# Runs 03/04/05 prove vi DNS+TCP, UDP loopback and legacy connect refusal.
# The two connect rows measure guest physical-counter-derived nanoseconds:
# explicit 5 s ETIMEDOUT and legacy 30 s EINVAL, followed by a successful
# connect/GET/close in the SAME process, proving lock and slot cleanup.
# --net is an isolated datagram attachment, NOT NAT or a host TCP socket.
# Only port 8080 has a responder; SYN to 8081 cannot receive any reply.
# Captured guest TX proves SYN/retransmit bytes reached that fixture.
# GOVINET's heartbeat is counter-paced here, not scheduler-tick sleeping:
# connect masks its core's IRQs, but must not stop a runnable peer goroutine.
# Prerequisite binaries: just go-gonet + just go-govinet.
# exec-order: assert-proven -- observation waits for the fixture's done marker.

vgate_name go-net "Live File/Conn, vi DNS/loopback, fail-closed reads, and bounded outbound connect with legacy 30 s regression"
vgate_share seed
vgate_runner_flags -Xswiftc -DSPIKE

vgate_file script.txt <<'EOF'
net ip 10.0.0.1
net arp 10.0.0.2
exec GONET.ELF
EOF

vgate_file script-vidns.txt <<'EOF'
net ip 10.0.0.1
net arp 10.0.0.2
exec GOVIDNS.ELF
EOF

vgate_file script-viloop.txt <<'EOF'
net ip 10.0.0.1
exec GOVINET.ELF viloop
EOF

vgate_file script-viclosed.txt <<'EOF'
net ip 10.0.0.1
net arp 10.0.0.2
exec GOVINET.ELF viclosed
EOF

vgate_file script-connect.txt <<'EOF'
net ip 10.0.0.1
net arp 10.0.0.2
exec GOCONNECT.ELF
EOF

vgate_file script-connect-legacy.txt <<'EOF'
net ip 10.0.0.1
net arp 10.0.0.2
exec GOCONNECT.ELF legacy
EOF

vgate_file script-connect-after.txt <<'EOF'
syscalls
net
echo netconnect-observed
EOF

vgate_file connect.go <<'EOF'
package main

import (
    "time"
    "virelai/vi"
    "virelai/vsys"
)

func fail(s string) {
    vsys.Println("netconnect: FAIL " + s)
    vsys.Exit(1)
}

func main() {
    legacy := false
    for _, arg := range vi.Args() {
        if arg == "legacy" { legacy = true }
    }
    mode := "bounded"
    if legacy { mode = "legacy" }
    vsys.Println("netconnect: dialing " + mode)
    begin := vi.Nanos()
    var c *vi.Conn
    var err error
    if legacy {
        c, err = vi.Dial("10.0.0.2", 8081)
    } else {
        c, err = vi.DialTimeout("10.0.0.2", 8081, 5*time.Second)
    }
    end := vi.Nanos()
    if c != nil || err == nil { fail("silent peer connected") }
    vsys.Println("netconnect: elapsed mode=" + mode + " begin_ns=" + vsys.Itoa64(begin) + " end_ns=" + vsys.Itoa64(end) + " err=" + err.Error())
    _, rc := vi.TCPReady()
    if rc != -vi.ErrEAGAIN { fail("socket owner remains") }
    vsys.Println("netconnect: no owner")
    if rc = vi.TCPClose(); rc != 0 { fail("close after timeout") }
    if legacy {
        c, err = vi.Dial("10.0.0.2", 8080)
    } else {
        c, err = vi.DialTimeout("10.0.0.2", 8080, 5*time.Second)
    }
    if err != nil { fail("reconnect " + err.Error()) }
    vsys.Println("netconnect: reconnected")
    if _, err = c.Send([]byte("GET / HTTP/1.0\r\n\r\n")); err != nil { fail("send") }
    c.SetRecvDeadline(5_000_000_000)
    buf := make([]byte, vi.TCPPayloadMax)
    n, err := c.Recv(buf)
    if err != nil || n < 15 || string(buf[:15]) != "HTTP/1.0 200 OK" { fail("GET response") }
    if err = c.Close(); err != nil { fail("close reconnect") }
    vsys.Println("netconnect: GET and close ok")
    vsys.Println("netconnect: done")
    vsys.Exit(0)
}
EOF

vgate_setup_python <<'PY'
import os, pathlib, subprocess
rd = pathlib.Path(os.environ["RUN_DIR"])
share = pathlib.Path(os.environ.get("VG_SHARE") or rd / "share")
tmp = rd / "connect-build"
tmp.mkdir()
env = dict(os.environ, TMPDIR=str(tmp), GO_BUILD_OUT=str(share),
           GO_BUILD_NAME="GOCONNECT", GO_LDFLAGS_VALUE="-s -w")
subprocess.run(["bash", "tools/go/build-go.sh", str(rd / "connect.go")], env=env, check=True)
PY

vgate_setup_python <<'PY'
import os, shutil, sys
rd = os.environ["RUN_DIR"]
share = os.environ.get("VG_SHARE") or os.path.join(rd, "share")
src = os.path.join(".build", "go", "GONET.ELF")
if not os.path.exists(src):
    sys.exit("GONET.ELF missing (expected " + src + ") - build it first: just go-gonet")
shutil.copy(src, os.path.join(share, "GONET.ELF"))
for name in ("GOVINET.ELF", "GOVIDNS.ELF"):
    src2 = os.path.join(".build", "go", name)
    if not os.path.exists(src2):
        sys.exit(name + " missing (expected " + src2 + ") - build it first: just go-govinet")
    shutil.copy(src2, os.path.join(share, name))
share_file = os.path.join(share, "GONET.SHARE")
with open(share_file, "w") as f:
    f.write("gonet-share-hello\n")
print("staged GONET.ELF (%d bytes), GOVINET.ELF (%d bytes), GOVIDNS.ELF (%d bytes), GONET.SHARE (%d bytes)"
      % (os.path.getsize(os.path.join(share, "GONET.ELF")),
         os.path.getsize(os.path.join(share, "GOVINET.ELF")),
         os.path.getsize(os.path.join(share, "GOVIDNS.ELF")),
         os.path.getsize(share_file)))
PY

vgate_setup_python <<'PY'
import os, pathlib, subprocess
rd = pathlib.Path(os.environ["RUN_DIR"])
share = pathlib.Path(os.environ.get("VG_SHARE") or rd / "share")
# Keep the original fixture's phases and assertions. Only its heartbeat
# pacing changes: a sleeping peer depends on timer IRQs on the dialing
# core, which slot 30 masks. A runnable peer on the other Go M instead
# samples the physical counter and cooperatively yields its kernel task.
source = pathlib.Path("tools/go/govinet.go").read_text()
needle = "\t\tvsys.Sleep(2)\n"
assert source.count(needle) == 1, "GOVINET heartbeat pacing anchor changed"
source = source.replace(needle, """\t\tdeadline := vsys.Nanotime() + 2_000_000_000
\t\tfor vsys.Nanotime() < deadline && !*stop {
\t\t\tvi.Yield()
\t\t}
""")
fixture = rd / "govinet-counter.go"
fixture.write_text(source)
env = dict(os.environ, TMPDIR=str(rd / "connect-build"), GO_BUILD_OUT=str(share),
           GO_BUILD_NAME="GOVINET", GO_LDFLAGS_VALUE="-s -w")
subprocess.run(["bash", "tools/go/build-go.sh", str(fixture)], env=env, check=True)
print("GOVINET heartbeat rebuilt with physical-counter pacing; original phases and assertions unchanged")
PY

# Run 01 -- the peer answers: the full N10 responder on 8080.
vgate_run 01 -- --net '$RUN_DIR/cap.bin' --net-arp-respond 10.0.0.2 \
    --net-tcp-respond 10.0.0.2:8080 \
    --script '$RUN_DIR/script.txt' --script-expect 'gonet OK' --timeout 120

# Run 02 -- the peer goes dark: SYN-ACK, then silence on data.
vgate_run 02 -- --net '$RUN_DIR/cap2.bin' --net-arp-respond 10.0.0.2 \
    --net-tcp-respond 10.0.0.2:8080:handshake \
    --script '$RUN_DIR/script.txt' --script-expect 'gonet OK' --timeout 120

vgate_assert 01 serial-contains 'exec: loaded GONET.ELF'
vgate_assert 01 serial-contains 'gonet: start'
vgate_assert 01 serial-contains 'gonet: readfile n='
vgate_assert 01 serial-contains 'gonet: connected'
vgate_assert 01 serial-contains 'gonet: wrote GET n='
vgate_assert 01 serial-contains 'gonet: hb='
vgate_assert 01 serial-contains 'gonet: body total='
vgate_assert 01 serial-contains 'gonet: heartbeat survived load'
vgate_assert 01 serial-contains 'gonet OK'
vgate_assert 01 output-contains "NET-TCP: answered the guest's HTTP request with 200 OK"
vgate_assert 01 serial-absent '[EXC] parking:'
# Phase 2.1: the EL0 clock is live on target (a 0/dead clock fails here).
vgate_assert 01 serial-contains 'gonet: clock ok'
vgate_assert 01 serial-absent 'gonet: clock DEAD'
vgate_assert 01 serial-contains 'gonet: failclosed ms='

vgate_assert 02 serial-contains 'gonet: connected'
vgate_assert 02 serial-contains 'gonet: hb='
vgate_assert 02 serial-contains 'gonet: read failed closed err='
# Phase 2.1: the same clock proof on the peer-goes-dark run.
vgate_assert 02 serial-contains 'gonet: clock ok'
vgate_assert 02 serial-absent 'gonet: clock DEAD'
vgate_assert 02 serial-contains 'gonet: failclosed ms='
vgate_assert 02 serial-contains 'gonet: heartbeat survived load'
vgate_assert 02 serial-contains 'gonet OK'
vgate_assert 02 output-contains "NET-TCP: answered the guest's SYN"
# The ORDER proof: a heartbeat line must appear AFTER the fail-closed line,
# i.e. the second goroutine kept running while the read was failing.
vgate_assert 02 python <<'PY'
import os, sys
ser = open(os.environ["VG_SER"], errors="replace").read()
lines = ser.splitlines()
fail = next((i for i, l in enumerate(lines) if "gonet: read failed closed err=" in l), None)
if fail is None:
    sys.exit("FAIL: no fail-closed marker (the peer went dark but the read did not fail closed)")
after = [i for i, l in enumerate(lines) if "gonet: hb=" in l and i > fail]
if not after:
    sys.exit("FAIL: the heartbeat did NOT continue after the read failed closed")
before = [i for i, l in enumerate(lines) if "gonet: hb=" in l and i < fail]
if not before:
    sys.exit("FAIL: the heartbeat never ran before the read blocked")
print("go-net order ok: %d heartbeats before the fail-closed line, %d after"
      % (len(before), len(after)))
PY

# --- M67a (#1446): the vi socket surface, the N5 bar -----------------------

# Run 03 -- the host round trip through vi: DNS-resolve a NAME (the host
# responder answers myhost.local -> 10.0.0.2), then Dial/Send/Recv the GET.
vgate_run 03 -- --net '$RUN_DIR/cap3.bin' --net-arp-respond 10.0.0.2 \
    --net-dns-respond 10.0.0.2:53 --net-tcp-respond 10.0.0.2:8080 \
    --script '$RUN_DIR/script-vidns.txt' --script-expect 'gonet OK' --timeout 120

vgate_assert 03 serial-contains 'govinet: vidns start'
vgate_assert 03 serial-contains 'govinet: vidns resolved myhost.local -> 10.0.0.2'
vgate_assert 03 serial-contains 'govinet: vidns connected'
vgate_assert 03 serial-contains 'govinet: vidns sent n='
vgate_assert 03 serial-contains 'govinet: vidns body total='
vgate_assert 03 serial-contains 'gonet OK'
vgate_assert 03 output-contains "NET-DNS: answered the guest's DNS query for 'myhost.local'"
vgate_assert 03 output-contains "NET-TCP: answered the guest's HTTP request with 200 OK"
vgate_assert 03 serial-absent 'govinet: FAIL'
vgate_assert 03 serial-absent '[EXC] parking:'

# Run 04 -- the loopback bar: the vi UDP seam's own-IP datagram comes back
# to the guest's own listen ring. No --net is armed: there is no device to
# leak onto, so a pass IS the loopback proof.
vgate_run 04 -- --script '$RUN_DIR/script-viloop.txt' --script-expect 'gonet OK' --timeout 120

vgate_assert 04 serial-contains 'govinet: viloop start'
vgate_assert 04 serial-contains 'govinet: viloop bound'
vgate_assert 04 serial-contains 'govinet: viloop sent n=4'
vgate_assert 04 serial-contains 'govinet: viloop echoed n=12'
vgate_assert 04 serial-contains 'gonet OK'
vgate_assert 04 serial-absent 'govinet: viloop no echo'
vgate_assert 04 serial-absent 'govinet: FAIL'

# Run 05 -- the closed-port drop: 8081 has no responder, so the connect
# waits in the kernel for its 30 s window and then refuses (rc = -1, the
# kernel's einval on connect timeout) while the heartbeat keeps printing.
vgate_run 05 -- --net '$RUN_DIR/cap5.bin' --net-arp-respond 10.0.0.2 \
    --net-tcp-respond 10.0.0.2:8080 \
    --script '$RUN_DIR/script-viclosed.txt' --script-expect 'gonet OK' --timeout 120

vgate_assert 05 serial-contains 'govinet: viclosed start'
vgate_assert 05 serial-contains 'govinet: viclosed dialing 10.0.0.2:8081'
vgate_assert 05 serial-contains 'govinet: viclosed refused rc=-1'
vgate_assert 05 serial-contains 'govinet: heartbeat survived load'
vgate_assert 05 serial-contains 'gonet OK'
vgate_assert 05 serial-absent 'govinet: FAIL'
vgate_assert 05 serial-absent '[EXC] parking:'
# The ORDER proof for a BLOCKING Dial: the heartbeat ran before the dial
# started, and — the point — it kept running across the 30 s refused dial.
vgate_assert 05 python <<'PY'
import os, sys
lines = open(os.environ["VG_SER"], errors="replace").read().splitlines()
dial = next((i for i, l in enumerate(lines) if "govinet: viclosed dialing" in l), None)
ref  = next((i for i, l in enumerate(lines) if "govinet: viclosed refused rc=" in l), None)
if dial is None or ref is None:
    sys.exit("FAIL: missing the dialing/refused markers")
before = [i for i, l in enumerate(lines) if "govinet: hb=" in l and i < dial]
during = [i for i, l in enumerate(lines) if "govinet: hb=" in l and dial < i < ref]
after  = [i for i, l in enumerate(lines) if "govinet: hb=" in l and i > ref]
if not before:
    sys.exit("FAIL: the heartbeat never ran before the dial")
if not during:
    sys.exit("FAIL: the heartbeat did NOT run during the 30 s refused dial — a blocking Dial wedged the process")
if not after:
    sys.exit("FAIL: the heartbeat did NOT continue after the refusal")
print("go-net viclosed order ok: %d beats before, %d during, %d after the refused dial"
      % (len(before), len(during), len(after)))
PY

# No injection, NAT, relay, or other TCP responder is armed. Port 8081
# goes unanswered by construction; the responder matches ONLY port 8080.
vgate_run connect -- --net '$RUN_DIR/connect.bin' --net-arp-respond 10.0.0.2 \
    --net-tcp-respond 10.0.0.2:8080 --script '$RUN_DIR/script-connect.txt' \
    --script2 '$RUN_DIR/script-connect-after.txt' --script2-after 'netconnect: done' \
    --script-expect 'netconnect-observed' --timeout 120

vgate_run connect-legacy -- --net '$RUN_DIR/connect-legacy.bin' --net-arp-respond 10.0.0.2 \
    --net-tcp-respond 10.0.0.2:8080 --script '$RUN_DIR/script-connect-legacy.txt' \
    --script2 '$RUN_DIR/script-connect-after.txt' --script2-after 'netconnect: done' \
    --script-expect 'netconnect-observed' --timeout 120

vgate_assert connect serial-contains 'netconnect: no owner'
vgate_assert connect serial-contains 'netconnect: reconnected'
vgate_assert connect serial-contains 'netconnect: GET and close ok'
vgate_assert connect serial-absent 'netconnect: FAIL'
vgate_assert connect serial-absent '[EXC] parking:'
vgate_assert connect output-contains "NET-TCP: answered the guest's HTTP request with 200 OK"
vgate_assert connect python <<'PY'
import os, pathlib, re, struct
rd = pathlib.Path(os.environ["RUN_DIR"])
ser = pathlib.Path(os.environ["VG_SER"]).read_text()
m = re.search(r"netconnect: elapsed mode=bounded begin_ns=(\d+) end_ns=(\d+) err=ETIMEDOUT", ser)
assert m, "missing guest counter samples or wrong timeout error"
elapsed = int(m[2]) - int(m[1])
assert 5_000_000_000 <= elapsed < 6_000_000_000, elapsed
assert re.search(r"tcp=idle,[^\n]*,timedout=1,", ser), "timeout singleton/counter not observed"
assert "  30 sys_tcp_connect calls=2" in ser and "  33 sys_tcp_close calls=2" in ser
data = (rd / "connect.bin").read_bytes()
suffix = os.environ.get("VIRELAI_GATE_SUFFIX", "")
pathlib.Path("artifacts", "go-net-connect-wire" + suffix + ".bin").write_bytes(data)
syns = []
i = 0
while i < len(data):
    assert data[i+12:i+14] in (b"\x08\x06", b"\x08\x00"), "unexpected capture frame"
    size = 42 if data[i+12:i+14] == b"\x08\x06" else 14 + struct.unpack_from("!H", data, i+16)[0]
    f = data[i:i+size]
    if f[12:14] == b"\x08\x00" and f[23] == 6 and f[37:38] == b"\x91" and f[36] == 0x1f:
        assert f[30:34] == bytes([10,0,0,2]) and f[47] == 2, "silent port transmitted something other than SYN"
        syns.append(f)
    i += size
assert len(syns) == 2 and syns[0] == syns[1], "expected initial SYN and one byte-identical 3 s retransmit"
print("bounded connect: guest counters %.6f s; two unanswered SYNs; clean owner/lock/GET/close" % (elapsed / 1e9))
PY

vgate_assert connect-legacy serial-contains 'netconnect: no owner'
vgate_assert connect-legacy serial-contains 'netconnect: GET and close ok'
vgate_assert connect-legacy serial-absent 'netconnect: FAIL'
vgate_assert connect-legacy serial-absent '[EXC] parking:'
vgate_assert connect-legacy output-contains "NET-TCP: answered the guest's HTTP request with 200 OK"
vgate_assert connect-legacy python <<'PY'
import os, pathlib, re, struct
rd = pathlib.Path(os.environ["RUN_DIR"])
ser = pathlib.Path(os.environ["VG_SER"]).read_text()
m = re.search(r"netconnect: elapsed mode=legacy begin_ns=(\d+) end_ns=(\d+) err=EINVAL", ser)
assert m, "missing guest counter samples or changed legacy timeout error"
elapsed = int(m[2]) - int(m[1])
assert 30_000_000_000 <= elapsed < 31_000_000_000, elapsed
assert re.search(r"tcp=idle,[^\n]*,timedout=1,", ser), "legacy timeout singleton/counter not observed"
assert "  30 sys_tcp_connect calls=2" in ser and "  33 sys_tcp_close calls=2" in ser
data = (rd / "connect-legacy.bin").read_bytes()
suffix = os.environ.get("VIRELAI_GATE_SUFFIX", "")
pathlib.Path("artifacts", "go-net-connect-legacy-wire" + suffix + ".bin").write_bytes(data)
syns = []
i = 0
while i < len(data):
    assert data[i+12:i+14] in (b"\x08\x06", b"\x08\x00"), "unexpected capture frame"
    size = 42 if data[i+12:i+14] == b"\x08\x06" else 14 + struct.unpack_from("!H", data, i+16)[0]
    f = data[i:i+size]
    if f[12:14] == b"\x08\x00" and f[23] == 6 and f[36:38] == b"\x1f\x91":
        assert f[30:34] == bytes([10,0,0,2]) and f[47] == 2
        syns.append(f)
    i += size
assert len(syns) == 10 and all(s == syns[0] for s in syns), "legacy SYN/retransmit sequence changed"
print("legacy connect: guest counters %.6f s; unchanged EINVAL; reconnect/GET/close" % (elapsed / 1e9))
PY
