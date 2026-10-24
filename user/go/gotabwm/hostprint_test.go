package main

import (
	"strings"
	"testing"

	"virelai/hostint"
	"virelai/printq"
	"virelai/vi"
)

// pqFixture is the PRINT dir's in-memory twin — the obxFixture shape:
// items is the listing the seam returns (PREQ names + payload names with
// kind/size), bodies the file contents only the PREQ markers carry.
type pqFixture struct {
	items map[string]vi.DirItem
	body  map[string][]byte
}

func newPqFixture(t *testing.T) *pqFixture {
	o := &pqFixture{items: map[string]vi.DirItem{}, body: map[string][]byte{}}
	prevList := hostprintListDir
	prevRead := hostprintReadFile
	prevDel := hostprintDeleteFile
	t.Cleanup(func() {
		hostprintListDir = prevList
		hostprintReadFile = prevRead
		hostprintDeleteFile = prevDel
	})
	hostprintListDir = func() ([]vi.DirItem, bool) {
		items := make([]vi.DirItem, 0, len(o.items))
		for _, it := range o.items {
			items = append(items, it)
		}
		return items, false
	}
	hostprintReadFile = func(name string) ([]byte, int64) {
		b, ok := o.body[name]
		if !ok {
			return nil, -vi.ErrENOENT
		}
		out := make([]byte, len(b))
		copy(out, b)
		return out, int64(len(b))
	}
	hostprintDeleteFile = func(name string) int64 {
		delete(o.items, name)
		delete(o.body, name)
		return 0
	}
	return o
}

func (o *pqFixture) plantReq(id uint32, payloadName string) {
	n := printq.ReqName(id)
	o.items[n] = vi.DirItem{Name: n, Size: uint64(len(payloadName))}
	o.body[n] = []byte(payloadName)
}

func (o *pqFixture) plantPayload(name string, size uint64, isDir bool) {
	o.items[name] = vi.DirItem{Name: name, Size: size, IsDir: isDir}
}

func (o *pqFixture) has(name string) bool { _, ok := o.items[name]; return ok }

// printRecords decodes every PRINT-* record the lane dir holds.
func (f *hostintFixture) printRecords() []hostint.Record {
	var out []hostint.Record
	for name, b := range f.files {
		if strings.HasPrefix(name, "PRINT-") {
			if rec, ok := hostint.DecodeRecord(b); ok {
				out = append(out, rec)
			}
		}
	}
	return out
}

func TestHostprintEmitsSignedRecord(t *testing.T) {
	f := newHostintFixture(t, strings.Repeat("c1", 32))
	o := newPqFixture(t)
	hostintSeatInit()
	f.plantArmed(0xBEEF, hostint.LanePrint)
	hostintSeatTick()
	if !f.hasLog("lanes=print") {
		t.Fatalf("armed mask missing print: %v", f.logs)
	}
	o.plantPayload("REPORT.PDF", 211, false)
	o.plantReq(0x00000001, "REPORT.PDF")
	hostintSeatTick()
	recs := f.printRecords()
	if len(recs) != 1 {
		t.Fatalf("records = %d", len(recs))
	}
	rec := recs[0]
	if rec.Tag != hostint.TagPrint || rec.Seq != 1 || rec.Epoch != 0xBEEF || rec.Sender != 0 {
		t.Fatalf("record fields = %+v", rec)
	}
	if !rec.Verify(f.key) {
		t.Fatal("emitted record does not verify under the lane key")
	}
	name, size, ok := dropPayload(rec.Payload)
	if !ok || name != "REPORT.PDF" || size != 211 {
		t.Fatalf("payload decode = %q %d %v", name, size, ok)
	}
	if !f.hasLog(MarkerHostintPrint + "seq=1 name=REPORT.PDF size=211") {
		t.Fatalf("missing print marker: %v", f.logs)
	}
	if o.has("PREQ-00000001") {
		t.Fatal("consumed PREQ still present")
	}
}

func TestHostprintRefusals(t *testing.T) {
	for _, c := range []struct {
		name  string
		setup func(o *pqFixture)
		want  string
	}{
		{"gone", func(o *pqFixture) { o.plantReq(1, "ABSENT.PDF") }, hostint.RefuseGone},
		{"name_rejected", func(o *pqFixture) {
			o.plantReq(2, "NOTAPDF.TXT")
			o.plantPayload("NOTAPDF.TXT", 9, false)
		}, hostint.RefuseNameRejected},
		{"unsupported_kind", func(o *pqFixture) {
			o.plantReq(3, "DIR.PDF")
			o.plantPayload("DIR.PDF", 0, true)
		}, hostint.RefuseUnsupported},
		{"payload_too_large", func(o *pqFixture) {
			o.plantReq(4, "BIG.PDF")
			o.plantPayload("BIG.PDF", printq.JobMax+1, false)
		}, hostint.RefuseTooLarge},
	} {
		t.Run(c.name, func(t *testing.T) {
			f := newHostintFixture(t, strings.Repeat("c1", 32))
			o := newPqFixture(t)
			hostintSeatInit()
			f.plantArmed(0xBEEF, hostint.LanePrint)
			hostintSeatTick()
			c.setup(o)
			hostintSeatTick()
			if !f.hasLog("hostint refuse lane=print reason=" + c.want) {
				t.Fatalf("missing refusal %q: %v", c.want, f.logs)
			}
			if len(f.printRecords()) != 0 {
				t.Fatal("refused intent still emitted")
			}
		})
	}
}

func TestHostprintLaneOffIsSilent(t *testing.T) {
	f := newHostintFixture(t, strings.Repeat("c1", 32))
	o := newPqFixture(t)
	hostintSeatInit()
	f.plantArmed(0xBEEF, hostint.LaneNotify) // armed, but not this lane
	hostintSeatTick()
	o.plantPayload("REPORT.PDF", 211, false)
	o.plantReq(1, "REPORT.PDF")
	hostintSeatTick()
	if len(f.printRecords()) != 0 {
		t.Fatalf("lane-off emitted: %v", f.printRecords())
	}
	for _, l := range f.logs {
		if strings.Contains(l, "lane=print") {
			t.Fatalf("lane-off logged a print line: %q", l)
		}
	}
	if !o.has("PREQ-00000001") {
		t.Fatal("lane-off consumed the intent marker")
	}
}
