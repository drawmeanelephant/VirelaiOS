package main

// imap_test.go — the class-A protocol-state half: recorded IMAP sessions
// driven through a scripted WireConn, asserting both what the client
// parsed and the exact bytes it wrote.

import (
	"errors"
	"strings"
	"testing"
)

var errFakeEOF = errors.New("fake: peer gone")

// fakeConn serves a scripted read stream and records writes verbatim.
// chunk > 0 drips the stream that many bytes per Read, and every armed
// deadline lands in deadlines so tests can prove one arm per command.
type fakeConn struct {
	reads     []byte
	writes    []string
	closed    bool
	deadline  int64
	deadlines []int64
	chunk     int
}

func (f *fakeConn) Read(p []byte) (int, error) {
	if len(f.reads) == 0 {
		return 0, errFakeEOF
	}
	if f.chunk > 0 && len(p) > f.chunk {
		p = p[:f.chunk]
	}
	n := copy(p, f.reads)
	f.reads = f.reads[n:]
	return n, nil
}

func (f *fakeConn) Write(p []byte) (int, error) {
	f.writes = append(f.writes, string(p))
	return len(p), nil
}

func (f *fakeConn) SetReadDeadline(at int64) {
	f.deadline = at
	f.deadlines = append(f.deadlines, at)
}
func (f *fakeConn) Close() error { f.closed = true; return nil }

func allWrites(f *fakeConn) string { return strings.Join(f.writes, "") }

// imapScript is the recorded session the fixture server also speaks:
// greeting, LOGIN, LIST with two boxes, SELECT INBOX (3 exists), then
// per-seq FETCH ENVELOPE / BODYSTRUCTURE / BODY[...] rows.
func imapScript() string {
	var b strings.Builder
	b.WriteString("* OK fixture IMAP4rev1 ready\r\n")
	// a1 LOGIN
	b.WriteString("a1 OK Login completed\r\n")
	// a2 LIST
	b.WriteString("* LIST (\\HasNoChildren) \"/\" \"INBOX\"\r\n")
	b.WriteString("* LIST (\\HasNoChildren) \"/\" Sent\r\n")
	b.WriteString("a2 OK List completed\r\n")
	// a3 SELECT
	b.WriteString("* 3 EXISTS\r\n")
	b.WriteString("* 1 RECENT\r\n")
	b.WriteString("* FLAGS (\\Seen \\Answered)\r\n")
	b.WriteString("* OK [UIDVALIDITY 7] UIDs valid\r\n")
	b.WriteString("a3 OK [READ-WRITE] Select completed\r\n")
	// a4 FETCH 1:3 (ENVELOPE)
	b.WriteString("* 1 FETCH (ENVELOPE (\"Wed, 21 Oct 2026 10:00:00 +0000\" \"gate hello\" " +
		"((\"Alice\" NIL \"alice\" \"fixture.local\")) NIL NIL NIL NIL NIL NIL \"<m1@fixture>\"))\r\n")
	b.WriteString("* 2 FETCH (ENVELOPE (\"Wed, 21 Oct 2026 10:05:00 +0000\" \"html issue\" " +
		"((NIL NIL \"bob\" \"fixture.local\")) NIL NIL NIL NIL NIL NIL \"<m2@fixture>\"))\r\n")
	b.WriteString("* 3 FETCH (ENVELOPE (\"Wed, 21 Oct 2026 10:10:00 +0000\" \"report attached\" " +
		"((\"Carol\" NIL \"carol\" \"fixture.local\")) NIL NIL NIL NIL NIL NIL \"<m3@fixture>\"))\r\n")
	b.WriteString("a4 OK Fetch completed\r\n")
	// a5 FETCH 1 (BODYSTRUCTURE)
	b.WriteString("* 1 FETCH (BODYSTRUCTURE (\"TEXT\" \"PLAIN\" (\"CHARSET\" \"US-ASCII\") NIL NIL \"7BIT\" 52 3))\r\n")
	b.WriteString("a5 OK Fetch completed\r\n")
	// a6 FETCH 1 (BODY[TEXT]) — literal
	body1 := "line one of the gate body\r\nsecond line\r\nthird line"
	b.WriteString("* 1 FETCH (BODY[TEXT] {" + itoa(int64(len(body1))) + "}\r\n" + body1 + ")\r\n")
	b.WriteString("a6 OK Fetch completed\r\n")
	return b.String()
}

func newScriptedIMAP(script string) (*imapClient, *fakeConn) {
	fc := &fakeConn{reads: []byte(script)}
	c := newIMAP(fc)
	c.now = func() int64 { return 0 }
	return c, fc
}

func TestIMAPRecordedSession(t *testing.T) {
	c, fc := newScriptedIMAP(imapScript())

	if err := c.greeting(); err != nil {
		t.Fatalf("greeting: %v", err)
	}
	if err := c.login("gate", "gate-pass"); err != nil {
		t.Fatalf("login: %v", err)
	}
	boxes, err := c.list()
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(boxes) != 2 || boxes[0] != "INBOX" || boxes[1] != "Sent" {
		t.Fatalf("boxes %v", boxes)
	}
	exists, err := c.selectBox("INBOX")
	if err != nil || exists != 3 {
		t.Fatalf("select exists=%d err=%v", exists, err)
	}
	hdrs, err := c.envelopes(1, 3)
	if err != nil {
		t.Fatalf("envelopes: %v", err)
	}
	if len(hdrs) != 3 {
		t.Fatalf("hdrs %d", len(hdrs))
	}
	if hdrs[0].Seq != 1 || hdrs[0].From != "Alice" || hdrs[0].Subject != "gate hello" {
		t.Fatalf("hdr[0] %+v", hdrs[0])
	}
	if hdrs[1].From != "bob@fixture.local" || hdrs[1].Subject != "html issue" {
		t.Fatalf("hdr[1] %+v", hdrs[1])
	}
	root, err := c.structure(1)
	if err != nil {
		t.Fatalf("structure: %v", err)
	}
	if root.media != "text" || root.sub != "plain" {
		t.Fatalf("structure %+v", root)
	}
	body, err := c.section(1, "TEXT")
	if err != nil {
		t.Fatalf("section: %v", err)
	}
	if !strings.Contains(string(body), "second line") {
		t.Fatalf("body %q", body)
	}

	// The wire half: exact command bytes the transcript answered.
	w := allWrites(fc)
	for _, want := range []string{
		"a1 LOGIN \"gate\" \"gate-pass\"\r\n",
		"a2 LIST \"\" \"*\"\r\n",
		"a3 SELECT \"INBOX\"\r\n",
		"a4 FETCH 1:3 (ENVELOPE)\r\n",
		"a5 FETCH 1 (BODYSTRUCTURE)\r\n",
		"a6 FETCH 1 (BODY[TEXT])\r\n",
	} {
		if !strings.Contains(w, want) {
			t.Fatalf("write %q missing from %q", want, w)
		}
	}
}

func TestIMAPRefusals(t *testing.T) {
	// A NO to LOGIN is a named refusal carrying the server's text.
	c, _ := newScriptedIMAP("* OK ready\r\n" + "a1 NO bad credentials\r\n")
	if err := c.greeting(); err != nil {
		t.Fatalf("greeting: %v", err)
	}
	err := c.login("gate", "wrong")
	if err == nil || !strings.Contains(err.Error(), "refused") ||
		!strings.Contains(err.Error(), "bad credentials") {
		t.Fatalf("login refusal %v", err)
	}

	// A BYE banner refuses the session.
	c2, _ := newScriptedIMAP("* BYE go away\r\n")
	if err := c2.greeting(); err == nil || !strings.Contains(err.Error(), "refused") {
		t.Fatalf("bye greeting %v", err)
	}

	// Literal split across reads must reassemble — feed it byte-dribbled.
	lit := "ab"
	script := "* OK x\r\n* 9 FETCH (BODY[TEXT] {2}\r\n" + lit + ")\r\na1 OK done\r\n"
	c3, _ := newScriptedIMAP(script)
	if err := c3.greeting(); err != nil {
		t.Fatalf("greeting3: %v", err)
	}
	body, err := c3.section(9, "TEXT")
	if err != nil || string(body) != "ab" {
		t.Fatalf("section3 %q %v", body, err)
	}
}

func TestIMAPDeadlinePerCommand(t *testing.T) {
	// Byte-drip service forces many fills inside one exchange; the
	// deadline is armed ONCE per greeting()/command() — a drip cannot
	// re-arm it per read (#3396).
	script := "* OK x\r\n* 1 EXISTS\r\na1 OK done\r\n"
	fc := &fakeConn{reads: []byte(script), chunk: 1}
	c := newIMAP(fc)
	tick := int64(0)
	c.now = func() int64 { tick += 1e9; return tick }
	if err := c.greeting(); err != nil {
		t.Fatalf("greeting: %v", err)
	}
	if _, _, err := c.command("NOOP"); err != nil {
		t.Fatalf("command: %v", err)
	}
	if len(fc.deadlines) != 2 {
		t.Fatalf("deadlines %v, want exactly one arm per greeting+command", fc.deadlines)
	}
	// Each armed value is fixed at its unit's start: the drip read
	// path never pushes a later deadline onto the conn.
	if fc.deadlines[0] != 1e9+cmdWaitNs || fc.deadlines[1] != 2e9+cmdWaitNs {
		t.Fatalf("armed deadlines %v", fc.deadlines)
	}
}

func TestIMAPUntaggedByteCap(t *testing.T) {
	// maxUntagged bounds record COUNT; untaggedBytesMax bounds the
	// aggregate bytes — a few huge records must trip it well before
	// the count cap (#3396: 512 x 512 KiB was ~256 MiB per command).
	var b strings.Builder
	b.WriteString("* OK x\r\n")
	lit := strings.Repeat("x", 200000)
	for i := 0; i < 12; i++ {
		b.WriteString("* 1 FETCH (BODY[TEXT] {200000}\r\n" + lit + ")\r\n")
	}
	b.WriteString("a1 OK done\r\n")
	fc := &fakeConn{reads: []byte(b.String())}
	c := newIMAP(fc)
	c.now = func() int64 { return 0 }
	if err := c.greeting(); err != nil {
		t.Fatalf("greeting: %v", err)
	}
	_, _, err := c.command("FETCH 1 (BODY[TEXT])")
	if err == nil || !strings.Contains(err.Error(), "untagged aggregate") {
		t.Fatalf("byte cap: %v", err)
	}
}

func TestLiteralTail(t *testing.T) {
	if n, ok := literalTail([]byte("x {52}")); !ok || n != 52 {
		t.Fatalf("literalTail {52}: %d %v", n, ok)
	}
	if _, ok := literalTail([]byte("x {52} y")); ok {
		t.Fatal("literalTail accepted trailing text")
	}
	if _, ok := literalTail([]byte("{}")); ok {
		t.Fatal("literalTail accepted empty digits")
	}
}
