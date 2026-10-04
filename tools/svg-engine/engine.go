//go:build virelai && svgfull

package main

import (
	"unsafe"
	"virelai/svg"
	"virelai/vector"
)

func engine(a []byte, n int) (int, int, uint8) {
	// These mapped arrays contain only POD. Headers remain on the Go stack.
	commands := unsafe.Slice((*vector.Command)(unsafe.Pointer(&a[outputEnd])), vector.MaxCommands)
	paintOffset := outputEnd + vector.MaxCommands*int(unsafe.Sizeof(vector.Command{}))
	paints := unsafe.Slice((*vector.Paint)(unsafe.Pointer(&a[paintOffset])), vector.MaxPaints)
	b := vector.Budget{Max: vector.MaxWork}
	s, c, f := svg.Parse(a[:n], vector.Storage{Commands: commands, Paints: paints}, &b)
	if f.Code != vector.OK {
		return 0, 0, uint8(f.Code)
	}
	pix := words(a[sourceEnd:outputEnd])
	if code := clearTarget(pix, c.Width*c.Height, &b); code != vector.OK {
		return 0, 0, uint8(code)
	}
	_, f = vector.Rasterize(s, vector.Target{Pix: pix, Width: c.Width, Height: c.Height, Stride: c.Width},
		vector.Workspace{Bytes: a[sceneEnd:scratchEnd]}, &b)
	return c.Width, c.Height, uint8(f.Code)
}

func clearTarget(pix []uint32, n int, b *vector.Budget) vector.Code {
	if n < 0 || n > len(pix) {
		return vector.InvalidBuffer
	}
	// §5 charges the caller's clear once per destination pixel, before write.
	if b == nil || b.Used > b.Max || b.Max > vector.MaxWork || uint64(n) > b.Max-b.Used {
		return vector.WorkLimit
	}
	for i := 0; i < n; i++ {
		b.Used++
		pix[i] = 0
	}
	return vector.OK
}
