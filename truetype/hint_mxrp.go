// Copyright 2012 The Freetype-Go Authors. All rights reserved.
// Use of this source code is governed by your choice of either the
// FreeType License or the GNU General Public License version 2 (or
// any later version), both of which can be found in the LICENSE file.

package truetype

// This file implements part of the Truetype bytecode interpreter: MDRP and MIRP instructions.

import (
	"errors"

	"golang.org/x/image/math/fixed"
)

// MDRPxxxxx: moves a direct relative point.
func (h *hinter) mdrp(opcode uint8, top int) (int, error) {
	top--
	i := h.stack[top]
	ref := h.point(0, current, h.gs.rp[0])
	p := h.point(1, current, i)
	if ref == nil || p == nil {
		return top, errors.New("truetype: hinting: point out of range")
	}

	oldDist := fixed.Int26_6(0)
	if h.gs.zp[0] == 0 || h.gs.zp[1] == 0 {
		p0 := h.point(1, unhinted, i)
		p1 := h.point(0, unhinted, h.gs.rp[0])
		oldDist = dotProduct(p0.X-p1.X, p0.Y-p1.Y, h.gs.dv)
	} else {
		p0 := h.point(1, inFontUnits, i)
		p1 := h.point(0, inFontUnits, h.gs.rp[0])
		oldDist = dotProduct(p0.X-p1.X, p0.Y-p1.Y, h.gs.dv)
		oldDist = h.font.scale(h.scale * oldDist)
	}

	// Single-width cut-in test.
	if x := fabs(oldDist - h.gs.singleWidth); x < h.gs.singleWidthCutIn {
		if oldDist >= 0 {
			oldDist = +h.gs.singleWidth
		} else {
			oldDist = -h.gs.singleWidth
		}
	}

	// Rounding bit.
	// TODO: metrics compensation.
	distance := oldDist
	if opcode&0x04 != 0 {
		distance = h.round(oldDist)
	}

	// Minimum distance bit.
	if opcode&0x08 != 0 {
		if oldDist >= 0 {
			if distance < h.gs.minDist {
				distance = h.gs.minDist
			}
		} else {
			if distance > -h.gs.minDist {
				distance = -h.gs.minDist
			}
		}
	}

	// Set-RP0 bit.
	h.gs.rp[1] = h.gs.rp[0]
	h.gs.rp[2] = i
	if opcode&0x10 != 0 {
		h.gs.rp[0] = i
	}

	// Move the point.
	oldDist = dotProduct(p.X-ref.X, p.Y-ref.Y, h.gs.pv)
	h.move(p, distance-oldDist, true)
	return top, nil
}

// MIRPxxxxx: moves an indirect relative point.
func (h *hinter) mirp(opcode uint8, top int) (int, error) {
	top -= 2
	i := h.stack[top]
	cvtDist := h.getScaledCVT(h.stack[top+1])
	if fabs(cvtDist-h.gs.singleWidth) < h.gs.singleWidthCutIn {
		if cvtDist >= 0 {
			cvtDist = +h.gs.singleWidth
		} else {
			cvtDist = -h.gs.singleWidth
		}
	}

	if h.gs.zp[1] == 0 {
		// TODO: implement once we have a .ttf file that triggers
		// this, so that we can step through C's freetype.
		return top, errors.New("truetype: hinting: unimplemented twilight point adjustment")
	}

	ref := h.point(0, unhinted, h.gs.rp[0])
	p := h.point(1, unhinted, i)
	if ref == nil || p == nil {
		return top, errors.New("truetype: hinting: point out of range")
	}
	oldDist := dotProduct(p.X-ref.X, p.Y-ref.Y, h.gs.dv)

	ref = h.point(0, current, h.gs.rp[0])
	p = h.point(1, current, i)
	if ref == nil || p == nil {
		return top, errors.New("truetype: hinting: point out of range")
	}
	curDist := dotProduct(p.X-ref.X, p.Y-ref.Y, h.gs.pv)

	if h.gs.autoFlip && oldDist^cvtDist < 0 {
		cvtDist = -cvtDist
	}

	// Rounding bit.
	// TODO: metrics compensation.
	distance := cvtDist
	if opcode&0x04 != 0 {
		// The CVT value is only used if close enough to oldDist.
		if (h.gs.zp[0] == h.gs.zp[1]) &&
			(fabs(cvtDist-oldDist) > h.gs.controlValueCutIn) {

			distance = oldDist
		}
		distance = h.round(distance)
	}

	// Minimum distance bit.
	if opcode&0x08 != 0 {
		if oldDist >= 0 {
			if distance < h.gs.minDist {
				distance = h.gs.minDist
			}
		} else {
			if distance > -h.gs.minDist {
				distance = -h.gs.minDist
			}
		}
	}

	// Set-RP0 bit.
	h.gs.rp[1] = h.gs.rp[0]
	h.gs.rp[2] = i
	if opcode&0x10 != 0 {
		h.gs.rp[0] = i
	}

	// Move the point.
	h.move(p, distance-curDist, true)
	return top, nil
}
