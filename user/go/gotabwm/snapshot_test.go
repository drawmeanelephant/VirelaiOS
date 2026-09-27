// M81g (#1767) — the seat's snapshot bundle: capture, and the restore drill.
//
// vi.WriteFileSafe, vi.DirList and vi.ConsoleLine all reach the raw syscall
// gateway and degrade off the guest, so the whole capture/restore path is
// driven through the seam vars in snapshot.go — the writeHostFile / execApp /
// closeWin pattern again (session.go, hid.go, interop.go). What that buys is
// the property the card is actually judged on, and it is a property about
// ORDER and ABSENCE, both of which a fake share can pin exactly:
//
//   - a corrupt bundle writes NOTHING (not "writes the good entries"),
//   - a refusal marker appears only after the read that refused,
//   - a successful restore's bytes are byte-exact, not merely plausible,
//   - the one-shot request is consumed on every outcome, so a bad bundle
//     cannot refuse forever.
package main

import (
	"bytes"
	"strings"
	"testing"

	"virelai/settings"
	"virelai/snapshot"
	"virelai/vi"
)

// fakeShare is a /host in a map, plus the ordered op log the ordering
// assertions read. The log is the point: "the marker printed after the
// publish returned" is a claim about SEQUENCE, and a recorder of side effects
// is the only way a host test can see it.
type fakeShare struct {
	files map[string][]byte
	dirs  map[string]bool
	ops   []string
	log   []string

	writeErr map[string]int64
	readErr  map[string]int64
	mkdirErr int64
}

func newFakeShare() *fakeShare {
	return &fakeShare{
		files:    map[string][]byte{},
		dirs:     map[string]bool{},
		writeErr: map[string]int64{},
		readErr:  map[string]int64{},
		mkdirErr: vi.ErrFileExists, // "already exists" is the normal case
	}
}

func (f *fakeShare) put(path string, body []byte) {
	f.files[path] = append([]byte(nil), body...)
	f.ops = append(f.ops, "put:"+path)
}

// install swaps every seam for this share and returns a cleanup that puts the
// real ones back, so a test in this file cannot leak a fake into another.
func (f *fakeShare) install(t *testing.T) {
	t.Helper()
	oldWrite, oldRead, oldList := snapWrite, snapRead, snapList
	oldExists, oldDelete, oldMkdir, oldLog := snapExists, snapDelete, snapMkdir, snapLog

	snapWrite = func(path string, b []byte) int64 {
		if rc, bad := f.writeErr[path]; bad {
			f.ops = append(f.ops, "write:"+path+"=err")
			return rc
		}
		f.files[path] = append([]byte(nil), b...)
		f.ops = append(f.ops, "write:"+path)
		return 0
	}
	// ReadFileAll's contract, including its truncation: a file longer than
	// max comes back TRUNCATED to max bytes with a non-negative result,
	// never an error. Callers detect an oversize file by length.
	snapRead = func(path string, max int) ([]byte, int64) {
		if rc, bad := f.readErr[path]; bad {
			f.ops = append(f.ops, "read:"+path+"=err")
			return nil, rc
		}
		body, ok := f.files[path]
		if !ok {
			f.ops = append(f.ops, "read:"+path+"=absent")
			return nil, vi.ErrFileNotFound // already negative
		}
		if len(body) > max {
			body = body[:max]
		}
		f.ops = append(f.ops, "read:"+path)
		return append([]byte(nil), body...), int64(len(body))
	}
	snapList = func(path string, buf []vi.DirEntry) (int, int64) {
		if !f.dirs[path] && path != "" {
			return 0, vi.ErrFileNotFound
		}
		prefix := path + "/"
		if path == "" {
			prefix = ""
		}
		n := 0
		for name := range f.files {
			if !strings.HasPrefix(name, prefix) {
				continue
			}
			rest := name[len(prefix):]
			if strings.Contains(rest, "/") {
				continue // a direct child only, like the kernel
			}
			if n >= len(buf) {
				break
			}
			copy(buf[n].Name[:], name[len(prefix):])
			buf[n].Size = uint32(len(f.files[name]))
			n++
		}
		f.ops = append(f.ops, "list:"+path)
		return n, int64(n)
	}
	snapExists = func(path string) bool {
		_, ok := f.files[path]
		return ok
	}
	snapDelete = func(path string) int64 {
		delete(f.files, path)
		f.ops = append(f.ops, "delete:"+path)
		return 0
	}
	snapMkdir = func(path string) int64 {
		f.ops = append(f.ops, "mkdir:"+path)
		if rc := f.mkdirErr; rc != 0 {
			return rc
		}
		f.dirs[path] = true
		return 0
	}
	snapLog = func(s string) {
		f.ops = append(f.ops, "log:"+s)
		f.log = append(f.log, s)
	}

	t.Cleanup(func() {
		snapWrite, snapRead, snapList = oldWrite, oldRead, oldList
		snapExists, snapDelete, snapMkdir, snapLog = oldExists, oldDelete, oldMkdir, oldLog
	})
}

// lines returns the marker lines the seat printed.
func (f *fakeShare) lines() []string { return f.log }

// writes returns the share paths that were published, in order.
func (f *fakeShare) writes() []string {
	var out []string
	for _, op := range f.ops {
		if strings.HasPrefix(op, "write:") {
			p := strings.TrimPrefix(op, "write:")
			if !strings.HasSuffix(p, "=err") {
				out = append(out, p)
			}
		}
	}
	return out
}

// seedBundle stages a GOOD bundle and the one-shot request, which is what a
// drill run looks like on a share.
func (f *fakeShare) seedBundle(t *testing.T) {
	t.Helper()
	raw, ok := snapshot.Build([]snapshot.Entry{
		{Name: snapshot.EntrySettings, Body: []byte("#v2\ntheme=amber\nwm=tabwm\n")},
		{Name: snapshot.EntrySession, Body: []byte{0x00, 0xff, 0x0a, 0x10, 0x20}},
		{Name: snapshot.DocsPrefix + "NOTE.TXT", Body: []byte("note body\n")},
	})
	if !ok {
		t.Fatal("Build")
	}
	f.put(bundlePath, raw)
	f.put(restoreFlag, []byte("restore\n"))
}

func TestCaptureCarriesSettingsSessionAndSortedDocs(t *testing.T) {
	f := newFakeShare()
	f.install(t)
	f.put(settings.Path, []byte("#v2\ntheme=dark\n"))
	f.put(sessionPath, []byte{1, 2, 3, 4})
	f.dirs[docsDir] = true
	// Seeded out of name order on purpose: a listing has no order, and the
	// capture's sort is what makes the encoded bundle a function of the
	// directory's CONTENTS.
	f.put(docsDir+"/ZETA.TXT", []byte("z\n"))
	f.put(docsDir+"/ALPHA.TXT", []byte("a\n"))
	f.put(docsDir+"/MID.TXT", []byte("m\n"))
	f.dirs[docsDir+"/SUB"] = true

	raw, entries, docs, ok := captureBundle()
	if !ok {
		t.Fatal("captureBundle refused a share that plainly has state")
	}
	if entries != 5 || docs != 3 {
		t.Fatalf("entries=%d docs=%d want 5/3", entries, docs)
	}
	b, ok, reason := snapshot.Parse(raw)
	if !ok {
		t.Fatalf("the capture wrote a bundle its own parser refuses: %s", reason)
	}
	got, found := b.Get(snapshot.EntrySettings)
	if !found || string(got) != "#v2\ntheme=dark\n" {
		t.Fatalf("settings entry = %q found=%v", got, found)
	}
	got, found = b.Get(snapshot.EntrySession)
	if !found || !bytes.Equal(got, []byte{1, 2, 3, 4}) {
		t.Fatalf("session entry = %v found=%v — the BINARY strip must survive", got, found)
	}
	sel := b.Docs()
	if len(sel) != 3 {
		t.Fatalf("docs selection = %d want 3", len(sel))
	}
	for i, want := range []string{"ALPHA.TXT", "MID.TXT", "ZETA.TXT"} {
		if sel[i].Name != want {
			t.Fatalf("docs[%d] = %q want %q (sorted, and a subdirectory is not a document)",
				i, sel[i].Name, want)
		}
	}
}

func TestSaveSnapshotPublishesTheBundleAndLogsAfterTheWrite(t *testing.T) {
	f := newFakeShare()
	f.install(t)
	f.put(settings.Path, []byte("#v2\n"))
	f.put(sessionPath, []byte{9})

	if !saveSnapshot() {
		t.Fatal("saveSnapshot reported failure")
	}
	if _, ok := f.files[bundlePath]; !ok {
		t.Fatal("saveSnapshot did not publish the bundle")
	}
	// The bundle is the ONLY thing a save touches: a snapshot must not
	// disturb the state it just captured.
	if len(f.writes()) != 1 || f.writes()[0] != bundlePath {
		t.Fatalf("a save published %v, want exactly [%s]", f.writes(), bundlePath)
	}
	// The marker is a CLAIM about the publish, so it must follow it.
	writeAt, logAt := -1, -1
	for i, op := range f.ops {
		if op == "write:"+bundlePath {
			writeAt = i
		}
		if strings.HasPrefix(op, "log:"+MarkerSnapshotSaved) && logAt < 0 {
			logAt = i
		}
	}
	if writeAt < 0 || logAt < 0 {
		t.Fatalf("missing ops: write=%d log=%d in %v", writeAt, logAt, f.ops)
	}
	if logAt < writeAt {
		t.Fatal("the `snapshot saved` marker was printed BEFORE the publish returned")
	}
	if got := f.lines(); len(got) != 1 ||
		!strings.HasPrefix(got[0], MarkerSnapshotSaved+"bytes=") {
		t.Fatalf("marker = %v", got)
	}
}

func TestSaveSnapshotSkipsUnusableDocumentsButKeepsTheState(t *testing.T) {
	f := newFakeShare()
	f.install(t)
	f.put(settings.Path, []byte("#v2\n"))
	f.put(sessionPath, []byte{9})
	f.dirs[docsDir] = true
	f.put(docsDir+"/GOOD.TXT", []byte("g\n"))
	// A share name the container would refuse (a space is outside the
	// charset) and one past the per-document cap. Neither may cost the
	// person their settings and session.
	f.put(docsDir+"/bad name.txt", []byte("b\n"))
	f.put(docsDir+"/BIG.TXT", bytes.Repeat([]byte("x"), maxDocBytes+1))

	raw, entries, docs, ok := captureBundle()
	if !ok {
		t.Fatal("one unusable document must not fail the whole capture")
	}
	if docs != 1 || entries != 3 {
		t.Fatalf("entries=%d docs=%d want 3/1", entries, docs)
	}
	b, ok, reason := snapshot.Parse(raw)
	if !ok {
		t.Fatalf("Parse: %s", reason)
	}
	if _, found := b.Get(snapshot.DocsPrefix + "bad name.txt"); found {
		t.Fatal("a document whose name the codec refuses went into the bundle anyway")
	}
	if _, found := b.Get(snapshot.DocsPrefix + "BIG.TXT"); found {
		t.Fatal("an over-cap document was captured; it must be skipped, never truncated")
	}
	if _, found := b.Get(snapshot.DocsPrefix + "GOOD.TXT"); !found {
		t.Fatal("the one usable document was dropped")
	}
}

func TestCaptureOnAnEmptyShareFailsHonestly(t *testing.T) {
	f := newFakeShare()
	f.install(t)
	// No settings, no session, no documents: there is no state to carry, and
	// a bundle claiming none is not a bundle. The save must say so rather
	// than publish an empty container and call it a snapshot.
	if saveSnapshot() {
		t.Fatal("an empty share must not report a successful snapshot")
	}
	if _, ok := f.files[bundlePath]; ok {
		t.Fatal("an empty capture published a bundle")
	}
	if got := f.lines(); len(got) != 1 || got[0] != MarkerSnapshotFail+"capture" {
		t.Fatalf("markers = %v, want the one capture-failure line", got)
	}
}

func TestRestoreIsANoOpWithoutTheRequest(t *testing.T) {
	f := newFakeShare()
	f.install(t)
	f.seedBundle(t)
	// Take the request away: a bundle merely EXISTING must never rewind a
	// boot, or every boot after a snapshot would replay it.
	delete(f.files, restoreFlag)

	if restoreSnapshot() {
		t.Fatal("restoreSnapshot ran with no request on the share")
	}
	if len(f.writes()) != 0 {
		t.Fatalf("a no-op restore wrote %v", f.writes())
	}
	if len(f.lines()) != 0 {
		t.Fatalf("a no-op restore printed %v", f.lines())
	}
	if _, ok := f.files[settings.Path]; ok {
		t.Fatal("a no-op restore created a settings file")
	}
}

func TestRestoreRehydratesEveryFileByteExact(t *testing.T) {
	f := newFakeShare()
	f.install(t)
	f.seedBundle(t)

	if !restoreSnapshot() {
		t.Fatal("restoreSnapshot refused a bundle Build wrote")
	}
	// Byte-exact is the card's word, so compare BYTES: a settings table that
	// came back "close enough" would decode to the same rows and still be a
	// failed restore.
	for _, name := range []string{settings.Path, sessionPath, docsDir + "/NOTE.TXT"} {
		if _, ok := f.files[name]; !ok {
			t.Fatalf("restore did not write %s (wrote %v)", name, f.writes())
		}
	}
	wantSettings := []byte("#v2\ntheme=amber\nwm=tabwm\n")
	if !bytes.Equal(f.files[settings.Path], wantSettings) {
		t.Fatalf("settings = %q want %q", f.files[settings.Path], wantSettings)
	}
	wantSession := []byte{0x00, 0xff, 0x0a, 0x10, 0x20}
	if !bytes.Equal(f.files[sessionPath], wantSession) {
		t.Fatalf("session = %v want %v", f.files[sessionPath], wantSession)
	}
	if !bytes.Equal(f.files[docsDir+"/NOTE.TXT"], []byte("note body\n")) {
		t.Fatalf("document = %q", f.files[docsDir+"/NOTE.TXT"])
	}
	// The request is consumed: a restore happens ONCE, not on every boot.
	if _, ok := f.files[restoreFlag]; ok {
		t.Fatal("the one-shot restore request survived a successful restore")
	}
	// The `restore` marker follows every publish it claims.
	lastWrite, restoreLog := -1, -1
	for i, op := range f.ops {
		if strings.HasPrefix(op, "write:") && !strings.HasSuffix(op, "=err") {
			lastWrite = i
		}
		if strings.HasPrefix(op, "log:"+MarkerSnapshotRestore) && restoreLog < 0 {
			restoreLog = i
		}
	}
	if restoreLog < 0 {
		t.Fatalf("no restore marker in %v", f.ops)
	}
	if restoreLog < lastWrite {
		t.Fatal("the `snapshot restore` marker preceded the last publish")
	}
}

// The load-bearing test of the card: a corrupt bundle is refused WHOLE. The
// cases are chosen so a partial application would be visible — in particular
// "valid settings entry, corrupt LAST entry", which is the shape a codec that
// streamed would happily half-apply.
func TestCorruptBundleIsRefusedWholeAndWritesNothing(t *testing.T) {
	good := []byte("#v2\ntheme=amber\n")
	doc := []byte("note body\n")

	cases := []struct {
		name    string
		corrupt []byte
		want    string
	}{
		{"no header", []byte("settings 5\nhello"), snapshot.ReasonHeader},
		{"newer header", []byte("#vb2 1\nsettings 5\nhello"), snapshot.ReasonHeader},
		{"body short", []byte(string(snapshot.Header(1)) + "settings 99\nshort"), snapshot.ReasonTruncated},
		{"not an entry header", []byte(string(snapshot.Header(1)) + "settings\n"), snapshot.ReasonEntryHeader},
		{"length over the cap", []byte(string(snapshot.Header(1)) + "settings 99999999\n"),
			snapshot.ReasonEntrySize},
		{"unknown entry", []byte(string(snapshot.Header(1)) + "mystery 1\nx"), snapshot.ReasonEntryName},
		{"traversal name", []byte(string(snapshot.Header(1)) + "docs/../etc 1\nx"), snapshot.ReasonEntryName},
		{
			// The important one: the FIRST entry is perfectly good and the
			// LAST is corrupt. Nothing may be written.
			name: "valid first entry, corrupt last",
			corrupt: append(append([]byte(string(snapshot.Header(2))),
				append([]byte("settings "+itoaTest(len(good))+"\n"), good...)...),
				[]byte("docs/BAD 40\nshort")...),
			want: snapshot.ReasonTruncated,
		},
		{
			name: "good settings, duplicate session",
			corrupt: append(append([]byte(string(snapshot.Header(2))),
				append([]byte("settings "+itoaTest(len(good))+"\n"), good...)...),
				append(append([]byte("session "+itoaTest(len(doc))+"\n"), doc...),
					append([]byte("session "+itoaTest(len(doc))+"\n"), doc...)...)...),
			want: snapshot.ReasonDuplicate,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f := newFakeShare()
			f.install(t)
			f.put(bundlePath, c.corrupt)
			f.put(restoreFlag, []byte("restore\n"))
			// A settings file already on the share, so "wrote nothing" is
			// distinguishable from "wrote the same bytes back".
			f.put(settings.Path, []byte("#v2\ntheme=dark\n"))

			if restoreSnapshot() {
				t.Fatal("a corrupt bundle was accepted")
			}
			if w := f.writes(); len(w) != 0 {
				t.Fatalf("a REFUSED bundle still wrote %v — it must be refused whole", w)
			}
			if got := f.files[settings.Path]; string(got) != "#v2\ntheme=dark\n" {
				t.Fatalf("the live settings file was disturbed: %q", got)
			}
			if len(f.lines()) != 1 {
				t.Fatalf("markers = %v, want exactly one refusal line", f.lines())
			}
			want := MarkerSnapshotBad + c.want
			if f.lines()[0] != want {
				t.Fatalf("marker = %q want %q", f.lines()[0], want)
			}
			// The request is consumed even on a refusal, so a bad bundle
			// cannot refuse on every later boot.
			if _, ok := f.files[restoreFlag]; ok {
				t.Fatal("the request survived a refused restore")
			}
		})
	}
}

func TestAbsentBundleReportsMissingAndWritesNothing(t *testing.T) {
	f := newFakeShare()
	f.install(t)
	// The request is staged but the bundle is not: an absent bundle is a
	// DIFFERENT diagnosis from a corrupt one, and both leave the seat on
	// its defaults.
	f.put(restoreFlag, []byte("restore\n"))
	f.put(settings.Path, []byte("#v2\ntheme=dark\n"))

	if restoreSnapshot() {
		t.Fatal("an absent bundle was accepted")
	}
	if w := f.writes(); len(w) != 0 {
		t.Fatalf("an absent bundle wrote %v", w)
	}
	if len(f.lines()) != 1 || f.lines()[0] != MarkerSnapshotMissing {
		t.Fatalf("markers = %v, want the one missing line", f.lines())
	}
	if _, ok := f.files[restoreFlag]; ok {
		t.Fatal("the request survived a restore with no bundle")
	}
}

func TestOversizeBundleIsRefusedWhole(t *testing.T) {
	f := newFakeShare()
	f.install(t)
	// One byte past the cap, valid header and all. Truncating it into
	// something that still parses would be the quiet failure; the stat gate
	// refuses it instead (the settings Load shape).
	f.put(bundlePath, bytes.Repeat([]byte("x"), snapshot.MaxBundle+1))
	f.put(restoreFlag, []byte("restore\n"))

	if restoreSnapshot() {
		t.Fatal("an oversize bundle was accepted")
	}
	if w := f.writes(); len(w) != 0 {
		t.Fatalf("an oversize bundle wrote %v", w)
	}
	if len(f.lines()) != 1 || f.lines()[0] != MarkerSnapshotBad+"oversize" {
		t.Fatalf("markers = %v", f.lines())
	}
}

func TestRestoreNamesAFailedPublishInsteadOfClaimingSuccess(t *testing.T) {
	f := newFakeShare()
	f.install(t)
	f.seedBundle(t)
	f.writeErr[settings.Path] = -5

	if restoreSnapshot() {
		t.Fatal("a failed publish reported success")
	}
	for _, l := range f.lines() {
		if strings.HasPrefix(l, MarkerSnapshotRestore) {
			t.Fatal("a partial restore printed the byte-exact restore marker")
		}
	}
	if len(f.lines()) != 1 || !strings.HasPrefix(f.lines()[0], MarkerSnapshotFail+"settings=-5") {
		t.Fatalf("markers = %v, want the one named failure", f.lines())
	}
}

func TestRestoreToleratesAnExistingDocsDirectory(t *testing.T) {
	f := newFakeShare()
	f.install(t)
	f.seedBundle(t)
	// "Already exists" is the normal case for a share that already has DOCS
	// and must not read as a failure.
	f.mkdirErr = vi.ErrFileExists
	if !restoreSnapshot() {
		t.Fatal("an existing DOCS directory failed the restore")
	}
	sawMkdir := false
	for _, op := range f.ops {
		if op == "mkdir:"+docsDir {
			sawMkdir = true
		}
	}
	if !sawMkdir {
		t.Fatal("the restore did not ensure the docs directory exists")
	}

	// A mkdir that fails for a REAL reason is named, not swallowed.
	f2 := newFakeShare()
	f2.install(t)
	f2.seedBundle(t)
	f2.mkdirErr = -6
	if restoreSnapshot() {
		t.Fatal("a failed mkdir reported success")
	}
	if len(f2.lines()) != 1 || !strings.HasPrefix(f2.lines()[0], MarkerSnapshotFail+"docs=-6") {
		t.Fatalf("markers = %v", f2.lines())
	}
}

func TestSnapshotMarkerShapes(t *testing.T) {
	cases := []struct{ got, want string }{
		{MarkerSnapshotSaved, "gotabwm: snapshot saved "},
		{MarkerSnapshotRestore, "gotabwm: snapshot restore "},
		{MarkerSnapshotBad, "gotabwm: snapshot bad "},
		{MarkerSnapshotMissing, "gotabwm: snapshot missing"},
		{MarkerSnapshotFail, "gotabwm: snapshot fail "},
		{bundlePath, "/host/SNAPSHOT.BUNDLE"},
		{restoreFlag, "/host/SNAPSHOT.RESTORE"},
		{docsDir, "/host/DOCS"},
	}
	for _, c := range cases {
		if c.got != c.want {
			t.Fatalf("marker = %q want %q", c.got, c.want)
		}
	}
}

func TestSnapshotTailShape(t *testing.T) {
	if got, want := snapshotTail(1234, 5, 3), "bytes=1234 entries=5 docs=3"; got != want {
		t.Fatalf("tail = %q want %q", got, want)
	}
}

func itoaTest(v int) string {
	if v == 0 {
		return "0"
	}
	var b []byte
	for v > 0 {
		b = append([]byte{byte('0' + v%10)}, b...)
		v /= 10
	}
	return string(b)
}
