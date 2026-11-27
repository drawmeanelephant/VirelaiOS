package main

import (
	"encoding/hex"

	"virelai/flint/parser"
	"virelai/flintstore"
)

// storeRoot is the CLI's active store root (tests replace it).
var storeRoot = DefaultStoreRoot

// StoredAtomFromParser converts a parsed Atom plus a UUIDv7 id into a
// StoredAtom — workspace policy, so it stays with the CLI rather than the
// shared store.
func StoredAtomFromParser(id string, a parser.Atom) StoredAtom {
	var meta map[string]string
	if len(a.Meta) > 0 {
		meta = make(map[string]string, len(a.Meta))
		for k, v := range a.Meta {
			meta[k] = v
		}
	}
	return flintstore.StoredAtom{
		ID:      id,
		Kind:    a.Kind,
		Name:    a.Name,
		Hash:    hex.EncodeToString(a.Hash[:]),
		SigHash: hex.EncodeToString(a.SigHash[:]),
		Span: Span{
			File:  a.File,
			Start: a.Start,
			End:   a.End,
		},
		Metadata: meta,
	}
}
