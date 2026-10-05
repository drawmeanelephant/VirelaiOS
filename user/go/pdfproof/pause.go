package pdfproof

import (
	"virelai/pdf"
	"virelai/vi"
)

// The host publishes a one-byte acknowledgment only after the complete live
// receipt is visible in the serial log. Pause I/O is outside every page
// transaction; it uses the existing staging buffer and a separate bounded
// diagnostic ledger, never changing a page's work/time accounting.
func waitReceipt(fs files, path string, scratch []byte, clock func() uint64, sleep func(uint64)) pdf.Code {
	if len(scratch) < 2 {
		return pdf.InvalidBuffer
	}
	start := clock()
	l := pdf.Ledger{Max: pdf.MaxWork}
	for clock()-start < pdf.MaxNanos {
		if c := l.Open(); c != pdf.OK {
			return c
		}
		h, rc := fs.open(path, vi.ModeRead)
		if rc < 0 {
			_ = l.Close()
			if rc != -vi.ErrENOENT {
				return pdf.ReadFailed
			}
			sleep(1)
			continue
		}
		c := l.Read(2) // the one-byte acknowledgment plus an EOF probe
		if c == pdf.OK {
			n, readRC := fs.read(uint32(h), scratch[:2])
			if readRC < 0 {
				c = pdf.ReadFailed
			} else if n != 1 || scratch[0] != '1' {
				c = pdf.InvalidBuffer
			}
		}
		fs.close(uint32(h))
		if closed := l.Close(); c == pdf.OK {
			c = closed
		}
		if clock()-start >= pdf.MaxNanos {
			return pdf.TimeLimit
		}
		return c
	}
	return pdf.TimeLimit
}
