// Copyright 2010 The Freetype-Go Authors. All rights reserved.
// Use of this source code is governed by your choice of either the
// FreeType License or the GNU General Public License version 2 (or
// any later version), both of which can be found in the LICENSE file.

package truetype

import "golang.org/x/image/math/fixed"

const (
	twilightZone = 0
	glyphZone    = 1
	numZone      = 2
)

type pointType uint32

const (
	current      pointType = 0
	unhinted     pointType = 1
	inFontUnits  pointType = 2
	numPointType           = 3
)

// callStackEntry is a bytecode call stack entry.
type callStackEntry struct {
	program   []byte
	pc        int
	loopCount int32
}

// hinter implements bytecode hinting. A hinter can be re-used to hint a series
// of glyphs from a Font.
type hinter struct {
	stack, store []int32

	// functions is a map from function number to bytecode.
	functions map[int32][]byte

	// font and scale are the font and scale last used for this hinter.
	// Changing the font will require running the new font's fpgm bytecode.
	// Changing either will require running the font's prep bytecode.
	font  *Font
	scale fixed.Int26_6

	// gs and defaultGS are the current and default graphics state. The
	// default graphics state is the global default graphics state after
	// the font's fpgm and prep programs have been run.
	gs, defaultGS graphicsState

	// points and ends are the twilight zone's points, glyph's points
	// and glyph's contour boundaries.
	points [numZone][numPointType][]Point
	ends   []int

	// scaledCVT is the lazily initialized scaled Control Value Table.
	scaledCVTInitialized bool
	scaledCVT            []fixed.Int26_6
}

// graphicsState is described at https://developer.apple.com/fonts/TTRefMan/RM04/Chap4.html
type graphicsState struct {
	// Projection vector, freedom vector and dual projection vector.
	pv, fv, dv [2]f2dot14
	// Reference points and zone pointers.
	rp, zp [3]int32
	// Control Value / Single Width Cut-In.
	controlValueCutIn, singleWidthCutIn, singleWidth fixed.Int26_6
	// Delta base / shift.
	deltaBase, deltaShift int32
	// Minimum distance.
	minDist fixed.Int26_6
	// Loop count.
	loop int32
	// Rounding policy.
	roundPeriod, roundPhase, roundThreshold fixed.Int26_6
	roundSuper45                            bool
	// Auto-flip.
	autoFlip bool
}

var globalDefaultGS = graphicsState{
	pv:                [2]f2dot14{0x4000, 0}, // Unit vector along the X axis.
	fv:                [2]f2dot14{0x4000, 0},
	dv:                [2]f2dot14{0x4000, 0},
	zp:                [3]int32{1, 1, 1},
	controlValueCutIn: (17 << 6) / 16, // 17/16 as a fixed.Int26_6.
	deltaBase:         9,
	deltaShift:        3,
	minDist:           1 << 6, // 1 as a fixed.Int26_6.
	loop:              1,
	roundPeriod:       1 << 6, // 1 as a fixed.Int26_6.
	roundThreshold:    1 << 5, // 1/2 as a fixed.Int26_6.
	roundSuper45:      false,
	autoFlip:          true,
}

func resetTwilightPoints(f *Font, p []Point) []Point {
	if n := int(f.maxTwilightPoints) + 4; n <= cap(p) {
		p = p[:n]
		for i := range p {
			p[i] = Point{}
		}
	} else {
		p = make([]Point, n)
	}
	return p
}

func (h *hinter) init(f *Font, scale fixed.Int26_6) error {
	h.points[twilightZone][0] = resetTwilightPoints(f, h.points[twilightZone][0])
	h.points[twilightZone][1] = resetTwilightPoints(f, h.points[twilightZone][1])
	h.points[twilightZone][2] = resetTwilightPoints(f, h.points[twilightZone][2])

	rescale := h.scale != scale
	if h.font != f {
		h.font, rescale = f, true
		if h.functions == nil {
			h.functions = make(map[int32][]byte)
		} else {
			for k := range h.functions {
				delete(h.functions, k)
			}
		}

		if x := int(f.maxStackElements); x > len(h.stack) {
			x += 255
			x &^= 255
			h.stack = make([]int32, x)
		}
		if x := int(f.maxStorage); x > len(h.store) {
			x += 15
			x &^= 15
			h.store = make([]int32, x)
		}
		if len(f.fpgm) != 0 {
			if err := h.run(f.fpgm, nil, nil, nil, nil); err != nil {
				return err
			}
		}
	}

	if rescale {
		h.scale = scale
		h.scaledCVTInitialized = false

		h.defaultGS = globalDefaultGS

		if len(f.prep) != 0 {
			if err := h.run(f.prep, nil, nil, nil, nil); err != nil {
				return err
			}
			h.defaultGS = h.gs
			// The MS rasterizer doesn't allow the following graphics state
			// variables to be modified by the CVT program.
			h.defaultGS.pv = globalDefaultGS.pv
			h.defaultGS.fv = globalDefaultGS.fv
			h.defaultGS.dv = globalDefaultGS.dv
			h.defaultGS.rp = globalDefaultGS.rp
			h.defaultGS.zp = globalDefaultGS.zp
			h.defaultGS.loop = globalDefaultGS.loop
		}
	}
	return nil
}
