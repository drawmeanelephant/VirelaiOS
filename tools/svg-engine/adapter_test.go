package main

import (
	"testing"
	"unsafe"
	"virelai/vector"
)

func TestArenaLayoutAndAllocation(t *testing.T) {
	a := make([]byte, arenaBytes)
	source := `<svg width="4" height="1"><rect width="2" height="1" fill="red"/></svg>`
	copy(a, source)
	if outputEnd+vector.MaxCommands*int(unsafe.Sizeof(vector.Command{}))+vector.MaxPaints*int(unsafe.Sizeof(vector.Paint{})) > sceneEnd ||
		arenaBytes-stagingEnd != 1_048_576 || scratchEnd-sceneEnd != vector.WorkspaceSize {
		t.Fatal("partition exceeds arena")
	}
	if n := testing.AllocsPerRun(100, func() {
		w, h, code := engine(a, len(source))
		if code != 0 || w != 4 || h != 1 {
			panic("engine failed")
		}
	}); n != 0 {
		t.Fatal(n)
	}
	pix := words(a[sourceEnd:outputEnd])
	if pix[0] != 0xffff0000 || pix[1] != 0xffff0000 || pix[2] != 0 || pix[3] != 0 {
		t.Fatal(pix[:4])
	}
}

func TestExactDestinationClearCounter(t *testing.T) {
	pix := []uint32{1, 2, 3, 4}
	b := vector.Budget{Used: 7, Max: 11}
	if code := clearTarget(pix, 3, &b); code != vector.OK || b.Used != 10 || pix[3] != 4 {
		t.Fatal(code, b, pix)
	}
	if code := clearTarget(pix, 4, &b); code != vector.WorkLimit || b.Used != 10 || pix[3] != 4 {
		t.Fatal(code, b, pix)
	}
}
