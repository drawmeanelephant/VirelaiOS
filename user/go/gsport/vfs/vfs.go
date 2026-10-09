// Package vfs is the gsport document-store adapter: a confined host
// backend for Gostalgia's vfs.FS, built on gsport/fsys, with the
// crash-recoverable save the environment's document model needs. It
// replaces the pin's os.Root confinement (internal/vfs/host.go), which
// compiles on this fork but whose *at calls the kernel refuses (ENOSYS —
// VirelaiOS has no dirfd abstraction; every file call is path-addressed).
//
// The load-bearing decisions, recorded for M95d (#2012):
//
//   - Confinement is lexical and enforced HERE, not inherited from the
//     caller. Names arrive in io/fs form; the backend re-checks every one:
//     '\\' and NUL are refused, any element fs.ValidPath rejects ("." and
//     ".." included) is refused, and a leading slash is refused, so a name
//     can never leave the root. Symlink escapes do not exist to defend
//     against: nothing at EL0 can create one (the port's Link/Symlink are
//     ENOSYS and the kernel's directory rows carry no link type). A
//     symlink planted inside the share by the HOST is the host's own act —
//     the share is trusted host space — and is recorded as out of scope,
//     not silently claimed covered.
//   - Save semantics are OLD-OR-NEW, matching Gostalgia's host contract
//     (temp write + rename-over). The publish sequence is the one
//     vi.WriteFileSafe pins — write the sibling temp "name~", fsync it
//     (slot 77), close, delete(name), rename(temp -> name) — composed from
//     fsys primitives here rather than delegated to vi, because the
//     recovery rule has to live where the reader is. The ordering makes
//     "target absent AND `name~` present" provable as a COMPLETE pending
//     publish: the delete runs only after the fsynced temp is closed, so
//     no other interleaving can produce that state. Recovery is lazy and
//     total: every operation on a name (Open/Stat/ReadFile/Remove,
//     WriteFile, and ReadDir entries) first completes a pending publish
//     on it by renaming `name~` into place. A crash therefore yields the
//     old bytes or the new bytes — never absent, never partial.
//   - "~" is a reserved suffix. WriteFile/Remove/MkdirAll on a final
//     element ending in "~" are refused by name (fs.ErrInvalid), so a
//     `~` file on the share is unambiguously publish residue: a pending
//     publish when its target is absent (completed on next access), or
//     inert residue when the target is live (re-truncated by the next
//     save; nothing sweeps it, same as vi.WriteFileSafe's orphan rule).
//     Remove deletes the target AND any `name~` residue — a delete is a
//     delete; residue must not resurrect a removed name on next access.
//   - Kernel bounds are checked before a slot burns: 512-byte path,
//     255-byte component, depth 8 (kernel/src/file_table.zig). Over-long
//     names are refused by name (ENAMETOOLONG) and never truncated. The
//     8-handle table's exhaustion surfaces as the kernel's ENOSPC;
//     IsTableFull names it — a refusal, never a hang (the backend holds
//     no handles between calls, so only the guest process's own open
//     handles can exhaust the table).
//   - ReadDir sees the kernel's 16-row window: vi.DirList has no listing
//     cursor (the v2 paged protocol exists in the kernel but is not
//     exposed through vi), so a directory over 16 entries reports the
//     first window only — recorded, not silently widened.
//   - Permissions are DROPPED, all of them. There is no mode model at
//     EL0 (ADR 0024 D1): the kernel ignores create perm bits and Chmod is
//     ENOSYS, so Gostalgia's 0644 documents, 0600 token/config and 0755
//     directories all become "host-default, unchangeable". This changes
//     nothing upstream relied on for security: the token's 0600 was local
//     hygiene, the share is host-visible regardless, and the enforced
//     boundary — the fs service's RequireCap checks — runs unchanged.
//     A stat-reported mode is the host's own mode read back through the
//     kernel's rows; nothing a guest writes alters it.
package vfs

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"sort"
	"strings"
	"time"

	"virelai/gsport/abi"
	"virelai/gsport/fsys"
)

// TempSuffix marks publish residue: the recoverable save stages bytes in
// "<name>~" before publishing them as <name>.
const TempSuffix = "~"

// PublishHook, when non-nil, is invoked after each named publish stage
// completes: "temp-open", "temp-write", "temp-fsync", "temp-close",
// "target-delete", "rename". It is the crash-drill and host-test seam:
// a hook that blocks (or panics) leaves the share in exactly that stage's
// state. Production code leaves it nil.
var PublishHook func(stage string)

// RecoverHook, when non-nil, is invoked with the fs-form name each time a
// read-side op completes a pending publish. Evidence seam for the driver;
// nil in production.
var RecoverHook func(name string)

// ErrTableFull names the 8-handles-per-process ceiling's refusal: the
// kernel's ENOSPC (ErrFileHandleFull row) with the cause attached.
var ErrTableFull = errors.New("vfs: file table full (8 handles per process)")

// IsTableFull reports whether err is the handle-table refusal.
func IsTableFull(err error) bool {
	return errors.Is(err, ErrTableFull)
}

// named maps a kernel/adapter refusal onto a *fs.PathError that references
// the ENVIRONMENT name (never the host path, matching the pin's contract)
// and names the handle-table ceiling.
func named(op, name string, err error) error {
	var ae *abi.Error
	if errors.As(err, &ae) && ae.Code == abi.ErrENOSPC {
		return &fs.PathError{Op: op, Path: name, Err: fmt.Errorf("%w (%s)", ErrTableFull, ae.Error())}
	}
	var pe *fs.PathError
	if errors.As(err, &pe) {
		return &fs.PathError{Op: op, Path: name, Err: pe.Err}
	}
	return &fs.PathError{Op: op, Path: name, Err: err}
}

// validName enforces the io/fs name contract on backend names — the same
// check the pin's validFSName runs, kept here so the backend's confinement
// does not depend on any caller having done it. fs.ValidPath refuses a
// leading slash, "." and ".." elements, and empty interior elements; the
// backslash rule keeps the one portable contract the pin established.
func validName(op, name string) error {
	if strings.ContainsRune(name, '\\') {
		return &fs.PathError{Op: op, Path: name, Err: fs.ErrInvalid}
	}
	if !fs.ValidPath(name) {
		return &fs.PathError{Op: op, Path: name, Err: fs.ErrInvalid}
	}
	return nil
}

// checkBounds refuses a path that exceeds the kernel's file bounds —
// before a slot burns. Composition with the root prefix is deliberate:
// the kernel measures the whole path it is handed.
func checkBounds(op, name, joined string) error {
	if len(joined) > fsys.MaxPathLen {
		return &fs.PathError{Op: op, Path: name,
			Err: fmt.Errorf("vfs: path over kernel bound (%d bytes): %w", fsys.MaxPathLen, fs.ErrInvalid)}
	}
	depth := 0
	for _, comp := range strings.Split(joined, "/") {
		if comp == "" {
			continue
		}
		depth++
		if len(comp) > fsys.MaxNameLen {
			return &fs.PathError{Op: op, Path: name,
				Err: fmt.Errorf("vfs: name over kernel bound (%d bytes): %w", fsys.MaxNameLen, fs.ErrInvalid)}
		}
		if depth > fsys.MaxDepth {
			return &fs.PathError{Op: op, Path: name,
				Err: fmt.Errorf("vfs: path over kernel depth bound (%d): %w", fsys.MaxDepth, fs.ErrInvalid)}
		}
	}
	return nil
}

// refuseReserved rejects "~"-suffixed final elements on mutating ops, so
// a "~" file on the share can only ever be publish residue (see the
// package doc). Reads of residue names stay legal — the bytes are real.
func refuseReserved(op, name string) error {
	base := name
	if i := strings.LastIndexByte(base, '/'); i >= 0 {
		base = base[i+1:]
	}
	if strings.HasSuffix(base, TempSuffix) {
		return &fs.PathError{Op: op, Path: name,
			Err: fmt.Errorf("vfs: %q is the reserved publish-residue suffix: %w", TempSuffix, fs.ErrInvalid)}
	}
	return nil
}

func callHook(stage string) {
	if PublishHook != nil {
		PublishHook(stage)
	}
}

// residueReadOK enforces the M97d F6 (#2091) invariant: a publish temp is
// a DIFFERENT path key, so it always carries the kernel's default class
// policy — never its target's (ADR 0024 D8 keys the secret class on the
// exact normalized path). Reading "<target>~" would therefore bypass the
// target's class — a secret-class or otherwise read-denied target's
// plaintext served back under the sibling name, during the publish
// window and as crash residue. The residue's read verdict is its
// target's own: open the target for read and let the kernel's class
// check answer. A target that does not open fails closed — absent means
// mid-publish (staged bytes are not a read contract; recovery already
// committed every legitimate state) and denied means the class covers
// the bytes under every name.
func residueReadOK(joined string) bool {
	target := strings.TrimSuffix(joined, TempSuffix)
	f, err := fsys.Open(target)
	if err != nil {
		return false
	}
	f.Close()
	return true
}

// refuseResidueRead denies a byte-read of "<name>~" when residueReadOK
// fails; it is the read-side twin of refuseReserved (which covers
// mutations). Names and directory listings stay legal — ADR 0024 D8
// denies secret BYTES, not names.
func refuseResidueRead(op, name, joined string) error {
	if !strings.HasSuffix(baseOf(joined), TempSuffix) {
		return nil
	}
	if residueReadOK(joined) {
		return nil
	}
	return &fs.PathError{Op: op, Path: name,
		Err: fmt.Errorf("vfs: publish residue inherits its target's read verdict: %w", fs.ErrPermission)}
}

// Publish is the recoverable save on raw host-share paths (the shape the
// internal/config overlay needs — config lives outside the VFS root):
// stage b into tmp, fsync, close, delete path, rename tmp -> path.
// Ordering is the whole contract (see the package doc): the target is
// deleted only after the fsynced temp is closed, so an absent target with
// a present temp is always a complete pending publish that Recover can
// commit. Every pre-delete failure removes the temp; a post-delete
// failure LEAVES it, because that state is exactly what recovery reads.
func Publish(path, tmp string, b []byte) error {
	if err := checkBounds("write", path, path); err != nil {
		return err
	}
	if err := checkBounds("write", tmp, tmp); err != nil {
		return err
	}
	f, err := fsys.Create(tmp) // create-or-truncate: stale residue is overwritten
	if err != nil {
		return named("write", path, err)
	}
	callHook("temp-open")
	if _, err := f.Write(b); err != nil {
		f.Close()
		_ = fsys.Delete(tmp)
		return named("write", path, err)
	}
	callHook("temp-write")
	if err := f.Sync(); err != nil {
		f.Close()
		_ = fsys.Delete(tmp)
		return named("write", path, err)
	}
	callHook("temp-fsync")
	if err := f.Close(); err != nil {
		_ = fsys.Delete(tmp)
		return named("write", path, err)
	}
	callHook("temp-close")
	if err := fsys.Delete(path); err != nil && !errors.Is(err, fs.ErrNotExist) {
		_ = fsys.Delete(tmp)
		return named("write", path, err)
	}
	callHook("target-delete")
	if err := fsys.Rename(tmp, path); err != nil {
		// The temp stays: this is the recoverable state by design.
		return named("write", path, err)
	}
	callHook("rename")
	return nil
}

// Recover completes a pending publish on raw host-share paths: path absent
// plus tmp present means the fsynced temp survived a kill between delete
// and rename, so the publish is committed by finishing the rename. Returns
// whether a recovery happened. Concurrent recoveries race to the same
// outcome; the loser's rename finds the target live and is not an error.
func Recover(path, tmp string) (bool, error) {
	if fsys.Exists(path) {
		return false, nil
	}
	if !fsys.Exists(tmp) {
		return false, nil
	}
	if err := fsys.Rename(tmp, path); err != nil {
		if fsys.Exists(path) {
			return true, nil // a racing recovery committed first
		}
		return false, err
	}
	return true, nil
}

// MkdirAll creates path and its missing parents on the host share (the
// path-level helper internal/config persists through). Directory modes
// are dropped — there is no mode model at EL0.
func MkdirAll(path string) error {
	return mkdirAllAbs(path)
}

// ReadFile reads the whole host-share file at path (path-level helper);
// no cap beyond memory — the share round-trips real bytes. A "~"-suffixed
// path answers only while its publish target is itself readable.
func ReadFile(path string) ([]byte, error) {
	if err := refuseResidueRead("read", path, path); err != nil {
		return nil, err
	}
	f, err := fsys.Open(path)
	if err != nil {
		return nil, named("read", path, err)
	}
	defer f.Close()
	var out []byte
	buf := make([]byte, 4096)
	for {
		n, err := f.Read(buf)
		out = append(out, buf[:n]...)
		if err != nil {
			return out, named("read", path, err)
		}
		if n == 0 {
			return out, nil
		}
	}
}

// ---------------------------------------------------------------------

// HostFS is the /host-backed root filesystem: one confined environment
// root over gsport/fsys. It satisfies Gostalgia's vfs.FS interface and
// keeps the pin's HostFS surface (NewHost/RootPath/Close plus the FS
// methods) so the internal/vfs overlay is a delegation, not a redesign.
type HostFS struct {
	root string // absolute host-share path, e.g. "/host/GSVFS/vfs"
}

// NewHost opens dir as the environment's host root. Like the pin's
// os.OpenRoot path, the directory must already exist — InitRoot creates
// it. Confinement is lexical: dir must be an absolute, in-bounds share
// path with no dot segments, and every backend name is re-validated to
// stay inside it.
func NewHost(dir string) (*HostFS, error) {
	if dir == "" || dir[0] != '/' {
		return nil, fmt.Errorf("vfs: host root %q is not an absolute share path", dir)
	}
	if dir != "/" {
		dir = strings.TrimSuffix(dir, "/")
	}
	for _, seg := range strings.Split(dir, "/") {
		if seg == "." || seg == ".." {
			return nil, fmt.Errorf("vfs: host root %q carries a dot segment", dir)
		}
	}
	if err := checkBounds("open", dir, dir); err != nil {
		return nil, err
	}
	if _, err := fsys.ReadDir(dir); err != nil {
		return nil, fmt.Errorf("vfs: host root %s: %w", dir, named("open", dir, err))
	}
	return &HostFS{root: dir}, nil
}

// RootPath returns the backing share directory.
func (h *HostFS) RootPath() string { return h.root }

// Close releases the root. The backend holds no handles between calls —
// there is nothing to release, and the method exists for interface parity.
func (h *HostFS) Close() error { return nil }

// resolve maps an fs-form name to its host path inside the root,
// re-checking confinement and kernel bounds. The join can never escape:
// validName refuses every element that could climb out, so containment is
// the rule's own property, not a check that runs after.
func (h *HostFS) resolve(op, name string) (string, error) {
	if err := validName(op, name); err != nil {
		return "", err
	}
	joined := h.root
	if name != "." {
		joined += "/" + name
	}
	if err := checkBounds(op, name, joined); err != nil {
		return "", err
	}
	return joined, nil
}

// recover completes a pending publish on an already-resolved host path.
func (h *HostFS) recover(op, name, joined string) error {
	ok, err := Recover(joined, joined+TempSuffix)
	if err != nil {
		return named(op, name, err)
	}
	if ok && RecoverHook != nil {
		RecoverHook(name)
	}
	return nil
}

func (h *HostFS) Open(name string) (fs.File, error) {
	if err := validName("open", name); err != nil {
		return nil, err
	}
	joined, err := h.resolve("open", name)
	if err != nil {
		return nil, err
	}
	if err := h.recover("open", name, joined); err != nil {
		return nil, err
	}
	info, err := fsys.Stat(joined)
	if err != nil {
		return nil, named("open", name, err)
	}
	if info.IsDir {
		list, err := h.ReadDir(name)
		if err != nil {
			return nil, err
		}
		return &dirFile{info: fileInfo{name: baseOf(name), dir: true}, entries: list}, nil
	}
	if err := refuseResidueRead("open", name, joined); err != nil {
		return nil, err
	}
	f, err := fsys.Open(joined)
	if err != nil {
		return nil, named("open", name, err)
	}
	return &regFile{f: f, info: fileInfo{name: baseOf(name), size: info.Size}}, nil
}

func (h *HostFS) Stat(name string) (fs.FileInfo, error) {
	if err := validName("stat", name); err != nil {
		return nil, err
	}
	joined, err := h.resolve("stat", name)
	if err != nil {
		return nil, err
	}
	if err := h.recover("stat", name, joined); err != nil {
		return nil, err
	}
	info, err := fsys.Stat(joined)
	if err != nil {
		return nil, named("stat", name, err)
	}
	return fileInfo{name: baseOf(name), size: info.Size, dir: info.IsDir}, nil
}

func (h *HostFS) ReadFile(name string) ([]byte, error) {
	if err := validName("read", name); err != nil {
		return nil, err
	}
	joined, err := h.resolve("read", name)
	if err != nil {
		return nil, err
	}
	if err := h.recover("read", name, joined); err != nil {
		return nil, err
	}
	if err := refuseResidueRead("read", name, joined); err != nil {
		return nil, err
	}
	f, err := fsys.Open(joined)
	if err != nil {
		return nil, named("read", name, err)
	}
	defer f.Close()
	var out []byte
	buf := make([]byte, 4096)
	for {
		n, err := f.Read(buf)
		out = append(out, buf[:n]...)
		if err != nil {
			return out, named("read", name, err)
		}
		if n == 0 {
			return out, nil
		}
	}
}

func (h *HostFS) ReadDir(name string) ([]fs.DirEntry, error) {
	if err := validName("readdir", name); err != nil {
		return nil, err
	}
	joined, err := h.resolve("readdir", name)
	if err != nil {
		return nil, err
	}
	rows, err := fsys.ReadDir(joined)
	if err != nil {
		return nil, named("readdir", name, err)
	}
	out := make([]fs.DirEntry, 0, len(rows))
	for _, e := range rows {
		en := entry{info: fileInfo{name: e.Name, size: e.Size, dir: e.IsDir}}
		// A "~" file row whose target is absent is a pending publish:
		// complete it and report the entry under the target's name.
		// Residue beside a live target stays listed as-is.
		if !e.IsDir && strings.HasSuffix(e.Name, TempSuffix) && e.Name != TempSuffix {
			base := strings.TrimSuffix(e.Name, TempSuffix)
			if !fsys.Exists(joined + "/" + base) {
				if rerr := h.recover("readdir", name+"/"+base, joined+"/"+base); rerr == nil {
					en.info.name = base
				}
			}
		}
		out = append(out, en)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name() < out[j].Name() })
	return out, nil
}

func (h *HostFS) MkdirAll(name string) error {
	if err := validName("mkdir", name); err != nil {
		return err
	}
	if err := refuseReserved("mkdir", name); err != nil {
		return err
	}
	if name == "." {
		return nil
	}
	return h.mkdirAll(name)
}

func (h *HostFS) mkdirAll(name string) error {
	// The whole name is validated before the walk: an invalid or escaping
	// parent must not create the leading segments it happens to share.
	if err := validName("mkdir", name); err != nil {
		return err
	}
	if _, err := h.resolve("mkdir", name); err != nil {
		return err
	}
	cur := ""
	for _, seg := range strings.Split(name, "/") {
		if cur == "" {
			cur = seg
		} else {
			cur += "/" + seg
		}
		joined, err := h.resolve("mkdir", cur)
		if err != nil {
			return err
		}
		if err := fsys.Mkdir(joined); err != nil {
			return named("mkdir", cur, err)
		}
	}
	// An existing FILE at the final path is tolerated by fsys.Mkdir's
	// EEXIST contract but is not "all dirs exist" — say so honestly.
	joined, err := h.resolve("mkdir", name)
	if err != nil {
		return err
	}
	if info, err := fsys.Stat(joined); err == nil && !info.IsDir {
		return &fs.PathError{Op: "mkdir", Path: name,
			Err: fmt.Errorf("vfs: %s exists and is not a directory", name)}
	}
	return nil
}

func (h *HostFS) WriteFile(name string, data []byte, perm fs.FileMode) error {
	if err := validName("write", name); err != nil {
		return err
	}
	if err := refuseReserved("write", name); err != nil {
		return err
	}
	// The perm argument is accepted and DROPPED (no mode model at EL0 —
	// the package doc records the mapping; upstream defaults a zero perm
	// to 0644, and both land identically here).
	_ = perm
	// Bounds-check the whole joined path BEFORE any parent mkdir: a name
	// over a kernel bound must be refused without burning a slot or
	// mutating the share (half-created parents are a mutation too).
	joined, err := h.resolve("write", name)
	if err != nil {
		return err
	}
	if dir := dirOf(name); dir != "" {
		if err := h.mkdirAll(dir); err != nil {
			return err
		}
	}
	// Complete a pending publish first — every op on a name does — then
	// this save's own temp write supersedes it.
	if err := h.recover("write", name, joined); err != nil {
		return err
	}
	return Publish(joined, joined+TempSuffix, data)
}

func (h *HostFS) Remove(name string) error {
	if err := validName("remove", name); err != nil {
		return err
	}
	if err := refuseReserved("remove", name); err != nil {
		return err
	}
	if name == "." {
		return &fs.PathError{Op: "remove", Path: name,
			Err: errors.New("vfs: cannot remove the root")}
	}
	joined, err := h.resolve("remove", name)
	if err != nil {
		return err
	}
	// Complete the pending publish, then delete BOTH names — residue must
	// not resurrect a removed file on the next access.
	if err := h.recover("remove", name, joined); err != nil {
		return err
	}
	if err := fsys.Delete(joined + TempSuffix); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return named("remove", name, err)
	}
	if err := fsys.Delete(joined); err != nil {
		return named("remove", name, err)
	}
	return nil
}

func dirOf(name string) string {
	if i := strings.LastIndexByte(name, '/'); i > 0 {
		return name[:i]
	}
	return ""
}

func baseOf(name string) string {
	if i := strings.LastIndexByte(name, '/'); i >= 0 {
		return name[i+1:]
	}
	return name
}

// mkdirAllAbs on a raw host path (the internal/config overlay's parent-dir
// step) — same walk as the FS method, without the fs name contract.
// Existing levels are probed with ReadDir, not Exists: the directory
// answer comes from slot 27 (dir_open), and the share root "/host" —
// which is a valid existing directory but not an openable file — must be
// skipped, not re-created (the kernel's mkdir on it is EINVAL).
func mkdirAllAbs(path string) error {
	cur := ""
	for _, seg := range strings.Split(strings.TrimPrefix(path, "/"), "/") {
		if cur == "" {
			cur = "/" + seg
		} else {
			cur += "/" + seg
		}
		if err := checkBounds("mkdir", path, cur); err != nil {
			return err
		}
		if _, err := fsys.ReadDir(cur); err == nil {
			continue // exists as a directory
		}
		if err := fsys.Mkdir(cur); err != nil {
			return named("mkdir", path, err)
		}
	}
	// A FILE at the final path is not "all dirs exist" — say so honestly.
	if info, err := fsys.Stat(path); err == nil && !info.IsDir {
		return &fs.PathError{Op: "mkdir", Path: path,
			Err: fmt.Errorf("vfs: %s exists and is not a directory", path)}
	}
	return nil
}

// ---------------------------------------------------------------------

// fileInfo is the stat shape this filesystem can honestly give: name,
// size, kind. Mode is the kernel's own convention (dirs 0755, files 0644);
// ModTime does not exist at this layer and reports the zero time.
type fileInfo struct {
	name string
	size int64
	dir  bool
}

func (i fileInfo) Name() string       { return i.name }
func (i fileInfo) Size() int64        { return i.size }
func (i fileInfo) IsDir() bool        { return i.dir }
func (i fileInfo) ModTime() time.Time { return time.Time{} }
func (i fileInfo) Sys() any           { return nil }
func (i fileInfo) Mode() fs.FileMode {
	if i.dir {
		return fs.ModeDir | 0o755
	}
	return 0o644
}

type entry struct{ info fileInfo }

func (e entry) Name() string               { return e.info.name }
func (e entry) IsDir() bool                { return e.info.dir }
func (e entry) Type() fs.FileMode          { return e.info.Mode().Type() }
func (e entry) Info() (fs.FileInfo, error) { return e.info, nil }

// regFile is one open regular file — a borrow of the kernel's cursor
// handle through fsys.
type regFile struct {
	f    *fsys.File
	info fileInfo
}

func (f *regFile) Stat() (fs.FileInfo, error) { return f.info, nil }
func (f *regFile) Close() error               { return f.f.Close() }
func (f *regFile) Read(b []byte) (int, error) {
	// The kernel's cursor answers EOF as a bare zero count; the fs.File
	// contract wants io.EOF, and io.ReadAll would spin on (0, nil).
	n, err := f.f.Read(b)
	if n == 0 && err == nil {
		return 0, io.EOF
	}
	return n, err
}

// dirFile is Open on a directory: a snapshot of the listing the fs.File
// contract wants (Read fails; ReadDir pages the snapshot).
type dirFile struct {
	info    fileInfo
	entries []fs.DirEntry
	pos     int
}

func (d *dirFile) Stat() (fs.FileInfo, error) { return d.info, nil }
func (d *dirFile) Close() error               { return nil }
func (d *dirFile) Read([]byte) (int, error) {
	return 0, &fs.PathError{Op: "read", Path: d.info.name, Err: errors.New("is a directory")}
}

func (d *dirFile) ReadDir(n int) ([]fs.DirEntry, error) {
	rem := d.entries[d.pos:]
	if n <= 0 {
		d.pos = len(d.entries)
		return rem, nil
	}
	if len(rem) == 0 {
		return nil, io.EOF
	}
	if len(rem) > n {
		rem = rem[:n]
	}
	d.pos += len(rem)
	return rem, nil
}
