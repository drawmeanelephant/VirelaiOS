// Act operations for the M74a file manager: rename, delete, copy and move.
//
// Every operation validates its arguments BEFORE touching a syscall, so the
// host `go test` run can pin the refusal shapes (-vi.ErrEINVAL) without a
// guest, and the guest never reaches the kernel with a path that could walk
// off the share. The vi layer is the host-stubbed syscall surface.
package main

import "virelai/vi"

const (
	// maxPreviewBytes caps what the preview pane reads (vi.ReadFileAll
	// clamps to vi.MaxFileBytes = 256 KiB anyway).
	maxPreviewBytes = 4096
	// maxPasteBytes caps a copy/paste body: a file at the read cap is
	// REFUSED rather than silently truncated (there is no stat-by-path in
	// ADR 0007, so ">= cap" is the honest boundary).
	maxPasteBytes = vi.MaxFileBytes
	// maxNameLen bounds a rename target typed in the prompt.
	maxNameLen = 32
)

// validName is the rename/paste target rule: non-empty, not "."/"..", no
// slash, no control bytes, within the kernel's per-entry name cap.
func validName(name string) bool {
	if name == "" || name == "." || name == ".." {
		return false
	}
	if len(name) > maxNameLen {
		return false
	}
	for i := 0; i < len(name); i++ {
		c := name[i]
		if c == '/' || c < 0x20 || c == 0x7f {
			return false
		}
	}
	return true
}

// renameEntry renames `from` to `to` inside dir (both names, one syscall).
func renameEntry(dir, from, to string) int64 {
	if !validName(from) || !validName(to) || from == to {
		return -vi.ErrEINVAL
	}
	src, ok1 := joinPath(dir, from)
	dst, ok2 := joinPath(dir, to)
	if !ok1 || !ok2 {
		return -vi.ErrEINVAL
	}
	return vi.FileRename(src, dst)
}

// deleteEntry moves the file `name` from dir into the shared trash.
// Directories are refused by the model before this is reached (ADR 0007 has
// no rmdir slot).
func deleteEntry(dir, name string) (string, int64) {
	if !validName(name) {
		return "", -vi.ErrEINVAL
	}
	p, ok := joinPath(dir, name)
	if !ok {
		return "", -vi.ErrEINVAL
	}
	return vi.TrashDelete(p)
}

// readCapped reads a whole file up to cap bytes. Returns (data, rc) with
// rc < 0 on failure and rc == len(data) on success (vi.ReadFileAll shape).
func readCapped(path string, cap int) ([]byte, int64) {
	return vi.ReadFileAll(path, cap)
}

// pasteCopy writes data at dir/baseName(src). The caller has already
// refused an existing target (a paste never overwrites).
func pasteCopy(dir, src string, data []byte) int64 {
	// Defense in depth: the model refuses a at-cap reads before this is
	// reached (there is no stat-by-path, so "at the read cap" is the honest
	// too-large boundary); EINVAL says the body itself broke the contract.
	if len(data) >= maxPasteBytes {
		return -vi.ErrEINVAL
	}
	dst, ok := joinPath(dir, baseName(src))
	if !ok {
		return -vi.ErrEINVAL
	}
	return vi.WriteFileSafe(dst, data)
}

// pasteMove renames src into dir under its own base name (one filesystem —
// the share — so a move is a rename). The caller has refused an existing
// target.
func pasteMove(dir, src string) int64 {
	dst, ok := joinPath(dir, baseName(src))
	if !ok || dst == src {
		return -vi.ErrEINVAL
	}
	return vi.FileRename(src, dst)
}
