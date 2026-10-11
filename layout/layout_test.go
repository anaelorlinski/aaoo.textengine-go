package layout

import (
	"os"
	"testing"

	"github.com/anaelorlinski/aaoo.textengine-go/face"
	"github.com/go-text/typesetting/fontscan"
)

type quiet struct{}

func (quiet) Printf(string, ...any) {}

// newEngine returns an engine with one font, under the family "test".
func newEngine(t *testing.T) *Engine {
	t.Helper()
	content, err := os.ReadFile("../sfnt/resources/DejaVuSerif.ttf")
	if err != nil {
		t.Fatal(err)
	}
	engine := NewEngine(fontscan.NewFontMap(quiet{}))
	if _, err := engine.AddFont(content, face.Description{Family: "test", Style: face.Style{Weight: 400, Width: face.WidthNormal}}); err != nil {
		t.Fatal(err)
	}
	return engine
}

func testStyle() *Style {
	style := DefaultStyle()
	style.Family = []string{"test"}
	style.Size = 20
	return &style
}

func TestDefaultStyle(t *testing.T) {
	style := DefaultStyle()
	if !style.TextWrap() || !style.SpaceCollapse() {
		t.Error("the default style wraps and collapses spaces")
	}
	if style.Stretch != FStr_Normal || style.Weight != 400 {
		t.Errorf("unexpected font: stretch %d, weight %d", style.Stretch, style.Weight)
	}
}

// A text with no limit is one line, as wide as its words.
func TestSplitFirstLineUnlimited(t *testing.T) {
	engine := newEngine(t)
	text := []rune("hello world")
	line := engine.SplitFirstLine(map[HyphenDictKey]Hyphener{}, text, testStyle(), 0, false, false, true)
	if line.ResumeAt != -1 || line.Length != len(text) {
		t.Fatalf("length %d, resume at %d: want the whole text", line.Length, line.ResumeAt)
	}
	if line.Width <= 0 || line.Height <= 0 || line.Baseline <= 0 || line.Baseline >= line.Height {
		t.Fatalf("unexpected size: width %v, height %v, baseline %v", line.Width, line.Height, line.Baseline)
	}
	if got := string(line.Layout.Text()); got != "hello world" {
		t.Fatalf("unexpected text %q", got)
	}
	if len(line.Layout.Runs) == 0 || line.Layout.Runs[0].Face == nil {
		t.Fatal("the line has no run")
	}
}

// A limit between the first word and the whole text breaks at the space.
func TestSplitFirstLineBreaks(t *testing.T) {
	engine := newEngine(t)
	cache := map[HyphenDictKey]Hyphener{}
	style := testStyle()
	text := []rune("hello world")

	whole := engine.SplitFirstLine(cache, text, style, 0, false, false, true).Width
	first := engine.SplitFirstLine(cache, []rune("hello"), style, 0, false, false, true).Width
	if !(first > 0 && first < whole) {
		t.Fatalf("widths: first word %v, whole text %v", first, whole)
	}

	line := engine.SplitFirstLine(cache, text, style, (first+whole)/2, true, false, true)
	if got := string(line.Layout.Text()); got != "hello" {
		t.Fatalf("first line %q, want %q", got, "hello")
	}
	if got := string(text[line.ResumeAt:]); got != "world" {
		t.Fatalf("rest %q, want %q", got, "world")
	}
	if line.Width != first {
		t.Errorf("width %v, want the one of the first word, %v", line.Width, first)
	}

	// without wrapping, the limit is ignored
	style.WhiteSpace = WhiteSpaceNowrap
	line = engine.SplitFirstLine(cache, text, style, (first+whole)/2, true, false, true)
	if line.ResumeAt != -1 {
		t.Errorf("nowrap: resume at %d, want the whole text", line.ResumeAt)
	}
}

// A preserved newline ends the line.
func TestSplitFirstLineNewline(t *testing.T) {
	engine := newEngine(t)
	style := testStyle()
	style.WhiteSpace = WhiteSpacePre
	text := []rune("ab\ncd")
	line := engine.SplitFirstLine(map[HyphenDictKey]Hyphener{}, text, style, 0, false, false, true)
	if line.Length != 2 || line.ResumeAt != 3 {
		t.Fatalf("length %d, resume at %d: want 2 and 3", line.Length, line.ResumeAt)
	}
}

func TestCanBreakText(t *testing.T) {
	engine := newEngine(t)
	for text, want := range map[string]bool{
		"a b": true, "ab": false, "a": false, "": false,
		// broken at the space, as browsers do, against rule LB13
		"a /b": true, "word )": true, " /": true, " ]": true,
		// no space, no break
		"word)": false, "a]": false,
	} {
		if got := engine.CanBreakText([]rune(text)); got != want {
			t.Errorf("%q: %v, want %v", text, got, want)
		}
	}

	// the rules of Unicode to the letter
	engine.BreakRules = UnicodeBreaks
	for text, want := range map[string]bool{
		"a b": true, "ab": false,
		" /": false, " ]": false, "word)": false,
	} {
		if got := engine.CanBreakText([]rune(text)); got != want {
			t.Errorf("Unicode rules, %q: %v, want %v", text, got, want)
		}
	}
}

// Justifying a line does not change the next layout of the same text.
func TestLineIsOwned(t *testing.T) {
	engine := newEngine(t)
	cache := map[HyphenDictKey]Hyphener{}
	text := []rune("hello world")
	width := func(l *Line) (w int) {
		for _, run := range l.Runs {
			w += int(run.Advance)
		}
		return w
	}

	first := engine.SplitFirstLine(cache, text, testStyle(), 0, false, false, true).Layout
	natural := width(first)
	first.SetJustification(10)
	first.ApplyJustification()
	if width(first) <= natural {
		t.Fatalf("justification did not widen the line: %d, was %d", width(first), natural)
	}

	second := engine.SplitFirstLine(cache, text, testStyle(), 0, false, false, true).Layout
	if got := width(second); got != natural {
		t.Errorf("the same text laid out again is %d wide, want %d", got, natural)
	}
}

// The size of a text scales with the font size.
func TestMeasures(t *testing.T) {
	engine := newEngine(t)
	style := testStyle()
	height, baseline := engine.SpaceHeight(style)
	if !(height > 0 && baseline > 0 && baseline < height) {
		t.Fatalf("line of a space: height %v, baseline %v", height, baseline)
	}
	x, zero := engine.Heightx(style), engine.Width0(style)
	if !(x > 0 && x < style.Size && zero > 0 && zero < style.Size) {
		t.Fatalf("x height %v, width of 0 %v, at size %v", x, zero, style.Size)
	}
	// read from the outline: exactly the height of the font, scaled
	f := engine.ResolveFace('x', style.FontDescription)
	gid, _ := f.NominalGlyph('x')
	extents, _ := f.GlyphExtents(gid)
	if want := extents.YBearing * style.Size / float32(f.Upem()); x != want {
		t.Errorf("x height %v, want %v from the outline", x, want)
	}
	style.Size *= 2
	if x2 := engine.Heightx(style); x2 != 2*x {
		t.Errorf("x height %v at twice the size, was %v", x2, x)
	}
}

// A font added under a key is found again, and resets the layouts kept.
func TestAddFontFile(t *testing.T) {
	engine := newEngine(t)
	content, err := os.ReadFile("../sfnt/resources/DejaVuSerif.ttf")
	if err != nil {
		t.Fatal(err)
	}
	desc := face.Description{Family: "other", Style: face.Style{Weight: 700, Width: face.WidthNormal}}
	if err := engine.AddFontFile("key", content, desc, []Feature{{Tag: [4]byte{'k', 'e', 'r', 'n'}, Value: 0}}, Stated{}); err != nil {
		t.Fatal(err)
	}
	if _, ok := engine.Fonts().Lookup("key"); !ok {
		t.Fatal("the font is not in the collection")
	}
	if err := engine.AddFontFile("bad", []byte("not a font"), desc, nil, Stated{}); err == nil {
		t.Fatal("a broken font was accepted")
	}
}
