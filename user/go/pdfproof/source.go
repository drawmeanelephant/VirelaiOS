// Package pdfproof owns native transport and publication, not PDF interpretation.
package pdfproof

import (
	"crypto/sha256"
	"hash"
	"virelai/pdf"
	"virelai/vi"
)

const chunk = 2048

type files struct {
	open  func(string, uint32) (int64, int64)
	read  func(uint32, []byte) (int, int64)
	write func(uint32, []byte) (int, int64)
	sync  func(uint32) int64
	close func(uint32)
}

var native = files{vi.FileOpen, vi.FileRead, vi.FileWrite, vi.FileSync, vi.FileClose}

// Source has no cache. Render owns the only 64 KiB window and supplies dst.
// scratch must be caller-owned, disjoint from Render's partitions, and 2048 B.
// The hash state and this Go header are runtime headroom, never mapped POD.
type Source struct {
	fs       files
	path     string
	scratch  []byte
	digest   hash.Hash
	size     int64
	sum      [32]byte
	checked  [32]byte
	position int64
	handle   uint32
	opened   bool
	admitted bool
}

func NewSource(path string, scratch []byte) Source {
	return Source{fs: native, path: path, scratch: scratch, digest: sha256.New()}
}

func (s *Source) Length() int64 {
	if !s.admitted {
		return -1
	}
	return s.size
}

func (s *Source) Hash() [32]byte { return s.sum }

func (s *Source) SetPath(path string) pdf.Code {
	if s.opened {
		return pdf.InvalidBuffer
	}
	s.path, s.admitted = path, false
	return pdf.OK
}

func (s *Source) open(l *pdf.Ledger) pdf.Code {
	if s.opened {
		return pdf.InvalidBuffer
	}
	if c := l.Open(); c != pdf.OK {
		return c
	}
	h, rc := s.fs.open(s.path, vi.ModeRead)
	if rc < 0 {
		_ = l.Close()
		return pdf.ReadFailed
	}
	s.handle, s.position, s.opened = uint32(h), 0, true
	return pdf.OK
}

// Close always releases the native handle, even after an exhausted ledger.
func (s *Source) Close(l *pdf.Ledger) pdf.Code {
	if !s.opened {
		return pdf.OK
	}
	s.fs.close(s.handle)
	s.opened = false
	return l.Close()
}

func (s *Source) rewind(l *pdf.Ledger) pdf.Code {
	if c := s.Close(l); c != pdf.OK {
		return c
	}
	if c := l.Rewind(); c != pdf.OK {
		return c
	}
	return s.open(l)
}

func (s *Source) read(b []byte, l *pdf.Ledger) (int, pdf.Code) {
	// Reserve a conservative charge before native copying, including failed
	// attempts and a short read's unused request. Never refund failed work.
	if c := l.Read(uint64(len(b))); c != pdf.OK {
		return 0, c
	}
	n, rc := s.fs.read(s.handle, b)
	if rc < 0 || n < 0 || n > len(b) {
		return 0, pdf.ReadFailed
	}
	s.position += int64(n)
	return n, pdf.OK
}

// Admit measures the complete source, with a cap+1 probe, without retaining it.
// Admission is charged but precedes the timed Render/recheck portion.
func (s *Source) Admit(l *pdf.Ledger) pdf.Code {
	s.admitted = false
	if len(s.scratch) != chunk || s.digest == nil || s.opened {
		return pdf.InvalidBuffer
	}
	n, sum, c := s.scan(l)
	if c != pdf.OK {
		return c
	}
	s.size, s.sum, s.admitted = n, sum, true
	return pdf.OK
}

func (s *Source) scan(l *pdf.Ledger) (int64, [32]byte, pdf.Code) {
	var sum [32]byte
	if c := s.open(l); c != pdf.OK {
		return 0, sum, c
	}
	if c := l.Charge(pdf.Zero, 256); c != pdf.OK {
		_ = s.Close(l)
		return 0, sum, c
	}
	s.digest.Reset()
	var size int64
	c := pdf.OK
	for c == pdf.OK {
		want := min(chunk, pdf.MaxSource+1-int(size))
		var n int
		n, c = s.read(s.scratch[:want], l)
		if c != pdf.OK {
			break
		}
		size += int64(n)
		if size > pdf.MaxSource {
			c = pdf.SourceLimit
			break
		}
		if n == 0 {
			// SHA padding can require two blocks; state cloning and the
			// fixed digest copies are conservatively reserved as well.
			c = l.Charge(pdf.Copy, 256)
			if c == pdf.OK {
				c = l.Charge(pdf.Hash, 128)
			}
			if c == pdf.OK {
				s.digest.Sum(s.checked[:0])
				sum = s.checked
			}
			break
		}
		c = l.Charge(pdf.Copy, uint64(n))
		if c == pdf.OK {
			c = l.Charge(pdf.Hash, uint64(n))
		}
		if c == pdf.OK {
			_, _ = s.digest.Write(s.scratch[:n])
		}
	}
	if closed := s.Close(l); c == pdf.OK {
		c = closed
	}
	return size, sum, c
}

func (s *Source) ReadAt(dst []byte, offset int64, l *pdf.Ledger) (int, pdf.Code) {
	if !s.admitted || offset < 0 || offset > s.size || int64(len(dst)) > s.size-offset {
		return 0, pdf.ReadFailed
	}
	if !s.opened {
		if c := s.open(l); c != pdf.OK {
			return 0, c
		}
	} else if offset < s.position {
		if c := s.rewind(l); c != pdf.OK {
			return 0, c
		}
	}
	for s.position < offset {
		n, c := s.read(s.scratch[:min(int64(chunk), offset-s.position)], l)
		if c != pdf.OK {
			return 0, c
		}
		if n == 0 {
			return 0, pdf.SourceChanged
		}
	}
	total := 0
	for total < len(dst) {
		n, c := s.read(dst[total:total+min(chunk, len(dst)-total)], l)
		if c != pdf.OK {
			return total, c
		}
		if n == 0 {
			return total, pdf.SourceChanged
		}
		total += n
	}
	return total, pdf.OK
}

// Recheck uses the same transaction ledger and the complete sequential source.
// Hash and length must both match admission before the first output is opened.
func (s *Source) Recheck(l *pdf.Ledger) pdf.Code {
	if !s.admitted {
		return pdf.InvalidBuffer
	}
	if c := s.Close(l); c != pdf.OK {
		return c
	}
	if c := l.Rewind(); c != pdf.OK {
		return c
	}
	n, sum, c := s.scan(l)
	if c == pdf.SourceLimit {
		return pdf.SourceChanged
	}
	if c != pdf.OK {
		return c
	}
	if c := l.Charge(pdf.Examine, 32); c != pdf.OK {
		return c
	}
	if n != s.size || sum != s.sum {
		return pdf.SourceChanged
	}
	return l.CheckTime()
}
