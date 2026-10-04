//go:build virelai

package main

import (
	"runtime"
	"unsafe"
	"virelai/vector"
	"virelai/vectorconsumer"
	"virelai/vi"
)

func run(a []byte, mode string) bool {
	if mode != "consumer" {
		return false
	}
	// No SVG source. Packed maximum page occupies the first 6 MiB, scene
	// the next 1 MiB, scratch 512 KiB, staging/alignment the last 512 KiB.
	pix := unsafe.Slice((*uint32)(unsafe.Pointer(&a[0])), 1024*1536+16)
	commands := unsafe.Slice((*vector.Command)(unsafe.Pointer(&a[6*1024*1024+4096])), vector.MaxCommands+1)
	off := 6*1024*1024 + 4096 + len(commands)*int(unsafe.Sizeof(vector.Command{}))
	paints := unsafe.Slice((*vector.Paint)(unsafe.Pointer(&a[off])), vector.MaxPaints+1)
	out := vector.Storage{Commands: commands, Paints: paints}
	ws := vector.Workspace{Bytes: a[7*1024*1024 : 7*1024*1024+vector.WorkspaceSize]}
	stage := a[15*512*1024 : 15*512*1024+2048]
	r := receipt{buf: a[15*512*1024+2048:]}
	r.text("schema\t1\narena\t8388608\t2048\t1\n")
	var before, after runtime.MemStats
	for _, id := range vectorconsumer.Names {
		s, w, h, stride, bg := vectorconsumer.Build(id, out)
		// Retained POD snapshots prove Commands/Paints are immutable without
		// importing engine-private helpers into the independent consumer.
		originalC := [16]vector.Command{}
		originalP := [2]vector.Paint{}
		copy(originalC[:], s.Commands)
		copy(originalP[:], s.Paints)
		if len(s.Commands) > len(originalC) || len(s.Paints) > len(originalP) {
			return false
		}
		for i := range pix {
			pix[i] = bg
		}
		dst := vector.Target{Pix: pix[:stride*h], Width: w, Height: h, Stride: stride}
		b := vector.Budget{Max: vector.MaxWork}
		runtime.ReadMemStats(&before)
		start := vi.Nanos()
		b.Used += uint64(stride * h)
		for i := range dst.Pix {
			dst.Pix[i] = bg
		}
		stats, f := vector.Rasterize(s, dst, ws, &b)
		elapsed := vi.Nanos() - start
		runtime.ReadMemStats(&after)
		if f.Code != vector.OK || !vectorconsumer.Check(id, dst) || elapsed < 0 || elapsed > 5_000_000_000 || after.Mallocs != before.Mallocs {
			return false
		}
		for i, c := range s.Commands {
			if c != originalC[i] {
				return false
			}
		}
		for i, p := range s.Paints {
			if p != originalP[i] {
				return false
			}
		}
		for _, p := range pix[stride*h:] {
			if p != bg {
				return false
			}
		}
		r.text("render\t")
		r.text("consumer-" + id)
		r.field(0)
		r.field(uint64(elapsed))
		r.field(b.Used)
		r.field(stats.Work)
		r.field(stats.Edges)
		r.field(stats.ScratchBytes)
		r.field(after.Mallocs - before.Mallocs)
		r.field(uint64(w))
		r.field(uint64(h))
		r.text("\n")
		if !write("consumer-"+id+".bgra", a[:stride*h*4], stage) {
			return false
		}
	}
	dst := vector.Target{Pix: pix[:4096], Width: 64, Height: 64, Stride: 64}
	runtime.ReadMemStats(&before)
	if !vectorconsumer.Failures(out, dst, ws) {
		return false
	}
	runtime.ReadMemStats(&after)
	if after.Mallocs != before.Mallocs {
		return false
	}
	r.text("direct-errors\t1\t0\n")
	return write("consumer.tsv", r.buf[:r.n], stage)
}
