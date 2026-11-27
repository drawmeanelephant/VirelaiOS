package main

import (
	"encoding/hex"
	"sort"
	"strings"

	"virelai/flint/parser"
)

// The rename/mv verbs record explicit identity intent after the source edit.
// They do not guess renames heuristically and they do not rewrite references:
// the user edits the tree, then names the Atom identity transition to FLINT.

func parseRenameArgs(args []string, nargs int) (string, []string, error) {
	var wsFlag string
	var positional []string
	for _, a := range args {
		if strings.HasPrefix(a, "--ws=") {
			wsFlag = a[len("--ws="):]
			continue
		}
		if strings.HasPrefix(a, "-") {
			return "", nil, refuse("usage", "")
		}
		positional = append(positional, a)
	}
	if len(positional) != nargs {
		return "", nil, refuse("usage", "")
	}
	return wsFlag, positional, nil
}

func findWorkspaceAtom(atoms []StoredAtom, arg string) (StoredAtom, bool) {
	var found *StoredAtom
	for i := range atoms {
		if atoms[i].ID == arg || atoms[i].Name == arg {
			if found != nil {
				return StoredAtom{}, false
			}
			found = &atoms[i]
		}
	}
	if found == nil {
		return StoredAtom{}, false
	}
	return *found, true
}

func parseWorkspaceFile(fs FS, srcRoot, rel string) ([]parser.Atom, []byte, error) {
	full := cleanMemPath(srcRoot + "/" + rel)
	src, err := fs.ReadFile(full, MaxFileReadBytes)
	if err != nil {
		return nil, nil, refuse("src_missing", rel)
	}
	pr, ok := parser.ForPath(rel)
	if !ok {
		return nil, nil, refuse("parse_unsupported", rel)
	}
	decls, perr := pr.Parse(rel, src)
	if perr != nil {
		if pe, ok := perr.(*parser.ParseError); ok {
			return nil, nil, refuse(pe.Code, pe.File)
		}
		return nil, nil, refuse("syntax", rel)
	}
	return decls, src, nil
}

func parserStoredAtom(id string, d parser.Atom) StoredAtom {
	return StoredAtomFromParser(id, d)
}

func declKey(rel string, d parser.Atom) AtomKey {
	return AtomKey{File: rel, Kind: d.Kind, Name: d.Name}
}

func atomKey(a StoredAtom) AtomKey {
	return AtomKey{File: a.Span.File, Kind: a.Kind, Name: a.Name}
}

func sameHash(hashHex string, digest [32]byte) bool {
	return hashHex == hex.EncodeToString(digest[:])
}

// reconcileUnaffectedAtoms refreshes spans for untouched tracked declarations
// in one parsed file and refuses src_behind when the explicit verb is being
// asked to sweep unrelated adds/modifies/deletes into the same stage. Allowed
// missing/extra keys are the two ends of the explicit move/rename itself.
func reconcileUnaffectedAtoms(atoms []StoredAtom, decls []parser.Atom, rel string, allowMissing, allowExtra, allowChanged map[AtomKey]bool) error {
	declByKey := make(map[AtomKey]parser.Atom, len(decls))
	for _, d := range decls {
		k := declKey(rel, d)
		if _, dup := declByKey[k]; dup {
			return refuse("duplicate_decl", rel+" "+d.Kind+" "+d.Name)
		}
		declByKey[k] = d
	}
	storedKeys := make(map[AtomKey]bool)
	for i := range atoms {
		a := &atoms[i]
		if a.Span.File != rel {
			continue
		}
		k := atomKey(*a)
		storedKeys[k] = true
		d, ok := declByKey[k]
		if !ok {
			if allowMissing[k] {
				continue
			}
			return refuse("src_behind", rel)
		}
		if !sameHash(a.Hash, d.Hash) || !sameHash(a.SigHash, d.SigHash) {
			if !allowChanged[k] {
				return refuse("src_behind", rel)
			}
			a.Hash = hex.EncodeToString(d.Hash[:])
			a.SigHash = hex.EncodeToString(d.SigHash[:])
		}
		a.Span = Span{File: rel, Start: d.Start, End: d.End}
	}
	for _, d := range decls {
		k := declKey(rel, d)
		if storedKeys[k] || allowExtra[k] {
			continue
		}
		return refuse("src_behind", rel)
	}
	return nil
}

func replaceAtom(atoms []StoredAtom, updated StoredAtom) {
	for i := range atoms {
		if atoms[i].ID == updated.ID {
			updated.Origin = atoms[i].Origin
			atoms[i] = updated
			return
		}
	}
}

func writeRefactorState(fs FS, root, wsName string, atoms []StoredAtom, stage, incoming []Op, files []string, sources map[string][]byte) error {
	sortStoredAtoms(atoms)
	folded := FoldStageOps(stage, incoming, atoms)
	if len(folded) > MaxAtoms {
		return refuse("store_too_large", "atoms")
	}
	sort.Strings(files)
	for _, rel := range files {
		src := sources[rel]
		if src == nil {
			continue
		}
		group := fileAtomGroup(atoms, rel)
		sortStoredAtoms(group)
		if err := storeFileBodies(fs, root, rel, src, group); err != nil {
			return err
		}
	}
	if err := WriteWorkspaceAtoms(fs, root, wsName, atoms); err != nil {
		return err
	}
	return WriteWorkspaceStage(fs, root, wsName, folded)
}

func loadRenameWorkspace(fs FS, root, wsFlag string) (*Workspace, []StoredAtom, []Op, error) {
	idx, err := LoadStoreIndex(fs, root)
	if err != nil {
		return nil, nil, nil, err
	}
	ws, err := resolveWorkspace(fs, idx, wsFlag, nil)
	if err != nil {
		return nil, nil, nil, err
	}
	atoms, err := LoadWorkspaceAtoms(fs, root, ws.Name)
	if err != nil {
		return nil, nil, nil, err
	}
	stage, err := LoadWorkspaceStage(fs, root, ws.Name)
	if err != nil {
		return nil, nil, nil, err
	}
	return ws, atoms, stage, nil
}

func sharedSpanAtoms(atoms []StoredAtom, old StoredAtom) (map[AtomKey]bool, map[AtomKey]StoredAtom) {
	allow := make(map[AtomKey]bool)
	before := make(map[AtomKey]StoredAtom)
	for _, a := range atoms {
		if a.ID == old.ID || a.Span.File != old.Span.File || a.Span.Start != old.Span.Start || a.Span.End != old.Span.End {
			continue
		}
		k := atomKey(a)
		allow[k] = true
		before[k] = a
	}
	return allow, before
}

func appendSharedModifyOps(incoming []Op, atoms []StoredAtom, before map[AtomKey]StoredAtom) []Op {
	for _, pre := range before {
		for _, a := range atoms {
			if a.ID != pre.ID || a.Hash == pre.Hash {
				continue
			}
			incoming = append(incoming, Op{Type: "modify", AtomID: a.ID, NewHash: a.Hash, NewSigHash: a.SigHash, OldHash: pre.Hash})
			break
		}
	}
	return incoming
}

// cmdRename implements `flint rename [--ws=<name>] <atom> <new-name>`.
func cmdRename(fs FS, root string, args []string) error {
	wsFlag, positional, err := parseRenameArgs(args, 2)
	if err != nil {
		return err
	}
	oldRef, newName := positional[0], positional[1]
	if newName == "" {
		return refuse("usage", "")
	}
	ws, atoms, stage, err := loadRenameWorkspace(fs, root, wsFlag)
	if err != nil {
		return err
	}
	old, ok := findWorkspaceAtom(atoms, oldRef)
	if !ok {
		return refuse("atom_missing", oldRef)
	}
	if old.Name == newName {
		return refuse("usage", "")
	}
	rel := old.Span.File
	decls, src, err := parseWorkspaceFile(fs, ws.Src, rel)
	if err != nil {
		return err
	}
	var target *parser.Atom
	for i := range decls {
		if decls[i].Kind == old.Kind && decls[i].Name == newName {
			target = &decls[i]
			break
		}
	}
	if target == nil {
		return refuse("atom_missing", newName)
	}
	for _, d := range decls {
		if d.Kind == old.Kind && d.Name == old.Name {
			return refuse("span_conflict", rel)
		}
	}
	for _, a := range atoms {
		if a.ID != old.ID && a.Span.File == rel && a.Kind == old.Kind && a.Name == newName {
			return refuse("duplicate_decl", rel+" "+old.Kind+" "+newName)
		}
	}

	oldKey := atomKey(old)
	newKey := AtomKey{File: rel, Kind: old.Kind, Name: newName}
	sharedChanged, sharedBefore := sharedSpanAtoms(atoms, old)
	if err := reconcileUnaffectedAtoms(atoms, decls, rel,
		map[AtomKey]bool{oldKey: true}, map[AtomKey]bool{newKey: true}, sharedChanged); err != nil {
		return err
	}

	updated := parserStoredAtom(old.ID, *target)
	incoming := []Op{{
		Type:    "rename",
		AtomID:  old.ID,
		NewName: newName,
		OldHash: old.Hash,
	}}
	if updated.Hash != old.Hash || updated.SigHash != old.SigHash {
		incoming = append(incoming, Op{
			Type:       "modify",
			AtomID:     old.ID,
			NewHash:    updated.Hash,
			NewSigHash: updated.SigHash,
			OldHash:    old.Hash,
		})
	}
	incoming = appendSharedModifyOps(incoming, atoms, sharedBefore)
	replaceAtom(atoms, updated)
	if err := writeRefactorState(fs, root, ws.Name, atoms, stage, incoming, []string{rel}, map[string][]byte{rel: src}); err != nil {
		return err
	}
	consoleFn("flint: rename ok " + old.Name + " " + newName)
	return nil
}

// cmdMv implements `flint mv [--ws=<name>] <atom> <new-file>`.
func cmdMv(fs FS, root string, args []string) error {
	wsFlag, positional, err := parseRenameArgs(args, 2)
	if err != nil {
		return err
	}
	atomRef, targetArg := positional[0], positional[1]
	ws, atoms, stage, err := loadRenameWorkspace(fs, root, wsFlag)
	if err != nil {
		return err
	}
	old, ok := findWorkspaceAtom(atoms, atomRef)
	if !ok {
		return refuse("atom_missing", atomRef)
	}

	var targetRel string
	if strings.HasPrefix(targetArg, "/") {
		var ok bool
		targetRel, ok = relToSrc(ws.Src, targetArg)
		if !ok || targetRel == "" {
			return refuse("src_missing", targetArg)
		}
	} else {
		targetRel = cleanMemPath(targetArg)
	}
	if targetRel == "" || strings.HasPrefix(targetRel, "../") || targetRel == ".." || strings.Contains(targetRel, "/../") {
		return refuse("path_unsafe", targetArg)
	}
	if _, ok := parser.ForPath(targetRel); !ok {
		return refuse("parse_unsupported", targetRel)
	}
	if targetRel == old.Span.File {
		return refuse("usage", "")
	}

	targetDecls, targetSrc, err := parseWorkspaceFile(fs, ws.Src, targetRel)
	if err != nil {
		return err
	}
	var target *parser.Atom
	for i := range targetDecls {
		if targetDecls[i].Kind == old.Kind && targetDecls[i].Name == old.Name {
			target = &targetDecls[i]
			break
		}
	}
	if target == nil {
		var candidates []*parser.Atom
		for i := range targetDecls {
			if targetDecls[i].Kind == old.Kind && sameHash(old.Hash, targetDecls[i].Hash) {
				candidates = append(candidates, &targetDecls[i])
			}
		}
		if len(candidates) > 1 {
			return refuse("span_conflict", targetRel)
		}
		if len(candidates) == 1 {
			target = candidates[0]
		}
	}
	if target == nil {
		return refuse("atom_missing", old.Name)
	}

	oldRel := old.Span.File
	var oldDecls []parser.Atom
	var oldSrc []byte
	if fs.Exists(cleanMemPath(ws.Src + "/" + oldRel)) {
		oldDecls, oldSrc, err = parseWorkspaceFile(fs, ws.Src, oldRel)
		if err != nil {
			return err
		}
		for _, d := range oldDecls {
			if d.Kind == old.Kind && d.Name == old.Name {
				return refuse("span_conflict", oldRel)
			}
		}
	} else {
		for _, a := range atoms {
			if a.ID != old.ID && a.Span.File == oldRel {
				return refuse("src_behind", oldRel)
			}
		}
	}

	for _, a := range atoms {
		if a.ID != old.ID && a.Span.File == targetRel && a.Kind == target.Kind && a.Name == target.Name {
			return refuse("duplicate_decl", targetRel+" "+target.Kind+" "+target.Name)
		}
	}

	oldKey := atomKey(old)
	targetKey := declKey(targetRel, *target)
	sharedChanged, sharedBefore := sharedSpanAtoms(atoms, old)
	if oldSrc != nil {
		if err := reconcileUnaffectedAtoms(atoms, oldDecls, oldRel,
			map[AtomKey]bool{oldKey: true}, nil, sharedChanged); err != nil {
			return err
		}
	}
	if err := reconcileUnaffectedAtoms(atoms, targetDecls, targetRel,
		nil, map[AtomKey]bool{targetKey: true}, nil); err != nil {
		return err
	}

	updated := parserStoredAtom(old.ID, *target)
	spanCopy := updated.Span
	incoming := []Op{{
		Type:    "move",
		AtomID:  old.ID,
		NewSpan: &spanCopy,
		OldHash: old.Hash,
	}}
	if updated.Name != old.Name {
		incoming = append(incoming, Op{Type: "rename", AtomID: old.ID, NewName: updated.Name, OldHash: old.Hash})
	}
	if updated.Hash != old.Hash || updated.SigHash != old.SigHash {
		incoming = append(incoming, Op{Type: "modify", AtomID: old.ID, NewHash: updated.Hash, NewSigHash: updated.SigHash, OldHash: old.Hash})
	}
	incoming = appendSharedModifyOps(incoming, atoms, sharedBefore)
	replaceAtom(atoms, updated)
	files := []string{targetRel}
	sources := map[string][]byte{targetRel: targetSrc}
	if oldSrc != nil {
		files = append(files, oldRel)
		sources[oldRel] = oldSrc
	}
	if err := writeRefactorState(fs, root, ws.Name, atoms, stage, incoming, files, sources); err != nil {
		return err
	}
	consoleFn("flint: mv ok " + old.Name + " " + targetRel)
	return nil
}
