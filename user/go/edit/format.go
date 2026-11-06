package main

// format.go — M138b (#2776): format-on-save for .go buffers. #3916 ships
// go/format as the default build: the M138b probe's recorded blocker was
// stale — it named the wrong bound.
//
// The probe (2026-10-20) measured GOEDIT+go/format at 2,556,064 bytes and
// refused it against kernel/src/exec.zig's exec_program_max (2,097,152).
// exec.zig's loader contract bounds a gap-layout static ELF — every
// GOOS=virelai Go binary — by exec_image_max (32 MiB) on the file and
// elf.map_max (64 MiB) on what it maps; it streams past the staging
// buffer, so exec_program_max never applies (exec.zig:753's check is
// gated on !gap_layout). Measured against the real bound the editfmt
// image fits with ~29.5 MiB to spare, so the opt-in tag, the nil seam
// and the named refusal are all gone: the shipped binary always formats.
//
// A format error never blocks the publish: the buffer is left alone, the
// error strip shows the diagnostic by name, and the save writes the
// unformatted bytes.

import (
	"bytes"
	"go/format"
	"strings"

	"virelai/vi"
)

// formatSource is the seam the host tests swap (useFmt); the shipped
// value is go/format's own.
var formatSource = format.Source

// isGoPath reports whether p names a Go source file. The suffix check is
// case-insensitive — the share carries both cases.
func isGoPath(p string) bool {
	return strings.HasSuffix(strings.ToLower(p), ".go")
}

// formatGo formats the buffer in place when the path names a .go file; it
// is save()'s first step, so a lease refusal still leaves the buffer
// formatted. An applied format is ONE composite undoable op covering the
// whole buffer — Ctrl-Z restores the pre-format bytes. The receipts:
// `format n=` on apply (the formatted length, also the already-clean
// no-op's receipt) and `format error <diag>` when the parser refused.
func (e *editor) formatGo() {
	if !isGoPath(e.path) {
		return
	}
	out, err := formatSource(e.buf)
	if err != nil {
		vi.ConsoleLine(markerFormat + "error " + err.Error())
		e.raiseBanner("format " + e.path + " failed: " + err.Error())
		return
	}
	vi.ConsoleLine(markerFormat + "n=" + vi.Itoa64(int64(len(out))))
	if bytes.Equal(out, e.buf) {
		return
	}
	old := append([]byte{}, e.buf...)
	cur := e.cur
	if cur > len(out) {
		cur = len(out)
	}
	e.applyOp(editOp{kind: opEdit, sp: []splice{{pos: 0, old: old, ins: out}}}, cur)
}
