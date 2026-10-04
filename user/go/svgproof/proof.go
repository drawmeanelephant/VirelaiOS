// Package svgproof adapts bytes to the public engine without a UI or DOM.
package svgproof

import (
	"unsafe"
	"virelai/svg"
	"virelai/vector"
)

const (
	ArenaBytes = 8 * 1024 * 1024
	SourceEnd  = 1024 * 1024
	OutputEnd  = SourceEnd + 4*1024*1024
	SceneEnd   = OutputEnd + 1024*1024
	ScratchEnd = SceneEnd + 512*1024
	StagingEnd = ScratchEnd + 512*1024
)

type Buffers struct {
	Source  []byte
	Pixels  []uint32
	Storage vector.Storage
	Scratch vector.Workspace
}

func View(a []byte) Buffers {
	commands := unsafe.Slice((*vector.Command)(unsafe.Pointer(&a[OutputEnd])), vector.MaxCommands)
	off := OutputEnd + len(commands)*int(unsafe.Sizeof(vector.Command{}))
	paints := unsafe.Slice((*vector.Paint)(unsafe.Pointer(&a[off])), vector.MaxPaints)
	return Buffers{a[:SourceEnd], unsafe.Slice((*uint32)(unsafe.Pointer(&a[SourceEnd])), (OutputEnd-SourceEnd)/4),
		vector.Storage{Commands: commands, Paints: paints}, vector.Workspace{Bytes: a[SceneEnd:ScratchEnd]}}
}

func (v Buffers) Render(n int, background uint32, b *vector.Budget) (vector.Scene, svg.Canvas, vector.Stats, vector.Failure) {
	if n < 0 || n > len(v.Source) {
		return vector.Scene{}, svg.Canvas{}, vector.Stats{}, vector.Failure{Code: vector.SourceLimit, Offset: 0, Paint: -1, Command: -1}
	}
	s, c, f := svg.Parse(v.Source[:n], v.Storage, b)
	if f.Code != vector.OK {
		return s, c, vector.Stats{}, f
	}
	size := c.Width * c.Height
	if b == nil || b.Used > b.Max || b.Max > vector.MaxWork || uint64(size) > b.Max-b.Used {
		return vector.Scene{}, svg.Canvas{}, vector.Stats{}, vector.Failure{Code: vector.WorkLimit, Offset: -1, Paint: -1, Command: -1}
	}
	// Admission precedes the caller clear. Parser failures never publish or
	// clear output. Raster preflight's unchanged-dst contract is tested
	// independently by vectorconsumer, with an existing sentinel target.
	b.Used += uint64(size)
	for i := 0; i < size; i++ {
		v.Pixels[i] = background
	}
	stats, f := vector.Rasterize(s, vector.Target{Pix: v.Pixels, Width: c.Width, Height: c.Height, Stride: c.Width}, v.Scratch, b)
	return s, c, stats, f
}
