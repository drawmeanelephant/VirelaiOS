//go:build virelai

package main

import (
	"runtime"
	"virelai/vi"
)

const arenaBytes = 8 * 1024 * 1024

func main() {
	args := vi.Args()
	if len(args) != 2 {
		vi.Console("svg-proof: argv admission failed\n")
		fail()
	}
	a, err := vi.MmapAnon(arenaBytes)
	if err != nil && err.Error() == "EINVAL" {
		// The kernel's unhinted cursor can overlap the runtime's large sbrk
		// reservation. Use the existing SDK's checked hint, never forge a
		// pointer or enlarge a cap. Still one retained populated private
		// arena; an overlapping hint is refused by the kernel.
		vi.Console("svg-proof: unhinted arena refused; checked hint\n")
		a, err = vi.MmapHint(0x80000000, arenaBytes, vi.ProtRead|vi.ProtWrite,
			vi.MapAnonymous|vi.MapPrivate|vi.MapPopulate)
	}
	if err != nil {
		vi.Console("svg-proof: arena admission failed\n")
		vi.Console("svg-proof: arena errno=")
		vi.Console(err.Error())
		vi.Console("\n")
		fail()
	}
	vi.Console("svg-proof: arena admitted\n")
	// All transport state occupies the charged staging partition. One
	// renderer mapping per process, never one per call.
	ok := run(a, args[1])
	runtime.KeepAlive(a)
	if !ok {
		vi.Console("svg-proof: suite failed\n")
		fail()
	}
	vi.Console("svg-proof: outputs closed\n")
	vi.Exit(0)
}

func fail() {
	vi.Console("svg-proof: FAIL\n")
	vi.Exit(1)
}

func read(name string, dst, stage []byte) (int, bool) {
	h, e := vi.FileOpen("/host/"+name, vi.ModeRead)
	if e < 0 {
		return 0, false
	}
	n := 0
	for {
		count, e := vi.FileRead(uint32(h), stage[:2048])
		if e < 0 || count < 0 || count > 2048 {
			vi.FileClose(uint32(h))
			return n, false
		}
		if count == 0 {
			break
		}
		if count > len(dst)-n {
			vi.FileClose(uint32(h))
			// Return cap+1, distinguishing source admission from I/O failure.
			return len(dst) + 1, true
		}
		copy(dst[n:], stage[:count])
		n += count
	}
	vi.FileClose(uint32(h))
	return n, true
}

func write(name string, data, stage []byte) bool {
	h, e := vi.FileOpen("/host/"+name, vi.ModeWrite|vi.ModeCreate)
	if e < 0 {
		return false
	}
	if vi.FileTruncate(uint32(h), 0) < 0 {
		vi.FileClose(uint32(h))
		return false
	}
	for len(data) > 0 {
		count := min(len(data), 2048)
		copy(stage[:count], data[:count])
		n, e := vi.FileWrite(uint32(h), stage[:count])
		if e < 0 || n <= 0 || n > count {
			vi.FileClose(uint32(h))
			return false
		}
		data = data[n:]
	}
	if vi.FileSync(uint32(h)) < 0 {
		vi.FileClose(uint32(h))
		return false
	}
	vi.FileClose(uint32(h))
	return true
}

type receipt struct {
	buf []byte
	n   int
}

func (r *receipt) text(s string) {
	if len(s) > len(r.buf)-r.n {
		fail()
	}
	r.n += copy(r.buf[r.n:], s)
}
func (r *receipt) number(n uint64) {
	var digits [20]byte
	i := len(digits)
	for {
		i--
		digits[i] = byte(n%10) + '0'
		n /= 10
		if n == 0 {
			break
		}
	}
	if len(digits)-i > len(r.buf)-r.n {
		fail()
	}
	r.n += copy(r.buf[r.n:], digits[i:])
}
func (r *receipt) field(n uint64) { r.text("\t"); r.number(n) }
func hash(bytes []byte) uint64 {
	n := uint64(14695981039346656037)
	for _, b := range bytes {
		n = (n ^ uint64(b)) * 1099511628211
	}
	return n
}
