package glyph

import "github.com/anaelorlinski/aaoo.textengine-go/face"

// Positioned is a glyph of a run: what layout decided of it. The lengths are
// in the unit of the size of the run.
type Positioned struct {
	ID face.GID
	// Advance is the distance to the next glyph along the baseline. It is
	// the one layout measured: with kerning, letter spacing and
	// justification, it is not the advance the font states.
	Advance float64
	// XOffset and YOffset move the glyph from its place on the baseline,
	// to the right and up, without moving the glyphs after it.
	XOffset, YOffset float64
	// Text is what the glyph stands for, when layout knows: the characters
	// of its cluster, on the first glyph of the cluster. A ligature has
	// several characters and no entry in the character map of its font;
	// this is what lets its text be copied from a document.
	Text string
}

// Run is a row of glyphs of one face at one size, laid out from left to
// right on a baseline: what layout hands to drawing. Nothing in it is to be
// shaped again.
type Run struct {
	Face *face.Face
	// Size is the length of one em, in the caller's unit.
	Size   float64
	Glyphs []Positioned

	// Slant shears the glyphs to the right by this fraction of their height,
	// to stand in for an italic the family lacks. FauxItalicSlant is the
	// usual value.
	Slant float64
	// Bold is the distance by which the outlines are expanded (or shrunk,
	// when negative) to stand in for a weight the family lacks, in the unit
	// of Size. See FauxBoldOffset.
	Bold float64
}

// Advance returns the length of the run along its baseline.
func (r *Run) Advance() float64 {
	sum := 0.0
	for i := range r.Glyphs {
		sum += r.Glyphs[i].Advance
	}
	return sum
}

// Outline returns the outlines of the glyphs of the run, each at its place,
// with the baseline at Y = 0 and the first glyph starting at X = 0. The
// slant is applied; the expansion of Bold is not, since it needs a path
// offset, which is the caller's.
func (r *Run) Outline() Outline {
	var out Outline
	x := 0.0
	style := Style{Slant: r.Slant}
	for i := range r.Glyphs {
		g := &r.Glyphs[i]
		if o, ok := FromFace(r.Face, g.ID, r.Size, style); ok {
			// The run is slanted as a whole, offsets included: a glyph
			// raised above the baseline moves to the right with what is at
			// its height. A mark the font placed over a letter then stays
			// over it, and a renderer that shears the whole line, as a PDF
			// text matrix does, puts it at the same place.
			dx, dy := x+g.XOffset+r.Slant*g.YOffset, g.YOffset
			for j := range o {
				for k := range o[j].Points() {
					o[j].P[k].X += dx
					o[j].P[k].Y += dy
				}
			}
			out = append(out, o...)
		}
		x += g.Advance
	}
	return out
}
