// Copyright 2010 The Freetype-Go Authors. All rights reserved.
// Use of this source code is governed by your choice of either the
// FreeType License or the GNU General Public License version 2 (or
// any later version), both of which can be found in the LICENSE file.

// Package truetype provides a parser for the TTF and TTC file formats.
// Those formats are documented at http://developer.apple.com/fonts/TTRefMan/
// and http://www.microsoft.com/typography/otspec/
//
// Some of a font's methods provide lengths or co-ordinates, e.g. bounds, font
// metrics and control points. All these methods take a scale parameter, which
// is the number of pixels in 1 em, expressed as a 26.6 fixed point value. For
// example, if 1 em is 10 pixels then scale is fixed.I(10), which is equal to
// fixed.Int26_6(10 << 6).
//
// To measure a TrueType font in ideal FUnit space, use scale equal to
// font.FUnitsPerEm().
package truetype // import "github.com/golang/freetype/truetype"

import (
	"fmt"

	"golang.org/x/image/math/fixed"
)

// An Index is a Font's index of a rune.
type Index uint16

// A NameID identifies a name table entry.
//
// See https://developer.apple.com/fonts/TrueType-Reference-Manual/RM06/Chap6name.html
type NameID uint16

const (
	NameIDCopyright          NameID = 0
	NameIDFontFamily         NameID = 1
	NameIDFontSubfamily      NameID = 2
	NameIDUniqueSubfamilyID  NameID = 3
	NameIDFontFullName       NameID = 4
	NameIDNameTableVersion   NameID = 5
	NameIDPostscriptName     NameID = 6
	NameIDTrademarkNotice    NameID = 7
	NameIDManufacturerName   NameID = 8
	NameIDDesignerName       NameID = 9
	NameIDFontDescription    NameID = 10
	NameIDFontVendorURL      NameID = 11
	NameIDFontDesignerURL    NameID = 12
	NameIDFontLicense        NameID = 13
	NameIDFontLicenseURL     NameID = 14
	NameIDPreferredFamily    NameID = 16
	NameIDPreferredSubfamily NameID = 17
	NameIDCompatibleName     NameID = 18
	NameIDSampleText         NameID = 19
)

const (
	// A 32-bit encoding consists of a most-significant 16-bit Platform ID and a
	// least-significant 16-bit Platform Specific ID. The magic numbers are
	// specified at https://www.microsoft.com/typography/otspec/name.htm
	//
	// The use of encoding 0, 1, and 2 is deprecated, but we support them for backward compatibility.
	unicodeEncodingDefault  = 0x00000000 // PID = 0 (Unicode), PSID = 0 (Unicode 1.0)
	unicodeEncoding11       = 0x00000001 // PID = 0 (Unicode), PSID = 1 (Unicode 1.1)
	unicodeEncodingISO10646 = 0x00000002 // PID = 0 (Unicode), PSID = 2 (ISO 10646)
	unicodeEncodingBMPOnly  = 0x00000003 // PID = 0 (Unicode), PSID = 3 (Unicode 2.0 BMP Only)
	unicodeEncodingFull     = 0x00000004 // PID = 0 (Unicode), PSID = 4 (Unicode 2.0 Full Repertoire)
	macintoshSimpleEncoding = 0x00010000 // PID = 1 (Macintosh), PSID = 1 (Macintosh)
	microsoftSymbolEncoding = 0x00030000 // PID = 3 (Microsoft), PSID = 0 (Symbol)
	microsoftUCS2Encoding   = 0x00030001 // PID = 3 (Microsoft), PSID = 1 (UCS-2)
	microsoftUCS4Encoding   = 0x0003000a // PID = 3 (Microsoft), PSID = 10 (UCS-4)
)

// An HMetric holds the horizontal metrics of a single glyph.
type HMetric struct {
	AdvanceWidth, LeftSideBearing fixed.Int26_6
}

// A VMetric holds the vertical metrics of a single glyph.
type VMetric struct {
	AdvanceHeight, TopSideBearing fixed.Int26_6
}

// A FormatError reports that the input is not a valid TrueType font.
type FormatError string

func (e FormatError) Error() string {
	return "freetype: invalid TrueType format: " + string(e)
}

// An UnsupportedError reports that the input uses a valid but unimplemented
// TrueType feature.
type UnsupportedError string

func (e UnsupportedError) Error() string {
	return "freetype: unsupported TrueType feature: " + string(e)
}

// u32 returns the big-endian uint32 at b[i:].
func u32(b []byte, i int) uint32 {
	return uint32(b[i])<<24 | uint32(b[i+1])<<16 | uint32(b[i+2])<<8 | uint32(b[i+3])
}

// u16 returns the big-endian uint16 at b[i:].
func u16(b []byte, i int) uint16 {
	return uint16(b[i])<<8 | uint16(b[i+1])
}

// readTable returns a slice of the TTF data given by a table's directory entry.
func readTable(ttf []byte, offsetLength []byte) ([]byte, error) {
	offset := int(u32(offsetLength, 0))
	if offset < 0 {
		return nil, FormatError(fmt.Sprintf("offset too large: %d", uint32(offset)))
	}
	length := int(u32(offsetLength, 4))
	if length < 0 {
		return nil, FormatError(fmt.Sprintf("length too large: %d", uint32(length)))
	}
	end := offset + length
	if end < 0 || end > len(ttf) {
		return nil, FormatError(fmt.Sprintf("offset + length too large: %d", uint32(offset)+uint32(length)))
	}
	return ttf[offset:end], nil
}

// parseSubtables returns the offset and platformID of the best subtable in
// table, where best favors a Unicode cmap encoding, and failing that, a
// Microsoft cmap encoding. offset is the offset of the first subtable in
// table, and size is the size of each subtable.
//
// If pred is non-nil, then only subtables that satisfy that predicate will be
// considered.
func parseSubtables(table []byte, name string, offset, size int, pred func([]byte) bool) (
	bestOffset int, bestPID uint32, retErr error,
) {
	if len(table) < 4 {
		return 0, 0, FormatError(name + " too short")
	}
	nSubtables := int(u16(table, 2))
	if len(table) < size*nSubtables+offset {
		return 0, 0, FormatError(name + " too short")
	}
	bestScore := -1
	for i := 0; i < nSubtables; i, offset = i+1, offset+size {
		if pred != nil && !pred(table[offset:]) {
			continue
		}
		// We read the 16-bit Platform ID and 16-bit Platform Specific ID as a single uint32.
		// All values are big-endian.
		pidPsid := u32(table, offset)
		// We prefer the Unicode cmap encoding. Failing to find that, we fall
		// back onto the Microsoft cmap encoding.
		// And we prefer full/UCS4 encoding over BMP/UCS2. So the priority goes:
		//    unicodeEncodingFull > macintoshSimpleEncoding > microsoftUCS4Encoding > unicodeEncodingBMPOnly > macintoshSimpleEncoding > microsoftUCS2Encoding > microsoftSymbolEncoding
		// It is in accord with the Psid part.
		score := int(pidPsid & 0xFFFF)
		if score <= bestScore {
			continue
		}
		switch pidPsid {
		case unicodeEncodingDefault, unicodeEncoding11, unicodeEncodingISO10646, unicodeEncodingBMPOnly, unicodeEncodingFull:
			bestOffset, bestPID, bestScore = offset, pidPsid>>16, score
		case macintoshSimpleEncoding:
			bestOffset, bestPID, bestScore = offset, pidPsid>>16, score
		case microsoftSymbolEncoding:
			bestOffset, bestPID, bestScore = offset, pidPsid>>16, score
		case microsoftUCS2Encoding, microsoftUCS4Encoding:
			bestOffset, bestPID, bestScore = offset, pidPsid>>16, score
		}
	}
	if bestScore < 0 {
		return 0, 0, UnsupportedError(name + " encoding")
	}
	return bestOffset, bestPID, nil
}

const (
	locaOffsetFormatUnknown int = iota
	locaOffsetFormatShort
	locaOffsetFormatLong
)

// A cm holds a parsed cmap entry.
type cm struct {
	start, end, delta, offset uint32
}

// A Font represents a Truetype font.
type Font struct {
	// Tables sliced from the TTF data. The different tables are documented
	// at http://developer.apple.com/fonts/TTRefMan/RM06/Chap6.html
	cmap, cvt, fpgm, glyf, hdmx, head, hhea, hmtx, kern, loca, maxp, name, os2, prep, vmtx []byte

	cmapIndexes []byte

	// Cached values derived from the raw ttf data.
	cm                      []cm
	locaOffsetFormat        int
	nGlyph, nHMetric, nKern int
	fUnitsPerEm             int32
	ascent                  int32               // In FUnits.
	descent                 int32               // In FUnits; typically negative.
	bounds                  fixed.Rectangle26_6 // In FUnits.
	// Values from the maxp section.
	maxTwilightPoints, maxStorage, maxFunctionDefs, maxStackElements uint16
	// USD-162 issue, cmap glyph id array.
	cmGlyphIDArray []uint8
	charcodeToGID  map[uint8]uint32
	hasShortCmap   bool
	// support of cmap format 6
	cmGlyphIDArrayUint16 []uint16
	firstCode            uint16

	// font without cmap can still be used with identity encoding
	hasCmap bool
}
