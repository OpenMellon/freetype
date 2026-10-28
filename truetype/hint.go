// Copyright 2012 The Freetype-Go Authors. All rights reserved.
// Use of this source code is governed by your choice of either the
// FreeType License or the GNU General Public License version 2 (or
// any later version), both of which can be found in the LICENSE file.

package truetype

// This file implements a Truetype bytecode interpreter.
// The opcodes are described at https://developer.apple.com/fonts/TTRefMan/RM05/Chap5.html

import (
	"errors"

	"golang.org/x/image/math/fixed"
)

func (h *hinter) run(program []byte, pCurrent, pUnhinted, pInFontUnits []Point, ends []int) error {
	h.gs = h.defaultGS
	h.points[glyphZone][current] = pCurrent
	h.points[glyphZone][unhinted] = pUnhinted
	h.points[glyphZone][inFontUnits] = pInFontUnits
	h.ends = ends

	if len(program) > 50000 {
		return errors.New("truetype: hinting: too many instructions")
	}
	var (
		steps, pc, top int
		opcode         uint8

		callStack    [32]callStackEntry
		callStackTop int
	)

	for 0 <= pc && pc < len(program) {
		steps++
		if steps == 100000 {
			return errors.New("truetype: hinting: too many steps")
		}
		opcode = program[pc]
		if top < int(popCount[opcode]) {
			return errors.New("truetype: hinting: stack underflow")
		}
		switch opcode {

		case opSVTCA0:
			h.gs.pv = [2]f2dot14{0, 0x4000}
			h.gs.fv = [2]f2dot14{0, 0x4000}
			h.gs.dv = [2]f2dot14{0, 0x4000}

		case opSVTCA1:
			h.gs.pv = [2]f2dot14{0x4000, 0}
			h.gs.fv = [2]f2dot14{0x4000, 0}
			h.gs.dv = [2]f2dot14{0x4000, 0}

		case opSPVTCA0:
			h.gs.pv = [2]f2dot14{0, 0x4000}
			h.gs.dv = [2]f2dot14{0, 0x4000}

		case opSPVTCA1:
			h.gs.pv = [2]f2dot14{0x4000, 0}
			h.gs.dv = [2]f2dot14{0x4000, 0}

		case opSFVTCA0:
			h.gs.fv = [2]f2dot14{0, 0x4000}

		case opSFVTCA1:
			h.gs.fv = [2]f2dot14{0x4000, 0}

		case opSPVTL0, opSPVTL1, opSFVTL0, opSFVTL1:
			top -= 2
			p1 := h.point(0, current, h.stack[top+0])
			p2 := h.point(0, current, h.stack[top+1])
			if p1 == nil || p2 == nil {
				return errors.New("truetype: hinting: point out of range")
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

		case opSPVFS:
			top -= 2
			h.gs.pv = normalize(f2dot14(h.stack[top]), f2dot14(h.stack[top+1]))
			h.gs.dv = h.gs.pv

		case opSFVFS:
			top -= 2
			h.gs.fv = normalize(f2dot14(h.stack[top]), f2dot14(h.stack[top+1]))

		case opGPV:
			if top+1 >= len(h.stack) {
				return errors.New("truetype: hinting: stack overflow")
			}
			h.stack[top+0] = int32(h.gs.pv[0])
			h.stack[top+1] = int32(h.gs.pv[1])
			top += 2

		case opGFV:
			if top+1 >= len(h.stack) {
				return errors.New("truetype: hinting: stack overflow")
			}
			h.stack[top+0] = int32(h.gs.fv[0])
			h.stack[top+1] = int32(h.gs.fv[1])
			top += 2

		case opSFVTPV:
			h.gs.fv = h.gs.pv

		case opISECT:
			top -= 5
			p := h.point(2, current, h.stack[top+0])
			a0 := h.point(1, current, h.stack[top+1])
			a1 := h.point(1, current, h.stack[top+2])
			b0 := h.point(0, current, h.stack[top+3])
			b1 := h.point(0, current, h.stack[top+4])
			if p == nil || a0 == nil || a1 == nil || b0 == nil || b1 == nil {
				return errors.New("truetype: hinting: point out of range")
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

		case opSRP0, opSRP1, opSRP2:
			top--
			h.gs.rp[opcode-opSRP0] = h.stack[top]

		case opSZP0, opSZP1, opSZP2:
			top--
			h.gs.zp[opcode-opSZP0] = h.stack[top]

		case opSZPS:
			top--
			h.gs.zp[0] = h.stack[top]
			h.gs.zp[1] = h.stack[top]
			h.gs.zp[2] = h.stack[top]

		case opSLOOP:
			top--
			// https://developer.apple.com/fonts/TrueType-Reference-Manual/RM05/Chap5.html#SLOOP
			// says that "Setting the loop variable to zero is an error". In
			// theory, the inequality on the next line should be "<=" instead
			// of "<". In practice, some font files' bytecode, such as the '2'
			// glyph in the DejaVuSansMono.ttf that comes with Ubuntu 14.04,
			// issue SLOOP with a zero on top of the stack. Just like the C
			// Freetype code, we allow the zero.
			if h.stack[top] < 0 {
				return errors.New("truetype: hinting: invalid data")
			}
			h.gs.loop = h.stack[top]

		case opRTG:
			h.gs.roundPeriod = 1 << 6
			h.gs.roundPhase = 0
			h.gs.roundThreshold = 1 << 5
			h.gs.roundSuper45 = false

		case opRTHG:
			h.gs.roundPeriod = 1 << 6
			h.gs.roundPhase = 1 << 5
			h.gs.roundThreshold = 1 << 5
			h.gs.roundSuper45 = false

		case opSMD:
			top--
			h.gs.minDist = fixed.Int26_6(h.stack[top])

		case opELSE:
			opcode = 1
			goto ifelse

		case opJMPR:
			top--
			pc += int(h.stack[top])
			continue

		case opSCVTCI:
			top--
			h.gs.controlValueCutIn = fixed.Int26_6(h.stack[top])

		case opSSWCI:
			top--
			h.gs.singleWidthCutIn = fixed.Int26_6(h.stack[top])

		case opSSW:
			top--
			h.gs.singleWidth = h.font.scale(h.scale * fixed.Int26_6(h.stack[top]))

		case opDUP:
			if top >= len(h.stack) {
				return errors.New("truetype: hinting: stack overflow")
			}
			h.stack[top] = h.stack[top-1]
			top++

		case opPOP:
			top--

		case opCLEAR:
			top = 0

		case opSWAP:
			h.stack[top-1], h.stack[top-2] = h.stack[top-2], h.stack[top-1]

		case opDEPTH:
			if top >= len(h.stack) {
				return errors.New("truetype: hinting: stack overflow")
			}
			h.stack[top] = int32(top)
			top++

		case opCINDEX, opMINDEX:
			x := int(h.stack[top-1])
			if x <= 0 || x >= top {
				return errors.New("truetype: hinting: invalid data")
			}
			h.stack[top-1] = h.stack[top-1-x]
			if opcode == opMINDEX {
				copy(h.stack[top-1-x:top-1], h.stack[top-x:top])
				top--
			}

		case opALIGNPTS:
			top -= 2
			p := h.point(1, current, h.stack[top])
			q := h.point(0, current, h.stack[top+1])
			if p == nil || q == nil {
				return errors.New("truetype: hinting: point out of range")
			}
			d := dotProduct(fixed.Int26_6(q.X-p.X), fixed.Int26_6(q.Y-p.Y), h.gs.pv) / 2
			h.move(p, +d, true)
			h.move(q, -d, true)

		case opUTP:
			top--
			p := h.point(0, current, h.stack[top])
			if p == nil {
				return errors.New("truetype: hinting: point out of range")
			}
			p.Flags &^= flagTouchedX | flagTouchedY

		case opLOOPCALL, opCALL:
			if callStackTop >= len(callStack) {
				return errors.New("truetype: hinting: call stack overflow")
			}
			top--
			f, ok := h.functions[h.stack[top]]
			if !ok {
				return errors.New("truetype: hinting: undefined function")
			}
			callStack[callStackTop] = callStackEntry{program, pc, 1}
			if opcode == opLOOPCALL {
				top--
				if h.stack[top] == 0 {
					break
				}
				callStack[callStackTop].loopCount = h.stack[top]
			}
			callStackTop++
			program, pc = f, 0
			continue

		case opFDEF:
			// Save all bytecode up until the next ENDF.
			startPC := pc + 1
		fdefloop:
			for {
				pc++
				if pc >= len(program) {
					return errors.New("truetype: hinting: unbalanced FDEF")
				}
				switch program[pc] {
				case opFDEF:
					return errors.New("truetype: hinting: nested FDEF")
				case opENDF:
					top--
					h.functions[h.stack[top]] = program[startPC : pc+1]
					break fdefloop
				default:
					var ok bool
					pc, ok = skipInstructionPayload(program, pc)
					if !ok {
						return errors.New("truetype: hinting: unbalanced FDEF")
					}
				}
			}

		case opENDF:
			if callStackTop == 0 {
				return errors.New("truetype: hinting: call stack underflow")
			}
			callStackTop--
			callStack[callStackTop].loopCount--
			if callStack[callStackTop].loopCount != 0 {
				callStackTop++
				pc = 0
				continue
			}
			program, pc = callStack[callStackTop].program, callStack[callStackTop].pc

		case opMDAP0, opMDAP1:
			top--
			i := h.stack[top]
			p := h.point(0, current, i)
			if p == nil {
				return errors.New("truetype: hinting: point out of range")
			}
			distance := fixed.Int26_6(0)
			if opcode == opMDAP1 {
				distance = dotProduct(p.X, p.Y, h.gs.pv)
				// TODO: metrics compensation.
				distance = h.round(distance) - distance
			}
			h.move(p, distance, true)
			h.gs.rp[0] = i
			h.gs.rp[1] = i

		case opIUP0, opIUP1:
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

		case opSHP0, opSHP1:
			if top < int(h.gs.loop) {
				return errors.New("truetype: hinting: stack underflow")
			}
			_, _, d, ok := h.displacement(opcode&1 == 0)
			if !ok {
				return errors.New("truetype: hinting: point out of range")
			}
			for ; h.gs.loop != 0; h.gs.loop-- {
				top--
				p := h.point(2, current, h.stack[top])
				if p == nil {
					return errors.New("truetype: hinting: point out of range")
				}
				h.move(p, d, true)
			}
			h.gs.loop = 1

		case opSHC0, opSHC1:
			top--
			zonePointer, i, d, ok := h.displacement(opcode&1 == 0)
			if !ok {
				return errors.New("truetype: hinting: point out of range")
			}
			if h.gs.zp[2] == 0 {
				// TODO: implement this when we have a glyph that does this.
				return errors.New("hinting: unimplemented SHC instruction")
			}
			contour := h.stack[top]
			if contour < 0 || len(ends) <= int(contour) {
				return errors.New("truetype: hinting: contour out of range")
			}
			j0, j1 := int32(0), int32(h.ends[contour])
			if contour > 0 {
				j0 = int32(h.ends[contour-1])
			}
			move := h.gs.zp[zonePointer] != h.gs.zp[2]
			for j := j0; j < j1; j++ {
				if move || j != i {
					h.move(h.point(2, current, j), d, true)
				}
			}

		case opSHZ0, opSHZ1:
			top--
			zonePointer, i, d, ok := h.displacement(opcode&1 == 0)
			if !ok {
				return errors.New("truetype: hinting: point out of range")
			}

			// As per C Freetype, SHZ doesn't move the phantom points, or mark
			// the points as touched.
			limit := int32(len(h.points[h.gs.zp[2]][current]))
			if h.gs.zp[2] == glyphZone {
				limit -= 4
			}
			for j := int32(0); j < limit; j++ {
				if i != j || h.gs.zp[zonePointer] != h.gs.zp[2] {
					h.move(h.point(2, current, j), d, false)
				}
			}

		case opSHPIX:
			top--
			d := fixed.Int26_6(h.stack[top])
			if top < int(h.gs.loop) {
				return errors.New("truetype: hinting: stack underflow")
			}
			for ; h.gs.loop != 0; h.gs.loop-- {
				top--
				p := h.point(2, current, h.stack[top])
				if p == nil {
					return errors.New("truetype: hinting: point out of range")
				}
				h.move(p, d, true)
			}
			h.gs.loop = 1

		case opIP:
			if top < int(h.gs.loop) {
				return errors.New("truetype: hinting: stack underflow")
			}
			pointType := inFontUnits
			twilight := h.gs.zp[0] == 0 || h.gs.zp[1] == 0 || h.gs.zp[2] == 0
			if twilight {
				pointType = unhinted
			}
			p := h.point(1, pointType, h.gs.rp[2])
			oldP := h.point(0, pointType, h.gs.rp[1])
			oldRange := dotProduct(p.X-oldP.X, p.Y-oldP.Y, h.gs.dv)

			p = h.point(1, current, h.gs.rp[2])
			curP := h.point(0, current, h.gs.rp[1])
			curRange := dotProduct(p.X-curP.X, p.Y-curP.Y, h.gs.pv)
			for ; h.gs.loop != 0; h.gs.loop-- {
				top--
				i := h.stack[top]
				p = h.point(2, pointType, i)
				oldDist := dotProduct(p.X-oldP.X, p.Y-oldP.Y, h.gs.dv)
				p = h.point(2, current, i)
				curDist := dotProduct(p.X-curP.X, p.Y-curP.Y, h.gs.pv)
				newDist := fixed.Int26_6(0)
				if oldDist != 0 {
					if oldRange != 0 {
						newDist = fixed.Int26_6(mulDiv(int64(oldDist), int64(curRange), int64(oldRange)))
					} else {
						newDist = -oldDist
					}
				}
				h.move(p, newDist-curDist, true)
			}
			h.gs.loop = 1

		case opMSIRP0, opMSIRP1:
			top -= 2
			i := h.stack[top]
			distance := fixed.Int26_6(h.stack[top+1])

			// TODO: special case h.gs.zp[1] == 0 in C Freetype.
			ref := h.point(0, current, h.gs.rp[0])
			p := h.point(1, current, i)
			if ref == nil || p == nil {
				return errors.New("truetype: hinting: point out of range")
			}
			curDist := dotProduct(p.X-ref.X, p.Y-ref.Y, h.gs.pv)

			// Set-RP0 bit.
			if opcode == opMSIRP1 {
				h.gs.rp[0] = i
			}
			h.gs.rp[1] = h.gs.rp[0]
			h.gs.rp[2] = i

			// Move the point.
			h.move(p, distance-curDist, true)

		case opALIGNRP:
			if top < int(h.gs.loop) {
				return errors.New("truetype: hinting: stack underflow")
			}
			ref := h.point(0, current, h.gs.rp[0])
			if ref == nil {
				return errors.New("truetype: hinting: point out of range")
			}
			for ; h.gs.loop != 0; h.gs.loop-- {
				top--
				p := h.point(1, current, h.stack[top])
				if p == nil {
					return errors.New("truetype: hinting: point out of range")
				}
				h.move(p, -dotProduct(p.X-ref.X, p.Y-ref.Y, h.gs.pv), true)
			}
			h.gs.loop = 1

		case opRTDG:
			h.gs.roundPeriod = 1 << 5
			h.gs.roundPhase = 0
			h.gs.roundThreshold = 1 << 4
			h.gs.roundSuper45 = false

		case opMIAP0, opMIAP1:
			top -= 2
			i := h.stack[top]
			distance := h.getScaledCVT(h.stack[top+1])
			if h.gs.zp[0] == 0 {
				p := h.point(0, unhinted, i)
				q := h.point(0, current, i)
				p.X = fixed.Int26_6((int64(distance) * int64(h.gs.fv[0])) >> 14)
				p.Y = fixed.Int26_6((int64(distance) * int64(h.gs.fv[1])) >> 14)
				*q = *p
			}
			p := h.point(0, current, i)
			oldDist := dotProduct(p.X, p.Y, h.gs.pv)
			if opcode == opMIAP1 {
				if fabs(distance-oldDist) > h.gs.controlValueCutIn {
					distance = oldDist
				}
				// TODO: metrics compensation.
				distance = h.round(distance)
			}
			h.move(p, distance-oldDist, true)
			h.gs.rp[0] = i
			h.gs.rp[1] = i

		case opNPUSHB:
			opcode = 0
			goto push

		case opNPUSHW:
			opcode = 0x80
			goto push

		case opWS:
			top -= 2
			i := int(h.stack[top])
			if i < 0 || len(h.store) <= i {
				return errors.New("truetype: hinting: invalid data")
			}
			h.store[i] = h.stack[top+1]

		case opRS:
			i := int(h.stack[top-1])
			if i < 0 || len(h.store) <= i {
				return errors.New("truetype: hinting: invalid data")
			}
			h.stack[top-1] = h.store[i]

		case opWCVTP:
			top -= 2
			h.setScaledCVT(h.stack[top], fixed.Int26_6(h.stack[top+1]))

		case opRCVT:
			h.stack[top-1] = int32(h.getScaledCVT(h.stack[top-1]))

		case opGC0, opGC1:
			i := h.stack[top-1]
			if opcode == opGC0 {
				p := h.point(2, current, i)
				h.stack[top-1] = int32(dotProduct(p.X, p.Y, h.gs.pv))
			} else {
				p := h.point(2, unhinted, i)
				// Using dv as per C Freetype.
				h.stack[top-1] = int32(dotProduct(p.X, p.Y, h.gs.dv))
			}

		case opSCFS:
			top -= 2
			i := h.stack[top]
			p := h.point(2, current, i)
			if p == nil {
				return errors.New("truetype: hinting: point out of range")
			}
			c := dotProduct(p.X, p.Y, h.gs.pv)
			h.move(p, fixed.Int26_6(h.stack[top+1])-c, true)
			if h.gs.zp[2] != 0 {
				break
			}
			q := h.point(2, unhinted, i)
			if q == nil {
				return errors.New("truetype: hinting: point out of range")
			}
			q.X = p.X
			q.Y = p.Y

		case opMD0, opMD1:
			top--
			pt, v, scale := pointType(0), [2]f2dot14{}, false
			if opcode == opMD0 {
				pt = current
				v = h.gs.pv
			} else if h.gs.zp[0] == 0 || h.gs.zp[1] == 0 {
				pt = unhinted
				v = h.gs.dv
			} else {
				pt = inFontUnits
				v = h.gs.dv
				scale = true
			}
			p := h.point(0, pt, h.stack[top-1])
			q := h.point(1, pt, h.stack[top])
			if p == nil || q == nil {
				return errors.New("truetype: hinting: point out of range")
			}
			d := int32(dotProduct(p.X-q.X, p.Y-q.Y, v))
			if scale {
				d = int32(int64(d*int32(h.scale)) / int64(h.font.fUnitsPerEm))
			}
			h.stack[top-1] = d

		case opMPPEM, opMPS:
			if top >= len(h.stack) {
				return errors.New("truetype: hinting: stack overflow")
			}
			// For MPS, point size should be irrelevant; we return the PPEM.
			h.stack[top] = int32(h.scale) >> 6
			top++

		case opFLIPON, opFLIPOFF:
			h.gs.autoFlip = opcode == opFLIPON

		case opDEBUG:
			// No-op.

		case opLT:
			top--
			h.stack[top-1] = bool2int32(h.stack[top-1] < h.stack[top])

		case opLTEQ:
			top--
			h.stack[top-1] = bool2int32(h.stack[top-1] <= h.stack[top])

		case opGT:
			top--
			h.stack[top-1] = bool2int32(h.stack[top-1] > h.stack[top])

		case opGTEQ:
			top--
			h.stack[top-1] = bool2int32(h.stack[top-1] >= h.stack[top])

		case opEQ:
			top--
			h.stack[top-1] = bool2int32(h.stack[top-1] == h.stack[top])

		case opNEQ:
			top--
			h.stack[top-1] = bool2int32(h.stack[top-1] != h.stack[top])

		case opODD, opEVEN:
			i := h.round(fixed.Int26_6(h.stack[top-1])) >> 6
			h.stack[top-1] = int32(i&1) ^ int32(opcode-opODD)

		case opIF:
			top--
			if h.stack[top] == 0 {
				opcode = 0
				goto ifelse
			}

		case opEIF:
			// No-op.

		case opAND:
			top--
			h.stack[top-1] = bool2int32(h.stack[top-1] != 0 && h.stack[top] != 0)

		case opOR:
			top--
			h.stack[top-1] = bool2int32(h.stack[top-1]|h.stack[top] != 0)

		case opNOT:
			h.stack[top-1] = bool2int32(h.stack[top-1] == 0)

		case opDELTAP1:
			goto delta

		case opSDB:
			top--
			h.gs.deltaBase = h.stack[top]

		case opSDS:
			top--
			h.gs.deltaShift = h.stack[top]

		case opADD:
			top--
			h.stack[top-1] += h.stack[top]

		case opSUB:
			top--
			h.stack[top-1] -= h.stack[top]

		case opDIV:
			top--
			if h.stack[top] == 0 {
				return errors.New("truetype: hinting: division by zero")
			}
			h.stack[top-1] = int32(fdiv(fixed.Int26_6(h.stack[top-1]), fixed.Int26_6(h.stack[top])))

		case opMUL:
			top--
			h.stack[top-1] = int32(fmul(fixed.Int26_6(h.stack[top-1]), fixed.Int26_6(h.stack[top])))

		case opABS:
			if h.stack[top-1] < 0 {
				h.stack[top-1] = -h.stack[top-1]
			}

		case opNEG:
			h.stack[top-1] = -h.stack[top-1]

		case opFLOOR:
			h.stack[top-1] &^= 63

		case opCEILING:
			h.stack[top-1] += 63
			h.stack[top-1] &^= 63

		case opROUND00, opROUND01, opROUND10, opROUND11:
			// The four flavors of opROUND are equivalent. See the comment below on
			// opNROUND for the rationale.
			h.stack[top-1] = int32(h.round(fixed.Int26_6(h.stack[top-1])))

		case opNROUND00, opNROUND01, opNROUND10, opNROUND11:
			// No-op. The spec says to add one of four "compensations for the engine
			// characteristics", to cater for things like "different dot-size printers".
			// https://developer.apple.com/fonts/TTRefMan/RM02/Chap2.html#engine_compensation
			// This code does not implement engine compensation, as we don't expect to
			// be used to output on dot-matrix printers.

		case opWCVTF:
			top -= 2
			h.setScaledCVT(h.stack[top], h.font.scale(h.scale*fixed.Int26_6(h.stack[top+1])))

		case opDELTAP2, opDELTAP3, opDELTAC1, opDELTAC2, opDELTAC3:
			goto delta

		case opSROUND, opS45ROUND:
			top--
			switch (h.stack[top] >> 6) & 0x03 {
			case 0:
				h.gs.roundPeriod = 1 << 5
			case 1, 3:
				h.gs.roundPeriod = 1 << 6
			case 2:
				h.gs.roundPeriod = 1 << 7
			}
			h.gs.roundSuper45 = opcode == opS45ROUND
			if h.gs.roundSuper45 {
				// The spec says to multiply by √2, but the C Freetype code says 1/√2.
				// We go with 1/√2.
				h.gs.roundPeriod *= 46341
				h.gs.roundPeriod /= 65536
			}
			h.gs.roundPhase = h.gs.roundPeriod * fixed.Int26_6((h.stack[top]>>4)&0x03) / 4
			if x := h.stack[top] & 0x0f; x != 0 {
				h.gs.roundThreshold = h.gs.roundPeriod * fixed.Int26_6(x-4) / 8
			} else {
				h.gs.roundThreshold = h.gs.roundPeriod - 1
			}

		case opJROT:
			top -= 2
			if h.stack[top+1] != 0 {
				pc += int(h.stack[top])
				continue
			}

		case opJROF:
			top -= 2
			if h.stack[top+1] == 0 {
				pc += int(h.stack[top])
				continue
			}

		case opROFF:
			h.gs.roundPeriod = 0
			h.gs.roundPhase = 0
			h.gs.roundThreshold = 0
			h.gs.roundSuper45 = false

		case opRUTG:
			h.gs.roundPeriod = 1 << 6
			h.gs.roundPhase = 0
			h.gs.roundThreshold = 1<<6 - 1
			h.gs.roundSuper45 = false

		case opRDTG:
			h.gs.roundPeriod = 1 << 6
			h.gs.roundPhase = 0
			h.gs.roundThreshold = 0
			h.gs.roundSuper45 = false

		case opSANGW, opAA:
			// These ops are "anachronistic" and no longer used.
			top--

		case opFLIPPT:
			if top < int(h.gs.loop) {
				return errors.New("truetype: hinting: stack underflow")
			}
			points := h.points[glyphZone][current]
			for ; h.gs.loop != 0; h.gs.loop-- {
				top--
				i := h.stack[top]
				if i < 0 || len(points) <= int(i) {
					return errors.New("truetype: hinting: point out of range")
				}
				points[i].Flags ^= flagOnCurve
			}
			h.gs.loop = 1

		case opFLIPRGON, opFLIPRGOFF:
			top -= 2
			i, j, points := h.stack[top], h.stack[top+1], h.points[glyphZone][current]
			if i < 0 || len(points) <= int(i) || j < 0 || len(points) <= int(j) {
				return errors.New("truetype: hinting: point out of range")
			}
			for ; i <= j; i++ {
				if opcode == opFLIPRGON {
					points[i].Flags |= flagOnCurve
				} else {
					points[i].Flags &^= flagOnCurve
				}
			}

		case opSCANCTRL:
			// We do not support dropout control, as we always rasterize grayscale glyphs.
			top--

		case opSDPVTL0, opSDPVTL1:
			top -= 2
			for i := 0; i < 2; i++ {
				pt := unhinted
				if i != 0 {
					pt = current
				}
				p := h.point(1, pt, h.stack[top])
				q := h.point(2, pt, h.stack[top+1])
				if p == nil || q == nil {
					return errors.New("truetype: hinting: point out of range")
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

		case opGETINFO:
			res := int32(0)
			if h.stack[top-1]&(1<<0) != 0 {
				// Set the engine version. We hard-code this to 35, the same as
				// the C freetype code, which says that "Version~35 corresponds
				// to MS rasterizer v.1.7 as used e.g. in Windows~98".
				res |= 35
			}
			if h.stack[top-1]&(1<<5) != 0 {
				// Set that we support grayscale.
				res |= 1 << 12
			}
			// We set no other bits, as we do not support rotated or stretched glyphs.
			h.stack[top-1] = res

		case opIDEF:
			// IDEF is for ancient versions of the bytecode interpreter, and is no longer used.
			return errors.New("truetype: hinting: unsupported IDEF instruction")

		case opROLL:
			h.stack[top-1], h.stack[top-3], h.stack[top-2] =
				h.stack[top-3], h.stack[top-2], h.stack[top-1]

		case opMAX:
			top--
			if h.stack[top-1] < h.stack[top] {
				h.stack[top-1] = h.stack[top]
			}

		case opMIN:
			top--
			if h.stack[top-1] > h.stack[top] {
				h.stack[top-1] = h.stack[top]
			}

		case opSCANTYPE:
			// We do not support dropout control, as we always rasterize grayscale glyphs.
			top--

		case opINSTCTRL:
			// TODO: support instruction execution control? It seems rare, and even when
			// nominally used (e.g. Source Sans Pro), it seems conditional on extreme or
			// unusual rasterization conditions. For example, the code snippet at
			// https://developer.apple.com/fonts/TTRefMan/RM05/Chap5.html#INSTCTRL
			// uses INSTCTRL when grid-fitting a rotated or stretched glyph, but
			// freetype-go does not support rotated or stretched glyphs.
			top -= 2

		default:
			if opcode < opPUSHB000 {
				return errors.New("truetype: hinting: unrecognized instruction")
			}

			if opcode < opMDRP00000 {
				// PUSHxxxx opcode.

				if opcode < opPUSHW000 {
					opcode -= opPUSHB000 - 1
				} else {
					opcode -= opPUSHW000 - 1 - 0x80
				}
				goto push
			}

			if opcode < opMIRP00000 {
				// MDRPxxxxx opcode.

				top--
				i := h.stack[top]
				ref := h.point(0, current, h.gs.rp[0])
				p := h.point(1, current, i)
				if ref == nil || p == nil {
					return errors.New("truetype: hinting: point out of range")
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

			} else {
				// MIRPxxxxx opcode.

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
					return errors.New("truetype: hinting: unimplemented twilight point adjustment")
				}

				ref := h.point(0, unhinted, h.gs.rp[0])
				p := h.point(1, unhinted, i)
				if ref == nil || p == nil {
					return errors.New("truetype: hinting: point out of range")
				}
				oldDist := dotProduct(p.X-ref.X, p.Y-ref.Y, h.gs.dv)

				ref = h.point(0, current, h.gs.rp[0])
				p = h.point(1, current, i)
				if ref == nil || p == nil {
					return errors.New("truetype: hinting: point out of range")
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
			}
		}
		pc++
		continue

	ifelse:
		// Skip past bytecode until the next ELSE (if opcode == 0) or the
		// next EIF (for all opcodes). Opcode == 0 means that we have come
		// from an IF. Opcode == 1 means that we have come from an ELSE.
		{
		ifelseloop:
			for depth := 0; ; {
				pc++
				if pc >= len(program) {
					return errors.New("truetype: hinting: unbalanced IF or ELSE")
				}
				switch program[pc] {
				case opIF:
					depth++
				case opELSE:
					if depth == 0 && opcode == 0 {
						break ifelseloop
					}
				case opEIF:
					depth--
					if depth < 0 {
						break ifelseloop
					}
				default:
					var ok bool
					pc, ok = skipInstructionPayload(program, pc)
					if !ok {
						return errors.New("truetype: hinting: unbalanced IF or ELSE")
					}
				}
			}
			pc++
			continue
		}

	push:
		// Push n elements from the program to the stack, where n is the low 7 bits of
		// opcode. If the low 7 bits are zero, then n is the next byte from the program.
		// The high bit being 0 means that the elements are zero-extended bytes.
		// The high bit being 1 means that the elements are sign-extended words.
		{
			width := 1
			if opcode&0x80 != 0 {
				opcode &^= 0x80
				width = 2
			}
			if opcode == 0 {
				pc++
				if pc >= len(program) {
					return errors.New("truetype: hinting: insufficient data")
				}
				opcode = program[pc]
			}
			pc++
			if top+int(opcode) > len(h.stack) {
				return errors.New("truetype: hinting: stack overflow")
			}
			if pc+width*int(opcode) > len(program) {
				return errors.New("truetype: hinting: insufficient data")
			}
			for ; opcode > 0; opcode-- {
				if width == 1 {
					h.stack[top] = int32(program[pc])
				} else {
					h.stack[top] = int32(int8(program[pc]))<<8 | int32(program[pc+1])
				}
				top++
				pc += width
			}
			continue
		}

	delta:
		{
			if opcode >= opDELTAC1 && !h.scaledCVTInitialized {
				h.initializeScaledCVT()
			}
			top--
			n := h.stack[top]
			if int32(top) < 2*n {
				return errors.New("truetype: hinting: stack underflow")
			}
			for ; n > 0; n-- {
				top -= 2
				b := h.stack[top]
				c := (b & 0xf0) >> 4
				switch opcode {
				case opDELTAP2, opDELTAC2:
					c += 16
				case opDELTAP3, opDELTAC3:
					c += 32
				}
				c += h.gs.deltaBase
				if ppem := (int32(h.scale) + 1<<5) >> 6; ppem != c {
					continue
				}
				b = (b & 0x0f) - 8
				if b >= 0 {
					b++
				}
				b = b * 64 / (1 << uint32(h.gs.deltaShift))
				if opcode >= opDELTAC1 {
					a := h.stack[top+1]
					if a < 0 || len(h.scaledCVT) <= int(a) {
						return errors.New("truetype: hinting: index out of range")
					}
					h.scaledCVT[a] += fixed.Int26_6(b)
				} else {
					p := h.point(0, current, h.stack[top+1])
					if p == nil {
						return errors.New("truetype: hinting: point out of range")
					}
					h.move(p, fixed.Int26_6(b), true)
				}
			}
			pc++
			continue
		}
	}
	return nil
}
