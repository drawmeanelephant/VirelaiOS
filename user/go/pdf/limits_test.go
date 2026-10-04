package pdf

import (
	"bytes"
	"fmt"
	"strings"
	"testing"
	"unsafe"
	"virelai/vector"
)

func syntaxEngine(data []byte) engine {
	l := &Ledger{Max: MaxWork}
	return engine{src: &memorySource{data: data}, l: l, f: fail(OK), object: -1, page: -1,
		window: make([]byte, 65536), winStart: -1, parse: make([]parseFrame, 32)}
}
func parseSyntax(data string) (value, Failure) {
	e := syntaxEngine([]byte(data))
	c := cursor{&e, 0, len(data)}
	v := c.value()
	return v, e.f
}
func TestTokenContainerDepthBoundaries(t *testing.T) {
	for _, test := range []struct {
		data string
		want Code
	}{
		{"/" + strings.Repeat("A", 64), OK}, {"/" + strings.Repeat("A", 65), TokenLimit},
		{strings.Repeat("0", 32), OK}, {strings.Repeat("0", 33), TokenLimit},
		{"(" + strings.Repeat("x", 4096) + ")", OK}, {"(" + strings.Repeat("x", 4097) + ")", TokenLimit},
		{strings.Repeat("(", 32) + strings.Repeat(")", 32), OK}, {strings.Repeat("(", 33) + strings.Repeat(")", 33), DepthLimit},
		{"[" + strings.Repeat("null ", 512) + "]", OK}, {"[" + strings.Repeat("null ", 513) + "]", ContainerLimit},
		{strings.Repeat("[", 32) + strings.Repeat("]", 32), OK}, {strings.Repeat("[", 33) + strings.Repeat("]", 33), DepthLimit},
		{"1.123456", OK}, {"1.1234567", Malformed}, {"1e2", Malformed},
		{"<A>", OK}, {"<A Z>", Malformed}, {"<< /A 1 /A 2 >>", Malformed}, {"<< /A#42 1 /AB 2 >>", Malformed},
		{"(\\\r\nx\\101\\n)", OK}, {"()", OK}, {"<>", OK},
	} {
		_, f := parseSyntax(test.data)
		if f.Code != test.want {
			t.Fatalf("%q: %s want %s", test.data[:min(60, len(test.data))], f.Error(), fail(test.want).Error())
		}
	}
	for _, n := range []int{256, 257} {
		var b strings.Builder
		b.WriteString("<<")
		for i := 0; i < n; i++ {
			fmt.Fprintf(&b, " /K%d null", i)
		}
		b.WriteString(" >>")
		_, f := parseSyntax(b.String())
		want := OK
		if n == 257 {
			want = ContainerLimit
		}
		if f.Code != want {
			t.Fatal(n, f)
		}
	}
}
func TestExactWorkBuckets(t *testing.T) {
	for k := WorkKind(0); k < WorkKinds; k++ {
		l := Ledger{Max: MaxWork}
		if l.Charge(k, 17) != OK || l.Used != 17 || l.Counts[k] != 17 {
			t.Fatal(k, l)
		}
		for other := WorkKind(0); other < WorkKinds; other++ {
			if other != k && l.Counts[other] != 0 {
				t.Fatal("cross bucket")
			}
		}
	}
	l := Ledger{Max: MaxWork, Used: MaxWork - 1}
	if l.Charge(Token, 1) != OK || l.Charge(Token, 1) != WorkLimit || l.Used != MaxWork || l.Counts[Token] != 1 {
		t.Fatal("work bound")
	}
	l = Ledger{Max: MaxWork}
	if l.Read(23) != OK || l.Used != 24 || l.Counts[IO] != 1 || l.Counts[Copy] != 23 || l.ReadBytes != 23 {
		t.Fatal(l)
	}
	if l.Rewind() != OK || l.Used != 25 || l.Rewinds != 1 {
		t.Fatal(l)
	}
	if l.expand() != OK || l.Used != 26 || l.Counts[DecodeByte] != 1 || l.Expanded != 1 {
		t.Fatal(l)
	}
}
func TestLedgerBoundaries(t *testing.T) {
	l := Ledger{Max: MaxWork, Expanded: MaxExpansion - 1}
	if l.expand() != OK || l.expand() != ExpansionLimit || l.Expanded != MaxExpansion {
		t.Fatal("expansion")
	}
	l = Ledger{Max: MaxWork, ReadBytes: MaxReads - 1}
	if l.Read(1) != OK || l.Read(1) != ReadLimit || l.ReadBytes != MaxReads {
		t.Fatal("read")
	}
	l = Ledger{Max: MaxWork, Rewinds: 31}
	if l.Rewind() != OK || l.Rewind() != ReadLimit || l.Rewinds != 32 {
		t.Fatal("rewind")
	}
	l = Ledger{Max: MaxWork}
	if l.Open() != OK || l.Open() != OK || l.Open() != HandleLimit || l.Handles != 2 {
		t.Fatal("handles")
	}
	if l.Close() != OK || l.Close() != OK || l.Handles != 0 {
		t.Fatal("close")
	}
	var clock uint64
	calls := 0
	l = Ledger{Max: MaxWork, Now: func() uint64 { calls++; return clock }, Deadline: MaxNanos}
	if l.Charge(Record, 1023) != OK || calls != 0 {
		t.Fatal("clock interval")
	}
	clock = MaxNanos
	if l.Charge(Record, 1) != OK || calls != 1 {
		t.Fatal("inclusive deadline")
	}
	clock++
	if l.Charge(Record, 1024) != TimeLimit || calls != 2 || l.Used != 2048 {
		t.Fatal("time+1")
	}
}
func TestAdmissionAndBufferBoundaries(t *testing.T) {
	a := make([]byte, ArenaBytes+8)
	l := Ledger{Max: MaxWork}
	s := memorySource{data: simple("", "")}
	for _, test := range []struct {
		a []byte
		c Code
	}{{a[:ArenaBytes], OK}, {a[:ArenaBytes-1], MemoryLimit}, {a, MemoryLimit}, {a[1 : ArenaBytes+1], InvalidBuffer}} {
		l = Ledger{Max: MaxWork}
		_, _, f := Render(&s, 0, test.a, &l)
		if f.Code != test.c {
			t.Fatal(f, test.c)
		}
	}
	s.size = MaxSource + 1
	l = Ledger{Max: MaxWork}
	if p, _, f := Render(&s, 0, a[:ArenaBytes], &l); f.Code != SourceLimit || p.Pix != nil || l.Used != 0 {
		t.Fatal("source cap+1", f)
	}
	s.size = 0
	s.code = ReadFailed
	l = Ledger{Max: MaxWork}
	if _, _, f := Render(&s, 0, a[:ArenaBytes], &l); f.Code != ReadFailed {
		t.Fatal(f)
	}
	s.code = OK
	l = Ledger{Max: MaxWork}
	if _, _, f := Render(&s, 1, a[:ArenaBytes], &l); f.Code != PageRange {
		t.Fatal(f)
	}
	l = Ledger{Max: MaxWork, Deadline: MaxNanos, Now: func() uint64 { return MaxNanos + 1 }}
	if _, _, f := Render(&s, 0, a[:ArenaBytes], &l); f.Code != TimeLimit {
		t.Fatal(f)
	}
	// Exactly 4 MiB including a single metadata-free comment. Offsets are
	// regenerated, not repaired by the parser.
	base := document("<< /Type /Catalog /Pages 2 0 R >>", "<< /Type /Pages /Kids [3 0 R] /Count 1 >>", "<< /Type /Page /Parent 2 0 R /MediaBox [0 0 .75 .75] >>")
	padding := MaxSource - len(base) - 6
	// Put the comment after EOF's mandatory marker only as whitespace is
	// forbidden there; insert it before the first object, adjusting xref.
	headerEnd := bytes.Index(base, []byte("1 0 obj"))
	prefix := append(bytes.Clone(base[:headerEnd]), []byte("%"+strings.Repeat("x", padding)+"\n")...)
	shift := len(prefix) - headerEnd
	body := string(base[headerEnd:])
	for i := 1; i <= 3; i++ {
		old := bytes.Index(base, []byte(fmt.Sprintf("%d 0 obj", i)))
		body = strings.Replace(body, fmt.Sprintf("%010d 00000 n", old), fmt.Sprintf("%010d 00000 n", old+shift), 1)
	}
	old := bytes.Index(base, []byte("xref\n"))
	body = strings.Replace(body, fmt.Sprintf("startxref\n%d\n", old), fmt.Sprintf("startxref\n%d\n", old+shift), 1)
	full := append(prefix, []byte(body)...)
	// startxref grows by decimal digits; use ignored header padding to make
	// the complete file exact without adding source data after EOF.
	if len(full) > MaxSource {
		t.Fatal(len(full))
	}
	full = append(full, bytes.Repeat([]byte(" "), MaxSource-len(full))...)
	if len(full) != MaxSource {
		t.Fatal(len(full))
	}
	rendered(t, full)
	refusal(t, append(full, ' '), SourceLimit)
}
func TestSceneBoundaries(t *testing.T) {
	// Exercise admission before record writes independently of pixel work.
	e := engine{l: &Ledger{Max: MaxWork}, f: fail(OK), state: &contentState{}, path: make([]vector.Command, MaxCommands)}
	e.stats.Commands = MaxCommands - 1
	e.emit(vector.Command{Verb: vector.Move})
	if e.f.Code != OK || e.stats.Commands != MaxCommands {
		t.Fatal(e.f)
	}
	e.emit(vector.Command{Verb: vector.Line})
	if e.f.Code != SceneLimit {
		t.Fatal(e.f)
	}
	e.f = fail(OK)
	e.stats.Commands = 0
	e.stats.Contours = MaxContours - 1
	e.state.pathN = 0
	e.emit(vector.Command{Verb: vector.Move})
	if e.f.Code != OK {
		t.Fatal(e.f)
	}
	e.emit(vector.Command{Verb: vector.Move})
	if e.f.Code != SceneLimit {
		t.Fatal(e.f)
	}
	e.f = fail(OK)
	e.stats.Paints = MaxPaints
	e.state.pathN = 1
	e.endPath(true, vector.NonZero)
	if e.f.Code != SceneLimit {
		t.Fatal(e.f)
	}
	if unsafe.Sizeof(vector.Command{})*MaxCommands+unsafe.Sizeof(vector.Paint{})*MaxPaints > 1_048_576 {
		t.Fatal("scene capacity")
	}
}
func TestEveryVectorFailureMapping(t *testing.T) {
	for c := vector.Code(0); c <= vector.InvalidBuffer; c++ {
		code := vectorCode(c)
		if (c == vector.OK) != (code == OK) {
			t.Fatal(c, code)
		}
	}
	if vectorCode(vector.CurveLimit) != CurveLimit || vectorCode(vector.SegmentLimit) != SegmentLimit {
		t.Fatal("lossy mapping")
	}
}
func TestContentsOperandsAndTJBoundaries(t *testing.T) {
	for _, n := range []int{16, 17} {
		content := strings.Repeat("0 ", n) + "f"
		want := MalformedContent
		if n == 17 {
			want = ContainerLimit
		}
		refusal(t, simple(content, ""), want)
	}
	glyph := "1 0 0 0 1 1 d1"
	for _, n := range []int{512, 513} {
		src := fontDocument("BT /F1 0 Tf ["+strings.Repeat("0 ", n)+"] TJ ET", glyph, "")
		if n == 512 {
			rendered(t, src)
		} else {
			refusal(t, src, ContainerLimit)
		}
	}
	for _, n := range []int{4096, 4097} {
		src := fontDocument("BT /F1 0 Tf ["+strings.Repeat("() ", 511)+"("+strings.Repeat("A", n)+")] TJ ET", glyph, "")
		if n == 4096 { // Expansion/glyph work is a separate legitimate ceiling.
			a := make([]byte, ArenaBytes)
			l := Ledger{Max: MaxWork}
			_, _, f := Render(&memorySource{data: src}, 0, a, &l)
			if f.Code != WorkLimit {
				t.Fatal("4096 supported string admission:", f)
			}
		} else {
			refusal(t, src, TokenLimit)
		}
	}
}
