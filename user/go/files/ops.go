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

// leaseBlocks reports whether a live advisory write lease another writer
// holds stands on path (M81d #1764). It is the read-only half of the
// convention: a mutating op calls this BEFORE it clobbers a path it does
// not hold a token for, and refuses with -vi.ErrEAGAIN when one is live.
// A path nobody leases (the usual case — one extra syscall) is free.
func leaseBlocks(path string) bool {
	return vi.CheckFileLease(path) == -vi.ErrEAGAIN
}

// refusalStatus is the status-line text for a refused act op. The
// live-lease case is named, not folded into the generic refusal: the user
// should know WHY nothing happened, and the status line is where GOFILES
// says so.
func refusalStatus(op string, rc int64) string {
	if rc == -vi.ErrEAGAIN {
		return op + " refused: lease held"
	}
	return op + " refused"
}

// renameEntry renames `from` to `to` inside dir (both names, one syscall).
// A live lease on the source refuses the rename: moving a leased file away
// would let its editor's next save resurrect the old name.
func renameEntry(dir, from, to string) int64 {
	if !validName(from) || !validName(to) || from == to {
		return -vi.ErrEINVAL
	}
	src, ok1 := joinPath(dir, from)
	dst, ok2 := joinPath(dir, to)
	if !ok1 || !ok2 {
		return -vi.ErrEINVAL
	}
	if leaseBlocks(src) {
		return -vi.ErrEAGAIN
	}
	return vi.FileRename(src, dst)
}

// deleteEntry moves the file `name` from dir into the shared trash.
// Directories are refused by the model before this is reached (ADR 0007 has
// no rmdir slot). A live lease refuses the delete: this is the exact
// silent-loss shape the lease exists for — an editor holds the file while
// the file manager removes it, and the editor's next save resurrects a
// file the user deleted.
func deleteEntry(dir, name string) (string, int64) {
	if !validName(name) {
		return "", -vi.ErrEINVAL
	}
	p, ok := joinPath(dir, name)
	if !ok {
		return "", -vi.ErrEINVAL
	}
	if leaseBlocks(p) {
		return "", -vi.ErrEAGAIN
	}
	return vi.TrashDelete(p)
}

// readCapped reads a whole file up to cap bytes. Returns (data, rc) with
// rc < 0 on failure and rc == len(data) on success (vi.ReadFileAll shape).
func readCapped(path string, cap int) ([]byte, int64) {
	return vi.ReadFileAll(path, cap)
}

// pasteCopy writes data at dir/baseName(src). The caller has already
// refused an existing target (a paste never overwrites); the lease check
// is defense in depth on the same rule — a live lease on the destination
// refuses the copy.
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
	if leaseBlocks(dst) {
		return -vi.ErrEAGAIN
	}
	return vi.WriteFileSafe(dst, data)
}

// pasteMove renames src into dir under its own base name (one filesystem —
// the share — so a move is a rename). The caller has refused an existing
// target; a live lease on the destination refuses the move.
func pasteMove(dir, src string) int64 {
	dst, ok := joinPath(dir, baseName(src))
	if !ok || dst == src {
		return -vi.ErrEINVAL
	}
	if leaseBlocks(dst) {
		return -vi.ErrEAGAIN
	}
	return vi.FileRename(src, dst)
}
