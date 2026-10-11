// Package caps simulates the small and petite capitals of a font that has
// none: lower-case letters are drawn as capitals at a smaller size.
//
// It works on the runs handed to the shaper of go-text, before shaping, and
// knows nothing of where the request comes from: in CSS it is
// font-variant-caps.
package caps

import (
	"math"
	"unicode"

	"github.com/go-text/typesetting/font"
	ot "github.com/go-text/typesetting/font/opentype"
	"github.com/go-text/typesetting/font/opentype/tables"
	"github.com/go-text/typesetting/language"
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

// Supported reports whether a face has the features of the variant at all.
//
// It says nothing of a given text: a font often has small capitals for the
// Latin letters only. Synthesize looks at each letter.
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

// substitutes reports whether a feature of the font replaces the glyph: one
// of its lookups covers it. The script and the language of the text are not
// looked at, nor is the context a lookup may ask for: a letter the feature
// names is taken as drawn by the font.
func substitutes(face *font.Face, tag ot.Tag, gid font.GID) bool {
	gsub := &face.Font.GSUB
	for _, feature := range gsub.Features {
		if feature.Tag != tag {
			continue
		}
		for _, index := range feature.LookupListIndices {
			if int(index) >= len(gsub.Lookups) {
				continue
			}
			for _, subtable := range gsub.Lookups[index].Subtables {
				if _, ok := subtable.Cov().Index(tables.GlyphID(gid)); ok {
					return true
				}
			}
		}
	}
	return false
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

// upper returns the capital of a letter in a language: Turkish and Azeri
// have a dotted capital for i.
func upper(r rune, lang language.Language) rune {
	if l := string(lang); len(l) >= 2 && (l[:2] == "tr" || l[:2] == "az") && (len(l) == 2 || l[2] == '-') {
		return unicode.TurkishCase.ToUpper(r)
	}
	return unicode.ToUpper(r)
}

// attaches reports whether a rune belongs with the one before it: a
// combining mark, a joiner or a variation selector. It is shaped with its
// base, at the size of its base, or the font cannot place it.
func attaches(r rune) bool {
	return unicode.Is(unicode.M, r) || unicode.Is(unicode.Cf, r)
}

// Synthesize prepares the runs of a text for a variant the font cannot draw.
// It returns the runs to shape and the text to shape them from, of the same
// length as text. When there is nothing to simulate, they are inputs and
// text themselves.
//
// A lower-case letter is shaped as its capital at a smaller size. With the
// "all" variants the capitals are shaped at that size too, so that the whole
// text has one height. The letters the font draws itself are left alone: the
// decision is taken letter by letter, since a font may have small capitals
// for some scripts only. So is a letter that has no capital of one
// character, or whose capital the font lacks.
func Synthesize(inputs []shaping.Input, text []rune, v Variant) ([]shaping.Input, []rune) {
	scale, all := Scale(v)
	if scale == 0 {
		return inputs, text
	}
	tags := features[v]

	var (
		out    []shaping.Input
		shaped []rune // text with the small letters in upper case, made when needed
		small  []bool // for the runes of one input: shaped at the small size
	)
	for i, input := range inputs {
		changed := false
		if input.Face != nil {
			small = small[:0]
			for j := input.RunStart; j < input.RunEnd; j++ {
				r := text[j]
				is, capital := false, r
				switch {
				case attaches(r):
					// as its base, whose capital it follows too
					is = j > input.RunStart && small[j-input.RunStart-1]
				case unicode.IsLower(r):
					gid, has := input.Face.NominalGlyph(r)
					if has && len(tags) > 0 && substitutes(input.Face, tags[0], gid) {
						break // the font draws it
					}
					capital = upper(r, input.Language)
					if _, ok := input.Face.NominalGlyph(capital); capital == r || !ok {
						capital = r
						break // no capital to draw it with
					}
					is = true
				case all && unicode.IsUpper(r):
					gid, has := input.Face.NominalGlyph(r)
					is = !(has && len(tags) > 1 && substitutes(input.Face, tags[1], gid))
				}
				small = append(small, is)
				if is {
					changed = true
					if capital != r {
						if shaped == nil {
							shaped = append([]rune(nil), text...)
						}
						shaped[j] = capital
					}
				}
			}
		}
		if !changed {
			if out != nil {
				out = append(out, input)
			}
			continue
		}
		if out == nil {
			out = append(make([]shaping.Input, 0, len(inputs)+4), inputs[:i]...)
		}
		// split the run where the letters change between small and not
		for start := input.RunStart; start < input.RunEnd; {
			isSmall := small[start-input.RunStart]
			end := start + 1
			for end < input.RunEnd && small[end-input.RunStart] == isSmall {
				end++
			}
			part := input
			part.RunStart, part.RunEnd = start, end
			if isSmall {
				part.Size = fixed.Int26_6(math.Round(float64(input.Size) * scale))
			}
			out = append(out, part)
			start = end
		}
	}
	if out == nil {
		return inputs, text
	}
	if shaped == nil {
		return out, text // capitals made smaller, no letter changed
	}
	for i := range out {
		out[i].Text = shaped
	}
	return out, shaped
}
