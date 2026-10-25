The terminal — a window bound to the kernel's `/dev/tty` seam
(selector 2, ADR 0020), hosting a real gosh session: `shlib`'s
editor and engine own the prompt, editing, line execution and
history. The binary itself draws no pixels — the kernel paints the
tty grid into the bound window — while named profiles, clickable
OSC-8 hyperlinks, a scrollback find bar, selection, OSC 52
clipboard bridging and bracketed paste ride on top.
`go-term.spec` types a line through the real session and watches
the restart path; the tile is
[ICONTERM](https://github.com/drawmeanelephant/VirelaiOS/blob/main/image/art/icons/iconterm.qoi).
