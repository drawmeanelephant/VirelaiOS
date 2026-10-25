A Charm TUI system monitor — a real `tea.Model` fed by the
kernel's bound-tty queue: Update receives tty-decoded key
messages, View emits Bubble Tea's ANSI text back into the bound
tty, repainted on every key and on a 1 Hz tick. There is no POSIX
tty or signals underneath — the Virelai port runs the model, not
Bubble Tea's host Program loop — the same shape GOFILES and GOHELP
ride. Its owning spec is `go-top.spec`, the same gate as GOTOP's;
the tile is [ICONPULS](https://github.com/drawmeanelephant/VirelaiOS/blob/main/image/art/icons/iconpuls.qoi).
