package main

import (
	"bytes"
	"encoding/hex"
	"strings"

	"virelai/flint/parser"
)

// Materialization (M215a): the reverse direction of `flint add`. A target
// Atom table names the bodies that rebuild each source file — either a file
// body blob found verbatim under the file's Atom-hash tuple, or a canonical
// synthesis from the per-Atom bodies. The two call sites are
// `flint checkout` (a stream head's replayed table) and `flint merge` (the
// merged table); both share the `src_behind` guard shape from atoms.go.

// sameAtomIdentities reports whether a fresh parse of a file's bytes
// yields exactly the Atom set `atoms`, ignoring spans (byte offsets are a
// working-table property; a materialized layout may legitimately differ).
func sameAtomIdentities(decls []parser.Atom, atoms []StoredAtom) bool {
	if len(decls) != len(atoms) {
		return false
	}
	type ident struct {
		kind, name, hash, sighash string
	}
	got := make(map[ident]int, len(decls))
	for _, d := range decls {
		got[ident{d.Kind, d.Name, hex.EncodeToString(d.Hash[:]), hex.EncodeToString(d.SigHash[:])}]++
	}
	for _, a := range atoms {
		k := ident{a.Kind, a.Name, a.Hash, a.SigHash}
		got[k]--
		if got[k] < 0 {
			return false
		}
	}
	return true
}

// parseFileAtoms parses file bytes under the workspace-relative name rel.
// Any parse failure becomes `src_behind <rel>`: a destination that does not
// even parse is a diverged working copy, never something to clobber.
func parseFileAtoms(rel string, src []byte) ([]parser.Atom, error) {
	p, ok := parser.ForPath(rel)
	if !ok {
		return nil, refuse("src_behind", rel)
	}
	decls, err := p.Parse(rel, src)
	if err != nil {
		return nil, refuse("src_behind", rel)
	}
	return decls, nil
}

// safeDestPath joins a stored relative file name onto a destination
// directory, refusing traversal (`path_unsafe <rel>`).
func safeDestPath(destDir, rel string) (string, error) {
	if rel == "" || strings.HasPrefix(rel, "/") || strings.HasPrefix(rel, "~") ||
		rel == ".." || strings.HasPrefix(rel, "../") ||
		strings.Contains(rel, "/../") || strings.HasSuffix(rel, "/..") {
		return "", refuse("path_unsafe", rel)
	}
	return cleanMemPath(destDir + "/" + rel), nil
}

// verifyFileBody checks that blob bytes re-parse to exactly the expected
// Atom set; a stored body that fails is `body_corrupt`, never trusted on
// its name alone.
func verifyFileBody(rel string, body []byte, atoms []StoredAtom) error {
	p, ok := parser.ForPath(rel)
	if !ok {
		return refuse("body_corrupt", rel)
	}
	decls, err := p.Parse(rel, body)
	if err != nil {
		return refuse("body_corrupt", rel)
	}
	if !sameAtomIdentities(decls, atoms) {
		return refuse("body_corrupt", rel)
	}
	return nil
}

// readAtomBody reads the M215b name-aware body first, then the M215a
// hash-only key as a compatibility fallback for pre-rename stores.
func readAtomBody(fs FS, root string, a StoredAtom) ([]byte, error) {
	key, err := atomBodyKey(a)
	if err != nil {
		return nil, err
	}
	body, err := readBlob(fs, root, key)
	if err != nil || body != nil {
		return body, err
	}
	return readBlob(fs, root, a.Hash)
}

// emitCanonicalFile synthesizes a parseable source file carrying exactly
// `atoms`: the language's package clause (Go only — derived from the
// qualified names, which the table always carries) plus every Atom body,
// read from blobs/<atom.hash>, in Span order. All `const` Atoms share one
// group so a valueless spec keeps its iota index; spans sharing a byte
// range (multi-name specs) emit once. A missing Atom body is the
// `body_missing` refusal a pre-content-store stream must produce. The
// synthesized bytes are verified by re-parse: anything short of the exact
// Atom set is `body_mismatch`.
func emitCanonicalFile(fs FS, root, rel string, atoms []StoredAtom) ([]byte, error) {
	var out bytes.Buffer
	goFile := strings.HasSuffix(rel, ".go")
	if goFile {
		pkg := ""
		for _, a := range atoms {
			if i := strings.IndexByte(a.Name, '.'); i > 0 {
				pkg = a.Name[:i]
				break
			}
		}
		out.WriteString("package ")
		out.WriteString(pkg)
		out.WriteString("\n\n")
	}
	// On Go the GenDecl keyword sits outside a spec's span; the atom body is
	// bare spec text and needs its keyword back. Zig spans cover the whole
	// declaration, so their bodies are emitted verbatim.
	goKeyword := func(kind string) string {
		if !goFile {
			return ""
		}
		switch kind {
		case "var":
			return "var "
		case "type", "type_alias", "struct", "interface":
			return "type "
		}
		return ""
	}
	bodyOf := func(a StoredAtom) ([]byte, error) {
		body, err := readAtomBody(fs, root, a)
		if err != nil {
			return nil, err
		}
		if body == nil {
			return nil, refuse("body_missing", rel+" "+a.Name)
		}
		return body, nil
	}
	type spanKey struct{ start, end uint32 }
	seen := map[spanKey]bool{}
	var consts []StoredAtom
	var others []StoredAtom
	for _, a := range atoms {
		k := spanKey{a.Span.Start, a.Span.End}
		if seen[k] {
			continue
		}
		seen[k] = true
		if a.Kind == "const" {
			consts = append(consts, a)
		} else {
			others = append(others, a)
		}
	}
	var decls []string
	writeBody := func(a StoredAtom) error {
		body, err := bodyOf(a)
		if err != nil {
			return err
		}
		decls = append(decls, goKeyword(a.Kind)+strings.TrimRight(string(body), "\n"))
		return nil
	}
	if len(consts) > 0 {
		var group strings.Builder
		group.WriteString("const (\n")
		for _, a := range consts {
			body, err := bodyOf(a)
			if err != nil {
				return nil, err
			}
			group.WriteString(strings.TrimRight(string(body), "\n"))
			group.WriteByte('\n')
		}
		group.WriteString(")")
		decls = append(decls, group.String())
	}
	for _, a := range others {
		if err := writeBody(a); err != nil {
			return nil, err
		}
	}
	// Declarations join on a blank line; the file ends on a single newline.
	out.WriteString(strings.Join(decls, "\n\n"))
	out.WriteByte('\n')
	content := out.Bytes()
	p, ok := parser.ForPath(rel)
	if !ok {
		return nil, refuse("body_mismatch", rel)
	}
	parsed, err := p.Parse(rel, content)
	if err != nil {
		return nil, refuse("body_mismatch", rel)
	}
	if !sameAtomIdentities(parsed, atoms) {
		return nil, refuse("body_mismatch", rel)
	}
	return content, nil
}

// planFileMaterialization resolves the bytes a file must hold so its
// re-parse reproduces `atoms` (the file's entries in the target Atom table,
// already in table order). Fast path: a file body blob keyed by the
// Atom-hash tuple stores the version verbatim — byte-exact, so spans land
// identically too. Otherwise the file is synthesized canonically and the
// result is stored under the tuple so the next materialization is
// byte-stable.
func planFileMaterialization(fs FS, root, rel string, atoms []StoredAtom) ([]byte, error) {
	key, err := fileBlobKey(rel, atoms)
	if err != nil {
		return nil, err
	}
	body, err := readBlob(fs, root, key)
	if err != nil {
		return nil, err
	}
	if body == nil {
		// M215a stores used the name-insensitive FLINT-SRC/1 tuple. Read it
		// only as a compatibility fallback; new writes always use the v2 key.
		legacyKey, kerr := fileBlobKeyV1(rel, atoms)
		if kerr != nil {
			return nil, kerr
		}
		body, err = readBlob(fs, root, legacyKey)
		if err != nil {
			return nil, err
		}
	}
	if body != nil {
		if err := verifyFileBody(rel, body, atoms); err != nil {
			return nil, err
		}
		return body, nil
	}
	content, err := emitCanonicalFile(fs, root, rel, atoms)
	if err != nil {
		return nil, err
	}
	if err := storeBlob(fs, root, key, content); err != nil {
		return nil, err
	}
	return content, nil
}

// srcBehindCheck enforces the materialization guard shared with atoms.go's
// `src_behind <file>`: a destination file that exists must hold either the
// target Atom set (already materialized) or the allowed base set (the clean
// pre-write working copy). Anything else is a user edit the emitter must
// not clobber. `base` is nil for checkout, where only the target is valid.
func srcBehindCheck(fs FS, dest, rel string, target, base []StoredAtom) error {
	if !fs.Exists(dest) {
		if len(base) > 0 {
			// Tracked file deleted from the working copy: diverged.
			return refuse("src_behind", rel)
		}
		return nil
	}
	disk, err := fs.ReadFile(dest, MaxFileReadBytes)
	if err != nil {
		return err
	}
	decls, err := parseFileAtoms(rel, disk)
	if err != nil {
		return err
	}
	if sameAtomIdentities(decls, target) {
		return nil
	}
	if base != nil && sameAtomIdentities(decls, base) {
		return nil
	}
	return refuse("src_behind", rel)
}

// filePlan is one materialization decision: the destination, the bytes to
// write (nil to remove), or a no-op when the destination already holds the
// target bytes.
type filePlan struct {
	dest    string
	content []byte
	remove  bool
	skip    bool
}

// planMaterialization resolves every target file under destDir and checks
// the src_behind guard against `baseByFile` (nil for checkout, the
// pre-merge table for merge) — without writing any source file. It may
// still store canonical file bodies under blobs/<key>: content-addressed
// writes that are harmless even if a later step refuses. All named refusals
// (`path_unsafe`, `src_behind`, `body_missing`, `body_corrupt`,
// `body_mismatch`) happen here, before the caller mutates history.
func planMaterialization(fs FS, root, destDir string, byFile map[string][]StoredAtom, files []string, baseByFile map[string][]StoredAtom) ([]filePlan, error) {
	plans := make([]filePlan, 0, len(files))
	for _, rel := range files {
		target := byFile[rel]
		base := baseByFile[rel]
		dest, err := safeDestPath(destDir, rel)
		if err != nil {
			return nil, err
		}
		p := filePlan{dest: dest}
		if len(target) == 0 {
			// The merged table drops this file: remove it when the working
			// copy is still the clean base.
			if err := srcBehindCheck(fs, dest, rel, nil, base); err != nil {
				return nil, err
			}
			p.remove = fs.Exists(dest)
			plans = append(plans, p)
			continue
		}
		content, err := planFileMaterialization(fs, root, rel, target)
		if err != nil {
			return nil, err
		}
		if fs.Exists(dest) {
			disk, err := fs.ReadFile(dest, MaxFileReadBytes)
			if err != nil {
				return nil, err
			}
			if bytes.Equal(disk, content) {
				p.skip = true
				plans = append(plans, p)
				continue
			}
		}
		if err := srcBehindCheck(fs, dest, rel, target, base); err != nil {
			return nil, err
		}
		p.content = content
		plans = append(plans, p)
	}
	return plans, nil
}

// execMaterialization applies a checked plan set and returns the number of
// files materialized.
func execMaterialization(fs FS, plans []filePlan) (int, error) {
	written := 0
	for _, p := range plans {
		switch {
		case p.skip:
			written++
		case p.remove:
			if err := fs.Remove(p.dest); err != nil {
				return 0, err
			}
			written++
		default:
			dir := p.dest[:strings.LastIndexByte(p.dest, '/')]
			if err := ensureDirAll(fs, dir); err != nil {
				return 0, err
			}
			if err := fs.WriteFileSafe(p.dest, p.content); err != nil {
				return 0, err
			}
			written++
		}
	}
	return written, nil
}

// materializeFiles plans and emits every target file under destDir in one
// step — the checkout path, where nothing else mutates history.
func materializeFiles(fs FS, root, destDir string, byFile map[string][]StoredAtom, files []string, baseByFile map[string][]StoredAtom) (int, error) {
	plans, err := planMaterialization(fs, root, destDir, byFile, files, baseByFile)
	if err != nil {
		return 0, err
	}
	return execMaterialization(fs, plans)
}
