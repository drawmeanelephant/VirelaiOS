//go:build virelai

// This size adapter uses a small immutable, compiled-in authored vector.
// Native PDF source transport, admission/recheck and receipts are M89c's job.
// Full/empty share the same source, 9 MiB populated arena and native writer.
package main

import (
	"runtime"
	"unsafe"
	"virelai/vi"
)

const arenaBytes = 9_437_184

var fixture = "%PDF-1.4\n%\xe2\xe3\xcf\xd3\n1 0 obj\n<< /Type /Catalog /Pages 2 0 R >>\nendobj\n2 0 obj\n<< /Type /Pages /Kids [3 0 R] /Count 1 >>\nendobj\n3 0 obj\n<< /Type /Page /Parent 2 0 R /MediaBox [0 0 48 48] /Contents 4 0 R  >>\nendobj\n4 0 obj\n<< /Length 0  >>\nstream\n\nendstream\nendobj\nxref\n0 5\n0000000000 65535 f \n0000000015 00000 n \n0000000064 00000 n \n0000000121 00000 n \n0000000207 00000 n \ntrailer\n<< /Size 5 /Root 1 0 R >>\nstartxref\n257\n%%EOF\n"

func main() {
	a, err := vi.MmapAnon(arenaBytes)
	if err != nil {
		vi.Console("pdf-engine: MemoryLimit\n")
		vi.Exit(1)
		return
	}
	w, h, code := engine(a)
	if code != 0 {
		vi.Console("pdf-engine: render refused\n")
		vi.Exit(1)
		return
	}
	args := vi.Args()
	if len(args) != 2 {
		vi.Console("pdf-engine: output name required\n")
		vi.Exit(1)
		return
	}
	if existing, r := vi.FileOpen(args[1], vi.ModeRead); r >= 0 {
		vi.FileClose(uint32(existing))
		vi.Console("pdf-engine: OutputExists\n")
		vi.Exit(1)
		return
	}
	f, r := vi.FileOpen(args[1], vi.ModeWrite|vi.ModeCreate)
	if r < 0 {
		vi.Exit(1)
		return
	}
	// Serialization has no second page. Header/staging use the state's I/O
	// allowance after Render's borrowed page has been finalized.
	stage := a[arenaBytes-4096 : arenaBytes-2048]
	stage[0] = 'P'
	stage[1] = 'D'
	stage[2] = 'F'
	stage[3] = '1'
	put(stage[4:8], uint32(w))
	put(stage[8:12], uint32(h))
	put(stage[12:16], uint32(w))
	ok := write(uint32(f), stage[:16])
	bytes := a[:w*h*4]
	for ok && len(bytes) > 0 {
		n := min(2048, len(bytes))
		copy(stage[:n], bytes[:n])
		ok = write(uint32(f), stage[:n])
		bytes = bytes[n:]
	}
	if ok {
		ok = vi.FileSync(uint32(f)) >= 0
	}
	vi.FileClose(uint32(f))
	runtime.KeepAlive(a)
	if !ok {
		vi.Console("pdf-engine: WriteFailed\n")
		vi.Exit(1)
		return
	}
	vi.Console("pdf-engine: complete\n")
}
func write(h uint32, b []byte) bool {
	for len(b) > 0 {
		n, r := vi.FileWrite(h, b)
		if r < 0 || n <= 0 || n > len(b) {
			return false
		}
		b = b[n:]
	}
	return true
}
func put(b []byte, n uint32) {
	for i := 0; i < 4; i++ {
		b[i] = byte(n >> uint(i*8))
	}
}
func words(a []byte) []uint32 { return unsafe.Slice((*uint32)(unsafe.Pointer(&a[0])), len(a)/4) }
