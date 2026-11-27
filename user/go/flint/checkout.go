package main

import (
	"strings"

	"virelai/vi"
)

// cmdCheckout implements `flint checkout --stream=<name> [--dir=<dir>]` —
// M215a. It replays the named Stream's head into an Atom table
// (ReplayStreamAtoms) and materializes every file the table represents
// under the destination directory: a file body blob when the version is
// stored verbatim, otherwise the canonical synthesis from per-Atom bodies.
// A stream committed before the content lane existed refuses `body_missing`
// by name rather than writing empty files. An existing destination file is
// guarded by `src_behind`: it must already carry the target Atoms, anything
// else is a diverged working copy.
func cmdCheckout(fs FS, root string, args []string) error {
	var streamName, dirFlag string
	for _, a := range args {
		switch {
		case strings.HasPrefix(a, "--stream="):
			streamName = a[len("--stream="):]
		case strings.HasPrefix(a, "--dir="):
			dirFlag = cleanMemPath(a[len("--dir="):])
		default:
			return refuse("usage", "")
		}
	}
	if streamName == "" {
		return refuse("usage", "")
	}

	idx, err := LoadStoreIndex(fs, root)
	if err != nil {
		return err
	}
	st, ok := idx.StreamsByName[streamName]
	if !ok {
		return refuse("stream_missing", streamName)
	}

	dest := dirFlag
	if dest == "" {
		// Default destination: the source directory of the workspace bound
		// to this stream, when exactly one exists.
		var bound *Workspace
		for _, w := range idx.Workspaces {
			if w.Stream == st.ID {
				if bound != nil {
					return refuse("ws_ambiguous", "")
				}
				bound = w
			}
		}
		if bound == nil {
			return refuse("usage", "")
		}
		dest = bound.Src
	}

	allChanges, err := LoadAllStreamChanges(fs, root, idx)
	if err != nil {
		return err
	}
	atoms, _, err := ReplayStreamAtoms(st.Head, allChanges)
	if err != nil {
		return err
	}
	if len(atoms) > MaxAtoms {
		return refuse("store_too_large", "atoms")
	}
	for _, a := range atoms {
		if a.Span.File == "" {
			return refuse("atom_malformed", a.ID)
		}
	}

	byFile, files := groupAtomsByFile(atoms)
	written, err := materializeFiles(fs, root, dest, byFile, files, nil)
	if err != nil {
		return err
	}
	consoleFn("flint: checkout ok stream=" + streamName +
		" files=" + vi.Itoa64(int64(written)))
	return nil
}
