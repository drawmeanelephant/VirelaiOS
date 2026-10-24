package printq

import (
	"strings"
	"testing"
)

// memIO is the in-memory staging drive — published files land in wrote.
type memIO struct {
	wrote  map[string][]byte
	armed  bool
	failAt string
}

func (m *memIO) io() IO {
	return IO{
		WriteSafe: func(path string, b []byte) int64 {
			if m.failAt != "" && strings.HasSuffix(path, m.failAt) {
				return -1
			}
			if m.wrote == nil {
				m.wrote = map[string][]byte{}
			}
			m.wrote[path] = append([]byte(nil), b...)
			return 0
		},
		IsDir: func(string) bool { return m.armed },
		Rand:  func(p []byte) (int, error) { copy(p, []byte{1, 2, 3, 4}); return 4, nil },
	}
}

func TestNameFoldsAndSuffixes(t *testing.T) {
	if got, ok := Name("DOC.PDF"); !ok || got != "DOC.PDF" {
		t.Fatalf("Name(DOC.PDF) = %q %v", got, ok)
	}
	if got, ok := Name("page.html"); !ok || got != "page.html.PDF" {
		t.Fatalf("Name(page.html) = %q %v", got, ok)
	}
	if _, ok := Name(""); ok {
		t.Fatal("Name(\"\") must refuse")
	}
	if got, ok := Name(".pdf"); !ok || got != "F.pdf" {
		t.Fatalf("Name(.pdf) = %q %v — leading non-alnum gains F", got, ok)
	}
	// A name at the bound keeps the suffix: base is cut to make room.
	long := strings.Repeat("a", 60)
	got, ok := Name(long)
	if !ok || len(got) > NameMax || !isPDFName(got) {
		t.Fatalf("Name(long) = %q %v", got, ok)
	}
}

func TestNameValidRequiresPDFSuffix(t *testing.T) {
	for name, want := range map[string]bool{
		"JOB.PDF":      true,
		"job.pdf":      true,
		"JOB.TXT":      false, // the lane's kind is in the name
		".PDF":         false, // leading non-alnum
		"NO EXT":       false,
		"BAD/NAME.PDF": false,
		"":             false,
	} {
		if NameValid(name) != want {
			t.Fatalf("NameValid(%q) = %v want %v", name, !want, want)
		}
	}
}

func TestReqNameRoundTrip(t *testing.T) {
	n := ReqName(0x1234abcd)
	if n != "PREQ-1234abcd" || !IsReqName(n) {
		t.Fatalf("ReqName = %q", n)
	}
	if IsReqName("PREQ-xyz") || IsReqName("REQ-1234abcd") || IsReqName("PREQ-1234") {
		t.Fatal("IsReqName accepted a non-marker")
	}
}

func TestPrintStagesPayloadThenMarker(t *testing.T) {
	m := &memIO{armed: true}
	if r := m.io().Print("DOC.PDF", []byte("%PDF-1.7 fake")); r != "" {
		t.Fatalf("Print refused: %q", r)
	}
	if string(m.wrote[Dir+"/DOC.PDF"]) != "%PDF-1.7 fake" {
		t.Fatal("payload not staged")
	}
	if string(m.wrote[Dir+"/PREQ-04030201"]) != "DOC.PDF" {
		t.Fatalf("marker content = %q", m.wrote[Dir+"/PREQ-04030201"])
	}
}

func TestPrintRefusals(t *testing.T) {
	for _, c := range []struct {
		name  string
		armed bool
		job   string
		pdf   []byte
		want  string
	}{
		{"flags-off", false, "D.PDF", []byte("%PDF-"), RefuseLaneDisabled},
		{"not-a-pdf", true, "D.PDF", []byte("hello"), RefuseUnsupported},
		{"empty", true, "D.PDF", nil, RefuseUnsupported},
		{"bad-name", true, "D.TXT", []byte("%PDF-"), RefuseNameRejected},
	} {
		m := &memIO{armed: c.armed}
		if r := m.io().Print(c.job, c.pdf); r != c.want {
			t.Fatalf("%s: Print = %q want %q", c.name, r, c.want)
		}
		if len(m.wrote) != 0 {
			t.Fatalf("%s: refused stage still wrote %v", c.name, m.wrote)
		}
	}
	// too-large must keep the %PDF- magic to reach the size check
	big := make([]byte, JobMax+1)
	copy(big, "%PDF-")
	m := &memIO{armed: true}
	if r := m.io().Print("D.PDF", big); r != RefuseTooLarge {
		t.Fatalf("too-large: %q", r)
	}
	// A failed marker write leaves the payload in place but unmarked.
	m = &memIO{armed: true, failAt: "PREQ-04030201"}
	if r := m.io().Print("D.PDF", []byte("%PDF-x")); r != RefuseIO {
		t.Fatalf("marker io: %q", r)
	}
	if _, ok := m.wrote[Dir+"/PREQ-04030201"]; ok {
		t.Fatal("failed marker write still landed")
	}
}
