package vi

// ScrollAxis identifies the direction axis of a decoded EvMouseScroll event.
type ScrollAxis uint8

const (
	ScrollVertical   ScrollAxis = 0
	ScrollHorizontal ScrollAxis = 1
)

// ScrollMaxMagnitude is the ADR 0013 D2 (M216k) upper bound on the 14-bit
// magnitude field of an EvMouseScroll event, in text lines.
const ScrollMaxMagnitude = 8191

// Scroll is the decoded payload of an ADR 0013 D2 EvMouseScroll (kind 12) event.
// Delta is signed in text lines: positive is down (vertical) or right
// (horizontal), negative is up or left, and 0 means no scroll (or a non-scroll
// event). Wheel is true when the event came from a hardware scroll wheel
// (Flags&ScrollWheel != 0) and false for PageUp/PageDown-synthesized events.
// X and Y carry the window-local pointer coordinates on wheel-sourced events
// (0 on synthesized events).
type Scroll struct {
	Delta int
	Axis  ScrollAxis
	Wheel bool
	X     int
	Y     int
}

// DecodeScroll unpacks an EvMouseScroll (kind 12) event per ADR 0013 D2 / M216k.
// Non-scroll events return the zero Scroll (Delta == 0).
func DecodeScroll(ev Event) Scroll {
	if ev.Kind != EvMouseScroll {
		return Scroll{}
	}
	mag := int(ev.Arg0 & 0x3fff)
	if mag > ScrollMaxMagnitude {
		mag = ScrollMaxMagnitude
	}
	axis := ScrollVertical
	if ev.Arg0&0x4000 != 0 {
		axis = ScrollHorizontal
	}
	delta := -mag
	if ev.Arg0&0x8000 != 0 {
		delta = mag
	}
	return Scroll{
		Delta: delta,
		Axis:  axis,
		Wheel: ev.Flags&ScrollWheel != 0,
		X:     int(ev.Arg1 & 0xffff),
		Y:     int((ev.Arg1 >> 16) & 0xffff),
	}
}
