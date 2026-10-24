package main

import (
	"strings"
	"testing"

	"virelai/hostint"
	"virelai/speech"
	"virelai/vi"
)

// sayFixture is the SAY/ dir's in-memory twin: items is the listing the
// seam returns, bodies the marker contents. The lane dir stays the
// hostintFixture's f.files — records land there, markers live here.
type sayFixture struct {
	items map[string]vi.DirItem
	body  map[string][]byte
}

func newSayFixture(t *testing.T) *sayFixture {
	s := &sayFixture{items: map[string]vi.DirItem{}, body: map[string][]byte{}}
	prevList := hostspeechListDir
	prevRead := hostspeechReadFile
	prevDel := hostspeechDeleteFile
	prevFailed := hostspeechListFailed
	t.Cleanup(func() {
		hostspeechListDir = prevList
		hostspeechReadFile = prevRead
		hostspeechDeleteFile = prevDel
		hostspeechListFailed = prevFailed
	})
	hostspeechListDir = func() ([]vi.DirItem, bool) {
		items := make([]vi.DirItem, 0, len(s.items))
		for _, it := range s.items {
			items = append(items, it)
		}
		return items, false
	}
	hostspeechReadFile = func(name string) ([]byte, int64) {
		b, ok := s.body[name]
		if !ok {
			return nil, -vi.ErrENOENT
		}
		out := make([]byte, len(b))
		copy(out, b)
		return out, int64(len(b))
	}
	hostspeechDeleteFile = func(name string) int64 {
		delete(s.items, name)
		delete(s.body, name)
		return 0
	}
	return s
}

func (s *sayFixture) plantSay(id uint32, text string) {
	n := speech.SayName(id)
	s.items[n] = vi.DirItem{Name: n, Size: uint64(len(text))}
	s.body[n] = []byte(text)
}

func (s *sayFixture) has(name string) bool { _, ok := s.items[name]; return ok }

// speakRecords decodes every SPEAK-* record the lane dir holds.
func (f *hostintFixture) speakRecords() []hostint.Record {
	var out []hostint.Record
	for name, b := range f.files {
		if strings.HasPrefix(name, "SPEAK-") {
			if rec, ok := hostint.DecodeRecord(b); ok {
				out = append(out, rec)
			}
		}
	}
	return out
}

func TestHostspeechEmitsSignedRecord(t *testing.T) {
	f := newHostintFixture(t, strings.Repeat("b7", 32))
	s := newSayFixture(t)
	hostintSeatInit()
	f.plantArmed(0xBEEF, hostint.LaneSpeech)
	hostintSeatTick()
	if !f.hasLog("lanes=speech") {
		t.Fatalf("armed mask missing speech: %v", f.logs)
	}
	s.plantSay(0x00000001, "hello")
	hostintSeatTick()
	recs := f.speakRecords()
	if len(recs) != 1 {
		t.Fatalf("records = %d", len(recs))
	}
	rec := recs[0]
	if rec.Tag != hostint.TagSpeak || rec.Seq != 1 || rec.Epoch != 0xBEEF || rec.Sender != 0 {
		t.Fatalf("record fields = %+v", rec)
	}
	if !rec.Verify(f.key) {
		t.Fatal("emitted record does not verify under the lane key")
	}
	if string(rec.Payload) != "hello" {
		t.Fatalf("payload = %q", rec.Payload)
	}
	if !f.hasLog(MarkerHostintSpeak + "seq=1 len=5 sha=2cf24dba") {
		t.Fatalf("missing speak marker: %v", f.logs)
	}
	if s.has("SAY-00000001") {
		t.Fatal("consumed SAY marker not deleted")
	}
}

func TestHostspeechRefusals(t *testing.T) {
	cases := []struct {
		name    string
		setup   func(s *sayFixture)
		refusal string
	}{
		{"payload_not_text empty", func(s *sayFixture) { s.plantSay(1, "") }, hostint.RefuseNotText},
		{"payload_not_text binary", func(s *sayFixture) {
			n := speech.SayName(1)
			s.items[n] = vi.DirItem{Name: n, Size: 2}
			s.body[n] = []byte{0xff, 0xfe}
		}, hostint.RefuseNotText},
		{"payload_too_large", func(s *sayFixture) {
			n := speech.SayName(1)
			s.items[n] = vi.DirItem{Name: n, Size: speech.TextMax + 1}
			s.body[n] = make([]byte, speech.TextMax+1)
		}, hostint.RefuseTooLarge},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newHostintFixture(t, strings.Repeat("b7", 32))
			s := newSayFixture(t)
			hostintSeatInit()
			f.plantArmed(9, hostint.LaneSpeech)
			hostintSeatTick()
			tc.setup(s)
			hostintSeatTick()
			if !f.hasLog("lane=speech reason=" + tc.refusal) {
				t.Fatalf("missing %s refusal: %v", tc.refusal, f.logs)
			}
			if n := len(f.speakRecords()); n != 0 {
				t.Fatalf("refused SAY emitted %d records", n)
			}
			if s.has("SAY-00000001") {
				t.Fatal("refused SAY not deleted")
			}
		})
	}
}

func TestHostspeechOneMarkerPerTick(t *testing.T) {
	f := newHostintFixture(t, strings.Repeat("b7", 32))
	s := newSayFixture(t)
	hostintSeatInit()
	f.plantArmed(7, hostint.LaneSpeech)
	hostintSeatTick()
	s.plantSay(1, "one")
	s.plantSay(2, "two")
	hostintSeatTick()
	if n := len(f.speakRecords()); n != 1 {
		t.Fatalf("one tick consumed %d markers", n)
	}
	hostintSeatTick()
	if n := len(f.speakRecords()); n != 2 {
		t.Fatalf("second marker did not drain: %d records", n)
	}
}

func TestHostspeechSilentWithoutLaneBit(t *testing.T) {
	f := newHostintFixture(t, strings.Repeat("b7", 32))
	s := newSayFixture(t)
	hostintSeatInit()
	// Clip-only ARMED: the speech drive must not even look at the dir.
	f.plantArmed(9, hostint.LaneClip)
	hostintSeatTick()
	s.plantSay(1, "hello")
	hostintSeatTick()
	if n := len(f.speakRecords()); n != 0 {
		t.Fatalf("unarmed lane emitted %d records", n)
	}
	if !s.has("SAY-00000001") {
		t.Fatal("SAY touched while the lane bit was off")
	}
	if f.hasLog("speech") {
		t.Fatalf("unarmed lane logged speech traffic: %v", f.logs)
	}
}

func TestHostspeechBackpressuredMarkerWaits(t *testing.T) {
	f := newHostintFixture(t, strings.Repeat("b7", 32))
	s := newSayFixture(t)
	hostintSeatInit()
	f.plantArmed(3, hostint.LaneSpeech)
	hostintSeatTick()
	// Fill the emit bound: four unconsumed SPEAK records (seqs the
	// runner has not drained) leave no room for the marker's emit.
	for i := uint32(1); i <= hostint.MaxOutstanding; i++ {
		f.files[hostint.RecordName(hostint.TagSpeak, i)] =
			hostint.EncodeRecord(f.key, hostint.TagSpeak, 0, i, 3, []byte("x"))
	}
	hostintSeatTick() // refresh the engine's listing view
	s.plantSay(9, "held")
	hostintSeatTick()
	if !s.has("SAY-00000009") {
		t.Fatal("backpressured SAY was consumed — the intent must wait")
	}
	if n := len(f.speakRecords()); n != int(hostint.MaxOutstanding) {
		t.Fatalf("emitted past the bound: %d", n)
	}
}
