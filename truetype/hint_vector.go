// Copyright 2012 The Freetype-Go Authors. All rights reserved.
// Use of this source code is governed by your choice of either the
// FreeType License or the GNU General Public License version 2 (or
// any later version), both of which can be found in the LICENSE file.

package truetype

// This file implements part of the Truetype bytecode interpreter: vector-setting instructions.

import (
	"errors"

	"golang.org/x/image/math/fixed"
)

// SPVTL, SPVTL, SFVTL: set a projection or freedom vector from two points.
func (h *hinter) spvtl(opcode uint8, top int) (int, error) {
	top -= 2
	p1 := h.point(0, current, h.stack[top+0])
	p2 := h.point(0, current, h.stack[top+1])
	if p1 == nil || p2 == nil {
		return top, errors.New("truetype: hinting: point out of range")
	}
	dx := f2dot14(p1.X - p2.X)
	dy := f2dot14(p1.Y - p2.Y)
	if dx == 0 && dy == 0 {
		dx = 0x4000
	} else if opcode&1 != 0 {
		// Counter-clockwise rotation.
		dx, dy = -dy, dx
	}
	v := normalize(dx, dy)
	if opcode < opSFVTL0 {
		h.gs.pv = v
		h.gs.dv = v
	} else {
		h.gs.fv = v
	}
	return top, nil
}

// ISECT: moves a point to the intersection of two lines.
func (h *hinter) isect(top int) (int, error) {
	top -= 5
	p := h.point(2, current, h.stack[top+0])
	a0 := h.point(1, current, h.stack[top+1])
	a1 := h.point(1, current, h.stack[top+2])
	b0 := h.point(0, current, h.stack[top+3])
	b1 := h.point(0, current, h.stack[top+4])
	if p == nil || a0 == nil || a1 == nil || b0 == nil || b1 == nil {
		return top, errors.New("truetype: hinting: point out of range")
	}

	dbx := b1.X - b0.X
	dby := b1.Y - b0.Y
	dax := a1.X - a0.X
	day := a1.Y - a0.Y
	dx := b0.X - a0.X
	dy := b0.Y - a0.Y
	discriminant := mulDiv(int64(dax), int64(-dby), 0x40) +
		mulDiv(int64(day), int64(dbx), 0x40)
	dotProduct := mulDiv(int64(dax), int64(dbx), 0x40) +
		mulDiv(int64(day), int64(dby), 0x40)
	// The discriminant above is actually a cross product of vectors
	// da and db. Together with the dot product, they can be used as
	// surrogates for sine and cosine of the angle between the vectors.
	// Indeed,
	//       dotproduct   = |da||db|cos(angle)
	//       discriminant = |da||db|sin(angle)
	// We use these equations to reject grazing intersections by
	// thresholding abs(tan(angle)) at 1/19, corresponding to 3 degrees.
	absDisc, absDotP := discriminant, dotProduct
	if absDisc < 0 {
		absDisc = -absDisc
	}
	if absDotP < 0 {
		absDotP = -absDotP
	}
	if 19*absDisc > absDotP {
		val := mulDiv(int64(dx), int64(-dby), 0x40) +
			mulDiv(int64(dy), int64(dbx), 0x40)
		rx := mulDiv(val, int64(dax), discriminant)
		ry := mulDiv(val, int64(day), discriminant)
		p.X = a0.X + fixed.Int26_6(rx)
		p.Y = a0.Y + fixed.Int26_6(ry)
	} else {
		p.X = (a0.X + a1.X + b0.X + b1.X) / 4
		p.Y = (a0.Y + a1.Y + b0.Y + b1.Y) / 4
	}
	p.Flags |= flagTouchedX | flagTouchedY
	return top, nil
}

// SDPVTL: sets the dual and projection vectors from two points.
func (h *hinter) sdpvtl(opcode uint8, top int) (int, error) {
	top -= 2
	for i := 0; i < 2; i++ {
		pt := unhinted
		if i != 0 {
			pt = current
		}
		p := h.point(1, pt, h.stack[top])
		q := h.point(2, pt, h.stack[top+1])
		if p == nil || q == nil {
			return top, errors.New("truetype: hinting: point out of range")
		}
		dx := f2dot14(p.X - q.X)
		dy := f2dot14(p.Y - q.Y)
		if dx == 0 && dy == 0 {
			dx = 0x4000
		} else if opcode&1 != 0 {
			// Counter-clockwise rotation.
			dx, dy = -dy, dx
		}
		if i == 0 {
			h.gs.dv = normalize(dx, dy)
		} else {
			h.gs.pv = normalize(dx, dy)
		}
	}
	return top, nil
}
