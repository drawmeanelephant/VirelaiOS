Named machine snapshots, managed from inside the guest —
`save`, `restore`, `rm` and `pin` over the armed ctl channel
(ADR 0056), each parked on the share as `SNAPSHOTS/<name>.vzsave`
while the machine keeps running. The two-step consent is the
design: a bare launch opens the windowed face where the same verbs
are a click-twice arm, and a restore never touches the running
machine — the rewind lands on the next boot. `go-snap.spec`
drives the verbs over the pinned vsock vector. The tile is
[ICONSNAP](https://github.com/drawmeanelephant/VirelaiOS/blob/main/image/art/icons/iconsnap.qoi).
