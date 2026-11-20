package main

import (
	"encoding/binary"
	"encoding/hex"
	"sort"
	"strings"

	"virelai/sshlib"
	"virelai/vi"
)

// Blob lane (M215a, ADR 0049 amendment — the additive store extension):
// /host/FLINT/blobs/<hex64> holds two body classes that let a replayed Atom
// table materialize back into source.
//
//   - Atom bodies: blobs/<atom.hash> = the raw span bytes
//     src[span.start:span.end] of that Atom version, written by `flint add`
//     for every Atom of every touched file. An Atom's canonical hash names
//     its body; bodies with the same canonical form share one blob.
//   - File bodies: blobs/<fileKey> = the complete source file bytes of one
//     file version, where fileKey derives from the file's Atom-hash tuple
//     (see fileBlobKey). The file's new_hashes name its body: replaying a
//     head reproduces the tuple, so the file body is locatable from the Atom
//     table alone — no schema change to Op or Change.
//
// A stream committed before the lane existed has no bodies: checkout and
// merge materialization refuse `body_missing` by name rather than emitting
// empty or partial files. Bodies are never trusted on their name alone: a
// file body is re-parsed and must reproduce the table's Atom hashes before
// it is written out (`body_corrupt`), and a synthesized file's re-parse
// must reproduce every expected Atom (`body_mismatch`).

const srcBlobDomain = "FLINT-SRC/1\x00"

func blobsDir(root string) string      { return root + "/blobs" }
func blobPath(root, key string) string { return blobsDir(root) + "/" + key }

// fileBlobKey derives the content id of one file version: SHA-256 over
// "FLINT-SRC/1\x00" || str(rel) || u32le(n) || hash_1 || … || hash_n, where
// hash_i is the decoded canonical hash of each of the file's Atoms in Atom
// table order (file, start, kind, name, id).
func fileBlobKey(rel string, atoms []StoredAtom) (string, error) {
	buf := make([]byte, 0, len(srcBlobDomain)+4+len(rel)+4+len(atoms)*32)
	buf = append(buf, srcBlobDomain...)
	var le [4]byte
	binary.LittleEndian.PutUint32(le[:], uint32(len(rel)))
	buf = append(buf, le[:]...)
	buf = append(buf, rel...)
	binary.LittleEndian.PutUint32(le[:], uint32(len(atoms)))
	buf = append(buf, le[:]...)
	for _, a := range atoms {
		hb, err := hex.DecodeString(a.Hash)
		if err != nil || len(hb) != 32 {
			return "", refuse("atom_malformed", a.Name)
		}
		buf = append(buf, hb...)
	}
	sum := sshlib.SHA256Sum(buf)
	return hex.EncodeToString(sum[:]), nil
}

// putBlob writes one body under blobs/<key> idempotently (WriteFileSafe is
// temp + fsync + rename), enforcing the 4 MiB body bound and the 4096-entry
// lane bound, counting newly written keys through *n so a batch caller pays
// one count-file write instead of one per body. The bound is a count file,
// never a listing: the guest's paged directory snapshot stops at 256
// entries (the legacy window at 16), so a lane that can reach 4096 bodies
// cannot be enumerated. The count is bumped only after bodies land, so a
// crash undercounts — loosening, never tightening — the bound.
func putBlob(fs FS, root, key string, body []byte, n *int) error {
	if len(body) > MaxFileReadBytes {
		return refuse("store_too_large", "blob")
	}
	if err := ensureDirAll(fs, blobsDir(root)); err != nil {
		return err
	}
	p := blobPath(root, key)
	if fs.Exists(p) {
		return nil
	}
	if *n >= MaxAtoms {
		return refuse("store_too_large", "blobs")
	}
	if err := fs.WriteFileSafe(p, body); err != nil {
		return err
	}
	*n++
	return nil
}

// storeBlob is the single-body putBlob: it owns the count read/write.
func storeBlob(fs FS, root, key string, body []byte) error {
	n, err := blobCount(fs, root)
	if err != nil {
		return err
	}
	m := n
	if err := putBlob(fs, root, key, body, &m); err != nil {
		return err
	}
	if m != n {
		return writeBlobCount(fs, root, m)
	}
	return nil
}

// blobCountPath is the lane's bookkeeping sibling — outside blobs/ so its
// name can never collide with a <hex64> content key.
func blobCountPath(root string) string { return root + "/blobs.count" }

// blobCount reads the lane's write-side entry count; an absent counter is
// an empty lane (older stores and fresh checkouts have none).
func blobCount(fs FS, root string) (int, error) {
	p := blobCountPath(root)
	if !fs.Exists(p) {
		return 0, nil
	}
	b, err := fs.ReadFile(p, 64)
	if err != nil {
		return 0, err
	}
	s := strings.TrimSpace(string(b))
	n := 0
	for _, c := range s {
		if c < '0' || c > '9' {
			return 0, refuse("store_malformed", "blobs.count")
		}
		n = n*10 + int(c-'0')
	}
	return n, nil
}

func writeBlobCount(fs FS, root string, n int) error {
	return fs.WriteFileSafe(blobCountPath(root), []byte(vi.Itoa64(int64(n))+"\n"))
}

// readBlob loads blobs/<key> bounded; it returns (nil, nil) when the lane
// has no such body — callers name the refusal.
func readBlob(fs FS, root, key string) ([]byte, error) {
	p := blobPath(root, key)
	if !fs.Exists(p) {
		return nil, nil
	}
	return fs.ReadFile(p, MaxFileReadBytes)
}

// storeFileBodies writes the blob lane's share of one staged file version:
// every Atom body keyed by its hash plus the complete file body keyed by
// the file's Atom-hash tuple. `atoms` is the file's entries in the new
// table order. One count read and one count write cover the whole file.
func storeFileBodies(fs FS, root, rel string, src []byte, atoms []StoredAtom) error {
	n, err := blobCount(fs, root)
	if err != nil {
		return err
	}
	m := n
	seen := make(map[string]bool, len(atoms))
	for _, a := range atoms {
		if int(a.Span.End) > len(src) || a.Span.Start >= a.Span.End {
			return refuse("atom_malformed", rel+" "+a.Name)
		}
		if seen[a.Hash] {
			continue
		}
		seen[a.Hash] = true
		if err := putBlob(fs, root, a.Hash, src[a.Span.Start:a.Span.End], &m); err != nil {
			return err
		}
	}
	key, err := fileBlobKey(rel, atoms)
	if err != nil {
		return err
	}
	if err := putBlob(fs, root, key, src, &m); err != nil {
		return err
	}
	if m != n {
		return writeBlobCount(fs, root, m)
	}
	return nil
}

// fileAtomGroup filters a table-sorted Atom list down to one file, keeping
// order.
func fileAtomGroup(atoms []StoredAtom, rel string) []StoredAtom {
	out := make([]StoredAtom, 0, 8)
	for _, a := range atoms {
		if a.Span.File == rel {
			out = append(out, a)
		}
	}
	return out
}

// groupAtomsByFile buckets a table-sorted Atom list by file and returns the
// file names in sorted order.
func groupAtomsByFile(atoms []StoredAtom) (map[string][]StoredAtom, []string) {
	byFile := map[string][]StoredAtom{}
	for _, a := range atoms {
		byFile[a.Span.File] = append(byFile[a.Span.File], a)
	}
	files := make([]string, 0, len(byFile))
	for rel := range byFile {
		files = append(files, rel)
	}
	sort.Strings(files)
	return byFile, files
}
