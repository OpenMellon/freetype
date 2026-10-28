// Copyright 2010 The Freetype-Go Authors. All rights reserved.
// Use of this source code is governed by your choice of either the
// FreeType License or the GNU General Public License version 2 (or
// any later version), both of which can be found in the LICENSE file.

package truetype

import (
	"math"

	"golang.org/x/image/math/fixed"
)

func (h *hinter) initializeScaledCVT() {
	h.scaledCVTInitialized = true
	if n := len(h.font.cvt) / 2; n <= cap(h.scaledCVT) {
		h.scaledCVT = h.scaledCVT[:n]
	} else {
		if n < 32 {
			n = 32
		}
		h.scaledCVT = make([]fixed.Int26_6, len(h.font.cvt)/2, n)
	}
	for i := range h.scaledCVT {
		unscaled := uint16(h.font.cvt[2*i])<<8 | uint16(h.font.cvt[2*i+1])
		h.scaledCVT[i] = h.font.scale(h.scale * fixed.Int26_6(int16(unscaled)))
	}
}

// getScaledCVT returns the scaled value from the font's Control Value Table.
func (h *hinter) getScaledCVT(i int32) fixed.Int26_6 {
	if !h.scaledCVTInitialized {
		h.initializeScaledCVT()
	}
	if i < 0 || len(h.scaledCVT) <= int(i) {
		return 0
	}
	return h.scaledCVT[i]
}

// setScaledCVT overrides the scaled value from the font's Control Value Table.
func (h *hinter) setScaledCVT(i int32, v fixed.Int26_6) {
	if !h.scaledCVTInitialized {
		h.initializeScaledCVT()
	}
	if i < 0 || len(h.scaledCVT) <= int(i) {
		return
	}
	h.scaledCVT[i] = v
}

func (h *hinter) point(zonePointer uint32, pt pointType, i int32) *Point {
	points := h.points[h.gs.zp[zonePointer]][pt]
	if i < 0 || len(points) <= int(i) {
		return nil
	}
	return &points[i]
}

func (h *hinter) move(p *Point, distance fixed.Int26_6, touch bool) {
	fvx := int64(h.gs.fv[0])
	pvx := int64(h.gs.pv[0])
	if fvx == 0x4000 && pvx == 0x4000 {
		p.X += fixed.Int26_6(distance)
		if touch {
			p.Flags |= flagTouchedX
		}
		return
	}

	fvy := int64(h.gs.fv[1])
	pvy := int64(h.gs.pv[1])
	if fvy == 0x4000 && pvy == 0x4000 {
		p.Y += fixed.Int26_6(distance)
		if touch {
			p.Flags |= flagTouchedY
		}
		return
	}

	fvDotPv := (fvx*pvx + fvy*pvy) >> 14

	if fvx != 0 {
		p.X += fixed.Int26_6(mulDiv(fvx, int64(distance), fvDotPv))
		if touch {
			p.Flags |= flagTouchedX
		}
	}

	if fvy != 0 {
		p.Y += fixed.Int26_6(mulDiv(fvy, int64(distance), fvDotPv))
		if touch {
			p.Flags |= flagTouchedY
		}
	}
}

func (h *hinter) iupInterp(interpY bool, p1, p2, ref1, ref2 int) {
	if p1 > p2 {
		return
	}
	if ref1 >= len(h.points[glyphZone][current]) ||
		ref2 >= len(h.points[glyphZone][current]) {
		return
	}

	var ifu1, ifu2 fixed.Int26_6
	if interpY {
		ifu1 = h.points[glyphZone][inFontUnits][ref1].Y
		ifu2 = h.points[glyphZone][inFontUnits][ref2].Y
	} else {
		ifu1 = h.points[glyphZone][inFontUnits][ref1].X
		ifu2 = h.points[glyphZone][inFontUnits][ref2].X
	}
	if ifu1 > ifu2 {
		ifu1, ifu2 = ifu2, ifu1
		ref1, ref2 = ref2, ref1
	}

	var unh1, unh2, delta1, delta2 fixed.Int26_6
	if interpY {
		unh1 = h.points[glyphZone][unhinted][ref1].Y
		unh2 = h.points[glyphZone][unhinted][ref2].Y
		delta1 = h.points[glyphZone][current][ref1].Y - unh1
		delta2 = h.points[glyphZone][current][ref2].Y - unh2
	} else {
		unh1 = h.points[glyphZone][unhinted][ref1].X
		unh2 = h.points[glyphZone][unhinted][ref2].X
		delta1 = h.points[glyphZone][current][ref1].X - unh1
		delta2 = h.points[glyphZone][current][ref2].X - unh2
	}

	var xy, ifuXY fixed.Int26_6
	if ifu1 == ifu2 {
		for i := p1; i <= p2; i++ {
			if interpY {
				xy = h.points[glyphZone][unhinted][i].Y
			} else {
				xy = h.points[glyphZone][unhinted][i].X
			}

			if xy <= unh1 {
				xy += delta1
			} else {
				xy += delta2
			}

			if interpY {
				h.points[glyphZone][current][i].Y = xy
			} else {
				h.points[glyphZone][current][i].X = xy
			}
		}
		return
	}

	scale, scaleOK := int64(0), false
	for i := p1; i <= p2; i++ {
		if interpY {
			xy = h.points[glyphZone][unhinted][i].Y
			ifuXY = h.points[glyphZone][inFontUnits][i].Y
		} else {
			xy = h.points[glyphZone][unhinted][i].X
			ifuXY = h.points[glyphZone][inFontUnits][i].X
		}

		if xy <= unh1 {
			xy += delta1
		} else if xy >= unh2 {
			xy += delta2
		} else {
			if !scaleOK {
				scaleOK = true
				scale = mulDiv(int64(unh2+delta2-unh1-delta1), 0x10000, int64(ifu2-ifu1))
			}
			numer := int64(ifuXY-ifu1) * scale
			if numer >= 0 {
				numer += 0x8000
			} else {
				numer -= 0x8000
			}
			xy = unh1 + delta1 + fixed.Int26_6(numer/0x10000)
		}

		if interpY {
			h.points[glyphZone][current][i].Y = xy
		} else {
			h.points[glyphZone][current][i].X = xy
		}
	}
}

func (h *hinter) iupShift(interpY bool, p1, p2, p int) {
	var delta fixed.Int26_6
	if interpY {
		delta = h.points[glyphZone][current][p].Y - h.points[glyphZone][unhinted][p].Y
	} else {
		delta = h.points[glyphZone][current][p].X - h.points[glyphZone][unhinted][p].X
	}
	if delta == 0 {
		return
	}
	for i := p1; i < p2; i++ {
		if i == p {
			continue
		}
		if interpY {
			h.points[glyphZone][current][i].Y += delta
		} else {
			h.points[glyphZone][current][i].X += delta
		}
	}
}

func (h *hinter) displacement(useZP1 bool) (zonePointer uint32, i int32, d fixed.Int26_6, ok bool) {
	zonePointer, i = uint32(0), h.gs.rp[1]
	if useZP1 {
		zonePointer, i = 1, h.gs.rp[2]
	}
	p := h.point(zonePointer, current, i)
	q := h.point(zonePointer, unhinted, i)
	if p == nil || q == nil {
		return 0, 0, 0, false
	}
	d = dotProduct(p.X-q.X, p.Y-q.Y, h.gs.pv)
	return zonePointer, i, d, true
}

// skipInstructionPayload increments pc by the extra data that follows a
// variable length PUSHB or PUSHW instruction.
func skipInstructionPayload(program []byte, pc int) (newPC int, ok bool) {
	switch program[pc] {
	case opNPUSHB:
		pc++
		if pc >= len(program) {
			return 0, false
		}
		pc += int(program[pc])
	case opNPUSHW:
		pc++
		if pc >= len(program) {
			return 0, false
		}
		pc += 2 * int(program[pc])
	case opPUSHB000, opPUSHB001, opPUSHB010, opPUSHB011,
		opPUSHB100, opPUSHB101, opPUSHB110, opPUSHB111:
		pc += int(program[pc] - (opPUSHB000 - 1))
	case opPUSHW000, opPUSHW001, opPUSHW010, opPUSHW011,
		opPUSHW100, opPUSHW101, opPUSHW110, opPUSHW111:
		pc += 2 * int(program[pc]-(opPUSHW000-1))
	}
	return pc, true
}

// f2dot14 is a 2.14 fixed point number.
type f2dot14 int16

func normalize(x, y f2dot14) [2]f2dot14 {
	fx, fy := float64(x), float64(y)
	l := 0x4000 / math.Hypot(fx, fy)
	fx *= l
	if fx >= 0 {
		fx += 0.5
	} else {
		fx -= 0.5
	}
	fy *= l
	if fy >= 0 {
		fy += 0.5
	} else {
		fy -= 0.5
	}
	return [2]f2dot14{f2dot14(fx), f2dot14(fy)}
}

// fabs returns abs(x) in 26.6 fixed point arithmetic.
func fabs(x fixed.Int26_6) fixed.Int26_6 {
	if x < 0 {
		return -x
	}
	return x
}

// fdiv returns x/y in 26.6 fixed point arithmetic.
func fdiv(x, y fixed.Int26_6) fixed.Int26_6 {
	return fixed.Int26_6((int64(x) << 6) / int64(y))
}

// fmul returns x*y in 26.6 fixed point arithmetic.
func fmul(x, y fixed.Int26_6) fixed.Int26_6 {
	return fixed.Int26_6((int64(x)*int64(y) + 1<<5) >> 6)
}

// dotProduct returns the dot product of [x, y] and q. It is almost the same as
//
//	px := int64(x)
//	py := int64(y)
//	qx := int64(q[0])
//	qy := int64(q[1])
//	return fixed.Int26_6((px*qx + py*qy + 1<<13) >> 14)
//
// except that the computation is done with 32-bit integers to produce exactly
// the same rounding behavior as C Freetype.
func dotProduct(x, y fixed.Int26_6, q [2]f2dot14) fixed.Int26_6 {
	// Compute x*q[0] as 64-bit value.
	l := uint32((int32(x) & 0xFFFF) * int32(q[0]))
	m := (int32(x) >> 16) * int32(q[0])

	lo1 := l + (uint32(m) << 16)
	hi1 := (m >> 16) + (int32(l) >> 31) + bool2int32(lo1 < l)

	// Compute y*q[1] as 64-bit value.
	l = uint32((int32(y) & 0xFFFF) * int32(q[1]))
	m = (int32(y) >> 16) * int32(q[1])

	lo2 := l + (uint32(m) << 16)
	hi2 := (m >> 16) + (int32(l) >> 31) + bool2int32(lo2 < l)

	// Add them.
	lo := lo1 + lo2
	hi := hi1 + hi2 + bool2int32(lo < lo1)

	// Divide the result by 2^14 with rounding.
	s := hi >> 31
	l = lo + uint32(s)
	hi += s + bool2int32(l < lo)
	lo = l

	l = lo + 0x2000
	hi += bool2int32(l < lo)

	return fixed.Int26_6((uint32(hi) << 18) | (l >> 14))
}

// mulDiv returns x*y/z, rounded to the nearest integer.
func mulDiv(x, y, z int64) int64 {
	xy := x * y
	if z < 0 {
		xy, z = -xy, -z
	}
	if xy >= 0 {
		xy += z / 2
	} else {
		xy -= z / 2
	}
	return xy / z
}

// round rounds the given number. The rounding algorithm is described at
// https://developer.apple.com/fonts/TTRefMan/RM02/Chap2.html#rounding
func (h *hinter) round(x fixed.Int26_6) fixed.Int26_6 {
	if h.gs.roundPeriod == 0 {
		// Rounding is off.
		return x
	}
	if x >= 0 {
		ret := x - h.gs.roundPhase + h.gs.roundThreshold
		if h.gs.roundSuper45 {
			ret /= h.gs.roundPeriod
			ret *= h.gs.roundPeriod
		} else {
			ret &= -h.gs.roundPeriod
		}
		if x != 0 && ret < 0 {
			ret = 0
		}
		return ret + h.gs.roundPhase
	}
	ret := -x - h.gs.roundPhase + h.gs.roundThreshold
	if h.gs.roundSuper45 {
		ret /= h.gs.roundPeriod
		ret *= h.gs.roundPeriod
	} else {
		ret &= -h.gs.roundPeriod
	}
	if ret < 0 {
		ret = 0
	}
	return -ret - h.gs.roundPhase
}

func bool2int32(b bool) int32 {
	if b {
		return 1
	}
	return 0
}
