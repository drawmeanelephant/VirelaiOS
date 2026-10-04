package pdf

import "testing"

func TestPipelineCategoryCounters(t *testing.T) {
	cases := [][]byte{
		simple("1 0 0 rg 0 0 48 48 re f", ""),
		fontDocument("BT /F1 12 Tf (AA) Tj ET", "1 0 0 0 1 1 d1 0 0 1 1 re f", ""),
		imageDocument("48 0 0 48 0 0 cm /I1 Do", []byte{255, 0, 0}, "/Width 1 /Height 1 /BitsPerComponent 8 /ColorSpace /DeviceRGB"),
	}
	want := [][WorkKinds]uint64{
		{13325, 3691, 32800, 209, 505, 28, 16, 83, 121, 111, 16, 0, 0, 0, 0, 46, 46, 0, 0, 16384, 1, 121464},
		{54957, 177085, 130928, 1154, 2118, 112, 33, 169, 700, 525, 60, 5, 0, 0, 0, 181, 181, 0, 0, 16384, 1, 45608},
		{50536, 12440, 49304, 809, 1277, 28, 24, 116, 221, 364, 8, 0, 0, 0, 0, 55, 55, 4160, 4096, 16384, 1, 0},
	}
	for i, src := range cases {
		_, _, l := rendered(t, src)
		if l.Counts != want[i] {
			t.Errorf("case %d categories=%v want=%v", i, l.Counts, want[i])
		}
		sum := uint64(0)
		for _, n := range l.Counts {
			sum += n
		}
		if sum != l.Used {
			t.Fatal("double counted or hidden work")
		}
		if l.Counts[Vector] != l.VectorBudget.Used {
			t.Fatal("vector delta counted more than once")
		}
	}
}
func TestLedgerSurvivesEngineAndSourceRecheck(t *testing.T) {
	a := make([]byte, ArenaBytes)
	src := memorySource{data: simple("", "")}
	baseline := Ledger{Max: MaxWork}
	if _, _, f := Render(&src, 0, a, &baseline); f.Code != OK {
		t.Fatal(f)
	}
	l := Ledger{Max: MaxWork, Used: 17, Counts: [WorkKinds]uint64{Hash: 17}}
	_, st, f := Render(&src, 0, a, &l)
	if f.Code != OK || l.Counts[Hash] != baseline.Counts[Hash]+17 ||
		l.Used != baseline.Used+17 || st.Work != l.Used {
		t.Fatal(f, st, l)
	}
	before := l.Used
	hashBefore := l.Counts[Hash]
	if l.Charge(Hash, uint64(len(src.data))) != OK || l.Used != before+uint64(len(src.data)) ||
		l.Counts[Hash] != hashBefore+uint64(len(src.data)) {
		t.Fatal("source recheck did not share ledger")
	}
}
func TestGlyphAndPageGraphicsDepthTogether(t *testing.T) {
	for _, n := range []int{15, 16} {
		q := ""
		Q := ""
		for i := 0; i < n; i++ {
			q += "q "
			Q += "Q "
		}
		src := fontDocument(q+"BT /F1 0 Tf (A) Tj ET "+Q, "1 0 0 0 1 1 d1", "")
		if n == 15 {
			rendered(t, src)
		} else {
			refusal(t, src, GraphicsDepthLimit)
		}
	}
}
