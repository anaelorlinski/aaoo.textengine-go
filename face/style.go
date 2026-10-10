package face

import "github.com/go-text/typesetting/font"

// The usual weights of a font, on the scale of CSS and of OpenType.
const (
	WeightThin       = 100
	WeightExtraLight = 200
	WeightLight      = 300
	WeightRegular    = 400
	WeightMedium     = 500
	WeightSemiBold   = 600
	WeightBold       = 700
	WeightExtraBold  = 800
	WeightBlack      = 900
)

// Width is the width of a font within its family, on the scale of the
// usWidthClass field of OpenType: 1 for ultra-condensed to 9 for
// ultra-expanded. The zero value stands for WidthNormal.
type Width uint8

const (
	WidthUltraCondensed Width = iota + 1
	WidthExtraCondensed
	WidthCondensed
	WidthSemiCondensed
	WidthNormal
	WidthSemiExpanded
	WidthExpanded
	WidthExtraExpanded
	WidthUltraExpanded
)

// stretches are the widths as go-text has them: a ratio to the normal width.
var stretches = [...]font.Stretch{
	WidthUltraCondensed: font.StretchUltraCondensed,
	WidthExtraCondensed: font.StretchExtraCondensed,
	WidthCondensed:      font.StretchCondensed,
	WidthSemiCondensed:  font.StretchSemiCondensed,
	WidthNormal:         font.StretchNormal,
	WidthSemiExpanded:   font.StretchSemiExpanded,
	WidthExpanded:       font.StretchExpanded,
	WidthExtraExpanded:  font.StretchExtraExpanded,
	WidthUltraExpanded:  font.StretchUltraExpanded,
}

// widthOf returns the width class the closest to a ratio.
func widthOf(stretch font.Stretch) Width {
	best, gap := WidthNormal, font.Stretch(-1)
	for w := WidthUltraCondensed; w <= WidthUltraExpanded; w++ {
		d := stretches[w] - stretch
		if d < 0 {
			d = -d
		}
		if gap < 0 || d < gap {
			best, gap = w, d
		}
	}
	return best
}

// Style is what tells the fonts of a family apart.
type Style struct {
	// Weight is from 1 to 1000, see WeightRegular and the like. The zero
	// value stands for WeightRegular.
	Weight int
	// Italic is set for an italic or an oblique font.
	Italic bool
	Width  Width
}

// normalized returns the style with its zero values replaced by what they
// stand for.
func (s Style) normalized() Style {
	if s.Weight <= 0 {
		s.Weight = WeightRegular
	}
	if s.Width < WidthUltraCondensed || s.Width > WidthUltraExpanded {
		s.Width = WidthNormal
	}
	return s
}

// Aspect returns the style as go-text has it.
func (s Style) Aspect() font.Aspect {
	s = s.normalized()
	out := font.Aspect{Style: font.StyleNormal, Weight: font.Weight(s.Weight), Stretch: stretches[s.Width]}
	if s.Italic {
		out.Style = font.StyleItalic
	}
	return out
}

// StyleOf returns the style for an aspect of go-text.
func StyleOf(aspect font.Aspect) Style {
	return Style{
		Weight: int(aspect.Weight),
		Italic: aspect.Style == font.StyleItalic,
		Width:  widthOf(aspect.Stretch),
	}.normalized()
}

// Description is what a font says of itself, or what it is registered as.
type Description struct {
	Family string
	Style
}

// Describe returns the family and the style the font states for itself, in
// its name and OS/2 tables.
func (f *Face) Describe() Description {
	d := f.ft.Describe()
	return Description{Family: d.Family, Style: StyleOf(d.Aspect)}
}
