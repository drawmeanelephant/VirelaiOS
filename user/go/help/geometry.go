package main

import (
	"virelai/tabapp"
	"virelai/vi"
)

// gridOf maps the window rect to the kernel's terminal grid. Read the active
// cell rung (M80i) and use the shared kernel geometry rather than assuming
// the legacy 8x8 framebuffer text cell is the terminal's row height.
func gridOf(w, h uint32) (int, int) {
	cellW, cellH := vi.TerminalCell()
	return tabapp.CellGrid(w, h, cellW, cellH)
}
