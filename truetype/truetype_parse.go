// Copyright 2010 The Freetype-Go Authors. All rights reserved.
// Use of this source code is governed by your choice of either the
// FreeType License or the GNU General Public License version 2 (or
// any later version), both of which can be found in the LICENSE file.

package truetype

import (
	"fmt"
	"strings"

	"golang.org/x/image/math/fixed"
)

func (f *Font) parseCmap() error {
	const (
		cmapFormat0         = 0
		cmapFormat4         = 4
		cmapFormat6         = 6
		cmapFormat12        = 12
		languageIndependent = 0
	)

	f.hasCmap = false

	offset, _, err := parseSubtables(f.cmap, "cmap", 4, 8, nil)
	if err != nil {
		if strings.Contains(err.Error(), "cmap too short") {
			return nil
		}
		return err
	}
	offset = int(u32(f.cmap, offset+4))
	if offset <= 0 || offset > len(f.cmap) {
		return FormatError("bad cmap offset")
	}

	cmapFormat := u16(f.cmap, offset)
	switch cmapFormat {
	case cmapFormat0:
		glyphIDArray := make([]uint8, 256)
		for i := 0; i < 256; i++ {
			glyphIDArray[i] = f.cmap[offset+6+i]
		}
		f.cmGlyphIDArray = glyphIDArray
		f.hasShortCmap = true
		f.hasCmap = true
		return nil
	case cmapFormat4:
		language := u16(f.cmap, offset+4)
		if language != languageIndependent {
			return UnsupportedError(fmt.Sprintf("language: %d", language))
		}
		segCountX2 := int(u16(f.cmap, offset+6))
		if segCountX2%2 == 1 {
			return FormatError(fmt.Sprintf("bad segCountX2: %d", segCountX2))
		}
		segCount := segCountX2 / 2
		offset += 14
		f.cm = make([]cm, segCount)
		for i := 0; i < segCount; i++ {
			f.cm[i].end = uint32(u16(f.cmap, offset))
			offset += 2
		}
		offset += 2
		for i := 0; i < segCount; i++ {
			f.cm[i].start = uint32(u16(f.cmap, offset))
			offset += 2
		}
		for i := 0; i < segCount; i++ {
			f.cm[i].delta = uint32(u16(f.cmap, offset))
			offset += 2
		}
		for i := 0; i < segCount; i++ {
			f.cm[i].offset = uint32(u16(f.cmap, offset))
			offset += 2
		}
		f.cmapIndexes = f.cmap[offset:]

		// If unable to parse cmap correctly, use unitype cmap parse implementation.
		if len(f.cmapIndexes) < 1 {
			charcodeMap := make(map[uint8]uint32, f.nGlyph)

			for i := 0; i < segCount; i++ {
				c1 := f.cm[i].start
				c2 := f.cm[i].end
				d := f.cm[i].delta
				rangeOffset := f.cm[i].offset

				for c := c1; c <= c2; c++ {
					var gid uint32

					if rangeOffset == 0 {
						gid = (c + d) & 0xFFFF
					} else {
						index := int(uint16(rangeOffset)/2 + (uint16(c) - uint16(c1)) + uint16(i) - uint16(offset))

						if index >= len(f.charcodeToGID) {
							return FormatError("out of bounds")
						}
						if f.charcodeToGID[uint8(index)] != 0 {
							gid = (f.charcodeToGID[uint8(index)] + d) & 0xFFFF
						} else {
							gid = 0
						}
					}

					if gid > 0 {
						if int(gid) >= int(f.nGlyph) {
							return FormatError("out of bounds")
						}
						b := byte(c)
						if _, has := charcodeMap[b]; !has {
							// Avoid overwrite, if get same twice, use the earlier entry.
							charcodeMap[b] = gid
						}
					}
				}
			}
			if len(charcodeMap) < 1 {
				return FormatError("no charcode map or cmap indexes")
			}
			f.charcodeToGID = charcodeMap
		}
		f.hasCmap = true
		return nil
	case cmapFormat6:
		firstCode := u16(f.cmap, offset+6)
		entryCount := u16(f.cmap, offset+8)
		glyphIDArray := make([]uint16, entryCount)
		for i := 0; i < int(entryCount); i++ {
			glyphIDArray[i] = u16(f.cmap, offset+10+2*i)
		}
		f.cmGlyphIDArrayUint16 = glyphIDArray
		f.hasShortCmap = true
		f.firstCode = firstCode
		f.hasCmap = true
		return nil
	case cmapFormat12:
		if u16(f.cmap, offset+2) != 0 {
			return FormatError(fmt.Sprintf("cmap format: % x", f.cmap[offset:offset+4]))
		}
		length := u32(f.cmap, offset+4)
		language := u32(f.cmap, offset+8)
		if language != languageIndependent {
			return UnsupportedError(fmt.Sprintf("language: %d", language))
		}
		nGroups := u32(f.cmap, offset+12)
		if length != 12*nGroups+16 {
			return FormatError("inconsistent cmap length")
		}
		offset += 16
		f.cm = make([]cm, nGroups)
		for i := uint32(0); i < nGroups; i++ {
			f.cm[i].start = u32(f.cmap, offset+0)
			f.cm[i].end = u32(f.cmap, offset+4)
			f.cm[i].delta = u32(f.cmap, offset+8) - f.cm[i].start
			offset += 12
		}
		f.hasCmap = true
		return nil
	}
	return UnsupportedError(fmt.Sprintf("cmap format: %d", cmapFormat))
}

func (f *Font) parseHead() error {
	if len(f.head) != 54 {
		return FormatError(fmt.Sprintf("bad head length: %d", len(f.head)))
	}
	f.fUnitsPerEm = int32(u16(f.head, 18))
	f.bounds.Min.X = fixed.Int26_6(int16(u16(f.head, 36)))
	f.bounds.Min.Y = fixed.Int26_6(int16(u16(f.head, 38)))
	f.bounds.Max.X = fixed.Int26_6(int16(u16(f.head, 40)))
	f.bounds.Max.Y = fixed.Int26_6(int16(u16(f.head, 42)))
	switch i := u16(f.head, 50); i {
	case 0:
		f.locaOffsetFormat = locaOffsetFormatShort
	case 1:
		f.locaOffsetFormat = locaOffsetFormatLong
	default:
		return FormatError(fmt.Sprintf("bad indexToLocFormat: %d", i))
	}
	return nil
}

func (f *Font) parseHhea() error {
	if len(f.hhea) < 36 {
		return FormatError(fmt.Sprintf("bad hhea length: %d", len(f.hhea)))
	}
	f.ascent = int32(int16(u16(f.hhea, 4)))
	f.descent = int32(int16(u16(f.hhea, 6)))
	f.nHMetric = int(u16(f.hhea, 34))
	if 4*f.nHMetric+2*(f.nGlyph-f.nHMetric) < len(f.hmtx) {
		return FormatError(fmt.Sprintf("bad hmtx length: %d", len(f.hmtx)))
	}
	return nil
}

func (f *Font) parseKern() error {
	// Apple's TrueType documentation (https://developer.apple.com/fonts/TrueType-Reference-Manual/RM06/Chap6kern.html) says:
	// "Previous versions of the 'kern' table defined both the version and nTables fields in the header
	// as UInt16 values and not UInt32 values. Use of the older format on the Mac OS is discouraged
	// (although AAT can sense an old kerning table and still make correct use of it). Microsoft
	// Windows still uses the older format for the 'kern' table and will not recognize the newer one.
	// Fonts targeted for the Mac OS only should use the new format; fonts targeted for both the Mac OS
	// and Windows should use the old format."
	// Since we expect that almost all fonts aim to be Windows-compatible, we only parse the "older" format,
	// just like the C Freetype implementation.
	if len(f.kern) == 0 {
		if f.nKern != 0 {
			return FormatError("bad kern table length")
		}
		return nil
	}
	if len(f.kern) < 18 {
		return FormatError("kern data too short")
	}
	version, offset := u16(f.kern, 0), 2
	if version != 0 {
		return UnsupportedError(fmt.Sprintf("kern version: %d", version))
	}

	n, offset := u16(f.kern, offset), offset+2
	if n == 0 {
		return UnsupportedError("kern nTables: 0")
	}
	// TODO: support multiple subtables. In practice, almost all .ttf files
	// have only one subtable, if they have a kern table at all. But it's not
	// impossible. Xolonium Regular (https://fontlibrary.org/en/font/xolonium)
	// has 3 subtables. Those subtables appear to be disjoint, rather than
	// being the same kerning pairs encoded in three different ways.
	//
	// For now, we'll use only the first subtable.

	offset += 2 // Skip the version.
	length, offset := int(u16(f.kern, offset)), offset+2
	coverage, offset := u16(f.kern, offset), offset+2
	if coverage != 0x0001 {
		// We only support horizontal kerning.
		return UnsupportedError(fmt.Sprintf("kern coverage: 0x%04x", coverage))
	}
	f.nKern, offset = int(u16(f.kern, offset)), offset+2
	// https://github.com/golang/freetype/issues/8
	// For large kerning tables, the extracted length value may be incorrect.
	// For such fonts, often only the lower 16 bits of the actual length are
	// stored. Here we validate the lower 16 bits, and also make sure that
	// the extracted number of kerning pairs does not exceed the total length
	// of the kerning table.
	if uint16(6*f.nKern) != uint16(length-14) || 6*f.nKern > len(f.kern)-18 {
		return FormatError("bad kern table length")
	}
	return nil
}

func (f *Font) parseMaxp() error {
	if len(f.maxp) != 32 {
		return FormatError(fmt.Sprintf("bad maxp length: %d", len(f.maxp)))
	}
	f.nGlyph = int(u16(f.maxp, 4))
	f.maxTwilightPoints = u16(f.maxp, 16)
	f.maxStorage = u16(f.maxp, 18)
	f.maxFunctionDefs = u16(f.maxp, 20)
	f.maxStackElements = u16(f.maxp, 24)
	return nil
}
