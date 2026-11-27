package flintstore

import (
	"bytes"
	"encoding/json"
	"sort"
	"strings"
)

// StreamsDir returns <root>/streams.
func StreamsDir(root string) string { return root + "/streams" }

// StreamRecordPath returns <root>/streams/<id>.json.
func StreamRecordPath(root, id string) string { return root + "/streams/" + id + ".json" }

// StreamLogPath returns <root>/streams/<id>.jsonl.
func StreamLogPath(root, id string) string { return root + "/streams/" + id + ".jsonl" }

// WorkspacesDir returns <root>/ws.
func WorkspacesDir(root string) string { return root + "/ws" }

// WorkspaceDir returns <root>/ws/<name>.
func WorkspaceDir(root, ws string) string { return root + "/ws/" + ws }

// WorkspaceConfigPath returns <root>/ws/<name>/WORKSPACE.json.
func WorkspaceConfigPath(root, ws string) string {
	return root + "/ws/" + ws + "/WORKSPACE.json"
}

// WorkspaceAtomsPath returns <root>/ws/<name>/ATOMS.jsonl.
func WorkspaceAtomsPath(root, ws string) string {
	return root + "/ws/" + ws + "/ATOMS.jsonl"
}

// WorkspaceStagePath returns <root>/ws/<name>/STAGE.jsonl.
func WorkspaceStagePath(root, ws string) string {
	return root + "/ws/" + ws + "/STAGE.jsonl"
}

// WorkspaceMergePath returns <root>/ws/<name>/MERGE.json.
func WorkspaceMergePath(root, ws string) string {
	return root + "/ws/" + ws + "/MERGE.json"
}

// ReadJSONLLines splits a JSONL payload into lines without their trailing
// '\n' or one trailing '\r' and enforces the ADR 0049 torn-append rule: any
// non-empty payload whose final byte is not '\n' is refused with
// `stream_torn`; an empty inner line is `change_malformed`.
func ReadJSONLLines(data []byte) ([][]byte, error) {
	if len(data) == 0 {
		return nil, nil
	}
	if data[len(data)-1] != '\n' {
		return nil, Refuse("stream_torn", "")
	}
	trimmed := data[:len(data)-1]
	lines := bytes.Split(trimmed, []byte{'\n'})
	for i, line := range lines {
		if len(line) > 0 && line[len(line)-1] == '\r' {
			line = line[:len(line)-1]
			lines[i] = line
		}
		if len(line) == 0 {
			return nil, Refuse("change_malformed", "")
		}
	}
	return lines, nil
}

// StoreIndex holds the in-memory index built on each invocation from flat
// files.
type StoreIndex struct {
	Root          string
	Streams       []*Stream // sorted by (name, id): the `list` order
	StreamsByID   map[string]*Stream
	StreamsByName map[string]*Stream
	Workspaces    map[string]*Workspace
	ChangeCount   int
}

// FindByID resolves a canonical stream id.
func (idx *StoreIndex) FindByID(id string) *Stream { return idx.StreamsByID[id] }

// FindByName resolves a stream name.
func (idx *StoreIndex) FindByName(name string) *Stream { return idx.StreamsByName[name] }

// FindByNameOrID resolves a canonical id first, then a stream name.
func (idx *StoreIndex) FindByNameOrID(nameOrID string) *Stream {
	if st := idx.FindByID(nameOrID); st != nil {
		return st
	}
	return idx.FindByName(nameOrID)
}

// SortedByID returns the indexed streams in canonical id order.
func (idx *StoreIndex) SortedByID() []*Stream {
	out := make([]*Stream, len(idx.Streams))
	copy(out, idx.Streams)
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

func sortStreams(streams []*Stream) {
	sort.Slice(streams, func(i, j int) bool {
		if streams[i].Name != streams[j].Name {
			return streams[i].Name < streams[j].Name
		}
		return streams[i].ID < streams[j].ID
	})
}

// validateStreamRecord checks the ADR 0049 record shape and that the record's
// id matches its file stem.
func validateStreamRecord(st *Stream, stem string) error {
	if st.ID == "" || st.ID != stem || !ValidStreamName(st.Name) || st.Created < 0 {
		return Refuse("stream_malformed", stem)
	}
	if st.Head != nil {
		if _, ok := ParseUUID(*st.Head); !ok {
			return Refuse("stream_malformed", stem)
		}
	}
	if st.ForkedFrom != nil {
		if _, ok := ParseUUID(st.ForkedFrom.Stream); !ok {
			return Refuse("stream_malformed", stem)
		}
		if _, ok := ParseUUID(st.ForkedFrom.Change); !ok {
			return Refuse("stream_malformed", stem)
		}
	}
	return nil
}

func parseStreamRecord(b []byte, stem string) (*Stream, error) {
	var st Stream
	if err := json.Unmarshal(b, &st); err != nil {
		return nil, Refuse("stream_malformed", stem)
	}
	if err := validateStreamRecord(&st, stem); err != nil {
		return nil, err
	}
	return &st, nil
}

// LoadStoreIndex scans <root>/streams and <root>/ws into memory. It is the
// CLI's loader: a log without a record is still counted toward the global
// Change bound and a record whose head names no Change is returned as-is.
func LoadStoreIndex(fs FS, root string) (*StoreIndex, error) {
	return loadStoreIndex(fs, root, false)
}

// LoadStoreIndexStrict applies the host flintd store-layout rules on top of
// the shared loader: canonical UUID stems, every log paired with a record, a
// record id matching its stem, unique ids and names, and an empty stream head
// equal to its fork origin. FLINTD.ELF uses it so the guest daemon and the
// host reference server accept and refuse the same layouts.
func LoadStoreIndexStrict(fs FS, root string) (*StoreIndex, error) {
	return loadStoreIndex(fs, root, true)
}

func loadStoreIndex(fs FS, root string, strict bool) (*StoreIndex, error) {
	idx := &StoreIndex{
		Root:          root,
		Streams:       []*Stream{},
		StreamsByID:   map[string]*Stream{},
		StreamsByName: map[string]*Stream{},
		Workspaces:    map[string]*Workspace{},
	}

	sDir := StreamsDir(root)
	if fs.Exists(sDir) {
		entries, err := fs.List(sDir)
		if err != nil {
			return nil, err
		}
		jsonStems := map[string]bool{}
		jsonlStems := map[string]bool{}
		for _, e := range entries {
			if e.IsDir || strings.Contains(e.Name, ".tmp-") {
				continue
			}
			switch {
			case strings.HasSuffix(e.Name, ".json"):
				stem := strings.TrimSuffix(e.Name, ".json")
				if strict {
					if _, ok := ParseUUID(stem); !ok {
						return nil, Refuse("stream_malformed", e.Name)
					}
				}
				jsonStems[stem] = true
			case strings.HasSuffix(e.Name, ".jsonl"):
				stem := strings.TrimSuffix(e.Name, ".jsonl")
				if strict {
					if _, ok := ParseUUID(stem); !ok {
						return nil, Refuse("stream_malformed", e.Name)
					}
				}
				jsonlStems[stem] = true
			}
		}
		if strict {
			for stem := range jsonlStems {
				if !jsonStems[stem] {
					return nil, Refuse("stream_malformed", stem+".jsonl")
				}
			}
		}

		stems := make([]string, 0, len(jsonStems))
		for stem := range jsonStems {
			stems = append(stems, stem)
		}
		sort.Strings(stems)

		for _, stem := range stems {
			b, err := fs.ReadFile(StreamRecordPath(root, stem), MaxFileReadBytes)
			if err != nil {
				return nil, err
			}
			st, err := parseStreamRecord(b, stem)
			if err != nil {
				return nil, err
			}
			if strict {
				if idx.StreamsByID[st.ID] != nil || idx.StreamsByName[st.Name] != nil {
					return nil, Refuse("stream_malformed", stem)
				}
			}

			if strict {
				// The daemon validates the whole stream here — parent chain,
				// every signature, and head recovery — so list/heads/open can
				// never advertise a stream that read would refuse.
				loaded, lerr := loadStreamInternal(fs, root, stem, true, true)
				if lerr != nil {
					return nil, lerr
				}
				idx.ChangeCount += len(loaded.Changes)
				if idx.ChangeCount > MaxChanges {
					return nil, Refuse("store_too_large", "changes")
				}
				st = loaded.Stream
			} else if jsonlStems[stem] {
				logBytes, err := fs.ReadFile(StreamLogPath(root, stem), MaxFileReadBytes)
				if err != nil {
					return nil, err
				}
				lines, err := ReadJSONLLines(logBytes)
				if err != nil {
					return nil, err
				}
				idx.ChangeCount += len(lines)
				if idx.ChangeCount > MaxChanges {
					return nil, Refuse("store_too_large", "changes")
				}
				if len(lines) > 0 {
					last, err := ParseChange(lines[len(lines)-1])
					if err != nil {
						return nil, err
					}
					if !PtrEqual(st.Head, &last.ID) {
						loaded, lerr := loadStreamInternal(fs, root, stem, false, false)
						if lerr != nil {
							// The CLI tolerates an unknown record head (a
							// simulated concurrent head move); the daemon
							// refuses it above.
							if RefusalName(lerr) != "stream_malformed" {
								return nil, lerr
							}
						} else {
							st = loaded.Stream
						}
					}
				}
			}

			idx.Streams = append(idx.Streams, st)
			idx.StreamsByID[st.ID] = st
			idx.StreamsByName[st.Name] = st
		}
		// Orphan logs still count toward the global bound in the CLI's
		// tolerant mode (`store_too_large changes`).
		if !strict {
			for stem := range jsonlStems {
				if jsonStems[stem] {
					continue
				}
				logBytes, err := fs.ReadFile(StreamLogPath(root, stem), MaxFileReadBytes)
				if err != nil {
					return nil, err
				}
				lines, err := ReadJSONLLines(logBytes)
				if err != nil {
					return nil, err
				}
				idx.ChangeCount += len(lines)
				if idx.ChangeCount > MaxChanges {
					return nil, Refuse("store_too_large", "changes")
				}
			}
		}
		if idx.ChangeCount > MaxChanges {
			return nil, Refuse("store_too_large", "changes")
		}
		sortStreams(idx.Streams)
	}

	wDir := WorkspacesDir(root)
	if fs.Exists(wDir) {
		entries, err := fs.List(wDir)
		if err != nil {
			return nil, err
		}
		for _, e := range entries {
			if !e.IsDir {
				continue
			}
			cfgPath := WorkspaceConfigPath(root, e.Name)
			if !fs.Exists(cfgPath) {
				continue
			}
			b, err := fs.ReadFile(cfgPath, MaxFileReadBytes)
			if err != nil {
				return nil, err
			}
			var ws Workspace
			if err := json.Unmarshal(b, &ws); err != nil || ws.Name == "" {
				return nil, Refuse("ws_malformed", e.Name)
			}
			idx.Workspaces[ws.Name] = &ws
		}
	}

	return idx, nil
}

// LoadWorkspaceAtoms reads ws/<name>/ATOMS.jsonl and enforces the 4096-Atom
// bound.
func LoadWorkspaceAtoms(fs FS, root, ws string) ([]StoredAtom, error) {
	p := WorkspaceAtomsPath(root, ws)
	if !fs.Exists(p) {
		return nil, nil
	}
	b, err := fs.ReadFile(p, MaxFileReadBytes)
	if err != nil {
		return nil, err
	}
	lines, err := ReadJSONLLines(b)
	if err != nil {
		return nil, err
	}
	if len(lines) > MaxAtoms {
		return nil, Refuse("store_too_large", "atoms")
	}
	atoms := make([]StoredAtom, 0, len(lines))
	for _, line := range lines {
		var a StoredAtom
		if err := json.Unmarshal(line, &a); err != nil || a.ID == "" || a.Kind == "" || a.Name == "" {
			return nil, Refuse("atom_malformed", "")
		}
		atoms = append(atoms, a)
	}
	return atoms, nil
}

// SortStoredAtoms orders atoms deterministically for ATOMS.jsonl.
func SortStoredAtoms(atoms []StoredAtom) {
	sort.Slice(atoms, func(i, j int) bool {
		if atoms[i].Span.File != atoms[j].Span.File {
			return atoms[i].Span.File < atoms[j].Span.File
		}
		if atoms[i].Span.Start != atoms[j].Span.Start {
			return atoms[i].Span.Start < atoms[j].Span.Start
		}
		if atoms[i].Kind != atoms[j].Kind {
			return atoms[i].Kind < atoms[j].Kind
		}
		if atoms[i].Name != atoms[j].Name {
			return atoms[i].Name < atoms[j].Name
		}
		return atoms[i].ID < atoms[j].ID
	})
}

// WriteWorkspaceAtoms writes ws/<name>/ATOMS.jsonl deterministically via
// WriteFileSafe.
func WriteWorkspaceAtoms(fs FS, root, ws string, atoms []StoredAtom) error {
	if len(atoms) > MaxAtoms {
		return Refuse("store_too_large", "atoms")
	}
	sorted := make([]StoredAtom, len(atoms))
	copy(sorted, atoms)
	SortStoredAtoms(sorted)
	var buf bytes.Buffer
	for _, a := range sorted {
		line, err := json.Marshal(a)
		if err != nil {
			return err
		}
		buf.Write(line)
		buf.WriteByte('\n')
	}
	return fs.WriteFileSafe(WorkspaceAtomsPath(root, ws), buf.Bytes())
}

// LoadWorkspaceStage reads ws/<name>/STAGE.jsonl if present.
func LoadWorkspaceStage(fs FS, root, ws string) ([]Op, error) {
	p := WorkspaceStagePath(root, ws)
	if !fs.Exists(p) {
		return nil, nil
	}
	b, err := fs.ReadFile(p, MaxFileReadBytes)
	if err != nil {
		return nil, err
	}
	lines, err := ReadJSONLLines(b)
	if err != nil {
		return nil, err
	}
	if len(lines) > MaxAtoms {
		return nil, Refuse("store_too_large", "atoms")
	}
	ops := make([]Op, 0, len(lines))
	for _, line := range lines {
		var op Op
		if err := json.Unmarshal(line, &op); err != nil || op.Type == "" || op.AtomID == "" {
			return nil, Refuse("stage_malformed", "")
		}
		ops = append(ops, op)
	}
	return ops, nil
}

// OpTypeCode maps an Op type to its canonical byte code (0 = unknown).
func OpTypeCode(t string) uint8 {
	switch t {
	case "add":
		return 1
	case "modify":
		return 2
	case "delete":
		return 3
	case "move":
		return 4
	case "rename":
		return 5
	default:
		return 0
	}
}

// SortOps orders ops by (atom_id bytes, type code) for canonical encoding.
func SortOps(ops []Op) {
	sort.Slice(ops, func(i, j int) bool {
		ui, oki := ParseUUID(ops[i].AtomID)
		uj, okj := ParseUUID(ops[j].AtomID)
		if oki && okj {
			if cmp := bytes.Compare(ui[:], uj[:]); cmp != 0 {
				return cmp < 0
			}
		} else if ops[i].AtomID != ops[j].AtomID {
			return ops[i].AtomID < ops[j].AtomID
		}
		return OpTypeCode(ops[i].Type) < OpTypeCode(ops[j].Type)
	})
}

// WriteWorkspaceStage writes ws/<name>/STAGE.jsonl deterministically via
// WriteFileSafe.
func WriteWorkspaceStage(fs FS, root, ws string, ops []Op) error {
	if len(ops) > MaxAtoms {
		return Refuse("store_too_large", "atoms")
	}
	sorted := make([]Op, len(ops))
	copy(sorted, ops)
	SortOps(sorted)
	var buf bytes.Buffer
	for _, op := range sorted {
		line, err := json.Marshal(op)
		if err != nil {
			return err
		}
		buf.Write(line)
		buf.WriteByte('\n')
	}
	return fs.WriteFileSafe(WorkspaceStagePath(root, ws), buf.Bytes())
}

// WriteJSONSafe marshals v to JSON with a trailing newline and writes it via
// WriteFileSafe.
func WriteJSONSafe(fs FS, path string, v any) error {
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	b = append(b, '\n')
	return fs.WriteFileSafe(path, b)
}
