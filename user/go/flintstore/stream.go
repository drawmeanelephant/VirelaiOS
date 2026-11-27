package flintstore

import (
	"sort"
	"strings"
)

// PtrEqual compares two optional strings by value.
func PtrEqual(a, b *string) bool {
	if a == nil && b == nil {
		return true
	}
	if a == nil || b == nil {
		return false
	}
	return *a == *b
}

// CopyStrPtr clones an optional string.
func CopyStrPtr(p *string) *string {
	if p == nil {
		return nil
	}
	v := *p
	return &v
}

// LoadedStream is a stream record with its full history: parsed Changes and
// the raw JSONL line bytes verbatim (no trailing newline).
type LoadedStream struct {
	Stream   *Stream
	Changes  []Change
	RawLines [][]byte
}

// ReadStreamChanges reads and parses streams/<id>.jsonl via the torn-aware
// ReadJSONLLines.
func ReadStreamChanges(fs FS, root, streamID string) ([]Change, error) {
	p := StreamLogPath(root, streamID)
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
	if len(lines) > MaxChanges {
		return nil, Refuse("store_too_large", "changes")
	}
	changes := make([]Change, 0, len(lines))
	for _, line := range lines {
		c, err := ParseChange(line)
		if err != nil {
			return nil, err
		}
		changes = append(changes, c)
	}
	return changes, nil
}

// WriteStreamRecord writes streams/<id>.json crash-safely via WriteJSONSafe.
func WriteStreamRecord(fs FS, root string, st *Stream) error {
	return WriteJSONSafe(fs, StreamRecordPath(root, st.ID), st)
}

// loadStreamInternal implements the shared store load and recovery. verify
// additionally checks each Change's Ed25519 signature (the daemon's read
// gate); the CLI loads without it so `verify` can report a bad signature as a
// per-Change result instead of a load refusal.
//
// Recovery rolls the record head forward over the FULL valid contiguous
// suffix: when an append crashed between the .jsonl write and the .json
// rewrite, the record head is any earlier Change (or the fork origin) and the
// log's last Change is the recovered head.
func loadStreamInternal(fs FS, root, streamID string, verify, strict bool) (*LoadedStream, error) {
	if strict {
		if _, ok := ParseUUID(streamID); !ok {
			return nil, Refuse("stream_missing", streamID)
		}
	}
	recPath := StreamRecordPath(root, streamID)
	if !fs.Exists(recPath) {
		return nil, Refuse("stream_missing", streamID)
	}
	b, err := fs.ReadFile(recPath, MaxFileReadBytes)
	if err != nil {
		return nil, err
	}
	st, err := parseStreamRecord(b, streamID)
	if err != nil {
		return nil, err
	}

	var rawLines [][]byte
	logPath := StreamLogPath(root, streamID)
	if fs.Exists(logPath) {
		logBytes, err := fs.ReadFile(logPath, MaxFileReadBytes)
		if err != nil {
			return nil, err
		}
		rawLines, err = ReadJSONLLines(logBytes)
		if err != nil {
			return nil, err
		}
	}
	if len(rawLines) > MaxChanges {
		return nil, Refuse("store_too_large", "changes")
	}

	expectedFirstParent := (*string)(nil)
	if st.ForkedFrom != nil {
		h := st.ForkedFrom.Change
		expectedFirstParent = &h
	}

	changes := make([]Change, 0, len(rawLines))
	for i, line := range rawLines {
		c, err := ParseChange(line)
		if err != nil {
			return nil, err
		}
		if strict {
			if c.Stream != streamID {
				return nil, Refuse("change_malformed", "")
			}
			if i == 0 {
				if !PtrEqual(c.Parent, expectedFirstParent) {
					return nil, Refuse("change_malformed", "")
				}
			} else if !PtrEqual(c.Parent, &changes[i-1].ID) {
				return nil, Refuse("change_malformed", "")
			}
			if verify {
				ok, err := VerifyChange(&c)
				if err != nil {
					return nil, err
				}
				if !ok {
					return nil, Refuse("sig_invalid", c.ID)
				}
			}
		}
		changes = append(changes, c)
	}

	if len(changes) == 0 {
		if strict && !PtrEqual(st.Head, expectedFirstParent) {
			return nil, Refuse("stream_malformed", streamID)
		}
	} else {
		last := changes[len(changes)-1]
		if !PtrEqual(st.Head, &last.ID) {
			canRollForward := PtrEqual(st.Head, expectedFirstParent)
			if !canRollForward {
				for i := 0; i < len(changes)-1; i++ {
					if PtrEqual(st.Head, &changes[i].ID) {
						canRollForward = true
						break
					}
				}
			}
			if strict {
				if !canRollForward {
					return nil, Refuse("stream_malformed", streamID)
				}
			}
			if canRollForward {
				head := last.ID
				st.Head = &head
				if err := WriteStreamRecord(fs, root, st); err != nil {
					return nil, err
				}
			}
		}
	}

	return &LoadedStream{Stream: st, Changes: changes, RawLines: rawLines}, nil
}

// LoadStream reads the Stream record and its Change log, rolling a stale
// record head forward over the contiguous validated suffix. Signatures are
// not re-checked here (the CLI verify verb owns that report); a record whose
// head names no Change is returned as-is so the CLI's stale-head refusal keeps
// its historical shape.
func LoadStream(fs FS, root, streamID string) (*Stream, []Change, error) {
	loaded, err := loadStreamInternal(fs, root, streamID, false, false)
	if err != nil {
		return nil, nil, err
	}
	return loaded.Stream, loaded.Changes, nil
}

// LoadStreamFull reads the Stream record, log, raw line bytes and parsed
// Changes, applying the host flintd load rules: canonical id, complete
// parent chain, every signature verified, and a record head that names a
// reachable Change (or the fork origin) before it rolls forward. It is the
// guest daemon's read gate: a corrupt or unsigned store refuses by name
// rather than serving bytes whose integrity the transport cannot prove.
func LoadStreamFull(fs FS, root, streamID string) (*LoadedStream, error) {
	return loadStreamInternal(fs, root, streamID, true, true)
}

// CheckStaleHead re-reads the Stream record (rolling forward if the log
// moved) and refuses with `stale_head` if the head no longer equals
// expectedHead.
func CheckStaleHead(fs FS, root, streamID string, expectedHead *string) (*Stream, error) {
	st, _, err := LoadStream(fs, root, streamID)
	if err != nil {
		return nil, err
	}
	if !PtrEqual(st.Head, expectedHead) {
		return nil, Refuse("stale_head", "")
	}
	return st, nil
}

// AppendStreamChange re-reads the Stream record to check `stale_head`,
// appends the Change JSON line with FileSync, and rewrites the Stream record
// with the new head.
func AppendStreamChange(fs FS, root, streamID string, expectedHead *string, c Change) (*Stream, error) {
	st, err := CheckStaleHead(fs, root, streamID, expectedHead)
	if err != nil {
		return nil, err
	}
	line, err := MarshalChangeLine(c)
	if err != nil {
		return nil, err
	}
	if err := fs.AppendSync(StreamLogPath(root, streamID), line); err != nil {
		return nil, err
	}
	head := c.ID
	st.Head = &head
	if err := WriteStreamRecord(fs, root, st); err != nil {
		return nil, err
	}
	return st, nil
}

// LoadAllStreamChanges scans all streams in deterministic ID order, rolling
// any stale records forward and returning all Changes across the store.
func LoadAllStreamChanges(fs FS, root string, idx *StoreIndex) ([]Change, error) {
	ids := make([]string, 0, len(idx.StreamsByID))
	for id := range idx.StreamsByID {
		ids = append(ids, id)
	}
	sort.Strings(ids)

	seenLog := make(map[string]bool, len(ids))
	var all []Change
	for _, id := range ids {
		st, changes, err := LoadStream(fs, root, id)
		if err != nil {
			return nil, err
		}
		idx.StreamsByID[st.ID] = st
		idx.StreamsByName[st.Name] = st
		seenLog[id+".jsonl"] = true
		all = append(all, changes...)
	}

	// Also inspect any orphan .jsonl files in streams/ so malformed logs are caught.
	sDir := StreamsDir(root)
	if fs.Exists(sDir) {
		entries, err := fs.List(sDir)
		if err != nil {
			return nil, err
		}
		for _, e := range entries {
			if e.IsDir || !strings.HasSuffix(e.Name, ".jsonl") || seenLog[e.Name] {
				continue
			}
			sid := strings.TrimSuffix(e.Name, ".jsonl")
			changes, err := ReadStreamChanges(fs, root, sid)
			if err != nil {
				return nil, err
			}
			all = append(all, changes...)
		}
	}
	if len(all) > MaxChanges {
		return nil, Refuse("store_too_large", "changes")
	}
	return all, nil
}

// ChangeReachableInForkChain reports whether targetChangeID is reachable in
// originStreamID's history, walking `forked_from` ancestry (docs/flint-server
// spec §2.4). initialCutoff bounds an origin stream to the history at or
// before the fork change; maxDepth bounds the walk.
func ChangeReachableInForkChain(fs FS, root, originStreamID, targetChangeID string, initialCutoff *string, maxDepth int) (bool, error) {
	currStream := originStreamID
	cutoff := CopyStrPtr(initialCutoff)
	for depth := 0; depth <= maxDepth; depth++ {
		loaded, err := loadStreamInternal(fs, root, currStream, true, true)
		if err != nil {
			if depth == 0 && RefusalName(err) == "stream_missing" {
				return false, err
			}
			if depth > 0 && RefusalName(err) == "stream_missing" {
				return false, nil
			}
			return false, err
		}
		if cutoff != nil {
			sawCutoff := false
			for i := range loaded.Changes {
				if loaded.Changes[i].ID == targetChangeID {
					return true, nil
				}
				if loaded.Changes[i].ID == *cutoff {
					sawCutoff = true
					break
				}
			}
			if !sawCutoff && loaded.Stream.ForkedFrom != nil &&
				loaded.Stream.ForkedFrom.Change == *cutoff && *cutoff == targetChangeID {
				return true, nil
			}
		} else {
			if PtrEqual(loaded.Stream.Head, &targetChangeID) {
				return true, nil
			}
			for i := range loaded.Changes {
				if loaded.Changes[i].ID == targetChangeID {
					return true, nil
				}
			}
		}
		if loaded.Stream.ForkedFrom == nil {
			return false, nil
		}
		if loaded.Stream.ForkedFrom.Change == targetChangeID {
			return true, nil
		}
		currStream = loaded.Stream.ForkedFrom.Stream
		nextCutoff := loaded.Stream.ForkedFrom.Change
		cutoff = &nextCutoff
	}
	return false, nil
}
