// Copyright 2012 The Freetype-Go Authors. All rights reserved.
// Use of this source code is governed by your choice of either the
// FreeType License or the GNU General Public License version 2 (or
// any later version), both of which can be found in the LICENSE file.

package truetype

// This file implements part of the Truetype bytecode interpreter: stack instructions.

import (
	"errors"
)

// CINDEX and MINDEX: copy or move a stack element to the top.
func (h *hinter) cindex(opcode uint8, top int) (int, error) {
	x := int(h.stack[top-1])
	if x <= 0 || x >= top {
		return top, errors.New("truetype: hinting: invalid data")
	}
	h.stack[top-1] = h.stack[top-1-x]
	if opcode == opMINDEX {
		copy(h.stack[top-1-x:top-1], h.stack[top-x:top])
		top--
	}
	return top, nil
}

// GETINFO: reports interpreter capabilities.
func (h *hinter) getinfo(top int) {
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
}

// NPUSHB, NPUSHW, PUSHB and PUSHW: push program data on to the stack.
func (h *hinter) push(program []byte, pc, top int, opcode uint8) (int, int, error) {
	width := 1
	if opcode&0x80 != 0 {
		opcode &^= 0x80
		width = 2
	}
	if opcode == 0 {
		pc++
		if pc >= len(program) {
			return pc, top, errors.New("truetype: hinting: insufficient data")
		}
		opcode = program[pc]
	}
	pc++
	if top+int(opcode) > len(h.stack) {
		return pc, top, errors.New("truetype: hinting: stack overflow")
	}
	if pc+width*int(opcode) > len(program) {
		return pc, top, errors.New("truetype: hinting: insufficient data")
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
	return pc, top, nil
}
