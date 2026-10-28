// Copyright 2012 The Freetype-Go Authors. All rights reserved.
// Use of this source code is governed by your choice of either the
// FreeType License or the GNU General Public License version 2 (or
// any later version), both of which can be found in the LICENSE file.

package truetype

// This file implements part of the Truetype bytecode interpreter: control flow and delta instructions.

import (
	"errors"

	"golang.org/x/image/math/fixed"
)

// FDEF: records the bytecode up to the matching ENDF.
func (h *hinter) defineFunction(program []byte, pc, top int) (int, int, error) {
	startPC := pc + 1
fdefloop:
	for {
		pc++
		if pc >= len(program) {
			return pc, top, errors.New("truetype: hinting: unbalanced FDEF")
		}
		switch program[pc] {
		case opFDEF:
			return pc, top, errors.New("truetype: hinting: nested FDEF")
		case opENDF:
			top--
			h.functions[h.stack[top]] = program[startPC : pc+1]
			break fdefloop
		default:
			var ok bool
			pc, ok = skipInstructionPayload(program, pc)
			if !ok {
				return pc, top, errors.New("truetype: hinting: unbalanced FDEF")
			}
		}
	}
	return pc, top, nil
}

// IF and ELSE: skips to the matching ELSE or EIF.
func (h *hinter) skipConditional(program []byte, pc int, opcode uint8) (int, error) {
ifelseloop:
	for depth := 0; ; {
		pc++
		if pc >= len(program) {
			return pc, errors.New("truetype: hinting: unbalanced IF or ELSE")
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
				return pc, errors.New("truetype: hinting: unbalanced IF or ELSE")
			}
		}
	}
	pc++
	return pc, nil
}

// DELTAP1-3 and DELTAC1-3: apply exceptions at specific sizes.
func (h *hinter) delta(opcode uint8, top int) (int, error) {
	if opcode >= opDELTAC1 && !h.scaledCVTInitialized {
		h.initializeScaledCVT()
	}
	top--
	n := h.stack[top]
	if int32(top) < 2*n {
		return top, errors.New("truetype: hinting: stack underflow")
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
				return top, errors.New("truetype: hinting: index out of range")
			}
			h.scaledCVT[a] += fixed.Int26_6(b)
		} else {
			p := h.point(0, current, h.stack[top+1])
			if p == nil {
				return top, errors.New("truetype: hinting: point out of range")
			}
			h.move(p, fixed.Int26_6(b), true)
		}
	}
	return top, nil
}
