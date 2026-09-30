# live-ssh-packet.spec -- M51 SSH1 (#1168): the guest stream adapter
# reassembles ONE SSH binary packet that the deterministic host responder
# sends split across many ≤192-byte TCP segments, paced one segment per
# guest ACK (the kernel's RX is one bounded slot with no reassembly). The
# guest echoes a SHA-256 over the reassembled payload; the python hook
# re-parses the SAME bytes it handed the responder and checks the digest,
# so framing + reassembly are proven, not just "some bytes arrived".
# Hermetic via --net. Class B (Apple silicon VZ).

vgate_name live-ssh-packet "SSH1 stream adapter reassembles a split SSH packet on VZ"
vgate_share seed
vgate_runner_flags -Xswiftc -DSPIKE
vgate_fmt user/src/lib/ssh/wire.zig user/src/lib/ssh/packet.zig user/src/lib/ssh/stream.zig user/src/sshpacket.zig build.zig

vgate_file script-1.txt <<'EOF'
net ip 10.0.0.1
net arp 10.0.0.2
exec SSHPACKET.BIN
echo sshpacket-launched
EOF

vgate_file script-2.txt <<'EOF'
procs
echo sshpacket-ok
EOF

# The known packet: a 1500-byte payload framed exactly as packet.zig expects
# (4-byte length, >=4 padding to an 8-byte block), 1512 bytes total -> 8
# paced 192-byte segments. The expected digest line is written for the
# serial-contains-file assert.
vgate_setup_python <<'PY'
import os, struct, hashlib
rd = os.environ["RUN_DIR"]
payload = bytes([0x14]) + bytes(((i * 31 + 7) & 0xff) for i in range(1499))
pad = 8 - ((4 + 1 + len(payload)) % 8)
if pad < 4:
    pad += 8
packet_length = 1 + len(payload) + pad
frame = struct.pack(">I", packet_length) + bytes([pad]) + payload + bytes([0x5a]) * pad
assert (4 + packet_length) % 8 == 0, "frame not block-aligned"
assert len(frame) > 192 * 7, "packet must split across many segments"
open(os.path.join(rd, "ssh-packet.bin"), "wb").write(frame)
digest = hashlib.sha256(payload).hexdigest()
open(os.path.join(rd, "ssh-packet-expected.txt"), "w").write(
    "sshpacket: len=%d sha256=%s" % (len(payload), digest))
PY

vgate_run 01 -- --net '$RUN_DIR/cap.bin' --net-arp-respond 10.0.0.2 --net-tcp-respond 10.0.0.2:2222:packet --net-tcp-respond-payload '$RUN_DIR/ssh-packet.bin' --script '$RUN_DIR/script-1.txt' --script2 '$RUN_DIR/script-2.txt' --script2-after 'sshpacket: len=' --script-expect 'tasks user-exec reaped' --timeout 60

vgate_assert 01 output-contains 'net-tcp-respond: ENABLED'
vgate_assert 01 output-contains 'net-tcp-respond mode: packet'
vgate_assert 01 output-contains 'NET-TCP: packet mode sent chunk 8/8'
vgate_assert 01 serial-contains 'sshpacket: starting'
vgate_assert 01 serial-contains 'sshpacket: connected'
vgate_assert 01 serial-contains-file ssh-packet-expected.txt
vgate_assert 01 serial-absent '[EXC] parking:'
vgate_assert 01 python <<'PY'
import os, sys, struct, hashlib
ser = open(os.environ["VG_SER"], errors="replace").read()
frame = open(os.path.join(os.environ["RUN_DIR"], "ssh-packet.bin"), "rb").read()
packet_length = struct.unpack(">I", frame[:4])[0]
pad = frame[4]
payload = frame[5:5 + packet_length - 1 - pad]
want = "sshpacket: len=%d sha256=%s" % (len(payload), hashlib.sha256(payload).hexdigest())
line = None
for l in ser.splitlines():
    if "sshpacket: len=" in l and "sha256=" in l:
        line = l
if line is None:
    sys.exit("FAIL: no sshpacket digest line in serial")
if want not in line:
    sys.exit("FAIL: reassembly digest mismatch: serial=%r want=%r" % (line.strip(), want))
print("sshpacket reassembly digest ok")
PY
