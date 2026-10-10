// Package glyph turns positioned glyphs into geometry: outlines at a size,
// the synthetic styles a font lacks, and the lines of text decorations.
//
// Coordinates have the Y axis pointing up and the origin on the baseline, as
// in a font. The geometry is plain data, so that a rasterizer, a PDF writer
// and an SVG writer can each convert it to their own path type.
package glyph

import (
	"math"

	"github.com/anaelorlinski/aaoo.textengine-go/face"
	ot "github.com/go-text/typesetting/font/opentype"
)

// Op is the kind of a path segment.
type Op uint8

const (
	MoveTo Op = iota // starts a contour at P[0]; the previous contour is closed
	LineTo           // a line to P[0]
	QuadTo           // a quadratic curve, control point P[0], to P[1]
	CubeTo           // a cubic curve, control points P[0] and P[1], to P[2]
)

// Point is a position in the plane.
type Point struct{ X, Y float64 }

// Segment is one step of an outline.
type Segment struct {
	Op Op
	P  [3]Point // 1, 1, 2 or 3 points are used, depending on Op
}

// Points returns the points the segment uses.
func (s *Segment) Points() []Point {
	switch s.Op {
	case QuadTo:
		return s.P[:2]
	case CubeTo:
		return s.P[:3]
	}
	return s.P[:1]
}

// Outline is the shape of a glyph: closed contours, filled with the non-zero
// rule. Each contour starts with a MoveTo and is closed implicitly.
type Outline []Segment

// Transform is the affine map x' = A·x + C·y + E, y' = B·x + D·y + F, the
// convention of PDF and of aomatrix.
type Transform struct{ A, B, C, D, E, F float64 }

// Apply returns a copy of the outline with every point mapped by t.
func (o Outline) Apply(t Transform) Outline {
	out := make(Outline, len(o))
	for i, s := range o {
		out[i].Op = s.Op
		for j, p := range s.Points() {
			out[i].P[j] = Point{t.A*p.X + t.C*p.Y + t.E, t.B*p.X + t.D*p.Y + t.F}
		}
	}
	return out
}

// Bounds returns the box of the points of the outline, control points
// included, and false for an empty outline.
func (o Outline) Bounds() (minX, minY, maxX, maxY float64, ok bool) {
	minX, minY = math.Inf(1), math.Inf(1)
	maxX, maxY = math.Inf(-1), math.Inf(-1)
	for i := range o {
		for _, p := range o[i].Points() {
			minX, maxX = math.Min(minX, p.X), math.Max(maxX, p.X)
			minY, maxY = math.Min(minY, p.Y), math.Max(maxY, p.Y)
			ok = true
		}
	}
	if !ok {
		return 0, 0, 0, 0, false
	}
	return minX, minY, maxX, maxY, true
}

// Style is what is asked of the outline of a glyph beyond its size.
type Style struct {
	// Slant shears the glyph to the right by this fraction of its height,
	// to stand in for an italic the family lacks. FauxItalicSlant is the
	// usual value.
	Slant float64
	// ScaleX stretches the glyph horizontally, to stand in for a condensed or
	// expanded width the family lacks. 0 means 1.
	ScaleX float64
}

// FauxItalicSlant is the shear of a synthetic italic: 0.3, about 17 degrees,
// the value tdewolff/canvas and most renderers use.
const FauxItalicSlant = 0.3

// FromFace returns the outline of a glyph at the given size, for the current
// variation settings of the face. size is the length of one em in the
// caller's unit. A glyph without an outline (a space, a bitmap or SVG glyph)
// gives an empty outline and false.
func FromFace(f *face.Face, gid face.GID, size float64, style Style) (Outline, bool) {
	src, ok := f.GoText().GlyphDataOutline(gid)
	if !ok || len(src.Segments) == 0 {
		return nil, false
	}
	scale := size / float64(f.UnitsPerEm())
	sx := style.ScaleX
	if sx == 0 {
		sx = 1
	}
	t := Transform{A: scale * sx, C: scale * sx * style.Slant, D: scale}
	out := make(Outline, len(src.Segments))
	for i, s := range src.Segments {
		switch s.Op {
		case ot.SegmentOpMoveTo:
			out[i].Op = MoveTo
		case ot.SegmentOpLineTo:
			out[i].Op = LineTo
		case ot.SegmentOpQuadTo:
			out[i].Op = QuadTo
		case ot.SegmentOpCubeTo:
			out[i].Op = CubeTo
		}
		for j, p := range s.ArgsSlice() {
			x, y := float64(p.X), float64(p.Y)
			out[i].P[j] = Point{t.A*x + t.C*y, t.D * y}
		}
	}
	return out, true
}

// FauxBoldOffset returns the distance by which the outline of a glyph is
// expanded (or shrunk, when negative) to stand in for a weight the family
// lacks, for a font of the given size. have is the weight of the font and
// want the one asked for, both on the scale of CSS, 1 to 1000.
//
// The steps are those of tdewolff/canvas: bold over regular is 2% of the
// size. They are not even: the light weights are closer to regular than the
// heavy ones, as they are in font families.
//
// Expanding an outline needs a path offset, which is the caller's: a PDF
// writer strokes the text instead (text render mode 2 with a line width of
// twice the offset).
func FauxBoldOffset(have, want int, size float64) float64 {
	return (fauxWeight(want) - fauxWeight(have)) * size
}

// fauxWeight is the expansion of the outlines that tells a weight from the
// regular one, as a fraction of the font size.
func fauxWeight(weight int) float64 {
	switch {
	case weight <= 150:
		return -0.02
	case weight <= 250:
		return -0.01
	case weight <= 350:
		return -0.005
	case weight <= 450:
		return 0
	case weight <= 550:
		return 0.005
	case weight <= 650:
		return 0.01
	case weight <= 750:
		return 0.02
	case weight <= 850:
		return 0.03
	}
	return 0.04
}

// SnapY returns the distance to add to the vertical position y so that it
// lands on a whole device pixel, where device = scale·y + translate. It
// returns 0 for a zero scale. Callers skip snapping under a rotation or a
// shear, where a horizontal line of text is not a row of pixels.
func SnapY(y, scale, translate float64) float64 {
	if scale == 0 {
		return 0
	}
	pos := scale*y + translate
	return (math.Round(pos) - pos) / scale
}
