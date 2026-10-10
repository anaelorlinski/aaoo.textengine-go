package glyph

import (
	"math"
	"os"
	"testing"

	"github.com/anaelorlinski/aaoo.textengine-go/face"
	"github.com/anaelorlinski/aaoo.textengine-go/sfnt"
)

// recorder collects the outline the sfnt package produces, to compare the
// two readers of this module on the same glyphs.
type recorder struct{ out Outline }

func (r *recorder) MoveTo(x, y float64) {
	r.out = append(r.out, Segment{Op: MoveTo, P: [3]Point{{x, y}}})
}

func (r *recorder) LineTo(x, y float64) {
	r.out = append(r.out, Segment{Op: LineTo, P: [3]Point{{x, y}}})
}

func (r *recorder) QuadTo(cx, cy, x, y float64) {
	r.out = append(r.out, Segment{Op: QuadTo, P: [3]Point{{cx, cy}, {x, y}}})
}

func (r *recorder) CubeTo(c1x, c1y, c2x, c2y, x, y float64) {
	r.out = append(r.out, Segment{Op: CubeTo, P: [3]Point{{c1x, c1y}, {c2x, c2y}, {x, y}}})
}
func (r *recorder) Close() {}

func load(t *testing.T, name string) (*face.Face, *sfnt.SFNT) {
	t.Helper()
	b, err := os.ReadFile("../sfnt/resources/" + name)
	if err != nil {
		t.Fatal(err)
	}
	f, err := face.Parse(b, 0)
	if err != nil {
		t.Fatal(err)
	}
	s, err := sfnt.ParseSFNT(b, 0)
	if err != nil {
		t.Fatal(err)
	}
	return f, s
}

// The go-text reader and the sfnt reader must agree on where a glyph is: the
// first draws what the second embeds.
func TestOutlineBoundsAgreeWithSFNT(t *testing.T) {
	for _, name := range []string{"DejaVuSerif.ttf", "EBGaramond12-Regular.otf"} {
		f, s := load(t, name)
		upem := float64(f.UnitsPerEm())
		for _, r := range "AgQ&é@fi§" {
			gid, ok := f.GlyphIndex(r)
			if !ok {
				t.Fatalf("%s: no glyph for %q", name, r)
			}
			if got := s.GlyphIndex(r); got != uint16(gid) {
				t.Errorf("%s %q: glyph %d from go-text, %d from sfnt", name, r, gid, got)
			}
			if a, b := float64(f.Advance(gid)), float64(s.GlyphAdvance(uint16(gid))); a != b {
				t.Errorf("%s %q: advance %v from go-text, %v from sfnt", name, r, a, b)
			}
			got, ok := FromFace(f, gid, upem, Style{})
			if !ok {
				t.Fatalf("%s %q: no outline", name, r)
			}
			var rec recorder
			if err := s.GlyphPath(&rec, uint16(gid), 0, 0, 0, 1, sfnt.NoHinting); err != nil {
				t.Fatal(err)
			}
			gx0, gy0, gx1, gy1, _ := got.Bounds()
			wx0, wy0, wx1, wy1, _ := rec.out.Bounds()
			for i, d := range []float64{gx0 - wx0, gy0 - wy0, gx1 - wx1, gy1 - wy1} {
				if math.Abs(d) > 1 { // one font unit: the readers round differently
					t.Errorf("%s %q: bound %d differs by %v units", name, r, i, d)
				}
			}
		}
	}
}

func TestSizeSlantAndStretch(t *testing.T) {
	f, _ := load(t, "DejaVuSerif.ttf")
	gid, _ := f.GlyphIndex('H')
	base, _ := FromFace(f, gid, 10, Style{})
	x0, y0, x1, y1, _ := base.Bounds()
	if y0 != 0 || y1 < 6 || y1 > 8 {
		t.Errorf("H at size 10 spans y %v..%v, want 0..about 7.3", y0, y1)
	}

	wide, _ := FromFace(f, gid, 10, Style{ScaleX: 2})
	wx0, _, wx1, wy1, _ := wide.Bounds()
	if math.Abs((wx1-wx0)-2*(x1-x0)) > 1e-9 || wy1 != y1 {
		t.Errorf("ScaleX 2: width %v, want %v; height changed: %v", wx1-wx0, 2*(x1-x0), wy1 != y1)
	}

	slanted, _ := FromFace(f, gid, 10, Style{Slant: FauxItalicSlant})
	sx0, sy0, sx1, sy1, _ := slanted.Bounds()
	if sy0 != y0 || sy1 != y1 {
		t.Errorf("a slant must not move points vertically")
	}
	// The foot stays where it is, the top moves right by slant × height.
	if math.Abs(sx0-x0) > 1e-9 || math.Abs((sx1-x1)-FauxItalicSlant*y1) > 1e-9 {
		t.Errorf("slant: left %v (was %v), right moved by %v, want %v", sx0, x0, sx1-x1, FauxItalicSlant*y1)
	}

	if _, ok := FromFace(f, mustGlyph(t, f, ' '), 10, Style{}); ok {
		t.Errorf("a space has no outline")
	}
}

func mustGlyph(t *testing.T, f *face.Face, r rune) face.GID {
	t.Helper()
	gid, ok := f.GlyphIndex(r)
	if !ok {
		t.Fatalf("no glyph for %q", r)
	}
	return gid
}

func TestFauxBoldOffset(t *testing.T) {
	for _, c := range []struct {
		have, want int
		offset     float64 // for a size of 10
	}{
		{400, 700, 0.2}, // bold over regular: 2% of the size
		{400, 400, 0},
		{400, 600, 0.1},
		{400, 900, 0.4},
		{700, 900, 0.2},
		{300, 400, 0.05},
		{700, 400, -0.2}, // lighter than the font
		{400, 100, -0.2},
		{420, 680, 0.2}, // a weight between two names goes to the closest
	} {
		if got := FauxBoldOffset(c.have, c.want, 10); math.Abs(got-c.offset) > 1e-12 {
			t.Errorf("%d drawn as %d: offset %v, want %v", c.have, c.want, got, c.offset)
		}
	}
}

func TestSnapY(t *testing.T) {
	for _, tc := range []struct{ y, scale, translate float64 }{
		{3.3, 1, 0}, {3.3, -1, 100.2}, {7.77, 3.7795, 0.4}, {-2.5, 2, 0},
	} {
		d := SnapY(tc.y, tc.scale, tc.translate)
		pos := tc.scale*(tc.y+d) + tc.translate
		if math.Abs(pos-math.Round(pos)) > 1e-9 {
			t.Errorf("%+v: lands on %v, not a whole pixel", tc, pos)
		}
		if math.Abs(d*tc.scale) > 0.5+1e-9 {
			t.Errorf("%+v: moved by %v device pixels, more than half", tc, d*tc.scale)
		}
	}
	if SnapY(1, 0, 0) != 0 {
		t.Errorf("zero scale must not move")
	}
}

func TestDecorations(t *testing.T) {
	f, _ := load(t, "DejaVuSerif.ttf")
	m := f.Metrics(face.ShaperRule)
	scale := 100 / float64(f.UnitsPerEm())
	d := DecorationsOf(f, 100, face.ShaperRule)

	// the bars are where the font puts their top edge
	if d.Underline.Top != float64(m.UnderlinePosition)*scale || d.Underline.Thickness != float64(m.UnderlineThickness)*scale {
		t.Errorf("underline %+v, the font has %v and %v units", d.Underline, m.UnderlinePosition, m.UnderlineThickness)
	}
	if d.Underline.Top >= 0 || d.Underline.Center() >= d.Underline.Top {
		t.Errorf("underline %+v: want under the baseline, growing downwards", d.Underline)
	}
	if d.Overline.Top != float64(m.Ascent)*scale || d.Overline.Thickness != d.Underline.Thickness {
		t.Errorf("overline %+v: want on the ascent, as thick as the underline", d.Overline)
	}
	if d.Strikethrough.Center() <= 0 || d.Strikethrough.Top >= d.Overline.Top {
		t.Errorf("strikethrough %+v: want between the baseline and the overline %+v", d.Strikethrough, d.Overline)
	}
	if d.XHeight != float64(m.XHeight)*scale {
		t.Errorf("x-height %v", d.XHeight)
	}

	half := DecorationsOf(f, 50, face.ShaperRule)
	if math.Abs(half.Underline.Top*2-d.Underline.Top) > 1e-9 || math.Abs(half.Underline.Thickness*2-d.Underline.Thickness) > 1e-9 {
		t.Errorf("decorations must scale with the size")
	}
	// the overline follows the ascent the rule selects
	if other := DecorationsOf(f, 100, face.MicrosoftRule); other.Overline.Top != float64(f.Metrics(face.MicrosoftRule).Ascent)*scale {
		t.Errorf("overline %+v does not follow the rule", other.Overline)
	}
}
