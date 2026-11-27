package main

import (
	"strings"

	"virelai/vi"
)

// FoldStageOps folds newly produced Ops into the existing staged Ops per
// ADR 0049 §1 and §2 step 2:
//   - add + modify -> add (with newest hash, sig_hash, and span)
//   - add + delete -> nothing (removed from stage)
//   - modify + modify -> modify (first old_hash -> last new_hash)
//   - modify + delete -> delete (first old_hash)
func FoldStageOps(existing []Op, incoming []Op, currentAtoms []StoredAtom) []Op {
	byAtom := make(map[string]map[string]Op, len(existing)+len(incoming))
	put := func(op Op) {
		ops := byAtom[op.AtomID]
		if ops == nil {
			ops = map[string]Op{}
			byAtom[op.AtomID] = ops
		}
		ops[op.Type] = op
	}
	for _, op := range existing {
		put(op)
	}

	earliestOldHash := func(ops map[string]Op, fallback string) string {
		for _, typ := range []string{"modify", "move", "rename", "delete"} {
			if op, ok := ops[typ]; ok && op.OldHash != "" {
				return op.OldHash
			}
		}
		return fallback
	}

	for _, op := range incoming {
		ops := byAtom[op.AtomID]
		if ops == nil {
			ops = map[string]Op{}
			byAtom[op.AtomID] = ops
		}

		if op.Type == "delete" {
			if _, wasAdd := ops["add"]; wasAdd {
				delete(byAtom, op.AtomID)
				continue
			}
			oldHash := earliestOldHash(ops, op.OldHash)
			byAtom[op.AtomID] = map[string]Op{
				"delete": {Type: "delete", AtomID: op.AtomID, OldHash: oldHash},
			}
			continue
		}

		if add, wasAdd := ops["add"]; wasAdd && op.Type != "add" {
			switch op.Type {
			case "modify":
				add.NewHash = op.NewHash
				add.NewSigHash = op.NewSigHash
				if op.NewSpan != nil {
					sp := *op.NewSpan
					add.NewSpan = &sp
				}
			case "rename":
				add.NewName = op.NewName
			case "move":
				add.NewSpan = op.NewSpan
			}
			ops["add"] = add
			continue
		}

		if prev, same := ops[op.Type]; same {
			switch op.Type {
			case "modify":
				if prev.OldHash != "" {
					op.OldHash = prev.OldHash
				}
			case "move", "rename":
				if prev.OldHash != "" {
					op.OldHash = prev.OldHash
				}
			}
		}
		ops[op.Type] = op
	}

	spanByID := make(map[string]Span, len(currentAtoms))
	for _, a := range currentAtoms {
		spanByID[a.ID] = a.Span
	}
	for id, ops := range byAtom {
		if add, ok := ops["add"]; ok {
			if sp, ok := spanByID[id]; ok {
				spCopy := sp
				add.NewSpan = &spCopy
				ops["add"] = add
			}
		}
	}

	out := make([]Op, 0, len(byAtom))
	for _, ops := range byAtom {
		for _, op := range ops {
			out = append(out, op)
		}
	}
	sortOps(out)
	return out
}

// cmdStatus implements `flint status [--ws=<name>]`.
func cmdStatus(fs FS, root string, args []string) error {
	var wsFlag string
	for _, a := range args {
		if strings.HasPrefix(a, "--ws=") {
			wsFlag = a[len("--ws="):]
			continue
		}
		return refuse("usage", "")
	}

	idx, err := LoadStoreIndex(fs, root)
	if err != nil {
		return err
	}
	ws, err := resolveWorkspace(fs, idx, wsFlag, nil)
	if err != nil {
		return err
	}

	atoms, err := LoadWorkspaceAtoms(fs, root, ws.Name)
	if err != nil {
		return err
	}
	stage, err := LoadWorkspaceStage(fs, root, ws.Name)
	if err != nil {
		return err
	}

	atomsByID := make(map[string]StoredAtom, len(atoms))
	seenKeys := make(map[AtomKey]bool, len(atoms))
	duplicates := 0
	for _, a := range atoms {
		atomsByID[a.ID] = a
		k := AtomKey{File: a.Span.File, Kind: a.Kind, Name: a.Name}
		if seenKeys[k] {
			duplicates++
		}
		seenKeys[k] = true
	}

	streamName := ws.Stream
	if st, ok := idx.StreamsByID[ws.Stream]; ok {
		streamName = st.Name
	}

	var countAdd, countMod, countDel, countMove, countRename int
	for _, op := range stage {
		switch op.Type {
		case "add":
			countAdd++
		case "modify":
			countMod++
		case "delete":
			countDel++
		case "move":
			countMove++
		case "rename":
			countRename++
		}
	}

	consoleFn("flint: status ws=" + ws.Name +
		" stream=" + streamName +
		" atoms=" + vi.Itoa64(int64(len(atoms))) +
		" staged=" + vi.Itoa64(int64(len(stage))) +
		" add=" + vi.Itoa64(int64(countAdd)) +
		" modify=" + vi.Itoa64(int64(countMod)) +
		" delete=" + vi.Itoa64(int64(countDel)) +
		" move=" + vi.Itoa64(int64(countMove)) +
		" rename=" + vi.Itoa64(int64(countRename)) +
		" duplicates=" + vi.Itoa64(int64(duplicates)))

	sortOps(stage)
	const stageCap = 64
	limit := len(stage)
	if limit > stageCap {
		limit = stageCap
	}
	for _, op := range stage[:limit] {
		name := opQualifiedName(op, atomsByID)
		consoleFn("flint: stage " + op.Type + " " + name)
	}
	if len(stage) > stageCap {
		consoleFn("flint: stage … +" + vi.Itoa64(int64(len(stage)-stageCap)))
	}
	return nil
}

func opQualifiedName(op Op, atomsByID map[string]StoredAtom) string {
	if op.NewName != "" {
		return op.NewName
	}
	if a, ok := atomsByID[op.AtomID]; ok && a.Name != "" {
		return a.Name
	}
	return op.AtomID
}
