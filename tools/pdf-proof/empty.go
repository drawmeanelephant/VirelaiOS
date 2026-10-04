//go:build virelai

package main

import (
	"unsafe"
	"virelai/pdf"
)

// The size baseline keeps exactly the same source/hash/writer/receipt/setup
// driver. Only parsing/decoding/vector rendering is removed.
func render(_ pdf.Source, _ int, arena []byte, ledger *pdf.Ledger) (pdf.Page, pdf.Stats, pdf.Failure) {
	p := unsafe.Slice((*uint32)(unsafe.Pointer(&arena[0])), 64*64)
	for i := range p {
		p[i] = 0xffffffff
	}
	return pdf.Page{Pix: p, Width: 64, Height: 64, Stride: 64}, pdf.Stats{ArenaBytes: pdf.ArenaBytes}, pdf.Failure{}
}
func codeName(c pdf.Code) string { return (pdf.Failure{Code: c}).Error() }
