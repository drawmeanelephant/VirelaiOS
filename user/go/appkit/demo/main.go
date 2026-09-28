// Command demo is a small live adopter of the appkit dialog. Its only surface
// is an owned Seam-B window buffer; the chrome is painted by Dialog.Draw, not
// a second gate-only rasterizer.
package main

import (
	"virelai/appkit"
	"virelai/draw"
	"virelai/theme"
	"virelai/vi"
	"virelai/widgets"
)

// Leave the kernel's argv/envp tail out of the runtime's first sbrk page.
var argvHeadroom [0x1000]byte

func main() {
	const w, h = 512, 384
	id, rc := vi.WinOpen(56, 56, w, h)
	if rc < 0 {
		vi.ConsoleLine("godialog: open failed")
		vi.Exit(1)
	}
	c, err := draw.WindowCanvas(id, w, h)
	if err != nil {
		vi.ConsoleLine("godialog: surface failed")
		vi.WinClose(id)
		vi.Exit(2)
	}
	// A tab attachment keeps the requested native geometry: the window's
	// bound surface must not be resized independently of its mapped bytes.
	_ = vi.WmMailRequest(vi.WmRpcKindAttachTab, uint32(id), 0, 0, 0, 0, "Dialog", "GODIALOG.ELF")
	d := appkit.NewPrompt("Open file", "Enter a path to open")
	d.Input = "notes.txt"
	d.UIFace, err = draw.ReadFont("/host/INTER.TTF", 500000)
	if err != nil {
		vi.ConsoleLine("godialog: fonts missing")
		vi.WinClose(id)
		vi.Exit(3)
	}
	d.IconFace, err = draw.ReadFont("/host/CHROME.TTF", 8192)
	if err != nil {
		vi.ConsoleLine("godialog: fonts missing")
		vi.WinClose(id)
		vi.Exit(3)
	}
	for ticks := 0; ticks < 30 && d.Open; ticks++ {
		draw.FillRect(c, widgets.Rect{W: w, H: h}, draw.Opaque(theme.Current.Bg))
		d.Draw(c)
		if vi.WinPresent(id) < 0 {
			vi.ConsoleLine("godialog: present failed")
			vi.WinClose(id)
			vi.Exit(4)
		}
		if ticks == 0 {
			vi.ConsoleLine("godialog: painted")
		}
		if ev, ok := vi.PollEvent(); ok {
			if ev.Kind == vi.EvWinClose {
				break
			}
			d.HandleKey(ev)
			d.HandleMouse(int(ev.Arg0), int(ev.Arg1), ev.Kind == vi.EvMouseDown)
		}
		vi.Sleep(1)
	}
	vi.WinClose(id)
	vi.ConsoleLine("godialog: done")
}
