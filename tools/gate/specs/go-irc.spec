# go-irc.spec -- M147a #2801 + M147b #2802 + M147c #2803: GOIRC.ELF
# protocol core vs a host Python fixture IRCd over the --net-tcp-respond
# relay; runs 04-08 drive the --ui tabapp front-end under a live GOTABWM
# seat. M152e #2919 adds run 09: /hl highlight rules firing the mention
# toast + the bounded per-channel log file's 32-line flush on the share.
#
# Run 01 is the M147a card leg: register (NICK/USER -> 001), JOIN #goirc,
# receive a PRIVMSG, hard-drop, watch the client back off, re-register
# and REJOIN (the 'goirc: rejoin #goirc' marker + the fixture's
# post-rejoin REJOIN-MARKER message). Run 02 refuses plaintext without
# the session consent bit -- 'goirc: refuse plaintext' before any dial.
# Run 03 exercises 433 nick-in-use retry (gate -> gate_ -> gate__).
#
# M147b (#2802):
#   run 04 -- the mention chain end to end: GOIRC --ui joins #goirc +
#     #other under the seat, the fixture sends alice's "ping gate"
#     on #other, the app posts vi.Notify ('goirc: toast #other' +
#     'gotabwm: notify paint id='), a seat alt-tab blurs GOIRC to
#     GOTABAPP, and the toast click refocuses it -- the pending mention
#     lands the user on #other ('goirc: focus #other').
#   run 05 -- plaintext without a stored consent asks the dialog and
#     No is the safe default: 'return' on the focused No refuses by
#     name before any dial.
#   run 06 -- the same dialog with 'tab,return' (focus Yes): the answer
#     is stored ('goirc: state saved' from the seat's kind-20 ack) and
#     the session dials + joins.
#
# M196c (#3380) adds runs 10+11. Run 10 is the stored-SASL leg: the
# 'irc-gate' row rides the share's SECRETS.TXT (app-bound to the
# uid_system-owned GOIRC.ELF), '--sasl=gate' resolves it through slot 70,
# the fixture only 903s when the AUTHENTICATE payload decodes to the
# row's value, and a 'procs' read gated on 'goirc: sasl ok' plus a
# serial-absent pin the password out of every line the boot printed --
# the card's "no password substring in argv". Run 11 is the argv
# refusal: '--sasl acct:pass' prints 'goirc: refuse sasl-argv' and never
# dials.
#
# #3779 adds runs 12+13, the first-run connect face: run 12 rm's
# GOIRC.TXT and bare-execs GOIRC.ELF under the seat -- the server+nick
# prompts take --input-string's two lines, the consent ask answers, and
# 'goirc: config saved' proves the register-time persist; run 13 boots
# the very next share state (the file run 12 wrote stays put), re-execs
# bare, and the picker's stored row connects with no prompt and no
# consent re-ask.
#
# M212c (#3874) adds runs 14-16, the WebSocket wave: run 14 is the
# codec's echo leg (WSECHO.ELF vs wsd.py's echo mode -- text, binary,
# ping/pong, fragmented text, close); run 15 is the consumer leg
# (GOIRC's --ws gateway transport, ircv3 line-per-message framing);
# run 16 dials five conn-indexed poisons for the named refusals.
#
# M147c (#2803) is the K=3 acceptance: vgate_repeat 3 replays every leg.
# Run 07 is the scripted conversation the card names -- TLS first,
# plaintext only after the stored consent, a mention toast with a real
# click-through inside its TTL, then the seat's ctrl-shift-s
# SNAPSHOT.BUNDLE as the terminal marker; run 08 is the restore boot
# (SESSION.TABS re-execs both rows bare -- no consent re-ask -- and the
# fixture's 'restored-tail' proves the scrollback/channels round-trip,
# corroborated by a byte-level decode of the bundle's session trailer).
# The armed share persists across iterations, so run 07's script re-rm's
# GOTABWM.DEMO/SESSION.TABS/NOTIFY.HIST/SNAPSHOT.BUNDLE and the fixture
# conn budget counts per-iteration legs.
#
# exec-order: assert-proven -- run 07's stage gates anchor on markers
# only the seat or the app prints ('goirc: join #other', 'goirc:
# consent ask', 'gotabwm: notify paint id='); run 07 ends on the
# snapshot marker the seat prints after publishing, run 08 on a
# fixture-answered PRIVMSG, so a program that never ran cannot pass.
#
# HOST PREREQUISITES:
#   bash tools/go/build-web.sh goirc GOIRC --gate -> .build/go/GOIRC-GATE.ELF
#   bash tools/go/build-gotabwm.sh                 -> .build/go/GOTABWM.ELF
#   bash tools/go/build-tabapp.sh                  -> .build/go/GOTABAPP.ELF
# GOIRC-GATE is the virelai_gate_roots build (the GOFETCH-GATE pattern):
# run 07's TLS leg anchors on the pinned fixture root, so the gate
# binary is what every run execs under the GOIRC.ELF name.
# Fixture transcripts land in $RUN_DIR/ircd-<port>.log for the python
# asserts. Run 07's TLS leg wraps the same pinned leaf.example.com
# chain go-fetch-https serves (user/src/lib/tls/vectors/fx).

vgate_name go-irc "M147a+b+c #2801/#2802/#2803: GOIRC.ELF core (register/join/recv/drop/reconnect, consent refusal, 433) + UI legs (mention toast click-through, consent dialog) + K=3 journey (TLS first, consent-stored plaintext, toast, snapshot, restore) + M152e #2919 run 09 (/hl toast, /log share file tail) + M196c #3380 runs 10-11 (slot-70 stored SASL + procs no-secret-on-argv, sasl-argv refusal) + #3779 runs 12-13 (first-run connect face: server+nick prompts + GOIRC.TXT persist, saved-server picker) + M212c #3874 runs 14-16 (WSECHO echo leg, GOIRC --ws consumer leg, five named ws refusals)"
vgate_share seed
vgate_runner_flags -Xswiftc -DSPIKE
vgate_repeat 3 BOOTS

# The monitor's exec verb packs at most 8 argv slots and the program
# name takes one (max_exec_args); the --flag=value spellings keep each
# leg inside the bound. script-01 relies on --script-expect/--timeout to
# end the leg, so no --max-ms.
vgate_file script-01.txt <<'EOF'
net ip 10.0.0.1
net arp 10.0.0.2
vf rm GOTABWM.DEMO
vf rm SESSION.TABS
vf rm NOTIFY.HIST
exec GOIRC.ELF --host=10.0.0.2 --nick=gate --join '#goirc' --plain --retries=3 --backoff-ms=300
EOF

vgate_file script-02.txt <<'EOF'
net ip 10.0.0.1
net arp 10.0.0.2
vf rm GOTABWM.DEMO
vf rm SESSION.TABS
vf rm NOTIFY.HIST
exec GOIRC.ELF --host=10.0.0.2 --nick=gate --join '#goirc' --max-ms=30000
EOF

vgate_file script-03.txt <<'EOF'
net ip 10.0.0.1
net arp 10.0.0.2
vf rm GOTABWM.DEMO
vf rm SESSION.TABS
vf rm NOTIFY.HIST
exec GOIRC.ELF --host=10.0.0.2 --port=6668 --nick=gate --join '#goirc' --plain --max-ms=60000
EOF

vgate_setup_python <<'PY'
import os, shutil, subprocess, sys, time

run = os.environ["RUN_DIR"]
share = os.environ.get("VG_SHARE") or os.path.join(run, "share")
os.makedirs(share, exist_ok=True)

goirc = os.path.join(".build", "go", "GOIRC-GATE.ELF")
if not os.path.exists(goirc):
    sys.exit("GOIRC-GATE.ELF missing (expected " + goirc + ") - build it "
             "first: bash tools/go/build-web.sh goirc GOIRC --gate")
# The gate-roots build rides under the shipping name: run 07's TLS leg
# validates the pinned fixture chain, and the seat's reopen LIFO re-execs
# 'GOIRC.ELF' regardless.
shutil.copy(goirc, os.path.join(share, "GOIRC.ELF"))
print("go-irc: staged GOIRC-GATE.ELF as GOIRC.ELF (%d)"
      % os.path.getsize(os.path.join(share, "GOIRC.ELF")))

# M147b runs 04-06 drive --ui under a live seat: stage the seat and the
# second tab the rail-click blur needs.
for name, script in (("GOTABWM.ELF", "build-gotabwm.sh"),
                     ("GOTABAPP.ELF", "build-tabapp.sh")):
    src = os.path.join(".build", "go", name)
    if not os.path.exists(src):
        sys.exit(name + " missing (expected " + src + ") - build it first: "
                 "bash tools/go/" + script)
    shutil.copy(src, os.path.join(share, name))
    print("go-irc: staged %s (%d)" % (name, os.path.getsize(src)))

# The armed share is ONE share for all 13 runs x K=3, so the boot
# autostart must not fire: it exec'd GOTABWM.ELF + restored a stale
# SESSION.TABS before the script's first `vf rm` could clear it (run 07
# then snapshotted a third row, run 08 restored n=3 — #3708). `wm=none`
# (the go-emoji/go-vault idiom) keeps every boot seatless; each run's
# script execs GOTABWM.ELF itself — same seat, same restore code path,
# same 'exec: loaded GOTABWM.ELF' marker, and run 08's n=2 restore still
# loads the file run 07's snapshot wrote because only run 08's script
# leaves SESSION.TABS in place.
with open(os.path.join(share, "SETTINGS.TXT"), "w") as f:
    f.write("#v2\nwm=none\n")

# M196c (#3380): the stored-SASL row run 10 resolves. `irc-gate` is
# app-bound to the uid_system-owned GOIRC.ELF (the FLINT agent-sign
# shape); the value exists only here and in the guest's kernel store —
# no script line carries it.
open(os.path.join(share, "SECRETS.TXT"), "w", encoding="utf-8").write(
    "#v1\nirc-gate\t1000\tsasl-secret-7\tGOIRC.ELF\n"
)
open(os.path.join(share, "OWNERS.TXT"), "w", encoding="utf-8").write(
    "#v1\nGOIRC.ELF\t644\t0\t-\n"
)

fixture = os.path.join(run, "ircd.py")
with open(fixture, "w") as f:
    f.write('''
import base64, socket, sys, time

mode, port, logpath = sys.argv[1], int(sys.argv[2]), sys.argv[3]
logf = open(logpath, "w", buffering=1)

def log(s):
    logf.write(s + "\\n")

# journey (M147c run 07) serves BOTH transports on one port — the
# runner arms a single --net-tcp-respond per VM, so the fixture
# discriminates by first byte: a 0x16 record header is a TLS ClientHello
# (wrapped in a TLS 1.3 responder serving the pinned leaf.example.com
# chain — the same material go-fetch-https's tlsresponder.py runs);
# anything else is plaintext IRC. argv[4]/[5] carry the PEM pair only
# for that mode.
tls_ctx = None
if mode == "journey":
    import ssl
    tls_ctx = ssl.SSLContext(ssl.PROTOCOL_TLS_SERVER)
    tls_ctx.minimum_version = ssl.TLSVersion.TLSv1_3
    tls_ctx.maximum_version = ssl.TLSVersion.TLSv1_3
    tls_ctx.load_cert_chain(sys.argv[4], sys.argv[5])

srv = socket.socket(socket.AF_INET, socket.SOCK_STREAM)
srv.setsockopt(socket.SOL_SOCKET, socket.SO_REUSEADDR, 1)
srv.bind(("127.0.0.1", port))
srv.listen(4)
srv.settimeout(1800)  # outlives the slowest leg sequence
log("listening mode=" + mode)

def send(conn, line):
    conn.sendall(line.encode() + b"\\r\\n")
    log("tx " + line)

def recvline(conn, buf):
    while b"\\n" not in buf:
        chunk = conn.recv(4096)
        if not chunk:
            return None, buf
        buf += chunk
    line, buf = buf.split(b"\\n", 1)
    return line.decode("utf-8", "replace").rstrip("\\r"), buf

def register(conn, idx, taken=()):
    """Read until NICK+USER (optionally refusing nicks in `taken` with
    433), then 001. Returns (nick, buf) or (None, buf) on close."""
    buf, nick, user = b"", None, False
    deadline = time.time() + 60
    while time.time() < deadline:
        line, buf = recvline(conn, buf)
        if line is None:
            return None, buf
        log("c%d rx %s" % (idx, line))
        parts = line.split()
        if not parts:
            continue
        cmd = parts[0].upper()
        if cmd == "NICK" and len(parts) > 1:
            nick = parts[1]
            if nick in taken:
                send(conn, ":srv 433 * %s :Nickname is already in use" % nick)
                nick = None
        elif cmd == "USER":
            user = True
        elif cmd == "PING":
            send(conn, ":srv PONG srv :" + (parts[1] if len(parts) > 1 else ""))
        if nick and user:
            send(conn, ":srv 001 %s :Welcome to the fixture net" % nick)
            return nick, buf
    return None, buf

def pump(conn, idx, nick, buf, on_join, hold=20, first_only=False,
         echo=False, on_privmsg=None):
    """Read commands until close/timeout; on_join(chan) fires per JOIN.
    first_only exits after the first JOIN — that close IS the drop the
    leg asserts. echo answers each client PRIVMSG with alice's
    'thanks' — the conversation's return half (M147c run 07); the
    reply deliberately carries NO nick — a nick-bearing echo would
    paint a second toast on the sending session and the pointer's
    'notify paint' gate could land on it instead of the #other ping's.
    on_privmsg fires once per client PRIVMSG after the echo (the
    journey's delayed-mention hook)."""
    deadline = time.time() + hold
    while time.time() < deadline:
        conn.settimeout(max(1.0, deadline - time.time()))
        try:
            line, buf = recvline(conn, buf)
        except socket.timeout:
            break
        if line is None:
            break
        log("c%d rx %s" % (idx, line))
        parts = line.split()
        if not parts:
            continue
        cmd = parts[0].upper()
        if cmd == "JOIN" and len(parts) > 1:
            ch = parts[1].lstrip(":")
            send(conn, ":%s!u@h JOIN %s" % (nick, ch))
            on_join(ch)
            if first_only:
                return
        elif cmd == "PING":
            send(conn, ":srv PONG srv :" + (parts[1] if len(parts) > 1 else ""))
        elif cmd == "PRIVMSG" and len(parts) > 2:
            if echo:
                send(conn, ":alice!u@h PRIVMSG %s :thanks" % parts[1])
            if on_privmsg is not None:
                on_privmsg()
        elif cmd == "QUIT":
            break

def sasl_session(conn, idx):
    """M196c (#3380) run 10: the client sends CAP REQ :sasl, NICK and
    USER up front. The fixture ACKs, asks for PLAIN, and only 903s when
    the AUTHENTICATE payload decodes to the store row's
    'gate\\x00gate\\x00sasl-secret-7' — 'goirc: sasl ok' is therefore the
    slot-70 round-trip's proof, not a handshake formality. A wrong
    payload earns a 904 and 001 is withheld until CAP END."""
    buf, nick, user, authz = b"", None, False, False
    deadline = time.time() + 60
    while time.time() < deadline:
        line, buf = recvline(conn, buf)
        if line is None:
            return
        log("c%d rx %s" % (idx, line))
        parts = line.split()
        if not parts:
            continue
        cmd = parts[0].upper()
        if cmd == "CAP" and len(parts) > 1 and parts[1].upper() == "REQ":
            send(conn, ":srv CAP * ACK :sasl")
        elif cmd == "NICK" and len(parts) > 1:
            nick = parts[1]
        elif cmd == "USER":
            user = True
        elif cmd == "AUTHENTICATE" and len(parts) > 1:
            if parts[1].upper() == "PLAIN":
                send(conn, "AUTHENTICATE +")
            else:
                got = base64.b64decode(parts[1]).split(b"\\x00")
                if got == [b"gate", b"gate", b"sasl-secret-7"]:
                    authz = True
                    log("c%d sasl payload ok" % idx)
                    send(conn, ":srv 903 gate :SASL authentication successful")
                else:
                    log("c%d sasl payload bad" % idx)
                    send(conn, ":srv 904 gate :SASL authentication failed")
        elif cmd == "CAP" and len(parts) > 1 and parts[1].upper() == "END":
            break
    if nick and user and authz:
        send(conn, ":srv 001 %s :Welcome to the fixture net" % nick)
        pump(conn, idx, nick, buf,
             lambda ch: send(conn, ":alice!u@h PRIVMSG %s :sasl-hello" % ch),
             hold=30)

# Conn budgets count EVERY iteration's legs — vgate_repeat 3 replays the
# spec, so rejoin alternates first/drop legs on idx parity and journey
# serves 3 registered conns per iteration (TLS session, consent-armed
# plaintext, the reopen's re-dial) plus slack for stray conns the relay
# layer opens but never registers.
conns = {"rejoin": 6, "nickuse": 3, "mention": 3, "once": 3,
         "logs": 3, "journey": 24, "sasl": 6}[mode]

if mode == "journey":
    import threading
    # M147c run 07 holds THREE live conns at once — the TLS session
    # stays open for the snapshot while the consent session and the
    # reopen's re-dial converse — so the per-conn register+pump is a
    # daemon thread, and the accept loop never stalls behind one leg.
    # Roles come from the REGISTERED order, not the accept index: a
    # conn that dies before NICK/USER (a relay-level probe) consumes
    # nothing.
    jlock = threading.Lock()
    jstate = {"plain": 0, "tls": None, "tls_nick": ""}

    def serve_journey(conn, idx):
        try:
            # Peek the first byte: 0x16 = TLS record header
            # (ClientHello) — wrap it; anything else is plaintext IRC.
            conn.settimeout(60)
            try:
                first = conn.recv(1, socket.MSG_PEEK)
            except socket.timeout:
                first = b""
            wrapped = False
            if first == b"\x16":
                try:
                    conn = tls_ctx.wrap_socket(conn, server_side=True)
                    wrapped = True
                    log("c%d tls ok" % idx)
                except Exception as e:
                    log("c%d tls %s" % (idx, e))
                    try:
                        conn.close()
                    except Exception:
                        pass
                    return
            log("c%d open" % idx)
            nick, buf = register(conn, idx)
            if not nick:
                conn.close()
                return
            with jlock:
                if wrapped:
                    role = "tls"
                    jstate["tls"] = conn
                    jstate["tls_nick"] = nick
                else:
                    jstate["plain"] += 1
                    role = "consent" if jstate["plain"] % 2 == 1 else "reopen"
            log("c%d role %s nick %s" % (idx, role, nick))
            if role == "tls":
                def on_jn(ch, conn=conn):
                    if ch == "#other":
                        # The scrollback flood — the blob's #other scroll
                        # offset is real because these lines exist.
                        for i in range(60):
                            send(conn, ":alice!u@h PRIVMSG #other :filler-%02d backlog" % i)
                    else:
                        send(conn, ":alice!u@h PRIVMSG %s :hello on %s" % (ch, ch))
            elif role == "consent":
                def on_jn(ch, conn=conn):
                    send(conn, ":alice!u@h PRIVMSG %s :welcome to the plain side" % ch)
            else:
                def on_jn(ch, conn=conn):
                    send(conn, ":alice!u@h PRIVMSG %s :restored-tail" % ch)
            # The mention ping rides the consent session's first client
            # PRIVMSG (the typed 'ok'): the toast can only paint once the
            # dialog is answered AND the typed line is on the wire, so
            # the click-through ordering is a fact, not a race. The
            # greetings carry no nick on purpose — the ONLY toast the
            # run paints is this ping's, so --pointer-virtio-after
            # 'notify paint' clicks a live one inside its TTL. The
            # once-flag is PER CONN, not global: the fixture outlives a
            # vgate_repeat iteration, and each iteration's consent leg
            # must ping its own iteration's TLS conn (jstate["tls"] is
            # always the newest wrap) or runs 07-02/07-03 never toast.
            pinged = [False]
            def ping_once():
                with jlock:
                    t, tn = jstate["tls"], jstate["tls_nick"]
                    if t is None or pinged[0]:
                        return
                    pinged[0] = True
                try:
                    send(t, ":alice!u@h PRIVMSG #other :ping %s you there?" % tn)
                except Exception as e:
                    log("c%d ping %s" % (idx, e))
            pump(conn, idx, nick, buf, on_jn, hold=90, echo=True,
                 on_privmsg=ping_once if role == "consent" else None)
        except Exception as e:
            log("c%d serve %s" % (idx, e))
        finally:
            try:
                conn.close()
            except Exception:
                pass
            log("c%d close" % idx)

for idx in range(conns):
    try:
        conn, _ = srv.accept()
    except socket.timeout:
        log("accept timeout at conn %d" % idx)
        break
    if mode == "journey":
        threading.Thread(target=serve_journey, args=(conn, idx),
                         daemon=True).start()
        continue
    conn.settimeout(60)
    log("c%d open" % idx)
    if mode == "rejoin":
        nick, buf = register(conn, idx)
        if not nick:
            conn.close()
            continue
        if idx % 2 == 0:
            # first-hello, then a hard close: the drop the client must
            # back off and reconnect from.
            pump(conn, idx, nick, buf,
                 lambda ch: send(conn, ":alice!u@h PRIVMSG %s :first-hello" % ch),
                 hold=15, first_only=True)
        else:
            pump(conn, idx, nick, buf,
                 lambda ch: send(conn, ":alice!u@h PRIVMSG %s :REJOIN-MARKER" % ch),
                 hold=30)
        conn.close()
        log("c%d close" % idx)
    elif mode == "nickuse":
        nick, buf = register(conn, idx, taken=("gate", "gate_"))
        if nick:
            pump(conn, idx, nick, buf, lambda ch: None, hold=30)
        conn.close()
        log("c%d close" % idx)
    elif mode == "mention":
        # M147b run 04: on the #other JOIN confirm, alice pings the
        # client's nick — the toast the gate then clicks through.
        nick, buf = register(conn, idx)
        if not nick:
            conn.close()
            continue
        def on_join(ch, conn=conn, nick=nick):
            if ch == "#other":
                send(conn, ":alice!u@h PRIVMSG #other :ping %s you there?" % nick)
            else:
                send(conn, ":alice!u@h PRIVMSG %s :plain hello on %s" % (ch, ch))
        pump(conn, idx, nick, buf, on_join, hold=45)
        conn.close()
        log("c%d close" % idx)
    elif mode == "once":
        # M147b run 06: a plain session that just holds — the leg's
        # interest is the consent dialog, not the traffic.
        nick, buf = register(conn, idx)
        if nick:
            pump(conn, idx, nick, buf, lambda ch: None, hold=45)
        conn.close()
        log("c%d close" % idx)
    elif mode == "logs":
        # M152e (#2919) run 09: the client joins, types '/hl add
        # virelai' + '/log on' + a channel 'hi'. That first client
        # PRIVMSG is the go signal — nothing the slash verbs do reaches
        # the wire — so the fixture answers it with the armed word's
        # line (no nick in it: the toast can only be the rule's) and a
        # 40-line flood; 42 logged lines cross the 32-line flush, and
        # the python assert reads the tail on the share.
        nick, buf = register(conn, idx)
        if not nick:
            conn.close()
            continue
        fired = [False]
        def on_pv():
            if fired[0]:
                return
            fired[0] = True
            send(conn, ":alice!u@h PRIVMSG #goirc :release virelai today")
            for i in range(40):
                send(conn, ":alice!u@h PRIVMSG #goirc :flood-%02d line" % (i + 1))
        pump(conn, idx, nick, buf, lambda ch: None, hold=60,
             on_privmsg=on_pv)
        conn.close()
        log("c%d close" % idx)
    elif mode == "sasl":
        try:
            sasl_session(conn, idx)
        except Exception as e:
            log("c%d sasl %s" % (idx, e))
        conn.close()
        log("c%d close" % idx)

srv.close()
log("fixture done")
''')

# Run 07's TLS fixture serves the pinned leaf.example.com chain —
# identical to go-fetch-https's responder material; nothing regenerates.
cfx = os.path.join("user", "src", "lib", "tls", "vectors", "fx")
chain = os.path.join(cfx, "chain-ec.pem")
irckey = os.path.join(cfx, "leaf-ec.key")
if not os.path.exists(chain) or not os.path.exists(irckey):
    sys.exit("pinned fixture chain missing under %s" % cfx)

procs = []
for mode, port in (("rejoin", 26667), ("nickuse", 26668),
                   ("mention", 26669), ("once", 26670),
                   ("journey", 26671), ("logs", 26672), ("sasl", 26673),
                   ("once", 26674), ("once", 26675)):
    subprocess.run([sys.executable,
                    os.path.join("tools", "lib", "port_guard.py"),
                    str(port)],
                   check=True)
    logpath = os.path.join(run, "ircd-%d.log" % port)
    log = open(logpath, "w")
    log.close()
    argv = [sys.executable, fixture, mode, str(port), logpath]
    if mode == "journey":
        argv += [chain, irckey]
    proc = subprocess.Popen(argv,
                            stdout=subprocess.DEVNULL, stderr=subprocess.STDOUT,
                            start_new_session=True)
    procs.append(proc)
    print("go-irc: fixture %s pid=%d on 127.0.0.1:%d" % (mode, proc.pid, port))

time.sleep(0.4)
for proc in procs:
    if proc.poll() is not None:
        sys.exit("go-irc: fixture exited early rc=%s" % proc.returncode)
PY

# Run 01 -- the card leg: register, join, receive, hard drop, reconnect,
# rejoin. The fixture closes conn 1 after first-hello; the client backs
# off (--backoff-ms 300), re-registers and re-sends JOIN -- 'goirc: rejoin
# #goirc' -- and the fixture's conn-2 answer carries REJOIN-MARKER.
vgate_run 01 -- \
    --screen '$RUN_DIR/screen' \
    --via-virtio \
    --net '$RUN_DIR/cap01.bin' --net-arp-respond 10.0.0.2 \
    --net-tcp-respond 10.0.0.2:6667:relay --net-tcp-respond-relay 127.0.0.1:26667 \
    --script '$RUN_DIR/script-01.txt' --script-expect 'goirc: msg #goirc alice REJOIN-MARKER' --timeout 300

vgate_assert 01 serial-contains 'goirc: dial 10.0.0.2 6667 plain'
vgate_assert 01 serial-count 'goirc: registered nick=gate' 2
vgate_assert 01 serial-count 'goirc: join #goirc' 2
vgate_assert 01 serial-contains 'goirc: msg #goirc alice first-hello'
vgate_assert 01 serial-contains 'goirc: drop '
vgate_assert 01 serial-contains 'goirc: backoff 300'
vgate_assert 01 serial-contains 'goirc: rejoin #goirc'
vgate_assert 01 serial-contains 'goirc: msg #goirc alice REJOIN-MARKER'
vgate_assert 01 serial-absent 'goirc: error'
vgate_assert 01 serial-absent 'goirc: refuse plaintext'
vgate_assert 01 serial-absent '[EXC] parking:'
vgate_assert 01 python <<'PY'
import os, sys
# The fixture log accumulates across vgate_repeat iterations — the
# cumulative count is what proves THIS iteration's conns ran. (REPEAT=1
# drops the -NN tag suffix; [-1] reads 1 either way.)
it = int(os.environ["VG_TAG"].rsplit("-", 1)[-1])
log = open(os.path.join(os.environ["RUN_DIR"], "ircd-26667.log")).read()
need = ["c0 open", "c0 rx NICK gate", "c0 rx USER", "c0 rx JOIN #goirc",
        "c1 open", "c1 rx NICK gate", "c1 rx USER", "c1 rx JOIN #goirc",
        "c0 close"]
missing = [n for n in need if n not in log]
if missing:
    sys.exit("fixture transcript missing: %r\n%s" % (missing, log))
joins = sum(1 for l in log.splitlines() if "rx JOIN #goirc" in l)
if joins < 2 * it:
    sys.exit("iteration %d: only %d JOINs on the transcript\n%s" % (it, joins, log))
PY

# Run 02 -- plaintext without the consent bit refuses before any dial:
# the named refusal marker, never a socket. (The TLS dial path itself is
# class-A covered; this leg is the consent rule's gate.)
vgate_run 02 -- \
    --screen '$RUN_DIR/screen' \
    --via-virtio \
    --net '$RUN_DIR/cap02.bin' --net-arp-respond 10.0.0.2 \
    --net-tcp-respond 10.0.0.2:6667:relay --net-tcp-respond-relay 127.0.0.1:26667 \
    --script '$RUN_DIR/script-02.txt' --script-expect 'goirc: refuse plaintext' --timeout 180

vgate_assert 02 serial-contains 'goirc: refuse plaintext'
vgate_assert 02 serial-contains 'goirc: closed'
vgate_assert 02 serial-absent 'goirc: registered'
vgate_assert 02 serial-absent 'goirc: join #goirc'
vgate_assert 02 serial-absent '[EXC] parking:'

# Run 03 -- 433 nick-in-use: the fixture refuses 'gate' and 'gate_',
# accepts 'gate__'. The registered marker carries the retry nick.
vgate_run 03 -- \
    --screen '$RUN_DIR/screen' \
    --via-virtio \
    --net '$RUN_DIR/cap03.bin' --net-arp-respond 10.0.0.2 \
    --net-tcp-respond 10.0.0.2:6668:relay --net-tcp-respond-relay 127.0.0.1:26668 \
    --script '$RUN_DIR/script-03.txt' --script-expect 'goirc: registered nick=gate__' --timeout 240

vgate_assert 03 serial-contains 'goirc: registered nick=gate__'
vgate_assert 03 serial-contains 'nick in use'
vgate_assert 03 serial-absent 'goirc: error'
vgate_assert 03 serial-absent '[EXC] parking:'
vgate_assert 03 python <<'PY'
import os, sys
log = open(os.path.join(os.environ["RUN_DIR"], "ircd-26668.log")).read()
need = ["c0 rx NICK gate\n", "c0 rx NICK gate_", "c0 rx NICK gate__",
        "c0 rx USER", "c0 open"]
missing = [n for n in need if n not in log]
sys.exit("fixture transcript missing: %r\n%s" % (missing, log) if missing else 0)
PY

# --- M147b (#2802) run 04: mention -> toast -> click-through ---------------
#
# GOIRC --ui runs under a LIVE seat (GOTABWM.DEMO removed): the tabapp
# declare makes it a rail tab, and vi.Notify is a real kind-12 frame the
# seat queues + paints. The chain, each link its own marker:
#
#   goirc: join #other               own JOIN confirmed (rail seeded it
#                                    earlier; the confirm re-adds noop)
#   goirc: msg #other alice ping ... the mention entry
#   goirc: toast #other              the app's ack -- the seat answered,
#                                    so the toast is not a void request
#   gotabwm: notify paint id=<n>     toast ON THE SCANOUT (pointer anchor)
#   goirc: blur                      the seat's alt-tab moved focus to
#                                    GOTABAPP (the rail cell script2 put
#                                    first)
#   gotabwm: notify dismiss id=<n>   the toast click dismissed it AND
#                                    refocused the sender (focusHosted)
#   goirc: focus #other              the pending mention lands the user
#                                    on the buffer that called
#
# The pointer batch is ONE batch anchored on 'notify paint' (toast provably
# on screen): press+release at the toast's centre (112,702) = middle of
# notifyRect(1280,720,0) = (8,692,208,20). The BLUR rides a different
# transport deliberately: pointer messages pace at 2.5 s each, so a
# blur-click + toast-click in one batch lands the toast press ~9 s after
# the push -- past NotifyTicks=8 (observed on the first attempt: the
# dismiss marker was expiry, and the press found an empty strip). A seat
# 'alt-tab' chord strokes at 0.25 s, so it blurs GOIRC to GOTABAPP the
# moment the toast paints, and the solo pointer press refocuses it while
# the toast is still alive.
#
# The blur is load-bearing either way: kernel focus() pushes WIN_FOCUS
# only when focused_id changes, so a toast click on an already-focused
# sender would never reach GOIRC's event loop for the pending-mention
# landing.
vgate_file script-04.txt <<'EOF'
net ip 10.0.0.1
net arp 10.0.0.2
vf rm GOTABWM.DEMO
vf rm SESSION.TABS
vf rm NOTIFY.HIST
set GOMAXPROCS=1
wm
exec GOTABWM.ELF
EOF

vgate_file script2-04.txt <<'EOF'
dui focus 0
exec GOTABAPP.ELF &
fg
exec GOIRC.ELF --ui --host=10.0.0.2 --port=6669 --nick=gate --join '#goirc,#other' --plain
EOF

vgate_run 04 -- \
    --screen '$RUN_DIR/screen-04' \
    --via-virtio \
    --net '$RUN_DIR/cap04.bin' --net-arp-respond 10.0.0.2 \
    --net-tcp-respond 10.0.0.2:6669:relay --net-tcp-respond-relay 127.0.0.1:26669 \
    --script '$RUN_DIR/script-04.txt' \
    --script2 '$RUN_DIR/script2-04.txt' --script2-after 'gotabwm: win focus' \
    --input-chords 'alt-tab' --input-chords-after 'gotabwm: notify paint id=' \
    --pointer-virtio '112,702,d;112,702,u' \
    --pointer-virtio-after 'gotabwm: notify paint id=' \
    --script-expect 'goirc: focus #other' --timeout 300

vgate_assert 04 serial-contains 'exec: loaded GOTABWM.ELF'
vgate_assert 04 serial-contains 'gotabwm: mode live'
vgate_assert 04 serial-absent 'gotabwm: mode demo'
vgate_assert 04 serial-contains 'exec: loaded GOTABAPP.ELF'
vgate_assert 04 serial-contains 'exec: loaded GOIRC.ELF'
vgate_assert 04 serial-contains 'goirc: open id='
vgate_assert 04 serial-contains 'goirc: declare accepted'
vgate_assert 04 serial-contains 'goirc: dial 10.0.0.2 6669 plain'
vgate_assert 04 serial-contains 'goirc: registered nick=gate'
vgate_assert 04 serial-contains 'goirc: join #goirc'
vgate_assert 04 serial-contains 'goirc: join #other'
vgate_assert 04 serial-contains 'goirc: msg #other alice ping gate you there?'
vgate_assert 04 serial-contains 'goirc: toast #other'
vgate_assert 04 serial-absent 'goirc: toast-refused'
vgate_assert 04 serial-contains 'gotabwm: notify id='
vgate_assert 04 serial-contains 'gotabwm: notify paint id='
vgate_assert 04 serial-contains 'goirc: blur'
vgate_assert 04 serial-contains 'gotabwm: notify dismiss id='
vgate_assert 04 serial-contains 'gotabwm: tab focus id='
vgate_assert 04 serial-contains 'goirc: focus #other'
vgate_assert 04 serial-absent 'goirc: error'
vgate_assert 04 serial-absent '[EXC] parking:'
# Ordering: the toast must be painted before the click dismisses it, and
# the blur delivered before the refocus can land the mention. The
# chord and the pointer batch are BOTH armed on 'notify paint', so which
# input reaches the kernel FIFO first is a race (#3708): the seat-side
# truth is unchanged (the click's refocus can only deliver WIN_FOCUS
# after the chord's blur took focus away), but the app's 'goirc: blur'
# prints from its own event loop and can interleave after the seat's
# dismiss/tab-focus markers on the shared serial line. The assert
# therefore pins two sub-chains that ARE causal: paint -> dismiss ->
# tab focus -> focus #other (the click-through itself), and blur ->
# focus #other (same-producer ordering inside the app's event queue --
# the landing still proves a real blur->refocus transition happened).
# 'tab focus id=' also prints at tab-setup, so each link is searched
# AFTER the previous one, not globally.
vgate_assert 04 python <<'PY'
import os, sys
ser = open(os.environ["VG_SER"], errors="replace").read()

def after(markers):
    pos = 0
    for m in markers:
        i = ser.find(m, pos)
        if i < 0:
            sys.exit("chain marker missing after %d: %r" % (pos, m))
        pos = i + len(m)

after(["gotabwm: notify paint id=", "gotabwm: notify dismiss id=",
       "gotabwm: tab focus id=", "goirc: focus #other"])
after(["goirc: blur", "goirc: focus #other"])
log = open(os.path.join(os.environ["RUN_DIR"], "ircd-26669.log")).read()
need = ["c0 open", "c0 rx NICK gate", "c0 rx USER", "c0 rx JOIN #goirc",
        "c0 rx JOIN #other", "tx :alice!u@h PRIVMSG #other :ping gate"]
missing = [n for n in need if n not in log]
sys.exit("fixture transcript missing: %r\n%s" % (missing, log) if missing else 0)
PY

# --- M147b (#2802) run 05: plaintext consent refuses by name ----------------
#
# --ui without --plain and with no stored consent raises appkit's Confirm
# BEFORE any dial; No is the default focus, so a bare 'return' chooses it:
# 'goirc: consent no' + the same named refusal the console driver prints.
# Nothing reaches the wire -- no dial marker, no registered, no fixture
# transcript at all.
vgate_file script-05.txt <<'EOF'
vf rm GOTABWM.DEMO
vf rm SESSION.TABS
vf rm NOTIFY.HIST
set GOMAXPROCS=1
wm
exec GOTABWM.ELF
EOF

vgate_file script2-05.txt <<'EOF'
dui focus 0
exec GOIRC.ELF --ui --host=10.0.0.2 --port=6670 --nick=gate
EOF

vgate_run 05 -- \
    --screen '$RUN_DIR/screen-05' \
    --via-virtio \
    --script '$RUN_DIR/script-05.txt' \
    --script2 '$RUN_DIR/script2-05.txt' --script2-after 'gotabwm: win focus' \
    --input-chords 'return' --input-chords-after 'goirc: consent ask' \
    --script-expect 'goirc: refuse plaintext' --timeout 240

vgate_assert 05 serial-contains 'goirc: consent ask'
vgate_assert 05 serial-contains 'goirc: consent no'
vgate_assert 05 serial-contains 'goirc: refuse plaintext'
vgate_assert 05 serial-absent 'goirc: consent yes'
vgate_assert 05 serial-absent 'goirc: dial'
vgate_assert 05 serial-absent 'goirc: registered'
vgate_assert 05 serial-absent '[EXC] parking:'

# --- M147b (#2802) run 06: consent yes is stored + dials ---------------------
#
# 'tab' moves the dialog focus from No (default) to Yes; 'return' chooses
# it: 'goirc: consent yes' -> SaveStateNow posts the blob ('goirc: state
# saved' prints on the seat's kind-20 ack, so the answer is PROVABLY
# stored) -> the session dials, registers and joins.
vgate_file script-06.txt <<'EOF'
net ip 10.0.0.1
net arp 10.0.0.2
vf rm GOTABWM.DEMO
vf rm SESSION.TABS
vf rm NOTIFY.HIST
set GOMAXPROCS=1
wm
exec GOTABWM.ELF
EOF

vgate_file script2-06.txt <<'EOF'
dui focus 0
exec GOIRC.ELF --ui --host=10.0.0.2 --port=6670 --nick=gate --join '#goirc'
EOF

vgate_run 06 -- \
    --screen '$RUN_DIR/screen-06' \
    --via-virtio \
    --net '$RUN_DIR/cap06.bin' --net-arp-respond 10.0.0.2 \
    --net-tcp-respond 10.0.0.2:6670:relay --net-tcp-respond-relay 127.0.0.1:26670 \
    --script '$RUN_DIR/script-06.txt' \
    --script2 '$RUN_DIR/script2-06.txt' --script2-after 'gotabwm: win focus' \
    --input-chords 'tab,return' --input-chords-after 'goirc: consent ask' \
    --script-expect 'goirc: join #goirc' --timeout 300

vgate_assert 06 serial-contains 'goirc: consent ask'
vgate_assert 06 serial-contains 'goirc: consent yes'
vgate_assert 06 serial-contains 'goirc: state saved'
vgate_assert 06 serial-contains 'goirc: dial 10.0.0.2 6670 plain'
vgate_assert 06 serial-contains 'goirc: registered nick=gate'
vgate_assert 06 serial-contains 'goirc: join #goirc'
vgate_assert 06 serial-absent 'goirc: refuse plaintext'
vgate_assert 06 serial-absent '[EXC] parking:'
vgate_assert 06 python <<'PY'
import os, sys
log = open(os.path.join(os.environ["RUN_DIR"], "ircd-26670.log")).read()
need = ["c0 open", "c0 rx NICK gate", "c0 rx USER", "c0 rx JOIN #goirc"]
missing = [n for n in need if n not in log]
sys.exit("fixture transcript missing: %r\n%s" % (missing, log) if missing else 0)
PY

# --- M147c (#2803) runs 07+08: the K=3 acceptance conversation ---------
#
# The card's journey splits across TWO boots — the seat's snapshot arm
# is focus-quiet gated (ctrl-shift-s parks until input AND app settle,
# ~4 ticks) so a snapshot and any later input choreography can never be
# sequenced inside one --input-chords batch; a fresh boot's session
# restore is also the stronger reading of the card's "a snapshot and a
# restore". vgate_repeat 3 replays both runs; each iteration is
# self-cleaning (run 07's script re-rms the session files and the
# journey fixture serves BOTH runs' conns on its shared port).
#
# Run 07 — the conversation boot:
#   TLS first           script2 execs GOIRC --ui --tls --sni
#                       leaf.example.com with the positional shorthand
#                       -- 'goirc: dial 10.0.0.2 6667 tls' lands before
#                       any consent ask.
#   plaintext after     script3 (gated on 'goirc: join #other') execs a
#   consent             second GOIRC --ui on the SAME responder port
#                       with no --plain: 'consent ask' -> chords
#                       'tab,return' -> 'consent yes' -> 'state saved'
#                       (the seat's kind-20 ack) -> 'dial 6667 plain'.
#                       (One VM arms ONE --net-tcp-respond, so the
#                       fixture's first-byte peek splits TLS from
#                       plaintext on the shared port.)
#   conversation        'o,k,return' submits 'ok' on the plain
#                       session's #goirc (still focused pre-click);
#                       the fixture echoes 'thanks' (no nick -- a
#                       nick-bearing echo would paint a second toast
#                       and the 'notify paint' gate could land on it) --
#                       both directions on the wire.
#   mention toast       the consent conn's first client PRIVMSG (the
#                       'ok') is what makes the fixture ping the nick
#                       on the TLS session's #other — the toast can
#                       only paint once the dialog is answered AND the
#                       typed line is on the wire, so the click's
#                       ordering is a fact, not a race. Greetings carry
#                       no nick: 'notify paint' is this ping's alone.
#   blur +              the SECOND declare stole focus earlier
#   click-through       ('goirc: blur' — a real window steal, not a
#                       seat chord); the pointer batch, gated on
#                       'notify paint', presses the toast centre
#                       (112,702) inside its TTL: 'notify dismiss' ->
#                       refocus -> the pending mention lands
#                       'goirc: focus #other'.
#   snapshot            the post-click pageups scroll the flooded
#                       #other (real scroll state), then ctrl-shift-s
#                       parks: at quiet 'state ask id=' -> 'state
#                       save id=' -> 'snapshot saved' — SESSION.TABS +
#                       the app_state trailer land inside
#                       SNAPSHOT.BUNDLE (the python decode below).
#                       The TLS row's blob also carries the --sni
#                       verify name (ircState v2): the re-exec has no
#                       argv, so without it the restored dial would
#                       validate the pinned chain against the raw IP
#                       and fail hostname_mismatch.
#
# Run 08 — the restore boot:
#   session restore     SESSION.TABS (written by run 07's snapshot —
#                       the script does NOT rm it) makes the seat
#                       re-exec both rows bare: 'session load n=2
#                       mode=restore' -> 2x 'state restored' -> each
#                       blob supplies host/port/sni/nick/channels/
#                       consent argv never carried. The TLS row re-dials
#                       tls,
#                       the consent row dials plain WITHOUT a second
#                       ask ('consent ask' is ABSENT this run) ->
#                       rejoin #goirc -> the fixture's second
#                       registered plaintext conn answers
#                       'restored-tail' (terminal).
#   scrollback/channels live: 'focus #goirc' post-restore + the
#    intact             fixture transcript roles. Byte-level: run 07's
#                       bundle trailer decodes to two ircState blobs —
#                       TLS carries {#goirc,#other}, focus #other and a
#                       nonzero scroll; plain carries the consent bit.
#
# The restore path is what the GOIRC declare-title change is for: the
# seat re-execs the bin with no argv, so the app declares 'GOIRC.ELF'
# (guessBin's default arm), repaints its rail label via SetTabTitle,
# and a zero-arg launch falls into runUI where the staged blob supplies
# every field argv would have carried.
vgate_file script-07.txt <<'EOF'
net ip 10.0.0.1
net arp 10.0.0.2
vf rm GOTABWM.DEMO
vf rm SESSION.TABS
vf rm NOTIFY.HIST
vf rm SNAPSHOT.BUNDLE
set GOMAXPROCS=1
wm
exec GOTABWM.ELF
EOF

# The monitor's exec verb packs at most 8 argv slots: the TLS leg rides
# the positional shorthand (<host> <nick> #chan ...) + the default 6667,
# so the only flags it needs are --ui/--tls/--sni.
vgate_file script2-07.txt <<'EOF'
dui focus 0
exec GOIRC.ELF --ui --tls --sni=leaf.example.com 10.0.0.2 gate '#goirc' '#other'
EOF

vgate_file script3-07.txt <<'EOF'
exec GOIRC.ELF --ui --host=10.0.0.2 --nick=gate --join '#goirc'
EOF

vgate_run 07 -- \
    --screen '$RUN_DIR/screen-07' \
    --via-virtio \
    --net '$RUN_DIR/cap07.bin' --net-arp-respond 10.0.0.2 \
    --net-tcp-respond 10.0.0.2:6667:relay --net-tcp-respond-relay 127.0.0.1:26671 \
    --script '$RUN_DIR/script-07.txt' \
    --script2 '$RUN_DIR/script2-07.txt' --script2-after 'gotabwm: win focus' \
    --script3 '$RUN_DIR/script3-07.txt' --script3-after 'goirc: join #other' \
    --input-chords 'tab,return,pageup,pageup,pageup,pageup,o,k,return,pageup,pageup,pageup,pageup,pageup,pageup,pageup,pageup,pageup,pageup,pageup,pageup,ctrl-shift-s' \
    --input-chords-after 'goirc: consent ask' \
    --pointer-virtio '112,702,d;112,702,u' \
    --pointer-virtio-after 'gotabwm: notify paint id=' \
    --script-expect 'gotabwm: snapshot saved bytes=' --timeout 300

vgate_assert 07 serial-contains 'gotabwm: mode live'
vgate_assert 07 serial-contains 'exec: loaded GOTABWM.ELF'
vgate_assert 07 serial-count 'goirc: open id=' 2
vgate_assert 07 serial-count 'goirc: declare accepted' 2
vgate_assert 07 serial-contains 'goirc: dial 10.0.0.2 6667 tls'
vgate_assert 07 serial-exact 'goirc: consent ask' 1
vgate_assert 07 serial-contains 'goirc: consent yes'
vgate_assert 07 serial-contains 'goirc: state saved'
vgate_assert 07 serial-contains 'goirc: dial 10.0.0.2 6667 plain'
vgate_assert 07 serial-count 'goirc: registered nick=gate' 2
vgate_assert 07 serial-count 'goirc: join #goirc' 2
vgate_assert 07 serial-contains 'goirc: join #other'
vgate_assert 07 serial-contains 'goirc: msg #other alice filler-00 backlog'
vgate_assert 07 serial-contains 'goirc: msg #other alice ping gate you there?'
vgate_assert 07 serial-contains 'goirc: toast #other'
vgate_assert 07 serial-absent 'goirc: toast-refused'
vgate_assert 07 serial-contains 'gotabwm: notify paint id='
vgate_assert 07 serial-count 'goirc: blur' 2
vgate_assert 07 serial-contains 'gotabwm: notify dismiss id='
vgate_assert 07 serial-contains 'gotabwm: tab focus id='
vgate_assert 07 serial-contains 'goirc: focus #other'
vgate_assert 07 serial-contains 'goirc: line ok'
vgate_assert 07 serial-contains 'alice thanks'
vgate_assert 07 serial-contains 'gotabwm: state ask id='
vgate_assert 07 serial-contains 'gotabwm: state save id='
vgate_assert 07 serial-contains 'gotabwm: snapshot saved bytes='
vgate_assert 07 serial-absent 'goirc: consent no'
vgate_assert 07 serial-absent 'goirc: refuse plaintext'
vgate_assert 07 serial-absent 'goirc: state refused'
vgate_assert 07 serial-absent 'goirc: error'
vgate_assert 07 serial-absent '[EXC] parking:'
# Order: TLS must dial BEFORE the consent ask, the plain dial only after
# consent yes; the typed 'ok' precedes the toast (the fixture sends the
# ping in answer to it); the toast chain runs paint -> dismiss ->
# refocus -> land; the snapshot arm fires only after input goes quiet.
vgate_assert 07 python <<'PY'
import os, sys
ser = open(os.environ["VG_SER"], errors="replace").read()

def after(markers):
    pos = 0
    for m in markers:
        i = ser.find(m, pos)
        if i < 0:
            sys.exit("marker %r missing after pos %d" % (m, pos))
        pos = i + len(m)

after(["goirc: dial 10.0.0.2 6667 tls", "goirc: consent ask",
       "goirc: consent yes", "goirc: state saved",
       "goirc: dial 10.0.0.2 6667 plain"])
after(["goirc: line ok", "goirc: msg #goirc alice thanks"])
after(["gotabwm: notify paint id=", "gotabwm: notify dismiss id=",
       "gotabwm: tab focus id=", "goirc: focus #other"])
after(["gotabwm: state ask id=", "gotabwm: snapshot saved bytes="])

# Fixture transcript: one port, three roles across runs 07+08 — the
# TLS conn (peek-wrapped) saw the register+joins, the consent conn took
# the typed 'ok', and the ping crossed onto the TLS conn.
tag = os.environ["VG_TAG"]
it = int(tag.rsplit("-", 1)[-1]) if "-" in tag else 1
log = open(os.path.join(os.environ["RUN_DIR"], "ircd-26671.log")).read()
need = ["tls ok", "role tls", "role consent", "rx JOIN #other",
        "tx :alice!u@h PRIVMSG #other :ping gate",
        "rx PRIVMSG #goirc :ok",
        "tx :alice!u@h PRIVMSG #goirc :thanks"]
missing = [n for n in need if n not in log]
opens = sum(1 for l in log.splitlines() if l.rstrip().endswith(" open"))
if opens < 2 * it:
    missing.append("opens %d < 2*iteration %d" % (opens, it))
if missing:
    sys.exit("journey fixture missing: %r\n%s" % (missing, log))
PY

# The snapshot's session trailer IS the scrollback/channels evidence:
# decode SNAPSHOT.BUNDLE -> 'session' entry -> .tabs v2 + the app_state
# trailer -> two ircState v1 blobs. The TLS blob carries {#goirc,#other},
# focus #other and a real scroll offset; the plain blob carries the
# consent bit + #goirc. This is the byte-level half of 'intact'.
vgate_assert 07 python <<'PY'
import os, struct, sys
share = os.environ["VG_SHARE"]
raw = open(os.path.join(share, "SNAPSHOT.BUNDLE"), "rb").read()
line, raw = raw.split(b"\n", 1)
if not line.startswith(b"#vb1 "):
    sys.exit("bundle header %r" % line)
want = int(line.split()[1])
entries = {}
for _ in range(want):
    hdr, raw = raw.split(b"\n", 1)
    name, n = hdr.split()
    entries[name.decode()] = raw[: int(n)]
    raw = raw[int(n):]
sess = entries["session"]
if sess[0] != 2:
    sys.exit("session not .tabs v2")
n = sess[2]
if n != 2:
    sys.exit("expected 2 tab records, got %d" % n)
t = sess[6 + 69 * n :]
if t[:2] != b"AS" or t[2] != 1:
    sys.exit("bad app_state trailer magic %r" % t[:4])
blobs = []
off = 6
for _ in range(t[3]):
    idx, appver, ln = t[off], t[off+1], struct.unpack("<I", t[off+4:off+8])[0]
    blobs.append((idx, appver, t[off+8 : off+8+ln]))
    off += 8 + ln
if len(blobs) != 2:
    sys.exit("expected 2 app_state blobs, got %d" % len(blobs))

def ircstate(b):
    # v2 is the M147b layout; v3 (M152e #2919) appends the /hl rule set —
    # a u16 count of u16-prefixed words after the chan records; v4
    # (M212c #3874) appends the ws request path as one u16-prefixed str.
    if b[0] not in (2, 3, 4):
        sys.exit("ircState ver %d" % b[0])
    o = 2
    def u16():
        nonlocal o
        v = struct.unpack("<H", b[o:o+2])[0]
        o += 2
        return v
    def s():
        nonlocal o
        n = u16()
        v = b[o:o+n].decode()
        o += n
        return v
    host = s(); port = u16(); sni = s(); nick = s(); focus = s()
    chans = {}
    for _ in range(u16()):
        chans[s()] = struct.unpack("<I", b[o:o+4])[0]; o += 4
    rules = []
    if b[0] >= 3:
        for _ in range(u16()):
            rules.append(s())
    wspath = ""
    if b[0] >= 4:
        wspath = s()
    if o != len(b):
        sys.exit("trailing blob bytes %d" % (len(b) - o))
    return dict(flags=b[1], host=host, port=port, sni=sni, nick=nick,
                focus=focus, chans=chans, rules=rules, wspath=wspath)

states = [ircstate(b) for _, _, b in blobs]
# One port carries both transports — discriminate by the flags word
# (bit0 tls / bit1 consent), not the port.
tls = [s for s in states if s["flags"] & 1]
plain = [s for s in states if not s["flags"] & 1]
if len(tls) != 1 or len(plain) != 1:
    sys.exit("blob flags: %r" % [s["flags"] for s in states])
tls, plain = tls[0], plain[0]
if tls["port"] != 6667 or plain["port"] != 6667:
    sys.exit("blob ports: %r %r" % (tls["port"], plain["port"]))
if not tls["flags"] & 1:
    sys.exit("tls blob missing the tls bit: %r" % tls)
if tls["sni"] != "leaf.example.com":
    sys.exit("tls blob sni %r" % tls["sni"])
if tls["chans"].keys() != {"", "#goirc", "#other"}:
    sys.exit("tls blob chans %r" % tls["chans"])
if tls["focus"] != "#other" or tls["chans"]["#other"] == 0:
    sys.exit("tls blob focus/scroll %r %r" % (tls["focus"], tls["chans"]))
if not plain["flags"] & 2:
    sys.exit("plain blob missing the consent bit: %r" % plain)
if plain["chans"].keys() != {"", "#goirc"} or plain["focus"] != "#goirc":
    sys.exit("plain blob %r" % plain)
print("run07 bundle: tls chans=%r scroll=%d focus=%r; plain consent+chans=%r"
      % (sorted(tls["chans"]), tls["chans"]["#other"], tls["focus"],
         sorted(plain["chans"])))
PY

# --- run 08: the restore boot ------------------------------------------
#
# No session-file rm's here: SESSION.TABS from run 07's snapshot IS the
# leg. The seat restores the strip and re-execs both rows bare; each
# app's staged blob supplies host/port/transport/nick/channels/consent
# that argv never carried, so the consent row dials plain WITHOUT
# re-asking — 'consent ask' must stay absent the whole run.
vgate_file script-08.txt <<'EOF'
net ip 10.0.0.1
net arp 10.0.0.2
set GOMAXPROCS=1
wm
exec GOTABWM.ELF
EOF

vgate_run 08 -- \
    --screen '$RUN_DIR/screen-08' \
    --via-virtio \
    --net '$RUN_DIR/cap08.bin' --net-arp-respond 10.0.0.2 \
    --net-tcp-respond 10.0.0.2:6667:relay --net-tcp-respond-relay 127.0.0.1:26671 \
    --script '$RUN_DIR/script-08.txt' \
    --script-expect 'goirc: msg #other alice filler-59 backlog' --timeout 300

vgate_assert 08 serial-contains 'gotabwm: mode live'
vgate_assert 08 serial-contains 'exec: loaded GOTABWM.ELF'
vgate_assert 08 serial-contains 'gotabwm: session load n=2 mode=restore'
vgate_assert 08 serial-contains 'gotabwm: session reexec n=2'
vgate_assert 08 serial-count 'gotabwm: session exec GOIRC.ELF' 2
vgate_assert 08 serial-count 'goirc: open id=' 2
vgate_assert 08 serial-count 'goirc: declare accepted' 2
vgate_assert 08 serial-count 'goirc: state restored' 2
vgate_assert 08 serial-contains 'goirc: dial 10.0.0.2 6667 tls'
vgate_assert 08 serial-contains 'goirc: dial 10.0.0.2 6667 plain'
vgate_assert 08 serial-absent 'goirc: consent ask'
vgate_assert 08 serial-count 'goirc: registered nick=gate' 2
vgate_assert 08 serial-count 'goirc: join #goirc' 2
vgate_assert 08 serial-contains 'goirc: join #other'
vgate_assert 08 serial-contains 'goirc: focus #goirc'
vgate_assert 08 serial-contains 'goirc: msg #goirc alice restored-tail'
vgate_assert 08 serial-absent 'goirc: refuse plaintext'
vgate_assert 08 serial-absent 'goirc: state refused'
vgate_assert 08 serial-absent 'goirc: error'
vgate_assert 08 serial-absent '[EXC] parking:'
# Order: the restore precedes every dial, and the plain session's join
# earns the fixture's 'restored-tail' (the role-2 conn).
vgate_assert 08 python <<'PY'
import os, sys
ser = open(os.environ["VG_SER"], errors="replace").read()
pos = 0
for m in ["gotabwm: session load n=2 mode=restore", "goirc: state restored",
          "goirc: dial 10.0.0.2 6667 plain", "goirc: join #goirc",
          "goirc: msg #goirc alice restored-tail"]:
    i = ser.find(m, pos)
    if i < 0:
        sys.exit("marker %r missing after pos %d" % (m, pos))
    pos = i + len(m)

# Fixture transcript (cumulative): the reopened conn took the 'reopen'
# role and answered 'restored-tail'; each iteration's two boots each
# wrap one TLS conn.
tag = os.environ["VG_TAG"]
it = int(tag.rsplit("-", 1)[-1]) if "-" in tag else 1
log = open(os.path.join(os.environ["RUN_DIR"], "ircd-26671.log")).read()
need = ["role reopen", "tx :alice!u@h PRIVMSG #goirc :restored-tail"]
missing = [n for n in need if n not in log]
tls = sum(1 for l in log.splitlines() if "tls ok" in l)
if tls < 2 * it:
    missing.append("tls wraps %d < 2*iteration %d" % (tls, it))
if missing:
    sys.exit("journey fixture missing: %r\n%s" % (missing, log))
PY

# --- M152e (#2919) run 09: /hl toast + the /log file's share tail --------
#
# One boot: the seat, then GOIRC --ui --plain joins #goirc. The single
# --input-string batch types the leg's three lines — the rule
# ('/hl add virelai'), the arm ('/log on' on the focused #goirc), and a
# channel 'hi' that is the fixture's go signal (the slash verbs emit no
# wire traffic, so the flood can't arrive before the rule is armed).
# The fixture answers 'hi' with a virelai line — carrying NO 'gate'
# word, so 'goirc: toast #goirc' can only be the rule's — and a 40-line
# flood: 42 backlog lines against the 32-line flush cadence, so
# /host/GOIRC/10-0-0-2-GOIRC.LOG holds lines 1-32 and the python assert
# finds flood-30 as the tail.
#
# exec-order: assert-proven -- the run ends on 'flood-40', a marker the
# fixture's wire traffic makes only GOIRC print; every assert reads
# output a non-running program cannot produce (the toast chain, the
# fixture transcript, the share file).
vgate_file script-09.txt <<'EOF'
net ip 10.0.0.1
net arp 10.0.0.2
vf rm GOTABWM.DEMO
vf rm SESSION.TABS
vf rm NOTIFY.HIST
set GOMAXPROCS=1
wm
exec GOTABWM.ELF
EOF

vgate_file script2-09.txt <<'EOF'
dui focus 0
exec GOIRC.ELF --ui --host=10.0.0.2 --port=6672 --nick=gate --join '#goirc' --plain
EOF

vgate_run 09 -- \
    --screen '$RUN_DIR/screen-09' \
    --via-virtio \
    --net '$RUN_DIR/cap09.bin' --net-arp-respond 10.0.0.2 \
    --net-tcp-respond 10.0.0.2:6672:relay --net-tcp-respond-relay 127.0.0.1:26672 \
    --script '$RUN_DIR/script-09.txt' \
    --script2 '$RUN_DIR/script2-09.txt' --script2-after 'gotabwm: win focus' \
    --input-string $'/hl add virelai\n/log on\nhi\n' \
    --input-string-after 'goirc: join #goirc' \
    --script-expect 'goirc: msg #goirc alice flood-40 line' --timeout 300

vgate_assert 09 serial-contains 'gotabwm: mode live'
vgate_assert 09 serial-contains 'exec: loaded GOIRC.ELF'
vgate_assert 09 serial-contains 'goirc: join #goirc'
vgate_assert 09 serial-contains 'goirc: line /hl add virelai'
vgate_assert 09 serial-contains 'goirc: hl add virelai'
vgate_assert 09 serial-contains 'goirc: line /log on'
vgate_assert 09 serial-contains 'goirc: log on #goirc'
vgate_assert 09 serial-contains 'goirc: line hi'
vgate_assert 09 serial-contains 'goirc: msg #goirc alice release virelai today'
vgate_assert 09 serial-contains 'goirc: toast #goirc'
vgate_assert 09 serial-absent 'goirc: toast-refused'
vgate_assert 09 serial-contains 'goirc: msg #goirc alice flood-40 line'
vgate_assert 09 serial-contains 'goirc: log write /host/GOIRC/10-0-0-2-GOIRC.LOG'
vgate_assert 09 serial-absent 'goirc: log write-fail'
vgate_assert 09 serial-absent 'goirc: error'
vgate_assert 09 serial-absent '[EXC] parking:'
# Order: the verbs must land before the wire 'hi' (the flood's trigger),
# the toast before the flood's tail, the publish before the run ends.
vgate_assert 09 python <<'PY'
import os, sys
ser = open(os.environ["VG_SER"], errors="replace").read()
pos = 0
for m in ["goirc: hl add virelai", "goirc: log on #goirc",
          "goirc: line hi", "goirc: toast #goirc",
          "goirc: log write /host/GOIRC/10-0-0-2-GOIRC.LOG",
          "goirc: msg #goirc alice flood-40 line"]:
    i = ser.find(m, pos)
    if i < 0:
        sys.exit("marker %r missing after pos %d" % (m, pos))
    pos = i + len(m)
PY
# Fixture transcript: registration, the JOIN, the typed 'hi' that
# triggered the flood, and both halves of the answer on the wire.
vgate_assert 09 python <<'PY'
import os, sys
tag = os.environ["VG_TAG"]
it = int(tag.rsplit("-", 1)[-1]) if "-" in tag else 1
log = open(os.path.join(os.environ["RUN_DIR"], "ircd-26672.log")).read()
need = ["c0 open", "c0 rx NICK gate", "c0 rx USER", "c0 rx JOIN #goirc",
        "c0 rx PRIVMSG #goirc :hi",
        "tx :alice!u@h PRIVMSG #goirc :release virelai today",
        "tx :alice!u@h PRIVMSG #goirc :flood-01 line",
        "tx :alice!u@h PRIVMSG #goirc :flood-40 line"]
missing = [n for n in need if n not in log]
joins = sum(1 for l in log.splitlines() if "rx JOIN #goirc" in l)
if joins < it:
    missing.append("joins %d < iteration %d" % (joins, it))
if missing:
    sys.exit("logs fixture missing: %r\n%s" % (missing, log))
PY
# The share file: the whole-file rewrite holds the first 32 logged
# lines — '<gate> hi', the virelai ping, flood-01..flood-30 — under the
# 64 KiB cap, flood-30 the tail the card names.
vgate_assert 09 python <<'PY'
import os, sys
share = os.environ["VG_SHARE"]
p = os.path.join(share, "GOIRC", "10-0-0-2-GOIRC.LOG")
if not os.path.exists(p):
    have = sorted(os.listdir(share)) if os.path.isdir(share) else "no share"
    sys.exit("log file missing at %s (share holds %r)" % (p, have))
body = open(p, "rb").read()
if len(body) > 64 * 1024:
    sys.exit("log over the 64 KiB cap: %d" % len(body))
lines = body.decode("utf-8", "replace").splitlines()
if lines[:2] != ["<gate> hi", "<alice> release virelai today"]:
    sys.exit("log head %r" % (lines[:3],))
if len(lines) != 32:
    sys.exit("flush cadence: %d lines on disk, want 32: %r"
             % (len(lines), lines[-4:]))
if lines[-1] != "<alice> flood-30 line":
    sys.exit("tail %r want flood-30" % (lines[-1],))
for i in range(30):
    want = "<alice> flood-%02d line" % (i + 1)
    if want not in lines:
        sys.exit("missing %r in %r" % (want, lines))
print("run09 log: %d lines, %d bytes, tail %r" % (len(lines), len(body), lines[-1]))
PY

# --- M196c (#3380) runs 10+11: the keychain legs ------------------------
#
# Run 10 — stored SASL + the procs no-secret read. '--sasl=gate' carries
# the ACCOUNT only; the password resolves out of the 'irc-gate' row the
# setup seeded (app-bound to the uid_system-owned GOIRC.ELF). The fixture
# 903s only when AUTHENTICATE decodes to the row's value, so 'goirc: sasl
# ok' IS the slot-70 round-trip. script2's 'procs' lands while the
# authenticated session is live: its rows name the image, never argv —
# and the serial-absent pins 'sasl-secret-7' out of the whole transcript
# (exec echo, procs output, marker stream alike; nothing the script
# typed ever spelled it).
vgate_file script-10.txt <<'EOF'
net ip 10.0.0.1
net arp 10.0.0.2
vf rm GOTABWM.DEMO
vf rm SESSION.TABS
vf rm NOTIFY.HIST
exec GOIRC.ELF --host=10.0.0.2 --port=6673 --nick=gate --join '#goirc' --plain --sasl=gate
EOF

vgate_file script2-10.txt <<'EOF'
procs
EOF

vgate_run 10 -- \
    --screen '$RUN_DIR/screen-10' \
    --via-virtio \
    --net '$RUN_DIR/cap10.bin' --net-arp-respond 10.0.0.2 \
    --net-tcp-respond 10.0.0.2:6673:relay --net-tcp-respond-relay 127.0.0.1:26673 \
    --script '$RUN_DIR/script-10.txt' \
    --script2 '$RUN_DIR/script2-10.txt' --script2-after 'goirc: sasl ok' \
    --script-expect 'goirc: msg #goirc alice sasl-hello' --timeout 300

vgate_assert 10 serial-contains 'exec: loaded GOIRC.ELF'
vgate_assert 10 serial-contains 'goirc: dial 10.0.0.2 6673 plain'
vgate_assert 10 serial-contains 'goirc: sasl ok'
vgate_assert 10 serial-contains 'goirc: registered nick=gate'
vgate_assert 10 serial-contains 'goirc: join #goirc'
vgate_assert 10 serial-contains 'goirc: msg #goirc alice sasl-hello'
vgate_assert 10 serial-contains 'procs: count='
vgate_assert 10 serial-contains 'name=GOIRC.ELF'
vgate_assert 10 serial-absent 'sasl-secret-7'
vgate_assert 10 serial-absent 'goirc: refuse sasl-argv'
vgate_assert 10 serial-absent 'goirc: sasl fail'
vgate_assert 10 serial-absent 'secret_not_found'
vgate_assert 10 serial-absent 'goirc: error'
vgate_assert 10 serial-absent '[EXC] parking:'
# Order + wire proof: the procs read comes after 'sasl ok' (the script2
# gate), and the fixture transcript shows the whole CAP/AUTHENTICATE
# exchange ending in a payload-verified 903 and the post-join greeting.
vgate_assert 10 python <<'PY'
import os, sys
ser = open(os.environ["VG_SER"], errors="replace").read()
pos = 0
for m in ["goirc: sasl ok", "procs: count="]:
    i = ser.find(m, pos)
    if i < 0:
        sys.exit("marker %r missing after pos %d" % (m, pos))
    pos = i + len(m)
log = open(os.path.join(os.environ["RUN_DIR"], "ircd-26673.log")).read()
need = ["rx CAP REQ :sasl", "tx :srv CAP * ACK :sasl",
        "rx AUTHENTICATE PLAIN", "tx AUTHENTICATE +",
        "sasl payload ok", "tx :srv 903", "rx CAP END",
        "rx NICK gate", "rx USER", "tx :srv 001",
        "rx JOIN #goirc", "tx :alice!u@h PRIVMSG #goirc :sasl-hello"]
missing = [n for n in need if n not in log]
sys.exit("sasl fixture missing: %r\n%s" % (missing, log) if missing else 0)
PY

# Run 11 — the argv credential is refused outright: '--sasl
# gate:sasl-decoy-9' prints the named marker and exits 2 before any
# dial. No --net legs: nothing may reach a socket.
vgate_file script-11.txt <<'EOF'
exec GOIRC.ELF --host=10.0.0.2 --nick=gate --plain --sasl=gate:sasl-decoy-9
EOF

vgate_run 11 -- \
    --screen '$RUN_DIR/screen-11' \
    --via-virtio \
    --script '$RUN_DIR/script-11.txt' --script-expect 'procs GOIRC.ELF exited status=2' --timeout 180

vgate_assert 11 serial-contains 'goirc: refuse sasl-argv'
vgate_assert 11 serial-contains 'procs GOIRC.ELF exited status=2'
vgate_assert 11 serial-absent 'goirc: dial'
vgate_assert 11 serial-absent 'goirc: registered'
vgate_assert 11 serial-absent 'goirc: sasl ok'
vgate_assert 11 serial-absent '[EXC] parking:'

# --- #3779 runs 12+13: the first-run connect face -------------------------
#
# Run 12 — no saved server. GOIRC.TXT is rm'd (it may exist from a prior
# iteration's run 12/13 — the share persists), the seat boots, and a BARE
# 'exec GOIRC.ELF' opens the window with no argv and no staged blob. The
# face is the prompt chain: --input-string's first line answers 'server'
# (10.0.0.2 → default port 6667, plain), the second answers 'nick'; the
# plaintext consent ask still gates the dial ('tab,return' = Yes), and the
# first 001 persists the resolved config — 'goirc: config saved' is the
# terminal marker and the file-on-share half is checked byte-level below.
vgate_file script-12.txt <<'EOF'
net ip 10.0.0.1
net arp 10.0.0.2
vf rm GOTABWM.DEMO
vf rm SESSION.TABS
vf rm NOTIFY.HIST
vf rm GOIRC.TXT
set GOMAXPROCS=1
wm
exec GOTABWM.ELF
EOF

vgate_file script2-12.txt <<'EOF'
dui focus 0
exec GOIRC.ELF
EOF

vgate_run 12 -- \
    --screen '$RUN_DIR/screen-12' \
    --via-virtio \
    --net '$RUN_DIR/cap12.bin' --net-arp-respond 10.0.0.2 \
    --net-tcp-respond 10.0.0.2:6667:relay --net-tcp-respond-relay 127.0.0.1:26674 \
    --script '$RUN_DIR/script-12.txt' \
    --script2 '$RUN_DIR/script2-12.txt' --script2-after 'gotabwm: win focus' \
    --input-string $'10.0.0.2\ngate\n' --input-string-after 'goirc: connect ask' \
    --input-chords 'tab,return' --input-chords-after 'goirc: consent ask' \
    --script-expect 'goirc: config saved' --timeout 300

vgate_assert 12 serial-contains 'gotabwm: mode live'
vgate_assert 12 serial-contains 'exec: loaded GOIRC.ELF'
vgate_assert 12 serial-contains 'goirc: open id='
vgate_assert 12 serial-contains 'goirc: declare accepted'
vgate_assert 12 serial-contains 'goirc: connect ask'
vgate_assert 12 serial-absent 'goirc: connect saved'
vgate_assert 12 serial-contains 'goirc: consent ask'
vgate_assert 12 serial-contains 'goirc: consent yes'
vgate_assert 12 serial-contains 'goirc: dial 10.0.0.2 6667 plain'
vgate_assert 12 serial-contains 'goirc: registered nick=gate'
vgate_assert 12 serial-contains 'goirc: config saved'
vgate_assert 12 serial-absent 'goirc: connect cancel'
vgate_assert 12 serial-absent 'goirc: config write-fail'
vgate_assert 12 serial-absent 'goirc: error'
vgate_assert 12 serial-absent '[EXC] parking:'
# Order: the face precedes the consent ask, the dial waits on consent yes,
# and the register precedes the publish — a prompt that never ran cannot
# produce any of these.
vgate_assert 12 python <<'PY'
import os, sys
ser = open(os.environ["VG_SER"], errors="replace").read()
pos = 0
for m in ["goirc: connect ask", "goirc: consent ask", "goirc: consent yes",
          "goirc: dial 10.0.0.2 6667 plain", "goirc: registered nick=gate",
          "goirc: config saved"]:
    i = ser.find(m, pos)
    if i < 0:
        sys.exit("marker %r missing after pos %d" % (m, pos))
    pos = i + len(m)

# The file IS the persist half: byte-level, written by the register-time
# publish — host+port+nick as typed, plain=on as consented.
tag = os.environ["VG_TAG"]
it = int(tag.rsplit("-", 1)[-1]) if "-" in tag else 1
body = open(os.path.join(os.environ["VG_SHARE"], "GOIRC.TXT")).read()
for want in ["host=10.0.0.2", "port=6667", "nick=gate", "tls=off",
             "plain=on"]:
    if want not in body:
        sys.exit("GOIRC.TXT missing %r:\n%s" % (want, body))

log = open(os.path.join(os.environ["RUN_DIR"], "ircd-26674.log")).read()
need = ["c0 open", "c0 rx NICK gate", "c0 rx USER", "tx :srv 001"]
missing = [n for n in need if n not in log]
opens = sum(1 for l in log.splitlines() if l.rstrip().endswith(" open"))
if opens < it:
    missing.append("opens %d < iteration %d" % (opens, it))
if missing:
    sys.exit("first-run fixture missing: %r\n%s" % (missing, log))
PY

# --- run 13: the saved-server picker --------------------------------------
#
# The very next boot over the same share: run 12's register-time publish
# left GOIRC.TXT in place, so the bare exec opens the picker — the stored
# row focused — and a bare 'return' connects it. The stored plain=on
# consent means 'consent ask' stays ABSENT the whole run; the register
# re-publishes the file.
vgate_file script-13.txt <<'EOF'
net ip 10.0.0.1
net arp 10.0.0.2
vf rm GOTABWM.DEMO
vf rm SESSION.TABS
vf rm NOTIFY.HIST
set GOMAXPROCS=1
wm
exec GOTABWM.ELF
EOF

vgate_file script2-13.txt <<'EOF'
dui focus 0
exec GOIRC.ELF
EOF

vgate_run 13 -- \
    --screen '$RUN_DIR/screen-13' \
    --via-virtio \
    --net '$RUN_DIR/cap13.bin' --net-arp-respond 10.0.0.2 \
    --net-tcp-respond 10.0.0.2:6667:relay --net-tcp-respond-relay 127.0.0.1:26675 \
    --script '$RUN_DIR/script-13.txt' \
    --script2 '$RUN_DIR/script2-13.txt' --script2-after 'gotabwm: win focus' \
    --input-chords 'return' --input-chords-after 'goirc: connect ask' \
    --script-expect 'goirc: config saved' --timeout 300

vgate_assert 13 serial-contains 'gotabwm: mode live'
vgate_assert 13 serial-contains 'exec: loaded GOIRC.ELF'
vgate_assert 13 serial-contains 'goirc: config /host/GOIRC.TXT'
vgate_assert 13 serial-contains 'goirc: connect ask'
vgate_assert 13 serial-contains 'goirc: connect saved'
vgate_assert 13 serial-absent 'goirc: consent ask'
vgate_assert 13 serial-contains 'goirc: dial 10.0.0.2 6667 plain'
vgate_assert 13 serial-contains 'goirc: registered nick=gate'
vgate_assert 13 serial-contains 'goirc: config saved'
vgate_assert 13 serial-absent 'goirc: connect cancel'
vgate_assert 13 serial-absent 'goirc: error'
vgate_assert 13 serial-absent '[EXC] parking:'
vgate_assert 13 python <<'PY'
import os, sys
ser = open(os.environ["VG_SER"], errors="replace").read()
pos = 0
for m in ["goirc: connect ask", "goirc: connect saved",
          "goirc: dial 10.0.0.2 6667 plain", "goirc: registered nick=gate",
          "goirc: config saved"]:
    i = ser.find(m, pos)
    if i < 0:
        sys.exit("marker %r missing after pos %d" % (m, pos))
    pos = i + len(m)
tag = os.environ["VG_TAG"]
it = int(tag.rsplit("-", 1)[-1]) if "-" in tag else 1
log = open(os.path.join(os.environ["RUN_DIR"], "ircd-26675.log")).read()
need = ["c0 open", "c0 rx NICK gate", "c0 rx USER", "tx :srv 001"]
missing = [n for n in need if n not in log]
opens = sum(1 for l in log.splitlines() if l.rstrip().endswith(" open"))
if opens < it:
    missing.append("opens %d < iteration %d" % (opens, it))
if missing:
    sys.exit("picker fixture missing: %r\n%s" % (missing, log))
PY

# ===========================================================================
# M212c (#3874) runs 14-16: the WebSocket legs. The consumer is GOIRC's
# --ws gateway transport (ircv3's line-per-message convention); wsd.py is
# the ws-speaking fixture — an echo peer for the codec's hermetic leg, an
# IRC-over-ws endpoint for the consumer leg, and a conn-indexed poisoner
# for the refusal leg. WSECHO.ELF is the codec's own driver (gate-only
# binary; the codec's unit tests live in user/go/ws).
#
#   run 14 -- echo leg: WSECHO round-trips text + binary + ping/pong,
#             reassembles the fixture's fragmented text, answers the
#             fixture's ping, and runs the close handshake.
#   run 15 -- consumer leg: GOIRC --ws --plain registers + joins +
#             receives over ws text messages; 'goirc: dial ... ws' is the
#             frame-path marker and the fixture log proves the frame
#             traffic carried the IRC lines.
#   run 16 -- refusal leg: WSECHO --refuse dials five poisoned conns and
#             names ws_opcode_bad / ws_server_masked / ws_frame_too_large
#             / ws_accept_bad / ws_extension_unnegotiated.
#
# HOST PREREQUISITE: bash tools/go/build-wsecho.sh -> .build/go/WSECHO.ELF

vgate_file script-14.txt <<'EOF'
net ip 10.0.0.1
net arp 10.0.0.2
vf rm GOTABWM.DEMO
vf rm SESSION.TABS
vf rm NOTIFY.HIST
exec WSECHO.ELF 10.0.0.2 8081
EOF

vgate_file script-15.txt <<'EOF'
net ip 10.0.0.1
net arp 10.0.0.2
vf rm GOTABWM.DEMO
vf rm SESSION.TABS
vf rm NOTIFY.HIST
exec GOIRC.ELF --host=10.0.0.2 --port=8080 --nick=gate --join '#goirc' --ws --plain
EOF

vgate_file script-16.txt <<'EOF'
net ip 10.0.0.1
net arp 10.0.0.2
vf rm GOTABWM.DEMO
vf rm SESSION.TABS
vf rm NOTIFY.HIST
exec WSECHO.ELF 10.0.0.2 8082 --refuse
EOF

vgate_setup_python <<'PY'
import os, shutil, subprocess, sys, time

run = os.environ["RUN_DIR"]
share = os.environ.get("VG_SHARE") or os.path.join(run, "share")
os.makedirs(share, exist_ok=True)

wsecho = os.path.join(".build", "go", "WSECHO.ELF")
if not os.path.exists(wsecho):
    sys.exit("WSECHO.ELF missing (expected " + wsecho + ") - build it "
             "first: bash tools/go/build-wsecho.sh")
shutil.copy(wsecho, os.path.join(share, "WSECHO.ELF"))
print("go-irc: staged WSECHO.ELF (%d)" % os.path.getsize(os.path.join(share, "WSECHO.ELF")))

# wsd.py -- the ws fixture: Upgrade handshake + frame codec in stdlib
# python. echo/wsirc/refuse modes; one process per port, conn-indexed.
wsd = os.path.join(run, "wsd.py")
with open(wsd, "w") as f:
    f.write('''
import base64, hashlib, socket, sys, time

mode, port, logpath = sys.argv[1], int(sys.argv[2]), sys.argv[3]
logf = open(logpath, "w", buffering=1)

def log(s):
    logf.write(s + "\\n")

GUID = b"258EAFA5-E914-47DA-95CA-C5AB0DC85B11"

srv = socket.socket(socket.AF_INET, socket.SOCK_STREAM)
srv.setsockopt(socket.SOL_SOCKET, socket.SO_REUSEADDR, 1)
srv.bind(("127.0.0.1", port))
srv.listen(4)
srv.settimeout(1800)
log("listening mode=" + mode)

def ws_key(head):
    for line in head.split(b"\\r\\n")[1:]:
        if line.lower().startswith(b"sec-websocket-key:"):
            return line.split(b":", 1)[1].strip()
    return None

def ws_accept(key):
    return base64.b64encode(hashlib.sha1(key + GUID).digest())

def ws_handshake(conn, poison=None):
    """Read the request head; answer 101 unless poison says otherwise.
    Returns the post-head leftover bytes, or None on failure."""
    buf = b""
    while b"\\r\\n\\r\\n" not in buf:
        chunk = conn.recv(4096)
        if not chunk:
            return None
        buf += chunk
        if len(buf) > 8192:
            return None
    head, buf = buf.split(b"\\r\\n\\r\\n", 1)
    log("hs " + head.split(b"\\r\\n", 1)[0].decode("utf-8", "replace"))
    key = ws_key(head)
    if key is None:
        return None
    if poison == "badaccept":
        acc = b"AAAAAAAAAAAAAAAAAAAAAAAAAAA="
    else:
        acc = ws_accept(key)
    extra = b""
    if poison == "extension":
        extra = b"\\r\\nSec-WebSocket-Extensions: permessage-deflate"
    conn.sendall(b"HTTP/1.1 101 Switching Protocols\\r\\n"
                 b"Upgrade: websocket\\r\\nConnection: Upgrade\\r\\n"
                 b"Sec-WebSocket-Accept: " + acc + extra + b"\\r\\n\\r\\n")
    return buf

def ws_send(conn, op, payload, fin=True, masked=False, declare=None):
    b0 = (0x80 if fin else 0) | op
    n = len(payload) if declare is None else declare
    out = bytes([b0])
    mb = 0x80 if masked else 0
    if n < 126:
        out += bytes([mb | n])
    elif n <= 0xFFFF:
        out += bytes([mb | 126, (n >> 8) & 0xFF, n & 0xFF])
    else:
        out += bytes([mb | 127]) + n.to_bytes(8, "big")
    if masked:
        key = b"\\x11\\x22\\x33\\x44"
        out += key
        payload = bytes(c ^ key[i % 4] for i, c in enumerate(payload))
    conn.sendall(out + payload)

def ws_recv(conn, buf):
    """Return (fin, op, payload, buf) or (None, None, None, buf) on EOF.
    Client frames arrive masked; server-intended traffic never is."""
    while True:
        if len(buf) >= 2:
            b0, b1 = buf[0], buf[1]
            n = b1 & 0x7F
            off = 2
            if n == 126:
                if len(buf) >= 4:
                    n = (buf[2] << 8) | buf[3]
                    off = 4
                else:
                    n = None
            elif n == 127:
                if len(buf) >= 10:
                    n = int.from_bytes(buf[2:10], "big")
                    off = 10
                else:
                    n = None
            if n is not None:
                mask = None
                if b1 & 0x80 and len(buf) >= off + 4:
                    mask = buf[off:off + 4]
                    off += 4
                if not (b1 & 0x80) or mask is not None:
                    if len(buf) >= off + n:
                        payload = bytes(buf[off:off + n])
                        if mask is not None:
                            payload = bytes(c ^ mask[i % 4] for i, c in enumerate(payload))
                        return b0 & 0x80, b0 & 0x0F, payload, buf[off + n:]
        chunk = conn.recv(4096)
        if not chunk:
            return None, None, None, buf
        buf += chunk

def ws_send_line(conn, line):
    ws_send(conn, 0x1, line.encode())
    log("tx " + line)

def wsirc_session(conn, idx, buf):
    """One IRC line per ws text message — the ircv3 gateway framing."""
    nick, user = None, False
    deadline = time.time() + 90
    while time.time() < deadline:
        try:
            fin, op, payload, buf = ws_recv(conn, buf)
        except Exception:
            return
        if op is None:
            return
        if op == 0x8:
            ws_send(conn, 0x8, payload)
            log("c%d ws rx close" % idx)
            return
        if op == 0x9:
            ws_send(conn, 0xA, payload)
            continue
        if op not in (0x1, 0x2):
            continue
        line = payload.decode("utf-8", "replace").rstrip("\\r\\n")
        log("c%d ws rx text %s" % (idx, line))
        parts = line.split()
        if not parts:
            continue
        cmd = parts[0].upper()
        if cmd == "NICK" and len(parts) > 1:
            nick = parts[1]
        elif cmd == "USER":
            user = True
        elif cmd == "PING":
            ws_send_line(conn, ":srv PONG srv :" + (parts[1] if len(parts) > 1 else ""))
        elif cmd == "JOIN" and len(parts) > 1:
            ch = parts[1].lstrip(":")
            ws_send_line(conn, ":%s!u@h JOIN %s" % (nick, ch))
            ws_send_line(conn, ":alice!u@h PRIVMSG %s :ws-hello" % ch)
        elif cmd == "PRIVMSG" and len(parts) > 2:
            ws_send_line(conn, ":alice!u@h PRIVMSG %s :thanks ws" % parts[1])
        elif cmd == "QUIT":
            return
        if nick and user:
            ws_send_line(conn, ":srv 001 %s :Welcome to the ws net" % nick)
            nick, user = nick, False  # 001 once
    return

def echo_session(conn, idx, buf):
    """The codec leg: fragmented text + a server ping up front, then
    echo data frames, pong the pings, answer the close."""
    ws_send(conn, 0x1, b"wsecho-frag-head", fin=False)
    ws_send(conn, 0x0, b"-tail", fin=True)
    ws_send(conn, 0x9, b"srvx")
    log("c%d ws tx frag+ping" % idx)
    deadline = time.time() + 90
    while time.time() < deadline:
        try:
            fin, op, payload, buf = ws_recv(conn, buf)
        except Exception:
            return
        if op is None:
            return
        if op == 0x1:
            log("c%d ws rx text %s" % (idx, payload.decode("utf-8", "replace")))
            ws_send(conn, 0x1, payload)
        elif op == 0x2:
            log("c%d ws rx bin %d" % (idx, len(payload)))
            ws_send(conn, 0x2, payload)
        elif op == 0x9:
            log("c%d ws rx ping" % idx)
            ws_send(conn, 0xA, payload)
        elif op == 0xA:
            log("c%d ws rx pong %s" % (idx, payload.decode("utf-8", "replace")))
        elif op == 0x8:
            log("c%d ws rx close" % idx)
            ws_send(conn, 0x8, payload)
            return
    return

def refuse_session(conn, idx):
    """Conn-indexed poison: the client's five named refusals in order."""
    which = idx % 5
    if which == 3:
        buf = ws_handshake(conn, poison="badaccept")
    elif which == 4:
        buf = ws_handshake(conn, poison="extension")
    else:
        buf = ws_handshake(conn)
    if buf is None:
        log("c%d poison handshake-read failed" % idx)
        return
    if which == 0:
        log("c%d poison opcode" % idx)
        ws_send(conn, 0x3, b"x")
    elif which == 1:
        log("c%d poison masked" % idx)
        ws_send(conn, 0x1, b"x", masked=True)
    elif which == 2:
        log("c%d poison oversize" % idx)
        ws_send(conn, 0x1, b"", declare=70000)  # head only
    else:
        log("c%d poison %s" % (idx, "badaccept" if which == 3 else "extension"))
    time.sleep(0.3)
    try:
        conn.close()
    except Exception:
        pass

budget = {"echo": 4, "wsirc": 4, "refuse": 16}[mode]
for idx in range(budget):
    try:
        conn, _ = srv.accept()
    except socket.timeout:
        log("accept timeout at conn %d" % idx)
        break
    conn.settimeout(60)
    log("c%d open" % idx)
    try:
        if mode == "refuse":
            refuse_session(conn, idx)
        else:
            buf = ws_handshake(conn)
            if buf is not None:
                if mode == "echo":
                    echo_session(conn, idx, buf)
                elif mode == "wsirc":
                    wsirc_session(conn, idx, buf)
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

procs = []
for mode, port in (("echo", 26676), ("wsirc", 26677), ("refuse", 26678)):
    subprocess.run([sys.executable,
                    os.path.join("tools", "lib", "port_guard.py"),
                    str(port)],
                   check=True)
    logpath = os.path.join(run, "wsd-%d.log" % port)
    open(logpath, "w").close()
    proc = subprocess.Popen([sys.executable, wsd, mode, str(port), logpath],
                            stdout=subprocess.DEVNULL, stderr=subprocess.STDOUT,
                            start_new_session=True)
    procs.append(proc)
    print("go-irc: ws fixture %s pid=%d on 127.0.0.1:%d" % (mode, proc.pid, port))

time.sleep(0.4)
for proc in procs:
    if proc.poll() is not None:
        sys.exit("go-irc: ws fixture exited early rc=%s" % proc.returncode)
PY

# Run 14 -- the echo leg: text + binary + ping/pong + fragmentation +
# close in one session against the ws fixture over :relay.
vgate_run 14 -- \
    --screen '$RUN_DIR/screen' \
    --via-virtio \
    --net '$RUN_DIR/cap14.bin' --net-arp-respond 10.0.0.2 \
    --net-tcp-respond 10.0.0.2:8081:relay --net-tcp-respond-relay 127.0.0.1:26676 \
    --script '$RUN_DIR/script-14.txt' --script-expect 'wsecho: done' --timeout 180

vgate_assert 14 serial-contains 'wsecho: dial 10.0.0.2 8081'
vgate_assert 14 serial-contains 'wsecho: handshake ok'
vgate_assert 14 serial-contains 'wsecho: text ok'
vgate_assert 14 serial-contains 'wsecho: binary ok'
vgate_assert 14 serial-contains 'wsecho: frag ok'
vgate_assert 14 serial-contains 'wsecho: ping srv'
vgate_assert 14 serial-contains 'wsecho: pong ok'
vgate_assert 14 serial-contains 'wsecho: close ok'
vgate_assert 14 serial-contains 'wsecho: done'
vgate_assert 14 serial-absent 'wsecho: error'
vgate_assert 14 serial-absent 'wsecho: refuse'
vgate_assert 14 serial-absent '[EXC] parking:'
vgate_assert 14 python <<'PY'
import os, sys
tag = os.environ["VG_TAG"]
it = int(tag.rsplit("-", 1)[-1]) if "-" in tag else 1
log = open(os.path.join(os.environ["RUN_DIR"], "wsd-26676.log")).read()
need = ["hs GET / HTTP/1.1", "ws tx frag+ping", "ws rx text wsecho-text-7",
        "ws rx bin 4", "ws rx ping", "ws rx close"]
missing = [n for n in need if n not in log]
# The auto-pong for the fixture's 'srvx' ping must be on the transcript —
# that is the client's RFC pong, not a script artifact.
if "ws rx pong srvx" not in log:
    missing.append("ws rx pong srvx")
opens = sum(1 for l in log.splitlines() if "hs GET" in l)
if opens < it:
    missing.append("handshakes %d < iteration %d" % (opens, it))
if missing:
    sys.exit("ws echo transcript missing: %r\n%s" % (missing, log))
PY

# Run 15 -- the consumer leg: GOIRC's --ws transport carries register +
# join + a received PRIVMSG; 'goirc: dial ... ws' names the frame path.
vgate_run 15 -- \
    --screen '$RUN_DIR/screen' \
    --via-virtio \
    --net '$RUN_DIR/cap15.bin' --net-arp-respond 10.0.0.2 \
    --net-tcp-respond 10.0.0.2:8080:relay --net-tcp-respond-relay 127.0.0.1:26677 \
    --script '$RUN_DIR/script-15.txt' --script-expect 'goirc: msg #goirc alice ws-hello' --timeout 240

vgate_assert 15 serial-contains 'goirc: dial 10.0.0.2 8080 ws'
vgate_assert 15 serial-contains 'goirc: registered nick=gate'
vgate_assert 15 serial-contains 'goirc: join #goirc'
vgate_assert 15 serial-contains 'goirc: msg #goirc alice ws-hello'
vgate_assert 15 serial-absent 'goirc: refuse plaintext'
vgate_assert 15 serial-absent 'ws_'
vgate_assert 15 serial-absent '[EXC] parking:'
vgate_assert 15 python <<'PY'
import os, sys
tag = os.environ["VG_TAG"]
it = int(tag.rsplit("-", 1)[-1]) if "-" in tag else 1
log = open(os.path.join(os.environ["RUN_DIR"], "wsd-26677.log")).read()
# The frame-path proof: the IRC register rode ws text messages, not a
# byte tunnel — each 'ws rx text' names one frame's line.
need = ["hs GET / HTTP/1.1", "ws rx text NICK gate", "ws rx text USER",
        "tx :srv 001 gate", "ws rx text JOIN #goirc", "ws-hello"]
missing = [n for n in need if n not in log]
regs = sum(1 for l in log.splitlines() if "ws rx text NICK" in l)
if regs < it:
    missing.append("NICK frames %d < iteration %d" % (regs, it))
if missing:
    sys.exit("wsirc transcript missing: %r\n%s" % (missing, log))
PY

# Run 16 -- the refusal leg: five poisoned conns, five named refusals.
vgate_run 16 -- \
    --screen '$RUN_DIR/screen' \
    --via-virtio \
    --net '$RUN_DIR/cap16.bin' --net-arp-respond 10.0.0.2 \
    --net-tcp-respond 10.0.0.2:8082:relay --net-tcp-respond-relay 127.0.0.1:26678 \
    --script '$RUN_DIR/script-16.txt' --script-expect 'wsecho: refuse done' --timeout 240

vgate_assert 16 serial-contains 'wsecho: refuse ws_opcode_bad'
vgate_assert 16 serial-contains 'wsecho: refuse ws_server_masked'
vgate_assert 16 serial-contains 'wsecho: refuse ws_frame_too_large'
vgate_assert 16 serial-contains 'wsecho: refuse ws_accept_bad'
vgate_assert 16 serial-contains 'wsecho: refuse ws_extension_unnegotiated'
vgate_assert 16 serial-contains 'wsecho: refuse done'
vgate_assert 16 serial-absent 'wsecho: refuse-miss'
vgate_assert 16 serial-absent 'wsecho: error'
vgate_assert 16 serial-absent '[EXC] parking:'
vgate_assert 16 python <<'PY'
import os, sys
tag = os.environ["VG_TAG"]
it = int(tag.rsplit("-", 1)[-1]) if "-" in tag else 1
log = open(os.path.join(os.environ["RUN_DIR"], "wsd-26678.log")).read()
for want in ["poison opcode", "poison masked", "poison oversize",
             "poison badaccept", "poison extension"]:
    if want not in log:
        sys.exit("fixture never served %r\n%s" % (want, log))
served = sum(1 for l in log.splitlines() if "poison" in l)
if served < 5 * it:
    sys.exit("iteration %d: only %d poisons served\n%s" % (it, served, log))
PY
