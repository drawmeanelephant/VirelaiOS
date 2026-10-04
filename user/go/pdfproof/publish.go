package pdfproof

import (
	"unsafe"
	"virelai/pdf"
	"virelai/vi"
)

func put(b []byte, n uint32) {
	for i := 0; i < 4; i++ {
		b[i] = byte(n >> uint(i*8))
	}
}

func fresh(fs files, path string, l *pdf.Ledger) (uint32, pdf.Code) {
	if c := l.Open(); c != pdf.OK {
		return 0, c
	}
	h, rc := fs.open(path, vi.ModeRead)
	if rc >= 0 {
		fs.close(uint32(h))
		_ = l.Close()
		return 0, pdf.OutputExists
	}
	if c := l.Close(); c != pdf.OK {
		return 0, c
	}
	if rc != -vi.ErrENOENT {
		return 0, pdf.WriteFailed
	}
	if c := l.Open(); c != pdf.OK {
		return 0, c
	}
	h, rc = fs.open(path, vi.ModeWrite|vi.ModeCreate)
	if rc < 0 {
		_ = l.Close()
		return 0, pdf.WriteFailed
	}
	return uint32(h), pdf.OK
}

func write(fs files, h uint32, b []byte, l *pdf.Ledger) pdf.Code {
	for len(b) > 0 {
		take := min(chunk, len(b))
		if c := l.Charge(pdf.IO, 1); c != pdf.OK {
			return c
		}
		if c := l.Charge(pdf.Copy, uint64(take)); c != pdf.OK {
			return c
		}
		n, rc := fs.write(h, b[:take])
		if rc < 0 || n <= 0 || n > take {
			return pdf.WriteFailed
		}
		b = b[n:]
	}
	return pdf.OK
}

func finish(fs files, h uint32, c pdf.Code, l *pdf.Ledger) pdf.Code {
	if c == pdf.OK {
		c = l.Charge(pdf.IO, 1)
		if c == pdf.OK && fs.sync(h) < 0 {
			c = pdf.WriteFailed
		}
	}
	fs.close(h)
	if closed := l.Close(); c == pdf.OK {
		c = closed
	}
	return c
}

// Publish writes the borrowed page directly. Call Recheck first. Serialization
// is outside the render deadline, but its work and handles remain charged.
func Publish(path string, p pdf.Page, stage []byte, l *pdf.Ledger) pdf.Code {
	return publish(native, path, p, stage, l)
}

func publish(fs files, path string, p pdf.Page, stage []byte, l *pdf.Ledger) pdf.Code {
	if p.Width <= 0 || p.Height <= 0 || p.Stride != p.Width ||
		p.Width > 1024 || p.Height > 1536 || len(p.Pix) != p.Width*p.Height || len(stage) < 16 {
		return pdf.InvalidBuffer
	}
	if uint64(len(p.Pix))*4+16 > pdf.MaxOutput {
		return pdf.OutputLimit
	}
	h, c := fresh(fs, path, l)
	if c != pdf.OK {
		return c
	}
	if c = l.Charge(pdf.Record, 16); c != pdf.OK {
		return finish(fs, h, c, l)
	}
	copy(stage, "PDF1")
	put(stage[4:8], uint32(p.Width))
	put(stage[8:12], uint32(p.Height))
	put(stage[12:16], uint32(p.Stride))
	c = write(fs, h, stage[:16], l)
	if c == pdf.OK {
		b := unsafe.Slice((*byte)(unsafe.Pointer(&p.Pix[0])), len(p.Pix)*4)
		c = write(fs, h, b, l)
	}
	return finish(fs, h, c, l)
}

func WriteReceipt(path string, b []byte, l *pdf.Ledger) pdf.Code {
	if len(b) > pdf.MaxReceipt {
		return pdf.ReceiptLimit
	}
	h, c := fresh(native, path, l)
	if c != pdf.OK {
		return c
	}
	return finish(native, h, write(native, h, b, l), l)
}
