package ttf

import "math"

// Outline decoding: glyf -> contours of points, with quadratic on/off-curve
// runs flattened to polylines. Everything here is bounds-checked against the
// glyph's own byte slice; a glyph that lies about its own length produces an
// error or an empty outline, never a panic or an out-of-range read.

// rawPoint is one outline point in font design units.
type rawPoint struct {
	x, y int32
	on   bool
}

// pointF is a point in pixel space.
type pointF struct{ x, y float64 }

// Component flags for composite glyphs.
const (
	compArg1And2AreWords = 0x0001
	compArgsAreXYValues  = 0x0002
	compHaveScale        = 0x0008
	compMoreComponents   = 0x0020
	compHaveXAndYScale   = 0x0040
	compHaveTwoByTwo     = 0x0080
)

// maxComponentDepth bounds composite recursion. A font whose composite graph
// is cyclic (a hostile one can be) hits this and yields what it has so far.
const maxComponentDepth = 8

// maxGlyphPoints bounds one glyph's point count — and, as of M97f F5
// (#2108), the TOTAL a glyph may resolve to across composite expansion.
// The simple-glyph check alone bounded each component, not the fan-out: a
// hostile composite multiplies it by fan^depth (the audit's 440-byte probe
// reached 8.4M points / ~96 MiB). The budget is a single ledger shared by
// every resolution under glyphContours, so a glyph resolves to at most
// maxGlyphPoints however it was composed. Far above any real glyph
// (Inter's largest is a few hundred) and small enough that a corrupted
// count cannot ask for a huge allocation.
const maxGlyphPoints = 4096

// glyphContours decodes a glyph into contours of raw font-unit points.
// Composite glyphs are resolved into their components' points with the
// component transform applied. An empty glyph returns (nil, nil). A glyph
// whose resolved total exceeds maxGlyphPoints is refused wholesale —
// ErrMalformed, no partial outline.
func (f *Face) glyphContours(gid uint16) ([][]rawPoint, error) {
	data, err := f.GlyphData(gid)
	if err != nil {
		return nil, err
	}
	if len(data) == 0 {
		return nil, nil
	}
	budget := maxGlyphPoints
	return f.contoursFrom(data, 0, &budget)
}

func (f *Face) contoursFrom(data []byte, depth int, budget *int) ([][]rawPoint, error) {
	if len(data) < 10 {
		return nil, ErrMalformed
	}
	nc := int(int16(be16(data[0:2])))
	switch {
	case nc >= 0:
		return f.simpleContours(data, nc)
	case nc == -1:
		if depth >= maxComponentDepth {
			return nil, ErrMalformed
		}
		return f.compositeContours(data, depth, budget)
	default:
		return nil, ErrMalformed
	}
}

// simpleContours decodes a simple glyph: endPtsOfContours, instructions, the
// flag run (with REPEAT), then the x and y delta streams.
func (f *Face) simpleContours(data []byte, nc int) ([][]rawPoint, error) {
	if nc == 0 {
		return nil, nil
	}
	// endPtsOfContours
	if 10+nc*2 > len(data) {
		return nil, ErrMalformed
	}
	ends := make([]int, nc)
	for i := 0; i < nc; i++ {
		ends[i] = int(be16(data[10+2*i : 12+2*i]))
	}
	numPoints := ends[nc-1] + 1
	if numPoints <= 0 || numPoints > maxGlyphPoints {
		return nil, ErrMalformed
	}
	// Monotonic end points, or the contour split below would go backwards.
	for i := 1; i < nc; i++ {
		if ends[i] < ends[i-1] {
			return nil, ErrMalformed
		}
	}

	p := 10 + nc*2
	instLen := int(be16(data[p : p+2]))
	p += 2
	if instLen < 0 || p+instLen > len(data) {
		return nil, ErrMalformed
	}
	p += instLen

	flags := make([]byte, numPoints)
	for i := 0; i < numPoints; {
		if p >= len(data) {
			return nil, ErrMalformed
		}
		fl := data[p]
		p++
		flags[i] = fl
		i++
		if fl&0x08 != 0 { // REPEAT
			if p >= len(data) {
				return nil, ErrMalformed
			}
			rep := int(data[p])
			p++
			for j := 0; j < rep && i < numPoints; j++ {
				flags[i] = fl
				i++
			}
		}
	}

	xs := make([]int32, numPoints)
	var x int32
	for i := 0; i < numPoints; i++ {
		fl := flags[i]
		switch {
		case fl&0x02 != 0: // X_SHORT_VECTOR
			if p >= len(data) {
				return nil, ErrMalformed
			}
			d := int32(data[p])
			p++
			if fl&0x10 == 0 { // sign bit: 0 = positive
				d = -d
			}
			x += d
		case fl&0x10 == 0: // not short, not same => int16 delta
			if p+2 > len(data) {
				return nil, ErrMalformed
			}
			x += int32(int16(be16(data[p : p+2])))
			p += 2
		}
		xs[i] = x
	}

	ys := make([]int32, numPoints)
	var y int32
	for i := 0; i < numPoints; i++ {
		fl := flags[i]
		switch {
		case fl&0x04 != 0: // Y_SHORT_VECTOR
			if p >= len(data) {
				return nil, ErrMalformed
			}
			d := int32(data[p])
			p++
			if fl&0x20 == 0 {
				d = -d
			}
			y += d
		case fl&0x20 == 0:
			if p+2 > len(data) {
				return nil, ErrMalformed
			}
			y += int32(int16(be16(data[p : p+2])))
			p += 2
		}
		ys[i] = y
	}

	all := make([]rawPoint, numPoints)
	for i := range all {
		all[i] = rawPoint{x: xs[i], y: ys[i], on: flags[i]&0x01 != 0}
	}

	contours := make([][]rawPoint, 0, nc)
	start := 0
	for i := 0; i < nc; i++ {
		end := ends[i]
		if end < start || end >= numPoints {
			return nil, ErrMalformed
		}
		contours = append(contours, all[start:end+1])
		start = end + 1
	}
	return contours, nil
}

// compositeContours resolves a composite glyph: each component names another
// glyph whose points are transformed (2x2 F2Dot14 plus an offset) and merged.
// budget is the shared resolved-points ledger (M97f F5, #2108): every merged
// contour debits it, and overspend refuses the whole glyph.
func (f *Face) compositeContours(data []byte, depth int, budget *int) ([][]rawPoint, error) {
	p := 10
	var out [][]rawPoint
	for {
		if p+4 > len(data) {
			return nil, ErrMalformed
		}
		flags := be16(data[p : p+2])
		sub := be16(data[p+2 : p+4])
		p += 4

		var dx, dy int32
		if flags&compArg1And2AreWords != 0 {
			if p+4 > len(data) {
				return nil, ErrMalformed
			}
			if flags&compArgsAreXYValues != 0 {
				dx = int32(int16(be16(data[p : p+2])))
				dy = int32(int16(be16(data[p+2 : p+4])))
			}
			p += 4
		} else {
			if p+2 > len(data) {
				return nil, ErrMalformed
			}
			if flags&compArgsAreXYValues != 0 {
				dx = int32(int8(data[p]))
				dy = int32(int8(data[p+1]))
			}
			p += 2
		}
		// Point-matching components (ARGS_ARE_XY_VALUES clear) are rare and
		// need the parent's point array; treat the offset as zero and say so
		// in the package doc rather than guess a wrong registration.

		a, b, c, d := 1.0, 0.0, 0.0, 1.0
		if flags&compHaveScale != 0 {
			if p+2 > len(data) {
				return nil, ErrMalformed
			}
			s := float64(int16(be16(data[p:p+2]))) / 16384.0
			a, d = s, s
			p += 2
		} else if flags&compHaveXAndYScale != 0 {
			if p+4 > len(data) {
				return nil, ErrMalformed
			}
			a = float64(int16(be16(data[p:p+2]))) / 16384.0
			d = float64(int16(be16(data[p+2:p+4]))) / 16384.0
			p += 4
		} else if flags&compHaveTwoByTwo != 0 {
			if p+8 > len(data) {
				return nil, ErrMalformed
			}
			a = float64(int16(be16(data[p:p+2]))) / 16384.0
			b = float64(int16(be16(data[p+2:p+4]))) / 16384.0
			c = float64(int16(be16(data[p+4:p+6]))) / 16384.0
			d = float64(int16(be16(data[p+6:p+8]))) / 16384.0
			p += 8
		}

		subData, err := f.GlyphData(sub)
		if err != nil {
			// A dangling component reference: skip it, keep the rest.
			if flags&compMoreComponents == 0 {
				break
			}
			continue
		}
		if len(subData) != 0 {
			subs, err := f.contoursFrom(subData, depth+1, budget)
			if err != nil {
				return nil, err
			}
			for _, ct := range subs {
				*budget -= len(ct)
				if *budget < 0 {
					return nil, ErrMalformed
				}
				moved := make([]rawPoint, len(ct))
				for i, pt := range ct {
					fx, fy := float64(pt.x), float64(pt.y)
					// Unscaled component offsets are only subtle for rotated
					// 2x2 components; applying the transform to the offset is
					// the correct reading whenever SCALED_COMPONENT_OFFSET is
					// set or no explicit flag is given (the common case).
					moved[i] = rawPoint{
						x:  int32(math.Round(a*fx+c*fy)) + dx,
						y:  int32(math.Round(b*fx+d*fy)) + dy,
						on: pt.on,
					}
				}
				out = append(out, moved)
			}
		}
		if flags&compMoreComponents == 0 {
			break
		}
	}
	return out, nil
}

// flattenContours scales raw font-unit contours to pixel space and flattens
// the quadratic runs into closed polylines. tol is the maximum allowed
// deviation of a chord from its curve, in pixels.
//
// TrueType outlines are Y-UP (the baseline is y=0 and ascenders are positive),
// while every surface this project paints into is Y-DOWN. The sign flip lives
// here, in the one place font units become pixels, so nothing downstream has to
// remember which way the font points. Negating y reverses each contour's
// orientation, which the non-zero winding fill rule tolerates: relative
// orientation — and therefore counters like the hole in 'o' — is preserved.
func flattenContours(contours [][]rawPoint, scale float64, tol float64) [][]pointF {
	out := make([][]pointF, 0, len(contours))
	for _, ct := range contours {
		poly := flattenContour(ct, scale, tol)
		if len(poly) >= 3 {
			out = append(out, poly)
		}
	}
	return out
}

func flattenContour(pts []rawPoint, scale float64, tol float64) []pointF {
	n := len(pts)
	if n == 0 {
		return nil
	}
	sc := func(p rawPoint) pointF {
		// Y-DOWN pixels from Y-UP font units: see flattenContours.
		return pointF{x: float64(p.x) * scale, y: -float64(p.y) * scale}
	}
	mid := func(a, b rawPoint) rawPoint {
		return rawPoint{x: (a.x + b.x) / 2, y: (a.y + b.y) / 2, on: true}
	}

	// Rotate so ordered[0] is on-curve; when the contour has no on-curve
	// point at all, synthesize one at the midpoint of the last/first pair.
	var ordered []rawPoint
	start := -1
	for i := 0; i < n; i++ {
		if pts[i].on {
			start = i
			break
		}
	}
	if start >= 0 {
		ordered = make([]rawPoint, 0, n)
		for k := 0; k < n; k++ {
			ordered = append(ordered, pts[(start+k)%n])
		}
	} else {
		ordered = make([]rawPoint, 0, n+1)
		ordered = append(ordered, mid(pts[n-1], pts[0]))
		ordered = append(ordered, pts...)
	}

	var poly []pointF
	cur := sc(ordered[0])
	poly = append(poly, cur)
	i := 1
	for i < len(ordered) {
		p := ordered[i]
		if p.on {
			cur = sc(p)
			poly = append(poly, cur)
			i++
			continue
		}
		ctrl := sc(p)
		var endP pointF
		if i+1 < len(ordered) {
			next := ordered[i+1]
			if next.on {
				endP = sc(next)
				i += 2
			} else {
				endP = sc(mid(p, next))
				i++
			}
		} else {
			// The contour closes back onto its first point.
			endP = sc(ordered[0])
			i++
		}
		flattenQuad(cur, ctrl, endP, tol, 0, &poly)
		cur = endP
	}
	// Drop a duplicated closing point.
	if len(poly) >= 2 {
		last, first := poly[len(poly)-1], poly[0]
		if math.Abs(last.x-first.x) < 1e-9 && math.Abs(last.y-first.y) < 1e-9 {
			poly = poly[:len(poly)-1]
		}
	}
	return poly
}

// flattenQuad appends the quadratic Bezier (p0, c, p1) to out as a polyline,
// subdividing until the control point's deviation from the chord is within tol.
func flattenQuad(p0, c, p1 pointF, tol float64, depth int, out *[]pointF) {
	if depth >= 12 {
		*out = append(*out, p1)
		return
	}
	dx, dy := p1.x-p0.x, p1.y-p0.y
	seg := math.Hypot(dx, dy)
	if seg < 1e-9 {
		// Degenerate chord: fall back to the control point's own distance.
		if math.Hypot(c.x-p0.x, c.y-p0.y) <= tol {
			*out = append(*out, p1)
			return
		}
	} else if math.Abs((c.x-p0.x)*dy-(c.y-p0.y)*dx)/seg <= tol {
		*out = append(*out, p1)
		return
	}
	m0 := pointF{x: (p0.x + c.x) / 2, y: (p0.y + c.y) / 2}
	m1 := pointF{x: (c.x + p1.x) / 2, y: (c.y + p1.y) / 2}
	m := pointF{x: (m0.x + m1.x) / 2, y: (m0.y + m1.y) / 2}
	flattenQuad(p0, m0, m, tol, depth+1, out)
	flattenQuad(m, m1, p1, tol, depth+1, out)
}
