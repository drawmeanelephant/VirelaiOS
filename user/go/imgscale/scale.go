// Package imgscale resamples row-major 0xAARRGGBB rasters with integer math.
// Both-axis downscales use an exact area filter; every other ratio uses
// edge-clamped bilinear interpolation. Channels are weighted premultiplied so
// transparent texels cannot darken the visible edge color.
package imgscale

import "math/bits"

// Scaler is the fixed-point resampling plan for one source and destination
// geometry. Pixel coordinates passed to Pixel are destination-relative.
type Scaler struct {
	sw, sh int64
	dw, dh int64
}

// New prepares the resampling policy for srcW x srcH source texels painted
// into dstW x dstH destination pixels. Nonpositive dimensions yield a scaler
// whose OK is false.
func New(srcW, srcH, dstW, dstH int) Scaler {
	if srcW <= 0 || srcH <= 0 || dstW <= 0 || dstH <= 0 {
		return Scaler{}
	}
	return Scaler{sw: int64(srcW), sh: int64(srcH), dw: int64(dstW), dh: int64(dstH)}
}

// OK reports whether the geometry can resample.
func (s Scaler) OK() bool {
	return s.sw > 0 && s.sh > 0 && s.dw > 0 && s.dh > 0
}

// Area reports whether both axes downscale, selecting the exact footprint
// filter. A one-axis shrink keeps the bilinear tap pair.
func (s Scaler) Area() bool {
	return s.OK() && s.dw < s.sw && s.dh < s.sh
}

// Pixel returns the straight-alpha resampled color at destination-relative
// (x, y). pix must contain at least srcW*srcH words; geometry validation and
// clipping belong to the caller, as in the draw canvas that owned this policy.
func (s Scaler) Pixel(pix []uint32, x, y int) uint32 {
	return s.pixel(pix, x, y, false)
}

// PixelOpaque is Pixel for a logically opaque RGB frame whose alpha byte is
// inert storage rather than coverage. Every tap contributes as alpha 255.
func (s Scaler) PixelOpaque(pix []uint32, x, y int) uint32 {
	return s.pixel(pix, x, y, true)
}

func (s Scaler) pixel(pix []uint32, x, y int, opaque bool) uint32 {
	if !s.OK() {
		return 0
	}
	x = min(max(x, 0), int(s.dw)-1)
	y = min(max(y, 0), int(s.dh)-1)
	if s.Area() {
		return s.areaPixel(pix, x, y, opaque)
	}
	return s.bilinearPixel(pix, x, y, opaque)
}

// scaleCoord returns the 16.16 source-space position of destination pixel
// rel's center: (rel + 1/2)·srcLen/dstLen − 1/2. The 128/64 divide keeps
// the mapping exact at any integer ratio without floating point.
func scaleCoord(rel, srcLen, dstLen int64) int64 {
	rel = min(max(rel, 0), dstLen-1)
	hi, lo := bits.Mul64(uint64(rel)*2+1, uint64(srcLen))
	hi = hi<<16 | lo>>48
	lo <<= 16
	q, _ := bits.Div64(hi, lo, uint64(dstLen)*2)
	return int64(q) - 32768
}

// edgeIndex clamps the bilinear tap pair at floor index i to [0, last];
// out-of-range taps collapse onto the edge pixel so every weight lands on
// real source data.
func edgeIndex(i, last int64) (int64, int64) {
	a := min(max(i, 0), last)
	b := min(max(i+1, 0), last)
	return a, b
}

func tapAlpha(p uint32, opaque bool) int64 {
	if opaque {
		return 255
	}
	return int64(p >> 24)
}

func (s Scaler) bilinearPixel(pix []uint32, x, y int, opaque bool) uint32 {
	sy := scaleCoord(int64(y), s.sh, s.dh)
	fy := int(sy & 0xffff)
	iy0, iy1 := edgeIndex(sy>>16, s.sh-1)
	row0, row1 := int(iy0)*int(s.sw), int(iy1)*int(s.sw)
	sx := scaleCoord(int64(x), s.sw, s.dw)
	fx := int(sx & 0xffff)
	ix0, ix1 := edgeIndex(sx>>16, s.sw-1)
	p00 := pix[row0+int(ix0)]
	p01 := pix[row0+int(ix1)]
	p10 := pix[row1+int(ix0)]
	p11 := pix[row1+int(ix1)]
	w00 := int64(65536-fx) * int64(65536-fy)
	w01 := int64(fx) * int64(65536-fy)
	w10 := int64(65536-fx) * int64(fy)
	w11 := int64(fx) * int64(fy)
	var pa, pr, pg, pb int64
	accum := func(p uint32, w int64) {
		a := tapAlpha(p, opaque)
		pa += a * w
		pr += a * int64(p>>16&0xff) * w
		pg += a * int64(p>>8&0xff) * w
		pb += a * int64(p&0xff) * w
	}
	accum(p00, w00)
	accum(p01, w01)
	accum(p10, w10)
	accum(p11, w11)
	pa = (pa + 1<<31) >> 32
	if pa == 0 {
		return 0
	}
	channel := func(pm int64) uint32 {
		pm = (pm + 1<<31) >> 32
		return uint32(min((pm+pa/2)/pa, 255))
	}
	return uint32(pa)<<24 | channel(pr)<<16 | channel(pg)<<8 | channel(pb)
}

// axisWeight normalizes a 16.16 footprint share to the bilinear path's
// 2^16-per-axis convention: v·65536/span for v ≤ span. v is one texel's
// overlap, so the product is bounded by 2^32 and needs no floating point.
func axisWeight(v, span int64) int64 {
	return v * 65536 / span
}

// areaPixel accumulates every source texel under the destination pixel's
// [x0,x1)×[y0,y1) footprint, premultiplied. Footprints tile the source
// exactly, so the tap range never needs edgeIndex's clamp.
func (s Scaler) areaPixel(pix []uint32, x, y int, opaque bool) uint32 {
	fy0 := int64(y) * s.sh * 65536 / s.dh
	fy1 := (int64(y) + 1) * s.sh * 65536 / s.dh
	spanY := fy1 - fy0
	if spanY <= 0 {
		return 0
	}
	fx0 := int64(x) * s.sw * 65536 / s.dw
	fx1 := (int64(x) + 1) * s.sw * 65536 / s.dw
	spanX := fx1 - fx0
	if spanX <= 0 {
		return 0
	}
	var pa, pr, pg, pb, wsum int64
	fullX, fullY := axisWeight(65536, spanX), axisWeight(65536, spanY)
	for jy := fy0 >> 16; jy <= (fy1-1)>>16; jy++ {
		wy := min(fy1, (jy+1)<<16) - max(fy0, jy<<16)
		wyn := fullY
		if wy != 65536 {
			wyn = axisWeight(wy, spanY)
		}
		row := jy * s.sw
		for jx := fx0 >> 16; jx <= (fx1-1)>>16; jx++ {
			wx := min(fx1, (jx+1)<<16) - max(fx0, jx<<16)
			wxn := fullX
			if wx != 65536 {
				wxn = axisWeight(wx, spanX)
			}
			w := wxn * wyn
			p := pix[row+jx]
			a := tapAlpha(p, opaque)
			pa += a * w
			pr += a * int64(p>>16&0xff) * w
			pg += a * int64(p>>8&0xff) * w
			pb += a * int64(p&0xff) * w
			wsum += w
		}
	}
	if wsum == 0 {
		return 0
	}
	pa = (pa + wsum/2) / wsum
	if pa == 0 {
		return 0
	}
	channel := func(pm int64) uint32 {
		pm = (pm + wsum/2) / wsum
		return uint32(min((pm+pa/2)/pa, 255))
	}
	return uint32(pa)<<24 | channel(pr)<<16 | channel(pg)<<8 | channel(pb)
}
