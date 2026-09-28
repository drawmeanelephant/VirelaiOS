# rfbprobe, the independent M84c/M84d host viewer

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
as an interactive display on this transport. Each unacknowledged segment
times out after 30 s of guest wall clock (`stalled`); a viewer whose RST or
FIN arrives while a segment waits drops at once (`peer`).

## Modes (M84d)

The runner passes flags with `--net-tcp-connect-stream-arg`. The probe's
exit is its close: status 0 makes the runner send FIN, any other status RST.

| `-mode` | What the viewer does | What the seat must do |
|---|---|---|
| `pixels` (default) | reads Calc's button pixels, sends Ctrl+Space and a click | route both, then `rfb drop peer` on the FIN |
| `security` | picks VNC auth (type 2), then keeps talking | SecurityResult=failed, hang up, no input |
| `malformed` | holds a content press, sends SetEncodings count=65535 | `rfb drop malformed`, release the press |
| `stall` | sends 3 of a request's 10 bytes | keep compositing, `rfb drop timeout` |
| `die-drag` | drags with the button held, dies mid full-frame update (exit 3) | `rfb drop peer`, release at the last point |
| `die-focus` | opens the launcher, holds `c`, dies (exit 3) | `rfb drop peer`, the launcher stays usable locally |

`-bridge 127.0.0.1:PORT` is the class-C tape path (`tools/rfb-tape.sh`,
ADR 0037 D5(1)): it accepts exactly one viewer on a loopback address and
pipes it to the guest, logging the first 64 bytes each way. It refuses a
non-loopback address.

None has no authentication or confidentiality. This probe and the
`--rfb-hermetic` seat opt-in are for same-trust-domain tests only, never
direct LAN exposure. See ADR 0037 D5/D6.
