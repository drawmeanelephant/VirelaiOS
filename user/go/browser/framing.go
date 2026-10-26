package main

import (
	"virelai/httpc"
	"virelai/webrender"
	"virelai/webstyle"
)

const maxHTTPHeaders = 16384
const maxHTTPLine = 4096
const maxHTTPFields = 128
const maxPageRequests = 25

// responseFrame is the parsed head of one response: where the wire body
// starts, the declared framing (Content-Length / chunked / close-
// delimited / no-body), the content coding, and the connection policy —
// all delegated to the shared httpc grammar (M135b).
type responseFrame struct {
	head, err string
	offset    int
	parsed    httpc.Head
	need      int // Content-Length; -1 means chunked or EOF framing
	complete  bool
}

// headScan is the incremental front of parseResponseFrame: the fetch
// drivers append each read to one buffer, so the terminator scan resumes
// where the last feed stopped and the line bound sees each byte once.
// Feeding an n-byte head in drips is O(n) total where reparse-per-read is
// O(n^2) in head length with the peer picking the read count (#3684).
// The buffer is append-only between feeds.
type headScan struct {
	scan   int // terminator scan resumes here (3 back for a split match)
	lineAt int // line-bound check resumes here
	open   int // open line's length, trailing '\r's excluded
	crs    int // '\r's at the open line's tail, counted on the next byte
	work   int // byte positions examined across feeds (test observability)
}

// headEnd is httpc.HeadEnd resumed: a terminator match can begin at most
// three bytes before the previous buffer end (\r\n\r\n split across
// reads), so the scan restarts there instead of at zero.
func (s *headScan) headEnd(raw []byte) int {
	for i := s.scan; i+1 < len(raw); i++ {
		s.work++
		if raw[i] == '\n' && raw[i+1] == '\n' {
			return i + 2
		}
		if i+3 < len(raw) && raw[i] == '\r' && raw[i+1] == '\n' && raw[i+2] == '\r' && raw[i+3] == '\n' {
			return i + 4
		}
	}
	if back := len(raw) - 3; back > s.scan {
		s.scan = back
	}
	return -1
}

// lineOver advances the per-line bound over the bytes appended since the
// last feed. open is the running length of the unterminated line without
// its trailing '\r's; crs holds '\r's pending at its tail (they count
// only once a non-'\r' lands behind them). The verdict is the one-shot
// grammar's len(TrimRight(line, "\r")) > maxHTTPLine applied per byte.
func (s *headScan) lineOver(raw []byte) bool {
	for ; s.lineAt < len(raw); s.lineAt++ {
		s.work++
		switch raw[s.lineAt] {
		case '\n':
			if s.open > maxHTTPLine {
				return true
			}
			s.open, s.crs = 0, 0
		case '\r':
			s.crs++
		default:
			s.open += s.crs + 1
			s.crs = 0
		}
	}
	return s.open > maxHTTPLine
}

func (s *headScan) feed(raw []byte) responseFrame {
	i := s.headEnd(raw)
	if i < 0 {
		if len(raw) > maxHTTPHeaders || s.lineOver(raw) {
			return responseFrame{err: "http-header-limit", complete: true}
		}
		return responseFrame{}
	}
	f := responseFrame{head: string(raw[:i]), offset: i, need: -1, complete: true}
	if i > maxHTTPHeaders {
		f.err = "http-header-limit"
		return f
	}
	h, perr := httpc.ParseHead(raw[:i], maxHTTPLine, maxHTTPFields)
	if perr != "" {
		f.err = perr
		return f
	}
	f.parsed = h
	f.need = h.Length
	return f
}

// parseResponseFrame is the one-shot form kept for complete buffers
// (responseNeed and the host tests); the fetch drivers keep a headScan
// across reads instead — same grammar, incremental cost.
func parseResponseFrame(raw []byte) responseFrame {
	var s headScan
	return s.feed(raw)
}

func formatPageRequest(u webrender.URL, cookie string) string {
	if len(cookie)+8 > maxHTTPLine {
		cookie = "" // an edited ledger cannot grow past the line bound
	}
	return httpc.GetRequest(urlAuthority(u), u.Path, cookie)
}

// responseBody turns the buffered wire bytes into the decoded body once
// the message is complete: Content-Length bounded, chunked (trailers
// skipped), or close-delimited, then content-decoded (gzip through
// flate). The named refusals are httpc's shared grammar.
func responseBody(f responseFrame, raw []byte, capn int, eof bool) ([]byte, bool, string) {
	if f.err != "" {
		return nil, false, f.err
	}
	if !f.complete {
		if eof {
			return nil, false, "http-premature-eof"
		}
		return nil, false, ""
	}
	wire := raw[f.offset:]
	var src []byte
	switch {
	case f.parsed.NoBody:
		return []byte{}, false, ""
	case f.parsed.Chunked:
		var dec httpc.Chunked
		dec.Feed(wire, &src, capn)
		if dec.Err != "" {
			return nil, false, dec.Err
		}
		if !dec.Done {
			if eof {
				return nil, false, httpc.ChunkedMalformed
			}
			return nil, false, ""
		}
	case f.need >= 0:
		if !f.parsed.Gzip && f.need > capn && len(wire) >= capn {
			return wire[:capn], true, ""
		}
		if len(wire) < f.need {
			if eof {
				return nil, false, "http-premature-eof"
			}
			return nil, false, ""
		}
		src = wire[:f.need]
	default: // close-delimited
		if !eof {
			if len(wire) > capn {
				return wire[:capn], true, ""
			}
			return nil, false, ""
		}
		src = wire
	}
	return httpc.Decode(f.parsed, src, capn)
}

// responseNeed retains the pre-existing host-test framing contract. Production
// fetchers use the structured parser to preserve full named errors.
func responseNeed(raw []byte) (int, bool) {
	f := parseResponseFrame(raw)
	if !f.complete {
		return 0, false
	}
	if f.err != "" || f.need > webstyle.MaxHTMLBytes {
		return -2, true
	}
	return f.need, true
}
