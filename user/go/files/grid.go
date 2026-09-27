package main

func gridForCell(w, h, cellW, cellH uint32) (int, int) {
	if cellW == 0 || cellH == 0 {
		return 0, 0
	}
	cols := w / cellW
	ch := h
	if ch > titleBarPx {
		ch -= titleBarPx
	} else {
		ch = 0
	}
	return int(cols), int(ch / cellH)
}
