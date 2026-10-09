package vfs

import (
	"errors"
	"io/fs"
	"strings"
	"testing"
	"unsafe"

	"virelai/gsport/fsys"
	"virelai/vi"
)

// fakeShare is a stateful in-memory kernel for the file slots: open/write
// truncates without append, rename refuses a live target (file-domain
// EEXIST), delete on an absent path is the ErrFileNotFound row, mkdir rides
// MODE_DIR create, and the handle table refuses the ninth open. That is
// the kernel contract gsport/vfs stands on — nothing more.
type fakeShare struct {
	files   map[string][]byte
	dirs    map[string]bool
	handles map[uint32]*fakeHandle
	next    uint32
	slots   []int
	dead    bool // crash simulation: every call returns ENOSYS
}

type fakeHandle struct {
	path   string
	write  bool
	cursor int
}

func newFakeShare() *fakeShare {
	k := &fakeShare{
		files:   map[string][]byte{},
		dirs:    map[string]bool{"/host": true},
		handles: map[uint32]*fakeHandle{},
	}
	return k
}

func strArg(p, n uintptr) string {
	return string(unsafe.Slice((*byte)(unsafe.Pointer(p)), int(n)))
}

func (k *fakeShare) hook(num uintptr, a0, a1, a2, a3 uintptr) int64 {
	k.slots = append(k.slots, int(num))
	if k.dead {
		return -vi.ErrENOSYS
	}
	switch num {
	case vi.SlotFileOpen: // 23
		path := strArg(a0, a1)
		flags := uint32(a2)
		if len(k.handles) >= 8 {
			return -vi.ErrENOSPC
		}
		if flags&vi.ModeDir != 0 {
			if flags&vi.ModeCreate != 0 {
				if k.dirs[path] {
					return vi.ErrFileExists
				}
				if _, ok := k.files[path]; ok {
					return vi.ErrFileExists
				}
				k.dirs[path] = true
			}
			if !k.dirs[path] {
				return -vi.ErrENOENT
			}
		} else if flags&vi.ModeCreate != 0 {
			if k.dirs[path] {
				return -vi.ErrEINVAL
			}
			if flags&vi.ModeAppend == 0 {
				k.files[path] = nil // write-open truncates
			} else if _, ok := k.files[path]; !ok {
				k.files[path] = nil
			}
		} else {
			if _, ok := k.files[path]; !ok {
				return -vi.ErrENOENT
			}
		}
		k.next++
		k.handles[k.next] = &fakeHandle{path: path, write: flags&(vi.ModeWrite|vi.ModeCreate) != 0}
		return int64(k.next)
	case vi.SlotFileRead: // 24
		h := k.handles[uint32(a0)]
		if h == nil {
			return -vi.ErrEBADF
		}
		body := k.files[h.path]
		if h.cursor >= len(body) {
			return 0
		}
		n := int(a2)
		if len(body)-h.cursor < n {
			n = len(body) - h.cursor
		}
		copy(unsafe.Slice((*byte)(unsafe.Pointer(a1)), int(a2)), body[h.cursor:h.cursor+n])
		h.cursor += n
		return int64(n)
	case vi.SlotFileWrite: // 25
		h := k.handles[uint32(a0)]
		if h == nil {
			return -vi.ErrEBADF
		}
		b := unsafe.Slice((*byte)(unsafe.Pointer(a1)), int(a2))
		k.files[h.path] = append(k.files[h.path][:h.cursor], b...)
		h.cursor += len(b)
		return int64(len(b))
	case vi.SlotFileClose: // 26
		delete(k.handles, uint32(a0))
		return 0
	case vi.SlotDirList: // 27
		path := strArg(a0, a1)
		buf := unsafe.Slice((*vi.DirEntry)(unsafe.Pointer(a2)), int(a3))
		n := 0
		emit := func(name string, size int, dir bool) {
			if n >= len(buf) {
				return
			}
			copy(buf[n].Name[:], name)
			buf[n].Size = uint32(size)
			if dir {
				buf[n].IsDir = 1
			}
			n++
		}
		prefix := path + "/"
		for d := range k.dirs {
			if strings.HasPrefix(d, prefix) && !strings.Contains(d[len(prefix):], "/") {
				emit(d[len(prefix):], 0, true)
			}
		}
		for f, b := range k.files {
			if strings.HasPrefix(f, prefix) && !strings.Contains(f[len(prefix):], "/") {
				emit(f[len(prefix):], len(b), false)
			}
		}
		if n == 0 && !k.dirs[path] && path != "" {
			return -vi.ErrENOENT
		}
		return int64(n)
	case vi.SlotFileDelete: // 34
		path := strArg(a0, a1)
		if _, ok := k.files[path]; ok {
			delete(k.files, path)
			return 0
		}
		if k.dirs[path] {
			delete(k.dirs, path)
			return 0
		}
		return vi.ErrFileNotFound
	case vi.SlotFileRename: // 35
		from := strArg(a0, a1)
		to := strArg(a2, a3)
		if _, ok := k.files[to]; ok {
			return vi.ErrFileExists
		}
		if k.dirs[to] {
			return vi.ErrFileExists
		}
		if b, ok := k.files[from]; ok {
			k.files[to] = b
			delete(k.files, from)
			return 0
		}
		return -vi.ErrENOENT
	case vi.SlotFileSync: // 77
		if k.handles[uint32(a0)] == nil {
			return -vi.ErrEBADF
		}
		return 0
	}
	return -vi.ErrENOSYS
}

func (k *fakeShare) install(t *testing.T) {
	t.Helper()
	prev := vi.SetSyscallHookForTest(k.hook)
	oldPub, oldRec := PublishHook, RecoverHook
	PublishHook, RecoverHook = nil, nil
	t.Cleanup(func() {
		vi.SetSyscallHookForTest(prev)
		PublishHook, RecoverHook = oldPub, oldRec
	})
}

func (k *fakeShare) reset() { k.slots = nil }

// crashAt arms the hook to simulate a kill at the named publish stage: the
// kernel goes dead from that point (no cleanup runs — a kill runs nothing).
func (k *fakeShare) crashAt(stage string) {
	PublishHook = func(s string) {
		if s == stage {
			k.dead = true
		}
	}
}

func mustHost(t *testing.T, k *fakeShare) *HostFS {
	t.Helper()
	k.dirs["/host/GS/vfs"] = true
	h, err := NewHost("/host/GS/vfs")
	if err != nil {
		t.Fatalf("NewHost: %v", err)
	}
	k.reset() // NewHost's existence probe is setup, not slot evidence
	return h
}

// --- confinement --------------------------------------------------------

func TestEscapeRefusedByName(t *testing.T) {
	k := newFakeShare()
	k.install(t)
	h := mustHost(t, k)

	bad := []string{"../x", "a/../b", "..", "/abs", "a\\b", "x/.."}
	for _, name := range bad {
		for _, call := range []func(string) error{
			func(n string) error { return h.WriteFile(n, []byte("x"), 0o644) },
			func(n string) error { return h.MkdirAll(n) },
			func(n string) error { return h.Remove(n) },
			func(n string) error { _, e := h.Open(n); return e },
			func(n string) error { _, e := h.ReadFile(n); return e },
			func(n string) error { _, e := h.ReadDir(n); return e },
		} {
			if err := call(name); err == nil {
				t.Fatalf("name %q reached the kernel", name)
			}
		}
	}
	if len(k.slots) != 0 {
		t.Fatalf("escape attempts burned slots %v", k.slots)
	}
	if len(k.files) != 0 || len(k.dirs) != 2 {
		t.Fatalf("escape attempt mutated the share: files=%v dirs=%v", k.files, k.dirs)
	}
}

func TestBoundsRefusedNeverTruncated(t *testing.T) {
	k := newFakeShare()
	k.install(t)
	h := mustHost(t, k)

	long := strings.Repeat("n", 300)
	if err := h.WriteFile(long, nil, 0); err == nil {
		t.Fatal("300-byte component accepted")
	}
	deep := strings.Repeat("d/", 9) + "x" // root is depth 3; +9 > 8
	if err := h.WriteFile(deep, nil, 0); err == nil {
		t.Fatal("depth-12 path accepted")
	}
	if len(k.slots) != 0 {
		t.Fatalf("refused paths burned slots %v", k.slots)
	}
	// A name AT the component bound is legal — refused only past it.
	ok := strings.Repeat("n", 240)
	if err := h.WriteFile(ok, []byte("x"), 0); err != nil {
		t.Fatalf("240-byte name refused: %v", err)
	}
	if string(k.files["/host/GS/vfs/"+ok]) != "x" {
		t.Fatal("240-byte name file missing or wrong")
	}
}

func TestReservedSuffixRefused(t *testing.T) {
	k := newFakeShare()
	k.install(t)
	h := mustHost(t, k)

	if err := h.WriteFile("doc~", []byte("x"), 0); !errors.Is(err, fs.ErrInvalid) {
		t.Fatalf("WriteFile doc~: %v, want fs.ErrInvalid", err)
	}
	if err := h.Remove("doc~"); !errors.Is(err, fs.ErrInvalid) {
		t.Fatalf("Remove doc~: %v, want fs.ErrInvalid", err)
	}
	if err := h.MkdirAll("d~"); !errors.Is(err, fs.ErrInvalid) {
		t.Fatalf("MkdirAll d~: %v, want fs.ErrInvalid", err)
	}
	if len(k.slots) != 0 {
		t.Fatalf("reserved names burned slots %v", k.slots)
	}
}

// --- publish sequence ---------------------------------------------------

func TestPublishSlotOrder(t *testing.T) {
	k := newFakeShare()
	k.install(t)
	h := mustHost(t, k)

	if err := h.WriteFile("doc.txt", []byte("body"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	// recovery probes (two absent-probe opens), then the publish sequence:
	// create tmp, write, fsync, close, delete, rename.
	want := []int{23, 23, 23, 25, 77, 26, 34, 35}
	if len(k.slots) != len(want) {
		t.Fatalf("slots %v, want %v", k.slots, want)
	}
	for i := range want {
		if k.slots[i] != want[i] {
			t.Fatalf("slots %v, want %v", k.slots, want)
		}
	}
	if string(k.files["/host/GS/vfs/doc.txt"]) != "body" {
		t.Fatal("published body wrong")
	}
	if _, ok := k.files["/host/GS/vfs/doc.txt~"]; ok {
		t.Fatal("temp left behind")
	}
}

// --- crash points --------------------------------------------------------

// crash drills run Publish with the kernel dying at each stage; the state
// assertions afterwards are the recorded old-or-new contract.
func TestCrashPoints(t *testing.T) {
	cases := []struct {
		stage    string // hook stage at which the kernel dies
		wantFile string // what ReadFile on the target must return afterwards
	}{
		{"temp-open", "old"},  // dead before body lands: old intact
		{"temp-write", "old"}, // partial temp, target never touched
		{"temp-fsync", "old"}, // complete fsynced temp, target still old
		{"temp-close", "old"},
		{"target-delete", "new"}, // absent target + complete temp -> recover new
	}
	for _, tc := range cases {
		k := newFakeShare()
		k.install(t)
		h := mustHost(t, k)
		k.files["/host/GS/vfs/doc.txt"] = []byte("old")

		k.crashAt(tc.stage)
		// The write dies inside Publish; the process would be killed here.
		_ = h.WriteFile("doc.txt", []byte("new"), 0o644)
		PublishHook = nil
		k.dead = false // next boot: the kernel answers again

		got, err := h.ReadFile("doc.txt")
		if tc.wantFile == "new" {
			if err != nil || string(got) != "new" {
				t.Fatalf("stage %s: read %q err %v, want recovered \"new\"", tc.stage, got, err)
			}
		} else {
			if err != nil || string(got) != "old" {
				t.Fatalf("stage %s: read %q err %v, want \"old\"", tc.stage, got, err)
			}
		}
	}
}

// TestInPlaceWriteTears is the fail-before leg: the same crash point on a
// backend that truncates the live file in place leaves PARTIAL bytes —
// the exact failure the publish sequence exists to remove. If a future
// change routed WriteFile back through create+truncate on the target,
// the crash tests above would see this state instead of old-or-new.
func TestInPlaceWriteTears(t *testing.T) {
	k := newFakeShare()
	k.install(t)
	h := mustHost(t, k)
	k.files["/host/GS/vfs/doc.txt"] = []byte("old")

	// An in-place overwrite, killed mid-write: open truncates the live
	// file, half the new body lands, the process dies.
	f, err := fsys.Create("/host/GS/vfs/doc.txt")
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if _, err := f.Write([]byte("ne")); err != nil { // half of "new"
		t.Fatalf("write: %v", err)
	}
	k.dead = true  // kill: no close, no sync, no cleanup runs
	k.dead = false // next boot reads the share as the crash left it

	b, err := h.ReadFile("doc.txt")
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if string(b) == "old" || string(b) == "new" {
		t.Fatalf("in-place write did NOT tear: got %q", b)
	}
	if string(b) != "ne" {
		t.Fatalf("unexpected torn state %q", b)
	}
}

// TestRecoveryCommitsPublish: kill after target-delete leaves an absent
// target plus a complete temp; the next access publishes the new bytes —
// never absent, never partial.
func TestRecoveryCommitsPublish(t *testing.T) {
	k := newFakeShare()
	k.install(t)
	h := mustHost(t, k)
	k.files["/host/GS/vfs/doc.txt"] = []byte("old")

	k.crashAt("target-delete")
	_ = h.WriteFile("doc.txt", []byte("new"), 0o644)
	PublishHook = nil
	k.dead = false // next boot

	if _, ok := k.files["/host/GS/vfs/doc.txt"]; ok {
		t.Fatal("target should be absent after the crash")
	}
	var recovered []string
	RecoverHook = func(name string) { recovered = append(recovered, name) }
	b, err := h.ReadFile("doc.txt")
	if err != nil || string(b) != "new" {
		t.Fatalf("recover: %q err %v", b, err)
	}
	if len(recovered) != 1 || recovered[0] != "doc.txt" {
		t.Fatalf("RecoverHook fired %v", recovered)
	}
	if _, ok := k.files["/host/GS/vfs/doc.txt~"]; ok {
		t.Fatal("residue left after recovery")
	}
}

func TestRecoveryAcrossStatOpenRemove(t *testing.T) {
	stage := func(h *HostFS, k *fakeShare) {
		k.files["/host/GS/vfs/doc.txt"] = []byte("old")
		k.crashAt("target-delete")
		_ = h.WriteFile("doc.txt", []byte("new"), 0o644)
		PublishHook = nil
		k.dead = false
	}

	// Stat recovers.
	k := newFakeShare()
	k.install(t)
	h := mustHost(t, k)
	stage(h, k)
	if _, err := h.Stat("doc.txt"); err != nil {
		t.Fatalf("Stat after crash: %v", err)
	}
	if string(k.files["/host/GS/vfs/doc.txt"]) != "new" {
		t.Fatal("Stat did not complete the pending publish")
	}

	// Open recovers and serves the new bytes.
	k = newFakeShare()
	k.install(t)
	h = mustHost(t, k)
	stage(h, k)
	f, err := h.Open("doc.txt")
	if err != nil {
		t.Fatalf("Open after crash: %v", err)
	}
	buf := make([]byte, 16)
	n, _ := f.Read(buf)
	f.Close()
	if string(buf[:n]) != "new" {
		t.Fatalf("Open read %q, want new", buf[:n])
	}

	// Remove completes the publish then deletes BOTH names: a delete is a
	// delete — residue must not resurrect the file.
	k = newFakeShare()
	k.install(t)
	h = mustHost(t, k)
	stage(h, k)
	if err := h.Remove("doc.txt"); err != nil {
		t.Fatalf("Remove after crash: %v", err)
	}
	if _, ok := k.files["/host/GS/vfs/doc.txt"]; ok {
		t.Fatal("Remove left the target")
	}
	if _, ok := k.files["/host/GS/vfs/doc.txt~"]; ok {
		t.Fatal("Remove left residue that would resurrect the file")
	}
	if _, err := h.ReadFile("doc.txt"); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("read after remove: %v, want not-exist", err)
	}
}

// TestReadDirCompletesPending: a "~" row with an absent target is a pending
// publish — the listing completes it and reports the target's name.
func TestReadDirCompletesPending(t *testing.T) {
	k := newFakeShare()
	k.install(t)
	h := mustHost(t, k)
	k.files["/host/GS/vfs/doc.txt"] = []byte("old")
	k.crashAt("target-delete")
	_ = h.WriteFile("doc.txt", []byte("new"), 0o644)
	PublishHook = nil
	k.dead = false

	entries, err := h.ReadDir(".")
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	if len(entries) != 1 || entries[0].Name() != "doc.txt" {
		t.Fatalf("ReadDir = %v, want [doc.txt]", names(entries))
	}
	if string(k.files["/host/GS/vfs/doc.txt"]) != "new" {
		t.Fatal("listing did not complete the pending publish")
	}

	// Inert residue beside a LIVE target stays listed as-is: the target
	// being present is what makes the "~" file residue, not a pending
	// publish.
	k.files["/host/GS/vfs/other.txt"] = []byte("live")
	k.files["/host/GS/vfs/other.txt~"] = []byte("orphan")
	entries, err = h.ReadDir(".")
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	got := names(entries)
	if len(got) != 3 || got[0] != "doc.txt" || got[1] != "other.txt" || got[2] != "other.txt~" {
		t.Fatalf("ReadDir = %v, want [doc.txt other.txt other.txt~]", got)
	}
	if _, ok := k.files["/host/GS/vfs/other.txt~"]; !ok {
		t.Fatal("live-target residue was wrongly consumed")
	}
}

func names(es []fs.DirEntry) []string {
	var out []string
	for _, e := range es {
		out = append(out, e.Name())
	}
	return out
}

// --- handles -------------------------------------------------------------

func TestHandleCeilingNamedRefusal(t *testing.T) {
	k := newFakeShare()
	k.install(t)
	h := mustHost(t, k)
	for i := 0; i < 3; i++ {
		k.files["/host/GS/vfs/f"+string(rune('0'+i))] = []byte("x")
	}
	var open []fs.File
	for i := 0; i < 8; i++ {
		f, err := h.Open("f0") // eight live handles on the one file
		if err != nil {
			t.Fatalf("open %d: %v", i, err)
		}
		open = append(open, f)
	}
	_, err := h.Open("f0")
	if err == nil {
		t.Fatal("ninth open succeeded — kernel bound is 8")
	}
	if !IsTableFull(err) {
		t.Fatalf("ninth open: %v, want the named table-full refusal", err)
	}
	for _, f := range open {
		f.Close()
	}
}

// --- ordinary operations --------------------------------------------------

func TestWriteReadRoundtripAndOverwrite(t *testing.T) {
	k := newFakeShare()
	k.install(t)
	h := mustHost(t, k)

	if err := h.WriteFile("users/guest/documents/note.txt", []byte("v1\n"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	if !k.dirs["/host/GS/vfs/users/guest/documents"] {
		t.Fatal("WriteFile did not create parents")
	}
	b, err := h.ReadFile("users/guest/documents/note.txt")
	if err != nil || string(b) != "v1\n" {
		t.Fatalf("read %q %v", b, err)
	}
	// Overwrite is the publish: old replaced wholesale, no tail.
	if err := h.WriteFile("users/guest/documents/note.txt", []byte("x\n"), 0); err != nil {
		t.Fatalf("rewrite: %v", err)
	}
	b, _ = h.ReadFile("users/guest/documents/note.txt")
	if string(b) != "x\n" {
		t.Fatalf("rewrite read %q", b)
	}
}

func TestMkdirAllAndStat(t *testing.T) {
	k := newFakeShare()
	k.install(t)
	h := mustHost(t, k)

	if err := h.MkdirAll("a/b/c"); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	for _, d := range []string{"/host/GS/vfs/a", "/host/GS/vfs/a/b", "/host/GS/vfs/a/b/c"} {
		if !k.dirs[d] {
			t.Fatalf("missing dir %s", d)
		}
	}
	info, err := h.Stat("a/b/c")
	if err != nil || !info.IsDir() {
		t.Fatalf("Stat dir: %v %v", info, err)
	}
	// MkdirAll through an existing FILE must fail, not tolerate.
	k.files["/host/GS/vfs/block"] = []byte("x")
	if err := h.MkdirAll("block"); err == nil {
		t.Fatal("MkdirAll over a file succeeded")
	}
}

func TestNewHostRequiresExistingDir(t *testing.T) {
	k := newFakeShare()
	k.install(t)
	k.dirs["/host/GS/vfs"] = true

	if _, err := NewHost("relative/path"); err == nil {
		t.Fatal("relative root accepted")
	}
	if _, err := NewHost("/host/../host/GS/vfs"); err == nil {
		t.Fatal("dot-segment root accepted")
	}
	if _, err := NewHost("/host/GONE"); err == nil {
		t.Fatal("missing root accepted — upstream os.OpenRoot refuses too")
	}
	k.files["/host/afile"] = []byte("x")
	if _, err := NewHost("/host/afile"); err == nil {
		t.Fatal("file as root accepted")
	}
}
