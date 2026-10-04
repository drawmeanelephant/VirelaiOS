package pdf

import (
	"bytes"
	"strings"
	"testing"
)

func TestAllExclusionPolicies(t *testing.T) {
	for _, key := range []string{
		"/Annots []", "/AA << >>", "/Group << >>", "/Metadata null", "/PieceInfo null", "/OC null",
		"/StructParents 0", "/Thumb null", "/Dur 1", "/Trans << >>", "/Outlines null", "/PageLabels null",
	} {
		refusal(t, simple("", key), UnsupportedFeature)
	}
	for _, key := range []string{"/F (no)", "/FFilter /FlateDecode", "/FDecodeParms null", "/FS /URL", "/URI (no)", "/EF << >>"} {
		refusal(t, simple("", "/Unknown null "+key), ExternalResource)
	}
	for _, op := range []string{"S", "s", "B", "B*", "b", "b*", "w", "J", "j", "M", "d", "G", "RG"} {
		refusal(t, simple(op, ""), UnsupportedStroke)
	}
	for _, op := range []string{"gs", "sh", "cs", "CS", "sc", "SC", "scn", "SCN", "k", "K", "ri", "i", "BMC", "BDC", "EMC", "MP", "DP", "BX", "EX", "unknown"} {
		refusal(t, simple(op, ""), UnsupportedFeature)
	}
	for _, op := range []string{"BI", "ID", "EI"} {
		refusal(t, simple(op, ""), UnsupportedImage)
	}
	for _, op := range []string{"d0", "g", "rg", "W", "W*", "BT", "Do", "Tf", "S"} {
		refusal(t, fontDocument("", "1 0 0 0 1 1 d1 "+op, ""), UnsupportedGlyph)
	}
	for _, subtype := range []string{"Type1", "TrueType", "Type0", "CIDFontType0", "CIDFontType2"} {
		refusal(t, resourceDocument("/F 4 0 R", "", "<< /Type /Font /Subtype /"+subtype+" >>"), UnsupportedFont)
	}
	for _, key := range []string{"/BaseEncoding /WinAnsiEncoding", "/ToUnicode null", "/FontFile null", "/WMode 1"} {
		src := fontDocument("", "1 0 0 0 1 1 d1", key)
		refusal(t, src, UnsupportedFont)
	}
	for _, key := range []string{
		"/BitsPerComponent 1 /ColorSpace /DeviceGray", "/BitsPerComponent 16 /ColorSpace /DeviceGray",
		"/BitsPerComponent 8 /ColorSpace /DeviceCMYK", "/BitsPerComponent 8 /ColorSpace [/Indexed /DeviceRGB 1 (x)]",
		"/BitsPerComponent 8 /ColorSpace /DeviceGray /ImageMask true",
		"/BitsPerComponent 8 /ColorSpace /DeviceGray /Interpolate true",
		"/BitsPerComponent 8 /ColorSpace /DeviceGray /Decode [1 0]",
		"/BitsPerComponent 8 /ColorSpace /DeviceGray /SMask null",
		"/BitsPerComponent 8 /ColorSpace /DeviceGray /Mask null",
	} {
		refusal(t, imageDocument("", []byte{1}, "/Width 1 /Height 1 "+key), UnsupportedImage)
	}
	for _, filter := range []string{"DCTDecode", "JPXDecode", "JBIG2Decode", "CCITTFaxDecode", "LZWDecode", "ASCIIHexDecode", "ASCII85Decode", "RunLengthDecode"} {
		refusal(t, imageDocument("", nil, "/Width 1 /Height 1 /BitsPerComponent 8 /ColorSpace /DeviceGray /Filter /"+filter), UnsupportedFilter)
	}
	for _, matrix := range []string{"1 1 0 1 0 0", "1 0 1 1 0 0", "0 0 0 1 0 0", "1 0 0 1 .1 0", "1.1 0 0 1 0 0"} {
		refusal(t, imageDocument(matrix+" cm /I1 Do", []byte{1}, "/Width 1 /Height 1 /BitsPerComponent 8 /ColorSpace /DeviceGray"), UnsupportedImage)
	}
	src := simple("", "")
	for _, test := range []struct {
		from, to string
		c        Code
	}{
		{"/Root 1 0 R", "/Root 1 0 R /Encrypt null", Encrypted},
		{"/Root 1 0 R", "/Root 1 0 R /Prev 0", UnsupportedStructure},
		{"/Root 1 0 R", "/Root 1 0 R /XRefStm 0", UnsupportedStructure},
		{"/Root 1 0 R", "/Root 100 0 R", Malformed},
		{"/Root 1 0 R", "/Root 1 1 R", UnsupportedStructure},
		{"/Size 5", "/Size 6", Malformed},
	} {
		refusal(t, bytes.Replace(src, []byte(test.from), []byte(test.to), 1), test.c)
	}
}
func TestBoundaryNumbersAndStringSpellings(t *testing.T) {
	for _, n := range []string{"0", "+0", "-0", ".0", "0.", "0.000000"} {
		rendered(t, simple(n+" g", ""))
	}
	for _, n := range []string{"1.000001", "-.000001"} {
		refusal(t, simple(n+" g", ""), MalformedContent)
	}
	for _, v := range []string{"32768", "-32768"} {
		rendered(t, simple(".5 0 0 .5 0 0 cm "+v+" 0 m n", ""))
	}
	for _, v := range []string{"32768.000001", "-32768.000001"} {
		refusal(t, simple(v+" 0 m n", ""), CoordinateLimit)
	}
	for _, test := range []struct {
		params string
		c      Code
	}{
		{"256 Tf", OK}, {"256.000001 Tf", UnsupportedText}, {"0 Tf", OK}, {"-1 Tf", UnsupportedText},
	} {
		src := fontDocument("BT /F1 "+test.params+" ET", "1 0 0 0 1 1 d1", "")
		if test.c == OK {
			rendered(t, src)
		} else {
			refusal(t, src, test.c)
		}
	}
	for _, test := range []struct {
		scale string
		c     Code
	}{{"256", OK}, {"256.000001", UnsupportedText}, {"0", UnsupportedText}, {".000001", OK}} {
		src := simple(test.scale+" Tz", "")
		if test.c == OK {
			rendered(t, src)
		} else {
			refusal(t, src, test.c)
		}
	}
	glyph := "1 0 0 0 1 1 d1"
	for _, text := range []string{"(A)", "<41>", "<4 1>", "(\\101)", "(\\A)", "(\\\nA)", "(\\\r\nA)"} {
		rendered(t, fontDocument("/F1 0 Tf BT "+text+" Tj ET", glyph, ""))
	}
}
func TestMasksClipOrderingAndState(t *testing.T) {
	p, _, _ := rendered(t, simple("1 0 0 rg 0 0 24 48 re W f 0 0 1 rg 0 0 48 48 re f", ""))
	if p.Pix[0] != 0xff0000ff || p.Pix[63] != 0xffffffff {
		t.Fatal("old clip paint order")
	}
	p, _, _ = rendered(t, simple("q 0 0 24 48 re W n Q 0 0 48 48 re f", ""))
	if p.Pix[63] != 0xff000000 {
		t.Fatal("clip not restored")
	}
	// Tracked local path is independent of the glyph path and survives BT.
	p, _, _ = rendered(t, fontDocument("0 0 48 48 re BT /F1 0 Tf (A) Tj ET f", "1 0 0 0 1 1 d1 0 0 1 1 re f", ""))
	if p.Pix[0] != 0xff000000 {
		t.Fatal("glyph replaced page path")
	}
	for _, op := range []string{"f", "f*"} {
		p, _, _ = rendered(t, simple("0 0 48 48 re 12 12 24 24 re "+op, ""))
		want := uint32(0xff000000)
		if op == "f*" {
			want = 0xffffffff
		}
		if p.Pix[32*64+32] != want {
			t.Fatal(op)
		}
	}
}
func TestIDsAndInfoDefaults(t *testing.T) {
	for _, n := range []int{32, 33} {
		src := bytes.Replace(simple("", ""), []byte("/Root 1 0 R"), []byte("/Root 1 0 R /ID [("+strings.Repeat("a", n)+") <>]"), 1)
		if n == 32 {
			rendered(t, src)
		} else {
			refusal(t, src, TokenLimit)
		}
	}
	for _, trapped := range []string{"True", "False", "Unknown"} {
		src := document("<< /Type /Catalog /Pages 2 0 R >>", "<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
			"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 .75 .75] /Contents null >>", "<< /CreationDate () /ModDate <> /Producer (x) /Trapped /"+trapped+" >>")
		src = bytes.Replace(src, []byte("/Root 1 0 R"), []byte("/Root 1 0 R /Info 4 0 R"), 1)
		rendered(t, src)
	}
}
