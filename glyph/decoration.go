package glyph

import "github.com/anaelorlinski/aaoo.textengine-go/face"

// Line is a horizontal bar of a text decoration: it spans the width of the
// decorated text, from Top-Thickness to Top, with the baseline at 0.
//
// A bar is given by its top edge because fonts state it that way: a thicker
// or a thinner bar than the font asks for then grows downwards.
type Line struct {
	Top       float64 // top edge of the bar, positive above the baseline
	Thickness float64
}

// Center returns the middle of the bar.
func (l Line) Center() float64 { return l.Top - l.Thickness/2 }

// Decorations are the lines a font asks for at a given size: what a text
// decoration is drawn with when nothing else, as a CSS property, says where
// it goes and how thick it is.
//
// These are the values of the font, to a fraction of a pixel. Rounding a
// thickness to whole pixels is the choice of who draws on a pixel grid.
type Decorations struct {
	Underline, Overline, Strikethrough Line
	// XHeight is the height of the letter x, for those who place the
	// strikethrough themselves: in the middle of it.
	XHeight float64
}

// DecorationsOf returns the decoration lines of a face at the given size.
// rule selects the ascent, which the overline sits on.
//
// A font that states no underline or strikethrough gets the usual fallbacks:
// a thickness of 5% of the ascent, an underline 10% of the ascent under the
// baseline, a strikethrough as thick as the underline and centred on half
// the x-height.
func DecorationsOf(f *face.Face, size float64, rule face.VerticalRule) Decorations {
	m := f.Metrics(rule)
	scale := size / float64(f.UnitsPerEm())
	ascent := float64(m.Ascent) * scale
	xHeight := float64(m.XHeight) * scale

	under := Line{Top: float64(m.UnderlinePosition) * scale, Thickness: float64(m.UnderlineThickness) * scale}
	if under.Top == 0 {
		under.Top = -0.1 * ascent
	}
	if under.Thickness <= 0 {
		under.Thickness = 0.05 * ascent
	}

	strike := Line{Top: float64(m.StrikethroughPosition) * scale, Thickness: float64(m.StrikethroughThickness) * scale}
	if strike.Thickness <= 0 {
		strike.Thickness = under.Thickness
	}
	if strike.Top == 0 {
		strike.Top = xHeight/2 + strike.Thickness/2
	}

	over := Line{Top: ascent, Thickness: under.Thickness}
	return Decorations{Underline: under, Overline: over, Strikethrough: strike, XHeight: xHeight}
}
