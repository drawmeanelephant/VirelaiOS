The spreadsheet — a virtualized grid behind a
`TableController`, an edit line with the sheet-convention replace
semantics, live formula recalc that refuses a cycle by name
(`#CYCLE`), a menu bar, fills, undo and column formats.
`ctrl+c`/`ctrl+v` move marked ranges as RFC 4180 CSV on the
`text/csv` clipboard lane — over the kernel's bound it refuses
honestly — and `ctrl+o`/`ctrl+s` raise the shared FilePicker for
crash-safe CSV import/export. `go-sheet.spec` drives the grid by
injected keystrokes; the tile is
[ICONSHT](https://github.com/drawmeanelephant/VirelaiOS/blob/main/image/art/icons/iconsht.qoi).
