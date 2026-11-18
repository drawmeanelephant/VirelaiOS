// Host pins for #3915: a directory drag carries a complete bounded tar, and
// a GOFILES window that receives the archive MIME validates it, saves it
// without replacing an existing file, and browses it in place. The walk, the
// read, the collision probe and the publish are the package's seams
// (carryListDir/carryReadFile/carryExists/carryPublish), so these tests pin
// the encoder, every bound and the receiver's refusals without a guest.
package main

import (
	"bytes"
	"errors"
	"strings"
	"testing"

	"virelai/tabapp"
	"virelai/vi"
)

// tabappOffer is the hook's own envelope shape: mime plus the staged bytes.
func tabappOffer(mime string, blob []byte) tabapp.DragOffer {
	return tabapp.DragOffer{Mime: mime, Blob: blob}
}

// carryFS is the fake share a carry walks: canonical dirs, real-path file
// bodies, and per-path injected listing/read faults.
type carryFS struct {
	dirs    map[string][]vi.DirItem
	files   map[string][]byte
	listErr map[string]error
	readErr map[string]int64
	reads   map[string]int
}

func newCarryFS() *carryFS {
	return &carryFS{
		dirs:    map[string][]vi.DirItem{},
		files:   map[string][]byte{},
		listErr: map[string]error{},
		readErr: map[string]int64{},
		reads:   map[string]int{},
	}
}

func (f *carryFS) dir(canon string, items ...vi.DirItem) {
	f.dirs[canon] = items
}

func (f *carryFS) file(real string, body []byte) {
	f.files[real] = body
}

func ditem(name string, isDir bool, size uint64) vi.DirItem {
	return vi.DirItem{Name: name, IsDir: isDir, Size: size}
}

// install swaps the carry seams for one test and restores them after.
func (f *carryFS) install(t *testing.T) {
	t.Helper()
	t.Cleanup(f.swap())
}

// swap installs the fake share and returns the restore.
func (f *carryFS) swap() func() {
	savedList, savedRead := carryListDir, carryReadFile
	carryListDir = func(dir string) ([]vi.DirItem, error) {
		if err, ok := f.listErr[dir]; ok {
			return nil, err
		}
		items, ok := f.dirs[dir]
		if !ok {
			return nil, errors.New("fixture: unexpected ListDirAt(" + dir + ")")
		}
		return items, nil
	}
	carryReadFile = func(path string, max int) ([]byte, int64) {
		f.reads[path]++
		if rc, ok := f.readErr[path]; ok {
			return nil, rc
		}
		body, ok := f.files[path]
		if !ok {
			return nil, -int64(vi.ErrENOENT)
		}
		if len(body) > max {
			// A growing read shows the over-listing tail: the caller must
			// notice len != listed size.
			return body[:max], int64(max)
		}
		return body, int64(len(body))
	}
	return func() { carryListDir, carryReadFile = savedList, savedRead }
}

// tarMembers decodes a built carry through the app's own reader (the same
// parser the archive browser runs) and returns each member's bytes.
func tarMembers(t *testing.T, blob []byte) map[string][]byte {
	t.Helper()
	ents, streamed, refuse := tarParse(&memTarSrc{buf: blob})
	if refuse != "" {
		t.Fatalf("built tar does not parse: %s", refuse)
	}
	if streamed != int64(len(blob)) {
		t.Fatalf("parser consumed %d of %d bytes", streamed, len(blob))
	}
	out := map[string][]byte{}
	for _, e := range ents {
		if e.cls == tarClsDir {
			out[e.name+"/"] = nil
			continue
		}
		arc := &tarArc{img: blob}
		body, _ := arc.readEnt(e, int(e.size)+1)
		out[e.name] = body
	}
	return out
}

// The carry is the whole subtree: the root member, nested files with their
// exact bytes, and empty directories.
func TestDirectoryCarryCompleteSubtree(t *testing.T) {
	fs := newCarryFS()
	fs.dir("/CARRY/SUB",
		ditem("INNER.TXT", false, 18),
		ditem("EMPTY", true, 0),
		ditem("DEEP", true, 0),
		ditem("ZED.TXT", false, 4),
	)
	fs.dir("/CARRY/SUB/DEEP",
		ditem("NESTED.BIN", false, 9),
		ditem("MORE.TXT", false, 6),
	)
	fs.dir("/CARRY/SUB/EMPTY")
	fs.file("/host/CARRY/SUB/INNER.TXT", []byte("carry-inner-alpha\n"))
	fs.file("/host/CARRY/SUB/ZED.TXT", []byte("zed\n"))
	fs.file("/host/CARRY/SUB/DEEP/NESTED.BIN", []byte("nested-99"))
	fs.file("/host/CARRY/SUB/DEEP/MORE.TXT", []byte("more\r\n"))
	fs.install(t)

	m := testModel(entry("SUB", true))
	m.path = "/host/CARRY"
	m.pressRow = 0
	mt, blob, ok := m.dragOffer()
	if !ok {
		t.Fatalf("directory carry refused: %v", m.pending)
	}
	if mt != "archive" {
		t.Fatalf("mime = %q, want archive", mt)
	}
	members := tarMembers(t, blob)
	want := []string{"SUB/", "SUB/DEEP/", "SUB/DEEP/MORE.TXT",
		"SUB/DEEP/NESTED.BIN", "SUB/EMPTY/", "SUB/INNER.TXT", "SUB/ZED.TXT"}
	for _, name := range want {
		if _, ok := members[name]; !ok {
			t.Fatalf("member %q missing; got %v", name, memberNames(members))
		}
	}
	if got := members["SUB/INNER.TXT"]; !bytes.Equal(got, fs.files["/host/CARRY/SUB/INNER.TXT"]) {
		t.Fatalf("INNER.TXT bytes = %q", got)
	}
	if got := members["SUB/DEEP/NESTED.BIN"]; !bytes.Equal(got, []byte("nested-99")) {
		t.Fatalf("NESTED.BIN bytes = %q", got)
	}
	// The literal encoded total: two terminator blocks plus every header and
	// padded body.
	total := uint64(1024)
	for _, name := range want {
		total += 512
		if body := members[name]; body != nil {
			total += paddedSize(uint64(len(body)))
		} else if strings.HasSuffix(name, "/") {
			// directory member: header only, already counted
			continue
		}
	}
	if uint64(len(blob)) != total {
		t.Fatalf("encoded length %d != formula total %d", len(blob), total)
	}
}

// The walk consumes the complete listing API, not this app's visible 16-row
// window: a directory with 24 entries carries all 24 (m.entries would hold
// 16) with their exact bytes.
func TestDirectoryCarryBeyondVisibleWindow(t *testing.T) {
	fs := newCarryFS()
	var items []vi.DirItem
	for i := 0; i < 24; i++ {
		name := "F" + vi.Itoa64(int64(10+i)) + ".TXT" // F10..F33, name-sorted
		body := []byte(name + "\n")
		items = append(items, ditem(name, false, uint64(len(body))))
		fs.file("/host/CARRY/SUB/"+name, body)
	}
	fs.dir("/CARRY/SUB", items...)
	fs.install(t)

	m := testModel(entry("SUB", true))
	m.path = "/host/CARRY"
	m.pressRow = 0
	_, blob, ok := m.dragOffer()
	if !ok {
		t.Fatalf("carry refused: %v", m.pending)
	}
	members := tarMembers(t, blob)
	if len(members) != 25 { // the root plus 24 files
		t.Fatalf("members = %d, want 25 (%v)", len(members), memberNames(members))
	}
	for _, it := range items {
		got, ok := members["SUB/"+it.Name]
		if !ok {
			t.Fatalf("member SUB/%s missing", it.Name)
		}
		if want := fs.files["/host/CARRY/SUB/"+it.Name]; !bytes.Equal(got, want) {
			t.Fatalf("%s bytes = %q, want %q", it.Name, got, want)
		}
	}
}

func memberNames(m map[string][]byte) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

// The budget is the whole encoded archive: a carry of exactly 65536 bytes is
// admitted (65536 == vi.DragMax, so the seat's own bound still stages it).
func TestDirectoryCarryAdmitsExactCap(t *testing.T) {
	fs := newCarryFS()
	const body = 63488 // 1024 terminators + 512 root + 512 header + 63488
	fs.dir("/CARRY/SUB", ditem("FULL.BIN", false, body))
	fs.file("/host/CARRY/SUB/FULL.BIN", bytes.Repeat([]byte{'x'}, body))
	fs.install(t)

	m := testModel(entry("SUB", true))
	m.path = "/host/CARRY"
	m.pressRow = 0
	_, blob, ok := m.dragOffer()
	if !ok {
		t.Fatalf("exact-cap carry refused: %v", m.pending)
	}
	if len(blob) != 65536 {
		t.Fatalf("encoded length = %d, want exactly 65536", len(blob))
	}
	if len(blob) > vi.DragMax {
		t.Fatalf("encoded length %d exceeds DragMax %d", len(blob), vi.DragMax)
	}
}

// One byte of body past the exact fit pads over the cap: the refusal names
// 65536, and the builder never reads a body it cannot deliver.
func TestDirectoryCarryRefusesOverCap(t *testing.T) {
	fs := newCarryFS()
	fs.dir("/CARRY/SUB", ditem("OVER.BIN", false, 63489))
	fs.file("/host/CARRY/SUB/OVER.BIN", bytes.Repeat([]byte{'x'}, 63489))
	fs.install(t)

	m := testModel(entry("SUB", true))
	m.path = "/host/CARRY"
	m.pressRow = 0
	if _, _, ok := m.dragOffer(); ok {
		t.Fatal("an over-cap archive must refuse")
	}
	assertPending(t, &m, markerDragNo+"SUB archive-over-65536")
	if n := fs.reads["/host/CARRY/SUB/OVER.BIN"]; n != 0 {
		t.Fatalf("over-cap carry read the body %d times", n)
	}
}

// An incomplete listing refuses instead of carrying a prefix.
func TestDirectoryCarryRefusesIncompleteListing(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want string
	}{
		{"tree", vi.ErrTreeLimit, "SUB tree"},
		{"list", errors.New("boom"), "SUB list"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			fs := newCarryFS()
			fs.dir("/CARRY/SUB", ditem("DEEP", true, 0))
			fs.dir("/CARRY/SUB/DEEP")
			fs.listErr["/CARRY/SUB/DEEP"] = c.err
			fs.install(t)
			m := testModel(entry("SUB", true))
			m.path = "/host/CARRY"
			m.pressRow = 0
			if _, _, ok := m.dragOffer(); ok {
				t.Fatal("an incomplete walk must refuse")
			}
			assertPending(t, &m, markerDragNo+c.want)
		})
	}
}

// An unreadable body refuses before any bytes are offered.
func TestDirectoryCarryRefusesUnreadableBody(t *testing.T) {
	fs := newCarryFS()
	fs.dir("/CARRY/SUB", ditem("LOCKED.TXT", false, 5))
	fs.readErr["/host/CARRY/SUB/LOCKED.TXT"] = -int64(vi.ErrEACCES)
	fs.install(t)

	m := testModel(entry("SUB", true))
	m.path = "/host/CARRY"
	m.pressRow = 0
	if _, _, ok := m.dragOffer(); ok {
		t.Fatal("an unreadable body must refuse")
	}
	assertPending(t, &m, markerDragNo+"SUB read rc="+vi.Itoa64(-int64(vi.ErrEACCES)))
}

// A body that no longer matches the listing (shrunk or grown) refuses: the
// archive would otherwise carry a truncated or silently clipped member.
func TestDirectoryCarryRefusesChangingBody(t *testing.T) {
	for _, c := range []struct {
		name string
		body []byte
	}{
		{"shrunk", []byte("short")},
		{"grown", []byte("much-longer-than-listed")},
	} {
		t.Run(c.name, func(t *testing.T) {
			fs := newCarryFS()
			fs.dir("/CARRY/SUB", ditem("MOVE.TXT", false, 10))
			fs.file("/host/CARRY/SUB/MOVE.TXT", c.body)
			fs.install(t)
			m := testModel(entry("SUB", true))
			m.path = "/host/CARRY"
			m.pressRow = 0
			if _, _, ok := m.dragOffer(); ok {
				t.Fatal("a changing body must refuse")
			}
			assertPending(t, &m, markerDragNo+"SUB changed")
		})
	}
}

// Pseudo-listings and volumes are not complete-walk trees.
func TestDirectoryCarryRefusesUnsupportedRoots(t *testing.T) {
	fs := newCarryFS()
	fs.install(t)
	m := testModel(entry("SUB", true))
	m.pressRow = 0
	m.path = "/"
	if _, _, ok := m.dragOffer(); ok {
		t.Fatal("the volumes root must refuse")
	}
	assertPending(t, &m, markerDragNo+"SUB volume")

	m.path = favsView
	if _, _, ok := m.dragOffer(); ok {
		t.Fatal("the favorites pseudo-listing must refuse")
	}
	assertPending(t, &m, markerDragNo+"SUB favs")

	m.path = "/usb1"
	if _, _, ok := m.dragOffer(); ok {
		t.Fatal("a USB volume must refuse")
	}
	assertPending(t, &m, markerDragNo+"SUB volume")

	m.path = "/host/CARRY"
	m.tar = &tarArc{name: "A.TAR"}
	if _, _, ok := m.dragOffer(); ok {
		t.Fatal("a member inside an archive must refuse")
	}
	assertPending(t, &m, markerDragNo+"SUB tar")
}

// A child name that cannot be a tar member refuses rather than being
// rewritten.
func TestDirectoryCarryRefusesInvalidMemberName(t *testing.T) {
	fs := newCarryFS()
	fs.dir("/CARRY/SUB", ditem("BAD/NAME", false, 3))
	fs.install(t)
	m := testModel(entry("SUB", true))
	m.path = "/host/CARRY"
	m.pressRow = 0
	if _, _, ok := m.dragOffer(); ok {
		t.Fatal("an invalid member name must refuse")
	}
	assertPending(t, &m, markerDragNo+"SUB path")
}

// A landing publishes a valid tar under a root-derived name and opens it
// with the existing browser. The real publisher runs over an in-memory
// share: one independently owned stage, one non-replacing rename, and the
// pre-existing collision keeps its bytes.
func TestArchiveDropSavesAndBrowses(t *testing.T) {
	blob, root := buildTestCarry(t)
	fs := newCarryFS()
	fs.dir("/FMR", ditem("SUB.TAR", false, 4), ditem("KEEP.TXT", false, 3))
	fs.file("/host/FMR/SUB.TAR", []byte("old\n"))
	fs.install(t)
	prior := []byte("old\n")
	s := newCarryStore()
	s.files["/host/FMR/SUB.TAR"] = append([]byte(nil), prior...)
	s.files["/host/FMR/KEEP.TXT"] = []byte("keep")
	s.install(t)
	s.tarOpenStore(t)
	t.Cleanup(func() { dropFresh = false })

	m := testModel()
	m.path = "/host/FMR"
	m.dropOffer(tabappOffer("archive", blob))
	if !dropFresh {
		t.Fatal("a landing must earn a repaint")
	}
	dest := "/host/FMR/SUB-1.TAR"
	if !bytes.Equal(s.files[dest], blob) {
		t.Fatalf("saved bytes differ from the offered archive (%d files)", len(s.files))
	}
	if !bytes.Equal(s.files["/host/FMR/SUB.TAR"], prior) {
		t.Fatal("the colliding destination lost its bytes")
	}
	if len(s.writes) != 1 {
		t.Fatalf("writes = %v, want exactly one stage", s.writes)
	}
	stage := s.writes[0]
	if stage == dest || stage == "/host/FMR/SUB.TAR" {
		t.Fatalf("stage %q aliases a destination", stage)
	}
	if _, ok := s.files[stage]; ok {
		t.Fatalf("the stage %q survived publication", stage)
	}
	if len(s.renames) != 1 || s.renames[0] != [2]string{stage, dest} {
		t.Fatalf("renames = %v, want the stage published to %q", s.renames, dest)
	}
	frame := pendingJoined(&m)
	if !strings.Contains(frame, markerDropSaved+"SUB-1.TAR bytes=") {
		t.Fatalf("missing save receipt:\n%s", frame)
	}
	if m.tar == nil || m.path != dest+"/" {
		t.Fatalf("saved archive not browsed: tar=%v path=%q", m.tar, m.path)
	}
	members := map[string]bool{}
	for _, e := range m.tar.ents {
		members[e.name] = true
	}
	for _, want := range []string{root, root + "/DEEP", root + "/EMPTY",
		root + "/DEEP/NESTED.TXT"} {
		if !members[want] {
			t.Fatalf("browsed archive lacks %q: %v", want, members)
		}
	}
	if !strings.Contains(frame, markerTarOpen+"SUB-1.TAR entries=") {
		t.Fatalf("missing browse receipt:\n%s", frame)
	}
}

// A destination that is not a real writable directory refuses; nothing is
// published.
func TestArchiveDropRefusesPseudoDestination(t *testing.T) {
	blob, _ := buildTestCarry(t)
	fs := newCarryFS()
	fs.install(t)
	savedPublish := carryPublish
	published := 0
	carryPublish = func(string, []byte) int64 { published++; return 0 }
	t.Cleanup(func() { carryPublish = savedPublish; dropFresh = false })

	for _, c := range []struct {
		path string
		tar  *tarArc
		want string
	}{
		{"/", nil, "volume"},
		{favsView, nil, "favs"},
		{"/host/FMR/X.TAR/", &tarArc{name: "X.TAR"}, "tar"},
		{"/disk", nil, "dest"},
		{"/usb1", nil, "dest"},
	} {
		m := testModel()
		m.path = c.path
		m.tar = c.tar
		m.dropOffer(tabappOffer("archive", blob))
		frame := pendingJoined(&m)
		if !strings.Contains(frame, markerDropNo+c.want) {
			t.Fatalf("%q: missing refusal %q in:\n%s", c.path, c.want, frame)
		}
	}
	if published != 0 {
		t.Fatalf("refused drops published %d files", published)
	}
}

// An invalid or incomplete tar never reaches publication.
func TestArchiveDropRefusesInvalidTar(t *testing.T) {
	valid, _ := buildTestCarry(t)
	// A tar whose file member's parent directory record is missing: the
	// browser would imply it, but a truthful carry must name it.
	incomplete := buildTarForTest(t, []testTarEnt{
		{"SUB", true, nil},
		{"SUB/DEEP/NESTED.TXT", false, []byte("orphan")},
	})
	cases := []struct {
		name string
		blob []byte
		want string
	}{
		{"garbage", []byte("not a tar at all"), "invalid"},
		{"truncated", valid[:len(valid)-600], "invalid"},
		{"trailing", append(append([]byte(nil), valid...), []byte("junk")...), "invalid"},
		{"incomplete", incomplete, "incomplete"},
		{"empty", []byte{}, "invalid"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			fs := newCarryFS()
			fs.dir("/FMR")
			fs.install(t)
			savedPublish := carryPublish
			published := 0
			carryPublish = func(string, []byte) int64 { published++; return 0 }
			t.Cleanup(func() { carryPublish = savedPublish; dropFresh = false })
			m := testModel()
			m.path = "/host/FMR"
			m.dropOffer(tabappOffer("archive", c.blob))
			frame := pendingJoined(&m)
			if !strings.Contains(frame, markerDropNo+c.want) {
				t.Fatalf("%s: missing refusal %q in:\n%s", c.name, c.want, frame)
			}
			if published != 0 {
				t.Fatalf("%s: invalid tar was published", c.name)
			}
		})
	}
}

// A failed publication refuses by the kernel's rc, cleans up its own stage
// and never opens a browser over a destination that does not exist.
func TestArchiveDropRefusesPublishFailure(t *testing.T) {
	blob, _ := buildTestCarry(t)
	fs := newCarryFS()
	fs.dir("/FMR")
	fs.install(t)
	s := newCarryStore()
	s.failWrite = -int64(vi.ErrEIO)
	s.install(t)
	prevOpen := tarOpenSrc
	opened := 0
	tarOpenSrc = func(string) (tarSrc, int64) {
		opened++
		return nil, -int64(vi.ErrENOENT)
	}
	t.Cleanup(func() { tarOpenSrc = prevOpen; dropFresh = false })

	m := testModel()
	m.path = "/host/FMR"
	m.dropOffer(tabappOffer("archive", blob))
	frame := pendingJoined(&m)
	if !strings.Contains(frame, markerDropNo+"write rc=") {
		t.Fatalf("missing write refusal:\n%s", frame)
	}
	if opened != 0 || m.tar != nil || m.path != "/host/FMR" {
		t.Fatalf("failed publish left the receiver in opened=%d tar=%v path=%q",
			opened, m.tar, m.path)
	}
	if len(s.writes) != 1 {
		t.Fatalf("writes = %v, want one attempted stage", s.writes)
	}
	if len(s.deletes) != 1 || s.deletes[0] != s.writes[0] {
		t.Fatalf("deletes = %v, want the failed stage %q", s.deletes, s.writes[0])
	}
	if len(s.files) != 0 {
		t.Fatalf("a failed publish left files behind: %v", s.files)
	}
}

// A vCard drop still runs the contact face: the archive receiver did not
// take over the existing path.
func TestVCardDropStillRoutesToContact(t *testing.T) {
	m := testModel()
	m.path = "/host/FMR"
	m.dropOffer(tabappOffer("text/vcard", []byte("BEGIN:VCARD\r\nFN:Grace\r\nEND:VCARD\r\n")))
	frame := pendingJoined(&m)
	if !strings.Contains(frame, "gofiles: contact") {
		t.Fatalf("vCard drop did not reach the contact face:\n%s", frame)
	}
	dropFresh = false
}

// carryStore is an in-memory host share behind the publisher's own
// primitives. Tests point carryWriteFile/carryRename/carryDelete/carryNonce/
// carryExists at it, so a landing runs the SHIPPED carryPublish — the stage
// write, the no-replace rename and the cleanup are the production sequence,
// not a replacement for it. A rename over a live name is refused, as the
// kernel's preserve-existing publish refuses it (-9).
type carryStore struct {
	files     map[string][]byte
	writes    []string
	renames   [][2]string
	deletes   []string
	nonces    []uint32
	onWrite   func(path string)
	onRename  func(from, to string)
	failWrite int64
}

func newCarryStore() *carryStore {
	return &carryStore{files: map[string][]byte{}}
}

func (s *carryStore) install(t *testing.T) {
	t.Helper()
	prevWrite, prevRename := carryWriteFile, carryRename
	prevDelete, prevNonce, prevExists := carryDelete, carryNonce, carryExists
	carryWriteFile = func(path string, data []byte) int64 {
		s.writes = append(s.writes, path)
		if s.failWrite != 0 {
			return s.failWrite
		}
		if s.onWrite != nil {
			s.onWrite(path)
		}
		s.files[path] = append([]byte(nil), data...)
		return 0
	}
	carryRename = func(from, to string) int64 {
		s.renames = append(s.renames, [2]string{from, to})
		if s.onRename != nil {
			s.onRename(from, to)
		}
		if _, exists := s.files[to]; exists {
			return vi.ErrFileExists
		}
		body, ok := s.files[from]
		if !ok {
			return -int64(vi.ErrENOENT)
		}
		s.files[to] = body
		delete(s.files, from)
		return 0
	}
	carryDelete = func(path string) int64 {
		s.deletes = append(s.deletes, path)
		if _, ok := s.files[path]; !ok {
			return -int64(vi.ErrENOENT)
		}
		delete(s.files, path)
		return 0
	}
	carryNonce = func() uint32 {
		if len(s.nonces) == 0 {
			return 0
		}
		n := s.nonces[0]
		s.nonces = s.nonces[1:]
		return n
	}
	carryExists = func(path string) bool { _, ok := s.files[path]; return ok }
	t.Cleanup(func() {
		carryWriteFile, carryRename = prevWrite, prevRename
		carryDelete, carryNonce, carryExists = prevDelete, prevNonce, prevExists
	})
}

func (s *carryStore) deleted(path string) bool {
	for _, p := range s.deletes {
		if p == path {
			return true
		}
	}
	return false
}

// tarOpenStore points the archive browser at the same in-memory share.
func (s *carryStore) tarOpenStore(t *testing.T) {
	t.Helper()
	prev := tarOpenSrc
	tarOpenSrc = func(path string) (tarSrc, int64) {
		body, ok := s.files[path]
		if !ok {
			return nil, -int64(vi.ErrENOENT)
		}
		return &memTarSrc{buf: body}, 0
	}
	t.Cleanup(func() { tarOpenSrc = prev })
}

// The publisher itself never touches a live destination. A writer that lands
// the name first keeps its exact bytes; the losing stage is removed and the
// caller gets the kernel's EEXIST so it refuses by name.
func TestArchivePublishCollisionKeepsWinnerBytes(t *testing.T) {
	const dest = "/host/FMR/SUB.TAR"
	winner := []byte("COMPETING-WRITER-BYTES\n")
	s := newCarryStore()
	s.files[dest] = append([]byte(nil), winner...)
	s.install(t)

	if rc := carryPublish(dest, []byte("OUR-ARCHIVE")); rc != vi.ErrFileExists {
		t.Fatalf("publish rc = %d, want the kernel's EEXIST %d", rc, vi.ErrFileExists)
	}
	if !bytes.Equal(s.files[dest], winner) {
		t.Fatalf("the winner's bytes changed: %q", s.files[dest])
	}
	if len(s.writes) != 1 {
		t.Fatalf("writes = %v, want exactly one stage", s.writes)
	}
	stage := s.writes[0]
	if stage == dest || stage == dest+"~" {
		t.Fatalf("stage %q is the destination or its shared temp", stage)
	}
	if !strings.HasPrefix(stage, "/host/FMR/") {
		t.Fatalf("stage %q is not a sibling of the destination", stage)
	}
	if _, ok := s.files[stage]; ok {
		t.Fatalf("the losing stage %q survived", stage)
	}
	if len(s.deletes) != 1 || s.deletes[0] != stage {
		t.Fatalf("deletes = %v, want only the losing stage %q", s.deletes, stage)
	}
	for _, op := range s.renames {
		if op[0] != stage || op[1] != dest {
			t.Fatalf("rename %v does not publish exactly the stage to dest", op)
		}
	}
	if s.deleted(dest) {
		t.Fatal("the publisher deleted the destination")
	}
}

// A competing writer that lands the chosen name AFTER the probe wins. The
// receiver refuses the collision, does not open a browser over another
// writer's file and leaves no partial archive behind.
func TestArchiveDropRefusesCollisionAfterProbe(t *testing.T) {
	blob, _ := buildTestCarry(t)
	const dest = "/host/FMR/SUB.TAR"
	competitor := []byte("COMPETITOR-AFTER-PROBE\n")
	fs := newCarryFS()
	fs.dir("/FMR")
	fs.install(t)
	s := newCarryStore()
	s.onRename = func(_, to string) {
		if to == dest {
			s.files[to] = append([]byte(nil), competitor...)
		}
	}
	s.install(t)
	opened := 0
	prevOpen := tarOpenSrc
	tarOpenSrc = func(string) (tarSrc, int64) {
		opened++
		return nil, -int64(vi.ErrENOENT)
	}
	t.Cleanup(func() { tarOpenSrc = prevOpen; dropFresh = false })

	m := testModel()
	m.path = "/host/FMR"
	m.dropOffer(tabappOffer("archive", blob))
	frame := pendingJoined(&m)
	if !strings.Contains(frame, markerDropNo+"collision") {
		t.Fatalf("missing collision refusal:\n%s", frame)
	}
	if strings.Contains(frame, markerDropSaved) || strings.Contains(frame, markerTarOpen) {
		t.Fatalf("a collision claimed a save or a browse:\n%s", frame)
	}
	if opened != 0 || m.tar != nil || m.path != "/host/FMR" {
		t.Fatalf("collision opened a browser: opened=%d tar=%v path=%q",
			opened, m.tar, m.path)
	}
	if !bytes.Equal(s.files[dest], competitor) {
		t.Fatalf("the competitor's bytes changed to %q", s.files[dest])
	}
	if len(s.writes) != 1 {
		t.Fatalf("writes = %v, want one stage", s.writes)
	}
	if _, ok := s.files[s.writes[0]]; ok {
		t.Fatalf("the losing stage %q survived", s.writes[0])
	}
	if s.deleted(dest) {
		t.Fatal("the receiver deleted another writer's destination")
	}
}

// Two drops that both probed the same free name interleave: each owns a
// distinct stage, the first completed publish wins with its exact bytes, and
// the other refuses the collision instead of deleting or overwriting it.
func TestArchiveDropInterleavedCollisionOwnsStages(t *testing.T) {
	blobA := buildTarForTest(t, []testTarEnt{
		{"SUB", true, nil},
		{"SUB/A.TXT", false, []byte("loser-alpha\n")},
	})
	blobB := buildTarForTest(t, []testTarEnt{
		{"SUB", true, nil},
		{"SUB/B.TXT", false, []byte("winner-bravo\n")},
	})
	const dest = "/host/FMR/SUB.TAR"
	fs := newCarryFS()
	fs.dir("/FMR")
	fs.install(t)
	s := newCarryStore()
	s.install(t)
	s.tarOpenStore(t)
	mA := testModel()
	mA.path = "/host/FMR"
	mB := testModel()
	mB.path = "/host/FMR"
	triggered := false
	// The second drop runs while the first is between its stage write and
	// its publish, so both name probes see SUB.TAR free.
	s.onWrite = func(string) {
		if triggered {
			return
		}
		triggered = true
		mB.dropOffer(tabappOffer("archive", blobB))
	}
	t.Cleanup(func() { dropFresh = false })

	mA.dropOffer(tabappOffer("archive", blobA))

	if !bytes.Equal(s.files[dest], blobB) {
		t.Fatalf("destination bytes are not the winner's exact archive")
	}
	if len(s.writes) != 2 {
		t.Fatalf("writes = %v, want two independently owned stages", s.writes)
	}
	stageA, stageB := s.writes[0], s.writes[1]
	if stageA == stageB {
		t.Fatalf("both drops shared one stage: %q", stageA)
	}
	for _, stage := range []string{stageA, stageB} {
		if stage == dest || stage == dest+"~" {
			t.Fatalf("stage %q aliases the destination or its shared temp", stage)
		}
		if _, ok := s.files[stage]; ok {
			t.Fatalf("stage %q survived the publish", stage)
		}
	}
	if !strings.Contains(pendingJoined(&mB), markerDropSaved+"SUB.TAR bytes=") {
		t.Fatalf("the winning drop did not save:\n%s", pendingJoined(&mB))
	}
	if mB.tar == nil || mB.path != dest+"/" {
		t.Fatalf("the winning drop did not browse: tar=%v path=%q", mB.tar, mB.path)
	}
	loser := pendingJoined(&mA)
	if !strings.Contains(loser, markerDropNo+"collision") {
		t.Fatalf("the losing drop did not refuse the collision:\n%s", loser)
	}
	if mA.tar != nil || mA.path != "/host/FMR" {
		t.Fatalf("the losing drop opened a browser: tar=%v path=%q", mA.tar, mA.path)
	}
	if len(s.deletes) != 1 || s.deletes[0] != stageA {
		t.Fatalf("deletes = %v, want only the losing stage %q", s.deletes, stageA)
	}
	if s.deleted(dest) {
		t.Fatal("an interleaved drop deleted the winner's destination")
	}
}

// A destination directory with no room for an independently owned stage
// refuses by path; nothing is written and no live file is touched.
func TestArchivePublishRefusesExhaustedStageRoom(t *testing.T) {
	s := newCarryStore()
	s.install(t)
	dir := "/host/" + strings.Repeat("D", 54-len("/host/"))
	dest := dir + "/A"
	if len(dest) > maxPath {
		t.Fatalf("fixture dest %q is over the path cap", dest)
	}
	if rc := carryPublish(dest, []byte("ARCHIVE")); rc != -vi.ErrENAMETOOLONG {
		t.Fatalf("publish rc = %d, want ENAMETOOLONG %d", rc, -vi.ErrENAMETOOLONG)
	}
	if len(s.writes) != 0 || len(s.deletes) != 0 || len(s.renames) != 0 {
		t.Fatalf("a refused publish wrote/renamed/deleted: %v %v %v",
			s.writes, s.renames, s.deletes)
	}
	if _, ok := s.files[dest]; ok {
		t.Fatal("a refused publish created the destination")
	}
}

// buildTestCarry runs the real sender over a small tree and returns the
// encoded archive plus its root name.
func buildTestCarry(t *testing.T) ([]byte, string) {
	t.Helper()
	fs := newCarryFS()
	fs.dir("/FMC/SUB",
		ditem("DEEP", true, 0),
		ditem("EMPTY", true, 0),
		ditem("INNER.TXT", false, 18),
	)
	fs.dir("/FMC/SUB/DEEP", ditem("NESTED.TXT", false, 7))
	fs.dir("/FMC/SUB/EMPTY")
	fs.file("/host/FMC/SUB/INNER.TXT", []byte("carry-inner-alpha\n"))
	fs.file("/host/FMC/SUB/DEEP/NESTED.TXT", []byte("nested\n"))
	restore := fs.swap()
	defer restore()

	m := model{path: "/host/FMC"}
	m.setSize(64, 23)
	m.entries[0] = entry("SUB", true)
	m.n = 1
	m.pressRow = 0
	mt, blob, ok := m.dragOffer()
	if !ok || mt != "archive" {
		t.Fatalf("fixture carry refused: mime=%q pending=%v", mt, m.pending)
	}
	return blob, "SUB"
}

type testTarEnt struct {
	name string
	dir  bool
	body []byte
}

// buildTarForTest lays out member records without the sender's completeness
// rules, so a receiver test can hand it an incomplete archive.
func buildTarForTest(t *testing.T, ents []testTarEnt) []byte {
	t.Helper()
	var out []byte
	for _, e := range ents {
		out = appendTarMember(out, e.name, e.dir, e.body)
	}
	out = append(out, make([]byte, 1024)...)
	return out
}
