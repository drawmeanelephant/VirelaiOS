package main

import (
	"bytes"
	"encoding/json"

	"virelai/flintstore"
)

// storeFS is the daemon's store backend: the guest /host channel in the
// guest, MemFS in host tests.
var storeFS flintstore.FS = flintstore.GuestFS{}

// connStream is the byte stream a session answers on: a *vi.Conn in the
// guest, a scripted buffer in tests.
type connStream interface {
	Recv(p []byte) (int, error)
	Send(p []byte) (int, error)
}

// serveSession runs one connection: accumulate bytes into NDJSON lines, answer
// each complete line in order, and stop on a framing error, a peer close or a
// send failure. One request is in flight at a time; coalesced bytes stay
// buffered for the next turn, so pipelining gains no concurrency.
func serveSession(conn connStream, root string, log func(string)) {
	var rx []byte
	chunk := make([]byte, 1460)
	for {
		if i := bytes.IndexByte(rx, '\n'); i >= 0 {
			line := rx[:i]
			rx = rx[i+1:]
			resp, closeAfter := handleLine(line, root, log)
			if len(resp) > 0 {
				if _, err := conn.Send(resp); err != nil {
					return
				}
			}
			if closeAfter {
				return
			}
			continue
		}
		if len(rx) > maxLineBytes {
			// No newline and more than a maximum line buffered: the peer is
			// sending an over-bound request. Answer once with seq 0 (the
			// request's seq is unknowable) and close, like the host oracle.
			resp := refusalLine(0, "store_too_large", "line exceeds 4 MiB")
			_, _ = conn.Send(resp)
			return
		}
		n, err := conn.Recv(chunk)
		if err != nil {
			return
		}
		if n <= 0 {
			return
		}
		rx = append(rx, chunk[:n]...)
	}
}

// handleLine processes one request line (without '\n'). It returns the exact
// response bytes, empty when the line must simply be dropped, and whether the
// session closes after the reply.
func handleLine(raw []byte, root string, log func(string)) ([]byte, bool) {
	line := raw
	if len(line) > 0 && line[len(line)-1] == '\r' {
		line = line[:len(line)-1]
	}
	if len(line) > maxLineBytes {
		return refusalLine(0, "store_too_large", "line exceeds 4 MiB"), true
	}
	if len(line) == 0 {
		return refusalLine(0, "usage", "empty request line"), false
	}

	var obj map[string]json.RawMessage
	if err := json.Unmarshal(line, &obj); err != nil || obj == nil {
		return refusalLine(0, "usage", "malformed json request"), false
	}

	seq, ok := parseSeq(obj["seq"])
	if !ok {
		return refusalLine(0, "usage", "invalid seq"), false
	}

	op, ok := jsonString(obj["op"])
	if !ok || op == "" {
		return refusalLine(seq, "usage", "missing op"), false
	}

	if log != nil {
		log("flintd: request seq=" + itoa64(seq) + " op=" + op)
	}

	var resp []byte
	switch op {
	case "hello":
		resp = handleHello(seq, obj)
	case "auth":
		resp = refusalLine(seq, "usage", "auth not enabled on this server")
	case "list":
		resp = handleList(seq, root)
	case "heads":
		resp = handleHeads(seq, root)
	case "open":
		resp = handleOpen(seq, obj, root)
	case "read":
		resp = handleRead(seq, obj, root)
	case "create", "append":
		// Read-only milestone: the verb is known but not served. Say so by
		// name and write nothing.
		resp = refusalLine(seq, "usage", op+" unsupported: read-only daemon")
	default:
		resp = refusalLine(seq, "usage", "unknown op")
	}
	if log != nil {
		logReply(log, seq, resp)
	}
	if len(resp) > maxLineBytes {
		resp = refusalLine(seq, "store_too_large", "response line exceeds 4 MiB")
	}
	return resp, false
}

func handleHello(seq uint64, obj map[string]json.RawMessage) []byte {
	proto, ok := jsonString(obj["proto"])
	if !ok {
		return refusalLine(seq, "proto_version", "missing proto")
	}
	if proto != protoVersion {
		return refusalLine(seq, "proto_version", "expected FLINT-SYNC/1")
	}
	return jsonLine(helloResponse{Seq: seq, OK: true, Proto: protoVersion, Server: serverName})
}

func handleList(seq uint64, root string) []byte {
	idx, err := flintstore.LoadStoreIndexStrict(storeFS, root)
	if err != nil {
		return refusalForStoreError(seq, err)
	}
	return jsonLine(listResponse{Seq: seq, OK: true, Streams: idx.Streams})
}

func handleHeads(seq uint64, root string) []byte {
	idx, err := flintstore.LoadStoreIndexStrict(storeFS, root)
	if err != nil {
		return refusalForStoreError(seq, err)
	}
	byID := idx.SortedByID()
	heads := make(map[string]*string, len(byID))
	for _, st := range byID {
		heads[st.ID] = st.Head
	}
	return jsonLine(headsResponse{Seq: seq, OK: true, Heads: heads})
}

func handleOpen(seq uint64, obj map[string]json.RawMessage, root string) []byte {
	stream, ok := jsonString(obj["stream"])
	if !ok {
		stream, ok = jsonString(obj["name"])
	}
	if !ok {
		return refusalLine(seq, "usage", "missing stream")
	}
	if stream == "" {
		return refusalLine(seq, "usage", "empty stream")
	}
	idx, err := flintstore.LoadStoreIndexStrict(storeFS, root)
	if err != nil {
		return refusalForStoreError(seq, err)
	}
	st := idx.FindByNameOrID(stream)
	if st == nil {
		return refusalLine(seq, "stream_missing", "stream not found")
	}
	return jsonLine(openResponse{Seq: seq, OK: true, Record: st})
}

func handleRead(seq uint64, obj map[string]json.RawMessage, root string) []byte {
	stream, ok := jsonString(obj["stream"])
	if !ok {
		return refusalLine(seq, "usage", "missing stream")
	}
	if stream == "" {
		return refusalLine(seq, "usage", "empty stream")
	}

	var after *string
	if raw, present := obj["after"]; present {
		var s string
		if err := json.Unmarshal(raw, &s); err == nil {
			if s != "" {
				after = &s
			}
		} else if string(bytes.TrimSpace(raw)) != "null" {
			return refusalLine(seq, "usage", "invalid after")
		}
	}

	limit := flintstore.DefaultReadLimit
	if raw, present := obj["limit"]; present {
		if string(bytes.TrimSpace(raw)) != "null" {
			var n int64
			if err := json.Unmarshal(raw, &n); err != nil {
				return refusalLine(seq, "usage", "invalid limit")
			}
			if n <= 0 {
				return refusalLine(seq, "usage", "invalid limit")
			}
			if n > int64(flintstore.MaxReadLimit) {
				n = int64(flintstore.MaxReadLimit)
			}
			limit = int(n)
		}
	}

	idx, err := flintstore.LoadStoreIndexStrict(storeFS, root)
	if err != nil {
		return refusalForStoreError(seq, err)
	}
	found := idx.FindByID(stream)
	if found == nil {
		return refusalLine(seq, "stream_missing", "stream not found")
	}
	loaded, err := flintstore.LoadStreamFull(storeFS, root, found.ID)
	if err != nil {
		return refusalForStoreError(seq, err)
	}

	start := 0
	if after != nil {
		matched := false
		for i := range loaded.Changes {
			if loaded.Changes[i].ID == *after {
				start = i + 1
				matched = true
				break
			}
		}
		if !matched && loaded.Stream.ForkedFrom != nil {
			reachable, rerr := flintstore.ChangeReachableInForkChain(
				storeFS, root, loaded.Stream.ForkedFrom.Stream,
				*after, &loaded.Stream.ForkedFrom.Change, len(idx.Streams)+1)
			if rerr != nil {
				return refusalForStoreError(seq, rerr)
			}
			if reachable {
				start = 0
				matched = true
			}
		}
		if !matched {
			return refusalLine(seq, "change_missing", "after change not found in stream")
		}
	}

	remaining := loaded.RawLines[start:]
	count := len(remaining)
	if count > limit {
		count = limit
	}
	lines := make([]string, 0, count)
	for _, raw := range remaining[:count] {
		lines = append(lines, string(raw))
	}
	return jsonLine(readResponse{
		Seq:     seq,
		OK:      true,
		Record:  loaded.Stream,
		Head:    loaded.Stream.Head,
		Changes: lines,
		More:    len(remaining) > limit,
	})
}

// logReply prints a one-line serial receipt naming the reply shape (never the
// payload), so a gate transcript shows exactly which verb answered or refused.
func logReply(log func(string), seq uint64, resp []byte) {
	var obj struct {
		OK     bool   `json:"ok"`
		Refuse string `json:"refuse"`
	}
	_ = json.Unmarshal(resp, &obj)
	if obj.OK {
		log("flintd: reply seq=" + itoa64(seq) + " ok")
		return
	}
	log("flintd: refuse seq=" + itoa64(seq) + " name=" + obj.Refuse)
}

func refusalForStoreError(seq uint64, err error) []byte {
	name := flintstore.RefusalName(err)
	detail := "invalid request"
	switch name {
	case "stream_missing":
		detail = "stream not found"
	case "stream_exists":
		detail = "stream already exists"
	case "stale_head":
		detail = "expected_head mismatch"
	case "change_malformed":
		detail = "malformed change"
	case "change_missing":
		detail = "change not found"
	case "sig_invalid":
		detail = "signature verification failed"
	case "store_too_large":
		detail = "store bound exceeded"
	case "stream_malformed":
		detail = "malformed stream record"
	case "stream_torn":
		detail = "jsonl missing trailing newline"
	case "message_invalid":
		detail = "invalid commit message"
	case "author_not_allowed":
		detail = "author not in allowlist"
	case "stream_too_large":
		detail = "store bound exceeded"
	case "usage":
		detail = "invalid request"
	case "":
		name = "stream_malformed"
		detail = "io error"
	}
	return refusalLine(seq, name, detail)
}

// ---------------------------------------------------------------------------
// wire encoders — field order and Go JSON escaping match the host flintd
// (docs/flint-server-spec.md §2.1) byte-for-byte for the read verbs.
// ---------------------------------------------------------------------------

type helloResponse struct {
	Seq    uint64 `json:"seq"`
	OK     bool   `json:"ok"`
	Proto  string `json:"proto"`
	Server string `json:"server"`
}

type listResponse struct {
	Seq     uint64               `json:"seq"`
	OK      bool                 `json:"ok"`
	Streams []*flintstore.Stream `json:"streams"`
}

type headsResponse struct {
	Seq   uint64             `json:"seq"`
	OK    bool               `json:"ok"`
	Heads map[string]*string `json:"heads"`
}

type openResponse struct {
	Seq    uint64             `json:"seq"`
	OK     bool               `json:"ok"`
	Record *flintstore.Stream `json:"record"`
}

type readResponse struct {
	Seq     uint64             `json:"seq"`
	OK      bool               `json:"ok"`
	Record  *flintstore.Stream `json:"record"`
	Head    *string            `json:"head"`
	Changes []string           `json:"changes"`
	More    bool               `json:"more"`
}

type refusalResponse struct {
	Seq    uint64 `json:"seq"`
	OK     bool   `json:"ok"`
	Refuse string `json:"refuse"`
	Detail string `json:"detail"`
}

func jsonLine(v any) []byte {
	b, err := json.Marshal(v)
	if err != nil {
		return refusalLine(0, "store_too_large", "encode failure")
	}
	return append(b, '\n')
}

func refusalLine(seq uint64, code, detail string) []byte {
	return jsonLine(refusalResponse{Seq: seq, OK: false, Refuse: code, Detail: detail})
}

func jsonString(raw json.RawMessage) (string, bool) {
	if len(raw) == 0 {
		return "", false
	}
	var s string
	if err := json.Unmarshal(raw, &s); err != nil {
		return "", false
	}
	return s, true
}

// parseSeq accepts a JSON integer in [0, u32max]; anything else is usage.
func parseSeq(raw json.RawMessage) (uint64, bool) {
	if len(raw) == 0 {
		return 0, false
	}
	var n uint64
	if err := json.Unmarshal(raw, &n); err != nil {
		return 0, false
	}
	if n > 0xffffffff {
		return 0, false
	}
	return n, true
}

func itoa64(v uint64) string {
	if v == 0 {
		return "0"
	}
	var buf [20]byte
	i := len(buf)
	for v > 0 {
		i--
		buf[i] = byte('0' + v%10)
		v /= 10
	}
	return string(buf[i:])
}
