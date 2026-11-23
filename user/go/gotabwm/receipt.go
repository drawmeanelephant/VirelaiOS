package main

// presentReceipt is the shared post-present latch behind the seat's
// "this layer painted and the frame actually presented" markers
// (M216g, #3946). Feed it the frame's (painted, presented) facts on
// every composite pass; feed reports true exactly once per episode —
// the first pass where both hold after the last arm(). The episode
// boundaries stay with each marker's own rule: the rail's is the boot,
// the toast strip's is a non-empty queue, the center's is an open
// panel, the drag ghost's is one armed drag, and the hover card's is
// one presented card.
type presentReceipt struct {
	fired bool
}

// arm re-arms the receipt: the next painted+presented pass earns the
// marker again. The zero value is armed.
func (r *presentReceipt) arm() { r.fired = false }

// feed reports whether this pass is the episode's receipt. A painted
// frame that never presented proves nothing about the scanout, and an
// unpainted pass cannot claim one — both leave the latch armed.
func (r *presentReceipt) feed(painted, presented bool) bool {
	if r.fired || !painted || !presented {
		return false
	}
	r.fired = true
	return true
}
