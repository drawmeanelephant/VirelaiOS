package flintstore

import "bytes"

// This file is the write half of the shared FLINT core: `create` and
// `append` from docs/flint-server-spec.md §2.3/§2.4. FLINTD.ELF serves both
// verbs; keeping the ordering here means the daemon and its tests share one
// implementation and no caller can append a half-validated batch.

// ValidateStreamShape reports the ADR 0049/`flintd` record shape rules: a
// canonical UUID id, a `[a-z0-9-]{1,32}` name, a non-negative created stamp,
// UUID head and fork origin fields. It does not consult the store.
func ValidateStreamShape(st *Stream) error {
	if st == nil {
		return Refuse("stream_malformed", "invalid stream record")
	}
	if _, ok := ParseUUID(st.ID); !ok {
		return Refuse("stream_malformed", "invalid stream record")
	}
	if !ValidStreamName(st.Name) || st.Created < 0 {
		return Refuse("stream_malformed", "invalid stream record")
	}
	if st.Head != nil {
		if _, ok := ParseUUID(*st.Head); !ok {
			return Refuse("stream_malformed", "invalid stream record")
		}
	}
	if st.ForkedFrom != nil {
		if _, ok := ParseUUID(st.ForkedFrom.Stream); !ok {
			return Refuse("stream_malformed", "invalid stream record")
		}
		if _, ok := ParseUUID(st.ForkedFrom.Change); !ok {
			return Refuse("stream_malformed", "invalid stream record")
		}
	}
	return nil
}

// CreateStream implements `create` (§2.4): validate the record, refuse a
// colliding id or name, resolve a fork origin to a real Change, and write
// streams/<id>.json only. The `.jsonl` is created lazily by the first append.
func CreateStream(fs FS, root string, st *Stream) error {
	if err := ValidateStreamShape(st); err != nil {
		return err
	}
	idx, err := LoadStoreIndexStrict(fs, root)
	if err != nil {
		return err
	}
	for _, existing := range idx.Streams {
		if existing.ID == st.ID || existing.Name == st.Name {
			return Refuse("stream_exists", "stream id or name already exists")
		}
	}
	if st.ForkedFrom != nil {
		if !PtrEqual(st.Head, &st.ForkedFrom.Change) {
			return Refuse("stream_malformed", "fork head must equal forked_from.change")
		}
		reachable, rerr := ChangeReachableInForkChain(
			fs, root, st.ForkedFrom.Stream, st.ForkedFrom.Change,
			nil, len(idx.Streams)+1)
		if rerr != nil {
			return rerr
		}
		if !reachable {
			return Refuse("change_missing", "fork origin change not found")
		}
	} else if st.Head != nil {
		return Refuse("stream_malformed", "fresh stream head must be null")
	}
	return WriteStreamRecord(fs, root, st)
}

// AppendStreamBatch implements `append` (§2.3) in the spec's order:
//
//  1. Load the store index and re-read the target record with the ADR
//     roll-forward, then compare the recovered head to expectedHead. This
//     stale-head check runs BEFORE any batch validation, so a client that
//     raced another writer learns that first.
//  2. Validate the entire nonempty batch (parse, stream id, message bound,
//     parent chain, duplicate ids, Ed25519 signature, optional author gate)
//     without touching the log. An empty batch is `change_malformed`.
//  3. Enforce the global 4096-Change bound and the 4 MiB store-file bound.
//  4. Append every raw line in ONE AppendSync, then publish the new head with
//     one crash-safe record rewrite.
//
// Nothing is appended unless the whole batch validates. allowAuthor may be
// nil (no allowlist); a refusal is `author_not_allowed`.
func AppendStreamBatch(
	fs FS, root, streamID string,
	expectedHead *string,
	rawLines [][]byte,
	allowAuthor func(author string) bool,
) (*Stream, error) {
	idx, err := LoadStoreIndexStrict(fs, root)
	if err != nil {
		return nil, err
	}
	st := idx.FindByID(streamID)
	if st == nil {
		return nil, Refuse("stream_missing", streamID)
	}
	if !PtrEqual(st.Head, expectedHead) {
		return nil, Refuse("stale_head", "expected_head mismatch")
	}
	if len(rawLines) == 0 {
		return nil, Refuse("change_malformed", "empty batch")
	}

	existing, err := ReadStreamChanges(fs, root, streamID)
	if err != nil {
		return nil, err
	}
	existingIDs := make(map[string]bool, len(existing))
	for i := range existing {
		existingIDs[existing[i].ID] = true
	}

	var appendBuf bytes.Buffer
	batchIDs := make(map[string]bool, len(rawLines))
	prevHead := st.Head
	for _, raw := range rawLines {
		line := bytes.TrimRight(raw, "\r\n")
		if len(line) == 0 ||
			bytes.IndexByte(line, '\n') >= 0 ||
			bytes.IndexByte(line, '\r') >= 0 {
			return nil, Refuse("change_malformed", "change entry is not one JSONL line")
		}
		c, perr := ParseChange(line)
		if perr != nil {
			return nil, perr
		}
		if c.Stream != streamID {
			return nil, Refuse("change_malformed", "change stream id mismatch")
		}
		if !ValidMessage(c.Message) {
			return nil, Refuse("message_invalid", "invalid commit message")
		}
		if !PtrEqual(c.Parent, prevHead) {
			return nil, Refuse("change_malformed", "parent chain mismatch")
		}
		if existingIDs[c.ID] || batchIDs[c.ID] {
			return nil, Refuse("change_malformed", "duplicate change id")
		}
		if ok, verr := VerifyChange(&c); verr != nil {
			return nil, verr
		} else if !ok {
			return nil, Refuse("sig_invalid", c.ID)
		}
		if allowAuthor != nil && !allowAuthor(c.Author) {
			return nil, Refuse("author_not_allowed", "author not in allowlist")
		}
		batchIDs[c.ID] = true
		appendBuf.Write(line)
		appendBuf.WriteByte('\n')
		head := c.ID
		prevHead = &head
	}

	if len(existing)+len(rawLines) > MaxChanges || idx.ChangeCount+len(rawLines) > MaxChanges {
		return nil, Refuse("store_too_large", "changes")
	}

	logPath := StreamLogPath(root, streamID)
	var existingLen int
	if fs.Exists(logPath) {
		b, rerr := fs.ReadFile(logPath, MaxFileReadBytes)
		if rerr != nil {
			return nil, rerr
		}
		existingLen = len(b)
	}
	if existingLen+appendBuf.Len() > MaxFileReadBytes {
		return nil, Refuse("store_too_large", "file")
	}

	if aerr := fs.AppendSync(logPath, appendBuf.Bytes()); aerr != nil {
		return nil, aerr
	}
	newHead := prevHead
	st.Head = newHead
	if werr := WriteStreamRecord(fs, root, st); werr != nil {
		return nil, werr
	}
	return st, nil
}
