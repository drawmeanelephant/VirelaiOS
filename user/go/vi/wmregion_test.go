package vi

// #3348: the kind-31 region-declare wire — the record's byte layout and
// the refuse-before-the-mailbox bounds, pinned against drift in both
// directions.

import "testing"

func TestEncodeRegionTitleWireLayout(t *testing.T) {
	title, ok := EncodeRegionTitle(2, CursorTxt, 10, 24, 500, 300)
	if !ok {
		t.Fatal("encode refused a legal record")
	}
	if title[0] != 2 || title[1] != 1 {
		t.Fatalf("title head = %v, want slot=2 shape=txt(1)", title[:2])
	}
	if got := getU16(title[2:]); got != 10 {
		t.Fatalf("x = %d, want 10", got)
	}
	if got := getU16(title[4:]); got != 24 {
		t.Fatalf("y = %d, want 24", got)
	}
	if got := getU16(title[6:]); got != 500 {
		t.Fatalf("w = %d, want 500", got)
	}
	if got := getU16(title[8:]); got != 300 {
		t.Fatalf("h = %d, want 300", got)
	}
	for i := 10; i < WmRpcTitleMax; i++ {
		if title[i] != 0 {
			t.Fatalf("title[%d] = %#x, reserved tail must stay zero", i, title[i])
		}
	}
}

func TestEncodeRegionTitleRefusals(t *testing.T) {
	if _, ok := EncodeRegionTitle(RegionSlotMax, CursorTxt, 0, 0, 10, 10); ok {
		t.Fatal("a slot past the wire bound encoded")
	}
	if _, ok := EncodeRegionTitle(0, 10, 0, 0, 10, 10); ok {
		t.Fatal("a shape past the CursorShape table encoded")
	}
	if _, ok := EncodeRegionTitle(0, CursorTxt, 0, 0, 0, 10); ok {
		t.Fatal("a half-empty rect (w=0 h>0) encoded")
	}
	if _, ok := EncodeRegionTitle(0, CursorTxt, 0, 0, 10, 0); ok {
		t.Fatal("a half-empty rect (w>0 h=0) encoded")
	}
	if _, ok := EncodeRegionTitle(0, CursorPtr, 0, 0, 0, 0); !ok {
		t.Fatal("the w=h=0 clear form must encode")
	}
}

func TestDecodeRegionTitleRoundTripAndRefusals(t *testing.T) {
	title, _ := EncodeRegionTitle(3, CursorLnk, 100, 40, 60, 12)
	slot, shape, x, y, w, h, ok := DecodeRegionTitle(title)
	if !ok || slot != 3 || shape != 2 || x != 100 || y != 40 || w != 60 || h != 12 {
		t.Fatalf("decode = slot=%d shape=%d (%d,%d %dx%d) ok=%v",
			slot, shape, x, y, w, h, ok)
	}
	var zero [WmRpcTitleMax]byte
	if _, _, _, _, _, _, ok := DecodeRegionTitle(zero); !ok {
		t.Fatal("an all-zero record must decode (slot 0 cleared)")
	}
	zero[0] = RegionSlotMax
	if _, _, _, _, _, _, ok := DecodeRegionTitle(zero); ok {
		t.Fatal("a slot past the bound decoded")
	}
	zero[0], zero[1] = 0, 10
	if _, _, _, _, _, _, ok := DecodeRegionTitle(zero); ok {
		t.Fatal("a shape past the table decoded")
	}
	zero[1] = 0
	zero[8] = 7 // h=7 with w=0 — the half-empty form
	if _, _, _, _, _, _, ok := DecodeRegionTitle(zero); ok {
		t.Fatal("a half-empty rect decoded")
	}
}
