// Copyright 2012 The Freetype-Go Authors. All rights reserved.
// Use of this source code is governed by your choice of either the
// FreeType License or the GNU General Public License version 2 (or
// any later version), both of which can be found in the LICENSE file.

package truetype

// This file implements part of the Truetype bytecode interpreter: interpolate untouched points.

// IUP: interpolates untouched points.
func (h *hinter) iupOp(opcode uint8) {
	iupY, mask := opcode == opIUP0, uint32(flagTouchedX)
	if iupY {
		mask = flagTouchedY
	}
	prevEnd := 0
	for _, end := range h.ends {
		for i := prevEnd; i < end; i++ {
			for i < end && h.points[glyphZone][current][i].Flags&mask == 0 {
				i++
			}
			if i == end {
				break
			}
			firstTouched, curTouched := i, i
			i++
			for ; i < end; i++ {
				if h.points[glyphZone][current][i].Flags&mask != 0 {
					h.iupInterp(iupY, curTouched+1, i-1, curTouched, i)
					curTouched = i
				}
			}
			if curTouched == firstTouched {
				h.iupShift(iupY, prevEnd, end, curTouched)
			} else {
				h.iupInterp(iupY, curTouched+1, end-1, curTouched, firstTouched)
				if firstTouched > 0 {
					h.iupInterp(iupY, prevEnd, firstTouched-1, curTouched, firstTouched)
				}
			}
		}
		prevEnd = end
	}
}
