// Package fsys is the gsport file adapter: the file PRIMITIVES only —
// open, read, write, sync, close, delete, rename, stat-shaped probes and
// directory listing — over explicit ADR 0007 file calls.
//
// Host assumption it replaces: os.Open/OpenFile/ReadFile/WriteFile/
// Remove/Rename/Sync/Readdir, which on VirelaiOS ride the fork's os layer
// to the same slots — but only through the ADR 0010 subset. This package
// exists for the paths where the wrapper must choose the call (a stat is
// a probe open, a directory walk is slot 27 not getdents, a replace is
// delete-then-rename because the kernel refuses rename-over-live-target).
//
// Slots: 23 open, 24 read, 25 write, 26 close, 27 dir list, 34 delete,
// 35 rename, 36 truncate, 77 sync.
//
// Refuses by name: paths outside the kernel bounds (512-byte path,
// 255-byte component, depth 8 — kernel/src/file_table.zig), rename that
// would need overwrite semantics (the kernel already refuses; the wrapper
// says so), and mode bits — guest file modes are not a permission
// boundary, so OpenFile-style perm arguments are never accepted.
package fsys

import (
	"errors"
	"io/fs"
	"strings"

	"virelai/gsport/abi"
)

// Kernel path bounds (kernel/src/file_table.zig).
const (
	MaxPathLen = 512
	MaxNameLen = 255
	MaxDepth   = 8
	MaxHandles = 8 // per-process file table
)

// FileInfo is the stat shape the kernel can actually answer: dir listing
// gives name+size+is_dir; open probe gives existence. There is no mtime or
// mode — asking for either is the POSIX assumption the package refuses.
type FileInfo struct {
	Name  string
	Size  int64
	IsDir bool
}

// checkPath enforces the kernel's path bounds before a call burns a slot.
func checkPath(op, path string) error {
	if path == "" {
		return &abi.Error{Op: op, Path: path, Code: abi.ErrEINVAL}
	}
	if len(path) > MaxPathLen {
		return &abi.Error{Op: op, Path: path, Code: abi.ErrENAMETOOLONG}
	}
	depth := 0
	for _, comp := range strings.Split(path, "/") {
		if comp == "" {
			continue
		}
		depth++
		if len(comp) > MaxNameLen {
			return &abi.Error{Op: op, Path: path, Code: abi.ErrENAMETOOLONG}
		}
		if depth > MaxDepth {
			return &abi.Error{Op: op, Path: path, Code: abi.ErrENAMETOOLONG}
		}
	}
	return nil
}

// File is one open guest handle (kernel table: MaxHandles per process).
type File struct {
	path   string
	handle uint32
}

func open(path string, flags uint32, op string) (*File, error) {
	if err := checkPath(op, path); err != nil {
		return nil, err
	}
	h, r := abi.FileOpen(path, flags)
	if r < 0 {
		return nil, abi.Check(op, path, r)
	}
	return &File{path: path, handle: uint32(h)}, nil
}

// Open opens path read-only.
func Open(path string) (*File, error) { return open(path, abi.ModeRead, "open") }

// Create opens path for writing, creating it when absent. There is no
// perm argument: guest file modes are not a permission boundary.
func Create(path string) (*File, error) {
	return open(path, abi.ModeWrite|abi.ModeCreate, "create")
}

// Append opens path for appending, creating when absent.
func Append(path string) (*File, error) {
	return open(path, abi.ModeWrite|abi.ModeCreate|abi.ModeAppend, "append")
}

// Mkdir creates a directory (ModeDir open is the kernel's mkdir op).
// An existing directory is not an error — the file-domain EEXIST answer
// is the kernel's "already there".
func Mkdir(path string) error {
	f, err := open(path, abi.ModeDir|abi.ModeCreate|abi.ModeWrite, "mkdir")
	if errors.Is(err, fs.ErrExist) {
		return nil
	}
	if err != nil {
		return err
	}
	f.Close()
	return nil
}

func (f *File) Read(b []byte) (int, error) {
	n, r := abi.FileRead(f.handle, b)
	if r < 0 {
		return 0, abi.Check("read", f.path, r)
	}
	return n, nil
}

func (f *File) Write(b []byte) (int, error) {
	n, r := abi.FileWriteAll(f.handle, b)
	if r < 0 {
		return n, abi.Check("write", f.path, r)
	}
	return n, nil
}

// Sync pushes the handle's host-side state to durability (slot 77).
func (f *File) Sync() error {
	return abi.Check("sync", f.path, abi.FileSync(f.handle))
}

// Truncate resizes the open file (slot 36).
func (f *File) Truncate(size int64) error {
	return abi.Check("truncate", f.path, abi.FileTruncate(f.handle, uint32(size)))
}

// Close releases the handle (slot 26). Idempotent.
func (f *File) Close() error {
	if f.handle == 0 {
		return nil
	}
	abi.FileClose(f.handle)
	f.handle = 0
	return nil
}

// Delete removes path (slot 34).
func Delete(path string) error {
	if err := checkPath("delete", path); err != nil {
		return err
	}
	return abi.Check("delete", path, abi.FileDelete(path))
}

// Rename moves old to new (slot 35). The kernel REFUSES a live target
// (file-domain EEXIST, -9); this adapter surfaces that refusal rather than
// retrying an overwrite POSIX callers expect.
func Rename(oldPath, newPath string) error {
	if err := checkPath("rename", oldPath); err != nil {
		return err
	}
	if err := checkPath("rename", newPath); err != nil {
		return err
	}
	r := abi.FileRename(oldPath, newPath)
	if r == abi.ErrFileExists {
		return &abi.Error{Op: "rename (kernel refuses overwrite of live target; use Replace)", Path: newPath, Code: -r}
	}
	return abi.Check("rename", oldPath, r)
}

// Replace is the guest's honest overwrite: delete-then-rename, the same
// publish sequence vi.WriteFileSafe pins. A crash between leaves the new
// name absent — the reader-sees-defaults failure mode — never a torn file.
func Replace(oldPath, newPath string) error {
	if err := Delete(newPath); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	return Rename(oldPath, newPath)
}

// Exists probes path with a probe open (slot 23).
func Exists(path string) bool { return abi.FileExists(path) }

// Stat answers what the kernel can say about path: a directory listing of
// the parent gives size+is_dir for entries it contains; a bare path is
// probed for existence and reported with Size 0.
func Stat(path string) (FileInfo, error) {
	if err := checkPath("stat", path); err != nil {
		return FileInfo{}, err
	}
	dir, base := split(path)
	entries, err := ReadDir(dir)
	if err == nil {
		for _, e := range entries {
			if e.Name == base {
				return e, nil
			}
		}
	}
	if !Exists(path) {
		return FileInfo{}, &abi.Error{Op: "stat", Path: path, Code: abi.ErrENOENT}
	}
	return FileInfo{Name: base}, nil
}

func split(path string) (dir, base string) {
	i := strings.LastIndexByte(path, '/')
	if i < 0 {
		return "", path
	}
	return path[:i], path[i+1:]
}

// ReadDir lists path (slot 27). The kernel window is abi.MaxDirEntries per
// call; this wrapper pages until a short page ends the listing.
func ReadDir(path string) ([]FileInfo, error) {
	if err := checkPath("readdir", path); err != nil {
		return nil, err
	}
	var out []FileInfo
	buf := make([]abi.DirEntry, abi.MaxDirEntries)
	seen := map[string]bool{}
	for {
		n, r := abi.DirList(path, buf)
		if r < 0 {
			return nil, abi.Check("readdir", path, r)
		}
		if n == 0 {
			return out, nil
		}
		added := 0
		for i := 0; i < n; i++ {
			name := buf[i].NameString()
			if name == "" || seen[name] {
				continue
			}
			seen[name] = true
			added++
			out = append(out, FileInfo{Name: name, Size: int64(buf[i].Size), IsDir: buf[i].Dir()})
		}
		if n < abi.MaxDirEntries || added == 0 {
			return out, nil
		}
	}
}

// ReadFile reads the whole file through one open (slots 23+24+26).
func ReadFile(path string, max int) ([]byte, error) {
	if err := checkPath("readfile", path); err != nil {
		return nil, err
	}
	b, r := abi.ReadFileAll(path, max)
	if r < 0 {
		return nil, abi.Check("readfile", path, r)
	}
	return b, nil
}
