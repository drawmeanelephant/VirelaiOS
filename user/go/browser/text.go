package main

import (
	"virelai/vi"
	"virelai/webrender"
	"virelai/webstyle"
)

// Text plumbing for the browser: load the faces the share stages, pick a text
// engine, and republish what was chosen on the serial line so a gate can assert
// the page is not silently back on the 8x8 grid.

// Share names frozen by M69d #1531 D1. Zig ui.init_fonts (#1536) consumes the
// same paths; this card only reads them.
const (
	fontUIPath     = "/host/INTER.TTF"
	fontBoldPath   = "/host/INTERB.TTF"
	fontItalicPath = "/host/INTERI.TTF"
	fontMonoPath   = "/host/FIRACODE.TTF"
)

// maxFontBytes bounds one face read. Inter Regular is 411,640 bytes, Bold
// 420,428, Italic 417,388, Fira Code 289,624 — 512 KiB holds each with room
// for a slightly larger licensed build. It is deliberately NOT vi.MaxFileBytes
// (256 KiB): that cap is a policy for the browser's PAGE loads, and reusing it
// here silently truncated Inter in half and dropped the renderer back onto the
// bitmap. A face past this bound fails to parse and the renderer falls back —
// visible, never blank.
const maxFontBytes = 512 * 1024

// maxImageBytes bounds one <img> read from the share.
const maxImageBytes = webstyle.MaxImageBytes

// readWholeFile reads up to max bytes of a share file. It is the browser's own
// reader rather than vi.ReadFileAll because that helper clamps every request to
// vi.MaxFileBytes (see maxFontBytes).
func readWholeFile(path string, max int) []byte {
	h, rc := vi.FileOpen(path, vi.ModeRead)
	if rc < 0 || h < 0 {
		return nil
	}
	defer vi.FileClose(uint32(h))
	out := make([]byte, 0, 64*1024)
	buf := make([]byte, 16*1024)
	for len(out) < max {
		n, rr := vi.FileRead(uint32(h), buf)
		if rr < 0 || n <= 0 {
			break
		}
		take := n
		if len(out)+take > max {
			take = max - len(out)
		}
		out = append(out, buf[:take]...)
		if take < n {
			break
		}
	}
	return out
}

// loadTextEngine loads the staged faces and returns the engine to render with.
// Any failure (a missing face, a short read, a rejected font) simply leaves
// that face out, which is how the renderer reaches its documented fallback.
func loadTextEngine() (webrender.Fonts, string, string) {
	ui := readWholeFile(fontUIPath, maxFontBytes)
	mono := readWholeFile(fontMonoPath, maxFontBytes)
	bold := readWholeFile(fontBoldPath, maxFontBytes)
	italic := readWholeFile(fontItalicPath, maxFontBytes)
	f := webrender.LoadFonts(webrender.FontFiles{UI: ui, Mono: mono, Bold: bold, Italic: italic})
	uiState := "truetype"
	if f.UI == nil {
		uiState = "missing"
	}
	monoState := "truetype"
	if f.Mono == nil {
		monoState = "missing"
	}
	return f, uiState, monoState
}

// textProbeString is the Inter-or-grid probe, published on the serial line.
//
// It is deliberately made of FACTS a gate can assert rather than adjectives:
// the engine's name, whether advances are per-glyph, the line heights the layout
// actually uses, and three measured advances. On the 8x8 fallback every advance
// is 8 and the h1 line box is 18px; on Inter at 13px the advances differ per
// glyph and the h1 line box is 33px. Those numbers cannot both be true, so a
// silent regression back to the grid fails the gate instead of merely looking
// slightly off in a screenshot.
func textProbeString(t webrender.TextEngine) string {
	if t == nil {
		t = webrender.Bitmap{}
	}
	body := webrender.Style{Size: 1, Color: webrender.ColorText}
	h1 := webrender.Style{Size: 2, Color: webrender.ColorText}
	mono := webrender.Style{Size: 1, Mono: true}
	boldFace, boldHeavier, italicFace := "no", "no", "no"
	if f, ok := t.(webrender.Fonts); ok {
		if f.Bold != nil {
			boldFace = "yes"
			if f.BoldHeavier() {
				boldHeavier = "yes"
			}
		}
		if f.Italic != nil {
			italicFace = "yes"
		}
	}
	return "face=" + t.Name() +
		" proportional=" + boolStr(t.Proportional()) +
		" body-lineh=" + itoa(t.LineHeight(body)) +
		" h1-lineh=" + itoa(t.LineHeight(h1)) +
		" mono-lineh=" + itoa(t.LineHeight(mono)) +
		" adv-i=" + itoa(t.Measure("i", body)) +
		" adv-W=" + itoa(t.Measure("W", body)) +
		" adv-space=" + itoa(t.Measure(" ", body)) +
		" bold-face=" + boldFace +
		" bold-heavier=" + boldHeavier +
		" italic-face=" + italicFace
}

// resolveImage returns only preflighted sources during layout. Resource reads
// and decoded-pixel accounting happen before building boxes.
func (a *app) resolveImage(src string) ([]byte, bool) {
	if src == "" {
		return nil, false
	}
	if a.imageSources != nil {
		data := a.imageSources[src]
		return data, len(data) > 0
	}
	// Host-test/direct callers retain a bounded resource refusal path.
	target := relativeTo(a.target, src)
	resolved, kind := resolveInput(target)
	if kind != "file" {
		return nil, false
	}
	data := readWholeFile(resolved, maxImageBytes+1)
	return data, len(data) > 0 && len(data) <= maxImageBytes
}
