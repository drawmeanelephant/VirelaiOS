package main

import "testing"

func TestGridForCellMatchesKernelGeometry(t *testing.T) {
	for _, tc := range []struct {
		name     string
		cellW    uint32
		cellH    uint32
		wantCols int
		wantRows int
	}{
		{name: "small", cellW: 7, cellH: 13, wantCols: 73, wantRows: 28},
		{name: "medium", cellW: 8, cellH: 16, wantCols: 64, wantRows: 23},
		{name: "large", cellW: 10, cellH: 21, wantCols: 51, wantRows: 17},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cols, rows := gridForCell(512, 384, tc.cellW, tc.cellH)
			if cols != tc.wantCols || rows != tc.wantRows {
				t.Fatalf("gridForCell = %dx%d, want %dx%d", cols, rows, tc.wantCols, tc.wantRows)
			}
		})
	}
}

func TestGridForCellRejectsZeroMetrics(t *testing.T) {
	if cols, rows := gridForCell(512, 384, 0, 16); cols != 0 || rows != 0 {
		t.Fatalf("zero width metrics produced %dx%d", cols, rows)
	}
	if cols, rows := gridForCell(512, 384, 8, 0); cols != 0 || rows != 0 {
		t.Fatalf("zero height metrics produced %dx%d", cols, rows)
	}
}
