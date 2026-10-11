package glyph

import (
	"fmt"
	"math"
	"os"
	"sort"
	"strings"
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

// The go-text reader and the sfnt reader must agree on which glyph a
// character is and on how far it advances: the first draws what the second
// embeds.
func TestGlyphsAndAdvancesAgreeWithSFNT(t *testing.T) {
	for _, name := range []string{"DejaVuSerif.ttf", "EBGaramond12-Regular.otf"} {
		f, s := load(t, name)
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
		}
	}
}

// edge is one step of a closed contour: its kind, control points and end.
type edge struct {
	op  Op
	pts []Point
}

func (e edge) String() string {
	var b strings.Builder
	fmt.Fprintf(&b, "%d", e.op)
	for _, p := range e.pts {
		fmt.Fprintf(&b, " %.3f,%.3f", p.X, p.Y)
	}
	return b.String()
}

// near allows the rounding of 16.16 fixed-point coordinates to float32.
func nearPoint(a, b Point) bool {
	return math.Abs(a.X-b.X) < 2e-3 && math.Abs(a.Y-b.Y) < 2e-3
}

// contours splits an outline into closed contours in a canonical form: a
// zero-length line and a closing line back to the start are dropped, and
// each contour is rotated to start at its smallest edge, so that two readers
// that start a contour at different points, or close it differently, still
// compare equal.
func contours(o Outline) [][]edge {
	var out [][]edge
	var cur []edge
	var start Point
	flush := func() {
		if len(cur) == 0 {
			return
		}
		last := cur[len(cur)-1]
		if last.op == LineTo && len(cur) > 1 && nearPoint(last.pts[0], start) {
			cur = cur[:len(cur)-1]
		}
		best := 0
		for i := range cur {
			if cur[i].String() < cur[best].String() {
				best = i
			}
		}
		out = append(out, append(append([]edge{}, cur[best:]...), cur[:best]...))
		cur = nil
	}
	for _, s := range o {
		switch s.Op {
		case MoveTo:
			flush()
			start = s.P[0]
		default:
			if s.Op == LineTo {
				prev := start
				if len(cur) > 0 {
					p := cur[len(cur)-1].pts
					prev = p[len(p)-1]
				}
				if nearPoint(s.P[0], prev) {
					continue
				}
			}
			cur = append(cur, edge{op: s.Op, pts: append([]Point(nil), s.Points()...)})
		}
	}
	flush()
	sort.Slice(out, func(i, j int) bool { return out[i][0].String() < out[j][0].String() })
	return out
}

// sameContours compares two canonical outlines point by point.
func sameContours(a, b [][]edge) string {
	if len(a) != len(b) {
		return fmt.Sprintf("%d contours from go-text, %d from sfnt", len(a), len(b))
	}
	for i := range a {
		if len(a[i]) != len(b[i]) {
			return fmt.Sprintf("contour %d: %d edges from go-text, %d from sfnt", i, len(a[i]), len(b[i]))
		}
		for j := range a[i] {
			ea, eb := a[i][j], b[i][j]
			if ea.op != eb.op || len(ea.pts) != len(eb.pts) {
				return fmt.Sprintf("contour %d edge %d: %s from go-text, %s from sfnt", i, j, ea, eb)
			}
			for k := range ea.pts {
				if !nearPoint(ea.pts[k], eb.pts[k]) {
					return fmt.Sprintf("contour %d edge %d: %s from go-text, %s from sfnt", i, j, ea, eb)
				}
			}
		}
	}
	return ""
}

// exactDiff reports the first difference in the raw segment lists.
func exactDiff(a, b Outline) string {
	if len(a) != len(b) {
		return fmt.Sprintf("%d segments from go-text, %d from sfnt", len(a), len(b))
	}
	for i := range a {
		if a[i].Op != b[i].Op {
			return fmt.Sprintf("segment %d: op %d from go-text, %d from sfnt", i, a[i].Op, b[i].Op)
		}
		pa, pb := a[i].Points(), b[i].Points()
		for j := range pa {
			if !nearPoint(pa[j], pb[j]) {
				return fmt.Sprintf("segment %d point %d: %v from go-text, %v from sfnt", i, j, pa[j], pb[j])
			}
		}
	}
	return ""
}

// The two readers of the module must draw the same outline for every glyph:
// the same segment kinds through the same points, contour by contour. Bounding
// boxes alone would let a wrong segment kind or a dropped inner contour pass.
//
// go-text, like FreeType and HarfBuzz, moves a TrueType glyph so that its
// left side bearing is the one of hmtx; the sfnt package gives the raw
// coordinates. The sfnt outline is moved by the same amount before the
// comparison: DejaVu Serif has 33 glyphs whose hmtx and glyf disagree by one
// unit.
func TestOutlineAgreesWithSFNT(t *testing.T) {
	for _, name := range []string{"DejaVuSerif.ttf", "EBGaramond12-Regular.otf"} {
		f, s := load(t, name)
		upem := float64(f.UnitsPerEm())
		n := int(s.NumGlyphs())
		exact, differ, empty, shifted := 0, 0, 0, 0
		for gid := 0; gid < n; gid++ {
			got, ok := FromFace(f, face.GID(gid), upem, Style{})
			var rec recorder
			if err := s.GlyphPath(&rec, uint16(gid), 0, 0, 0, 1, sfnt.NoHinting); err != nil {
				t.Errorf("%s glyph %d: sfnt: %v", name, gid, err)
				continue
			}
			if !ok {
				if len(rec.out) != 0 {
					t.Errorf("%s glyph %d: go-text gives no outline, sfnt gives %d segments", name, gid, len(rec.out))
				}
				empty++
				continue
			}
			if s.IsTrueType {
				xMin, _, _, _ := s.GlyphBounds(uint16(gid))
				if dx := float64(s.Hmtx.LeftSideBearing(uint16(gid)) - xMin); dx != 0 {
					shifted++
					rec.out = rec.out.Apply(Transform{A: 1, D: 1, E: dx})
				}
			}
			if d := exactDiff(got, rec.out); d != "" {
				exact++
				if exact <= 3 {
					t.Logf("%s glyph %d: raw segments differ: %s", name, gid, d)
				}
			}
			if d := sameContours(contours(got), contours(rec.out)); d != "" {
				differ++
				if differ <= 5 {
					t.Errorf("%s glyph %d: outlines differ: %s", name, gid, d)
				}
			}
		}
		t.Logf("%s: %d glyphs, %d without outline, %d moved by the side bearing, %d differ in raw segments, %d differ once canonical", name, n, empty, shifted, exact, differ)
		if differ > 0 {
			t.Errorf("%s: %d of %d glyphs differ between go-text and sfnt", name, differ, n)
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
