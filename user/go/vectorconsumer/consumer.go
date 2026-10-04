// Package vectorconsumer is an independent caller of the ADR 0041 API.
// It deliberately imports neither SVG nor any renderer-private state.
package vectorconsumer

import "virelai/vector"

const Sentinel uint32 = 0x71345678

var Names = [...]string{"curves", "nonzero", "evenodd", "affine", "clip", "alpha", "page", "padding", "empty"}

func command(v vector.Verb, p ...vector.Point) vector.Command {
	c := vector.Command{Verb: v}
	for i := range p {
		c.P[i] = p[i]
	}
	return c
}

// Build uses only borrowed storage. Curves are authored here, not parsed from
// the corresponding host-only SVG reference. Coordinates use pixel edges.
func Build(id string, out vector.Storage) (vector.Scene, int, int, int, uint32) {
	c, p := out.Commands, out.Paints
	w, h, stride, bg := 64, 64, 64, uint32(0)
	n := 0
	put := func(v vector.Verb, pts ...vector.Point) {
		c[n] = command(v, pts...)
		n++
	}
	rect := func(x0, y0, x1, y1 float64, reverse bool) {
		put(vector.Move, vector.Point{X: x0, Y: y0})
		if reverse {
			put(vector.Line, vector.Point{X: x0, Y: y1})
			put(vector.Line, vector.Point{X: x1, Y: y1})
			put(vector.Line, vector.Point{X: x1, Y: y0})
		} else {
			put(vector.Line, vector.Point{X: x1, Y: y0})
			put(vector.Line, vector.Point{X: x1, Y: y1})
			put(vector.Line, vector.Point{X: x0, Y: y1})
		}
		put(vector.Close)
	}
	p[0] = vector.Paint{Transform: vector.Affine{A: 1, D: 1},
		Clip: vector.Rect{X1: w, Y1: h}, Color: 0xff2468ac}
	paints := 1
	switch id {
	case "curves":
		put(vector.Move, vector.Point{X: 4, Y: 32})
		put(vector.Quad, vector.Point{X: 16, Y: 4}, vector.Point{X: 32, Y: 32})
		put(vector.Cubic, vector.Point{X: 40, Y: 60}, vector.Point{X: 52, Y: 4}, vector.Point{X: 60, Y: 32})
		put(vector.Line, vector.Point{X: 60, Y: 60})
		put(vector.Line, vector.Point{X: 4, Y: 60})
		put(vector.Close)
	case "nonzero", "evenodd":
		rect(4, 4, 60, 60, false)
		rect(16, 16, 48, 48, id == "nonzero")
		if id == "evenodd" {
			p[0].Rule = vector.EvenOdd
		}
	case "affine":
		rect(6, 6, 30, 24, false)
		p[0].Transform = vector.Affine{A: 96.0 / 72, D: -96.0 / 72, E: 8, F: 56}
	case "clip":
		rect(-10, -10, 80, 80, false)
		p[0].Clip = vector.Rect{X0: 7, Y0: 13, X1: 47, Y1: 53}
	case "alpha":
		bg = 0x80402010
		rect(8, 8, 56, 56, false)
		p[0].Color = 0x80804020
	case "page":
		w, h, stride, bg = 1024, 1536, 1024, 0xffffffff
		rect(0, 0, 1024, 1536, false)
		p[0].Clip = vector.Rect{X1: w, Y1: h}
		p[0].Count = uint32(n)
		p[0].Color = 0xff2468ac
		first := n
		rect(1000, 1535, 1024, 1536, false)
		p[1] = vector.Paint{First: uint32(first), Count: uint32(n - first),
			Transform: vector.Affine{A: 1, D: 1}, Clip: p[0].Clip, Color: 0xffff0000}
		paints = 2
	case "padding":
		stride, bg = 80, Sentinel
		rect(0, 0, 64, 64, false)
	case "empty":
		return vector.Scene{}, w, h, stride, Sentinel
	default:
		return vector.Scene{}, 0, 0, 0, 0
	}
	if paints == 1 {
		p[0].Count = uint32(n)
	}
	return vector.Scene{Commands: c[:n], Paints: p[:paints]}, w, h, stride, bg
}

// Analytic expectations are independent of the rasterizer and its checksums.
// Curves additionally require the pinned external filled-path reference.
func Check(id string, dst vector.Target) bool {
	if id == "curves" {
		return dst.Pix[58*dst.Stride+32] == 0xff2468ac && dst.Pix[0] == 0
	}
	for y := 0; y < dst.Height; y++ {
		for x := 0; x < dst.Stride; x++ {
			want := uint32(0)
			switch id {
			case "nonzero", "evenodd":
				if x >= 4 && x < 60 && y >= 4 && y < 60 && !(x >= 16 && x < 48 && y >= 16 && y < 48) {
					want = 0xff2468ac
				}
			case "affine":
				if x >= 16 && x < 48 && y >= 24 && y < 48 {
					want = 0xff2468ac
				}
			case "clip":
				if x >= 7 && x < 47 && y >= 13 && y < 53 {
					want = 0xff2468ac
				}
			case "alpha":
				want = 0x80402010
				if x >= 8 && x < 56 && y >= 8 && y < 56 {
					// sa=da=128, den=48896; integer ADR source-over.
					want = 0xc06b351b
				}
			case "page":
				want = 0xff2468ac
				if y == 1535 && x >= 1000 {
					want = 0xffff0000
				}
			case "padding":
				want = Sentinel
				if x < dst.Width {
					want = 0xff2468ac
				}
			case "empty":
				want = Sentinel
			}
			if dst.Pix[y*dst.Stride+x] != want {
				return false
			}
		}
	}
	return true
}

// Failures exercises every direct-call error reachable through the public
// contract. SVG-only codes and caller Memory/Time admission have no direct
// Rasterize trigger. All Code messages are checked, not invented triggers.
func Failures(out vector.Storage, dst vector.Target, ws vector.Workspace) bool {
	for code := vector.OK; code <= vector.InvalidBuffer; code++ {
		f := vector.Failure{Code: code, Offset: -1, Paint: -1, Command: -1}
		if len(f.Error()) > 128 || (f.Error() == "") != (code == vector.OK) {
			return false
		}
	}
	run := func(s vector.Scene, d vector.Target, scratch vector.Workspace, b *vector.Budget, want vector.Code) bool {
		for i := range dst.Pix {
			dst.Pix[i] = Sentinel
		}
		stats, f := vector.Rasterize(s, d, scratch, b)
		if f.Code != want || f.Offset != -1 || b != nil && b.Used <= b.Max && stats.Work > b.Used {
			return false
		}
		for _, pixel := range dst.Pix {
			if pixel != Sentinel {
				return false
			}
		}
		return true
	}
	fresh := func() *vector.Budget { return &vector.Budget{Max: vector.MaxWork} }
	s, _, _, _, _ := Build("clip", out)
	if !run(s, dst, ws, nil, vector.WorkLimit) ||
		!run(s, dst, ws, &vector.Budget{Max: vector.MaxWork + 1}, vector.WorkLimit) ||
		!run(s, dst, ws, &vector.Budget{Used: 2, Max: 1}, vector.WorkLimit) ||
		!run(s, vector.Target{Width: 1025, Height: 1, Stride: 1025}, ws, fresh(), vector.CanvasLimit) ||
		!run(s, vector.Target{Width: 1, Height: 1537, Stride: 1}, ws, fresh(), vector.CanvasLimit) ||
		!run(s, vector.Target{Width: 1, Height: 1, Stride: int(^uint(0) >> 1)}, ws, fresh(), vector.InvalidBuffer) ||
		!run(s, vector.Target{Width: 1, Height: 1, Stride: 1}, ws, fresh(), vector.InvalidBuffer) ||
		!run(s, dst, vector.Workspace{Bytes: ws.Bytes[:vector.WorkspaceSize-1]}, fresh(), vector.ScratchLimit) ||
		!run(s, dst, vector.Workspace{Bytes: ws.Bytes[1:]}, fresh(), vector.ScratchLimit) ||
		!run(s, dst, ws, &vector.Budget{Max: 10}, vector.WorkLimit) {
		return false
	}
	for _, mutate := range [...]int{0, 1, 2, 3, 4, 5} {
		s, _, _, _, _ = Build("clip", out)
		want := vector.Malformed
		switch mutate {
		case 0:
			s.Commands[0].Verb = vector.Line
		case 1:
			s.Commands[1].Verb = vector.Verb(255)
		case 2:
			s.Paints[0].Rule = vector.Rule(255)
		case 3:
			s.Paints[0].Clip.X1 = -1
		case 4:
			s.Commands[1].P[0].X = 32769
			want = vector.CoordinateLimit
		case 5:
			s.Paints[0].Transform.A = 257
			want = vector.CoordinateLimit
		}
		if !run(s, dst, ws, fresh(), want) {
			return false
		}
	}
	// A later malformed paint must not leave the earlier valid paint behind.
	s, _, _, _, _ = Build("page", out)
	s.Commands[5].Verb = vector.Line
	if !run(s, dst, ws, fresh(), vector.Malformed) {
		return false
	}
	for i := range out.Commands {
		out.Commands[i] = vector.Command{Verb: vector.Line}
	}
	out.Commands[0].Verb = vector.Move
	out.Paints[0] = vector.Paint{Count: vector.MaxCommands, Transform: vector.Affine{A: 1, D: 1}}
	s = vector.Scene{Commands: out.Commands[:vector.MaxCommands], Paints: out.Paints[:1]}
	if !run(s, dst, ws, fresh(), vector.OK) {
		return false
	}
	if len(out.Commands) > vector.MaxCommands && !run(vector.Scene{Commands: out.Commands}, dst, ws, fresh(), vector.CommandLimit) {
		return false
	}
	out.Commands[vector.MaxCommands-1] = command(vector.Quad, vector.Point{X: 1, Y: 1}, vector.Point{X: 2})
	if !run(s, dst, ws, fresh(), vector.SegmentLimit) {
		return false
	}
	for i := 0; i <= vector.MaxContours; i++ {
		out.Commands[i] = vector.Command{Verb: vector.Move}
	}
	out.Paints[0].Count = vector.MaxContours + 1
	if !run(vector.Scene{Commands: out.Commands[:vector.MaxContours+1], Paints: out.Paints[:1]}, dst, ws, fresh(), vector.ContourLimit) {
		return false
	}
	if len(out.Paints) > vector.MaxPaints && !run(vector.Scene{Paints: out.Paints}, dst, ws, fresh(), vector.SceneLimit) {
		return false
	}
	return true
}
