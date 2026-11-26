package flintstore

import (
	"bytes"
	"sort"
	"strings"

	"virelai/vi"
)

// DirEntry is one directory listing item returned by FS.List.
type DirEntry struct {
	Name  string
	IsDir bool
}

// FS abstracts the guest /host file channel for FLINT and provides an
// in-memory implementation (MemFS) for deterministic class-A tests.
type FS interface {
	ReadFile(path string, max int) ([]byte, error)
	WriteFileSafe(path string, data []byte) error
	AppendSync(path string, data []byte) error
	Mkdir(path string) error
	List(path string) ([]DirEntry, error)
	Remove(path string) error
	Exists(path string) bool
}

// FSError wraps a failed FS operation with the ADR 0007 errno name
// (vi.ErrnoName).
type FSError struct {
	Op    string
	Path  string
	Errno string
}

func (e *FSError) Error() string {
	if e.Path == "" {
		return e.Op + ": " + e.Errno
	}
	return e.Op + " " + e.Path + ": " + e.Errno
}

func newFSErr(op, path string, rc int64) error {
	name := vi.ErrnoName(rc)
	if name == "" {
		name = "EIO"
	}
	return &FSError{Op: op, Path: path, Errno: name}
}

// RefuseError represents a named FLINT refusal (`flint: refuse <code> ...`).
type RefuseError struct {
	Code   string
	Detail string
}

func (e *RefuseError) Error() string {
	if e.Detail == "" {
		return "flint: refuse " + e.Code
	}
	return "flint: refuse " + e.Code + " " + e.Detail
}

// Refuse builds a named refusal.
func Refuse(code, detail string) error { return &RefuseError{Code: code, Detail: detail} }

// RefusalName returns the named refusal code carried by err, or "".
func RefusalName(err error) string {
	if re, ok := err.(*RefuseError); ok {
		return re.Code
	}
	return ""
}

// GuestFS implements FS over the VirelaiOS `vi` syscall package.
type GuestFS struct{}

func (GuestFS) ReadFile(path string, max int) ([]byte, error) {
	if max <= 0 {
		max = MaxFileReadBytes
	}
	if max+1 <= vi.MaxFileBytes {
		b, rc := vi.ReadFileAll(path, max+1)
		if rc < 0 {
			return nil, newFSErr("read", path, rc)
		}
		if len(b) > max {
			return nil, Refuse("store_too_large", "file")
		}
		return b, nil
	}
	// Fast path for files smaller than vi.MaxFileBytes.
	b, rc := vi.ReadFileAll(path, vi.MaxFileBytes)
	if rc < 0 {
		return nil, newFSErr("read", path, rc)
	}
	if len(b) < vi.MaxFileBytes {
		if len(b) > max {
			return nil, Refuse("store_too_large", "file")
		}
		return b, nil
	}
	// File is at least vi.MaxFileBytes; stream up to max+1 bytes via FileOpen/FileRead.
	h, orc := vi.FileOpen(path, vi.ModeRead)
	if orc < 0 {
		return nil, newFSErr("read", path, orc)
	}
	defer vi.FileClose(uint32(h))
	out := make([]byte, 0, vi.MaxFileBytes*2)
	buf := make([]byte, 4096)
	limit := max + 1
	for len(out) < limit {
		n, rrc := vi.FileRead(uint32(h), buf)
		if rrc < 0 {
			return nil, newFSErr("read", path, rrc)
		}
		if n == 0 {
			break
		}
		take := n
		if len(out)+take > limit {
			take = limit - len(out)
		}
		out = append(out, buf[:take]...)
	}
	if len(out) > max {
		return nil, Refuse("store_too_large", "file")
	}
	return out, nil
}

func (GuestFS) WriteFileSafe(path string, data []byte) error {
	if rc := vi.WriteFileSafe(path, data); rc < 0 {
		return newFSErr("write", path, rc)
	}
	return nil
}

func (GuestFS) AppendSync(path string, data []byte) error {
	h, rc := vi.FileOpen(path, vi.ModeWrite|vi.ModeCreate|vi.ModeAppend)
	if rc < 0 {
		return newFSErr("open", path, rc)
	}
	n, wrc := vi.FileWriteAll(uint32(h), data)
	if wrc < 0 {
		vi.FileClose(uint32(h))
		return newFSErr("write", path, wrc)
	}
	if n != len(data) {
		vi.FileClose(uint32(h))
		return newFSErr("write", path, -vi.ErrENOSPC)
	}
	if src := vi.FileSync(uint32(h)); src < 0 {
		vi.FileClose(uint32(h))
		return newFSErr("sync", path, src)
	}
	vi.FileClose(uint32(h))
	return nil
}

func (GuestFS) Mkdir(path string) error {
	h, rc := vi.FileOpen(path, vi.ModeWrite|vi.ModeCreate|vi.ModeDir)
	if rc == vi.ErrFileExists {
		return nil
	}
	if rc < 0 {
		return newFSErr("mkdir", path, rc)
	}
	vi.FileClose(uint32(h))
	return nil
}

func (GuestFS) List(dir string) ([]DirEntry, error) {
	if items, err := vi.ListDirAt(CanonShareDir(dir)); err == nil {
		out := make([]DirEntry, 0, len(items))
		for _, it := range items {
			out = append(out, DirEntry{Name: it.Name, IsDir: it.IsDir})
		}
		sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
		return out, nil
	}
	var entries [vi.MaxDirEntries]vi.DirEntry
	n, rc := vi.DirList(dir, entries[:])
	if rc < 0 {
		return nil, newFSErr("list", dir, rc)
	}
	if n >= vi.MaxDirEntries {
		return nil, newFSErr("list", dir, -vi.ErrENOSPC)
	}
	out := make([]DirEntry, 0, n)
	for _, e := range entries[:n] {
		out = append(out, DirEntry{Name: e.NameString(), IsDir: e.Dir()})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

func (GuestFS) Remove(path string) error {
	if rc := vi.FileDelete(path); rc < 0 {
		return newFSErr("remove", path, rc)
	}
	return nil
}

func (GuestFS) Exists(path string) bool {
	h, rc := vi.FileOpen(path, vi.ModeRead)
	if rc >= 0 {
		vi.FileClose(uint32(h))
		return true
	}
	if rc == vi.ErrFileIsDir {
		return true
	}
	var entries [1]vi.DirEntry
	_, drc := vi.DirList(path, entries[:])
	return drc >= 0
}

// MemFS is an in-memory FS implementation for host unit tests.
type MemFS struct {
	Files map[string][]byte
	Dirs  map[string]bool
	// BeforeWrite, when set, runs before each mutating operation. Tests use
	// it to inject a deterministic crash window between store writes.
	BeforeWrite func(op, path string) error
}

// NewMemFS returns an empty in-memory FS with /host present.
func NewMemFS() *MemFS {
	return &MemFS{
		Files: map[string][]byte{},
		Dirs:  map[string]bool{"/host": true},
	}
}

// CleanPath trims redundant trailing slashes from a guest path.
func CleanPath(p string) string {
	if p == "" {
		return ""
	}
	for len(p) > 1 && strings.HasSuffix(p, "/") {
		p = p[:len(p)-1]
	}
	return p
}

// CanonShareDir maps a /host path to the custom-virtio share's root-relative
// path used by vi.ListDirAt (slot 79).
func CanonShareDir(dir string) string {
	switch {
	case dir == "" || dir == "/host" || dir == "/host/":
		return "/"
	case strings.HasPrefix(dir, "/host/"):
		return dir[len("/host"):]
	case strings.HasPrefix(dir, "/"):
		return dir
	default:
		return "/" + dir
	}
}

func (m *MemFS) markParents(p string) {
	for {
		idx := strings.LastIndexByte(p, '/')
		if idx <= 0 {
			break
		}
		p = p[:idx]
		m.Dirs[p] = true
	}
}

func (m *MemFS) ReadFile(path string, max int) ([]byte, error) {
	path = CleanPath(path)
	if m.Dirs[path] {
		return nil, newFSErr("read", path, vi.ErrFileIsDir)
	}
	b, ok := m.Files[path]
	if !ok {
		return nil, newFSErr("read", path, vi.ErrFileNotFound)
	}
	if max <= 0 {
		max = MaxFileReadBytes
	}
	if len(b) > max {
		return nil, Refuse("store_too_large", "file")
	}
	return bytes.Clone(b), nil
}

func (m *MemFS) WriteFileSafe(path string, data []byte) error {
	path = CleanPath(path)
	if path == "" {
		return newFSErr("write", path, -vi.ErrEINVAL)
	}
	if m.BeforeWrite != nil {
		if err := m.BeforeWrite("write", path); err != nil {
			return err
		}
	}
	if m.Dirs[path] {
		return newFSErr("write", path, vi.ErrFileIsDir)
	}
	m.markParents(path)
	m.Files[path] = bytes.Clone(data)
	return nil
}

func (m *MemFS) AppendSync(path string, data []byte) error {
	path = CleanPath(path)
	if path == "" {
		return newFSErr("append", path, -vi.ErrEINVAL)
	}
	if m.BeforeWrite != nil {
		if err := m.BeforeWrite("append", path); err != nil {
			return err
		}
	}
	if m.Dirs[path] {
		return newFSErr("append", path, vi.ErrFileIsDir)
	}
	m.markParents(path)
	m.Files[path] = append(m.Files[path], data...)
	return nil
}

func (m *MemFS) Mkdir(path string) error {
	path = CleanPath(path)
	if path == "" {
		return newFSErr("mkdir", path, -vi.ErrEINVAL)
	}
	if m.BeforeWrite != nil {
		if err := m.BeforeWrite("mkdir", path); err != nil {
			return err
		}
	}
	if _, isFile := m.Files[path]; isFile {
		return newFSErr("mkdir", path, vi.ErrFileExists)
	}
	m.markParents(path)
	m.Dirs[path] = true
	return nil
}

func (m *MemFS) List(dir string) ([]DirEntry, error) {
	dir = CleanPath(dir)
	if _, isFile := m.Files[dir]; isFile {
		return nil, newFSErr("list", dir, -vi.ErrEINVAL)
	}
	prefix := dir + "/"
	children := map[string]bool{} // name -> isDir
	foundDir := m.Dirs[dir]
	for p := range m.Dirs {
		if strings.HasPrefix(p, prefix) {
			foundDir = true
			rest := p[len(prefix):]
			if rest == "" {
				continue
			}
			if i := strings.IndexByte(rest, '/'); i >= 0 {
				children[rest[:i]] = true
			} else {
				children[rest] = true
			}
		}
	}
	for p := range m.Files {
		if strings.HasPrefix(p, prefix) {
			foundDir = true
			rest := p[len(prefix):]
			if rest == "" {
				continue
			}
			if i := strings.IndexByte(rest, '/'); i >= 0 {
				children[rest[:i]] = true
			} else {
				if _, ok := children[rest]; !ok {
					children[rest] = false
				}
			}
		}
	}
	if !foundDir {
		return nil, newFSErr("list", dir, vi.ErrFileNotFound)
	}
	out := make([]DirEntry, 0, len(children))
	for name, isDir := range children {
		out = append(out, DirEntry{Name: name, IsDir: isDir})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

func (m *MemFS) Remove(path string) error {
	path = CleanPath(path)
	if m.BeforeWrite != nil {
		if err := m.BeforeWrite("remove", path); err != nil {
			return err
		}
	}
	if _, ok := m.Files[path]; !ok {
		if !m.Dirs[path] {
			return newFSErr("remove", path, vi.ErrFileNotFound)
		}
		delete(m.Dirs, path)
		return nil
	}
	delete(m.Files, path)
	return nil
}

func (m *MemFS) Exists(path string) bool {
	path = CleanPath(path)
	if m.Dirs[path] {
		return true
	}
	_, ok := m.Files[path]
	return ok
}

// EnsureDirAll creates path and its missing parents.
func EnsureDirAll(fs FS, path string) error {
	path = CleanPath(path)
	if path == "" || fs.Exists(path) {
		return nil
	}
	if i := strings.LastIndexByte(path, '/'); i > 0 {
		if err := EnsureDirAll(fs, path[:i]); err != nil {
			return err
		}
	}
	return fs.Mkdir(path)
}
