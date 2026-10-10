package face

import (
	"bytes"
	"encoding/binary"
	"fmt"

	"github.com/anaelorlinski/aaoo.textengine-go/sfnt"
	ot "github.com/go-text/typesetting/font/opentype"
)

// VerticalRule selects which of the ascents and descents a font states are
// used: where the text sits in its line, and how far apart lines are.
//
// A font states them up to three times: in its hhea table, and twice in its
// OS/2 table, as "typographic" and as "Windows" values. They often differ:
// Gill Sans has 0.68 and 0.32 em in hhea, with a line gap of 0.2 em, and 0.93
// and 0.24 em as Windows values.
type VerticalRule uint8

const (
	// ShaperRule takes the typographic values when the font asks for them
	// (USE_TYPO_METRICS), the hhea ones otherwise: what go-text, HarfBuzz,
	// FreeType and the browsers of macOS and Linux do.
	ShaperRule VerticalRule = iota
	// MicrosoftRule takes the typographic values when the font asks for
	// them, the Windows ones otherwise, and the hhea ones when OS/2 has
	// none: what the OpenType specification recommends, see
	// https://learn.microsoft.com/en-us/typography/opentype/spec/recom#baseline-to-baseline-distances
	MicrosoftRule
)

// Vertical holds the vertical metrics a font states, as they are in its
// tables, in font units. They are those of the default instance of a
// variable font.
type Vertical struct {
	// hhea table; the descender is negative
	HheaAscender, HheaDescender, HheaLineGap int16
	// OS/2 table, typographic values; the descender is negative
	TypoAscender, TypoDescender, TypoLineGap int16
	// OS/2 table, Windows values; both are positive
	WinAscent, WinDescent uint16
	// UseTypoMetrics is the USE_TYPO_METRICS bit of OS/2.fsSelection.
	UseTypoMetrics bool
}

var (
	tagHhea = ot.MustNewTag("hhea")
	tagOS2  = ot.MustNewTag("OS/2")
)

// ReadVertical reads the vertical metrics of the font at the given index of
// a TTF, OTF, TTC, WOFF, WOFF2 or EOT file. It reads the two tables that hold
// them and nothing else of the font.
func ReadVertical(data []byte, index int) (Vertical, error) {
	data, err := sfnt.ToSFNT(data)
	if err != nil {
		return Vertical{}, err
	}
	loaders, err := ot.NewLoaders(bytes.NewReader(data))
	if err != nil {
		return Vertical{}, err
	}
	if index < 0 || index >= len(loaders) {
		return Vertical{}, fmt.Errorf("font index %d out of range: the file holds %d fonts", index, len(loaders))
	}
	return readVertical(loaders[index]), nil
}

// readVertical leaves at zero the values of a table that is missing or too
// short to hold them.
func readVertical(ld *ot.Loader) Vertical {
	var v Vertical
	if b, err := ld.RawTable(tagHhea); err == nil && len(b) >= 10 {
		v.HheaAscender = int16(binary.BigEndian.Uint16(b[4:]))
		v.HheaDescender = int16(binary.BigEndian.Uint16(b[6:]))
		v.HheaLineGap = int16(binary.BigEndian.Uint16(b[8:]))
	}
	// The first version of OS/2 written by Apple stops before these fields.
	if b, err := ld.RawTable(tagOS2); err == nil && len(b) >= 78 {
		v.UseTypoMetrics = binary.BigEndian.Uint16(b[62:])&0x0080 != 0
		v.TypoAscender = int16(binary.BigEndian.Uint16(b[68:]))
		v.TypoDescender = int16(binary.BigEndian.Uint16(b[70:]))
		v.TypoLineGap = int16(binary.BigEndian.Uint16(b[72:]))
		v.WinAscent = binary.BigEndian.Uint16(b[74:])
		v.WinDescent = binary.BigEndian.Uint16(b[76:])
	}
	return v
}

// Resolve returns the ascent, the descent and the line gap the rule selects,
// in font units. The three are positive or zero: the descent is a distance
// below the baseline.
func (v Vertical) Resolve(rule VerticalRule) (ascent, descent, lineGap float32) {
	ascent = float32(max(v.HheaAscender, 0))
	descent = float32(max(-int32(v.HheaDescender), 0))
	lineGap = float32(max(v.HheaLineGap, 0))

	switch {
	case v.UseTypoMetrics:
		if v.TypoAscender > 0 && v.TypoDescender < 0 {
			ascent, descent = float32(v.TypoAscender), float32(-int32(v.TypoDescender))
			lineGap = float32(max(v.TypoLineGap, 0))
		}
	case rule == MicrosoftRule:
		if v.WinAscent != 0 && v.WinDescent != 0 {
			ascent, descent = float32(v.WinAscent), float32(v.WinDescent)
			// what is left of the hhea line once the Windows box is taken
			hhea := int32(v.HheaAscender) - int32(v.HheaDescender) + int32(v.HheaLineGap)
			lineGap = float32(max(hhea-int32(v.WinAscent)-int32(v.WinDescent), 0))
		}
	}
	return ascent, descent, lineGap
}
