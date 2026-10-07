package main

import (
	"bytes"
	"os"
	"testing"
)

func TestHermeticStandInPointerGeometry(t *testing.T) {
	body, err := os.ReadFile("../../../tests/fixtures/web/reference/wikipedia.html")
	if err != nil {
		t.Fatal(err)
	}
	body = bytes.Replace(body, []byte("</head>"), []byte(`<link rel="stylesheet" href="/m93.css"></head>`), 1)
	body = bytes.Replace(body, []byte("<body>"), []byte(`<body><p><a href="/m93-next">Internal page</a></p>`), 1)
	a := &app{hist: newHistory(), text: browserFonts(t), target: "https://en.wikipedia.org:24560/wiki/Harbor"}
	a.resourceForTest = func(string, int) ([]byte, string) { return []byte("p { color:#202122; }"), "" }
	a.loadBody(body, a.target)
	p := documentPresentation(512, 384)
	for _, c := range a.controls {
		x := winX + p.X + (c.rect.X+c.rect.W/2)*p.W/contentW
		y := winY + p.Y + (c.rect.Y+c.rect.H/2)*p.H/contentH
		t.Logf("control=%s native=%+v scanout-center=%d,%d", c.kind, c.rect, x, y)
		cx, cy, ok := p.inverse(x-winX, y-winY, 0)
		if !ok || !inRect(cx, cy, c.rect.X, c.rect.Y, c.rect.W, c.rect.H) {
			t.Fatal("control center did not map back into its source rect")
		}
	}
}
