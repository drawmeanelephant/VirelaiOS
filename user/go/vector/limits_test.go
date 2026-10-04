package vector

import (
	"math"
	"testing"
)

func hidden(commands []Command) Scene {
	return Scene{Commands: commands, Paints: []Paint{{Count: uint32(len(commands)), Transform: Affine{A: 1, D: 1}}}}
}
func TestGeometryCapacityBoundaries(t *testing.T) {
	ws, dst := scratchBuffer(), Target{make([]uint32, 1), 1, 1, 1}
	run := func(s Scene, want Code, edges uint64) {
		t.Helper()
		b := Budget{Max: MaxWork}
		stats, f := Rasterize(s, dst, ws, &b)
		if f.Code != want || want == OK && stats.Edges != edges {
			t.Fatalf("got %+v %+v want %d/%d", stats, f, want, edges)
		}
	}
	cmd := make([]Command, MaxCommands)
	cmd[0].Verb = Move
	for i := 1; i < len(cmd); i++ {
		cmd[i].Verb = Line
	}
	run(hidden(cmd), OK, MaxEdges)
	run(hidden(append(cmd, Command{Verb: Line})), CommandLimit, 0)
	cmd[len(cmd)-1] = Command{Verb: Quad, P: [3]Point{{1, 1}, {2, 0}}}
	run(hidden(cmd), SegmentLimit, 0)
	cmd = make([]Command, MaxContours*2)
	for i := 0; i < MaxContours; i++ {
		cmd[i*2].Verb, cmd[i*2+1].Verb = Move, Close
	}
	run(hidden(cmd), OK, MaxContours)
	run(hidden(append(cmd, Command{Verb: Move})), ContourLimit, 0)
	paints := make([]Paint, MaxPaints)
	run(Scene{Paints: paints}, OK, 0)
	run(Scene{Paints: append(paints, Paint{})}, SceneLimit, 0)
	for _, v := range []float64{-32768, 32768} {
		run(hidden([]Command{{Verb: Move, P: [3]Point{{v, v}}}}), OK, 1)
		run(hidden([]Command{{Verb: Move, P: [3]Point{{math.Nextafter(v, v*2), v}}}}), CoordinateLimit, 0)
	}
	f := flattener{sc: workspace(ws), m: &meter{b: &Budget{Max: MaxWork}}}
	if f.curve(Point{}, Point{1, 1}, Point{2, 0}, Point{}, false, 12) || f.code != CurveLimit {
		t.Fatal(f.code)
	}
	f = flattener{sc: workspace(ws), m: &meter{b: &Budget{Max: MaxWork}}}
	if !f.curve(Point{}, Point{1, 0}, Point{2, 0}, Point{}, false, 12) {
		t.Fatal(f.code)
	}
}
func TestBufferAndBudgetBoundaries(t *testing.T) {
	s, ws := rectScene(1, 1), scratchBuffer()
	for _, tc := range []struct {
		target Target
		code   Code
	}{
		{Target{make([]uint32, 1024*1536), 1024, 1536, 1024}, OK},
		{Target{nil, 1025, 1, 1025}, CanvasLimit},
		{Target{nil, 1, 1537, 1}, CanvasLimit},
		{Target{nil, 0, 1, 1}, CanvasLimit},
		{Target{nil, 1, 0, 1}, CanvasLimit},
		{Target{nil, 1, 1, math.MaxInt}, InvalidBuffer},
		{Target{nil, 1, 1, -1}, InvalidBuffer},
		{Target{nil, 1, 1, 1}, InvalidBuffer},
		{Target{make([]uint32, 1040), 1, 1, 1040}, OK},
		{Target{make([]uint32, 1041), 1, 1, 1041}, InvalidBuffer},
	} {
		b := Budget{Max: MaxWork}
		_, f := Rasterize(s, tc.target, ws, &b)
		if f.Code != tc.code {
			t.Fatal(f, tc.code)
		}
	}
	for _, b := range []*Budget{nil, {Used: 1, Max: 0}, {Max: MaxWork + 1}, {Used: math.MaxUint64, Max: math.MaxUint64}} {
		_, f := Rasterize(s, Target{make([]uint32, 1), 1, 1, 1}, ws, b)
		if f.Code != WorkLimit {
			t.Fatal(f)
		}
	}
	b := Budget{Used: MaxWork, Max: MaxWork}
	stats, f := Rasterize(Scene{}, Target{make([]uint32, 1), 1, 1, 1}, ws, &b)
	if f.Code != OK || stats != (Stats{}) || b.Used != MaxWork {
		t.Fatal(f, stats, b)
	}
	misaligned := make([]byte, WorkspaceSize+1)
	b = Budget{Max: MaxWork}
	_, f = Rasterize(s, Target{make([]uint32, 1), 1, 1, 1}, Workspace{misaligned[1:]}, &b)
	if f.Code != ScratchLimit {
		t.Fatal(f)
	}
}
func TestSortAndOffscreenCounters(t *testing.T) {
	b := Budget{Max: MaxWork}
	m := meter{b: &b}
	a := []crossing{{3, 1}, {1, 1}, {2, 1}}
	if !sortCross(a, &m) || m.counts[sortCompareWork] != 3 || m.counts[sortMoveWork] != 6 {
		t.Fatal(a, m.counts)
	}
	s := rectScene(1, 1)
	s.Paints[0].Transform.E = 100
	dst := Target{[]uint32{0x12345678}, 1, 1, 1}
	stats, f := Rasterize(s, dst, scratchBuffer(), &b)
	if f.Code != OK || stats.Edges != 4 || dst.Pix[0] != 0x12345678 {
		t.Fatal(stats, f)
	}
}
func TestCurvesFiniteChordAndConsumerGeometry(t *testing.T) {
	if distance(Point{2, 0}, Point{}, Point{1, 0}) != 1 || distance(Point{1, 1}, Point{}, Point{}) != math.Sqrt2 {
		t.Fatal("finite-chord distance")
	}
	for _, verb := range []Verb{Quad, Cubic} {
		c := Command{Verb: verb, P: [3]Point{{0, 0}, {4, 0}, {4, 0}}}
		if verb == Quad {
			c.P[2] = Point{}
		}
		s := Scene{Commands: []Command{{Verb: Move}, c, {Verb: Line, P: [3]Point{{4, 4}}}, {Verb: Line, P: [3]Point{{0, 4}}}, {Verb: Close}},
			Paints: []Paint{{Count: 5, Transform: Affine{A: 1, D: -1, F: 4}, Clip: Rect{1, 1, 3, 4}, Color: 0xffabcdef}}}
		dst := Target{make([]uint32, 16), 4, 4, 4}
		b := Budget{Max: MaxWork}
		_, f := Rasterize(s, dst, scratchBuffer(), &b)
		if f.Code != OK {
			t.Fatal(f)
		}
		for y := 0; y < 4; y++ {
			for x := 0; x < 4; x++ {
				want := uint32(0)
				if x >= 1 && x < 3 && y >= 1 {
					want = 0xffabcdef
				}
				if dst.Pix[y*4+x] != want {
					t.Fatalf("verb %d pixel %d,%d %x", verb, x, y, dst.Pix[y*4+x])
				}
			}
		}
	}
}
