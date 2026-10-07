package prof

import (
	"bytes"
	"encoding/binary"
	"strings"
	"testing"

	"virelai/vi"
)

// Small pinned AArch64 ELF shape, constructed from bytes, not a host binary.
func fixtureELF() []byte {
	b := make([]byte, 512)
	copy(b, "\x7fELF\x02\x01\x01")
	put16 := func(o int, v uint16) { binary.LittleEndian.PutUint16(b[o:], v) }
	put32 := func(o int, v uint32) { binary.LittleEndian.PutUint32(b[o:], v) }
	put64 := func(o int, v uint64) { binary.LittleEndian.PutUint64(b[o:], v) }
	put16(16, 2)
	put16(18, 183)
	put32(20, 1)
	put64(40, 64)
	put16(52, 64)
	put16(58, 64)
	put16(60, 3)
	// Section 1 .symtab, section 2 string table. No section names required.
	put32(128+4, 2)
	put64(128+24, 256)
	put64(128+32, 72)
	put32(128+40, 2)
	put64(128+56, 24)
	put32(192+4, 3)
	put64(192+24, 352)
	names := "\x00runtime.worker\x00cmd/compile/main.main\x00"
	copy(b[352:], names)
	put64(192+32, uint64(len(names)))
	for i, start := range []uint64{0x10000, 0x10100} {
		o := 256 + 24*(i+1)
		index := uint32(1)
		if i == 1 {
			index = 16
		}
		put32(o, index)
		b[o+4] = 0x12
		put16(o+6, 1)
		put64(o+8, start)
		put64(o+16, 0x100)
	}
	return b
}

func TestFixtureSymbolsAndFoldedCallerOrder(t *testing.T) {
	symbols, err := ReadSymbols(bytes.NewReader(fixtureELF()))
	if err != nil {
		t.Fatal(err)
	}
	for _, pc := range []uint64{0x10000, 0x100ff} {
		name, ok := symbols.Resolve(pc)
		if !ok || name != "runtime.worker" {
			t.Fatalf("pc=%x: %q %v", pc, name, ok)
		}
	}
	if _, ok := symbols.Resolve(0x10200); ok {
		t.Fatal("exclusive symbol end resolved")
	}
	report := NewReport()
	report.Add(symbols, vi.SampleRecord{PC: 0x10108, Depth: 1, Frames: [16]uint64{0x10004}})
	report.Add(symbols, vi.SampleRecord{PC: 0x9999})
	var folded bytes.Buffer
	if err := report.WriteFolded(&folded); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(folded.String(), "runtime.worker;cmd/compile/main.main 1\n") ||
		report.Percent() != 50 || len(report.Top(10)) != 2 {
		t.Fatalf("report=%+v folded=%q", report, folded.String())
	}
}

func TestMalformedAndStrippedELFRefused(t *testing.T) {
	for _, data := range [][]byte{nil, []byte("not an ELF"), fixtureELF()[:300]} {
		if _, err := ReadSymbols(bytes.NewReader(data)); err == nil {
			t.Fatal("malformed image accepted")
		}
	}
	b := fixtureELF()
	binary.LittleEndian.PutUint16(b[60:], 0)
	if _, err := ReadSymbols(bytes.NewReader(b)); err == nil {
		t.Fatal("stripped image accepted")
	}
	if got := escapeFrame("evil;name\nline"); got != "evil_name_line" {
		t.Fatal(got)
	}
}

func TestSampleWireRefusesMalformedHeaderAndUnusedFrames(t *testing.T) {
	buf := make([]byte, 24+vi.ProfileRecordBytes)
	put32 := func(o int, v uint32) { binary.LittleEndian.PutUint32(buf[o:], v) }
	put32(0, 1)
	put32(4, vi.ProfileRecordBytes)
	put32(8, 1)
	put32(24, 1)
	binary.LittleEndian.PutUint64(buf[16:], 7)
	records, dropped, err := vi.DecodeSamples(buf, 1)
	if err != nil || len(records) != 1 || dropped != 7 {
		t.Fatalf("%v %d %v", records, dropped, err)
	}
	for _, offset := range []int{0, 4, 8, 12, 24, 28, 64, 68, 72} {
		broken := append([]byte(nil), buf...)
		broken[offset] = 255
		if _, _, err := vi.DecodeSamples(broken, 1); err == nil {
			t.Fatalf("bad field at %d accepted", offset)
		}
	}
	if _, _, err := vi.DecodeSamples(buf[:23], 0); err == nil {
		t.Fatal("short header accepted")
	}
}
