# go-mail.spec -- M187c #3184 + M193a #3253: GOMAIL.ELF — the smallest
# honest mail client: IMAP4rev1 LIST/SELECT/FETCH (boxes, bounded
# headers, one readable body) + SMTP submit, over tls.Dial with the
# pinned fixture chain, plaintext only behind the consent bit, named
# refusals for everything else — and now a bounded local cache under
# /host/MAIL/ (folders + envelopes + last-N bodies, LRU) that `use`
# writes and `search`/offline legs read. Host Python fixture = the
# go-irc pattern: a scripted server on 127.0.0.1 behind
# --net-tcp-respond ...:relay; no live net.
#
# Run 01 is the card leg: the GOMAIL.TXT account file resolves host/
# creds/sni, --tls dials 993, and `fetch INBOX 1 2 3` walks the whole
# journey in one session — LIST, SELECT (3 EXISTS), the bounded
# ENVELOPE list, a text/plain body read verbatim, a text/html body
# rendered stripped, and a multipart whose application/pdf attachment
# lists by name and refuses to open — while the cache write-through
# lands on the share. Run 02 submits one plaintext message over
# SMTP+TLS (AUTH PLAIN, dot-stuffed DATA). Run 03 asks for plaintext
# without consent — 'gomail: refuse plaintext' before any dial. Run 04
# is the consent answer: --plain dials 143 and fetches. Runs 05/06 are
# the folder legs: `use Sent` LISTs, SELECTs and remembers; `use Lists`
# refuses a name the real LIST never carried even though the seeded
# cache remembers it. Runs 07/08 are the search legs: seeded-cache
# header and body hits with no dial at all. Run 09 is the cache-miss
# leg: a dead transport names itself offline, paints remembered rows,
# and refuses seq 9 by name; run 10 serves a remembered body offline.
# Runs c1-c3 (M193d #3256) are the comms-chain mail leg, K=3: `send
# --to grace@example.com` — the exact argv shape the M193c contact
# face's mail-them row execs (two-token --to, no subject/body flags) —
# submits over SMTP+TLS, the fixture transcript carrying the RCPT.
#
# M196b (#3379): the credential left the file — GOMAIL.TXT carries no
# pass= and the password resolves out of the kernel secret store as the
# `mail-gate` row (slot 70, seeded app-bound to GOMAIL.ELF), so every
# dial leg prints 'gomail: secret mail-gate' and the wire's own
# LOGIN/AUTH bytes carry what only the store could supply. Runs 11-13
# are the credential legs: a plaintext pass= file row refuses by name,
# --pass refuses on argv, and an unprovisioned mail-<acct> names
# secret_not_found before any socket opens.
#
# Runs w1-w3 (M208a #3822) are the window legs, K=3: a live GOTABWM seat,
# a bare `exec GOMAIL.ELF` (the dock/launcher argv shape), and one chord
# batch walks the panes with the letter keys — INBOX's cached headers +
# body (run 01 left them), then 'f' on Sent runs the existing fetch path
# against the IMAP+TLS fixture so its envelopes land in the pane and the
# cache. Each leg clones the seeded GOMAIL-TLS.TXT over GOMAIL.TXT first
# (the shared file must stay transport-less for run 03's plaintext
# refusal); the legs are LAST so the mutation can never reach a CLI leg.
#
# Runs m1-m3 (M208b #3823) are the compose + attachment legs, K=3: the
# window's 'c' compose pane types To/Subject/Body through real chords
# and Ctrl-S submits over SMTP+TLS to the fixture (RCPT TO:<bob>), then
# `fetch Attach` saves the box's three fixture attachments under
# /host/MAIL/ and `open` routes them through APPS.TXT's opens= contract
# — invite.ics execs GOCLOCK.ELF (headless import, the merge markers),
# card.vcf execs GOFILES.ELF (the contact face), payload.bin refuses by
# type and is never executed.
#
# exec-order: assert-proven — runs end on markers only the app prints
# from fixture traffic ('gomail: saved /host/MAIL/report.pdf',
# 'gomail: sent bytes='), and the python halves read the fixture's own
# transcript of the wire bytes.
#
# HOST PREREQUISITES:
#   bash tools/go/build-gomail.sh --gate -> .build/go/GOMAIL-GATE.ELF
# GOMAIL-GATE is the virelai_gate_roots build (the GOIRC-GATE pattern):
# the TLS legs anchor on the pinned leaf.example.com chain, so the gate
# binary is what every run execs under the GOMAIL.ELF name. Fixture
# transcripts land in $RUN_DIR/maild-<port>.log for the python asserts.

vgate_name go-mail "M187c #3184 + M193a #3253 + M193d #3256 + M196b #3379 + M208a #3822 + M208b #3823: GOMAIL.ELF — IMAP4 fetch/list/read + SMTP submit over tls.Dial (pinned fixture chain), plaintext consent refusal + grant, attachment/html truth-in-bounds, + use-able folders, bounded /host/MAIL cache, offline search + cache-miss legs, + the chain's send --to leg in the contact face's own argv shape, K=3, + the keychain credential — mail-<user> out of the slot-70 store, pass= and --pass refused by name, secret_not_found named, + the no-arg window: boxes/headers/message panes over the cache with an explicit Fetch, K=3 under a live seat, + compose pane send + attachment save/open: .ics -> GOCLOCK merge, .vcf -> GOFILES contact face, .bin saved not run, K=3"
vgate_share seed
vgate_runner_flags -Xswiftc -DSPIKE

# The monitor's exec verb packs at most 8 argv slots and the program
# name takes one (max_exec_args); every leg stays inside the bound
# because the account file carries host/user/from/sni and the password
# arrives through the store — never argv.
vgate_file script-01.txt <<'EOF'
net ip 10.0.0.1
net arp 10.0.0.2
exec GOMAIL.ELF --tls fetch INBOX 1 2 3
EOF

vgate_file script-02.txt <<'EOF'
net ip 10.0.0.1
net arp 10.0.0.2
exec GOMAIL.ELF --tls send --to=alice@fixture.local --subject=gate-one --body=see-the-report
EOF

vgate_file script-03.txt <<'EOF'
exec GOMAIL.ELF fetch INBOX
EOF

vgate_file script-04.txt <<'EOF'
net ip 10.0.0.1
net arp 10.0.0.2
exec GOMAIL.ELF --plain fetch INBOX 1
EOF

vgate_file script-05.txt <<'EOF'
net ip 10.0.0.1
net arp 10.0.0.2
exec GOMAIL.ELF --tls use Sent
EOF

vgate_file script-06.txt <<'EOF'
net ip 10.0.0.1
net arp 10.0.0.2
exec GOMAIL.ELF --tls use Lists
EOF

# Runs 07/08 carry no net lines at all: search is a cache read and must
# never dial.
vgate_file script-07.txt <<'EOF'
exec GOMAIL.ELF search weekly
EOF

vgate_file script-08.txt <<'EOF'
exec GOMAIL.ELF search orchid
EOF

# Runs 09/10 ask for TLS with no net configured: the dial fails and the
# verb answers from the cache — hit and miss both named.
vgate_file script-09.txt <<'EOF'
exec GOMAIL.ELF --tls fetch INBOX 9
EOF

vgate_file script-10.txt <<'EOF'
exec GOMAIL.ELF --tls read INBOX 1
EOF

vgate_setup_python <<'PY'
import os, shutil, subprocess, sys, time

run = os.environ["RUN_DIR"]
share = os.environ.get("VG_SHARE") or os.path.join(run, "share")
os.makedirs(share, exist_ok=True)

gomail = os.path.join(".build", "go", "GOMAIL-GATE.ELF")
if not os.path.exists(gomail):
    sys.exit("GOMAIL-GATE.ELF missing (expected " + gomail + ") - build it "
             "first: bash tools/go/build-web.sh gomail GOMAIL --gate")
# The gate-roots build rides under the shipping name: the TLS legs
# validate the pinned leaf.example.com chain.
shutil.copy(gomail, os.path.join(share, "GOMAIL.ELF"))
print("go-mail: staged GOMAIL-GATE.ELF as GOMAIL.ELF (%d)"
      % os.path.getsize(os.path.join(share, "GOMAIL.ELF")))

# M208a runs w1-w3 drive the window under a live seat: stage GOTABWM and
# keep the boot seatless (wm=none — the go-irc/go-vault idiom) so each
# window leg execs the seat itself; the CLI legs never needed one.
gotabwm = os.path.join(".build", "go", "GOTABWM.ELF")
if not os.path.exists(gotabwm):
    sys.exit("GOTABWM.ELF missing (expected " + gotabwm + ") - build it "
             "first: bash tools/go/build-gotabwm.sh")
shutil.copy(gotabwm, os.path.join(share, "GOTABWM.ELF"))
print("go-mail: staged GOTABWM.ELF (%d)" % os.path.getsize(gotabwm))
with open(os.path.join(share, "SETTINGS.TXT"), "w") as f:
    f.write("#v2\nwm=none\n")

# M208b: the attachment hand-off targets — GOCLOCK's headless .ics
# merge and GOFILES' .vcf contact face — plus the real manifest whose
# opens= rows route them (gomail's `open` reads APPS.TXT, never a
# hardcoded table).
for elf, build in (("GOFILES.ELF", "build-files.sh"),
                   ("GOCLOCK.ELF", "build-goclock.sh")):
    src = os.path.join(".build", "go", elf)
    if not os.path.exists(src):
        sys.exit(elf + " missing (expected " + src + ") - build it "
                 "first: bash tools/go/" + build)
    shutil.copy(src, os.path.join(share, elf))
    print("go-mail: staged %s (%d)" % (elf, os.path.getsize(src)))
shutil.copy(os.path.join("image", "apps.txt"),
            os.path.join(share, "APPS.TXT"))
print("go-mail: staged APPS.TXT manifest for the opens= route")

# The account file the app resolves at boot — deliberately WITHOUT a
# tls/plain row: run 03's bare fetch must hit the plaintext refusal, so
# the file never carries a transport answer and each leg's flag decides.
# And deliberately WITHOUT a pass= row: since M196b the credential is
# the store's, not the file's.
with open(os.path.join(share, "GOMAIL.TXT"), "w") as f:
    f.write("# go-mail gate fixture account\n"
            "host=10.0.0.2\n"
            "user=gate\n"
            "from=gate@fixture.local\n"
            "sni=leaf.example.com\n")
print("go-mail: wrote GOMAIL.TXT account file (no pass= — store holds it)")

# M208a: the same account with its transport answer filled — the window
# legs clone it over GOMAIL.TXT so their bare `exec GOMAIL.ELF` (no argv
# can carry --tls) dials TLS; run 03's refusal keeps the plain file.
with open(os.path.join(share, "GOMAIL-TLS.TXT"), "w") as f:
    f.write("# go-mail gate fixture account — tls=on for the window legs\n"
            "host=10.0.0.2\n"
            "user=gate\n"
            "from=gate@fixture.local\n"
            "sni=leaf.example.com\n"
            "tls=on\n")
print("go-mail: wrote GOMAIL-TLS.TXT (the window legs' account)")

# M196b: the credential row, app-bound to GOMAIL.ELF. An apps-allowlist
# entry is visible only when the named image is operator-owned on /host
# (kernel secret.zig visible() -> trust.system_owned_key), so OWNERS.TXT
# pins GOMAIL.ELF uid_system 0644 — the same pair go-flint seeds for
# FLINT.ELF's agent-sign row.
with open(os.path.join(share, "SECRETS.TXT"), "w") as f:
    f.write("#v1\nmail-gate\t1000\tgate-pass\tGOMAIL.ELF\n")
with open(os.path.join(share, "OWNERS.TXT"), "w") as f:
    f.write("#v1\nGOMAIL.ELF\t644\t0\t-\n")
print("go-mail: seeded SECRETS.TXT mail-gate (app-bound) + OWNERS.TXT")

# The refused fixture: an account file still carrying a plaintext pass=
# row — run 11's named-refusal leg.
with open(os.path.join(share, "GOMAIL-PLAIN.TXT"), "w") as f:
    f.write("# legacy plaintext account — refused by name\n"
            "host=10.0.0.2\n"
            "user=gate\n"
            "pass=gate-pass\n"
            "from=gate@fixture.local\n"
            "sni=leaf.example.com\n")
print("go-mail: wrote GOMAIL-PLAIN.TXT (the pass= refusal fixture)")

# M193a seed: remembered mail under /host/MAIL/ — the cache files the
# app's own codec reads. 'Lists' exists ONLY here: the wire LIST never
# carries it, so run 06's `use Lists` proves `use` trusts the real LIST
# over the cache, while runs 07/08 prove search reads the seeded corpus
# and run 09 proves a miss names itself instead of fabricating.
maildir = os.path.join(share, "MAIL")
os.makedirs(maildir, exist_ok=True)
with open(os.path.join(maildir, "BOXES"), "w") as f:
    f.write("INBOX\nSent\nLists\n")
with open(os.path.join(maildir, "HDRS"), "w") as f:
    f.write("Lists\t7\tdana@fixture.local\tweekly digest\t"
            "Wed, 21 Oct 2026 11:00:00 +0000\n")
seed_body = b"the orchid thread grows\r\nsecond seeded line"
# M208a: a body matching the seeded envelope too (Lists/7) — the window
# legs' message pane renders it; 'weekly' still hits once (the envelope
# dedupes), 'orchid' is untouched.
seed_body7 = b"weekly digest body"
with open(os.path.join(maildir, "BODIES"), "wb") as f:
    f.write(b"@Lists\t9\t1\ttext/plain\t" + str(len(seed_body)).encode()
            + b"\n" + seed_body + b"\n")
    f.write(b"@Lists\t7\t1\ttext/plain\t" + str(len(seed_body7)).encode()
            + b"\n" + seed_body7 + b"\n")
print("go-mail: seeded /host/MAIL cache (BOXES, Lists hdr 7, bodies 9+7)")

fixture = os.path.join(run, "maild.py")
with open(fixture, "w") as f:
    f.write('''
import socket, sys, time

mode, port, logpath = sys.argv[1], int(sys.argv[2]), sys.argv[3]
logf = open(logpath, "w", buffering=1)

def log(s):
    logf.write(s + "\\n")

# mode: "imap-tls" | "imap-plain" | "smtp-tls" | "smtp-plain" — one VM
# arms ONE --net-tcp-respond, so each run gets its own fixture leg on
# its own port. TLS wraps with the pinned leaf.example.com chain (the
# same material go-irc's journey leg serves).
proto, _, transport = mode.partition("-")
ctx = None
if transport == "tls":
    import ssl
    ctx = ssl.SSLContext(ssl.PROTOCOL_TLS_SERVER)
    ctx.minimum_version = ssl.TLSVersion.TLSv1_3
    ctx.maximum_version = ssl.TLSVersion.TLSv1_3
    ctx.load_cert_chain(sys.argv[4], sys.argv[5])

srv = socket.socket(socket.AF_INET, socket.SOCK_STREAM)
srv.setsockopt(socket.SOL_SOCKET, socket.SO_REUSEADDR, 1)
srv.bind(("127.0.0.1", port))
srv.listen(4)
srv.settimeout(1800)
log("listening mode=" + mode)

def send(conn, line):
    conn.sendall(line.encode() + b"\\r\\n")
    log("tx " + line)

def sendlit(conn, seq, key, payload):
    # '* n FETCH (BODY[s] {n}CRLF<payload>)' — the literal is inline in
    # the record; the closing paren rides the post-literal line.
    head = "* %d FETCH (%s {%d}" % (seq, key, len(payload))
    conn.sendall(head.encode() + b"\\r\\n" + payload + b")\\r\\n")
    log("tx " + head + " <lit %d> )" % len(payload))

def recvline(conn, buf):
    while b"\\n" not in buf:
        chunk = conn.recv(4096)
        if not chunk:
            return None, buf
        buf += chunk
    line, buf = buf.split(b"\\n", 1)
    return line.decode("utf-8", "replace").rstrip("\\r"), buf

# The three fixture messages: a text/plain, a text/html (the client
# renders it stripped), and a multipart/mixed whose application/pdf
# attachment saves under /host/MAIL/ (M208b — fetched parts land on the
# share base64-decoded instead of an unconditional refuse).
import base64
B1 = b"line one of the gate body\\r\\nsecond line\\r\\nthird line"
B2 = b"<html><body><p>html <b>hello</b> fixture</p></body></html>"
B3 = b"see the attached report"
PDF_B64 = base64.b64encode(b"%PDF-1.4 gate report bytes")
# M208b: the Attach box's three messages each carry one hand-off-class
# attachment — text/calendar -> GOCLOCK's merge, text/vcard -> GOFILES'
# contact face, application/octet-stream -> saved, never routed. The
# payloads ride the wire base64 like a real MIME part.
ICS = (b"BEGIN:VCALENDAR\\r\\nVERSION:2.0\\r\\nPRODID:-//VirelaiOS//M208b//EN\\r\\n"
       b"BEGIN:VEVENT\\r\\nUID:mail-attach-1@virelai\\r\\nDTSTAMP:20261028T120000Z\\r\\n"
       b"DTSTART:20270620T150000Z\\r\\nSUMMARY:Mail sync\\r\\nEND:VEVENT\\r\\nEND:VCALENDAR\\r\\n")
VCF = (b"BEGIN:VCARD\\r\\nVERSION:4.0\\r\\nFN:Grace Hopper\\r\\nORG:Cognition\\r\\n"
       b"TEL:+1-555-0100\\r\\nEMAIL:grace@example.com\\r\\nEND:VCARD\\r\\n")
BIN = b"\\x00\\x01\\x02payload-not-a-program"
ICS_B64, VCF_B64, BIN_B64 = (base64.b64encode(ICS), base64.b64encode(VCF),
                           base64.b64encode(BIN))
def att_bs(media, sub, fn, payload):
    return ("((\\"TEXT\\" \\"PLAIN\\" (\\"CHARSET\\" \\"US-ASCII\\") NIL NIL \\"7BIT\\" %d 1) "
            "(\\"%s\\" \\"%s\\" NIL NIL NIL \\"BASE64\\" %d NIL "
            "(\\"attachment\\" (\\"filename\\" \\"%s\\")) NIL NIL) \\"MIXED\\")"
            % (len(B3), media, sub, len(payload), fn))
ATTACH_MSGS = {
    1: dict(
        env="\\"Wed, 21 Oct 2026 11:00:00 +0000\\" \\"calendar invite\\" "
            "((\\"Dana\\" NIL \\"dana\\" \\"fixture.local\\")) NIL NIL NIL NIL NIL NIL "
            "\\"<a1@fixture.local>\\"",
        bs=att_bs("TEXT", "CALENDAR", "invite.ics", ICS_B64),
        parts={"1": B3, "2": ICS_B64}),
    2: dict(
        env="\\"Wed, 21 Oct 2026 11:05:00 +0000\\" \\"contact card\\" "
            "((\\"Erin\\" NIL \\"erin\\" \\"fixture.local\\")) NIL NIL NIL NIL NIL NIL "
            "\\"<a2@fixture.local>\\"",
        bs=att_bs("TEXT", "VCARD", "card.vcf", VCF_B64),
        parts={"1": B3, "2": VCF_B64}),
    3: dict(
        env="\\"Wed, 21 Oct 2026 11:10:00 +0000\\" \\"binary blob\\" "
            "((\\"Fran\\" NIL \\"fran\\" \\"fixture.local\\")) NIL NIL NIL NIL NIL NIL "
            "\\"<a3@fixture.local>\\"",
        bs=att_bs("APPLICATION", "OCTET-STREAM", "payload.bin", BIN_B64),
        parts={"1": B3, "2": BIN_B64}),
}
MSGS = {
    1: dict(
        env="\\"Wed, 21 Oct 2026 10:00:00 +0000\\" \\"gate hello\\" "
            "((\\"Alice\\" NIL \\"alice\\" \\"fixture.local\\")) NIL NIL NIL NIL NIL NIL "
            "\\"<m1@fixture.local>\\"",
        bs="(\\"TEXT\\" \\"PLAIN\\" (\\"CHARSET\\" \\"US-ASCII\\") NIL NIL \\"7BIT\\" %d 3)" % len(B1),
        parts={"TEXT": B1, "1": B1}),
    2: dict(
        env="\\"Wed, 21 Oct 2026 10:05:00 +0000\\" \\"html issue\\" "
            "((NIL NIL \\"bob\\" \\"fixture.local\\")) NIL NIL NIL NIL NIL NIL "
            "\\"<m2@fixture.local>\\"",
        bs="(\\"TEXT\\" \\"HTML\\" (\\"CHARSET\\" \\"UTF-8\\") NIL NIL \\"7BIT\\" %d 1)" % len(B2),
        parts={"TEXT": B2, "1": B2}),
    3: dict(
        env="\\"Wed, 21 Oct 2026 10:10:00 +0000\\" \\"report attached\\" "
            "((\\"Carol\\" NIL \\"carol\\" \\"fixture.local\\")) NIL NIL NIL NIL NIL NIL "
            "\\"<m3@fixture.local>\\"",
        bs="((\\"TEXT\\" \\"PLAIN\\" (\\"CHARSET\\" \\"US-ASCII\\") NIL NIL \\"7BIT\\" %d 1) "
           "(\\"APPLICATION\\" \\"PDF\\" NIL NIL NIL \\"BASE64\\" %d NIL "
           "(\\"attachment\\" (\\"filename\\" \\"report.pdf\\")) NIL NIL) \\"MIXED\\")"
           % (len(B3), len(PDF_B64)),
        parts={"1": B3, "2": PDF_B64, "TEXT": B3}),
}

def imap_serve(conn, idx):
    send(conn, "* OK fixture IMAP4rev1 ready")
    buf = b""
    selbox = "INBOX"  # SELECT retargets the message table (M208b: Attach)
    deadline = time.time() + 120
    while time.time() < deadline:
        conn.settimeout(max(1.0, deadline - time.time()))
        try:
            line, buf = recvline(conn, buf)
        except socket.timeout:
            break
        if line is None:
            break
        log("c%d rx %s" % (idx, line))
        parts = line.split(" ", 2)
        if len(parts) < 2 or not parts[0].startswith("a"):
            continue
        tag, verb = parts[0], parts[1].upper()
        rest = parts[2] if len(parts) > 2 else ""
        msgs = ATTACH_MSGS if selbox == "Attach" else MSGS
        if verb == "LOGIN":
            send(conn, tag + " OK Login completed")
        elif verb == "LIST":
            send(conn, "* LIST (\\\\HasNoChildren) \\"/\\" \\"INBOX\\"")
            send(conn, "* LIST (\\\\HasNoChildren) \\"/\\" \\"Sent\\"")
            send(conn, "* LIST (\\\\HasNoChildren) \\"/\\" \\"Attach\\"")
            send(conn, tag + " OK List completed")
        elif verb == "SELECT":
            selbox = rest.strip('"')
            send(conn, "* 3 EXISTS")
            send(conn, "* 1 RECENT")
            send(conn, "* FLAGS (\\\\Seen \\\\Answered \\\\Draft)")
            send(conn, "* OK [UIDVALIDITY 7] UIDs valid")
            send(conn, tag + " OK [READ-WRITE] Select completed")
        elif verb == "FETCH":
            # rest: "<set> (<items>)" — set is one seq or a:b.
            spec, _, items = rest.partition(" ")
            items = items.strip()
            seqs = []
            if ":" in spec:
                a, _, b = spec.partition(":")
                seqs = range(int(a), int(b) + 1)
            else:
                seqs = [int(spec)]
            for seq in seqs:
                m = msgs.get(seq)
                if m is None:
                    continue
                if "ENVELOPE" in items:
                    send(conn, "* %d FETCH (ENVELOPE (%s))" % (seq, m["env"]))
                elif "BODYSTRUCTURE" in items:
                    send(conn, "* %d FETCH (BODYSTRUCTURE %s)" % (seq, m["bs"]))
                elif "BODY[" in items:
                    section = items.split("BODY[", 1)[1].split("]", 1)[0]
                    payload = m["parts"].get(section, b"")
                    sendlit(conn, seq, items.strip("()"), payload)
            send(conn, tag + " OK Fetch completed")
        elif verb == "LOGOUT":
            send(conn, "* BYE fixture logging out")
            send(conn, tag + " OK Logout completed")
            return

def smtp_serve(conn, idx):
    send(conn, "220 fixture ESMTP ready")
    buf = b""
    data = False
    deadline = time.time() + 120
    while time.time() < deadline:
        conn.settimeout(max(1.0, deadline - time.time()))
        try:
            line, buf = recvline(conn, buf)
        except socket.timeout:
            break
        if line is None:
            break
        log("c%d rx %s" % (idx, line))
        if data:
            if line == ".":
                data = False
                send(conn, "250 2.0.0 queued as m001")
            continue
        verb = line.split(" ", 1)[0].upper()
        if verb == "EHLO" or verb == "HELO":
            send(conn, "250-fixture greets virelai")
            send(conn, "250 AUTH PLAIN")
        elif verb == "AUTH":
            send(conn, "235 2.7.0 authenticated")
        elif verb == "MAIL":
            send(conn, "250 2.1.0 sender ok")
        elif verb == "RCPT":
            send(conn, "250 2.1.5 rcpt ok")
        elif verb == "DATA":
            send(conn, "354 end with <CR><LF>.<CR><LF>")
            data = True
        elif verb == "QUIT":
            send(conn, "221 2.0.0 bye")
            return
        else:
            send(conn, "502 5.5.2 verb not implemented")

conns = 12  # c0 fetch, c1-c2 use, c3-c8 windows, c9-c11 M208b attach (imap-tls)
for idx in range(conns):
    try:
        conn, _ = srv.accept()
    except socket.timeout:
        log("accept timeout at conn %d" % idx)
        break
    conn.settimeout(60)
    if ctx is not None:
        try:
            conn = ctx.wrap_socket(conn, server_side=True)
            log("c%d tls ok" % idx)
        except Exception as e:
            log("c%d tls %s" % (idx, e))
            try:
                conn.close()
            except Exception:
                pass
            continue
    log("c%d open" % idx)
    try:
        if proto == "imap":
            imap_serve(conn, idx)
        else:
            smtp_serve(conn, idx)
    except Exception as e:
        log("c%d serve %s" % (idx, e))
    try:
        conn.close()
    except Exception:
        pass
    log("c%d close" % idx)

srv.close()
log("fixture done")
''')

# The TLS legs serve the pinned leaf.example.com chain — identical to
# go-irc's journey material; nothing regenerates.
cfx = os.path.join("user", "src", "lib", "tls", "vectors", "fx")
chain = os.path.join(cfx, "chain-ec.pem")
mailkey = os.path.join(cfx, "leaf-ec.key")
if not os.path.exists(chain) or not os.path.exists(mailkey):
    sys.exit("pinned fixture chain missing under %s" % cfx)

procs = []
for mode, port in (("imap-tls", 26993), ("smtp-tls", 26465),
                   ("imap-plain", 26143)):
    subprocess.run(["sh", "-c", "lsof -ti tcp:%d | xargs kill -9" % port],
                   stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
    logpath = os.path.join(run, "maild-%d.log" % port)
    log = open(logpath, "w")
    log.close()
    argv = [sys.executable, fixture, mode, str(port), logpath]
    if mode.endswith("-tls"):
        argv += [chain, mailkey]
    proc = subprocess.Popen(argv,
                            stdout=subprocess.DEVNULL, stderr=subprocess.STDOUT,
                            start_new_session=True)
    procs.append(proc)
    print("go-mail: fixture %s pid=%d on 127.0.0.1:%d" % (mode, proc.pid, port))

time.sleep(0.4)
for proc in procs:
    if proc.poll() is not None:
        sys.exit("go-mail: fixture exited early rc=%s" % proc.returncode)
PY

# --- Run 01: the card leg — IMAP4+TLS fetch/list/read journey ---------
#
# 'fetch INBOX 1 2 3' over the gate-roots TLS dial: account file ->
# LIST two boxes -> SELECT 3 EXISTS -> the bounded ENVELOPE list ->
# seq 1 text/plain verbatim, seq 2 text/html rendered stripped, seq 3
# multipart whose report.pdf attachment lists by name and refuses.
vgate_run 01 -- \
    --screen '$RUN_DIR/screen' \
    --via-virtio \
    --net '$RUN_DIR/cap01.bin' --net-arp-respond 10.0.0.2 \
    --net-tcp-respond 10.0.0.2:993:relay --net-tcp-respond-relay 127.0.0.1:26993 \
    --script '$RUN_DIR/script-01.txt' --script-expect 'gomail: closed' --timeout 240

vgate_assert 01 serial-contains 'gomail: account /host/GOMAIL.TXT'
vgate_assert 01 serial-contains 'gomail: secret mail-gate'
vgate_assert 01 serial-contains 'gomail: dial 10.0.0.2 993 tls'
vgate_assert 01 serial-contains 'gomail: imap ready'
vgate_assert 01 serial-contains 'gomail: login gate'
vgate_assert 01 serial-contains 'gomail: box INBOX'
vgate_assert 01 serial-contains 'gomail: box Sent'
vgate_assert 01 serial-contains 'gomail: select INBOX exists=3'
vgate_assert 01 serial-contains 'gomail: hdr 1 Alice: gate hello'
vgate_assert 01 serial-contains 'gomail: hdr 2 bob@fixture.local: html issue'
vgate_assert 01 serial-contains 'gomail: hdr 3 Carol: report attached'
vgate_assert 01 serial-contains 'gomail: body 1 text/plain'
vgate_assert 01 serial-contains 'gomail: text line one of the gate body'
vgate_assert 01 serial-contains 'gomail: html-stripped 2'
vgate_assert 01 serial-contains 'gomail: text html hello fixture'
vgate_assert 01 serial-contains 'gomail: body 3 text/plain'
vgate_assert 01 serial-contains 'gomail: attach 3 report.pdf application/pdf'
vgate_assert 01 serial-contains 'gomail: saved /host/MAIL/report.pdf'
vgate_assert 01 serial-contains 'gomail: closed'
vgate_assert 01 serial-contains 'gomail: cache /host/MAIL/BOXES'
vgate_assert 01 serial-contains 'gomail: cache /host/MAIL/HDRS'
vgate_assert 01 serial-contains 'gomail: cache /host/MAIL/BODIES'
vgate_assert 01 serial-absent 'gomail: error'
vgate_assert 01 serial-absent 'gomail: refuse plaintext'
vgate_assert 01 serial-absent 'gomail: cache-fail'
# The credential itself never echoes — not from the file (there is no
# pass=), not from the store read, not from LOGIN.
vgate_assert 01 serial-absent 'gate-pass'
vgate_assert 01 serial-absent '[EXC] parking:'
# The cache really landed: the share holds the folders, the envelopes,
# and the bodies the wire just proved.
vgate_assert 01 share-contains MAIL/BOXES 'INBOX'
vgate_assert 01 share-contains MAIL/HDRS 'gate hello'
vgate_assert 01 share-contains MAIL/BODIES 'line one of the gate body'
# M208b: the attachment landed base64-decoded with an index row.
vgate_assert 01 share-contains MAIL/report.pdf '%PDF-1.4 gate report bytes'
vgate_assert 01 share-contains MAIL/ATTACH 'report.pdf'
# The wire half: the fixture transcript carries the exact tagged
# commands — a program that never ran cannot produce them.
vgate_assert 01 python <<'PY'
import os, sys
log = open(os.path.join(os.environ["RUN_DIR"], "maild-26993.log")).read()
need = ["c0 tls ok", "c0 rx a1 LOGIN", "c0 rx a2 LIST", "c0 rx a3 SELECT",
        "c0 rx a4 FETCH 1:3 (ENVELOPE)",
        "c0 rx a5 FETCH 1 (BODYSTRUCTURE)", "c0 rx a6 FETCH 1 (BODY[TEXT])",
        "FETCH 2 (BODYSTRUCTURE)", "FETCH 2 (BODY[TEXT])",
        "FETCH 3 (BODYSTRUCTURE)", "FETCH 3 (BODY[1])", "FETCH 3 (BODY[2])",
        "rx a12 LOGOUT"]
missing = [n for n in need if n not in log]
if missing:
    sys.exit("imap fixture transcript missing: %r\n%s" % (missing, log))
PY

# --- Run 02: SMTP+TLS submit -------------------------------------------
#
# One plaintext message: greeting -> EHLO -> AUTH PLAIN -> MAIL/RCPT ->
# dot-stuffed DATA -> 250 queued -> QUIT.
vgate_run 02 -- \
    --screen '$RUN_DIR/screen-02' \
    --via-virtio \
    --net '$RUN_DIR/cap02.bin' --net-arp-respond 10.0.0.2 \
    --net-tcp-respond 10.0.0.2:465:relay --net-tcp-respond-relay 127.0.0.1:26465 \
    --script '$RUN_DIR/script-02.txt' --script-expect 'gomail: sent bytes=' --timeout 240

vgate_assert 02 serial-contains 'gomail: account /host/GOMAIL.TXT'
vgate_assert 02 serial-contains 'gomail: secret mail-gate'
vgate_assert 02 serial-contains 'gomail: dial 10.0.0.2 465 tls'
vgate_assert 02 serial-contains 'gomail: smtp ready'
vgate_assert 02 serial-contains 'gomail: mail gate@fixture.local'
vgate_assert 02 serial-contains 'gomail: rcpt alice@fixture.local'
vgate_assert 02 serial-contains 'gomail: sent bytes='
vgate_assert 02 serial-contains 'gomail: closed'
vgate_assert 02 serial-absent 'gomail: error'
vgate_assert 02 serial-absent 'gate-pass'
vgate_assert 02 serial-absent '[EXC] parking:'
vgate_assert 02 python <<'PY'
import os, sys
log = open(os.path.join(os.environ["RUN_DIR"], "maild-26465.log")).read()
need = ["c0 tls ok", "c0 rx EHLO virelai",
        "c0 rx AUTH PLAIN AGdhdGUAZ2F0ZS1wYXNz",
        "c0 rx MAIL FROM:<gate@fixture.local>",
        "c0 rx RCPT TO:<alice@fixture.local>",
        "c0 rx DATA", "c0 rx Subject: gate-one",
        "c0 rx see-the-report", "c0 rx .", "c0 rx QUIT",
        "tx 250 2.0.0 queued as m001"]
missing = [n for n in need if n not in log]
if missing:
    sys.exit("smtp fixture transcript missing: %r\n%s" % (missing, log))
PY

# --- Run 03: plaintext without consent refuses by name -----------------
#
# GOMAIL.TXT carries no transport answer and the exec has no flag, so
# the request is plaintext and unconsented: the named refusal prints
# BEFORE any dial — no socket, no fixture traffic, no net config. The
# store read precedes the dial boundary, so 'gomail: secret mail-gate'
# lands first — the credential resolved even though the wire refused.
vgate_run 03 -- \
    --screen '$RUN_DIR/screen-03' \
    --via-virtio \
    --script '$RUN_DIR/script-03.txt' --script-expect 'gomail: refuse plaintext' --timeout 180

vgate_assert 03 serial-contains 'gomail: secret mail-gate'
vgate_assert 03 serial-contains 'gomail: refuse plaintext'
vgate_assert 03 serial-contains 'gomail: closed'
vgate_assert 03 serial-absent 'gomail: dial'
vgate_assert 03 serial-absent 'gomail: imap ready'
vgate_assert 03 serial-absent '[EXC] parking:'

# --- Run 04: the consent answer — --plain dials and fetches -------------
vgate_run 04 -- \
    --screen '$RUN_DIR/screen-04' \
    --via-virtio \
    --net '$RUN_DIR/cap04.bin' --net-arp-respond 10.0.0.2 \
    --net-tcp-respond 10.0.0.2:143:relay --net-tcp-respond-relay 127.0.0.1:26143 \
    --script '$RUN_DIR/script-04.txt' --script-expect 'gomail: closed' --timeout 240

vgate_assert 04 serial-contains 'gomail: secret mail-gate'
vgate_assert 04 serial-contains 'gomail: dial 10.0.0.2 143 plain'
vgate_assert 04 serial-contains 'gomail: imap ready'
vgate_assert 04 serial-contains 'gomail: select INBOX exists=3'
vgate_assert 04 serial-contains 'gomail: text line one of the gate body'
vgate_assert 04 serial-contains 'gomail: closed'
vgate_assert 04 serial-absent 'gomail: refuse plaintext'
vgate_assert 04 serial-absent 'gomail: error'
vgate_assert 04 serial-absent '[EXC] parking:'
vgate_assert 04 python <<'PY'
import os, sys
log = open(os.path.join(os.environ["RUN_DIR"], "maild-26143.log")).read()
need = ["c0 open", "c0 rx a1 LOGIN", "c0 rx a2 LIST", "c0 rx a3 SELECT",
        "c0 rx a4 FETCH 1:3 (ENVELOPE)", "c0 rx a5 FETCH 1 (BODYSTRUCTURE)",
        "LOGOUT"]
missing = [n for n in need if n not in log]
if missing:
    sys.exit("imap-plain fixture transcript missing: %r\n%s" % (missing, log))
PY

# --- Run 05: 'use Sent' — folder browsing over the real LIST ------------
#
# M193a: LIST paints the folders, `use` SELECTs the named one and writes
# the remembered selection + folder list into /host/MAIL/.
vgate_run 05 -- \
    --screen '$RUN_DIR/screen-05' \
    --via-virtio \
    --net '$RUN_DIR/cap05.bin' --net-arp-respond 10.0.0.2 \
    --net-tcp-respond 10.0.0.2:993:relay --net-tcp-respond-relay 127.0.0.1:26993 \
    --script '$RUN_DIR/script-05.txt' --script-expect 'gomail: use Sent exists=3' --timeout 240

vgate_assert 05 serial-contains 'gomail: secret mail-gate'
vgate_assert 05 serial-contains 'gomail: dial 10.0.0.2 993 tls'
vgate_assert 05 serial-contains 'gomail: imap ready'
vgate_assert 05 serial-contains 'gomail: box INBOX'
vgate_assert 05 serial-contains 'gomail: box Sent'
vgate_assert 05 serial-contains 'gomail: use Sent exists=3'
vgate_assert 05 serial-contains 'gomail: cache /host/MAIL/BOXES'
vgate_assert 05 serial-contains 'gomail: cache /host/MAIL/USE'
vgate_assert 05 serial-contains 'gomail: closed'
vgate_assert 05 serial-absent 'gomail: error'
vgate_assert 05 serial-absent 'gomail: refuse'
vgate_assert 05 serial-absent '[EXC] parking:'
vgate_assert 05 share-contains MAIL/USE 'Sent'
vgate_assert 05 share-contains MAIL/BOXES 'Sent'
vgate_assert 05 python <<'PY'
import os, sys
log = open(os.path.join(os.environ["RUN_DIR"], "maild-26993.log")).read()
need = ["LIST", "SELECT \"Sent\""]
missing = [n for n in need if n not in log]
if missing:
    sys.exit("use fixture transcript missing: %r\n%s" % (missing, log))
PY

# --- Run 06: 'use Lists' — no fake folders ------------------------------
#
# The seeded BOXES remembers 'Lists' but the wire LIST does not: `use`
# validates over the REAL LIST, so the name refuses by name and no
# SELECT is ever sent.
vgate_run 06 -- \
    --screen '$RUN_DIR/screen-06' \
    --via-virtio \
    --net '$RUN_DIR/cap06.bin' --net-arp-respond 10.0.0.2 \
    --net-tcp-respond 10.0.0.2:993:relay --net-tcp-respond-relay 127.0.0.1:26993 \
    --script '$RUN_DIR/script-06.txt' --script-expect 'gomail: refuse use Lists' --timeout 240

vgate_assert 06 serial-contains 'gomail: secret mail-gate'
vgate_assert 06 serial-contains 'gomail: box INBOX'
vgate_assert 06 serial-contains 'gomail: box Sent'
vgate_assert 06 serial-contains 'gomail: refuse use Lists'
vgate_assert 06 serial-contains 'gomail: closed'
vgate_assert 06 serial-absent 'gomail: use Lists'
vgate_assert 06 serial-absent 'gomail: error'
vgate_assert 06 serial-absent '[EXC] parking:'
vgate_assert 06 python <<'PY'
import os, sys
log = open(os.path.join(os.environ["RUN_DIR"], "maild-26993.log")).read()
if 'SELECT "Lists"' in log:
    sys.exit("use SELECTed a folder the LIST never carried\n%s" % log)
PY

# --- Run 07: 'search weekly' — seeded-cache header hit, no dial ----------
vgate_run 07 -- \
    --screen '$RUN_DIR/screen-07' \
    --via-virtio \
    --script '$RUN_DIR/script-07.txt' --script-expect 'gomail: search hits=1' --timeout 180

vgate_assert 07 serial-contains 'gomail: search weekly'
vgate_assert 07 serial-contains 'gomail: hit Lists 7 dana@fixture.local: weekly digest'
vgate_assert 07 serial-contains 'gomail: search hits=1'
vgate_assert 07 serial-contains 'gomail: closed'
vgate_assert 07 serial-absent 'gomail: dial'
vgate_assert 07 serial-absent 'gomail: error'
vgate_assert 07 serial-absent '[EXC] parking:'

# --- Run 08: 'search orchid' — seeded-cache body hit, no dial ------------
vgate_run 08 -- \
    --screen '$RUN_DIR/screen-08' \
    --via-virtio \
    --script '$RUN_DIR/script-08.txt' --script-expect 'gomail: search hits=1' --timeout 180

vgate_assert 08 serial-contains 'gomail: hit Lists 9 (body)'
vgate_assert 08 serial-contains 'gomail: search hits=1'
vgate_assert 08 serial-contains 'gomail: closed'
vgate_assert 08 serial-absent 'gomail: dial'
vgate_assert 08 serial-absent 'gomail: error'
vgate_assert 08 serial-absent '[EXC] parking:'

# --- Run 09: cache-miss fetch — offline names itself, never fabricates ---
#
# --tls asks for the wire with no net configured: the dial fails, the
# offline leg paints the remembered folders + INBOX envelopes, then
# 'gomail: cache-miss INBOX 9' names the miss — no 'gomail: body 9' is
# ever invented.
vgate_run 09 -- \
    --screen '$RUN_DIR/screen-09' \
    --via-virtio \
    --script '$RUN_DIR/script-09.txt' --script-expect 'gomail: cache-miss INBOX 9' --timeout 180

vgate_assert 09 serial-contains 'gomail: offline fetch INBOX'
vgate_assert 09 serial-contains 'gomail: box INBOX'
vgate_assert 09 serial-contains 'gomail: box Sent'
vgate_assert 09 serial-contains 'gomail: hdr 1 Alice: gate hello'
vgate_assert 09 serial-contains 'gomail: cache-miss INBOX 9'
vgate_assert 09 serial-contains 'gomail: closed'
vgate_assert 09 serial-absent 'gomail: dial'
vgate_assert 09 serial-absent 'gomail: body 9'
vgate_assert 09 serial-absent 'gomail: hdr 9'
vgate_assert 09 serial-absent 'gomail: error'
vgate_assert 09 serial-absent '[EXC] parking:'

# --- Run 10: offline read — a remembered body paints offline --------------
vgate_run 10 -- \
    --screen '$RUN_DIR/screen-10' \
    --via-virtio \
    --script '$RUN_DIR/script-10.txt' --script-expect 'gomail: closed' --timeout 180

vgate_assert 10 serial-contains 'gomail: offline read INBOX 1'
vgate_assert 10 serial-contains 'gomail: body 1 text/plain'
vgate_assert 10 serial-contains 'gomail: text line one of the gate body'
vgate_assert 10 serial-contains 'gomail: closed'

# --- Runs 11-13 (M196b #3379): the credential legs ---------------------
#
# Every dial above already proved the store row works ('gomail: secret
# mail-gate' + the wire's own LOGIN/AUTH bytes — the fixture account
# carries no pass=). These three legs name the three refusal shapes:
# run 11's GOMAIL-PLAIN.TXT still carries a plaintext pass= row and the
# file refuses by name before any dial; run 12's --pass argv refuses the
# same way (a password never rides the command line — the flag is
# recognized only so the refusal can name it); run 13 steers the lookup
# at mail-ghost via --user, which nothing provisioned, so the named
# secret_not_found refusal lands before any socket opens.
vgate_file script-11.txt <<'EOF'
exec GOMAIL.ELF --account /host/GOMAIL-PLAIN.TXT --tls fetch INBOX
EOF

vgate_run 11 -- \
    --screen '$RUN_DIR/screen-11' \
    --via-virtio \
    --script '$RUN_DIR/script-11.txt' --script-expect 'gomail: refuse pass /host/GOMAIL-PLAIN.TXT' --timeout 180

vgate_assert 11 serial-contains 'gomail: refuse pass /host/GOMAIL-PLAIN.TXT'
vgate_assert 11 serial-contains 'gomail: closed'
# No account line prints for a refused file, no socket opens, and the
# seeded value never echoes — the refusal names the row, not the secret.
vgate_assert 11 serial-absent 'gomail: account'
vgate_assert 11 serial-absent 'gomail: dial'
vgate_assert 11 serial-absent 'gomail: login'
vgate_assert 11 serial-absent 'gate-pass'
vgate_assert 11 serial-absent '[EXC] parking:'

vgate_file script-12.txt <<'EOF'
exec GOMAIL.ELF --pass argv-pass --tls fetch INBOX
EOF

vgate_run 12 -- \
    --screen '$RUN_DIR/screen-12' \
    --via-virtio \
    --script '$RUN_DIR/script-12.txt' --script-expect 'gomail: refuse pass argv' --timeout 180

vgate_assert 12 serial-contains 'gomail: refuse pass argv'
vgate_assert 12 serial-contains 'gomail: closed'
# The argv refusal lands before the file even loads.
vgate_assert 12 serial-absent 'gomail: account'
vgate_assert 12 serial-absent 'gomail: dial'
vgate_assert 12 serial-absent 'gomail: login'
vgate_assert 12 serial-absent 'gate-pass'
vgate_assert 12 serial-absent '[EXC] parking:'

vgate_file script-13.txt <<'EOF'
exec GOMAIL.ELF --tls --user ghost fetch INBOX
EOF

vgate_run 13 -- \
    --screen '$RUN_DIR/screen-13' \
    --via-virtio \
    --script '$RUN_DIR/script-13.txt' --script-expect 'gomail: refuse secret_not_found mail-ghost' --timeout 180

vgate_assert 13 serial-contains 'gomail: account /host/GOMAIL.TXT'
vgate_assert 13 serial-contains 'gomail: refuse secret_not_found mail-ghost'
vgate_assert 13 serial-contains 'gomail: closed'
vgate_assert 13 serial-absent 'gomail: dial'
vgate_assert 13 serial-absent 'gomail: login'
vgate_assert 13 serial-absent 'gate-pass'
vgate_assert 13 serial-absent '[EXC] parking:'

# --- Runs c1-c3 (M193d #3256): the chain's mail leg, K=3 ---------------
# The contact card's mail-them row execs `GOMAIL.ELF send --to
# grace@example.com` — the two-token --to form, no subject/body flags.
# Each leg drives that same verb shape at the SMTP+TLS fixture (the
# --tls flag stands in for the account file's transport row, which the
# spec's GOMAIL.TXT deliberately lacks): 'gomail: rcpt
# grace@example.com' is the argv's own echo and the transcript's
# RCPT TO:<grace@example.com> is the wire-level receipt. Three
# identical tagged runs is the K=3 acceptance — the same boot, three
# times, unchanged.
vgate_file script-c.txt <<'EOF'
net ip 10.0.0.1
net arp 10.0.0.2
exec GOMAIL.ELF --tls send --to grace@example.com
EOF

vgate_run c1 -- \
    --screen '$RUN_DIR/screen-c1' \
    --via-virtio \
    --net '$RUN_DIR/cap-c1.bin' --net-arp-respond 10.0.0.2 \
    --net-tcp-respond 10.0.0.2:465:relay --net-tcp-respond-relay 127.0.0.1:26465 \
    --script '$RUN_DIR/script-c.txt' --script-expect 'gomail: sent bytes=' --timeout 240

vgate_assert c1 serial-contains 'gomail: account /host/GOMAIL.TXT'
vgate_assert c1 serial-contains 'gomail: dial 10.0.0.2 465 tls'
vgate_assert c1 serial-contains 'gomail: smtp ready'
vgate_assert c1 serial-contains 'gomail: mail gate@fixture.local'
vgate_assert c1 serial-contains 'gomail: rcpt grace@example.com'
vgate_assert c1 serial-contains 'gomail: sent bytes='
vgate_assert c1 serial-contains 'gomail: closed'
vgate_assert c1 serial-absent 'gomail: error'
vgate_assert c1 serial-absent 'gomail: refuse'
vgate_assert c1 serial-absent '[EXC] parking:'
vgate_assert c1 python <<'PY'
import os, sys
log = open(os.path.join(os.environ["RUN_DIR"], "maild-26465.log")).read()
need = ["c1 rx RCPT TO:<grace@example.com>",
        "c1 rx MAIL FROM:<gate@fixture.local>",
        "c1 rx DATA", "c1 rx .", "c1 rx QUIT",
        "tx 250 2.0.0 queued as m001"]
missing = [n for n in need if n not in log]
if missing:
    sys.exit("chain smtp fixture transcript missing: %r\n%s" % (missing, log))
PY

vgate_run c2 -- \
    --screen '$RUN_DIR/screen-c2' \
    --via-virtio \
    --net '$RUN_DIR/cap-c2.bin' --net-arp-respond 10.0.0.2 \
    --net-tcp-respond 10.0.0.2:465:relay --net-tcp-respond-relay 127.0.0.1:26465 \
    --script '$RUN_DIR/script-c.txt' --script-expect 'gomail: sent bytes=' --timeout 240

vgate_assert c2 serial-contains 'gomail: account /host/GOMAIL.TXT'
vgate_assert c2 serial-contains 'gomail: dial 10.0.0.2 465 tls'
vgate_assert c2 serial-contains 'gomail: smtp ready'
vgate_assert c2 serial-contains 'gomail: mail gate@fixture.local'
vgate_assert c2 serial-contains 'gomail: rcpt grace@example.com'
vgate_assert c2 serial-contains 'gomail: sent bytes='
vgate_assert c2 serial-contains 'gomail: closed'
vgate_assert c2 serial-absent 'gomail: error'
vgate_assert c2 serial-absent 'gomail: refuse'
vgate_assert c2 serial-absent '[EXC] parking:'
vgate_assert c2 python <<'PY'
import os, sys
log = open(os.path.join(os.environ["RUN_DIR"], "maild-26465.log")).read()
need = ["c2 rx RCPT TO:<grace@example.com>",
        "c2 rx MAIL FROM:<gate@fixture.local>",
        "c2 rx DATA", "c2 rx .", "c2 rx QUIT",
        "tx 250 2.0.0 queued as m001"]
missing = [n for n in need if n not in log]
if missing:
    sys.exit("chain smtp fixture transcript missing: %r\n%s" % (missing, log))
PY

vgate_run c3 -- \
    --screen '$RUN_DIR/screen-c3' \
    --via-virtio \
    --net '$RUN_DIR/cap-c3.bin' --net-arp-respond 10.0.0.2 \
    --net-tcp-respond 10.0.0.2:465:relay --net-tcp-respond-relay 127.0.0.1:26465 \
    --script '$RUN_DIR/script-c.txt' --script-expect 'gomail: sent bytes=' --timeout 240

vgate_assert c3 serial-contains 'gomail: account /host/GOMAIL.TXT'
vgate_assert c3 serial-contains 'gomail: dial 10.0.0.2 465 tls'
vgate_assert c3 serial-contains 'gomail: smtp ready'
vgate_assert c3 serial-contains 'gomail: mail gate@fixture.local'
vgate_assert c3 serial-contains 'gomail: rcpt grace@example.com'
vgate_assert c3 serial-contains 'gomail: sent bytes='
vgate_assert c3 serial-contains 'gomail: closed'
vgate_assert c3 serial-absent 'gomail: error'
vgate_assert c3 serial-absent 'gomail: refuse'
vgate_assert c3 serial-absent '[EXC] parking:'
vgate_assert c3 python <<'PY'
import os, sys
log = open(os.path.join(os.environ["RUN_DIR"], "maild-26465.log")).read()
need = ["c3 rx RCPT TO:<grace@example.com>",
        "c3 rx MAIL FROM:<gate@fixture.local>",
        "c3 rx DATA", "c3 rx .", "c3 rx QUIT",
        "tx 250 2.0.0 queued as m001"]
missing = [n for n in need if n not in log]
if missing:
    sys.exit("chain smtp fixture transcript missing: %r\n%s" % (missing, log))
PY

# --- Runs w1-w3 (M208a #3822): the window legs, K=3 ---------------------
#
# A bare `exec GOMAIL.ELF` — the dock/launcher's argv shape — opens the
# tabapp window under a live GOTABWM seat. Each leg first clears the
# cache's remembered box + corpus so the open is deterministic: no USE
# means resolveBox's INBOX default, no HDRS/BODIES means the pane's
# evidence can only come from THIS leg's fetch. The chord batch is just
# 'f,f' — the pane nav letters/arrows stay app-side (the seat's chord
# palette claims some of them, so the leg proves the model through the
# keys that always reach): the first Fetch runs the existing fetch path
# and the fixture's envelopes land as the pane's new rows — "the new
# mail" the leg proves; the second reads seq 1's body through the same
# path and the message pane renders it on reload. GOMAIL-TLS.TXT is
# cloned over GOMAIL.TXT in each leg's own script (the shared file
# stays transport-less for run 03; the legs run LAST so the mutation
# reaches no CLI leg). SESSION.TABS is cleared so no stale session row
# re-execs a second GOMAIL tab mid-leg.
vgate_file script-w.txt <<'EOF'
net ip 10.0.0.1
net arp 10.0.0.2
set GOMAXPROCS=1
vf rm GOTABWM.DEMO
vf rm SESSION.TABS
vf rm GOMAIL.TXT
vf clone GOMAIL-TLS.TXT GOMAIL.TXT
vf rm MAIL/USE
vf rm MAIL/HDRS
vf rm MAIL/BODIES
wm
exec GOTABWM.ELF
EOF

vgate_file script2-w.txt <<'EOF'
dui focus 0
exec GOMAIL.ELF
EOF

vgate_run w1 -- \
    --screen '$RUN_DIR/screen-w1' \
    --via-virtio \
    --net '$RUN_DIR/cap-w1.bin' --net-arp-respond 10.0.0.2 \
    --net-tcp-respond 10.0.0.2:993:relay --net-tcp-respond-relay 127.0.0.1:26993 \
    --script '$RUN_DIR/script-w.txt' \
    --script2 '$RUN_DIR/script2-w.txt' --script2-after 'gotabwm: mode live' \
    --input-chords 'f,f' \
    --input-chords-after 'gomail: window' \
    --script-expect 'gomail: fetch done' --timeout 300

vgate_assert w1 serial-contains 'exec: loaded GOTABWM.ELF'
vgate_assert w1 serial-contains 'gotabwm: mode live'
vgate_assert w1 serial-contains 'exec: loaded GOMAIL.ELF'
vgate_assert w1 serial-contains 'gomail: account /host/GOMAIL.TXT'
vgate_assert w1 serial-contains 'gomail: open id='
vgate_assert w1 serial-contains 'gomail: declare accepted'
vgate_assert w1 serial-contains 'gomail: box INBOX'
vgate_assert w1 serial-contains 'gomail: box Sent'
vgate_assert w1 serial-contains 'gomail: window'
vgate_assert w1 serial-contains 'gomail: fetch INBOX'
vgate_assert w1 serial-contains 'gomail: secret mail-gate'
vgate_assert w1 serial-contains 'gomail: dial 10.0.0.2 993 tls'
vgate_assert w1 serial-contains 'gomail: imap ready'
vgate_assert w1 serial-contains 'gomail: select INBOX exists=3'
vgate_assert w1 serial-contains 'gomail: hdr 1 Alice: gate hello'
vgate_assert w1 serial-contains 'gomail: hdr 3 Carol: report attached'
vgate_assert w1 serial-contains 'gomail: body 1 text/plain'
vgate_assert w1 serial-contains 'gomail: text line one of the gate body'
vgate_assert w1 serial-contains 'gomail: cache /host/MAIL/HDRS'
vgate_assert w1 serial-contains 'gomail: fetch done'
vgate_assert w1 serial-absent 'gomail: error'
vgate_assert w1 serial-absent 'gomail: refuse'
vgate_assert w1 serial-absent '[EXC] parking:'
# The fetched rows really landed on the share.
vgate_assert w1 share-contains MAIL/HDRS 'gate hello'
vgate_assert w1 python <<'PY'
import os, sys
log = open(os.path.join(os.environ["RUN_DIR"], "maild-26993.log")).read()
need = ["c3 tls ok", "c3 rx a1 LOGIN", 'c3 rx a3 SELECT "INBOX"',
        "c3 rx a4 FETCH", "c4 tls ok", 'c4 rx a3 SELECT "INBOX"',
        "c4 rx a5 FETCH 1 (BODYSTRUCTURE)"]
missing = [n for n in need if n not in log]
if missing:
    sys.exit("w1 imap fixture transcript missing: %r\n%s" % (missing, log))
PY

vgate_run w2 -- \
    --screen '$RUN_DIR/screen-w2' \
    --via-virtio \
    --net '$RUN_DIR/cap-w2.bin' --net-arp-respond 10.0.0.2 \
    --net-tcp-respond 10.0.0.2:993:relay --net-tcp-respond-relay 127.0.0.1:26993 \
    --script '$RUN_DIR/script-w.txt' \
    --script2 '$RUN_DIR/script2-w.txt' --script2-after 'gotabwm: mode live' \
    --input-chords 'f,f' \
    --input-chords-after 'gomail: window' \
    --script-expect 'gomail: fetch done' --timeout 300

vgate_assert w2 serial-contains 'gomail: open id='
vgate_assert w2 serial-contains 'gomail: window'
vgate_assert w2 serial-contains 'gomail: box INBOX'
vgate_assert w2 serial-contains 'gomail: fetch INBOX'
vgate_assert w2 serial-contains 'gomail: dial 10.0.0.2 993 tls'
vgate_assert w2 serial-contains 'gomail: select INBOX exists=3'
vgate_assert w2 serial-contains 'gomail: hdr 1 Alice: gate hello'
vgate_assert w2 serial-contains 'gomail: body 1 text/plain'
vgate_assert w2 serial-contains 'gomail: text line one of the gate body'
vgate_assert w2 serial-contains 'gomail: fetch done'
vgate_assert w2 serial-absent 'gomail: error'
vgate_assert w2 serial-absent 'gomail: refuse'
vgate_assert w2 serial-absent '[EXC] parking:'
vgate_assert w2 python <<'PY'
import os, sys
log = open(os.path.join(os.environ["RUN_DIR"], "maild-26993.log")).read()
need = ["c5 tls ok", 'c5 rx a3 SELECT "INBOX"', "c5 rx a4 FETCH",
        "c6 tls ok", 'c6 rx a3 SELECT "INBOX"']
missing = [n for n in need if n not in log]
if missing:
    sys.exit("w2 imap fixture transcript missing: %r\n%s" % (missing, log))
PY

vgate_run w3 -- \
    --screen '$RUN_DIR/screen-w3' \
    --via-virtio \
    --net '$RUN_DIR/cap-w3.bin' --net-arp-respond 10.0.0.2 \
    --net-tcp-respond 10.0.0.2:993:relay --net-tcp-respond-relay 127.0.0.1:26993 \
    --script '$RUN_DIR/script-w.txt' \
    --script2 '$RUN_DIR/script2-w.txt' --script2-after 'gotabwm: mode live' \
    --input-chords 'f,f' \
    --input-chords-after 'gomail: window' \
    --script-expect 'gomail: fetch done' --timeout 300

vgate_assert w3 serial-contains 'gomail: open id='
vgate_assert w3 serial-contains 'gomail: window'
vgate_assert w3 serial-contains 'gomail: box INBOX'
vgate_assert w3 serial-contains 'gomail: fetch INBOX'
vgate_assert w3 serial-contains 'gomail: dial 10.0.0.2 993 tls'
vgate_assert w3 serial-contains 'gomail: select INBOX exists=3'
vgate_assert w3 serial-contains 'gomail: hdr 1 Alice: gate hello'
vgate_assert w3 serial-contains 'gomail: body 1 text/plain'
vgate_assert w3 serial-contains 'gomail: text line one of the gate body'
vgate_assert w3 serial-contains 'gomail: fetch done'
vgate_assert w3 serial-absent 'gomail: error'
vgate_assert w3 serial-absent 'gomail: refuse'
vgate_assert w3 serial-absent '[EXC] parking:'
vgate_assert w3 python <<'PY'
import os, sys
log = open(os.path.join(os.environ["RUN_DIR"], "maild-26993.log")).read()
need = ["c7 tls ok", 'c7 rx a3 SELECT "INBOX"', "c7 rx a4 FETCH",
        "c8 tls ok", 'c8 rx a3 SELECT "INBOX"']
missing = [n for n in need if n not in log]
if missing:
    sys.exit("w3 imap fixture transcript missing: %r\n%s" % (missing, log))
PY

# --- Runs m1-m3 (M208b #3823): compose/send + attachment hand-off, K=3 --
#
# Each leg boots the seat, opens the window, and types a whole message
# through the compose pane: 'c' opens it, the To/Subject/Body fields
# fill in order (Tab walks the ring), Ctrl-S submits through the same
# runSend path the verb drives — the fixture's RCPT TO:<bob> is the
# wire receipt. 'q' then quits the window so the console is free again.
# script3 runs the four attachment legs in order: fetch Attach saves
# the three fixture parts under /host/MAIL (base64-decoded), `open`
# routes invite.ics to GOCLOCK's headless merge and card.vcf to
# GOFILES' contact face, and payload.bin refuses by type — saved,
# never executed. The leg's own script clears ATTACH, the saved names,
# and GOCLOCK.ALARM first so all three runs are byte-identical.
# exec spawns async (the shell does not wait), so each leg runs under
# `&` and the following `fg` lines hold it: one fg waits up to ~5 ticks
# on the latest background job, so the run of fgs gives the fetch its
# whole window before the opens read MAIL/ATTACH — without the wait
# the opens race the fetch and refuse on the missing index.
vgate_file script-m.txt <<'EOF'
net ip 10.0.0.1
net arp 10.0.0.2
set GOMAXPROCS=1
vf rm GOTABWM.DEMO
vf rm SESSION.TABS
vf rm GOMAIL.TXT
vf clone GOMAIL-TLS.TXT GOMAIL.TXT
vf rm MAIL/USE
vf rm MAIL/ATTACH
vf rm MAIL/invite.ics
vf rm MAIL/card.vcf
vf rm MAIL/payload.bin
vf rm GOCLOCK.ALARM
wm
exec GOTABWM.ELF
EOF

vgate_file script2-m.txt <<'EOF'
dui focus 0
exec GOMAIL.ELF
EOF

vgate_file script3-m.txt <<'EOF'
exec GOMAIL.ELF --tls fetch Attach 1 2 3 &
fg
fg
fg
fg
fg
fg
fg
fg
fg
fg
fg
fg
fg
fg
fg
fg
fg
fg
fg
fg
exec GOMAIL.ELF open invite.ics &
fg
fg
fg
fg
fg
fg
fg
fg
exec GOMAIL.ELF open card.vcf &
fg
fg
fg
fg
fg
fg
fg
fg
exec GOMAIL.ELF open payload.bin
EOF

vgate_run m1 -- \
    --screen '$RUN_DIR/screen-m1' \
    --via-virtio \
    --net '$RUN_DIR/cap-m1.bin' --net-arp-respond 10.0.0.2 \
    --net-tcp-respond 10.0.0.2:993:relay --net-tcp-respond-relay 127.0.0.1:26993 \
    --net-tcp-respond 10.0.0.2:465:relay --net-tcp-respond-relay 127.0.0.1:26465 \
    --script '$RUN_DIR/script-m.txt' \
    --script2 '$RUN_DIR/script2-m.txt' --script2-after 'gotabwm: mode live' \
    --input-chords 'c,b,o,b,tab,g,a,t,e,space,s,y,n,c,tab,g,a,t,e,space,m,a,i,l,ctrl-s,q' \
    --input-chords-after 'gomail: window' \
    --script3 '$RUN_DIR/script3-m.txt' --script3-after 'gomail: closed' \
    --script-expect 'gomail: refuse open payload.bin application/octet-stream' --timeout 300

vgate_assert m1 serial-contains 'exec: loaded GOTABWM.ELF'
vgate_assert m1 serial-contains 'gotabwm: mode live'
vgate_assert m1 serial-contains 'gomail: open id='
vgate_assert m1 serial-contains 'gomail: window'
# The compose pane typed, submitted and named itself.
vgate_assert m1 serial-contains 'gomail: compose'
vgate_assert m1 serial-contains 'gomail: smtp ready'
vgate_assert m1 serial-contains 'gomail: mail gate@fixture.local'
vgate_assert m1 serial-contains 'gomail: rcpt bob'
vgate_assert m1 serial-contains 'gomail: sent bytes='
vgate_assert m1 serial-contains 'gomail: compose sent'
# The fetch proved the Attach box and saved all three parts.
vgate_assert m1 serial-contains 'gomail: box Attach'
vgate_assert m1 serial-contains 'gomail: select Attach exists=3'
vgate_assert m1 serial-contains 'gomail: attach 1 invite.ics text/calendar'
vgate_assert m1 serial-contains 'gomail: attach 2 card.vcf text/vcard'
vgate_assert m1 serial-contains 'gomail: attach 3 payload.bin application/octet-stream'
vgate_assert m1 serial-contains 'gomail: saved /host/MAIL/invite.ics'
vgate_assert m1 serial-contains 'gomail: saved /host/MAIL/card.vcf'
vgate_assert m1 serial-contains 'gomail: saved /host/MAIL/payload.bin'
# The opens= route: .ics -> GOCLOCK's merge markers, .vcf -> GOFILES'
# contact face, .bin -> named refusal and no exec.
vgate_assert m1 serial-contains 'gomail: open invite.ics handler=GOCLOCK.ELF'
vgate_assert m1 serial-contains 'goclock: import 15:00 once Mail sync'
vgate_assert m1 serial-contains 'goclock: import done added=1 refused=0'
vgate_assert m1 serial-contains 'gomail: open card.vcf handler=GOFILES.ELF'
vgate_assert m1 serial-contains 'gofiles: quicklook card.vcf kind=contact'
vgate_assert m1 serial-contains 'gofiles: contact fn=Grace Hopper'
vgate_assert m1 serial-contains 'gofiles: contact done cards=1'
vgate_assert m1 serial-contains 'gomail: refuse open payload.bin application/octet-stream'
vgate_assert m1 serial-absent 'gomail: error'
vgate_assert m1 serial-absent 'gomail: attach-fail'
vgate_assert m1 serial-absent 'gofiles: contact refused'
vgate_assert m1 serial-absent '[EXC] parking:'
# The saved payloads really landed, decoded — never the wire's b64.
vgate_assert m1 share-contains MAIL/invite.ics 'BEGIN:VCALENDAR'
vgate_assert m1 share-contains MAIL/invite.ics 'SUMMARY:Mail sync'
vgate_assert m1 share-contains MAIL/card.vcf 'FN:Grace Hopper'
vgate_assert m1 share-contains MAIL/payload.bin 'payload-not-a-program'
vgate_assert m1 share-contains MAIL/ATTACH 'invite.ics'
vgate_assert m1 python <<'PY'
import os, sys
ilog = open(os.path.join(os.environ["RUN_DIR"], "maild-26993.log")).read()
slog = open(os.path.join(os.environ["RUN_DIR"], "maild-26465.log")).read()
ineed = ["c9 tls ok", 'c9 rx a3 SELECT "Attach"',
         "c9 rx a4 FETCH 1:3 (ENVELOPE)", "c9 rx a5 FETCH 1 (BODYSTRUCTURE)",
         "c9 rx a6 FETCH 1 (BODY[1])", "c9 rx a7 FETCH 1 (BODY[2])",
         "FETCH 2 (BODY[2])", "FETCH 3 (BODY[2])"]
sneed = ["c4 tls ok", "c4 rx RCPT TO:<bob>", "c4 rx DATA",
         "c4 rx Subject: gate sync", "c4 rx gate mail", "c4 rx QUIT"]
missing = [n for n in ineed if n not in ilog]
missing += [n for n in sneed if n not in slog]
if missing:
    sys.exit("m1 fixture transcript missing: %r\nimap:\n%s\nsmtp:\n%s"
             % (missing, ilog, slog))
PY

vgate_run m2 -- \
    --screen '$RUN_DIR/screen-m2' \
    --via-virtio \
    --net '$RUN_DIR/cap-m2.bin' --net-arp-respond 10.0.0.2 \
    --net-tcp-respond 10.0.0.2:993:relay --net-tcp-respond-relay 127.0.0.1:26993 \
    --net-tcp-respond 10.0.0.2:465:relay --net-tcp-respond-relay 127.0.0.1:26465 \
    --script '$RUN_DIR/script-m.txt' \
    --script2 '$RUN_DIR/script2-m.txt' --script2-after 'gotabwm: mode live' \
    --input-chords 'c,b,o,b,tab,g,a,t,e,space,s,y,n,c,tab,g,a,t,e,space,m,a,i,l,ctrl-s,q' \
    --input-chords-after 'gomail: window' \
    --script3 '$RUN_DIR/script3-m.txt' --script3-after 'gomail: closed' \
    --script-expect 'gomail: refuse open payload.bin application/octet-stream' --timeout 300

vgate_assert m2 serial-contains 'gomail: window'
vgate_assert m2 serial-contains 'gomail: compose'
vgate_assert m2 serial-contains 'gomail: rcpt bob'
vgate_assert m2 serial-contains 'gomail: sent bytes='
vgate_assert m2 serial-contains 'gomail: select Attach exists=3'
vgate_assert m2 serial-contains 'gomail: saved /host/MAIL/invite.ics'
vgate_assert m2 serial-contains 'gomail: saved /host/MAIL/card.vcf'
vgate_assert m2 serial-contains 'gomail: saved /host/MAIL/payload.bin'
vgate_assert m2 serial-contains 'gomail: open invite.ics handler=GOCLOCK.ELF'
vgate_assert m2 serial-contains 'goclock: import done added=1 refused=0'
vgate_assert m2 serial-contains 'gomail: open card.vcf handler=GOFILES.ELF'
vgate_assert m2 serial-contains 'gofiles: contact fn=Grace Hopper'
vgate_assert m2 serial-contains 'gofiles: contact done cards=1'
vgate_assert m2 serial-contains 'gomail: refuse open payload.bin application/octet-stream'
vgate_assert m2 serial-absent 'gomail: error'
vgate_assert m2 serial-absent 'gomail: attach-fail'
vgate_assert m2 serial-absent '[EXC] parking:'
vgate_assert m2 python <<'PY'
import os, sys
ilog = open(os.path.join(os.environ["RUN_DIR"], "maild-26993.log")).read()
slog = open(os.path.join(os.environ["RUN_DIR"], "maild-26465.log")).read()
ineed = ["c10 tls ok", 'c10 rx a3 SELECT "Attach"', "FETCH 3 (BODY[2])"]
sneed = ["c5 tls ok", "c5 rx RCPT TO:<bob>"]
missing = [n for n in ineed if n not in ilog]
missing += [n for n in sneed if n not in slog]
if missing:
    sys.exit("m2 fixture transcript missing: %r" % missing)
PY

vgate_run m3 -- \
    --screen '$RUN_DIR/screen-m3' \
    --via-virtio \
    --net '$RUN_DIR/cap-m3.bin' --net-arp-respond 10.0.0.2 \
    --net-tcp-respond 10.0.0.2:993:relay --net-tcp-respond-relay 127.0.0.1:26993 \
    --net-tcp-respond 10.0.0.2:465:relay --net-tcp-respond-relay 127.0.0.1:26465 \
    --script '$RUN_DIR/script-m.txt' \
    --script2 '$RUN_DIR/script2-m.txt' --script2-after 'gotabwm: mode live' \
    --input-chords 'c,b,o,b,tab,g,a,t,e,space,s,y,n,c,tab,g,a,t,e,space,m,a,i,l,ctrl-s,q' \
    --input-chords-after 'gomail: window' \
    --script3 '$RUN_DIR/script3-m.txt' --script3-after 'gomail: closed' \
    --script-expect 'gomail: refuse open payload.bin application/octet-stream' --timeout 300

vgate_assert m3 serial-contains 'gomail: window'
vgate_assert m3 serial-contains 'gomail: compose'
vgate_assert m3 serial-contains 'gomail: rcpt bob'
vgate_assert m3 serial-contains 'gomail: sent bytes='
vgate_assert m3 serial-contains 'gomail: select Attach exists=3'
vgate_assert m3 serial-contains 'gomail: saved /host/MAIL/invite.ics'
vgate_assert m3 serial-contains 'gomail: saved /host/MAIL/card.vcf'
vgate_assert m3 serial-contains 'gomail: saved /host/MAIL/payload.bin'
vgate_assert m3 serial-contains 'gomail: open invite.ics handler=GOCLOCK.ELF'
vgate_assert m3 serial-contains 'goclock: import done added=1 refused=0'
vgate_assert m3 serial-contains 'gomail: open card.vcf handler=GOFILES.ELF'
vgate_assert m3 serial-contains 'gofiles: contact fn=Grace Hopper'
vgate_assert m3 serial-contains 'gofiles: contact done cards=1'
vgate_assert m3 serial-contains 'gomail: refuse open payload.bin application/octet-stream'
vgate_assert m3 serial-absent 'gomail: error'
vgate_assert m3 serial-absent 'gomail: attach-fail'
vgate_assert m3 serial-absent '[EXC] parking:'
vgate_assert m3 python <<'PY'
import os, sys
ilog = open(os.path.join(os.environ["RUN_DIR"], "maild-26993.log")).read()
slog = open(os.path.join(os.environ["RUN_DIR"], "maild-26465.log")).read()
ineed = ["c11 tls ok", 'c11 rx a3 SELECT "Attach"', "FETCH 3 (BODY[2])"]
sneed = ["c6 tls ok", "c6 rx RCPT TO:<bob>"]
missing = [n for n in ineed if n not in ilog]
missing += [n for n in sneed if n not in slog]
if missing:
    sys.exit("m3 fixture transcript missing: %r" % missing)
PY
