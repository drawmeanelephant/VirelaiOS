package vi

import "testing"

func TestDecodeScrollSignsAndAxes(t *testing.T) {
	cases := []struct {
		name      string
		ev        Event
		wantDelta int
		wantAxis  ScrollAxis
		wantWheel bool
		wantX     int
		wantY     int
	}{
		{
			name:      "vertical down wheel",
			ev:        Event{Kind: EvMouseScroll, Flags: ScrollWheel, Arg0: 0x8003, Arg1: 120 | (340 << 16)},
			wantDelta: 3,
			wantAxis:  ScrollVertical,
			wantWheel: true,
			wantX:     120,
			wantY:     340,
		},
		{
			name:      "vertical up wheel with mods",
			ev:        Event{Kind: EvMouseScroll, Flags: ScrollWheel | ModShift | ModCtrl, Arg0: 0x0005, Arg1: 42 | (99 << 16)},
			wantDelta: -5,
			wantAxis:  ScrollVertical,
			wantWheel: true,
			wantX:     42,
			wantY:     99,
		},
		{
			name:      "horizontal right wheel",
			ev:        Event{Kind: EvMouseScroll, Flags: ScrollWheel, Arg0: 0xc002, Arg1: 10 | (20 << 16)},
			wantDelta: 2,
			wantAxis:  ScrollHorizontal,
			wantWheel: true,
			wantX:     10,
			wantY:     20,
		},
		{
			name:      "horizontal left wheel",
			ev:        Event{Kind: EvMouseScroll, Flags: ScrollWheel, Arg0: 0x4004, Arg1: 15 | (25 << 16)},
			wantDelta: -4,
			wantAxis:  ScrollHorizontal,
			wantWheel: true,
			wantX:     15,
			wantY:     25,
		},
		{
			name:      "synthesized PageDown",
			ev:        Event{Kind: EvMouseScroll, Flags: 0, Arg0: 0x8001, Arg1: 0},
			wantDelta: 1,
			wantAxis:  ScrollVertical,
			wantWheel: false,
		},
		{
			name:      "synthesized PageUp",
			ev:        Event{Kind: EvMouseScroll, Flags: 0, Arg0: 0x0001, Arg1: 0},
			wantDelta: -1,
			wantAxis:  ScrollVertical,
			wantWheel: false,
		},
		{
			name:      "zero magnitude yields zero delta",
			ev:        Event{Kind: EvMouseScroll, Flags: ScrollWheel, Arg0: 0x8000, Arg1: 0},
			wantDelta: 0,
			wantAxis:  ScrollVertical,
			wantWheel: true,
		},
		{
			name:      "non-scroll event returns zero",
			ev:        Event{Kind: EvKeyDown, Flags: ScrollWheel, Arg0: 0x8003, Arg1: 10},
			wantDelta: 0,
			wantAxis:  ScrollVertical,
			wantWheel: false,
		},
		{
			name:      "upper arg0 bits ignored",
			ev:        Event{Kind: EvMouseScroll, Flags: ScrollWheel, Arg0: 0xffff8002, Arg1: 0},
			wantDelta: 2,
			wantAxis:  ScrollVertical,
			wantWheel: true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := DecodeScroll(tc.ev)
			if got.Delta != tc.wantDelta || got.Axis != tc.wantAxis || got.Wheel != tc.wantWheel ||
				got.X != tc.wantX || got.Y != tc.wantY {
				t.Fatalf("DecodeScroll(%+v) = %+v, want {Delta:%d Axis:%d Wheel:%v X:%d Y:%d}",
					tc.ev, got, tc.wantDelta, tc.wantAxis, tc.wantWheel, tc.wantX, tc.wantY)
			}
		})
	}
}

func TestDecodeScrollMagnitudeBound(t *testing.T) {
	if ScrollMaxMagnitude != 8191 {
		t.Fatalf("ScrollMaxMagnitude = %d want 8191", ScrollMaxMagnitude)
	}
	// Exact upper bound passes through.
	if got := DecodeScroll(Event{Kind: EvMouseScroll, Flags: ScrollWheel, Arg0: 0x8000 | 8191}); got.Delta != 8191 {
		t.Fatalf("max positive = %d want 8191", got.Delta)
	}
	if got := DecodeScroll(Event{Kind: EvMouseScroll, Flags: ScrollWheel, Arg0: 8191}); got.Delta != -8191 {
		t.Fatalf("max negative = %d want -8191", got.Delta)
	}
	// Values above 8191 in the 14-bit field clamp to 8191.
	if got := DecodeScroll(Event{Kind: EvMouseScroll, Flags: ScrollWheel, Arg0: 0x8000 | 8192}); got.Delta != 8191 {
		t.Fatalf("over-bound positive = %d want 8191", got.Delta)
	}
	if got := DecodeScroll(Event{Kind: EvMouseScroll, Flags: ScrollWheel, Arg0: 0x3fff}); got.Delta != -8191 {
		t.Fatalf("over-bound negative (0x3fff) = %d want -8191", got.Delta)
	}
	if got := DecodeScroll(Event{Kind: EvMouseScroll, Flags: ScrollWheel, Arg0: 0xc000 | 0x3fff}); got.Delta != 8191 || got.Axis != ScrollHorizontal {
		t.Fatalf("over-bound horizontal = %+v want Delta=8191 Horizontal", got)
	}
}
