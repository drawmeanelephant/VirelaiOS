// #3915: the directory carry and its receiving half.
//
// A drag off a directory row stages a bounded tar of the whole subtree under
// the existing `archive` MIME: the root member, every nested file with its
// exact bytes, and empty directories. Enumeration goes through the complete
// paged listing (vi.ListDirAt) rather than the legacy 16-row window or the
// search walk's partial subtree, so a row beyond the visible window is still
// carried. The encoded total — two terminator blocks plus every header and
// padded body — must fit vi.DragMax (65536 bytes); exact fit is admitted and
// anything past it refuses by name, never truncated.
//
// The receiving half dispatches the same MIME in a GOFILES window: the tar is
// parsed and shape-checked before anything is published, the saved name is
// derived from the archive's own root member (DragOffer has no filename
// field), and the bytes land through an independently owned stage plus the
// kernel's atomic preserve-existing rename. A destination that another
// writer creates between the free-name probe and that rename is never
// deleted or replaced: the rename refuses, the drop refuses by name, and the
// existing read-only archive browser opens only a file this drop saved.
// Nothing is extracted, and the opens=text/vcard contact drop keeps its own
// path.
package main

import (
	"errors"
	"strings"

	"virelai/vi"
)

const (
	// carryMax is the literal encoded bound: the seat's vi.DragMax, counted
	// over the whole serialized archive, not just member bodies.
	carryMax = 65536
	// carryBlocks is the two zero blocks every tar ends with.
	carryBlocks = 2 * tarBlock
	// carryNameMax is the ustar name field. The writer emits no pax/GNU long
	// names, so a member that cannot fit refuses instead of being rewritten.
	carryNameMax = 100
	// carryMaxDepth bounds the walk independently of the real-path cap.
	carryMaxDepth = 8
	// carryAltMax bounds collision alternates: ROOT-1.TAR .. ROOT-99.TAR.
	carryAltMax = 99
	// carryBaseMax keeps ROOT + "-99.TAR" inside the kernel's per-entry
	// name cap (maxNameLen = 32).
	carryBaseMax = maxNameLen - len("-99.TAR")
	// carryStageNameLen is one publish's staging name: "~" plus eight hex
	// digits mixing the owner nonce with the per-publish sequence. It is
	// deliberately not the shared `dest~` candidate WriteFileSafe uses.
	carryStageNameLen = 9
	// carryStageTries bounds re-picking when a leftover stage happens to
	// hold the mixed value.
	carryStageTries = 4
)

// The carry's seams: the complete paged listing and the body read on the
// guest, a fake tree in host tests (the searchDirList/readHead pattern). The
// receiver's publication primitives are seams for the same reason: host
// tests drive the REAL carryPublish over an in-memory share, so the sequence
// under test is the shipped one (independently owned stage, complete body,
// then the kernel's atomic non-replacing rename), never a stand-in for it.
var (
	carryListDir  = vi.ListDirAt
	carryReadFile = func(path string, max int) ([]byte, int64) { return readHead(path, max) }
	carryExists   = vi.FileExists
	// carryWriteFile writes one complete file through the crash-safe
	// primitive; carryRename is the kernel's preserve-existing publish;
	// carryDelete removes only a stage this feature owns.
	carryWriteFile = vi.WriteFileSafe
	carryRename    = vi.FileRename
	carryDelete    = vi.FileDelete
	carryNonce     = carryRandom32
	carryPublish   = publishCarry
)

// paddedSize rounds a body length up to the 512-byte archive block grid.
func paddedSize(n uint64) uint64 { return (n + tarBlock - 1) &^ (tarBlock - 1) }

// carryRandom32 mixes the guest CSPRNG into a stage's owner nonce. A host
// build reports -ENOSYS to every device syscall, so the per-publish sequence
// alone still separates stages there; on the guest the nonce keeps two
// processes apart too.
func carryRandom32() uint32 {
	var b [4]byte
	if n, err := vi.Random(b[:]); err == nil && n == len(b) {
		return uint32(b[0])<<24 | uint32(b[1])<<16 | uint32(b[2])<<8 | uint32(b[3])
	}
	return 0
}

// canonSharePath converts a GOFILES path (/host/FMC/SUB) into the paged
// listing's share-canonical form (/FMC/SUB). Anything outside the host share
// — the volumes root, /disk, a USB slot — is not a complete-walk tree here and
// returns "".
func canonSharePath(path string) string {
	if path == rootPath {
		return "/"
	}
	if strings.HasPrefix(path, rootPath+"/") {
		return path[len(rootPath):]
	}
	return ""
}

// carryItem is one member the walk committed: the real path to read and the
// archive name it carries.
type carryItem struct {
	real  string
	tar   string
	dir   bool
	size  uint64
	depth int
}

// sortedItems orders one listing page by name so the archive is deterministic
// (the kernel's snapshot order is already sorted, but the host tests and a
// later backend are not promised it).
func sortedItems(items []vi.DirItem) {
	for i := 1; i < len(items); i++ {
		for j := i; j > 0 && items[j].Name < items[j-1].Name; j-- {
			items[j], items[j-1] = items[j-1], items[j]
		}
	}
}

// buildDirCarry enumerates rootReal's subtree completely, checks the encoded
// budget before reading a single body, then reads and encodes. A non-empty
// reason refuses the whole carry: no prefix is ever returned.
func buildDirCarry(rootReal, rootName string) ([]byte, string) {
	if !validName(rootName) {
		return nil, "path"
	}
	canon := canonSharePath(rootReal)
	if canon == "" {
		return nil, "volume"
	}
	items := []carryItem{{real: rootReal, tar: rootName, dir: true}}
	total := uint64(carryBlocks + tarBlock) // terminators + the root header

	var walk func(canon, real, tar string, depth int) string
	walk = func(canon, real, tar string, depth int) string {
		list, err := carryListDir(canon)
		if err != nil {
			if errors.Is(err, vi.ErrTreeLimit) {
				return "tree"
			}
			return "list"
		}
		sortedItems(list)
		for _, it := range list {
			if !validName(it.Name) {
				return "path"
			}
			childReal, ok := joinPath(real, it.Name)
			if !ok {
				return "path"
			}
			childTar := tar + "/" + it.Name
			if len(childTar) > carryNameMax {
				return "path"
			}
			cost := uint64(tarBlock)
			if !it.IsDir {
				if it.Size > carryMax {
					return "archive-over-65536"
				}
				cost += paddedSize(it.Size)
			}
			if total+cost > carryMax {
				return "archive-over-65536"
			}
			total += cost
			items = append(items, carryItem{real: childReal, tar: childTar,
				dir: it.IsDir, size: it.Size, depth: depth + 1})
			if it.IsDir {
				if depth+1 > carryMaxDepth {
					return "path"
				}
				if why := walk(canonSharePath(childReal), childReal, childTar, depth+1); why != "" {
					return why
				}
			}
		}
		return ""
	}
	if why := walk(canon, rootReal, rootName, 0); why != "" {
		return nil, why
	}

	out := make([]byte, 0, total)
	out = appendTarMember(out, rootName, true, nil)
	for _, it := range items[1:] {
		if it.dir {
			out = appendTarMember(out, it.tar, true, nil)
			continue
		}
		// Read one byte past the listed size: a grown file then reports the
		// mismatch instead of being silently clipped, and a shrunk file
		// falls short. Both refuse the whole carry.
		body, rc := carryReadFile(it.real, int(it.size)+1)
		if rc < 0 {
			return nil, "read rc=" + vi.Itoa64(rc)
		}
		if uint64(len(body)) != it.size {
			return nil, "changed"
		}
		out = appendTarMember(out, it.tar, false, body)
	}
	out = append(out, make([]byte, carryBlocks)...)
	if uint64(len(out)) != total {
		return nil, "changed" // the accounting and the bytes disagree
	}
	if len(out) > carryMax {
		return nil, "archive-over-65536"
	}
	return out, ""
}

// appendTarMember appends one ustar record: a 512-byte header the in-tree
// reader verifies (checksum, "ustar" magic, octal size) and the body padded to
// the block grid. Directories carry the trailing slash and typeflag '5', files
// typeflag '0'. No pax/GNU extension records are emitted — every name is
// checked against the 100-byte field before this is called.
func appendTarMember(dst []byte, name string, dir bool, body []byte) []byte {
	var h [tarBlock]byte
	member := name
	if dir {
		member += "/"
	}
	copy(h[0:100], member)
	putTarOctal(h[100:108], 0o755)             // mode (dirs and files: readable)
	putTarOctal(h[108:116], 0)                 // uid
	putTarOctal(h[116:124], 0)                 // gid
	putTarOctal(h[124:136], uint64(len(body))) // size
	putTarOctal(h[136:148], 0)                 // mtime: epoch 0, deterministic
	if dir {
		h[156] = '5'
	} else {
		h[156] = '0'
	}
	copy(h[257:265], "ustar\x0000") // magic "ustar\0", version "00"
	for i := 148; i < 156; i++ {
		h[i] = ' '
	}
	var sum uint64
	for _, b := range h {
		sum += uint64(b)
	}
	putTarChecksum(h[148:156], sum)
	dst = append(dst, h[:]...)
	dst = append(dst, body...)
	if pad := int(paddedSize(uint64(len(body)))) - len(body); pad > 0 {
		dst = append(dst, make([]byte, pad)...)
	}
	return dst
}

// putTarOctal writes a numeric field: zero-padded octal digits and a NUL
// terminator, the form tarOctal and Python's tarfile both read.
func putTarOctal(f []byte, v uint64) {
	for i := range f {
		f[i] = '0'
	}
	i := len(f) - 2
	for v > 0 && i >= 0 {
		f[i] = byte('0' + v&7)
		v >>= 3
		i--
	}
	f[len(f)-1] = 0
}

// putTarChecksum writes the conventional checksum field: six octal digits,
// NUL, space.
func putTarChecksum(f []byte, v uint64) {
	for i := 0; i < 6; i++ {
		f[i] = '0'
	}
	i := 5
	for v > 0 && i >= 0 {
		f[i] = byte('0' + v&7)
		v >>= 3
		i--
	}
	f[6] = 0
	f[7] = ' '
}

// dragDirOffer is the directory arm of dragOffer: a complete bounded tar, or
// the named refusal that kept it from claiming bytes it does not have.
func (m *model) dragDirOffer(e vi.DirEntry, name string) (string, []byte, bool) {
	refuse := func(why string) (string, []byte, bool) {
		m.emit(markerDragNo + name + " " + why)
		m.status = dirCarryStatus(why)
		return "", nil, false
	}
	switch {
	case m.tar != nil:
		return refuse("tar")
	case m.path == favsView:
		return refuse("favs")
	case volumeOf(m.path) != volHost:
		return refuse("volume")
	}
	src, ok := joinPath(m.path, name)
	if !ok {
		return refuse("path")
	}
	blob, why := buildDirCarry(src, name)
	if why != "" {
		return refuse(why)
	}
	m.dragMT = "archive"
	m.dragLen = len(blob)
	return "archive", blob, true
}

// dirCarryStatus is the status-line text for a refused directory carry.
func dirCarryStatus(why string) string {
	switch why {
	case "volume":
		return "drag: volumes do not carry"
	case "tar":
		return "drag: archive rows do not carry"
	case "favs":
		return "drag: pinned places do not carry"
	case "tree":
		return "drag: subtree exceeds the listing bound"
	case "list":
		return "drag: subtree could not be listed"
	case "path":
		return "drag: path too long"
	case "changed":
		return "drag: file changed while reading"
	case "archive-over-65536":
		return "drag: archive over the 65536-byte cap"
	}
	if strings.HasPrefix(why, "read rc=") {
		return "drag: subtree file unreadable"
	}
	return "drag: directory does not carry"
}

// --- receiving half -------------------------------------------------------
// The DropTarget door itself lives in drop.go (tag-free, like the model), so
// the host tests can drive a landing without the guest command.

// inspectCarry parses and shape-checks an offered archive: complete ustar
// records with two terminators, exactly the offered bytes consumed, one
// single-component root directory first, only file/dir members under that
// root, and every parent directory explicitly present. The result is the root
// name, the member count, or the named refusal.
func inspectCarry(blob []byte) (root string, entries int, why string) {
	if len(blob) < 2*tarBlock || len(blob)%tarBlock != 0 {
		return "", 0, "invalid"
	}
	ents, streamed, refuse := tarParse(&memTarSrc{buf: blob})
	if refuse != "" || streamed != int64(len(blob)) || len(ents) == 0 {
		return "", 0, "invalid"
	}
	first := ents[0]
	if first.cls != tarClsDir || strings.Contains(first.name, "/") ||
		!validName(first.name) {
		return "", 0, "root"
	}
	root = first.name
	dirs := map[string]bool{root: true}
	seen := map[string]bool{}
	for _, e := range ents {
		switch e.cls {
		case tarClsRefused, tarClsLink, tarClsOther:
			return "", 0, "invalid"
		}
		if e.name != root && !strings.HasPrefix(e.name, root+"/") {
			return "", 0, "invalid"
		}
		if seen[e.name] {
			return "", 0, "invalid"
		}
		seen[e.name] = true
		if e.cls == tarClsDir {
			if e.size != 0 {
				return "", 0, "invalid"
			}
			dirs[e.name] = true
		}
	}
	for name := range seen {
		if name == root {
			continue
		}
		parts := strings.Split(strings.TrimPrefix(name, root+"/"), "/")
		cur := root
		for _, part := range parts[:len(parts)-1] {
			cur += "/" + part
			if !dirs[cur] {
				return "", 0, "incomplete"
			}
		}
	}
	return root, len(ents), ""
}

// archiveName derives a safe, root-derived destination name and never returns
// one already in the directory (or already existing on disk). A collision
// takes a deterministic -N alternate; an exhausted or impossible name
// refuses.
func archiveName(root, dir string, list []vi.DirItem) (string, string) {
	base := make([]byte, 0, len(root))
	for i := 0; i < len(root); i++ {
		c := root[i]
		if c >= 0x21 && c < 0x7f && c != '/' {
			base = append(base, c)
		} else {
			base = append(base, '_')
		}
	}
	if len(base) == 0 {
		base = append(base, "ARCHIVE"...)
	}
	if len(base) > carryBaseMax {
		base = base[:carryBaseMax]
	}
	taken := make(map[string]bool, len(list))
	for _, it := range list {
		taken[it.Name] = true
	}
	for n := 0; n <= carryAltMax; n++ {
		name := string(base)
		if n > 0 {
			name += "-" + vi.Itoa64(int64(n))
		}
		name += ".TAR"
		if taken[name] {
			continue
		}
		if dest, ok := joinPath(dir, name); ok && carryExists(dest) {
			continue
		}
		return name, ""
	}
	return "", "name"
}

// carryStageSeq is bumped once per stage attempt, so two publishes in this
// process can never choose the same name even when the CSPRNG repeats.
var carryStageSeq uint32

// carryStagePath returns a staging path this publish alone owns. The stage
// must be a sibling: the kernel's rename is same-directory only, and
// WriteFileSafe appends one byte for its own temp, so the directory has to
// leave room for the name plus that byte inside maxPath. A directory without
// that room refuses by path rather than picking a name another writer could
// also have chosen.
func carryStagePath(dir, dest string) (string, int64) {
	if maxPath-(len(dir)+2) < carryStageNameLen {
		return "", -vi.ErrENAMETOOLONG
	}
	for try := 0; try < carryStageTries; try++ {
		carryStageSeq++
		name := "~" + carryHex8(carryNonce()+carryStageSeq*0x9e3779b1)
		stage := dir + "/" + name
		if stage == dest || stage+"~" == dest {
			continue
		}
		if carryExists(stage) {
			continue
		}
		return stage, 0
	}
	return "", -vi.ErrENOSPC
}

// carryHex8 renders exactly eight lowercase hex digits without fmt.
func carryHex8(v uint32) string {
	const digits = "0123456789abcdef"
	var b [8]byte
	for i := 7; i >= 0; i-- {
		b[i] = digits[v&0xf]
		v >>= 4
	}
	return string(b[:])
}

// publishCarry writes data to an independently owned stage in dest's own
// directory and publishes it with the kernel's atomic preserve-existing
// rename. It never opens, deletes or renames over dest: a writer that lands
// the name after the caller's free-name probe keeps its exact bytes, this
// returns the kernel's -9, and only the losing stage is removed. The old
// probe-then-WriteFileSafe path deleted such a destination and shared one
// `name~` candidate between writers.
func publishCarry(dest string, data []byte) int64 {
	slash := strings.LastIndexByte(dest, '/')
	if slash <= 0 || slash == len(dest)-1 {
		return -vi.ErrEINVAL
	}
	stage, rc := carryStagePath(dest[:slash], dest)
	if rc < 0 {
		return rc
	}
	if rc := carryWriteFile(stage, data); rc < 0 {
		_ = carryDelete(stage) // our own stage; dest was never touched
		return rc
	}
	if rc := carryRename(stage, dest); rc < 0 {
		_ = carryDelete(stage) // our own stage; dest was never touched
		return rc
	}
	return 0
}
