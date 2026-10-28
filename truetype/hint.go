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
		err            error

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
			if top, err = h.spvtl(opcode, top); err != nil {
				return err
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
			if top, err = h.isect(top); err != nil {
				return err
			}

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
			if pc, err = h.skipConditional(program, pc, 1); err != nil {
				return err
			}
			continue

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
			if top, err = h.cindex(opcode, top); err != nil {
				return err
			}

		case opALIGNPTS:
			top -= 2
			if top, err = h.alignpts(top); err != nil {
				return err
			}

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
			if pc, top, err = h.defineFunction(program, pc, top); err != nil {
				return err
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
			if top, err = h.mdap(opcode, top); err != nil {
				return err
			}

		case opIUP0, opIUP1:
			h.iupOp(opcode)

		case opSHP0, opSHP1:
			if top, err = h.shp(opcode, top); err != nil {
				return err
			}

		case opSHC0, opSHC1:
			if top, err = h.shc(opcode, top); err != nil {
				return err
			}

		case opSHZ0, opSHZ1:
			if top, err = h.shz(opcode, top); err != nil {
				return err
			}

		case opSHPIX:
			top--
			if top, err = h.shpix(top); err != nil {
				return err
			}

		case opIP:
			if top, err = h.ip(top); err != nil {
				return err
			}

		case opMSIRP0, opMSIRP1:
			if top, err = h.msirp(opcode, top); err != nil {
				return err
			}

		case opALIGNRP:
			if top, err = h.alignrp(top); err != nil {
				return err
			}

		case opRTDG:
			h.gs.roundPeriod = 1 << 5
			h.gs.roundPhase = 0
			h.gs.roundThreshold = 1 << 4
			h.gs.roundSuper45 = false

		case opMIAP0, opMIAP1:
			if top, err = h.miap(opcode, top); err != nil {
				return err
			}

		case opNPUSHB:
			if pc, top, err = h.push(program, pc, top, 0); err != nil {
				return err
			}
			continue

		case opNPUSHW:
			if pc, top, err = h.push(program, pc, top, 0x80); err != nil {
				return err
			}
			continue

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
			if top, err = h.scfs(top); err != nil {
				return err
			}

		case opMD0, opMD1:
			if top, err = h.md(opcode, top); err != nil {
				return err
			}

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
				if pc, err = h.skipConditional(program, pc, 0); err != nil {
					return err
				}
				continue
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
			if top, err = h.delta(opcode, top); err != nil {
				return err
			}

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
			if top, err = h.delta(opcode, top); err != nil {
				return err
			}

		case opSROUND, opS45ROUND:
			top = h.sround(opcode, top)

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
			if top, err = h.flippt(top); err != nil {
				return err
			}

		case opFLIPRGON, opFLIPRGOFF:
			if top, err = h.fliprg(opcode, top); err != nil {
				return err
			}

		case opSCANCTRL:
			// We do not support dropout control, as we always rasterize grayscale glyphs.
			top--

		case opSDPVTL0, opSDPVTL1:
			if top, err = h.sdpvtl(opcode, top); err != nil {
				return err
			}

		case opGETINFO:
			h.getinfo(top)

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
				if pc, top, err = h.push(program, pc, top, opcode); err != nil {
					return err
				}
				continue
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
	}
	return nil
}
