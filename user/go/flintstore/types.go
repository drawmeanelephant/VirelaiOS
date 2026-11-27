// Package flintstore is the shared FLINT core: ADR 0049 stream/change types,
// canonical encodings and Ed25519 signatures, bounded flat-file store IO and
// torn-append/head recovery.
//
// Both the FLINT.ELF workspace CLI (user/go/flint) and the serialized guest
// daemon FLINTD.ELF (user/go/flintd) import this package. Parser and workspace
// command policy stay in the CLI; the daemon only serves the store.
package flintstore

import "unicode/utf8"

const (
	// DefaultStoreRoot is the guest share path the FLINT CLI and daemon use
	// when the caller names no other root.
	DefaultStoreRoot = "/host/FLINT"
	// MaxFileReadBytes bounds every store file read (4 MiB).
	MaxFileReadBytes = 4 * 1024 * 1024
	// MaxAtoms bounds a workspace ATOMS.jsonl/STAGE.jsonl (4096).
	MaxAtoms = 4096
	// MaxChanges is the global change-count bound (4096) and the per-stream
	// cap the wire protocol applies.
	MaxChanges = 4096
	// MaxLineBytes bounds one wire NDJSON request/response line (4 MiB).
	MaxLineBytes = 4 * 1024 * 1024
	// MaxMessageBytes bounds a Change message (512 UTF-8 bytes).
	MaxMessageBytes = 512
	// MaxStreamNameBytes bounds a stream name (`[a-z0-9-]{1,32}`).
	MaxStreamNameBytes = 32
	// DefaultReadLimit is the `read` page size when the request omits limit.
	DefaultReadLimit = 256
	// MaxReadLimit is the largest `read` page size; larger asks clamp.
	MaxReadLimit = 1024
)

// Span locates an Atom within a source file relative to the workspace src root.
type Span struct {
	File  string `json:"file"`
	Start uint32 `json:"start"`
	End   uint32 `json:"end"`
}

// StoredAtom is the JSON representation of an Atom in ATOMS.jsonl.
type StoredAtom struct {
	ID       string            `json:"id"`
	Kind     string            `json:"kind"`
	Name     string            `json:"name"`
	Hash     string            `json:"hash"`
	SigHash  string            `json:"sig_hash"`
	Span     Span              `json:"span"`
	Metadata map[string]string `json:"metadata,omitempty"`
	Origin   string            `json:"origin,omitempty"`
}

// Op is a staged or committed operation on an Atom (STAGE.jsonl / Change.ops).
type Op struct {
	Type       string `json:"type"`
	AtomID     string `json:"atom_id"`
	Kind       string `json:"kind,omitempty"`
	NewName    string `json:"new_name,omitempty"`
	NewSpan    *Span  `json:"new_span,omitempty"`
	NewHash    string `json:"new_hash,omitempty"`
	NewSigHash string `json:"new_sig_hash,omitempty"`
	OldHash    string `json:"old_hash,omitempty"`
}

// ForkOrigin records the Stream and Change a forked Stream started from.
type ForkOrigin struct {
	Stream string `json:"stream"`
	Change string `json:"change"`
}

// Stream is the JSON record stored at streams/<id>.json.
type Stream struct {
	ID         string      `json:"id"`
	Name       string      `json:"name"`
	Head       *string     `json:"head"`
	Created    int64       `json:"created"`
	ForkedFrom *ForkOrigin `json:"forked_from"`
}

// Workspace is the JSON record stored at ws/<name>/WORKSPACE.json.
type Workspace struct {
	Name    string `json:"name"`
	Src     string `json:"src"`
	Stream  string `json:"stream"`
	Created int64  `json:"created"`
}

// Change is the JSON representation of a signed Change line in
// streams/<id>.jsonl.
type Change struct {
	ID      string  `json:"id"`
	Parent  *string `json:"parent"`
	Stream  string  `json:"stream"`
	TS      int64   `json:"ts"`
	Author  string  `json:"author"`
	Message string  `json:"message"`
	Ops     []Op    `json:"ops"`
	Merged  *string `json:"merged"`
	Sig     string  `json:"sig"`
}

// ValidStreamName reports whether s is `[a-z0-9-]{1,32}`.
func ValidStreamName(s string) bool {
	if len(s) == 0 || len(s) > MaxStreamNameBytes {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		if !((c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') || c == '-') {
			return false
		}
	}
	return true
}

// ValidMessage reports whether s is 1..512 bytes of valid UTF-8 that is not
// all whitespace — the ADR 0049 change-message rule.
func ValidMessage(s string) bool {
	if len(s) == 0 || len(s) > MaxMessageBytes {
		return false
	}
	trimmed := 0
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case ' ', '\t', '\r', '\n':
		default:
			trimmed++
		}
	}
	return trimmed > 0 && utf8.ValidString(s)
}
