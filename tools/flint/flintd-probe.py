#!/usr/bin/env python3
"""tools/flint/flintd-probe.py -- M214a (#3918) host driver for FLINTD.ELF.

Runs as the runner's ``--net-tcp-connect-stream`` child: stdin carries
guest->host bytes, stdout carries host->guest bytes, and stderr carries the
``FLINTD-PROBE:`` evidence lines the gate asserts on. It speaks FLINT-SYNC/1
(docs/flint-server-spec.md §2) against the in-guest daemon and records:

  * --transcript  every request line and reply line, in order (evidence)
  * --received    the raw Change line bytes the daemon read back, one per line
                  (byte-compared with the host-seeded .jsonl)

The probe deliberately exercises framing beyond one segment: a request over
1460 bytes, seven-byte fragments, two requests coalesced into one write, and a
100 KiB read reply. Signatures of every served Change are verified client-side
with the pinned RFC 8032 key, so the gate proves the daemon returned verifiable
canonical bytes rather than re-serialized JSON.
"""

import argparse
import hashlib
import json
import os
import struct
import sys

Q = 2**255 - 19
L = 2**252 + 27742317777372353535851937790883648493
D = (-121665 * pow(121666, Q - 2, Q)) % Q
I = pow(2, (Q - 1) // 4, Q)


def _inv(x):
    return pow(x, Q - 2, Q)


def _xrecover(y):
    xx = (y * y - 1) * _inv(D * y * y + 1)
    x = pow(xx, (Q + 3) // 8, Q)
    if (x * x - xx) % Q != 0:
        x = (x * I) % Q
    if x & 1:
        x = Q - x
    return x


_BY = (4 * _inv(5)) % Q
_B = (_xrecover(_BY), _BY)


# Extended twisted-Edwards coordinates (X:Y:Z:T), x=X/Z, y=Y/Z, T=XY/Z:
# one inversion per encoded point instead of two per group operation, so a
# 300-Change verification pass costs well under the daemon's idle budget.
def _ext(p):
    x, y = p
    return (x, y, 1, x * y % Q)


def _ext_add(p, q):
    x1, y1, z1, t1 = p
    x2, y2, z2, t2 = q
    a = (y1 - x1) * (y2 - x2) % Q
    b = (y1 + x1) * (y2 + x2) % Q
    c = t1 * 2 * D * t2 % Q
    d = z1 * 2 * z2 % Q
    e, f, g, h = b - a, d - c, d + c, b + a
    return (e * f % Q, g * h % Q, f * g % Q, e * h % Q)


def _ext_dbl(p):
    x1, y1, z1, _ = p
    a = x1 * x1 % Q
    b = y1 * y1 % Q
    c = 2 * z1 * z1 % Q
    d = -a % Q
    e = ((x1 + y1) * (x1 + y1) - a - b) % Q
    g = (d + b) % Q
    f = (g - c) % Q
    h = (d - b) % Q
    return (e * f % Q, g * h % Q, f * g % Q, e * h % Q)


def _ext_mul(p, e):
    q = (0, 1, 1, 0)
    while e:
        if e & 1:
            q = _ext_add(q, p)
        p = _ext_dbl(p)
        e >>= 1
    return q


def _ext_eq(p, q):
    return ((p[0] * q[2] - q[0] * p[2]) % Q == 0 and
            (p[1] * q[2] - q[1] * p[2]) % Q == 0)


def _ext_enc(p):
    x = p[0] * _inv(p[2]) % Q
    y = p[1] * _inv(p[2]) % Q
    return (y | ((x & 1) << 255)).to_bytes(32, "little")


def _dec_point(b):
    if len(b) != 32:
        return None
    y = int.from_bytes(b, "little")
    sign = y >> 255
    y &= (1 << 255) - 1
    if y >= Q:
        return None
    x = _xrecover(y)
    if (x & 1) != sign:
        x = Q - x
    return (x, y)


def _hint(m):
    return int.from_bytes(hashlib.sha512(m).digest(), "little")


def ed_public_key(seed):
    a = int.from_bytes(hashlib.sha512(seed).digest()[:32], "little")
    a &= (1 << 254) - 8
    a |= 1 << 254
    return _ext_enc(_ext_mul(_ext(_B), a))


def ed_sign(msg, seed):
    h = hashlib.sha512(seed).digest()
    a = int.from_bytes(h[:32], "little")
    a &= (1 << 254) - 8
    a |= 1 << 254
    pub = _ext_enc(_ext_mul(_ext(_B), a))
    r = _hint(h[32:] + msg) % L
    r_enc = _ext_enc(_ext_mul(_ext(_B), r))
    k = _hint(r_enc + pub + msg) % L
    return r_enc + ((r + k * a) % L).to_bytes(32, "little")


def ed_verify(msg, sig, pub):
    if len(sig) != 64 or len(pub) != 32:
        return False
    r = _dec_point(sig[:32])
    a = _dec_point(pub)
    s = int.from_bytes(sig[32:], "little")
    if r is None or a is None or s >= L:
        return False
    k = _hint(sig[:32] + pub + msg) % L
    return _ext_eq(_ext_mul(_ext(_B), s),
                   _ext_add(_ext(r), _ext_mul(_ext(a), k)))


# ---------------------------------------------------------------------------
# FLINT canonical bytes (ADR 0049)
# ---------------------------------------------------------------------------

DOMAIN_OPS = b"FLINT-OPS/1\x00"
DOMAIN_CHANGE = b"FLINT-CHANGE/1\x00"


def uuid_bytes(s):
    h = s.replace("-", "")
    if len(h) != 32:
        return None
    try:
        return bytes.fromhex(h)
    except ValueError:
        return None


def _opt_str(raw):
    if raw is None:
        return b"\x00"
    b = raw.encode("utf-8")
    return b"\x01" + struct.pack("<I", len(b)) + b


def _opt_uuid(raw):
    if raw is None:
        return b"\x00"
    b = uuid_bytes(raw)
    if b is None:
        raise ValueError("bad uuid %r" % raw)
    return b"\x01" + b


def ops_hash(ops):
    if not ops:
        return hashlib.sha256(DOMAIN_OPS + struct.pack("<I", 0)).digest()
    raise ValueError("probe verifies empty-op fixtures only")


def sig_msg(change):
    msg = bytearray(DOMAIN_CHANGE)
    msg += uuid_bytes(change["id"])
    msg += _opt_uuid(change.get("parent"))
    msg += uuid_bytes(change["stream"])
    msg += struct.pack("<Q", int(change["ts"]))
    msg += bytes.fromhex(change["author"])
    body = change["message"].encode("utf-8")
    msg += struct.pack("<I", len(body)) + body
    msg += _opt_uuid(change.get("merged"))
    msg += ops_hash(change.get("ops") or [])
    return bytes(msg)


def verify_change(line, pub_hex):
    change = json.loads(line)
    return ed_verify(sig_msg(change), bytes.fromhex(change["sig"]),
                     bytes.fromhex(pub_hex))


# ---------------------------------------------------------------------------
# NDJSON transport (one request in flight; replies may be fragmented)
# ---------------------------------------------------------------------------

class Wire:
    def __init__(self, transcript_path):
        self.rx = b""
        self.transcript = open(transcript_path, "w", encoding="utf-8")
        self.seq = 0

    def send_raw(self, data):
        out = sys.stdout.buffer
        out.write(data)
        out.flush()

    def request(self, obj):
        self.seq += 1
        obj = dict(obj)
        obj["seq"] = self.seq
        line = json.dumps(obj, separators=(",", ":")).encode("utf-8")
        self.transcript.write("> " + line.decode("utf-8") + "\n")
        self.transcript.flush()
        self.send_raw(line + b"\n")
        return self.seq, self.response(self.seq)

    def response(self, seq):
        while True:
            while b"\n" not in self.rx:
                chunk = os.read(0, 4096)
                if not chunk:
                    raise RuntimeError("guest closed before response seq=%d" % seq)
                self.rx += chunk
                if len(self.rx) > 4 * 1024 * 1024 + 2:
                    raise RuntimeError("response line exceeded its bound")
            line, self.rx = self.rx.split(b"\n", 1)
            if line.endswith(b"\r"):
                line = line[:-1]
            self.transcript.write("< " + line.decode("utf-8") + "\n")
            self.transcript.flush()
            obj = json.loads(line)
            got = obj.get("seq")
            if got != seq:
                raise RuntimeError("seq %r, want %d" % (got, seq))
            return obj

    def close(self):
        self.transcript.close()


def fail(msg):
    sys.stderr.write("FLINTD-PROBE: FAIL %s\n" % msg)
    sys.stderr.flush()
    sys.exit(1)


def note(msg):
    sys.stderr.write("FLINTD-PROBE: ok %s\n" % msg)
    sys.stderr.flush()


def expect_ok(wire, obj, what):
    if not obj.get("ok"):
        raise RuntimeError("%s refused: %s" % (what, obj.get("refuse")))


def expect_refuse(wire, obj, want, what):
    if obj.get("ok") or obj.get("refuse") != want:
        raise RuntimeError("%s: want refuse %s, got %r" % (what, want, obj))


def main(argv):
    ap = argparse.ArgumentParser()
    ap.add_argument("--store", required=True, help="host path of the guest share's FLINT store")
    ap.add_argument("--seed", required=True, help="host-seeded streams/<id>.jsonl to compare")
    ap.add_argument("--seed-file", required=True, help="pinned Ed25519 seed hex file")
    ap.add_argument("--stream-id", required=True)
    ap.add_argument("--stream-name", default="seed")
    ap.add_argument("--torn-stream", required=True)
    ap.add_argument("--big-stream", required=True)
    ap.add_argument("--transcript", required=True)
    ap.add_argument("--received", required=True)
    ap.add_argument("--page", type=int, default=256)
    args = ap.parse_args(argv)

    seed_hex = open(args.seed_file, encoding="utf-8").read().strip()
    seed = bytes.fromhex(seed_hex)
    pub_hex = ed_public_key(seed).hex()
    seeded_lines = [l for l in open(args.seed, "rb").read().split(b"\n") if l]
    stream = args.stream_id

    wire = Wire(args.transcript)
    try:
        # --- hello / list / heads / open -----------------------------------
        _, obj = wire.request({"op": "hello", "proto": "FLINT-SYNC/1"})
        expect_ok(wire, obj, "hello")
        if obj.get("proto") != "FLINT-SYNC/1" or obj.get("server") != "flintd":
            raise RuntimeError("hello shape: %r" % obj)
        note("hello proto=FLINT-SYNC/1 server=flintd")

        _, obj = wire.request({"op": "list"})
        expect_ok(wire, obj, "list")
        found = [s for s in obj.get("streams") or [] if s.get("id") == stream]
        if len(found) != 1 or found[0].get("name") != args.stream_name:
            raise RuntimeError("list did not return the seeded stream: %r" % obj)
        note("list streams=%d" % len(obj.get("streams") or []))

        _, obj = wire.request({"op": "open", "stream": args.stream_name})
        expect_ok(wire, obj, "open")
        head = obj.get("record", {}).get("head")
        if not head:
            raise RuntimeError("open record head is null")
        note("open name=%s head=%s" % (args.stream_name, head))

        _, obj = wire.request({"op": "heads"})
        expect_ok(wire, obj, "heads")
        if (obj.get("heads") or {}).get(stream) != head:
            raise RuntimeError("heads[%s] = %r, open head %r" %
                               (stream, (obj.get("heads") or {}).get(stream), head))
        note("heads matches open")

        # --- default page 256 + after-pagination ---------------------------
        _, page = wire.request({"op": "read", "stream": stream})
        expect_ok(wire, page, "read default")
        first = page.get("changes") or []
        if len(first) != args.page or not page.get("more"):
            raise RuntimeError("default page = %d changes more=%r" %
                               (len(first), page.get("more")))
        if first[0] != seeded_lines[0].decode("utf-8"):
            raise RuntimeError("first raw Change bytes differ from the seeded log")
        last_id = json.loads(first[-1])["id"]
        note("read default changes=%d more=true" % len(first))

        _, rest = wire.request({"op": "read", "stream": stream,
                                "after": last_id, "limit": 1024})
        expect_ok(wire, rest, "read after")
        tail = rest.get("changes") or []
        if len(tail) != len(seeded_lines) - args.page or rest.get("more"):
            raise RuntimeError("after-256 page = %d more=%r" %
                               (len(tail), rest.get("more")))
        raw_all = first + tail
        if len(raw_all) != len(seeded_lines):
            raise RuntimeError("read returned %d raw lines, seeded %d" %
                               (len(raw_all), len(seeded_lines)))
        for line in raw_all:
            if not verify_change(line, pub_hex):
                raise RuntimeError("served Change failed client signature verification")
        note("read paged raw=%d verified=%d" % (len(raw_all), len(raw_all)))
        with open(args.received, "w", encoding="utf-8") as fh:
            for line in raw_all:
                fh.write(line + "\n")

        # --- coalesced receives: two request lines in ONE write -------------
        wire.seq += 1
        req_a = json.dumps({"op": "hello", "seq": wire.seq, "proto": "FLINT-SYNC/1"},
                           separators=(",", ":")).encode()
        wire.seq += 1
        req_b = json.dumps({"op": "list", "seq": wire.seq},
                           separators=(",", ":")).encode()
        wire.transcript.write("> " + req_a.decode() + "\n> " + req_b.decode() + "\n")
        wire.send_raw(req_a + b"\n" + req_b + b"\n")
        expect_ok(wire, wire.response(wire.seq - 1), "coalesced hello")
        expect_ok(wire, wire.response(wire.seq), "coalesced list")
        note("coalesced two requests one write")

        # --- fragmented request larger than one 1460-byte segment -----------
        # 2000-byte unknown `after` value: every byte crosses in 7-byte
        # fragments, and the daemon must answer change_missing with seq echo.
        wire.seq += 1
        big_after = "0199de2d-dc00-7000-8000-" + ("%012d" % 0) + ("f" * 1950)
        req = json.dumps({"op": "read", "seq": wire.seq, "stream": stream,
                          "after": big_after}, separators=(",", ":")).encode()
        wire.transcript.write("> " + req.decode() + "\n")
        for i in range(0, len(req), 7):
            wire.send_raw(req[i:i + 7])
        wire.send_raw(b"\n")
        obj = wire.response(wire.seq)
        expect_refuse(wire, obj, "change_missing", "oversized fragmented request")
        note("fragmented request bytes=%d refused=change_missing" % len(req))

        # --- named refusals -------------------------------------------------
        wire.seq += 1
        bad_proto = json.dumps({"op": "hello", "seq": wire.seq, "proto": "FLINT-SYNC/2"},
                               separators=(",", ":")).encode()
        wire.transcript.write("> " + bad_proto.decode() + "\n")
        wire.send_raw(bad_proto + b"\n")
        obj = wire.response(wire.seq)
        expect_refuse(wire, obj, "proto_version", "proto mismatch")

        wire.seq += 1
        wire.send_raw(b'{"op":\n')
        obj = wire.response(0)
        expect_refuse(wire, obj, "usage", "malformed json")

        _, obj = wire.request({"op": "read", "stream":
                               "0199de2d-dc00-7000-8000-00000000ffff"})
        expect_refuse(wire, obj, "stream_missing", "unknown stream")

        _, obj = wire.request({"op": "read", "stream": stream,
                               "after": "0199de2d-dc00-7000-8000-00000000fffe"})
        expect_refuse(wire, obj, "change_missing", "unknown after")
        note("named refusals proto_version usage stream_missing change_missing")

        # --- read-only honesty: create/append/auth refuse, store unchanged --
        _, obj = wire.request({"op": "append", "stream": stream,
                               "expected_head": head, "changes": []})
        expect_refuse(wire, obj, "usage", "append on read-only daemon")
        if "read-only" not in (obj.get("detail") or ""):
            raise RuntimeError("append refusal is not honest: %r" % obj)
        _, obj = wire.request({"op": "auth", "nonce": "00" * 16, "hmac": "00" * 32})
        expect_refuse(wire, obj, "usage", "auth on read-only daemon")
        note("create/append/auth refuse honestly")

        # --- recovery: append a signed suffix without rewriting the record --
        parent = json.loads(raw_all[-1])["id"]
        new_id = "0199de2d-dc00-7000-8000-%012d" % (len(raw_all) + 1)
        ts = int(json.loads(raw_all[-1])["ts"]) + 1
        rec = {"id": new_id, "parent": parent, "stream": stream, "ts": ts,
               "author": pub_hex, "message": "recovered suffix", "ops": [],
               "merged": None}
        rec["sig"] = ed_sign(sig_msg(rec), seed).hex()
        raw_new = json.dumps(rec, separators=(",", ":")).encode()
        with open(os.path.join(args.store, "streams", stream + ".jsonl"), "ab") as fh:
            fh.write(raw_new + b"\n")
            fh.flush()
            os.fsync(fh.fileno())
        _, obj = wire.request({"op": "read", "stream": stream, "after": parent})
        expect_ok(wire, obj, "read recovery suffix")
        recovered = obj.get("changes") or []
        if len(recovered) != 1 or json.loads(recovered[0])["id"] != new_id:
            raise RuntimeError("recovery suffix = %r" % recovered)
        if not verify_change(recovered[0], pub_hex):
            raise RuntimeError("recovered Change failed verification")
        record = json.load(open(os.path.join(args.store, "streams", stream + ".json")))
        if record.get("head") != new_id:
            raise RuntimeError("daemon did not roll the record head forward: %r" % record)
        note("recovered contiguous suffix and record head=%s" % new_id)

        # --- 4 MiB store-file bound ----------------------------------------
        big_path = os.path.join(args.store, "streams", args.big_stream + ".jsonl")
        with open(big_path, "wb") as fh:
            fh.write(b"x" * (4 * 1024 * 1024 + 1))
        _, obj = wire.request({"op": "list"})
        expect_refuse(wire, obj, "store_too_large", "oversize store file")
        os.unlink(big_path)
        note("4 MiB store file bound refused store_too_large")

        # --- torn history refuses honestly ---------------------------------
        with open(os.path.join(args.store, "streams", args.torn_stream + ".jsonl"), "ab") as fh:
            fh.write(b'{"id":"torn')
            fh.flush()
            os.fsync(fh.fileno())
        _, obj = wire.request({"op": "open", "stream": args.torn_stream})
        expect_refuse(wire, obj, "stream_torn", "torn history")
        note("torn history refused stream_torn")

        # --- the daemon is still serving after every refusal ----------------
        _, obj = wire.request({"op": "hello", "proto": "FLINT-SYNC/1"})
        expect_ok(wire, obj, "final hello")
        note("session alive after refusals")
    finally:
        wire.close()

    sys.stderr.write("FLINTD-PROBE: PASS read/framing/recovery legs\n")
    sys.stderr.flush()
    return 0


if __name__ == "__main__":
    try:
        sys.exit(main(sys.argv[1:]))
    except Exception as exc:  # noqa: BLE001 - probe failure must name itself
        fail(str(exc))
