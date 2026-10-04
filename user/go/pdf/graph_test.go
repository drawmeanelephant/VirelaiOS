package pdf

import (
	"bytes"
	"fmt"
	"strings"
	"testing"
)

func treeDocument(pages int, perPage int) []byte {
	objects := []string{"<< /Type /Catalog /Pages 2 0 R >>", ""}
	var kids strings.Builder
	for i := 0; i < pages; i++ {
		n := len(objects) + 1
		fmt.Fprintf(&kids, "%d 0 R ", n)
		objects = append(objects, "")
		var content strings.Builder
		for j := 0; j < perPage; j++ {
			fmt.Fprintf(&content, "%d 0 R ", len(objects)+1)
			objects = append(objects, stream(nil, ""))
		}
		objects[n-1] = fmt.Sprintf("<< /Type /Page /Parent 2 0 R /MediaBox [0 0 .75 .75] /Contents [%s] >>", content.String())
	}
	objects[1] = fmt.Sprintf("<< /Type /Pages /Kids [%s] /Count %d >>", kids.String(), pages)
	return document(objects...)
}
func TestPagesContentsAndObjectBounds(t *testing.T) {
	rendered(t, treeDocument(256, 0))
	// 257 kids is the independently earlier Kids limit. A two-node tree
	// reaches the page limit without first exceeding the Kids capacity.
	var objects []string
	objects = append(objects, "<< /Type /Catalog /Pages 2 0 R >>", "<< /Type /Pages /Kids [3 0 R 4 0 R] /Count 257 >>", "", "")
	for i := 0; i < 2; i++ {
		var kids strings.Builder
		count := 128
		if i == 1 {
			count = 129
		}
		for j := 0; j < count; j++ {
			fmt.Fprintf(&kids, "%d 0 R ", len(objects)+1)
			objects = append(objects, fmt.Sprintf("<< /Type /Page /Parent %d 0 R /MediaBox [0 0 .75 .75] >>", i+3))
		}
		objects[i+2] = fmt.Sprintf("<< /Type /Pages /Parent 2 0 R /Kids [%s] /Count %d >>", kids.String(), count)
	}
	refusal(t, document(objects...), PageLimit)
	rendered(t, treeDocument(1, 16))
	refusal(t, treeDocument(1, 17), ContainerLimit)
	// Every one of 8192 objects is reachable, with small empty pages/streams
	// and empty Type 3 resource dictionaries, not an orphan capacity proxy.
	objects = []string{"<< /Type /Catalog /Pages 2 0 R >>", ""}
	var kids strings.Builder
	emptyFont := "<< /Type /Font /Subtype /Type3 /FontBBox [0 0 0 0] /FontMatrix [1 0 0 1 0 0] /CharProcs << >> /Encoding << /Differences [] >> /FirstChar 0 /LastChar 0 /Widths [0] >>"
	for i := 0; i < 256; i++ {
		n := len(objects) + 1
		objects = append(objects, "")
		fmt.Fprintf(&kids, "%d 0 R ", n)
		var contents, fonts strings.Builder
		for j := 0; j < 16; j++ {
			fmt.Fprintf(&contents, "%d 0 R ", len(objects)+1)
			objects = append(objects, stream(nil, ""))
		}
		count := 15
		if i == 255 {
			count = 13
		}
		for j := 0; j < count; j++ {
			fmt.Fprintf(&fonts, "/F%d %d 0 R ", j, len(objects)+1)
			objects = append(objects, emptyFont)
		}
		objects[n-1] = fmt.Sprintf("<< /Type /Page /Parent 2 0 R /MediaBox [0 0 .75 .75] /Contents [%s] /Resources << /Font << %s >> >> >>", contents.String(), fonts.String())
	}
	if len(objects) != 8192 {
		t.Fatal(len(objects))
	}
	objects[1] = fmt.Sprintf("<< /Type /Pages /Kids [%s] /Count 256 >>", kids.String())
	_, st, _ := rendered(t, document(objects...))
	if st.Objects != 8192 {
		t.Fatal(st.Objects)
	}
	refusal(t, document(append(objects, "null")...), ObjectLimit)
}
func TestPageTreeDepthAndCycles(t *testing.T) {
	for _, depth := range []int{31, 32} {
		objects := []string{"<< /Type /Catalog /Pages 2 0 R >>"}
		for i := 0; i < depth; i++ {
			n := i + 2
			parent := ""
			if i > 0 {
				parent = fmt.Sprintf("/Parent %d 0 R", n-1)
			}
			if i == depth-1 {
				objects = append(objects, fmt.Sprintf("<< /Type /Page %s /MediaBox [0 0 .75 .75] >>", parent))
			} else {
				objects = append(objects, fmt.Sprintf("<< /Type /Pages %s /Kids [%d 0 R] /Count 1 >>", parent, n+1))
			}
		}
		if depth == 31 {
			rendered(t, document(objects...))
		} else {
			refusal(t, document(objects...), DepthLimit)
		}
	}
	for _, src := range [][]byte{
		document("<< /Type /Catalog /Pages 2 0 R >>", "<< /Type /Pages /Kids [2 0 R] /Count 1 >>"),
		document("<< /Type /Catalog /Pages 2 0 R >>", "<< /Type /Pages /Kids [3 0 R 3 0 R] /Count 2 >>", "<< /Type /Page /Parent 2 0 R /MediaBox [0 0 .75 .75] >>"),
		document("<< /Type /Catalog /Pages 2 0 R >>", "<< /Type /Pages /Kids [3 0 R] /Count 1 >>", "<< /Type /Page /Parent 1 0 R /MediaBox [0 0 .75 .75] >>"),
	} {
		refusal(t, src, Malformed)
	}
}
func resourceDocument(fonts, xobjects string, objects ...string) []byte {
	prefix := []string{"<< /Type /Catalog /Pages 2 0 R >>", "<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 .75 .75] /Resources << /Font << " + fonts + " >> /XObject << " + xobjects + " >> >> >>"}
	return document(append(prefix, objects...)...)
}
func TestResourceFontGlyphBounds(t *testing.T) {
	font := "<< /Type /Font /Subtype /Type3 /FontBBox [0 0 0 0] /FontMatrix [1 0 0 1 0 0] /CharProcs << >> /Encoding << /Differences [] >> /FirstChar 0 /LastChar 0 /Widths [0] >>"
	for _, n := range []int{16, 17} {
		var names strings.Builder
		var objects []string
		for i := 0; i < n; i++ {
			fmt.Fprintf(&names, "/F%d %d 0 R ", i, i+4)
			objects = append(objects, font)
		}
		if n == 16 {
			rendered(t, resourceDocument(names.String(), "", objects...))
		} else {
			refusal(t, resourceDocument(names.String(), "", objects...), FontLimit)
		}
	}
	for _, n := range []int{64, 65} {
		var names strings.Builder
		for i := 0; i < n; i++ {
			fmt.Fprintf(&names, "/Alias%d 4 0 R ", i)
		}
		if n == 64 {
			rendered(t, resourceDocument(names.String(), "", font))
		} else {
			refusal(t, resourceDocument(names.String(), "", font), ContainerLimit)
		}
		img := stream([]byte{255}, "/Type /XObject /Subtype /Image /Width 1 /Height 1 /BitsPerComponent 8 /ColorSpace /DeviceGray")
		if n == 64 {
			rendered(t, resourceDocument("", names.String(), img))
		} else {
			refusal(t, resourceDocument("", names.String(), img), ContainerLimit)
		}
	}
	for _, n := range []int{256, 257} {
		var procs strings.Builder
		for i := 0; i < n; i++ {
			fmt.Fprintf(&procs, "/G%d 5 0 R ", i)
		}
		f := strings.Replace(font, "/CharProcs << >>", "/CharProcs << "+procs.String()+" >>", 1)
		if n == 256 {
			rendered(t, resourceDocument("/F 4 0 R", "", f, stream([]byte("0 0 0 0 0 0 d1"), "")))
		} else {
			// Dictionary capacity is checked before the role glyph ceiling.
			refusal(t, resourceDocument("/F 4 0 R", "", f, stream([]byte("0 0 0 0 0 0 d1"), "")), ContainerLimit)
		}
	}
}
func TestWholeDocumentAndInheritedProperties(t *testing.T) {
	refusal(t, document("<< /Type /Catalog /Pages 2 0 R >>", "<< /Type /Pages /Kids [3 0 R 4 0 R] /Count 2 /MediaBox [0 0 48 48] >>",
		"<< /Type /Page /Parent 2 0 R >>", "<< /Type /Page /Parent 2 0 R /Contents 5 0 R >>", stream([]byte("S"), "")), UnsupportedStroke)
	refusal(t, resourceDocument("/Unused 4 0 R", "", "<< /Type /Font /Subtype /Type1 >>"), UnsupportedFont)
	refusal(t, resourceDocument("", "/Unused 4 0 R", stream(nil, "/Type /XObject /Subtype /Image /Width 1 /Height 1 /BitsPerComponent 4 /ColorSpace /DeviceGray")), UnsupportedImage)
	src := document("<< /Type /Catalog /Pages 2 0 R >>", "<< /Type /Pages /Kids [3 0 R] /Count 1 /MediaBox [0 0 48 48] /CropBox [12 12 36 36] /Resources << /XObject << /Bad 4 0 R >> >> >>",
		"<< /Type /Page /Parent 2 0 R /Resources << >> >>", stream(nil, "/Type /XObject /Subtype /Form"))
	// Local Resources replaces, rather than merges. The displaced object
	// remains an orphan, not an invisible unvalidated resource.
	refusal(t, src, UnsupportedFeature)
	src = document("<< /Type /Catalog /Pages 2 0 R >>", "<< /Type /Pages /Kids [3 0 R] /Count 1 /MediaBox [0 0 48 48] /CropBox [12 12 36 36] >>", "<< /Type /Page /Parent 2 0 R >>")
	p, _, _ := rendered(t, src)
	if p.Width != 32 || p.Height != 32 {
		t.Fatal(p.Width, p.Height)
	}
}
func TestMetadataAndIndirectDictionaries(t *testing.T) {
	for _, n := range []int{16384, 16385} {
		meta := "<< /Title (" + strings.Repeat("a", 4096) + ") /Author (" + strings.Repeat("b", 4096) + ") /Subject (" + strings.Repeat("c", 4096) + ") /Keywords (" + strings.Repeat("d", 4096) + ")"
		if n > 16384 {
			meta += " /Creator (x)"
		}
		meta += " >>"
		src := document("<< /Type /Catalog /Pages 2 0 R >>", "<< /Type /Pages /Kids [3 0 R] /Count 1 >>", "<< /Type /Page /Parent 2 0 R /MediaBox [0 0 .75 .75] >>", meta)
		src = bytes.Replace(src, []byte("/Root 1 0 R"), []byte("/Root 1 0 R /Info 4 0 R"), 1)
		if n == 16384 {
			rendered(t, src)
		} else {
			refusal(t, src, TokenLimit)
		}
	}
	src := document("<< /Type /Catalog /Pages 2 0 R >>", "<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 .75 .75] /Resources 4 0 R >>", "<< /Font 5 0 R >>", "<< >>")
	rendered(t, src)
	for _, n := range []int{29, 30} {
		objects := []string{"<< /Type /Catalog /Pages 2 0 R >>", "<< /Type /Pages /Kids [3 0 R] /Count 1 >>", "<< /Type /Page /Parent 2 0 R /MediaBox [0 0 .75 .75] /Resources 4 0 R >>"}
		for i := 0; i < n; i++ {
			v := fmt.Sprintf("%d 0 R", i+5)
			if i == n-1 {
				v = "<< >>"
			}
			objects = append(objects, v)
		}
		if n == 29 {
			rendered(t, document(objects...))
		} else {
			refusal(t, document(objects...), DepthLimit)
		}
	}
}
