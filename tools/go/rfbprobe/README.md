# rfbprobe, the independent M84c host viewer

Build with `just go-rfbprobe` (or `bash tools/go/rfbprobe/build.sh`).
`just gate live-rfb` builds the host probe during setup; its guest binaries
remain explicit prerequisites.
The runner connects its standard input/output to a **hermetic** `--net` TCP
connection. There is no host listener and no NAT/real-device reachability.
The probe requires the GOCALC.ELF button to be visible before connection.

The probe used `user/go/rfb/doc.go` as its wire reference and does not import
or call the codec. The doc specified the banner, None negotiation, ServerInit,
raw rectangle layout, X11 keysym numbers, and BGR0 wire pixels. Its author
also inspected the codec for the separate in-seat integration, so this is
not a blind, independent authoring claim.
The doc did **not** supply an exact hosted Calc pixel or launcher hit
coordinates. Those came from the Calc/widget layout and seat launcher code,
and are pinned by `live-rfb.spec`. It also did not describe the runner's
single-slot TCP pacing; the hermetic stream adapter handles that separately.
The seat accepts one viewer per explicit invocation, with no reconnect.
Full raw frames take roughly 19,200 paced TCP segments and are not intended
as an interactive display on this transport. Each stalled segment times out
after 30 guest ticks.

None has no authentication or confidentiality. This probe and the
`--rfb-hermetic` seat opt-in are for same-trust-domain tests only, never
direct LAN exposure. See ADR 0037 D5/D6.
