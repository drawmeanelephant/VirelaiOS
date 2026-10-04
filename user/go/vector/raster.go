package vector

import (
	"math"
	"unsafe"
)

// Each bucket is one of §5's fixed-size operations. Byte clearing is charged
// per byte, not per machine word. Tests pin bucket counts as well as totals.
const (
	validateWork = iota
	transformWork
	subdivideWork
	edgeWork
	edgeTestWork
	crossingWork
	sortCompareWork
	sortMoveWork
	windingWork
	spanWork
	coverageWork
	blendWork
	zeroWork
	workKinds
)

type meter struct {
	b       *Budget
	counts  [workKinds]uint64
	planned uint64
	plan    bool
}

func (m *meter) take(kind int, n uint64) bool {
	if n > m.b.Max-m.b.Used {
		return false
	}
	m.b.Used += n
	m.counts[kind] += n
	if m.plan {
		m.planned += n
	}
	return true
}

type edge struct{ a, b Point }
type crossing struct {
	x   float64
	dir int64
}
type paintPlan struct{ first, count uint64 }
type scratch struct {
	edges []edge
	cross []crossing
	row   []float64
	plans []paintPlan
	high  *uint64
}

const scratchUsed = MaxEdges*32 + MaxEdges*16 + 1024*8 + MaxPaints*16

func workspace(w Workspace) scratch {
	p := unsafe.Pointer(&w.Bytes[0])
	return scratch{
		unsafe.Slice((*edge)(p), MaxEdges),
		unsafe.Slice((*crossing)(unsafe.Add(p, MaxEdges*32)), MaxEdges),
		unsafe.Slice((*float64)(unsafe.Add(p, MaxEdges*48)), 1024),
		unsafe.Slice((*paintPlan)(unsafe.Add(p, MaxEdges*48+1024*8)), MaxPaints),
		nil,
	}
}
func (s scratch) touch(end uint64) {
	if s.high != nil && end > *s.high {
		*s.high = end
	}
}

func finiteBound(v, limit float64) bool { return !math.IsNaN(v) && math.Abs(v) <= limit }
func pointOK(p Point) bool              { return finiteBound(p.X, 32768) && finiteBound(p.Y, 32768) }
func affineOK(a Affine) bool {
	return finiteBound(a.A, 256) && finiteBound(a.B, 256) && finiteBound(a.C, 256) &&
		finiteBound(a.D, 256) && finiteBound(a.E, 32768) && finiteBound(a.F, 32768)
}
func mapped(a Affine, p Point) Point { return Point{a.A*p.X + a.C*p.Y + a.E, a.B*p.X + a.D*p.Y + a.F} }
func arity(v Verb) int {
	switch v {
	case Move, Line:
		return 1
	case Quad:
		return 2
	case Cubic:
		return 3
	case Close:
		return 0
	default:
		return -1
	}
}

// Rasterize never writes dst until both geometry and the exact replay work
// have passed preflight. Inputs must remain immutable and must not alias ws.
func Rasterize(s Scene, dst Target, ws Workspace, b *Budget) (Stats, Failure) {
	var stats Stats
	if b == nil || b.Used > b.Max || b.Max > MaxWork {
		return stats, failure(WorkLimit, -1, -1)
	}
	start := b.Used
	m := meter{b: b}
	f := rasterize(s, dst, ws, &m, &stats)
	stats.Work = b.Used - start
	return stats, f
}

func rasterize(s Scene, dst Target, ws Workspace, m *meter, stats *Stats) Failure {
	if dst.Width <= 0 || dst.Width > 1024 || dst.Height <= 0 || dst.Height > 1536 {
		return failure(CanvasLimit, -1, -1)
	}
	if dst.Stride < dst.Width || dst.Stride > 1040 || len(dst.Pix) < dst.Stride*dst.Height {
		return failure(InvalidBuffer, -1, -1)
	}
	if len(ws.Bytes) < WorkspaceSize || uintptr(unsafe.Pointer(&ws.Bytes[0]))&7 != 0 {
		return failure(ScratchLimit, -1, -1)
	}
	if len(s.Commands) > MaxCommands {
		return failure(CommandLimit, -1, -1)
	}
	if len(s.Paints) > MaxPaints || len(s.Commands)*int(unsafe.Sizeof(Command{}))+len(s.Paints)*int(unsafe.Sizeof(Paint{})) > MaxScene {
		return failure(SceneLimit, -1, -1)
	}
	next, contours := uint64(0), 0
	for pi, p := range s.Paints {
		if !m.take(validateWork, 1) {
			return failure(WorkLimit, pi, -1)
		}
		if uint64(p.First) != next || uint64(p.Count) > uint64(len(s.Commands))-next ||
			p.Rule > EvenOdd || p.Clip.X1 < p.Clip.X0 || p.Clip.Y1 < p.Clip.Y0 {
			return failure(Malformed, pi, -1)
		}
		if !affineOK(p.Transform) {
			return failure(CoordinateLimit, pi, -1)
		}
		open := false
		for ci := int(p.First); ci < int(next+uint64(p.Count)); ci++ {
			if !m.take(validateWork, 1) {
				return failure(WorkLimit, pi, ci)
			}
			c := s.Commands[ci]
			n := arity(c.Verb)
			if n < 0 || (!open && c.Verb != Move) {
				return failure(Malformed, pi, ci)
			}
			if c.Verb == Move {
				open = true
				contours++
				if contours > MaxContours {
					return failure(ContourLimit, pi, ci)
				}
			}
			for k := 0; k < 3; k++ {
				if k >= n {
					if c.P[k] != (Point{}) {
						return failure(Malformed, pi, ci)
					}
				} else {
					if !pointOK(c.P[k]) {
						return failure(CoordinateLimit, pi, ci)
					}
					if !m.take(transformWork, 1) {
						return failure(WorkLimit, pi, ci)
					}
					if !pointOK(mapped(p.Transform, c.P[k])) {
						return failure(CoordinateLimit, pi, ci)
					}
				}
			}
		}
		next += uint64(p.Count)
	}
	if next != uint64(len(s.Commands)) {
		return failure(Malformed, -1, -1)
	}
	if len(s.Paints) == 0 {
		return success()
	}
	sc := workspace(ws)
	sc.high = &stats.ScratchBytes
	// The dry expansion and sampler perform precisely the operations replay
	// will perform, including pricing blends without reading destination.
	m.plan = true
	n, f := expand(s, sc, m)
	stats.Edges = uint64(n)
	if f.Code != OK {
		return f
	}
	if f = sample(s, dst, sc, m, false); f.Code != OK {
		return f
	}
	m.plan = false
	if m.planned > m.b.Max-m.b.Used {
		return failure(WorkLimit, -1, -1)
	}
	// Immutable inputs make both passes identical. The reservation above
	// ensures none of the checked operations below can exhaust the budget.
	if _, f = expand(s, sc, m); f.Code != OK {
		return f
	}
	return sample(s, dst, sc, m, true)
}

type flattener struct {
	sc        scratch
	m         *meter
	n, pi, ci int
	code      Code
}

func (f *flattener) emit(a, b Point) bool {
	if !f.m.take(edgeWork, 1) {
		f.code = WorkLimit
		return false
	}
	if f.n == MaxEdges {
		f.code = SegmentLimit
		return false
	}
	f.sc.edges[f.n] = edge{a, b}
	f.n++
	f.sc.touch(uint64(f.n * 32))
	return true
}

// Distance to a finite segment, not its supporting line: this also bounds
// degenerate chords and controls which backtrack beyond either endpoint.
func distance(p, a, b Point) float64 {
	dx, dy := b.X-a.X, b.Y-a.Y
	t := 0.0
	if d := dx*dx + dy*dy; d != 0 {
		t = ((p.X-a.X)*dx + (p.Y-a.Y)*dy) / d
		t = math.Max(0, math.Min(1, t))
	}
	return math.Hypot(p.X-a.X-t*dx, p.Y-a.Y-t*dy)
}
func midpoint(a, b Point) Point { return Point{(a.X + b.X) * .5, (a.Y + b.Y) * .5} }

func (f *flattener) curve(a, b, c, d Point, cubic bool, depth int) bool {
	if !f.m.take(subdivideWork, 1) {
		f.code = WorkLimit
		return false
	}
	end := c
	flat := distance(b, a, c) <= 1.0/16
	if cubic {
		end = d
		flat = distance(b, a, d) <= 1.0/16 && distance(c, a, d) <= 1.0/16
	}
	if flat {
		return f.emit(a, end)
	}
	if depth == 12 {
		f.code = CurveLimit
		return false
	}
	ab, bc := midpoint(a, b), midpoint(b, c)
	abc := midpoint(ab, bc)
	if !cubic {
		return f.curve(a, ab, abc, Point{}, false, depth+1) && f.curve(abc, bc, c, Point{}, false, depth+1)
	}
	cd := midpoint(c, d)
	bcd := midpoint(bc, cd)
	center := midpoint(abc, bcd)
	return f.curve(a, ab, abc, center, true, depth+1) && f.curve(center, bcd, cd, d, true, depth+1)
}

func expand(s Scene, sc scratch, m *meter) (int, Failure) {
	f := flattener{sc: sc, m: m}
	for pi, p := range s.Paints {
		first := f.n
		var cur, start Point
		open, closed := false, false
		for ci := int(p.First); ci < int(p.First)+int(p.Count); ci++ {
			f.pi, f.ci = pi, ci
			c := s.Commands[ci]
			for k := 0; k < arity(c.Verb); k++ {
				if !m.take(transformWork, 1) {
					return f.n, failure(WorkLimit, pi, ci)
				}
				c.P[k] = mapped(p.Transform, c.P[k])
			}
			ok := true
			switch c.Verb {
			case Move:
				if open && !closed {
					ok = f.emit(cur, start)
				}
				cur, start, open, closed = c.P[0], c.P[0], true, false
			case Line:
				ok = f.emit(cur, c.P[0])
				cur, closed = c.P[0], false
			case Quad:
				ok = f.curve(cur, c.P[0], c.P[1], Point{}, false, 0)
				cur, closed = c.P[1], false
			case Cubic:
				ok = f.curve(cur, c.P[0], c.P[1], c.P[2], true, 0)
				cur, closed = c.P[2], false
			case Close:
				ok = f.emit(cur, start)
				cur, closed = start, true
			}
			if !ok {
				return f.n, failure(f.code, pi, ci)
			}
		}
		if open && !closed && !f.emit(cur, start) {
			return f.n, failure(f.code, pi, int(p.First)+int(p.Count)-1)
		}
		if !m.take(edgeWork, 1) { // fixed-size paint plan write
			return f.n, failure(WorkLimit, pi, -1)
		}
		sc.plans[pi] = paintPlan{uint64(first), uint64(f.n - first)}
		sc.touch(uint64(MaxEdges*48 + 1024*8 + (pi+1)*16))
	}
	return f.n, success()
}

// In-place heapsort gives a deterministic O(n log n) worst-case bound. Every
// comparison, swap (two record moves), and saved-record move is metered.
func sift(a []crossing, root, end int, m *meter) bool {
	for root*2+1 < end {
		child := root*2 + 1
		if child+1 < end {
			if !m.take(sortCompareWork, 1) {
				return false
			}
			if a[child].x < a[child+1].x {
				child++
			}
		}
		if !m.take(sortCompareWork, 1) {
			return false
		}
		if a[root].x >= a[child].x {
			break
		}
		if !m.take(sortMoveWork, 3) {
			return false
		}
		a[root], a[child] = a[child], a[root]
		root = child
	}
	return true
}
func sortCross(a []crossing, m *meter) bool {
	for i := len(a)/2 - 1; i >= 0; i-- {
		if !sift(a, i, len(a), m) {
			return false
		}
	}
	for end := len(a) - 1; end > 0; end-- {
		if !m.take(sortMoveWork, 3) {
			return false
		}
		a[0], a[end] = a[end], a[0]
		if !sift(a, 0, end, m) {
			return false
		}
	}
	return true
}

func addSpan(row []float64, a, b float64, clip Rect, m *meter) bool {
	if !m.take(spanWork, 1) {
		return false
	}
	a, b = math.Max(a, float64(clip.X0)), math.Min(b, float64(clip.X1))
	if b <= a {
		return true
	}
	for x, end := int(math.Floor(a)), int(math.Ceil(b)); x < end; x++ {
		if !m.take(coverageWork, 1) {
			return false
		}
		row[x] += math.Min(b, float64(x+1)) - math.Max(a, float64(x))
	}
	return true
}

func sample(s Scene, dst Target, sc scratch, m *meter, write bool) Failure {
	for pi, p := range s.Paints {
		clip := Rect{max(0, p.Clip.X0), max(0, p.Clip.Y0), min(dst.Width, p.Clip.X1), min(dst.Height, p.Clip.Y1)}
		pl := sc.plans[pi]
		edges := sc.edges[pl.first : pl.first+pl.count]
		// Still expand/charge hidden and clipped geometry, but no pixel work.
		if clip.X1 <= clip.X0 || clip.Y1 <= clip.Y0 || p.Color>>24 == 0 || len(edges) == 0 {
			continue
		}
		low, high := float64(clip.Y1), float64(clip.Y0)
		for _, e := range edges {
			if !m.take(edgeTestWork, 1) {
				return failure(WorkLimit, pi, -1)
			}
			low = math.Min(low, math.Min(e.a.Y, e.b.Y))
			high = math.Max(high, math.Max(e.a.Y, e.b.Y))
		}
		y0, y1 := max(clip.Y0, int(math.Floor(low))), min(clip.Y1, int(math.Ceil(high)))
		for y := y0; y < y1; y++ {
			if !m.take(zeroWork, uint64(dst.Width*8)) {
				return failure(WorkLimit, pi, -1)
			}
			clear(sc.row[:dst.Width])
			for sub := 0; sub < 4; sub++ {
				ys := float64(y) + float64(2*sub+1)/8
				n := 0
				for _, e := range edges {
					if !m.take(edgeTestWork, 1) {
						return failure(WorkLimit, pi, -1)
					}
					if (e.a.Y <= ys && ys < e.b.Y) || (e.b.Y <= ys && ys < e.a.Y) {
						if !m.take(crossingWork, 1) {
							return failure(WorkLimit, pi, -1)
						}
						dir := int64(1)
						if e.b.Y < e.a.Y {
							dir = -1
						}
						sc.cross[n] = crossing{e.a.X + (ys-e.a.Y)*(e.b.X-e.a.X)/(e.b.Y-e.a.Y), dir}
						n++
					}
				}
				cross := sc.cross[:n]
				if !sortCross(cross, m) {
					return failure(WorkLimit, pi, -1)
				}
				wind := int64(0)
				for k := 0; k < n; {
					x := cross[k].x
					for k < n && cross[k].x == x {
						if !m.take(windingWork, 1) {
							return failure(WorkLimit, pi, -1)
						}
						wind += cross[k].dir
						k++
					}
					inside := wind != 0
					if p.Rule == EvenOdd {
						inside = wind&1 != 0
					}
					if inside && k < n && !addSpan(sc.row, x, cross[k].x, clip, m) {
						return failure(WorkLimit, pi, -1)
					}
				}
			}
			for x := clip.X0; x < clip.X1; x++ {
				if !m.take(coverageWork, 1) { // normalize the accumulated row
					return failure(WorkLimit, pi, -1)
				}
				c := uint32(math.Min(255, math.Floor(sc.row[x]*255/4+.5)))
				if c != 0 {
					if !m.take(blendWork, 1) {
						return failure(WorkLimit, pi, -1)
					}
					if write {
						i := y*dst.Stride + x
						dst.Pix[i] = blend(dst.Pix[i], p.Color, c)
					}
				}
			}
		}
	}
	return success()
}

func blend(dst, src, coverage uint32) uint32 {
	sa := ((src>>24)*coverage + 127) / 255
	if sa == 0 {
		return dst
	}
	da := dst >> 24
	den := sa*255 + da*(255-sa)
	if den == 0 {
		return 0
	}
	out := ((den + 127) / 255) << 24
	for shift := uint(0); shift < 24; shift += 8 {
		ch := (((src>>shift)&255)*sa*255 + ((dst>>shift)&255)*da*(255-sa) + den/2) / den
		out |= ch << shift
	}
	return out
}
