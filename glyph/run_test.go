package glyph

import (
	"os"
	"testing"

	"github.com/anaelorlinski/aaoo.textengine-go/face"
)

func TestRun(t *testing.T) {
	data, err := os.ReadFile("../sfnt/resources/DejaVuSerif.ttf")
	if err != nil {
		t.Fatal(err)
	}
	f, err := face.Parse(data, 0)
	if err != nil {
		t.Fatal(err)
	}
	a, _ := f.GlyphIndex('A')
	space, _ := f.GlyphIndex(' ')
	one, _ := FromFace(f, a, 10, Style{})
	x0, y0, x1, y1, _ := one.Bounds()

	run := Run{Face: f, Size: 10, Glyphs: []Positioned{
		{ID: a, Advance: 8},
		{ID: space, Advance: 3},
		{ID: a, Advance: 8, XOffset: 1, YOffset: 2},
	}}
	if run.Advance() != 19 {
		t.Errorf("advance %v, want 19", run.Advance())
	}
	out := run.Outline()
	if len(out) != 2*len(one) {
		t.Fatalf("%d segments, want twice the %d of one A: a space has no outline", len(out), len(one))
	}
	// the second A is after the first and the space, moved by its offsets
	bx0, by0, bx1, by1, _ := out[len(one):].Bounds()
	const eps = 1e-9
	near := func(a, b float64) bool { return a-b < eps && b-a < eps }
	if !near(bx0, x0+12) || !near(bx1, x1+12) || !near(by0, y0+2) || !near(by1, y1+2) {
		t.Errorf("second A at %v %v %v %v, the first at %v %v %v %v", bx0, by0, bx1, by1, x0, y0, x1, y1)
	}

	// A slant shears the run about its baseline: the first glyph is the
	// slanted glyph where it was, and a glyph raised by 2 moves to the right
	// by the slant of that height.
	run.Slant = FauxItalicSlant
	slanted := run.Outline()
	alone, _ := FromFace(f, a, 10, Style{Slant: FauxItalicSlant})
	ax0, _, ax1, _, _ := alone.Bounds()
	sx0, _, sx1, _, _ := slanted[:len(one)].Bounds()
	if !near(sx0, ax0) || !near(sx1, ax1) || sx1 <= x1 {
		t.Errorf("slanted A spans %v to %v, want %v to %v", sx0, sx1, ax0, ax1)
	}
	if rx0, _, _, _, _ := slanted[len(one):].Bounds(); !near(rx0, sx0+12+FauxItalicSlant*2) {
		t.Errorf("the raised A starts at %v, want %v", rx0, sx0+12+FauxItalicSlant*2)
	}
}
