The accessibility outline reader — every 500 ms it asks the
seat's WINDOWS_QUERY which window holds the focus, reads that
window's published outline at `/host/A11Y.<win>`, and paints it as
an indented role/state/name list; no focus, no outline and a
malformed file are each named states, never crashes. `-announce`
prints each new utterance and `-say` stages it on the speech lane,
deduped per window so a rename re-announces and a repeat does not.
`go-a11y.spec` narrates a scripted focus drive in order. The tile
is [ICONA11Y](https://github.com/drawmeanelephant/VirelaiOS/blob/main/image/art/icons/icona11y.qoi).
