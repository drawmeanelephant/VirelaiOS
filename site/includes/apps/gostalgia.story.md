An entire foreign Go userland shipped as one static ELF —
the pinned Gostalgia tree (apps, services, a shell over IPC) built
for GOOS=virelai through the `gsport` overlay mechanism, its own
repository untouched. It launches `shell --root /host/GS` onto a
GOTABWM-hosted window tty, Bubble Tea v1 and all.
`go-gostalgia.spec` drives real HID keystrokes from the seat's own
launcher row into a live session and back out to a clean exit. The
tile is [ICONSTLG](https://github.com/drawmeanelephant/VirelaiOS/blob/main/image/art/icons/iconstlg.qoi).
