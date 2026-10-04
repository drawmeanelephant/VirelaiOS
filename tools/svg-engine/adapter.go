//go:build virelai

package main

import (
	"runtime"
	"unsafe"
	"virelai/vi"
)

const (
	arenaBytes = 8 * 1024 * 1024
	sourceEnd  = 1024 * 1024
	outputEnd  = sourceEnd + 4*1024*1024
	sceneEnd   = outputEnd + 1024*1024
	scratchEnd = sceneEnd + 512*1024
	stagingEnd = scratchEnd + 512*1024
)

// Empty and full adapters share this transport, mapping, output writer and
// fixed capacities. Only engine.go changes, selected by the svgfull tag.
func main() {
	a, err := vi.MmapAnon(arenaBytes)
	if err != nil {
		vi.Console("svg-engine: arena refused\n")
		vi.Exit(1)
	}
	args := vi.Args()
	if len(args) != 3 {
		vi.Console("svg-engine: requires input and output names\n")
		vi.Exit(1)
	}
	stage := a[scratchEnd:stagingEnd]
	h, errno := vi.FileOpen(args[1], vi.ModeRead)
	if errno < 0 {
		vi.Exit(1)
	}
	n := 0
	for {
		count, errno := vi.FileRead(uint32(h), stage[:2048])
		if errno < 0 || count < 0 || count > 2048 || n+count > sourceEnd {
			vi.FileClose(uint32(h))
			vi.Console("svg-engine: source refused\n")
			vi.Exit(1)
		}
		if count == 0 {
			break
		}
		copy(a[n:n+count], stage[:count])
		n += count
	}
	vi.FileClose(uint32(h))
	// All setup/source transport precedes render admission. This source
	// buffer is never copied into a Go string.
	w, height, code := engine(a, n)
	if code != 0 {
		vi.Console("svg-engine: render refused\n")
		vi.Exit(1)
	}
	h, errno = vi.FileOpen(args[2], vi.ModeWrite|vi.ModeCreate)
	if errno < 0 {
		vi.Exit(1)
	}
	if vi.FileTruncate(uint32(h), 0) < 0 {
		vi.FileClose(uint32(h))
		vi.Exit(1)
	}
	// Raw little-endian, top-down straight-alpha BGRA; no image codec.
	bytes := a[sourceEnd : sourceEnd+w*height*4]
	for len(bytes) > 0 {
		chunk := min(len(bytes), 2048)
		copy(stage[:chunk], bytes[:chunk])
		count, errno := vi.FileWrite(uint32(h), stage[:chunk])
		if errno < 0 || count <= 0 || count > chunk {
			vi.FileClose(uint32(h))
			vi.Exit(1)
		}
		bytes = bytes[count:]
	}
	vi.FileClose(uint32(h))
	runtime.KeepAlive(a)
	vi.Console("svg-engine: output closed\n")
}

func words(a []byte) []uint32 {
	return unsafe.Slice((*uint32)(unsafe.Pointer(&a[0])), len(a)/4)
}
