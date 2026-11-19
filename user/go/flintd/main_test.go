package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"virelai/ed25519"
	"virelai/flintstore"
	"virelai/sshlib"
)

// testSeed is RFC 8032 §7.1 TEST 1 (the same fixture FLINT's CLI tests pin).
func testSeed(t *testing.T) [32]byte {
	t.Helper()
	b, ok := sshlib.ParseHex("9d61b19deffd5a60ba844af492ec2cc44449c5697b326919703bac031cae7f60")
	if !ok {
		t.Fatal("bad seed")
	}
	var seed [32]byte
	copy(seed[:], b)
	return seed
}

func signedChange(t *testing.T, seed [32]byte, c flintstore.Change) flintstore.Change {
	t.Helper()
	pub := ed25519.EdDerivePublic(seed)
	c.Author = sshlib.EncodeHex(pub[:])
	msg, err := flintstore.SigMsg(&c)
	if err != nil {
		t.Fatal(err)
	}
	sig := ed25519.EdSign(msg, seed)
	c.Sig = sshlib.EncodeHex(sig[:])
	return c
}

type memStore struct {
	fs    *flintstore.MemFS
	root  string
	lines [][]byte
}

// seedStore builds a valid ADR 0049 store with n signed linear changes.
func seedStore(t *testing.T, n int) *memStore {
	t.Helper()
	fs := flintstore.NewMemFS()
	root := "/host/FLINT"
	streamID := "0199de2d-dc00-7000-8000-00000000a010"
	if err := fs.Mkdir(root); err != nil {
		t.Fatal(err)
	}
	if err := fs.Mkdir(flintstore.StreamsDir(root)); err != nil {
		t.Fatal(err)
	}
	seed := testSeed(t)
	var parent *string
	var changes []flintstore.Change
	var lines [][]byte
	for i := 1; i <= n; i++ {
		id := fmt.Sprintf("0199de2d-dc00-7000-8000-%012d", i)
		c := signedChange(t, seed, flintstore.Change{
			ID:      id,
			Parent:  parent,
			Stream:  streamID,
			TS:      int64(1760000000000 + i),
			Message: fmt.Sprintf("pinned change %03d %s", i, strings.Repeat("x", 96)),
			Ops:     []flintstore.Op{},
		})
		changes = append(changes, c)
		line, err := flintstore.MarshalChangeLine(c)
		if err != nil {
			t.Fatal(err)
		}
		lines = append(lines, bytes.TrimSuffix(line, []byte{'\n'}))
		parent = &changes[len(changes)-1].ID
	}
	head := parent
	st := &flintstore.Stream{ID: streamID, Name: "seed", Head: head, Created: 1760000000000}
	if err := flintstore.WriteStreamRecord(fs, root, st); err != nil {
		t.Fatal(err)
	}
	var log bytes.Buffer
	for _, line := range lines {
		log.Write(line)
		log.WriteByte('\n')
	}
	if err := fs.WriteFileSafe(flintstore.StreamLogPath(root, streamID), log.Bytes()); err != nil {
		t.Fatal(err)
	}
	old := storeFS
	storeFS = fs
	t.Cleanup(func() { storeFS = old })
	return &memStore{fs: fs, root: root, lines: lines}
}

type scriptConn struct {
	chunks [][]byte
	out    bytes.Buffer
}

func (c *scriptConn) Recv(p []byte) (int, error) {
	if len(c.chunks) == 0 {
		return 0, io.EOF
	}
	chunk := c.chunks[0]
	c.chunks = c.chunks[1:]
	if len(chunk) > len(p) {
		c.chunks = append([][]byte{chunk[len(p):]}, c.chunks...)
		chunk = chunk[:len(p)]
	}
	copy(p, chunk)
	return len(chunk), nil
}

func (c *scriptConn) Send(p []byte) (int, error) {
	c.out.Write(p)
	return len(p), nil
}

func (c *scriptConn) responses(t *testing.T) []map[string]json.RawMessage {
	t.Helper()
	var out []map[string]json.RawMessage
	for _, line := range strings.Split(strings.TrimSuffix(c.out.String(), "\n"), "\n") {
		if line == "" {
			continue
		}
		var obj map[string]json.RawMessage
		if err := json.Unmarshal([]byte(line), &obj); err != nil {
			t.Fatalf("response line is not JSON: %q: %v", line, err)
		}
		out = append(out, obj)
	}
	return out
}

func strField(t *testing.T, obj map[string]json.RawMessage, name string) string {
	t.Helper()
	var s string
	if err := json.Unmarshal(obj[name], &s); err != nil {
		t.Fatalf("field %s: %v", name, err)
	}
	return s
}

func boolField(t *testing.T, obj map[string]json.RawMessage, name string) bool {
	t.Helper()
	var b bool
	if err := json.Unmarshal(obj[name], &b); err != nil {
		t.Fatalf("field %s: %v", name, err)
	}
	return b
}

// TestHelloListHeadsGolden replays the pinned host-server transcript against
// the guest dispatcher and compares every response byte-for-byte.
func TestHelloListHeadsGolden(t *testing.T) {
	// A store root with an empty streams/ directory.
	fs := flintstore.NewMemFS()
	root := "/host/FLINT"
	if err := fs.Mkdir(flintstore.StreamsDir(root)); err != nil {
		t.Fatal(err)
	}
	old := storeFS
	storeFS = fs
	defer func() { storeFS = old }()

	raw, err := os.ReadFile(filepath.Join("..", "flint", "testdata", "sync", "hello_list.ndjson"))
	if err != nil {
		t.Fatal(err)
	}
	var requests, responses []string
	for _, line := range strings.Split(string(raw), "\n") {
		switch {
		case strings.HasPrefix(line, "> "):
			requests = append(requests, strings.TrimPrefix(line, "> "))
		case strings.HasPrefix(line, "< "):
			responses = append(responses, strings.TrimPrefix(line, "< "))
		}
	}
	conn := &scriptConn{chunks: [][]byte{[]byte(strings.Join(requests, "\n") + "\n")}}
	serveSession(conn, root, nil)
	want := strings.Join(responses, "\n") + "\n"
	if conn.out.String() != want {
		t.Fatalf("golden transcript mismatch:\n got: %q\nwant: %q", conn.out.String(), want)
	}
}

// TestReadRawBytesAndPagination proves a seeded store is served as the
// original raw Change line bytes (never a re-serialized Change) and that the
// default 256 / maximum-1024 pagination and `more` flag are exact.
func TestReadRawBytesAndPagination(t *testing.T) {
	st := seedStore(t, 300)
	streamID := "0199de2d-dc00-7000-8000-00000000a010"

	read := func(request string) map[string]json.RawMessage {
		conn := &scriptConn{chunks: [][]byte{[]byte(request + "\n")}}
		serveSession(conn, st.root, nil)
		resps := conn.responses(t)
		if len(resps) != 1 {
			t.Fatalf("want 1 response, got %d", len(resps))
		}
		return resps[0]
	}

	page := read(`{"op":"read","seq":41,"stream":"` + streamID + `"}`)
	if !boolField(t, page, "ok") {
		t.Fatalf("read refused: %s", page["refuse"])
	}
	var changes []string
	if err := json.Unmarshal(page["changes"], &changes); err != nil {
		t.Fatal(err)
	}
	if len(changes) != flintstore.DefaultReadLimit {
		t.Fatalf("default page = %d changes, want %d", len(changes), flintstore.DefaultReadLimit)
	}
	if !boolField(t, page, "more") {
		t.Fatal("expected more=true on the first default page")
	}
	for i, got := range changes {
		if got != string(st.lines[i]) {
			t.Fatalf("change %d raw bytes mismatch:\n got: %s\nwant: %s", i, got, st.lines[i])
		}
	}
	var seq uint64
	if err := json.Unmarshal(page["seq"], &seq); err != nil || seq != 41 {
		t.Fatalf("seq echo = %v (%v), want 41", seq, err)
	}
	var head string
	if err := json.Unmarshal(page["head"], &head); err != nil || head != fmt.Sprintf("0199de2d-dc00-7000-8000-%012d", 300) {
		t.Fatalf("head = %q, want last change", head)
	}

	rest := read(`{"op":"read","seq":42,"stream":"` + streamID + `","after":"0199de2d-dc00-7000-8000-` +
		fmt.Sprintf("%012d", 256) + `","limit":1024}`)
	if err := json.Unmarshal(rest["changes"], &changes); err != nil {
		t.Fatal(err)
	}
	if len(changes) != 44 || boolField(t, rest, "more") {
		t.Fatalf("after-256 page = %d changes more=%v, want 44/false", len(changes), boolField(t, rest, "more"))
	}
	if changes[0] != string(st.lines[256]) {
		t.Fatalf("after-256 first raw line mismatch")
	}

	// A limit above the maximum clamps to 1024 rather than refusing.
	clamped := read(`{"op":"read","seq":43,"stream":"` + streamID + `","limit":5000}`)
	if err := json.Unmarshal(clamped["changes"], &changes); err != nil {
		t.Fatal(err)
	}
	if len(changes) != 300 || boolField(t, clamped, "more") {
		t.Fatalf("clamped page = %d more=%v, want 300/false", len(changes), boolField(t, clamped, "more"))
	}
}

// TestFramingFragmentedCoalescedLarge exercises partial-line accumulation,
// two requests coalesced into one receive, and requests/responses larger than
// one 1460-byte TCP segment.
func TestFramingFragmentedCoalescedLarge(t *testing.T) {
	st := seedStore(t, 300)
	streamID := "0199de2d-dc00-7000-8000-00000000a010"

	hello := `{"op":"hello","seq":1,"proto":"FLINT-SYNC/1"}`
	list := `{"op":"list","seq":2}`
	read := `{"op":"read","seq":3,"stream":"` + streamID + `"}`
	// Split hello mid-line, coalesce its tail with hello's newline + list, and
	// split the read request into 7-byte fragments.
	var chunks [][]byte
	chunks = append(chunks, []byte(hello[:9]))
	chunks = append(chunks, []byte(hello[9:]+"\n"+list+"\n"))
	for i := 0; i < len(read); i += 7 {
		end := i + 7
		if end > len(read) {
			end = len(read)
		}
		chunks = append(chunks, []byte(read[i:end]))
	}
	chunks = append(chunks, []byte("\n"))

	conn := &scriptConn{chunks: chunks}
	serveSession(conn, st.root, nil)
	resps := conn.responses(t)
	if len(resps) != 3 {
		t.Fatalf("want 3 responses, got %d: %q", len(resps), conn.out.String())
	}
	for i, want := range []uint64{1, 2, 3} {
		var seq uint64
		if err := json.Unmarshal(resps[i]["seq"], &seq); err != nil || seq != want {
			t.Fatalf("response %d seq=%d want %d", i, seq, want)
		}
		if !boolField(t, resps[i], "ok") {
			t.Fatalf("response %d refused: %s", i, resps[i]["refuse"])
		}
	}
	if len(conn.out.String()) <= 1460 {
		t.Fatalf("read response did not exceed one TCP segment: %d bytes", len(conn.out.String()))
	}
	var pageChanges []string
	if err := json.Unmarshal(resps[2]["changes"], &pageChanges); err != nil {
		t.Fatal(err)
	}
	if len(pageChanges) == 0 || pageChanges[0] != string(st.lines[0]) {
		t.Fatal("large response lost the first raw Change line")
	}
	// Every emitted response line is complete and newline-terminated.
	if !bytes.HasSuffix(conn.out.Bytes(), []byte("\n")) {
		t.Fatal("response stream did not end on a line boundary")
	}

	// CRLF framing is accepted and stripped once.
	crlf := &scriptConn{chunks: [][]byte{[]byte(hello + "\r\n")}}
	serveSession(crlf, st.root, nil)
	if got := crlf.responses(t); len(got) != 1 || !boolField(t, got[0], "ok") {
		t.Fatalf("CRLF hello: %q", crlf.out.String())
	}
}

// TestReadRefusals covers the named refusals the read half owes.
func TestReadRefusals(t *testing.T) {
	st := seedStore(t, 3)
	streamID := "0199de2d-dc00-7000-8000-00000000a010"
	cases := []struct {
		name string
		line string
		code string
	}{
		{"missing stream", `{"op":"read","seq":1}`, "usage"},
		{"empty stream", `{"op":"read","seq":1,"stream":""}`, "usage"},
		{"unknown stream", `{"op":"read","seq":1,"stream":"0199de2d-dc00-7000-8000-00000000ffff"}`, "stream_missing"},
		{"unknown after", `{"op":"read","seq":1,"stream":"` + streamID + `","after":"0199de2d-dc00-7000-8000-00000000ffff"}`, "change_missing"},
		{"bad limit", `{"op":"read","seq":1,"stream":"` + streamID + `","limit":0}`, "usage"},
		{"bad after type", `{"op":"read","seq":1,"stream":"` + streamID + `","after":7}`, "usage"},
		{"proto mismatch", `{"op":"hello","seq":1,"proto":"FLINT-SYNC/2"}`, "proto_version"},
		{"missing proto", `{"op":"hello","seq":1}`, "proto_version"},
		{"missing seq", `{"op":"list"}`, "usage"},
		{"negative seq", `{"op":"list","seq":-1}`, "usage"},
		{"malformed json", `{"op":`, "usage"},
		{"unknown op", `{"op":"frobnicate","seq":1}`, "usage"},
		{"open missing", `{"op":"open","seq":1,"stream":"nope"}`, "stream_missing"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			resp, closeAfter := handleLine([]byte(tc.line), st.root, nil)
			if closeAfter {
				t.Fatal("ordinary refusal must not close the session")
			}
			var obj map[string]json.RawMessage
			if err := json.Unmarshal(resp, &obj); err != nil {
				t.Fatalf("refusal is not JSON: %s", resp)
			}
			if boolField(t, obj, "ok") {
				t.Fatalf("want refusal, got ok: %s", resp)
			}
			if got := strField(t, obj, "refuse"); got != tc.code {
				t.Fatalf("refuse = %q, want %q (%s)", got, tc.code, resp)
			}
		})
	}

	// An over-4-MiB request line answers store_too_large with seq 0 and closes.
	big := bytes.Repeat([]byte{'x'}, flintstore.MaxLineBytes+1)
	resp, closeAfter := handleLine(big, st.root, nil)
	if !closeAfter {
		t.Fatal("over-bound line must close the session")
	}
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(resp, &obj); err != nil {
		t.Fatal(err)
	}
	if got := strField(t, obj, "refuse"); got != "store_too_large" {
		t.Fatalf("refuse = %q, want store_too_large", got)
	}
	var seq uint64
	if err := json.Unmarshal(obj["seq"], &seq); err != nil || seq != 0 {
		t.Fatalf("over-bound seq = %v, want 0", seq)
	}

	// serveSession enforces the same bound while accumulating without a newline.
	conn := &scriptConn{chunks: [][]byte{big}}
	serveSession(conn, st.root, nil)
	if !bytes.Contains(conn.out.Bytes(), []byte(`"store_too_large"`)) {
		t.Fatalf("accumulation bound did not refuse: %q", conn.out.String())
	}
}

// TestWriteAndAuthRefuseHonestly proves this milestone's read-only posture:
// create/append/auth refuse by name and no store byte changes.
func TestWriteAndAuthRefuseHonestly(t *testing.T) {
	st := seedStore(t, 1)
	before := snapshot(t, st.fs)
	for _, line := range []string{
		`{"op":"create","seq":1,"record":{"id":"0199de2d-dc00-7000-8000-00000000f010","name":"new","head":null,"created":1,"forked_from":null}}`,
		`{"op":"append","seq":2,"stream":"0199de2d-dc00-7000-8000-00000000a010","expected_head":null,"changes":[]}`,
		`{"op":"auth","seq":3,"nonce":"000102030405060708090a0b0c0d0e0f","hmac":"00"}`,
	} {
		resp, closeAfter := handleLine([]byte(line), st.root, nil)
		if closeAfter {
			t.Fatalf("write/auth refusal closed the session: %s", resp)
		}
		var obj map[string]json.RawMessage
		if err := json.Unmarshal(resp, &obj); err != nil {
			t.Fatal(err)
		}
		if got := strField(t, obj, "refuse"); got != "usage" {
			t.Fatalf("refuse = %q, want usage (%s)", got, resp)
		}
		if !strings.Contains(strField(t, obj, "detail"), "read-only daemon") &&
			!strings.Contains(strField(t, obj, "detail"), "auth not enabled") {
			t.Fatalf("refusal detail must be honest about the milestone: %s", resp)
		}
	}
	if after := snapshot(t, st.fs); !equalSnapshots(before, after) {
		t.Fatal("read-only refusals changed the store")
	}
}

type snap struct {
	files map[string][]byte
	dirs  map[string]bool
}

func snapshot(t *testing.T, fs *flintstore.MemFS) snap {
	t.Helper()
	s := snap{files: map[string][]byte{}, dirs: map[string]bool{}}
	for p, b := range fs.Files {
		s.files[p] = append([]byte(nil), b...)
	}
	for p := range fs.Dirs {
		s.dirs[p] = true
	}
	return s
}

func equalSnapshots(a, b snap) bool {
	if len(a.files) != len(b.files) || len(a.dirs) != len(b.dirs) {
		return false
	}
	for p, av := range a.files {
		if !bytes.Equal(av, b.files[p]) {
			return false
		}
	}
	for p := range a.dirs {
		if !b.dirs[p] {
			return false
		}
	}
	return true
}

// TestReadRecoversStaleHead proves the served history is complete after a
// crash between the log append and the record rewrite: the full contiguous
// suffix is returned and the record is rolled forward on disk.
func TestReadRecoversStaleHead(t *testing.T) {
	st := seedStore(t, 3)
	streamID := "0199de2d-dc00-7000-8000-00000000a010"
	// Rewind the record head to the first change (as if a two-change append
	// had landed and the record rewrite crashed).
	first := "0199de2d-dc00-7000-8000-000000000001"
	if err := flintstore.WriteStreamRecord(st.fs, st.root,
		&flintstore.Stream{ID: streamID, Name: "seed", Head: &first, Created: 1760000000000}); err != nil {
		t.Fatal(err)
	}
	conn := &scriptConn{chunks: [][]byte{[]byte(`{"op":"read","seq":7,"stream":"` + streamID + `","after":"` + first + `"}` + "\n")}}
	serveSession(conn, st.root, nil)
	resps := conn.responses(t)
	var changes []string
	if err := json.Unmarshal(resps[0]["changes"], &changes); err != nil {
		t.Fatal(err)
	}
	if len(changes) != 2 {
		t.Fatalf("recovered suffix = %d changes, want 2", len(changes))
	}
	if changes[0] != string(st.lines[1]) || changes[1] != string(st.lines[2]) {
		t.Fatal("recovered suffix bytes mismatch")
	}
	rec, err := st.fs.ReadFile(flintstore.StreamRecordPath(st.root, streamID), flintstore.MaxFileReadBytes)
	if err != nil {
		t.Fatal(err)
	}
	var stream flintstore.Stream
	if err := json.Unmarshal(rec, &stream); err != nil {
		t.Fatal(err)
	}
	if stream.Head == nil || *stream.Head != "0199de2d-dc00-7000-8000-000000000003" {
		t.Fatalf("record head was not rolled forward: %v", stream.Head)
	}
}

// TestOpenAndHeadsShapes checks the canonical list/heads/open reply shapes.
func TestOpenAndHeadsShapes(t *testing.T) {
	st := seedStore(t, 2)
	conn := &scriptConn{chunks: [][]byte{[]byte(
		`{"op":"list","seq":1}` + "\n" +
			`{"op":"heads","seq":2}` + "\n" +
			`{"op":"open","seq":3,"stream":"seed"}` + "\n")}}
	serveSession(conn, st.root, nil)
	lines := strings.Split(strings.TrimSuffix(conn.out.String(), "\n"), "\n")
	if len(lines) != 3 {
		t.Fatalf("want 3 replies, got %q", conn.out.String())
	}
	var list struct {
		Streams []flintstore.Stream `json:"streams"`
	}
	if err := json.Unmarshal([]byte(lines[0]), &list); err != nil {
		t.Fatal(err)
	}
	if len(list.Streams) != 1 || list.Streams[0].Name != "seed" || list.Streams[0].ForkedFrom != nil {
		t.Fatalf("list record = %+v", list.Streams)
	}
	if lines[1] != `{"seq":2,"ok":true,"heads":{"0199de2d-dc00-7000-8000-00000000a010":"0199de2d-dc00-7000-8000-000000000002"}}` {
		t.Fatalf("heads reply = %s", lines[1])
	}
	var open struct {
		Record flintstore.Stream `json:"record"`
	}
	if err := json.Unmarshal([]byte(lines[2]), &open); err != nil {
		t.Fatal(err)
	}
	if open.Record.ID != "0199de2d-dc00-7000-8000-00000000a010" || open.Record.Head == nil {
		t.Fatalf("open record = %+v", open.Record)
	}
}
