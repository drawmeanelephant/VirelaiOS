package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"testing"

	"virelai/ttf"
	"virelai/webrender"
	"virelai/webstyle"
)

func TestPresentationGeometryAndInverse(t *testing.T) {
	p := documentPresentation(512, 384)
	if p != (presentation{0, 64, 512, 288}) {
		t.Fatal(p)
	}
	for _, size := range [][2]int{{512, 384}, {1280, 720}, {800, 600}, {128, 64}, {0, 0}} {
		p := documentPresentation(size[0], size[1])
		if p.W > 1280 || p.H > 720 {
			t.Fatal("upscale")
		}
		if p.W > 0 && p.H > 0 {
			x, y, ok := p.inverse(p.X+p.W-1, p.Y+p.H-1, 40)
			if !ok || x >= 1280 || y >= 760 {
				t.Fatal("invalid inverse")
			}
			if _, _, ok := p.inverse(p.X-1, p.Y, 0); ok {
				t.Fatal("letterbox hit")
			}
		}
	}
}

func TestSampleFrameAreaFiltersLogicalPage(t *testing.T) {
	a := &app{nativePix: make([]uint32, webstyle.ViewportWidth*webstyle.ViewportHeight)}
	for y := 0; y < webstyle.ViewportHeight; y++ {
		for x := 0; x < webstyle.ViewportWidth; x++ {
			if x%2 == 0 {
				a.nativePix[y*webstyle.ViewportWidth+x] = 0xffffff
			}
		}
	}
	pix := make([]uint32, 512*288)
	a.sampleFrame(virender{pix: pix, w: 512, h: 288}, presentation{0, 0, 512, 288})
	want := []uint32{153, 153, 102, 102, 153, 153, 102, 102}
	for x, channel := range want {
		got := pix[x]
		for shift := uint(0); shift <= 16; shift += 8 {
			if v := got >> shift & 0xff; v != channel {
				t.Fatalf("presentation pixel x=%d shift=%d = %02x, want area-filtered stripe mean %02x (frame word %08x)", x, shift, v, channel, got)
			}
		}
	}
}

func TestInvalidResizeCannotReenableTheFallbackCanvas(t *testing.T) {
	a := &app{}
	if w, h := a.canvasSize(); w != winW || h != winH {
		t.Fatal("unconfigured fallback changed")
	}
	for _, size := range [][2]int{{0, 0}, {-1, 384}, {1281, 720}, {512, 64}} {
		a.bindCanvas(size[0], size[1])
		w, h := a.canvasSize()
		if documentPresentation(w, h).W != 0 || a.status != "viewport-too-small" {
			t.Fatalf("invalid resize revived fallback: %v -> %dx%d %q", size, w, h, a.status)
		}
	}
	a.bindCanvas(winW, winH)
	if a.status != "" {
		t.Fatal("valid resize did not recover")
	}
}

func TestVirenderMaskBlendsInsteadOfThresholding(t *testing.T) {
	pix := []uint32{0, 0, 0}
	v := virender{pix: pix, w: 3, h: 1}
	v.BlitMask(0, 0, &ttf.Mask{Width: 3, Height: 1, Alpha: []byte{0, 127, 255}}, 0xffffff)
	// a=127 is half coverage: in linear light that is ~0xBC, far above the
	// 0x7F an sRGB-space lerp produced — the M99o difference, pinned.
	if pix[0]&0xffffff != 0 || pix[1]&0xffffff != 0xbbbbbb || pix[2]&0xffffff != 0xffffff {
		t.Fatalf("coverage=%x", pix)
	}
}

func frameHash(pix []uint32) string {
	h := sha256.New()
	var rgb [3]byte
	for _, p := range pix {
		rgb = [3]byte{byte(p >> 16), byte(p >> 8), byte(p)}
		_, _ = h.Write(rgb[:])
	}
	return hex.EncodeToString(h.Sum(nil))
}

// The webrender manifest owns native paint truth. Browser presentation is a
// separate owner now that sampleFrame resamples, so its exact hashes live in
// this package's fixture rather than in webrender's standalone reference pair.
func TestIntegratedPipelineMatchesApprovedReferencePixels(t *testing.T) {
	data, err := os.ReadFile("../webrender/testdata/golden/m93-reference.json")
	if err != nil {
		t.Fatal(err)
	}
	var manifest struct {
		Images map[string]struct{ Native string }
	}
	if err := json.Unmarshal(data, &manifest); err != nil {
		t.Fatal(err)
	}
	presentationData, err := os.ReadFile("testdata/reference-presentation.json")
	if err != nil {
		t.Fatal(err)
	}
	type presentationReferenceManifest struct {
		Version       int
		Width, Height int
		Verdict       string
		Fixtures      map[string]struct{ Hash string }
	}
	var presentationManifest presentationReferenceManifest
	if err := json.Unmarshal(presentationData, &presentationManifest); err != nil {
		t.Fatal(err)
	}
	if presentationManifest.Version != 1 || presentationManifest.Width != 512 || presentationManifest.Height != 288 ||
		presentationManifest.Verdict != "Approve footprint-filtered reference presentation expectations" {
		t.Fatal("missing filtered reference presentation approval")
	}
	fonts := browserFonts(t)
	stage := os.Getenv("M93G_STAGE_REFERENCE_PRESENTATION")
	staged := presentationReferenceManifest{Version: 1, Width: 512, Height: 288,
		Verdict:  "Approve footprint-filtered reference presentation expectations",
		Fixtures: map[string]struct{ Hash string }{}}
	for name, expected := range manifest.Images {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join("../../../tests/fixtures/web/reference", name+".html")
			body, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			a := &app{hist: newHistory(), text: fonts, target: "/host/" + name + ".html"}
			a.loadBody(body, a.target)
			native := a.nativeSurface()
			native.Fill(0, 0, 1280, 720, webrender.ColorPageBg)
			webrender.Paint(a.lay, native, 0, 0, 1280, 720, 0)
			if got := frameHash(a.nativePix); got != expected.Native {
				t.Fatalf("native hash=%s want=%s", got, expected.Native)
			}
			sampled := make([]uint32, 512*288)
			a.sampleFrame(virender{pix: sampled, w: 512, h: 288}, presentation{0, 0, 512, 288})
			if stage != "" {
				if err := os.MkdirAll(stage, 0o755); err != nil {
					t.Fatal(err)
				}
				im := image.NewNRGBA(image.Rect(0, 0, 512, 288))
				for i, p := range sampled {
					im.Pix[i*4], im.Pix[i*4+1], im.Pix[i*4+2], im.Pix[i*4+3] = byte(p>>16), byte(p>>8), byte(p), 255
				}
				var out bytes.Buffer
				if err := png.Encode(&out, im); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(stage, name+".png"), out.Bytes(), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			got := frameHash(sampled)
			if stage != "" {
				staged.Fixtures[name] = struct{ Hash string }{Hash: got}
			} else {
				wantPresentation, ok := presentationManifest.Fixtures[name]
				if !ok {
					t.Fatalf("missing filtered presentation expectation for %s", name)
				}
				if got != wantPresentation.Hash {
					t.Fatalf("presentation hash=%s want=%s", got, wantPresentation.Hash)
				}
			}
			if len(a.nativePix)*4 != webstyle.ViewportWidth*webstyle.ViewportHeight*4 {
				t.Fatal("native frame geometry changed")
			}
		})
	}
	if stage != "" {
		data, _ := json.MarshalIndent(staged, "", "  ")
		if err := os.WriteFile(filepath.Join(stage, "reference-presentation.json"), append(data, '\n'), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}
