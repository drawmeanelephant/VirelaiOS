//go:build !virelai

// Host replay uses the same public calls, but is not the independent oracle.
package main

import (
	"encoding/binary"
	"fmt"
	"os"
	"path/filepath"
	"unsafe"
	"virelai/svgproof"
	"virelai/vector"
	"virelai/vectorconsumer"
)

func main() {
	if len(os.Args) != 3 {
		panic("host replay requires corpus and output directories")
	}
	if err := os.MkdirAll(os.Args[2], 0755); err != nil {
		panic(err)
	}
	a := make([]byte, svgproof.ArenaBytes)
	v := svgproof.View(a)
	files, err := filepath.Glob(filepath.Join(os.Args[1], "*.svg"))
	if err != nil {
		panic(err)
	}
	for _, path := range files {
		name := filepath.Base(path)
		if name == "consumer-curves.svg" {
			continue
		}
		src, err := os.ReadFile(path)
		if err != nil {
			panic(err)
		}
		if len(src) > len(v.Source) {
			continue // large negatives are exercised by host tests and guest
		}
		copy(v.Source, src)
		b := vector.Budget{Max: vector.MaxWork}
		_, c, _, f := v.Render(len(src), 0, &b)
		if f.Code != vector.OK {
			panic(fmt.Sprintf("%s: %+v", name, f))
		}
		write(filepath.Join(os.Args[2], name[:len(name)-4]+".bgra"), v.Pixels[:c.Width*c.Height])
	}
	c := make([]vector.Command, vector.MaxCommands+1)
	p := make([]vector.Paint, vector.MaxPaints+1)
	pix := unsafe.Slice((*uint32)(unsafe.Pointer(&a[0])), 1024*1536)
	ws := vector.Workspace{Bytes: a[7*1024*1024 : 7*1024*1024+vector.WorkspaceSize]}
	for _, id := range vectorconsumer.Names {
		s, w, h, stride, bg := vectorconsumer.Build(id, vector.Storage{Commands: c, Paints: p})
		for i := range pix[:stride*h] {
			pix[i] = bg
		}
		dst := vector.Target{Pix: pix[:stride*h], Width: w, Height: h, Stride: stride}
		b := vector.Budget{Max: vector.MaxWork, Used: uint64(stride * h)}
		_, f := vector.Rasterize(s, dst, ws, &b)
		if f.Code != vector.OK || !vectorconsumer.Check(id, dst) {
			panic(fmt.Sprintf("consumer %s: %+v analytic mismatch", id, f))
		}
		write(filepath.Join(os.Args[2], "consumer-"+id+".bgra"), dst.Pix)
	}
}

func write(path string, pix []uint32) {
	bytes := make([]byte, len(pix)*4)
	for i, p := range pix {
		binary.LittleEndian.PutUint32(bytes[i*4:], p)
	}
	if err := os.WriteFile(path, bytes, 0644); err != nil {
		panic(err)
	}
}
