//go:build virelai

package main

import (
	"virelai/pdf"
	"virelai/vi"
)

type embeddedSource struct{}

func (*embeddedSource) Length() int64 { return int64(len(fixture)) }
func (*embeddedSource) ReadAt(dst []byte, offset int64, l *pdf.Ledger) (int, pdf.Code) {
	if offset < 0 || offset > int64(len(fixture)) {
		return 0, pdf.ReadFailed
	}
	n := min(len(dst), len(fixture)-int(offset))
	if c := l.Read(uint64(n)); c != pdf.OK {
		return 0, c
	}
	copy(dst[:n], fixture[offset:int(offset)+n])
	return n, pdf.OK
}
func now() uint64 { return uint64(vi.Nanos()) }
func engine(a []byte) (int, int, uint8) {
	s := embeddedSource{}
	l := pdf.Ledger{Max: pdf.MaxWork, Now: now, Deadline: now() + pdf.MaxNanos}
	p, _, f := pdf.Render(&s, 0, a, &l)
	return p.Width, p.Height, uint8(f.Code)
}
