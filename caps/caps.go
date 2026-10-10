// Package caps simulates the small and petite capitals of a font that has
// none: lower-case letters are drawn as capitals at a smaller size.
//
// It works on the runs handed to the shaper of go-text, before shaping, and
// knows nothing of where the request comes from: in CSS it is
// font-variant-caps.
package caps

import (
	"unicode"

	"github.com/go-text/typesetting/font"
	ot "github.com/go-text/typesetting/font/opentype"
	"github.com/go-text/typesetting/shaping"
	"golang.org/x/image/math/fixed"
)

// Variant is a way of drawing a text in capitals of a reduced height.
type Variant uint8

const (
	// None draws the text as it is.
	None Variant = iota
	// Small draws the lower-case letters as small capitals.
	Small
	// AllSmall draws the capitals as small capitals too.
	AllSmall
	// Petite draws the lower-case letters as petite capitals, which are
	// smaller than small ones.
	Petite
	// AllPetite draws the capitals as petite capitals too.
	AllPetite
	// Unicase mixes small capitals for the capitals with lower-case letters.
	// It is simulated as Small.
	Unicase
	// Titling asks for capitals designed for titles. They are a design and
	// not a size: they are not simulated.
	Titling
)

// The sizes of simulated small and petite capitals, as a fraction of the font
// size: the values of tdewolff/canvas.
const (
	SmallScale  = 0.85
	PetiteScale = 0.7
)

// features are the OpenType features each variant asks of a font. A font
// that lacks one of them cannot draw the variant.
var features = [...][]ot.Tag{
	Small:     {ot.MustNewTag("smcp")},
	AllSmall:  {ot.MustNewTag("smcp"), ot.MustNewTag("c2sc")},
	Petite:    {ot.MustNewTag("pcap")},
	AllPetite: {ot.MustNewTag("pcap"), ot.MustNewTag("c2pc")},
	Unicase:   {ot.MustNewTag("unic")},
	Titling:   nil,
}

// Supported reports whether a face has the features of the variant, so that
// the font draws it itself.
func Supported(face *font.Face, v Variant) bool {
	if int(v) >= len(features) {
		return true
	}
	for _, tag := range features[v] {
		if _, ok := face.Font.GSUB.FindFeatureIndex(tag); !ok {
			return false
		}
	}
	return true
}

// Scale returns the size of the simulated capitals of a variant as a
// fraction of the font size, and whether the capitals of the text are
// reduced too. The scale is 0 for a variant that is not simulated.
func Scale(v Variant) (scale float64, all bool) {
	switch v {
	case Small, Unicase:
		return SmallScale, false
	case AllSmall:
		return SmallScale, true
	case Petite:
		return PetiteScale, false
	case AllPetite:
		return PetiteScale, true
	}
	return 0, false
}

// Synthesize prepares the runs of a text for a variant the font cannot draw.
// It returns the runs to shape and the text to shape them from, of the same
// length as text. When there is nothing to simulate, they are inputs and
// text themselves.
//
// A run of lower-case letters is shaped as capitals at a smaller size. With
// the "all" variants the capitals are shaped at that size too, so that the
// whole text has one height. Everything else is left alone, as are the runs
// of a font that has the features.
func Synthesize(inputs []shaping.Input, text []rune, v Variant) ([]shaping.Input, []rune) {
	scale, all := Scale(v)
	if scale == 0 {
		return inputs, text
	}
	small := func(r rune) bool { return unicode.IsLower(r) || (all && unicode.IsUpper(r)) }

	var (
		out    []shaping.Input
		shaped []rune // text with the small letters in upper case, made when needed
	)
	for i, input := range inputs {
		if input.Face == nil || Supported(input.Face, v) {
			if out != nil {
				out = append(out, input)
			}
			continue
		}
		if out == nil {
			out = append(make([]shaping.Input, 0, len(inputs)+4), inputs[:i]...)
			shaped = make([]rune, len(text))
			copy(shaped, text)
		}
		// split the run where the letters change between small and not
		for start := input.RunStart; start < input.RunEnd; {
			isSmall := small(text[start])
			end := start + 1
			for end < input.RunEnd && small(text[end]) == isSmall {
				end++
			}
			part := input
			part.RunStart, part.RunEnd = start, end
			if isSmall {
				part.Size = fixed.Int26_6(float64(input.Size) * scale)
				for j := start; j < end; j++ {
					shaped[j] = unicode.ToUpper(text[j])
				}
			}
			out = append(out, part)
			start = end
		}
	}
	if out == nil {
		return inputs, text
	}
	for i := range out {
		out[i].Text = shaped
	}
	return out, shaped
}
