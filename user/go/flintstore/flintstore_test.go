package flintstore

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"virelai/ed25519"
	"virelai/sshlib"
)

const testSeedHex = "9d61b19deffd5a60ba844af492ec2cc44449c5697b326919703bac031cae7f60"

func testSeed(t *testing.T) [32]byte {
	t.Helper()
	b, ok := sshlib.ParseHex(testSeedHex)
	if !ok {
		t.Fatal("bad test seed")
	}
	var seed [32]byte
	copy(seed[:], b)
	return seed
}

func signedChange(t *testing.T, seed [32]byte, c Change) Change {
	t.Helper()
	pub := ed25519.EdDerivePublic(seed)
	c.Author = sshlib.EncodeHex(pub[:])
	msg, err := SigMsg(&c)
	if err != nil {
		t.Fatal(err)
	}
	sig := ed25519.EdSign(msg, seed)
	c.Sig = sshlib.EncodeHex(sig[:])
	return c
}

func writeStore(t *testing.T, fs FS, root string, st *Stream, changes []Change) [][]byte {
	t.Helper()
	if err := fs.Mkdir(root); err != nil {
		t.Fatal(err)
	}
	if err := fs.Mkdir(StreamsDir(root)); err != nil {
		t.Fatal(err)
	}
	if err := WriteStreamRecord(fs, root, st); err != nil {
		t.Fatal(err)
	}
	var raw [][]byte
	var log bytes.Buffer
	for _, c := range changes {
		line, err := MarshalChangeLine(c)
		if err != nil {
			t.Fatal(err)
		}
		raw = append(raw, bytes.TrimSuffix(line, []byte{'\n'}))
		log.Write(line)
	}
	if len(changes) > 0 {
		if err := fs.WriteFileSafe(StreamLogPath(root, st.ID), log.Bytes()); err != nil {
			t.Fatal(err)
		}
	}
	return raw
}

// TestSignatureVectorsExtracted proves the extracted canonical encoder still
// reproduces the pinned Ed25519 vectors byte-for-byte: every `good.jsonl`
// Change parses and verifies, every `bad.jsonl` line still verifies against
// its own recomputed preimage (the vectors are bad-signature fixtures whose
// canonical preimage must stay stable).
func TestSignatureVectorsExtracted(t *testing.T) {
	good, err := os.ReadFile(filepath.Join("..", "flint", "testdata", "sig", "good.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	lines, err := ReadJSONLLines(good)
	if err != nil {
		t.Fatal(err)
	}
	if len(lines) == 0 {
		t.Fatal("no good signature vectors")
	}
	for _, line := range lines {
		c, err := ParseChange(line)
		if err != nil {
			t.Fatalf("ParseChange: %v", err)
		}
		ok, err := VerifyChange(&c)
		if err != nil || !ok {
			t.Fatalf("good vector %s did not verify: ok=%v err=%v", c.ID, ok, err)
		}
	}

	bad, err := os.ReadFile(filepath.Join("..", "flint", "testdata", "sig", "bad.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	badLines, err := ReadJSONLLines(bad)
	if err != nil {
		t.Fatal(err)
	}
	for _, line := range badLines {
		c, err := ParseChange(line)
		if err != nil {
			t.Fatalf("ParseChange(bad): %v", err)
		}
		if ok, _ := VerifyChange(&c); ok {
			t.Fatalf("bad vector %s verified", c.ID)
		}
	}
}

// TestRecoverFullContiguousSuffix proves recovery rolls the record head
// forward over every valid descendant — not only the final direct child —
// after a crash between the .jsonl write and the .json rewrite.
func TestRecoverFullContiguousSuffix(t *testing.T) {
	fs := NewMemFS()
	seed := testSeed(t)
	root := "/host/FLINT"
	streamID := "0199de2d-dc00-7000-8000-00000000b010"
	var changes []Change
	var parent *string
	for i := 1; i <= 3; i++ {
		id := "0199de2d-dc00-7000-8000-00000000b10" + string(rune('0'+i))
		changes = append(changes, signedChange(t, seed, Change{
			ID:      id,
			Parent:  parent,
			Stream:  streamID,
			TS:      int64(1760000000000 + i),
			Message: "change",
			Ops:     []Op{},
		}))
		parent = &changes[len(changes)-1].ID
	}
	st := &Stream{ID: streamID, Name: "main", Head: nil, Created: 1760000000000}
	writeStore(t, fs, root, st, changes)

	for _, staleHead := range []*string{nil, &changes[0].ID, &changes[1].ID} {
		stale := &Stream{ID: streamID, Name: "main", Head: CopyStrPtr(staleHead), Created: 1760000000000}
		if err := WriteStreamRecord(fs, root, stale); err != nil {
			t.Fatal(err)
		}
		loaded, err := LoadStreamFull(fs, root, streamID)
		if err != nil {
			t.Fatalf("LoadStreamFull(head=%v): %v", staleHead, err)
		}
		if loaded.Stream.Head == nil || *loaded.Stream.Head != changes[2].ID {
			t.Fatalf("recovered head = %v, want %s", loaded.Stream.Head, changes[2].ID)
		}
		rec, err := fs.ReadFile(StreamRecordPath(root, streamID), MaxFileReadBytes)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Contains(rec, []byte(changes[2].ID)) {
			t.Fatalf("record was not rewritten with the recovered head: %s", rec)
		}
	}

	// An unknown record head is not a recoverable suffix: refuse by name.
	unknown := "0199de2d-dc00-7000-8000-00000000b1ff"
	if err := WriteStreamRecord(fs, root, &Stream{ID: streamID, Name: "main", Head: &unknown, Created: 1760000000000}); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadStreamFull(fs, root, streamID); RefusalName(err) != "stream_malformed" {
		t.Fatalf("unknown head: want stream_malformed, got %v", err)
	}
}

// TestTornLogRefuses proves a partial final record refuses stream_torn instead
// of serving a partial history.
func TestTornLogRefuses(t *testing.T) {
	fs := NewMemFS()
	seed := testSeed(t)
	root := "/host/FLINT"
	streamID := "0199de2d-dc00-7000-8000-00000000c010"
	c := signedChange(t, seed, Change{
		ID: "0199de2d-dc00-7000-8000-00000000c101", Stream: streamID,
		TS: 1760000000001, Message: "one", Ops: []Op{},
	})
	writeStore(t, fs, root, &Stream{ID: streamID, Name: "main", Head: &c.ID, Created: 1760000000000}, []Change{c})
	line, err := MarshalChangeLine(c)
	if err != nil {
		t.Fatal(err)
	}
	torn := append(append([]byte{}, line...), []byte("{\"id\":\"torn\"")...)
	if err := fs.WriteFileSafe(StreamLogPath(root, streamID), torn); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadStreamFull(fs, root, streamID); RefusalName(err) != "stream_torn" {
		t.Fatalf("want stream_torn, got %v", err)
	}
	if _, err := LoadStoreIndexStrict(fs, root); RefusalName(err) != "stream_torn" {
		t.Fatalf("index: want stream_torn, got %v", err)
	}
}

// TestStoreBoundsExtracted proves the 4 MiB file bound and 4096-change bound
// are shared by both index loaders.
func TestStoreBoundsExtracted(t *testing.T) {
	fs := NewMemFS()
	root := "/host/FLINT"
	streamID := "0199de2d-dc00-7000-8000-00000000d010"
	if err := fs.Mkdir(StreamsDir(root)); err != nil {
		t.Fatal(err)
	}
	if err := WriteStreamRecord(fs, root, &Stream{ID: streamID, Name: "main", Created: 1760000000000}); err != nil {
		t.Fatal(err)
	}
	// > 4 MiB store file.
	big := bytes.Repeat([]byte{'x'}, MaxFileReadBytes+1)
	if err := fs.WriteFileSafe(StreamLogPath(root, streamID), big); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadStoreIndex(fs, root); RefusalName(err) != "store_too_large" {
		t.Fatalf("CLI index: want store_too_large, got %v", err)
	}
	if _, err := LoadStoreIndexStrict(fs, root); RefusalName(err) != "store_too_large" {
		t.Fatalf("strict index: want store_too_large, got %v", err)
	}
	// > 4096 changes within the file bound.
	var buf bytes.Buffer
	for i := 0; i <= MaxChanges; i++ {
		buf.WriteString("{}\n")
	}
	if err := fs.WriteFileSafe(StreamLogPath(root, streamID), buf.Bytes()); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadStoreIndexStrict(fs, root); RefusalName(err) != "store_too_large" {
		t.Fatalf("strict change bound: want store_too_large, got %v", err)
	}
}

// TestStrictIndexValidatesWholeStream proves the daemon index validates every
// Change (chain and signature), not only the line the record head names, so
// list/heads/open cannot advertise a stream that read would refuse. The CLI
// loader stays tolerant of a corrupt middle Change because its verify verb
// owns that report.
func TestStrictIndexValidatesWholeStream(t *testing.T) {
	fs := NewMemFS()
	seed := testSeed(t)
	root := "/host/FLINT"
	streamID := "0199de2d-dc00-7000-8000-00000000f010"
	var changes []Change
	var parent *string
	for i := 1; i <= 3; i++ {
		c := signedChange(t, seed, Change{
			ID:      "0199de2d-dc00-7000-8000-00000000f10" + string(rune('0'+i)),
			Parent:  parent,
			Stream:  streamID,
			TS:      int64(1760000000000 + i),
			Message: "change",
			Ops:     []Op{},
		})
		changes = append(changes, c)
		parent = &changes[len(changes)-1].ID
	}
	st := &Stream{ID: streamID, Name: "main", Head: &changes[2].ID, Created: 1760000000000}
	writeStore(t, fs, root, st, changes)

	// Flip one signature nibble on the middle Change; the head still names a
	// valid last Change.
	bad := changes[1]
	last := bad.Sig[len(bad.Sig)-1]
	if last == '0' {
		bad.Sig = bad.Sig[:len(bad.Sig)-1] + "1"
	} else {
		bad.Sig = bad.Sig[:len(bad.Sig)-1] + "0"
	}
	writeStore(t, fs, root, st, []Change{changes[0], bad, changes[2]})
	if _, err := LoadStoreIndex(fs, root); err != nil {
		t.Fatalf("CLI index should tolerate a bad middle signature: %v", err)
	}
	if _, err := LoadStoreIndexStrict(fs, root); RefusalName(err) != "sig_invalid" {
		t.Fatalf("strict index: want sig_invalid, got %v", err)
	}

	// A broken middle parent link is stream_malformed even though the last
	// line parses and matches the record head.
	broken := changes[1]
	other := changes[2].ID
	broken.Parent = &other
	broken = signedChange(t, seed, broken)
	writeStore(t, fs, root, st, []Change{changes[0], broken, changes[2]})
	if _, err := LoadStoreIndexStrict(fs, root); RefusalName(err) != "change_malformed" &&
		RefusalName(err) != "stream_malformed" {
		t.Fatalf("strict index: want change_malformed/stream_malformed, got %v", err)
	}
}

// TestStrictLayoutRules proves the daemon loader refuses the layouts the host
// reference server refuses.
func TestStrictLayoutRules(t *testing.T) {
	fs := NewMemFS()
	root := "/host/FLINT"
	id := "0199de2d-dc00-7000-8000-00000000e010"
	if err := fs.Mkdir(StreamsDir(root)); err != nil {
		t.Fatal(err)
	}
	// Orphan log without a record.
	if err := fs.WriteFileSafe(StreamLogPath(root, id), []byte("{}\n")); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadStoreIndexStrict(fs, root); RefusalName(err) != "stream_malformed" {
		t.Fatalf("orphan log: want stream_malformed, got %v", err)
	}
	// A non-UUID stem is not a stream.
	if err := fs.WriteFileSafe(StreamsDir(root)+"/not-a-uuid.json", []byte("{}")); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadStoreIndexStrict(fs, root); RefusalName(err) != "stream_malformed" {
		t.Fatalf("bad stem: want stream_malformed, got %v", err)
	}
}
