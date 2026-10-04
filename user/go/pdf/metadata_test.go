package pdf

import (
	"bytes"
	"strings"
	"testing"
)

func TestDocumentWideIgnoredMetadataBound(t *testing.T) {
	for _, titleLast := range []int{4092, 4093} {
		font := "<< /Type /Font /Subtype /Type3 /FontBBox [0 0 0 0] /FontMatrix [1 0 0 1 0 0] /CharProcs << >> /Encoding << /Differences [] >> /FirstChar 0 /LastChar 0 /Widths [0] /Name /ABCD >>"
		info := "<< /Title (" + strings.Repeat("a", 4096) + ") /Author (" + strings.Repeat("b", 4096) + ") /Subject (" + strings.Repeat("c", 4096) + ") /Keywords (" + strings.Repeat("d", titleLast) + ") >>"
		src := resourceDocument("/One 4 0 R /Alias 4 0 R", "", font, info)
		src = bytes.Replace(src, []byte("/Root 1 0 R"), []byte("/Root 1 0 R /Info 5 0 R"), 1)
		if titleLast == 4092 {
			rendered(t, src)
		} else {
			refusal(t, src, TokenLimit)
		}
	}
	for code := OK; int(code) < len(messages); code++ {
		if len(fail(code).Error()) > 128 {
			t.Fatal(code, "unbounded diagnostic")
		}
	}
}
