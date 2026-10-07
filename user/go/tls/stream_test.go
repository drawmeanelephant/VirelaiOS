package tls

import (
	"testing"

	"virelai/vi"
)

type fakeSeam struct {
	stream    []byte
	pos       int
	chunk     int
	pollEvery int
	polls     int
	sent      []byte
	maxSend   int
	closed    bool
}

func TestExplicitDeadlinePollingDoesNotParkForASchedulerTick(t *testing.T) {
	prev := vi.SyscallHookForTest()
	defer vi.SetSyscallHookForTest(prev)
	sleeps, clocks := 0, 0
	vi.SetSyscallHookForTest(func(slot, x0, x1, x2, x3 uintptr) int64 {
		if slot == vi.SlotSleep {
			sleeps++
		}
		return 0
	})
	calls := 0
	s := &tcpStream{buf: make([]byte, streamStashCap), deadlineAt: 2_000_000_000,
		now: func() int64 { clocks++; return 0 }}
	s.recv = func(out []byte) (int, error) {
		calls++
		if calls == 1 {
			return 0, nil
		}
		out[0] = 'x'
		return 1, nil
	}
	out := make([]byte, 1)
	if n, err := s.read(out); n != 1 || err != nil || out[0] != 'x' || sleeps != 0 || clocks < 1024 {
		t.Fatalf("deadline polling used tick sleep or unbounded clock wait: n=%d err=%v sleeps=%d clocks=%d", n, err, sleeps, clocks)
	}
}

func (f *fakeSeam) recv(out []byte) (int, error) {
	if f.closed {
		return 0, errStreamClosed
	}
	f.polls++
	if f.pollEvery != 0 && f.polls%f.pollEvery == 0 {
		return 0, nil
	}
	if f.pos >= len(f.stream) {
		return 0, nil
	}
	take := f.chunk
	if take > len(out) {
		take = len(out)
	}
	if take > len(f.stream)-f.pos {
		take = len(f.stream) - f.pos
	}
	copy(out, f.stream[f.pos:f.pos+take])
	f.pos += take
	return take, nil
}

func (f *fakeSeam) send(data []byte) (int, error) {
	if f.closed {
		return 0, errStreamClosed
	}
	if len(data) > f.maxSend {
		f.maxSend = len(data)
	}
	f.sent = append(f.sent, data...)
	return len(data), nil
}

func streamOf(f *fakeSeam) *tcpStream {
	s := &tcpStream{idleLimit: 200000, buf: make([]byte, streamStashCap)}
	s.recv = f.recv
	s.send = f.send
	return s
}

func TestStreamReassembles192ByteChunks(t *testing.T) {
	payload := make([]byte, 5000)
	for i := range payload {
		payload[i] = byte(i*7 + 1)
	}
	f := &fakeSeam{stream: payload, chunk: tcpChunkMax}
	s := streamOf(f)
	got := make([]byte, 5000)
	n, err := s.read(got)
	if err != nil {
		t.Fatal(err)
	}
	if n != 5000 {
		t.Fatalf("n = %d", n)
	}
	for i := range payload {
		if got[i] != payload[i] {
			t.Fatalf("byte %d: got %d want %d", i, got[i], payload[i])
		}
	}
}

func TestStreamShortChunksSurviveRecordReads(t *testing.T) {
	msg := []byte("TLS record header and body")
	for _, c := range []int{1, 2, 3, 17, 64, 192} {
		f := &fakeSeam{stream: msg, chunk: c}
		s := streamOf(f)
		var hdr [5]byte
		if _, err := s.read(hdr[:]); err != nil {
			t.Fatalf("chunk %d header: %v", c, err)
		}
		rest := make([]byte, len(msg)-5)
		if _, err := s.read(rest); err != nil {
			t.Fatalf("chunk %d body: %v", c, err)
		}
		if string(append(hdr[:], rest...)) != string(msg) {
			t.Fatalf("chunk %d reassembled %q", c, append(hdr[:], rest...))
		}
	}
}

func TestStreamZeroPollIsNothingYet(t *testing.T) {
	payload := make([]byte, 600)
	for i := range payload {
		payload[i] = byte(i)
	}
	f := &fakeSeam{stream: payload, chunk: 100, pollEvery: 3}
	s := streamOf(f)
	got := make([]byte, 600)
	n, err := s.read(got)
	if err != nil {
		t.Fatal(err)
	}
	if n != 600 {
		t.Fatalf("n = %d", n)
	}
	for i := range payload {
		if got[i] != payload[i] {
			t.Fatalf("byte %d", i)
		}
	}
}

func TestStreamWritesPaceTo192(t *testing.T) {
	f := &fakeSeam{stream: nil, chunk: tcpChunkMax}
	s := streamOf(f)
	out := make([]byte, 1000)
	for i := range out {
		out[i] = byte(i)
	}
	n, err := s.write(out)
	if err != nil {
		t.Fatal(err)
	}
	if n != 1000 {
		t.Fatalf("n = %d", n)
	}
	if f.maxSend > tcpChunkMax {
		t.Fatalf("max send %d exceeds chunk", f.maxSend)
	}
	if string(f.sent) != string(out) {
		t.Fatal("sent bytes mismatch")
	}
}

func TestStreamDrainStashesEarlyReply(t *testing.T) {
	f := &fakeSeam{stream: []byte("EARLY-REPLY"), chunk: 5}
	s := streamOf(f)
	if _, err := s.write([]byte("request-that-spans-several-segments")); err != nil {
		t.Fatal(err)
	}
	got := make([]byte, 32)
	n, err := s.read(got)
	if err != nil {
		t.Fatal(err)
	}
	if n != 11 || string(got[:n]) != "EARLY-REPLY" {
		t.Fatalf("got %q n=%d", got[:n], n)
	}
}

func TestStreamOverflowAndDeadSeamFailClosed(t *testing.T) {
	payload := make([]byte, 900)
	{
		f := &fakeSeam{stream: payload, chunk: tcpChunkMax}
		s := streamOf(f)
		s.buf = make([]byte, 64)
		var out [900]byte
		if _, err := s.read(out[:]); err != errStreamOverflow {
			t.Fatalf("overflow: %v", err)
		}
	}
	{
		f := &fakeSeam{closed: true, chunk: 10}
		s := streamOf(f)
		var out [16]byte
		if _, err := s.read(out[:]); err != errStreamClosed {
			t.Fatalf("closed: %v", err)
		}
	}
	{
		f := &fakeSeam{stream: nil, chunk: 10}
		s := streamOf(f)
		s.idleLimit = 5
		var out [16]byte
		if _, err := s.read(out[:]); err != errStreamClosed {
			t.Fatalf("idle: %v", err)
		}
	}
}

func TestChainValidationErrorUnwraps(t *testing.T) {
	err := error(chainValidationError{res: resultHostnameMismatch})
	if !IsHostnameMismatch(err) {
		t.Fatal("hostname")
	}
	if IsExpired(err) || IsNoPathToRoot(err) {
		t.Fatal("wrong class")
	}
	exp := error(chainValidationError{res: resultExpired})
	if !IsExpired(exp) || IsHostnameMismatch(exp) {
		t.Fatal("expired")
	}
	np := error(chainValidationError{res: resultNoPathToRoot})
	if !IsNoPathToRoot(np) {
		t.Fatal("chain")
	}
	if IsHostnameMismatch(errStreamTimeout) {
		t.Fatal("timeout is not a name mismatch")
	}
}

func TestStreamAbsoluteDeadlineWinsDuringProgress(t *testing.T) {
	f := &fakeSeam{stream: make([]byte, 900), chunk: 1}
	s := streamOf(f)
	clock := int64(1)
	s.now = func() int64 { clock++; return clock }
	s.deadlineAt = 12
	n, err := s.read(make([]byte, 900))
	if err != errStreamTimeout || n >= 900 || f.pos >= 900 {
		t.Fatalf("progress bypassed absolute deadline: n=%d pos=%d err=%v", n, f.pos, err)
	}
}

func TestStreamExpiredDeadlinePreventsReadAndWrite(t *testing.T) {
	f := &fakeSeam{stream: []byte("queued"), chunk: 6}
	s := streamOf(f)
	s.now = func() int64 { return 10 }
	s.deadlineAt = 10
	s.n = 1
	s.buf[0] = 'x'
	if n, err := s.read(make([]byte, 1)); n != 0 || err != errStreamTimeout {
		t.Fatalf("buffered expired read=%d,%v", n, err)
	}
	if n, err := s.write([]byte("not sent")); n != 0 || err != errStreamTimeout || len(f.sent) != 0 {
		t.Fatalf("expired write=%d,%v sent=%d", n, err, len(f.sent))
	}
}

func TestStreamCallerDeadlineBoundsIdlePolls(t *testing.T) {
	f := &fakeSeam{chunk: 1}
	s := streamOf(f)
	clock := int64(0)
	s.now = func() int64 { clock++; return clock }
	s.deadlineAt = 8
	if _, err := s.read(make([]byte, 1)); err != errStreamTimeout || f.polls > 8 {
		t.Fatalf("idle deadline err=%v polls=%d", err, f.polls)
	}
}
