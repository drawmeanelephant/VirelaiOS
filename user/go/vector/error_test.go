package vector

import "testing"

func TestFailureMessagesAndIndexes(t *testing.T) {
	for c := OK; c <= InvalidBuffer; c++ {
		f := failure(c, -1, -1)
		if len(f.Error()) > 128 || (f.Error() == "") != (c == OK) || f.Offset != -1 || f.Paint != -1 || f.Command != -1 {
			t.Fatal(c, f)
		}
	}
}
func TestNoPartialPaintOnLaterFailure(t *testing.T) {
	s := rectScene(1, 1)
	s.Paints = append(s.Paints, Paint{First: 5, Count: 1, Transform: Affine{A: 1, D: 1}, Color: 0xffffffff})
	s.Commands = append(s.Commands, Command{Verb: Line})
	dst := Target{[]uint32{0x12345678}, 1, 1, 1}
	b := Budget{Max: MaxWork}
	_, f := Rasterize(s, dst, scratchBuffer(), &b)
	if f.Code != Malformed || f.Paint != 1 || f.Command != 5 || dst.Pix[0] != 0x12345678 {
		t.Fatal(f, dst.Pix)
	}
}
