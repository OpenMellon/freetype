// Copyright 2012 The Freetype-Go Authors. All rights reserved.
// Use of this source code is governed by your choice of either the
// FreeType License or the GNU General Public License version 2 (or
// any later version), both of which can be found in the LICENSE file.

package truetype

// This file implements part of the Truetype bytecode interpreter: point movement instructions.

import (
	"errors"

	"golang.org/x/image/math/fixed"
)

// ALIGNPTS: aligns two points.
func (h *hinter) alignpts(top int) (int, error) {
	p := h.point(1, current, h.stack[top])
	q := h.point(0, current, h.stack[top+1])
	if p == nil || q == nil {
		return top, errors.New("truetype: hinting: point out of range")
	}
	d := dotProduct(fixed.Int26_6(q.X-p.X), fixed.Int26_6(q.Y-p.Y), h.gs.pv) / 2
	h.move(p, +d, true)
	h.move(q, -d, true)
	return top, nil
}

// MDAP: moves a direct absolute point.
func (h *hinter) mdap(opcode uint8, top int) (int, error) {
	i := h.stack[top]
	p := h.point(0, current, i)
	if p == nil {
		return top, errors.New("truetype: hinting: point out of range")
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
	return top, nil
}

// SHP: shifts points by the displacement of a reference point.
func (h *hinter) shp(opcode uint8, top int) (int, error) {
	if top < int(h.gs.loop) {
		return top, errors.New("truetype: hinting: stack underflow")
	}
	_, _, d, ok := h.displacement(opcode&1 == 0)
	if !ok {
		return top, errors.New("truetype: hinting: point out of range")
	}
	for ; h.gs.loop != 0; h.gs.loop-- {
		top--
		p := h.point(2, current, h.stack[top])
		if p == nil {
			return top, errors.New("truetype: hinting: point out of range")
		}
		h.move(p, d, true)
	}
	h.gs.loop = 1
	return top, nil
}

// SHC: shifts a contour by the displacement of a reference point.
func (h *hinter) shc(opcode uint8, top int) (int, error) {
	top--
	zonePointer, i, d, ok := h.displacement(opcode&1 == 0)
	if !ok {
		return top, errors.New("truetype: hinting: point out of range")
	}
	if h.gs.zp[2] == 0 {
		// TODO: implement this when we have a glyph that does this.
		return top, errors.New("hinting: unimplemented SHC instruction")
	}
	contour := h.stack[top]
	if contour < 0 || len(h.ends) <= int(contour) {
		return top, errors.New("truetype: hinting: contour out of range")
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
	return top, nil
}

// SHZ: shifts a zone by the displacement of a reference point.
func (h *hinter) shz(opcode uint8, top int) (int, error) {
	top--
	zonePointer, i, d, ok := h.displacement(opcode&1 == 0)
	if !ok {
		return top, errors.New("truetype: hinting: point out of range")
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
	return top, nil
}

// SHPIX: shifts points by a pixel amount.
func (h *hinter) shpix(top int) (int, error) {
	d := fixed.Int26_6(h.stack[top])
	if top < int(h.gs.loop) {
		return top, errors.New("truetype: hinting: stack underflow")
	}
	for ; h.gs.loop != 0; h.gs.loop-- {
		top--
		p := h.point(2, current, h.stack[top])
		if p == nil {
			return top, errors.New("truetype: hinting: point out of range")
		}
		h.move(p, d, true)
	}
	h.gs.loop = 1
	return top, nil
}

// IP: interpolates points.
func (h *hinter) ip(top int) (int, error) {
	if top < int(h.gs.loop) {
		return top, errors.New("truetype: hinting: stack underflow")
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
	return top, nil
}

// MSIRP: moves a stack indirect relative point.
func (h *hinter) msirp(opcode uint8, top int) (int, error) {
	top -= 2
	i := h.stack[top]
	distance := fixed.Int26_6(h.stack[top+1])

	// TODO: special case h.gs.zp[1] == 0 in C Freetype.
	ref := h.point(0, current, h.gs.rp[0])
	p := h.point(1, current, i)
	if ref == nil || p == nil {
		return top, errors.New("truetype: hinting: point out of range")
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
	return top, nil
}

// ALIGNRP: aligns points to the reference point.
func (h *hinter) alignrp(top int) (int, error) {
	if top < int(h.gs.loop) {
		return top, errors.New("truetype: hinting: stack underflow")
	}
	ref := h.point(0, current, h.gs.rp[0])
	if ref == nil {
		return top, errors.New("truetype: hinting: point out of range")
	}
	for ; h.gs.loop != 0; h.gs.loop-- {
		top--
		p := h.point(1, current, h.stack[top])
		if p == nil {
			return top, errors.New("truetype: hinting: point out of range")
		}
		h.move(p, -dotProduct(p.X-ref.X, p.Y-ref.Y, h.gs.pv), true)
	}
	h.gs.loop = 1
	return top, nil
}

// MIAP: moves an indirect absolute point.
func (h *hinter) miap(opcode uint8, top int) (int, error) {
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
	return top, nil
}

// SCFS: sets the coordinate of a point along the freedom vector.
func (h *hinter) scfs(top int) (int, error) {
	top -= 2
	i := h.stack[top]
	p := h.point(2, current, i)
	if p == nil {
		return top, errors.New("truetype: hinting: point out of range")
	}
	c := dotProduct(p.X, p.Y, h.gs.pv)
	h.move(p, fixed.Int26_6(h.stack[top+1])-c, true)
	if h.gs.zp[2] != 0 {
		return top, nil
	}
	q := h.point(2, unhinted, i)
	if q == nil {
		return top, errors.New("truetype: hinting: point out of range")
	}
	q.X = p.X
	q.Y = p.Y
	return top, nil
}

// MD: measures the distance between two points.
func (h *hinter) md(opcode uint8, top int) (int, error) {
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
		return top, errors.New("truetype: hinting: point out of range")
	}
	d := int32(dotProduct(p.X-q.X, p.Y-q.Y, v))
	if scale {
		d = int32(int64(d*int32(h.scale)) / int64(h.font.fUnitsPerEm))
	}
	h.stack[top-1] = d
	return top, nil
}

// FLIPPT: flips points between on-curve and off-curve.
func (h *hinter) flippt(top int) (int, error) {
	if top < int(h.gs.loop) {
		return top, errors.New("truetype: hinting: stack underflow")
	}
	points := h.points[glyphZone][current]
	for ; h.gs.loop != 0; h.gs.loop-- {
		top--
		i := h.stack[top]
		if i < 0 || len(points) <= int(i) {
			return top, errors.New("truetype: hinting: point out of range")
		}
		points[i].Flags ^= flagOnCurve
	}
	h.gs.loop = 1
	return top, nil
}

// FLIPRGON and FLIPRGOFF: flip a range of points.
func (h *hinter) fliprg(opcode uint8, top int) (int, error) {
	top -= 2
	i, j, points := h.stack[top], h.stack[top+1], h.points[glyphZone][current]
	if i < 0 || len(points) <= int(i) || j < 0 || len(points) <= int(j) {
		return top, errors.New("truetype: hinting: point out of range")
	}
	for ; i <= j; i++ {
		if opcode == opFLIPRGON {
			points[i].Flags |= flagOnCurve
		} else {
			points[i].Flags &^= flagOnCurve
		}
	}
	return top, nil
}
