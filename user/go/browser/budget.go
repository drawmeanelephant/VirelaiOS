package main

import (
	"unsafe"

	"virelai/webrender"
	"virelai/webstyle"
)

// The ledger is a conservative preflight, not a physical-memory receipt.
// Reserve the reusable native frame, four fonts/caches and runtime headroom;
// the guest's kernel receipt remains the acceptance measurement.
const pageBaseReserve = 8 * 1024 * 1024

// A decode may temporarily retain its output beside bounded inflater/vector
// scratch and a later layout decode. Check this headroom before the decoder
// allocates, not merely after an aggregate pixel check has already failed.
const imageDecodeReserve = 2*webstyle.MaxDecodedImageBytes + 1024*1024

func (a *app) canDecodeImage(sourceBytes int) bool {
	return int64(imageDecodeReserve)+int64(sourceBytes)*2 <= a.memoryRemaining
}

func (a *app) preflightPage(body []byte) bool {
	nodes, words, inWord := int64(4), int64(0), false
	for _, c := range body {
		if c == '<' {
			nodes += 2 // elements/text runs, including anonymous splits below
		}
		space := c == ' ' || c == '\t' || c == '\r' || c == '\n' || c == '\f'
		if !space && !inWord {
			words++
		}
		inWord = !space
	}
	nodes = min(nodes, webstyle.MaxNodes)
	words = min(words, webstyle.MaxPaintItems)
	perNode := int64(unsafe.Sizeof(webrender.Node{}) + unsafe.Sizeof(webstyle.ComputedStyle{}) +
		unsafe.Sizeof(webrender.Box{}) + unsafe.Sizeof(webrender.Item{}))
	estimate := int64(pageBaseReserve) + int64(len(body))*4 + nodes*perNode +
		words*int64(unsafe.Sizeof(webrender.Item{}))
	limit := int64(webstyle.MaxDemandPages * 4096)
	if estimate > limit {
		return false
	}
	a.memoryRemaining = limit - estimate
	return true
}

func (a *app) reserveCSS(source string) bool {
	// Every colon can begin a declaration and every opening brace a rule.
	// Account the largest admitted shorthand expansion before parsing it,
	// including tokenizer strings/rule/selector overhead. Over-counting
	// punctuation inside strings is safe and visible, never an OOM promise.
	cost := int64(len(source)) * 8
	for _, c := range source {
		switch c {
		case ':':
			cost += 12 * 96
		case '{':
			cost += 512
		}
	}
	if cost > a.memoryRemaining {
		a.diagnostic(webstyle.DiagnosticLimit, "page-memory-limit: stylesheet")
		return false
	}
	a.memoryRemaining -= cost
	return true
}
