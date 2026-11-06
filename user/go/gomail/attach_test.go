package main

// attach_test.go — the class-A half of M208b: the name sanitizer's
// hostile inputs, the base64 decode, the ATTACH index codec + bounds,
// the save path over a scripted WireConn, the opens= route's three
// verdicts (calendar -> GOCLOCK, vcard -> GOFILES, unrouted -> named
// refusal) plus the unknown-name refusal, and the compose pane's
// editing + submit contract — all over fakes, no syscall.

import (
	"errors"
	"strings"
	"testing"
)

// --- sanitizer ----------------------------------------------------------

func TestSanitizeAttachName(t *testing.T) {
	cases := []struct{ in, want string }{
		{"invite.ics", "invite.ics"},
		{"report.pdf", "report.pdf"},
		{"../../etc/passwd", "passwd"},
		{"..\\..\\evil.exe", "evil.exe"},
		{"a/b/c.txt", "c.txt"},
		{"evil\x1b[2J.ics", "evil__2J.ics"},
		{".hidden", "hidden"},
		{"..", "attach"},
		{"", "attach"},
		{"   ", "attach"},
		{"name with spaces.txt", "name_with_spaces.txt"},
	}
	for _, c := range cases {
		if got := sanitizeAttachName(c.in); got != c.want {
			t.Fatalf("sanitize %q = %q want %q", c.in, got, c.want)
		}
	}
	long := strings.Repeat("a", 80) + ".ics"
	if got := sanitizeAttachName(long); len(got) != attachNameMax {
		t.Fatalf("bound len=%d want %d", len(got), attachNameMax)
	}
}

// --- base64 ---------------------------------------------------------------

func TestB64Decode(t *testing.T) {
	src := "BEGIN:VCALENDAR\r\nBEGIN:VEVENT\r\nSUMMARY:x\r\nEND:VEVENT\r\nEND:VCALENDAR\r\n"
	got, ok := b64decode([]byte(b64encode([]byte(src))))
	if !ok || string(got) != src {
		t.Fatalf("roundtrip ok=%v got=%q", ok, got)
	}
	// MIME folds: whitespace inside the stream decodes through.
	got, ok = b64decode([]byte("QkVHSU46VkNB\r\nTEVOREFS"))
	if !ok || string(got) != "BEGIN:VCALENDAR" {
		t.Fatalf("folded ok=%v got=%q", ok, got)
	}
	if _, ok := b64decode([]byte("!!!!")); ok {
		t.Fatal("bad alphabet decoded")
	}
	if _, ok := b64decode([]byte("QQ")); !ok {
		t.Fatal("2-sextet tail refused")
	}
}

// --- index codec + naming -------------------------------------------------

func TestAttachIndexAndDedupe(t *testing.T) {
	f := swapCache(t)
	rows := []attachRow{
		{box: "Attach", seq: 1, saved: "invite.ics", mt: "text/calendar", orig: "invite.ics"},
	}
	attachIndexWrite(rows)
	got := attachIndex()
	if len(got) != 1 || got[0].saved != "invite.ics" || got[0].seq != 1 {
		t.Fatalf("index %+v", got)
	}
	if r, ok := attachFind("invite.ics"); !ok || r.mt != "text/calendar" {
		t.Fatalf("find %+v", r)
	}
	// A second message's same-named attachment splits, never overwrites.
	if n := attachSaveName(got, "Attach", 2, "invite.ics", "text/calendar"); n != "invite-2.ics" {
		t.Fatalf("dedupe name %q", n)
	}
	// The routed subtype's extension is ensured for the hand-off doors.
	if n := attachSaveName(nil, "A", 9, "meeting", "text/calendar"); n != "meeting.ics" {
		t.Fatalf("ext name %q", n)
	}
	if n := attachSaveName(nil, "A", 9, "card", "text/vcard"); n != "card.vcf" {
		t.Fatalf("vcf ext name %q", n)
	}
	// The bound evicts the oldest rows first.
	var many []attachRow
	for i := 0; i < attachIndexMax+5; i++ {
		many = append(many, attachRow{box: "B", seq: i + 1, saved: "s", mt: "x", orig: "o"})
	}
	attachIndexWrite(many)
	if n := len(attachIndex()); n != attachIndexMax {
		t.Fatalf("index cap %d", n)
	}
	_ = f
}

// --- the scripted save ----------------------------------------------------

// fakeIMAPForPart returns an imapClient whose conn answers one FETCH
// BODY[section] with the literal payload.
func fakeIMAPForPart(seq int, section, payload string) (*imapClient, *fakeConn) {
	script := "* " + itoa(int64(seq)) + " FETCH (BODY[" + section + "] {" +
		itoa(int64(len(payload))) + "}\r\n" + payload + ")\r\n" +
		"a1 OK Fetch completed\r\n"
	fc := &fakeConn{reads: []byte(script)}
	return newIMAP(fc), fc
}

func TestSaveAttachDecodesAndIndexes(t *testing.T) {
	f := swapCache(t)
	raw := "BEGIN:VCALENDAR\r\nEND:VCALENDAR\r\n"
	c, fc := fakeIMAPForPart(1, "2", b64encode([]byte(raw)))
	a := &partInfo{num: "2", media: "text", sub: "calendar",
		filename: "invite.ics", encoding: "base64"}
	saveAttach(c, "Attach", 1, a)
	if got := string(f.files["/host/MAIL/invite.ics"]); got != raw {
		t.Fatalf("saved bytes %q", got)
	}
	if w := allWrites(fc); !strings.Contains(w, "FETCH 1 (BODY[2])") {
		t.Fatalf("wire %q", w)
	}
	rows := attachIndex()
	if len(rows) != 1 || rows[0].mt != "text/calendar" || rows[0].box != "Attach" {
		t.Fatalf("index %+v", rows)
	}
	// A hostile declared name sanitizes on the way down — the payload
	// lands under MAIL/ as a basename, never a path.
	c2, _ := fakeIMAPForPart(2, "2", "x")
	a2 := &partInfo{num: "2", media: "application", sub: "octet-stream",
		filename: "../evil/../payload.bin", encoding: "7bit"}
	saveAttach(c2, "Attach", 2, a2)
	if got := string(f.files["/host/MAIL/payload.bin"]); got != "x" {
		t.Fatalf("hostile save %q", got)
	}
	if _, ok := f.files["/host/MAIL/../evil/../payload.bin"]; ok {
		t.Fatal("hostile path wrote")
	}
}

// --- the opens= route -----------------------------------------------------

// The row order is the REAL manifest's (image/apps.txt): NOTE's bare
// opens=text row precedes the subtype rows, exactly where it claimed
// text/calendar and text/vcard on the gate before the route preferred
// the exact subtype spelling (#3858).
const testAppsTxt = `# test manifest
NOTE.ELF | Text Editor | n | dock=true | v=2 | opens=text
GOFILES.ELF | Files | f | dock=true | v=2 | opens=text/vcard
GOCLOCK.ELF | Clock | k | dock=false | v=2 | opens=text/calendar
`

// swapAttachSeams installs a fake manifest read and a recording exec.
func swapAttachSeams(t *testing.T, apps string) *[]string {
	t.Helper()
	oldRead, oldExec := attAppsRead, attExec
	attAppsRead = func(path string, max int) ([]byte, int64) {
		if apps == "" {
			return nil, -1
		}
		return []byte(apps), 0
	}
	var calls []string
	attExec = func(name string, args ...string) (int64, error) {
		calls = append(calls, name+" "+strings.Join(args, " "))
		return 42, nil
	}
	t.Cleanup(func() { attAppsRead, attExec = oldRead, oldExec })
	return &calls
}

func seedAttachIndex(f *fakeCacheHost) {
	f.put("ATTACH", "Attach\t1\tinvite.ics\ttext/calendar\tinvite.ics\n"+
		"Attach\t2\tcard.vcf\ttext/vcard\tcard.vcf\n"+
		"Attach\t3\tpayload.bin\tapplication/octet-stream\tpayload.bin\n")
}

func TestOpenAttachRoutes(t *testing.T) {
	f := swapCache(t)
	seedAttachIndex(f)
	calls := swapAttachSeams(t, testAppsTxt)

	// text/calendar hands the saved path to GOCLOCK — the manifest's
	// opens= answer, exec'd in the contract's own argv shape.
	if err := openAttach("invite.ics"); err != nil {
		t.Fatalf("ics open %v", err)
	}
	if got := (*calls)[0]; got != "GOCLOCK.ELF /host/MAIL/invite.ics" {
		t.Fatalf("ics exec %q", got)
	}
	// text/vcard -> GOFILES' contact face.
	if err := openAttach("card.vcf"); err != nil {
		t.Fatalf("vcf open %v", err)
	}
	if got := (*calls)[1]; got != "GOFILES.ELF /host/MAIL/card.vcf" {
		t.Fatalf("vcf exec %q", got)
	}
	// application/octet-stream has no acceptor — saved, refused by type,
	// never exec'd.
	err := openAttach("payload.bin")
	if err == nil || err.Error() != "gomail: refuse open payload.bin application/octet-stream" {
		t.Fatalf("bin open %v", err)
	}
	if len(*calls) != 2 {
		t.Fatalf("routed %d execs", len(*calls))
	}
	// An unknown name is its own named refusal.
	if err := openAttach("ghost.zip"); err == nil ||
		err.Error() != "gomail: refuse open ghost.zip" {
		t.Fatalf("ghost %v", err)
	}
}

// A type with no exact acceptor still falls back to the first
// bare-family acceptor in manifest order — the subtype preference narrows
// the dispatch, it never removes a family route.
func TestOpenAttachFamilyFallback(t *testing.T) {
	f := swapCache(t)
	f.put("ATTACH", "Attach\t1\tnotes.txt\ttext/plain\tnotes.txt\n")
	calls := swapAttachSeams(t, testAppsTxt)
	if err := openAttach("notes.txt"); err != nil {
		t.Fatalf("txt open %v", err)
	}
	if got := (*calls)[0]; got != "NOTE.ELF /host/MAIL/notes.txt" {
		t.Fatalf("txt exec %q", got)
	}
}

func TestOpenAttachNoManifestAndExecRefusal(t *testing.T) {
	f := swapCache(t)
	seedAttachIndex(f)
	// An unreadable manifest is an empty catalog: the routed types now
	// refuse by name rather than guessing a handler.
	swapAttachSeams(t, "")
	if err := openAttach("invite.ics"); err == nil ||
		err.Error() != "gomail: refuse open invite.ics text/calendar" {
		t.Fatalf("no manifest %v", err)
	}
	// The exec's own refusal is named, never swallowed.
	old := attExec
	attExec = func(name string, args ...string) (int64, error) {
		return 0, errors.New("ENOENT")
	}
	t.Cleanup(func() { attExec = old })
	attAppsRead = func(path string, max int) ([]byte, int64) {
		return []byte(testAppsTxt), 0
	}
	if err := openAttach("invite.ics"); err == nil ||
		err.Error() != "gomail: refuse open invite.ics ENOENT" {
		t.Fatalf("exec refusal %v", err)
	}
}

// TestOpenAttachAppsTxtParse pins the manifest read: a v1 row's opens= is
// ignored (the additive-tail rule), argv= rides ahead of the path, and
// a bare family word accepts its subtypes.
func TestOpenAttachAppsTxtParse(t *testing.T) {
	f := swapCache(t)
	seedAttachIndex(f)
	apps := "GOCLOCK.ELF | Clock | k | dock=false | v=2 | argv=import | opens=text/calendar\n" +
		"OLD.ELF | Legacy | o | dock=false | opens=text/calendar\n" +
		"ANY.ELF | Any | a | dock=false | v=2 | opens=text\n"
	calls := swapAttachSeams(t, apps)
	if err := openAttach("invite.ics"); err != nil {
		t.Fatalf("open %v", err)
	}
	if got := (*calls)[0]; got != "GOCLOCK.ELF import /host/MAIL/invite.ics" {
		t.Fatalf("argv shape %q", got)
	}
	// v1's opens= never routes — OLD.ELF is skipped even though it
	// would match.
	if len(*calls) != 1 {
		t.Fatalf("execs %v", *calls)
	}
	// A bare family word accepts the subtype under it.
	if !opensAccepts([]string{"text"}, "text/calendar") {
		t.Fatal("family word refused subtype")
	}
	if opensAccepts([]string{"text/plain"}, "text/calendar") {
		t.Fatal("subtype spelling over-matched")
	}
	if opensAccepts([]string{"image"}, "text/calendar") {
		t.Fatal("wrong family accepted")
	}
}

// --- the compose pane -----------------------------------------------------

func keyRune(c *composeModel, r byte) compAct {
	// Letters' HID usages don't matter to the pane — only arg1 (the
	// typed byte) is read for printables.
	return c.composeKey(0, uint32(r), false)
}

func TestComposeModelEditAndFields(t *testing.T) {
	c := composeModel{live: true}
	for _, r := range []byte("bob") {
		keyRune(&c, r)
	}
	if string(c.to) != "bob" {
		t.Fatalf("to %q", c.to)
	}
	// Tab walks the field ring; Enter is a newline only inside Body.
	c.composeKey(0x2b, 0, false)
	keyRune(&c, 's')
	c.composeKey(0x28, 0, false) // Enter on Subject -> Body
	if c.field != compFieldBody {
		t.Fatalf("field %d", c.field)
	}
	keyRune(&c, 'x')
	c.composeKey(0x28, 0, false) // Enter in Body -> newline
	keyRune(&c, 'y')
	if string(c.body) != "x\ny" {
		t.Fatalf("body %q", c.body)
	}
	// Backspace trims one rune.
	c.composeKey(0x2a, 0, false)
	if string(c.body) != "x\n" {
		t.Fatalf("bs body %q", c.body)
	}
	// Recipients split on commas, trimmed.
	c.to = []byte("a@x, b@y ,, ")
	got := c.recipients()
	if len(got) != 2 || got[0] != "a@x" || got[1] != "b@y" {
		t.Fatalf("rcpts %v", got)
	}
	// Esc cancels out of the pane.
	if act := c.composeKey(0x29, 0, false); act != compCancel || c.live {
		t.Fatalf("esc act=%v live=%v", act, c.live)
	}
	// Ctrl-S submits.
	c.live = true
	if act := c.composeKey(0x16, 's', true); act != compSend {
		t.Fatalf("ctrl-s %v", act)
	}
	// The pane is bounded.
	c.field, c.to = compFieldTo, nil
	for i := 0; i < compFieldMax+10; i++ {
		keyRune(&c, 'z')
	}
	if len(c.to) != compFieldMax {
		t.Fatalf("bound %d", len(c.to))
	}
}

func TestComposeSendRunsSendPath(t *testing.T) {
	f := swapCache(t)
	seedViewCache(f)
	var got options
	v := newTestView(Config{Host: "h", User: "u"})
	v.sendFn = func(cfg Config, o options) error {
		got = o
		say("gomail: sent bytes=100")
		return nil
	}
	v.comp = composeModel{live: true, to: []byte("bob"), subject: []byte("hi"), body: []byte("line")}
	v.composeSend()
	if v.comp.live || len(got.to) != 1 || got.to[0] != "bob" ||
		got.subject != "hi" || got.body != "line" {
		t.Fatalf("send comp=%+v opts=%+v", v.comp, got)
	}
	if v.status != "gomail: sent bytes=100" {
		t.Fatalf("status %q", v.status)
	}
	// A refusal names itself and the draft stays.
	v.sendFn = func(Config, options) error { return protoErr("refuse plaintext", "") }
	v.comp = composeModel{live: true, to: []byte("bob")}
	v.composeSend()
	if v.status != "gomail: refuse plaintext" || !v.comp.live {
		t.Fatalf("refusal status=%q live=%v", v.status, v.comp.live)
	}
}

func TestViewOpenAttachRing(t *testing.T) {
	f := swapCache(t)
	seedViewCache(f)
	f.put("ATTACH", "Lists\t7\tinvite.ics\ttext/calendar\tinvite.ics\n"+
		"Lists\t7\tcard.vcf\ttext/vcard\tcard.vcf\n")
	calls := swapAttachSeams(t, testAppsTxt)

	v := newTestView(Config{})
	v.load()
	v.pane = paneBoxes
	v.move(1) // Lists, seq 7
	// The index rows paint into the body pane.
	joined := strings.Join(v.body, "\n")
	if !strings.Contains(joined, "attach: invite.ics (text/calendar)") ||
		!strings.Contains(joined, "attach: card.vcf (text/vcard)") {
		t.Fatalf("body %v", v.body)
	}
	v.openAttach()
	if (*calls)[0] != "GOCLOCK.ELF /host/MAIL/invite.ics" {
		t.Fatalf("first open %q", (*calls)[0])
	}
	v.openAttach()
	if (*calls)[1] != "GOFILES.ELF /host/MAIL/card.vcf" {
		t.Fatalf("ring open %q", (*calls)[1])
	}
	// A message with no saved rows names the miss.
	v.pane = paneBoxes
	v.move(-1) // INBOX — no envelope rows here, so no message either
	v.openAttach()
	if v.status != "gomail: no message" {
		t.Fatalf("status %q", v.status)
	}
}
