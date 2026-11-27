package flintstore

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
)

const writeTestStream = "0199de2d-dc00-7000-8000-00000000e010"

func linearChanges(t *testing.T, seed [32]byte, streamID string, n int, baseTS int64) []Change {
	t.Helper()
	var out []Change
	var parent *string
	for i := 1; i <= n; i++ {
		c := signedChange(t, seed, Change{
			ID:      fmt.Sprintf("0199de2d-dc00-7000-8000-%012d", i),
			Parent:  parent,
			Stream:  streamID,
			TS:      baseTS + int64(i),
			Message: fmt.Sprintf("change %d", i),
			Ops:     []Op{},
		})
		out = append(out, c)
		parent = &out[len(out)-1].ID
	}
	return out
}

func otherSeed(t *testing.T) [32]byte {
	t.Helper()
	var seed [32]byte
	for i := range seed {
		seed[i] = 0x11
	}
	return seed
}

func streamRecord(id, name string, head *string) *Stream {
	return &Stream{ID: id, Name: name, Head: head, Created: 1760000000000}
}

func emptyStore(t *testing.T) (*MemFS, string) {
	t.Helper()
	fs := NewMemFS()
	root := "/host/FLINT"
	if err := fs.Mkdir(StreamsDir(root)); err != nil {
		t.Fatal(err)
	}
	return fs, root
}

func TestCreateStreamValidatesCanonicalShapeAndCollisions(t *testing.T) {
	fs, root := emptyStore(t)

	fresh := streamRecord(writeTestStream, "wire", nil)
	if err := CreateStream(fs, root, fresh); err != nil {
		t.Fatalf("create fresh stream: %v", err)
	}
	if !fs.Exists(StreamRecordPath(root, writeTestStream)) {
		t.Fatal("create did not write the stream record")
	}
	if fs.Exists(StreamLogPath(root, writeTestStream)) {
		t.Fatal("create must not write a .jsonl (it is created lazily)")
	}

	// A second stream may not reuse the id or the name.
	if err := CreateStream(fs, root, streamRecord(writeTestStream, "other", nil)); RefusalName(err) != "stream_exists" {
		t.Fatalf("duplicate id: want stream_exists, got %v", err)
	}
	if err := CreateStream(fs, root, streamRecord("0199de2d-dc00-7000-8000-00000000e011", "wire", nil)); RefusalName(err) != "stream_exists" {
		t.Fatalf("duplicate name: want stream_exists, got %v", err)
	}

	bad := []struct {
		name string
		st   *Stream
		want string
	}{
		{"short id", streamRecord("not-a-uuid", "bad", nil), "stream_malformed"},
		{"uppercase id", streamRecord("0199DE2D-DC00-7000-8000-00000000E012", "bad", nil), "stream_malformed"},
		{"bad name", streamRecord("0199de2d-dc00-7000-8000-00000000e013", "Bad_Name", nil), "stream_malformed"},
		{"long name", streamRecord("0199de2d-dc00-7000-8000-00000000e014", strings.Repeat("a", 33), nil), "stream_malformed"},
		{"negative created", &Stream{ID: "0199de2d-dc00-7000-8000-00000000e015", Name: "neg", Created: -1}, "stream_malformed"},
		{"bad head uuid", streamRecord("0199de2d-dc00-7000-8000-00000000e016", "badhead", nil), "stream_malformed"},
	}
	bad[5].st.Head = ptr("nope")
	for _, tc := range bad {
		if err := CreateStream(fs, root, tc.st); RefusalName(err) != tc.want {
			t.Fatalf("%s: want %s, got %v", tc.name, tc.want, err)
		}
	}

	// A fresh (non-fork) stream may not name a head.
	if err := CreateStream(fs, root, streamRecord("0199de2d-dc00-7000-8000-00000000e017", "freshhead", ptr("0199de2d-dc00-7000-8000-000000000001"))); RefusalName(err) != "stream_malformed" {
		t.Fatalf("fresh head: want stream_malformed, got %v", err)
	}

	// Fork origins must resolve to a real Change in a real stream.
	missing := &Stream{
		ID: "0199de2d-dc00-7000-8000-00000000e018", Name: "forkmissing",
		Head: ptr("0199de2d-dc00-7000-8000-000000000001"),
		ForkedFrom: &ForkOrigin{
			Stream: "0199de2d-dc00-7000-8000-00000000ffff",
			Change: "0199de2d-dc00-7000-8000-000000000001",
		},
		Created: 1760000000000,
	}
	if err := CreateStream(fs, root, missing); RefusalName(err) != "stream_missing" {
		t.Fatalf("missing fork origin: want stream_missing, got %v", err)
	}
}

func TestCreateStreamForkAtReachableChange(t *testing.T) {
	fs, root := emptyStore(t)
	seed := testSeed(t)
	changes := linearChanges(t, seed, writeTestStream, 2, 1760000000000)
	writeStore(t, fs, root, streamRecord(writeTestStream, "main", &changes[1].ID), changes)

	fork := &Stream{
		ID: "0199de2d-dc00-7000-8000-00000000e020", Name: "feature",
		Head:       &changes[0].ID,
		ForkedFrom: &ForkOrigin{Stream: writeTestStream, Change: changes[0].ID},
		Created:    1760000000100,
	}
	if err := CreateStream(fs, root, fork); err != nil {
		t.Fatalf("fork at reachable change: %v", err)
	}

	// The head must equal the fork origin; a different head is malformed.
	badHead := &Stream{
		ID: "0199de2d-dc00-7000-8000-00000000e021", Name: "feature2",
		Head:       &changes[1].ID,
		ForkedFrom: &ForkOrigin{Stream: writeTestStream, Change: changes[0].ID},
		Created:    1760000000100,
	}
	if err := CreateStream(fs, root, badHead); RefusalName(err) != "stream_malformed" {
		t.Fatalf("head mismatch: want stream_malformed, got %v", err)
	}

	// An unreachable Change refuses change_missing.
	unknown := &Stream{
		ID: "0199de2d-dc00-7000-8000-00000000e022", Name: "feature3",
		Head:       ptr("0199de2d-dc00-7000-8000-0000000000ff"),
		ForkedFrom: &ForkOrigin{Stream: writeTestStream, Change: "0199de2d-dc00-7000-8000-0000000000ff"},
		Created:    1760000000100,
	}
	if err := CreateStream(fs, root, unknown); RefusalName(err) != "change_missing" {
		t.Fatalf("unreachable fork change: want change_missing, got %v", err)
	}
}

func TestAppendStaleHeadBeforeBatchValidation(t *testing.T) {
	fs, root := emptyStore(t)
	seed := testSeed(t)
	changes := linearChanges(t, seed, writeTestStream, 2, 1760000000000)
	writeStore(t, fs, root, streamRecord(writeTestStream, "main", &changes[1].ID), changes)
	before, err := fs.ReadFile(StreamLogPath(root, writeTestStream), MaxFileReadBytes)
	if err != nil {
		t.Fatal(err)
	}

	// Stale expected head wins over an unparseable batch: the race is
	// reported before the batch is even inspected.
	if _, err := AppendStreamBatch(fs, root, writeTestStream, &changes[0].ID, [][]byte{[]byte("not json")}, nil); RefusalName(err) != "stale_head" {
		t.Fatalf("stale head + malformed batch: want stale_head, got %v", err)
	}
	// ... and over an empty batch too.
	if _, err := AppendStreamBatch(fs, root, writeTestStream, nil, nil, nil); RefusalName(err) != "stale_head" {
		t.Fatalf("stale head + empty batch: want stale_head, got %v", err)
	}
	// With the correct head the same empty batch is change_malformed.
	if _, err := AppendStreamBatch(fs, root, writeTestStream, &changes[1].ID, nil, nil); RefusalName(err) != "change_malformed" {
		t.Fatalf("empty batch: want change_malformed, got %v", err)
	}
	// Unknown stream refuses stream_missing.
	if _, err := AppendStreamBatch(fs, root, "0199de2d-dc00-7000-8000-00000000ffff", nil, nil, nil); RefusalName(err) != "stream_missing" {
		t.Fatalf("missing stream: want stream_missing, got %v", err)
	}

	after, err := fs.ReadFile(StreamLogPath(root, writeTestStream), MaxFileReadBytes)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, after) {
		t.Fatal("a refused append changed the log bytes")
	}
	if rec, _ := fs.ReadFile(StreamRecordPath(root, writeTestStream), MaxFileReadBytes); !bytes.Contains(rec, []byte(changes[1].ID)) {
		t.Fatalf("a refused append changed the record head: %s", rec)
	}
}

func TestAppendValidatesWholeBatchBeforePublication(t *testing.T) {
	fs, root := emptyStore(t)
	seed := testSeed(t)
	base := linearChanges(t, seed, writeTestStream, 1, 1760000000000)
	writeStore(t, fs, root, streamRecord(writeTestStream, "main", &base[0].ID), base)
	before, err := fs.ReadFile(StreamLogPath(root, writeTestStream), MaxFileReadBytes)
	if err != nil {
		t.Fatal(err)
	}

	mk := func(idSuffix int, parent *string, msg string, seedOverride *[32]byte) Change {
		s := seed
		if seedOverride != nil {
			s = *seedOverride
		}
		return signedChange(t, s, Change{
			ID:      fmt.Sprintf("0199de2d-dc00-7000-8000-%012d", idSuffix),
			Parent:  parent,
			Stream:  writeTestStream,
			TS:      int64(1760000000100 + idSuffix),
			Message: msg,
			Ops:     []Op{},
		})
	}
	line := func(c Change) []byte {
		b, err := MarshalChangeLine(c)
		if err != nil {
			t.Fatal(err)
		}
		return bytes.TrimSuffix(b, []byte{'\n'})
	}

	good := mk(101, &base[0].ID, "first", nil)
	badSig := mk(102, &good.ID, "second", nil)
	last := badSig.Sig[len(badSig.Sig)-1]
	if last == '0' {
		badSig.Sig = badSig.Sig[:len(badSig.Sig)-1] + "1"
	} else {
		badSig.Sig = badSig.Sig[:len(badSig.Sig)-1] + "0"
	}

	cases := []struct {
		name  string
		lines [][]byte
		want  string
	}{
		{"second line bad signature", [][]byte{line(good), line(badSig)}, "sig_invalid"},
		{"wrong stream id", [][]byte{line(signedChange(t, seed, Change{
			ID: "0199de2d-dc00-7000-8000-000000000103", Parent: &base[0].ID,
			Stream: "0199de2d-dc00-7000-8000-00000000ffff", TS: 1760000000200,
			Message: "other stream", Ops: []Op{},
		}))}, "change_malformed"},
		{"parent chain break", [][]byte{line(mk(104, nil, "detached", nil))}, "change_malformed"},
		{"duplicate existing id", [][]byte{line(mk(1, &base[0].ID, "dup", nil))}, "change_malformed"},
		{"invalid message", [][]byte{line(mk(105, &base[0].ID, strings.Repeat("x", MaxMessageBytes+1), nil))}, "message_invalid"},
		{"not json", [][]byte{[]byte("{")}, "change_malformed"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := AppendStreamBatch(fs, root, writeTestStream, &base[0].ID, tc.lines, nil); RefusalName(err) != tc.want {
				t.Fatalf("want %s, got %v", tc.want, err)
			}
			after, rerr := fs.ReadFile(StreamLogPath(root, writeTestStream), MaxFileReadBytes)
			if rerr != nil {
				t.Fatal(rerr)
			}
			if !bytes.Equal(before, after) {
				t.Fatal("a rejected batch changed the log bytes")
			}
		})
	}

	// The independent --allow-author gate refuses before publication and
	// does not bypass signature verification.
	allowed := edAuthor(t, seed)
	if _, err := AppendStreamBatch(fs, root, writeTestStream, &base[0].ID,
		[][]byte{line(mk(107, &base[0].ID, "foreign", ptrSeed(otherSeed(t))))},
		func(author string) bool { return author == allowed }); RefusalName(err) != "author_not_allowed" {
		t.Fatalf("author gate: want author_not_allowed, got %v", err)
	}
	after, _ := fs.ReadFile(StreamLogPath(root, writeTestStream), MaxFileReadBytes)
	if !bytes.Equal(before, after) {
		t.Fatal("author refusal changed the log bytes")
	}

	// A two-line valid batch then lands both lines and publishes the last
	// head. Replaying the same batch refuses stale_head instead of
	// duplicating accepted history.
	second := mk(108, &good.ID, "second", nil)
	if st, err := AppendStreamBatch(fs, root, writeTestStream, &base[0].ID, [][]byte{line(good), line(second)}, nil); err != nil {
		t.Fatalf("valid batch: %v", err)
	} else if st.Head == nil || *st.Head != second.ID {
		t.Fatalf("head = %v, want %s", st.Head, second.ID)
	}
	if _, err := AppendStreamBatch(fs, root, writeTestStream, &base[0].ID, [][]byte{line(good), line(second)}, nil); RefusalName(err) != "stale_head" {
		t.Fatalf("replay: want stale_head, got %v", err)
	}
}

// ptrSeed is a tiny helper for choosing a signing seed in a table entry.
func ptrSeed(s [32]byte) *[32]byte { return &s }

func edAuthor(t *testing.T, seed [32]byte) string {
	t.Helper()
	// Deriving through a signed Change keeps the helper in lockstep with the
	// package's Ed25519 vector path.
	c := signedChange(t, seed, Change{
		ID: "0199de2d-dc00-7000-8000-0000000000aa", Stream: writeTestStream,
		TS: 1, Message: "author", Ops: []Op{},
	})
	return c.Author
}

func TestAppendOneAppendSyncAndCrashRecovery(t *testing.T) {
	fs, root := emptyStore(t)
	seed := testSeed(t)
	base := linearChanges(t, seed, writeTestStream, 1, 1760000000000)
	writeStore(t, fs, root, streamRecord(writeTestStream, "main", &base[0].ID), base)
	batch := linearChanges(t, seed, writeTestStream, 5, 1760000010000)[1:4] // ids 2..4
	for i := range batch {
		if i == 0 {
			batch[i].Parent = &base[0].ID
		} else {
			batch[i].Parent = &batch[i-1].ID
		}
		batch[i] = signedChange(t, seed, batch[i])
	}
	var lines [][]byte
	for _, c := range batch {
		b, err := MarshalChangeLine(c)
		if err != nil {
			t.Fatal(err)
		}
		lines = append(lines, bytes.TrimSuffix(b, []byte{'\n'}))
	}

	appends := 0
	fs.BeforeWrite = func(op, path string) error {
		if op == "append" {
			appends++
		}
		return nil
	}
	if _, err := AppendStreamBatch(fs, root, writeTestStream, &base[0].ID, lines, nil); err != nil {
		t.Fatalf("batch append: %v", err)
	}
	if appends != 1 {
		t.Fatalf("append sync calls = %d, want exactly 1", appends)
	}
	fs.BeforeWrite = nil
	returned, err := LoadStreamFull(fs, root, writeTestStream)
	if err != nil {
		t.Fatal(err)
	}
	if len(returned.Changes) != 4 || *returned.Stream.Head != batch[2].ID {
		t.Fatalf("published head/changes = %v/%d", returned.Stream.Head, len(returned.Changes))
	}
}

func TestAppendCrashWindowRecoversFullSuffix(t *testing.T) {
	fs, root := emptyStore(t)
	seed := testSeed(t)
	base := linearChanges(t, seed, writeTestStream, 1, 1760000000000)
	writeStore(t, fs, root, streamRecord(writeTestStream, "main", &base[0].ID), base)

	batch := linearChanges(t, seed, writeTestStream, 4, 1760000020000)[1:4] // ids 2..4
	for i := range batch {
		if i == 0 {
			batch[i].Parent = &base[0].ID
		} else {
			batch[i].Parent = &batch[i-1].ID
		}
		batch[i] = signedChange(t, seed, batch[i])
	}
	var lines [][]byte
	for _, c := range batch {
		b, _ := MarshalChangeLine(c)
		lines = append(lines, bytes.TrimSuffix(b, []byte{'\n'}))
	}

	recordPath := StreamRecordPath(root, writeTestStream)
	// The crash window: the log append succeeds, the record rewrite never
	// lands. This is deterministic, unlike a process kill.
	fs.BeforeWrite = func(op, path string) error {
		if op == "write" && path == recordPath {
			return errors.New("simulated crash before head publication")
		}
		return nil
	}
	if _, err := AppendStreamBatch(fs, root, writeTestStream, &base[0].ID, lines, nil); err == nil {
		t.Fatal("expected the simulated record rewrite to fail")
	}
	fs.BeforeWrite = nil

	// The record still names the old head; the log carries all three lines.
	recBytes, err := fs.ReadFile(recordPath, MaxFileReadBytes)
	if err != nil {
		t.Fatal(err)
	}
	var rec Stream
	if err := json.Unmarshal(recBytes, &rec); err != nil {
		t.Fatal(err)
	}
	if rec.Head == nil || *rec.Head != base[0].ID {
		t.Fatalf("record head after crash = %v, want %s", rec.Head, base[0].ID)
	}

	// Recovery rolls the head forward over the complete contiguous suffix
	// and leaves every original raw line intact.
	recovered, err := LoadStreamFull(fs, root, writeTestStream)
	if err != nil {
		t.Fatalf("recover: %v", err)
	}
	if recovered.Stream.Head == nil || *recovered.Stream.Head != batch[2].ID {
		t.Fatalf("recovered head = %v, want %s", recovered.Stream.Head, batch[2].ID)
	}
	if len(recovered.Changes) != 4 {
		t.Fatalf("recovered changes = %d, want 4", len(recovered.Changes))
	}
	for i := 1; i < len(recovered.RawLines); i++ {
		want, _ := MarshalChangeLine(recovered.Changes[i])
		if !bytes.Equal(recovered.RawLines[i], bytes.TrimSuffix(want, []byte{'\n'})) {
			t.Fatalf("recovered raw line %d changed", i)
		}
	}

	// A crash that loses the append itself leaves no bytes and no head move.
	fs.BeforeWrite = func(op, path string) error {
		if op == "append" {
			return errors.New("simulated crash before append")
		}
		return nil
	}
	before, _ := fs.ReadFile(StreamLogPath(root, writeTestStream), MaxFileReadBytes)
	next := linearChanges(t, seed, writeTestStream, 5, 1760000030000)[4]
	next.Parent = &batch[2].ID
	next = signedChange(t, seed, next)
	nextLine, _ := MarshalChangeLine(next)
	if _, err := AppendStreamBatch(fs, root, writeTestStream, &batch[2].ID,
		[][]byte{bytes.TrimSuffix(nextLine, []byte{'\n'})}, nil); err == nil {
		t.Fatal("expected the simulated append to fail")
	}
	fs.BeforeWrite = nil
	after, _ := fs.ReadFile(StreamLogPath(root, writeTestStream), MaxFileReadBytes)
	if !bytes.Equal(before, after) {
		t.Fatal("a failed AppendSync changed the log bytes")
	}
}

func TestAppendMessageBoundary(t *testing.T) {
	fs, root := emptyStore(t)
	seed := testSeed(t)
	base := linearChanges(t, seed, writeTestStream, 1, 1760000000000)
	writeStore(t, fs, root, streamRecord(writeTestStream, "main", &base[0].ID), base)

	mk := func(id int, msg string) []byte {
		c := signedChange(t, seed, Change{
			ID: fmt.Sprintf("0199de2d-dc00-7000-8000-%012d", id), Parent: &base[0].ID,
			Stream: writeTestStream, TS: 1760000040000 + int64(id), Message: msg, Ops: []Op{},
		})
		b, _ := MarshalChangeLine(c)
		return bytes.TrimSuffix(b, []byte{'\n'})
	}

	// 513 bytes refuses; the exact 512-byte boundary is admitted.
	if _, err := AppendStreamBatch(fs, root, writeTestStream, &base[0].ID,
		[][]byte{mk(201, strings.Repeat("x", MaxMessageBytes+1))}, nil); RefusalName(err) != "message_invalid" {
		t.Fatalf("513-byte message: want message_invalid, got %v", err)
	}
	if st, err := AppendStreamBatch(fs, root, writeTestStream, &base[0].ID,
		[][]byte{mk(202, strings.Repeat("x", MaxMessageBytes))}, nil); err != nil {
		t.Fatalf("512-byte message at the boundary: %v", err)
	} else if st.Head == nil {
		t.Fatal("boundary append did not publish a head")
	}
}

func TestAppendGlobalChangeBoundary(t *testing.T) {
	fs, root := emptyStore(t)
	seed := testSeed(t)
	// 4096 is the global bound; seed one short and prove exact admission,
	// then the next Change refuses without being written.
	changes := linearChanges(t, seed, writeTestStream, MaxChanges-1, 1760000000000)
	writeStore(t, fs, root, streamRecord(writeTestStream, "main", &changes[len(changes)-1].ID), changes)

	mkLine := func(id int) []byte {
		c := signedChange(t, seed, Change{
			ID: fmt.Sprintf("0199de2d-dc00-7000-8000-%012d", id), Parent: &changes[len(changes)-1].ID,
			Stream: writeTestStream, TS: 1760000050000, Message: "last slot", Ops: []Op{},
		})
		b, _ := MarshalChangeLine(c)
		return bytes.TrimSuffix(b, []byte{'\n'})
	}
	// MaxChanges-1... the seeded ids are 1..4095, so id 4096 is free.
	last := mkLine(4096)
	if st, err := AppendStreamBatch(fs, root, writeTestStream, &changes[len(changes)-1].ID, [][]byte{last}, nil); err != nil {
		t.Fatalf("append at exactly %d changes: %v", MaxChanges, err)
	} else if st.Head == nil {
		t.Fatal("boundary append published no head")
	}

	before, _ := fs.ReadFile(StreamLogPath(root, writeTestStream), MaxFileReadBytes)
	over := signedChange(t, seed, Change{
		ID: "0199de2d-dc00-7000-8000-000000004097", Parent: ptr("0199de2d-dc00-7000-8000-000000004096"),
		Stream: writeTestStream, TS: 1760000050001, Message: "over", Ops: []Op{},
	})
	overLine, _ := MarshalChangeLine(over)
	if _, err := AppendStreamBatch(fs, root, writeTestStream, ptr("0199de2d-dc00-7000-8000-000000004096"),
		[][]byte{bytes.TrimSuffix(overLine, []byte{'\n'})}, nil); RefusalName(err) != "store_too_large" {
		t.Fatalf("over the %d bound: want store_too_large, got %v", MaxChanges, err)
	}
	after, _ := fs.ReadFile(StreamLogPath(root, writeTestStream), MaxFileReadBytes)
	if !bytes.Equal(before, after) {
		t.Fatal("an over-bound append changed the log bytes")
	}
}

func TestAppendTornHistoryRefuses(t *testing.T) {
	fs, root := emptyStore(t)
	seed := testSeed(t)
	changes := linearChanges(t, seed, writeTestStream, 1, 1760000000000)
	writeStore(t, fs, root, streamRecord(writeTestStream, "main", &changes[0].ID), changes)
	logBytes, _ := fs.ReadFile(StreamLogPath(root, writeTestStream), MaxFileReadBytes)
	torn := append(append([]byte{}, logBytes...), []byte(`{"id":"torn`)...)
	if err := fs.WriteFileSafe(StreamLogPath(root, writeTestStream), torn); err != nil {
		t.Fatal(err)
	}
	if _, err := AppendStreamBatch(fs, root, writeTestStream, &changes[0].ID, [][]byte{[]byte("{}")}, nil); RefusalName(err) != "stream_torn" {
		t.Fatalf("torn history: want stream_torn, got %v", err)
	}
}

func ptr(s string) *string { return &s }
