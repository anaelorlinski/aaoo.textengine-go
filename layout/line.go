package layout

import (
	"slices"

	"github.com/go-text/typesetting/font"
	"github.com/go-text/typesetting/shaping"
)

// FirstLine exposes the result of laying out
// one line of text
type FirstLine struct {
	// Output layout containing (at least) the first line
	Layout *Line

	// Length in runes of the first line
	Length int

	// ResumeAt is the number of runes to skip for the next line.
	// May be -1 if the whole text fits in one line.
	// This may be greater than [Length] in case of preserved
	// newline characters or white space collapse.
	ResumeAt int

	// Width is the width in pixels of the first line
	Width float32

	// Height is the height in pixels of the first line,
	// computed by merging the font extents (y position and height)
	// of each run in the line.
	Height float32

	// Baseline is the baseline in pixels of the first line
	Baseline float32

	FirstLineRTL bool // true is the first line direction is RTL
}

// Line is a line of text laid out: its glyphs, positioned on one line.
type Line struct {
	text  []rune
	Style *Style
	// Runs are the shaped runs of the line, in visual order.
	Runs shaping.Line

	MaxWidth float32 // input constraint, not always respected. Inf for no constraint

	justification float32
}

// clone returns a copy of the line whose runs and glyphs may be modified.
func (l *Line) clone() *Line {
	if l == nil {
		return nil
	}
	out := *l
	out.Runs = slices.Clone(l.Runs)
	for i := range out.Runs {
		out.Runs[i].Glyphs = slices.Clone(l.Runs[i].Glyphs)
	}
	return &out
}

// Text returns a readonly slice of the text in the layout
func (l Line) Text() []rune { return l.text }

// Metrics returns the metrics of the lines decorating the text. It is the
// zero value when the style is not [Style.Decorated].
func (l Line) Metrics() LineMetrics {
	if !l.Style.Decorated || len(l.Runs) == 0 {
		return LineMetrics{}
	}

	face := l.Runs[0].Face
	factor := l.Style.FontDescription.Size / float32(face.Upem())

	extents, _ := face.FontHExtents()
	return LineMetrics{
		Ascent:                 extents.Ascender * factor,
		UnderlinePosition:      face.LineMetric(font.UnderlinePosition) * factor,
		UnderlineThickness:     face.LineMetric(font.UnderlineThickness) * factor,
		StrikethroughPosition:  face.LineMetric(font.StrikethroughPosition) * factor,
		StrikethroughThickness: face.LineMetric(font.StrikethroughThickness) * factor,
	}
}

// Justification returns the current justification
func (l Line) Justification() float32 { return l.justification }

// SetJustification add an additional spacing between words
// to justify text. Depending on the implementation, it
// may be ignored until [ApplyJustification] is called.
func (l *Line) SetJustification(spacing float32) {
	l.justification = spacing
}

func (l *Line) ApplyJustification() {
	shaping.AddSpacing(l.Runs, l.text, floatToFixed(l.justification), 0)
}

// returns a prefix of text
func ShortTextHintRunes(text []rune, maxWidth, fontSize float32) []rune {
	// Try to use a small amount of text to avoid the whole layout. We need
	// at least one line, and one possible line break point on the second line.
	cut := len(text)

	const ratio = 4 // number that almost always respects char_height / char_width > ratio
	if fontSize*ratio > maxWidth {
		// Trying to find minimum or very small size, let's naively split on
		// spaces and keep one word + one letter.
		if spaceIndex := IndexRune(text, ' '); spaceIndex != -1 {
			cut = spaceIndex + 2 // index + space + one letter
		}
	} else {
		// Use the magic ratio and hope that we’ll get the right amount of text.
		cut = int(maxWidth / fontSize * ratio)
	}

	if cut > len(text) {
		cut = len(text)
	}

	return text[:cut]
}

func IndexRune(text []rune, s rune) int {
	for i, v := range text {
		if v == s {
			return i
		}
	}
	return -1
}

func HasPrefix(s []rune, r rune) bool {
	return len(s) != 0 && s[0] == r
}

func HasSuffix(s []rune, r rune) bool {
	return len(s) != 0 && s[len(s)-1] == r
}

func TrimSuffix(s []rune, r rune) []rune {
	for len(s) > 0 && s[len(s)-1] == r {
		s = s[:len(s)-1]
	}
	return s
}

func TrimPrefix(s []rune, r rune) []rune {
	for len(s) > 0 && s[0] == r {
		s = s[1:]
	}
	return s
}
