// pinned-sha256: f2947aade9fd96a46fd69fab9440cfc73040751e4dbff47d5fea6db62acfdcae
package vfs

import (
	"io/fs"
	"strings"

	gvfs "virelai/gsport/vfs"
)

// validFSName enforces the io/fs name contract on the raw backends:
// names arrive here already in fs form (no leading slash, no dot
// segments) and must stay that way. Backslashes are rejected on every
// platform: they are ordinary filename characters on Unix but path
// separators on Windows, and one portable contract is easier to reason
// about than two.
//
// Virelai port: kept identical to the pin — internal/mem.go still calls
// it. The host backend's own confinement (kernel bounds, ~-suffix, NUL)
// lives in gsport/vfs, behind the HostFS alias below.
func validFSName(op, name string) error {
	if strings.ContainsRune(name, '\\') {
		return &fs.PathError{Op: op, Path: name, Err: fs.ErrInvalid}
	}
	if !fs.ValidPath(name) {
		return &fs.PathError{Op: op, Path: name, Err: fs.ErrInvalid}
	}
	return nil
}

// HostFS is an FS backed by a directory on the host.
//
// Virelai port (M95d, #2012): upstream's implementation wrapped os.Root
// (Openat), which compiles on GOOS=virelai but returns ENOSYS — the
// kernel has no dirfd abstraction; every file call is path-addressed.
// This backend is the gsport/vfs adapter: it maps the environment's
// fs-form names onto host-mounted share paths under dir, enforces
// confinement and the kernel's path/name/depth bounds before any ABI
// call, and publishes writes via fsync-then-rename so a kill mid-save
// can never leave a partial file (see gsport/vfs for the old-or-new
// recovery contract). The alias keeps the pinned method surface —
// RootPath, Close, Open, Stat, ReadDir, ReadFile, MkdirAll, WriteFile,
// Remove — so callers and the VFS interface check in vfs.go are
// unchanged.
type HostFS = gvfs.HostFS

// NewHost opens dir as the backing directory for a HostFS.
func NewHost(dir string) (*HostFS, error) {
	return gvfs.NewHost(dir)
}
