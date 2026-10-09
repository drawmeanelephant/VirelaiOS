// Package tty's host surface: the kernel calls degrade to ENOSYS here,
// so these tests pin the pieces that stay honest off-guest — the paced
// input wrapper's ^C/^D forwarding onto the sig seam, the file's
// term.File shape, and the cell-grid math the overlays rely on.
package tty

import (
	"io"
	"os"
	"testing"
	"time"

	"virelai/tabapp"
)

func TestOpenRefusesOnHost(t *testing.T) {
	in, out, err := Open()
	if err == nil || in != nil || out != nil {
		t.Fatalf("Open on host = %v, want refusal (tabapp.Init -> win_open ENOSYS)", err)
	}
	if Bound() {
		t.Fatal("host open must not leave a bound fd")
	}
}

func TestInputForwardsCtrlBytesToSeam(t *testing.T) {
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	defer w.Close()
	in := &File{f: r}
	if _, err := w.Write([]byte{'a', 0x03, 'b'}); err != nil {
		t.Fatal(err)
	}
	buf := make([]byte, 8)
	done := make(chan int, 1)
	go func() {
		n, _ := in.Read(buf)
		done <- n
	}()
	select {
	case n := <-done:
		if n != 3 || string(buf[:n]) != "a\x03b" {
			t.Fatalf("Read = %d %q, want the typed bytes passed through", n, buf[:n])
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Read blocked on a filled pipe")
	}
	select {
	case <-ctl.Done():
	case <-time.After(time.Second):
		t.Fatal("^C (0x03) did not request shutdown on the sig seam")
	}
}

func TestInputPacesEmptyReads(t *testing.T) {
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	// On the host an os.Pipe read BLOCKS already; the virelai pacing loop
	// only shows when the kernel answers 0. What this pins on host is the
	// contract a caller sees: bytes that arrive late still come back.
	in := &File{f: r}
	defer r.Close()
	go func() {
		time.Sleep(30 * time.Millisecond)
		_, _ = w.Write([]byte("late"))
	}()
	buf := make([]byte, 8)
	n, err := in.Read(buf)
	if err != nil && err != io.EOF {
		t.Fatalf("Read err = %v", err)
	}
	if string(buf[:n]) != "late" {
		t.Fatalf("Read = %q, want paced delivery of late bytes", buf[:n])
	}
}

func TestTranslateEnterMapsLFOutsidePaste(t *testing.T) {
	out, tail, paste := translateEnter([]byte("a\nb\n"), false)
	if string(out) != "a\rb\r" || tail != nil || paste {
		t.Fatalf("translateEnter = %q tail=%q paste=%v", out, tail, paste)
	}
}

func TestTranslateEnterKeepsPasteContentVerbatim(t *testing.T) {
	src := "x\n" + pasteBegin + "a\nb" + pasteEnd + "y\n"
	out, tail, paste := translateEnter([]byte(src), false)
	want := "x\r" + pasteBegin + "a\nb" + pasteEnd + "y\r"
	if string(out) != want || tail != nil || paste {
		t.Fatalf("translateEnter = %q tail=%q paste=%v, want %q", out, tail, paste, want)
	}
}

func TestTranslateEnterMarkerSplitAcrossReads(t *testing.T) {
	// The begin marker lands split across two kernel reads; the tail
	// holdback must defer the decision, then emit the marker whole.
	out, tail, paste := translateEnter([]byte("k\x1b[20"), false)
	if string(out) != "k" || string(tail) != "\x1b[20" || paste {
		t.Fatalf("first chunk = %q tail=%q paste=%v", out, tail, paste)
	}
	out, tail, paste = translateEnter(append(tail, []byte("0~l\n")...), paste)
	if string(out) != pasteBegin+"l\n" || tail != nil || !paste {
		t.Fatalf("second chunk = %q tail=%q paste=%v", out, tail, paste)
	}
}

func TestTranslateEnterEndMarkerSplit(t *testing.T) {
	out, tail, paste := translateEnter([]byte("a\n\x1b[20"), true)
	if string(out) != "a\n" || string(tail) != "\x1b[20" || !paste {
		t.Fatalf("first chunk = %q tail=%q paste=%v", out, tail, paste)
	}
	out, tail, paste = translateEnter(append(tail, []byte("1~b\n")...), paste)
	if string(out) != pasteEnd+"b\r" || tail != nil || paste {
		t.Fatalf("second chunk = %q tail=%q paste=%v", out, tail, paste)
	}
}

func TestReadReturnsNormalizedBytes(t *testing.T) {
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	defer w.Close()
	in := &File{f: r}
	if _, err := w.Write([]byte("exit\n")); err != nil {
		t.Fatal(err)
	}
	buf := make([]byte, 8)
	n, err := in.Read(buf)
	if err != nil {
		t.Fatal(err)
	}
	if string(buf[:n]) != "exit\r" {
		t.Fatalf("Read = %q, want the kernel's Enter byte normalized to CR", buf[:n])
	}
}

func TestFileIsTermFileShaped(t *testing.T) {
	// term.File is io.ReadWriteCloser + Fd(); the shape is what
	// initInput's type assertion checks.
	var _ interface {
		io.ReadWriteCloser
		Fd() uintptr
	} = (*File)(nil)
}

func TestNoteResizeCellMath(t *testing.T) {
	// Host TerminalCell has no SETTINGS.TXT to read and keeps the boot
	// cell (8x16), so the kernel formulas are checkable off-guest:
	// 512x384 -> clamp(512/8)=64 cols, (384-16)/16=23 rows — the same
	// numbers charmhello's owner-seam resize proves live.
	cols, rows := tabapp.CellGrid(512, 384, 8, 16)
	if cols != 64 || rows != 23 {
		t.Fatalf("CellGrid(512,384) = %dx%d, want 64x23", cols, rows)
	}
	got := false
	OnSize(func(c, r int) { got = c == 64 && r == 23 })
	nc, nr := NoteResize(512, 384)
	if nc != 64 || nr != 23 {
		t.Fatalf("NoteResize(512,384) = %dx%d, want 64x23", nc, nr)
	}
	if !got {
		t.Fatal("OnSize observer did not fire")
	}
	if w, h := Size(); w != 64 || h != 23 {
		t.Fatalf("Size = %dx%d after NoteResize, want 64x23", w, h)
	}
	// The rows floor: a rect shorter than the title band still answers 1.
	if c, r := tabapp.CellGrid(512, 8, 8, 16); c != 64 || r != 1 {
		t.Fatalf("CellGrid(512,8) = %dx%d, want 64x1", c, r)
	}
}

func TestCloseIsIdempotent(t *testing.T) {
	Close()
	Close()
	if WindowID() != -1 {
		t.Fatal("WindowID must reset after Close")
	}
}
