// Copyright 2012 The Freetype-Go Authors. All rights reserved.
// Use of this source code is governed by your choice of either the
// FreeType License or the GNU General Public License version 2 (or
// any later version), both of which can be found in the LICENSE file.

package truetype

// This file implements part of the Truetype bytecode interpreter: rounding instructions.

import (
	"golang.org/x/image/math/fixed"
)

// SROUND and S45ROUND: configure super rounding.
func (h *hinter) sround(opcode uint8, top int) int {
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
	return top
}
