// imgcat opens a window-bound tty, decodes a host-share QOI, and writes
// sixel to the terminal's own image layer. The kernel owns all pixels.
package main

import (
	"virelai/tabapp"
	"virelai/vi"
	"virelai/webrender"
)

const maxFile = 32 * 1024

func main() {
	args := vi.Args()
	if len(args) != 2 {
		vi.ConsoleLine("imgcat: usage IMGCAT.ELF /host/IMAGE.QOI")
		vi.Exit(2)
	}
	data, rc := vi.ReadFileAll(args[1], maxFile)
	if rc < 0 || len(data) >= maxFile {
		vi.ConsoleLine("imgcat: image read refused")
		vi.Exit(1)
	}
	img, err := webrender.DecodeImage(data) // shared GOVIEW QOI decoder
	if err != nil {
		vi.ConsoleLine("imgcat: image decode refused")
		vi.Exit(1)
	}
	seq, err := sixel(img)
	if err != nil {
		vi.ConsoleLine(err.Error())
		vi.Exit(1)
	}
	ta := tabapp.Init(tabapp.Config{
		Name: "IMGCAT.ELF", Title: "imgcat",
		X: 64, Y: 48, W: 640, H: 400,
	})
	if ta == nil {
		vi.ConsoleLine("imgcat: window refused")
		vi.Exit(1)
	}
	fd, rc := vi.FileOpen("/dev/tty", vi.ModeRead|vi.ModeWrite)
	if rc < 0 {
		vi.ConsoleLine("imgcat: tty refused")
		ta.CloseAndExit(1)
	}
	h := uint32(fd)
	if vi.TtyAttachWindow(ta.Win) < 0 {
		vi.ConsoleLine("imgcat: attach refused")
		vi.FileClose(h)
		ta.CloseAndExit(1)
	}
	// A known origin makes the 2x2-cell image and its later SU/ED positions
	// independent of any prompt or shell output.
	if !writeTTY(h, []byte("\x1b[2J\x1b[H\x1b[3;5H")) || !writeTTY(h, seq) {
		vi.ConsoleLine("imgcat: tty write refused")
		vi.FileClose(h)
		ta.CloseAndExit(1)
	}
	vi.ConsoleLine("imgcat: image " + vi.Itoa64(int64(img.Width)) + "x" +
		vi.Itoa64(int64(img.Height)))

	// Keep this window alive so a CLI invocation actually leaves a viewable
	// image. s scrolls the grid one cell up, c clears it, q closes the window.
	var buf [32]byte
	for {
		n, r := vi.FileRead(h, buf[:])
		if r < 0 {
			break
		}
		for _, key := range buf[:n] {
			switch key {
			case 's':
				// A region whose top is below row zero keeps history out
				// of the viewport and makes SU visibly move both tiles.
				if writeTTY(h, []byte("\x1b[2;4r\x1b[1S")) {
					vi.ConsoleLine("imgcat: scrolled")
				}
			case 'c':
				if writeTTY(h, []byte("\x1b[2J")) {
					vi.ConsoleLine("imgcat: cleared")
				}
			case 'q':
				vi.ConsoleLine("imgcat: quit")
				vi.FileClose(h)
				ta.CloseAndExit(0)
			}
		}
		ev, _, ok := vi.PollEventRaw()
		if ok && ta.Dispatch(ev) == tabapp.ActionClosed {
			break
		}
		if n == 0 {
			vi.Sleep(1)
		}
	}
	vi.FileClose(h)
	ta.CloseAndExit(0)
}

func writeTTY(h uint32, data []byte) bool {
	n, rc := vi.FileWriteAll(h, data)
	return rc >= 0 && n == len(data)
}
