package layout

import "testing"

func TestLayoutTablesCoverTheExpectedDifferences(t *testing.T) {
	cases := []struct {
		layout ID
		usage  uint8
		shift  bool
		want   rune
	}{
		{US, 0x1c, false, 'y'},
		{US, 0x1d, false, 'z'},
		{US, 0x33, true, ':'},
		{DE, 0x1c, false, 'z'},
		{DE, 0x1d, false, 'y'},
		{DE, 0x33, false, 'ö'},
		{DE, 0x33, true, 'Ö'},
		{DE, 0x36, false, ','},
		{DE, 0x36, true, ';'},
		{DE, 0x37, false, '.'},
		{DE, 0x37, true, ':'},
		{DE, 0x64, false, '<'},
		{DE, 0x64, true, '>'},
		{DE, 0x1f, true, '"'},
	}
	for _, tc := range cases {
		got, ok := Translate(tc.layout, tc.usage, tc.shift)
		if !ok || got != tc.want {
			t.Errorf("Translate(%s, %#x, shift=%v) = (%q,%v), want (%q,true)",
				tc.layout, tc.usage, tc.shift, got, ok, tc.want)
		}
	}
}

func TestLayoutNamesAndUnmappedUsages(t *testing.T) {
	for _, want := range []ID{US, DE} {
		got, ok := Parse(string(want))
		if !ok || got != want {
			t.Errorf("Parse(%q) = (%q,%v)", want, got, ok)
		}
	}
	if _, ok := Parse("fr"); ok {
		t.Fatal("unsupported layout was accepted")
	}
	if _, ok := Translate(DE, 0x2e, false); ok {
		t.Fatal("the dead acute key must remain outside this layout's printable subset")
	}
	if got := Values(); len(got) != 2 || got[0] != "us" || got[1] != "de" {
		t.Fatalf("Values() = %v, want [us de]", got)
	}
}
