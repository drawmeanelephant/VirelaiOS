//go:build virelai

package main

// Same SDK/transport/output/mapping as the full adapter, omitting only calls
// and packages of the engine. This is a size baseline, never a gate renderer.
func run(a []byte, mode string) bool {
	stage := a[6*1024*1024+512*1024 : 6*1024*1024+512*1024+2048]
	_, ok := read(mode+".svg", a[:1024*1024], stage)
	return ok && write(mode+".bgra", a[1024*1024:1024*1024+16384], stage)
}
