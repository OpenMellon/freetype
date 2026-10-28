// Copyright 2010 The Freetype-Go Authors. All rights reserved.
// Use of this source code is governed by your choice of either the
// FreeType License or the GNU General Public License version 2 (or
// any later version), both of which can be found in the LICENSE file.

package truetype

import "golang.org/x/image/math/fixed"

// scale returns x divided by f.fUnitsPerEm, rounded to the nearest integer.
func (f *Font) scale(x fixed.Int26_6) fixed.Int26_6 {
	if x >= 0 {
		x += fixed.Int26_6(f.fUnitsPerEm) / 2
	} else {
		x -= fixed.Int26_6(f.fUnitsPerEm) / 2
	}
	return x / fixed.Int26_6(f.fUnitsPerEm)
}

// Bounds returns the union of a Font's glyphs' bounds.
func (f *Font) Bounds(scale fixed.Int26_6) fixed.Rectangle26_6 {
	b := f.bounds
	b.Min.X = f.scale(scale * b.Min.X)
	b.Min.Y = f.scale(scale * b.Min.Y)
	b.Max.X = f.scale(scale * b.Max.X)
	b.Max.Y = f.scale(scale * b.Max.Y)
	return b
}

// FUnitsPerEm returns the number of FUnits in a Font's em-square's side.
func (f *Font) FUnitsPerEm() int32 {
	return f.fUnitsPerEm
}

// Index returns a Font's index for the given rune.
func (f *Font) Index(x rune) Index {
	c := uint32(x)
	if !f.hasCmap {
		return Index(c)
	}
	if len(f.cmGlyphIDArray) > int(c) {
		val := f.cmGlyphIDArray[c]
		return Index(val)
	}

	if len(f.cmGlyphIDArrayUint16)+int(f.firstCode) > int(c) && int(f.firstCode) <= int(c) {
		val := f.cmGlyphIDArrayUint16[c-uint32(f.firstCode)]
		return Index(val)
	}

	if len(f.charcodeToGID) > 0 && c < 256 {
		// charcodeToGID is not empty and c is in 0..255 uint8 range
		val := f.charcodeToGID[uint8(c)]
		return Index(val)
	}

	for i, j := 0, len(f.cm); i < j; {
		h := i + (j-i)/2
		cm := &f.cm[h]
		if c < cm.start {
			j = h
		} else if cm.end < c {
			i = h + 1
		} else if cm.offset == 0 {
			return Index(c + cm.delta)
		} else {
			offset := int(cm.offset) + 2*(h-len(f.cm)+int(c-cm.start))
			return Index(u16(f.cmapIndexes, offset))
		}
	}
	return 0
}

// Name returns the Font's name value for the given NameID. It returns "" if
// there was an error, or if that name was not found.
func (f *Font) Name(id NameID) string {
	x, platformID, err := parseSubtables(f.name, "name", 6, 12, func(b []byte) bool {
		return NameID(u16(b, 6)) == id
	})
	if err != nil {
		return ""
	}
	offset, length := u16(f.name, 4)+u16(f.name, x+10), u16(f.name, x+8)
	// Return the ASCII value of the encoded string.
	// The string is encoded as UTF-16 on non-Apple platformIDs; Apple is platformID 1.
	src := f.name[offset : offset+length]
	var dst []byte
	if platformID != 1 { // UTF-16.
		if len(src)&1 != 0 {
			return ""
		}
		dst = make([]byte, len(src)/2)
		for i := range dst {
			dst[i] = printable(u16(src, 2*i))
		}
	} else { // ASCII.
		dst = make([]byte, len(src))
		for i, c := range src {
			dst[i] = printable(uint16(c))
		}
	}
	return string(dst)
}

// HasShortCmap returns true if font use simple encoding.
func (f *Font) HasShortCmap() bool {
	return f.hasShortCmap
}

// HasCmap returns true if font has a cmap. Still the font without cmap may be acceptable for Identity-H or Identity-V encoded fonts.
func (f *Font) HasCmap() bool {
	return f.hasCmap
}

func printable(r uint16) byte {
	if 0x20 <= r && r < 0x7f {
		return byte(r)
	}
	return '?'
}

// unscaledHMetric returns the unscaled horizontal metrics for the glyph with
// the given index.
func (f *Font) unscaledHMetric(i Index) (h HMetric) {
	j := int(i)
	if j < 0 || f.nGlyph <= j || f.nHMetric <= 0 {
		return HMetric{}
	}
	if j >= f.nHMetric {
		p := 4 * (f.nHMetric - 1)
		q := p + 2*(j-f.nHMetric) + 4
		if p+2 > len(f.hmtx) || q+2 > len(f.hmtx) {
			return HMetric{}
		}
		return HMetric{
			AdvanceWidth:    fixed.Int26_6(u16(f.hmtx, p)),
			LeftSideBearing: fixed.Int26_6(int16(u16(f.hmtx, q))),
		}
	}
	if 4*j+4 > len(f.hmtx) {
		return HMetric{}
	}
	return HMetric{
		AdvanceWidth:    fixed.Int26_6(u16(f.hmtx, 4*j)),
		LeftSideBearing: fixed.Int26_6(int16(u16(f.hmtx, 4*j+2))),
	}
}

// HMetric returns the horizontal metrics for the glyph with the given index.
func (f *Font) HMetric(scale fixed.Int26_6, i Index) HMetric {
	h := f.unscaledHMetric(i)
	h.AdvanceWidth = f.scale(scale * h.AdvanceWidth)
	h.LeftSideBearing = f.scale(scale * h.LeftSideBearing)
	return h
}

// unscaledVMetric returns the unscaled vertical metrics for the glyph with
// the given index. yMax is the top of the glyph's bounding box.
func (f *Font) unscaledVMetric(i Index, yMax fixed.Int26_6) (v VMetric) {
	j := int(i)
	if j < 0 || f.nGlyph <= j {
		return VMetric{}
	}
	if 4*j+4 <= len(f.vmtx) {
		return VMetric{
			AdvanceHeight:  fixed.Int26_6(u16(f.vmtx, 4*j)),
			TopSideBearing: fixed.Int26_6(int16(u16(f.vmtx, 4*j+2))),
		}
	}
	// The OS/2 table has grown over time.
	// https://developer.apple.com/fonts/TTRefMan/RM06/Chap6OS2.html
	// says that it was originally 68 bytes. Optional fields, including
	// the ascender and descender, are described at
	// http://www.microsoft.com/typography/otspec/os2.htm
	if len(f.os2) >= 72 {
		sTypoAscender := fixed.Int26_6(int16(u16(f.os2, 68)))
		sTypoDescender := fixed.Int26_6(int16(u16(f.os2, 70)))
		return VMetric{
			AdvanceHeight:  sTypoAscender - sTypoDescender,
			TopSideBearing: sTypoAscender - yMax,
		}
	}
	return VMetric{
		AdvanceHeight:  fixed.Int26_6(f.fUnitsPerEm),
		TopSideBearing: 0,
	}
}

// VMetric returns the vertical metrics for the glyph with the given index.
func (f *Font) VMetric(scale fixed.Int26_6, i Index) VMetric {
	// TODO: should 0 be bounds.YMax?
	v := f.unscaledVMetric(i, 0)
	v.AdvanceHeight = f.scale(scale * v.AdvanceHeight)
	v.TopSideBearing = f.scale(scale * v.TopSideBearing)
	return v
}

// Kern returns the horizontal adjustment for the given glyph pair. A positive
// kern means to move the glyphs further apart.
func (f *Font) Kern(scale fixed.Int26_6, i0, i1 Index) fixed.Int26_6 {
	if f.nKern == 0 {
		return 0
	}
	g := uint32(i0)<<16 | uint32(i1)
	lo, hi := 0, f.nKern
	for lo < hi {
		i := (lo + hi) / 2
		ig := u32(f.kern, 18+6*i)
		if ig < g {
			lo = i + 1
		} else if ig > g {
			hi = i
		} else {
			return f.scale(scale * fixed.Int26_6(int16(u16(f.kern, 22+6*i))))
		}
	}
	return 0
}

// Parse returns a new Font for the given TTF or TTC data.
//
// For TrueType Collections, the first font in the collection is parsed.
func Parse(ttf []byte) (font *Font, err error) {
	return parse(ttf, 0)
}

func parse(ttf []byte, offset int) (font *Font, err error) {
	if len(ttf)-offset < 12 {
		err = FormatError("TTF data is too short")
		return
	}
	originalOffset := offset
	magic, offset := u32(ttf, offset), offset+4
	switch magic {
	case 0x00010000:
		// No-op.
	case 0x74727565:
		// No-op.
	case 0x74746366: // "ttcf" as a big-endian uint32.
		if originalOffset != 0 {
			err = FormatError("recursive TTC")
			return
		}
		ttcVersion, offset := u32(ttf, offset), offset+4
		if ttcVersion != 0x00010000 && ttcVersion != 0x00020000 {
			err = FormatError("bad TTC version")
			return
		}
		numFonts, offset := int(u32(ttf, offset)), offset+4
		if numFonts <= 0 {
			err = FormatError("bad number of TTC fonts")
			return
		}
		if len(ttf[offset:])/4 < numFonts {
			err = FormatError("TTC offset table is too short")
			return
		}
		// TODO: provide an API to select which font in a TrueType collection to return,
		// not just the first one. This may require an API to parse a TTC's name tables,
		// so users of this package can select the font in a TTC by name.
		offset = int(u32(ttf, offset))
		if offset <= 0 || offset > len(ttf) {
			err = FormatError("bad TTC offset")
			return
		}
		return parse(ttf, offset)
	default:
		err = FormatError("bad TTF version")
		return
	}
	n, offset := int(u16(ttf, offset)), offset+2
	offset += 6 // Skip the searchRange, entrySelector and rangeShift.
	if len(ttf) < 16*n+offset {
		err = FormatError("TTF data is too short")
		return
	}
	f := new(Font)
	// Assign the table slices.
	for i := 0; i < n; i++ {
		x := 16*i + offset
		switch string(ttf[x : x+4]) {
		case "cmap":
			f.cmap, err = readTable(ttf, ttf[x+8:x+16])
		case "cvt ":
			f.cvt, err = readTable(ttf, ttf[x+8:x+16])
		case "fpgm":
			f.fpgm, err = readTable(ttf, ttf[x+8:x+16])
		case "glyf":
			f.glyf, err = readTable(ttf, ttf[x+8:x+16])
		case "hdmx":
			f.hdmx, err = readTable(ttf, ttf[x+8:x+16])
		case "head":
			f.head, err = readTable(ttf, ttf[x+8:x+16])
		case "hhea":
			f.hhea, err = readTable(ttf, ttf[x+8:x+16])
		case "hmtx":
			f.hmtx, err = readTable(ttf, ttf[x+8:x+16])
		case "kern":
			f.kern, err = readTable(ttf, ttf[x+8:x+16])
		case "loca":
			f.loca, err = readTable(ttf, ttf[x+8:x+16])
		case "maxp":
			f.maxp, err = readTable(ttf, ttf[x+8:x+16])
		case "name":
			f.name, err = readTable(ttf, ttf[x+8:x+16])
		case "OS/2":
			f.os2, err = readTable(ttf, ttf[x+8:x+16])
		case "prep":
			f.prep, err = readTable(ttf, ttf[x+8:x+16])
		case "vmtx":
			f.vmtx, err = readTable(ttf, ttf[x+8:x+16])
		}
		if err != nil {
			return
		}
	}
	// Parse and sanity-check the TTF data.
	if err = f.parseHead(); err != nil {
		return
	}
	if err = f.parseMaxp(); err != nil {
		return
	}
	if err = f.parseCmap(); err != nil {
		return
	}
	if err = f.parseKern(); err != nil {
		return
	}
	if err = f.parseHhea(); err != nil {
		return
	}
	font = f
	return
}
