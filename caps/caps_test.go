package caps

import (
	"os"
	"testing"
	"unicode"

	"github.com/anaelorlinski/aaoo.textengine-go/face"
	"github.com/go-text/typesetting/di"
	"github.com/go-text/typesetting/language"
	"github.com/go-text/typesetting/shaping"
	"golang.org/x/image/math/fixed"
)

// DejaVu Serif has no small capitals.
func inputs(t *testing.T, text []rune) []shaping.Input {
	t.Helper()
	data, err := os.ReadFile("../sfnt/resources/DejaVuSerif.ttf")
	if err != nil {
		t.Fatal(err)
	}
	f, err := face.Parse(data, 0)
	if err != nil {
		t.Fatal(err)
	}
	for _, v := range []Variant{Small, AllSmall, Petite, AllPetite, Unicase} {
		if Supported(f.GoText(), v) {
			t.Fatalf("the test font has the features of variant %d", v)
		}
	}
	if !Supported(f.GoText(), None) || !Supported(f.GoText(), Titling) {
		t.Fatalf("no feature is asked of a font for None and Titling")
	}
	return []shaping.Input{{Text: text, RunStart: 0, RunEnd: len(text), Face: f.GoText(), Size: fixed.I(100)}}
}

type part struct {
	text string
	size int // in pixels
}

func parts(runs []shaping.Input) []part {
	out := make([]part, len(runs))
	for i, r := range runs {
		out[i] = part{string(r.Text[r.RunStart:r.RunEnd]), r.Size.Round()}
	}
	return out
}

func TestSynthesize(t *testing.T) {
	for _, c := range []struct {
		v    Variant
		text string
		want []part
	}{
		{Small, "Ab cD", []part{{"A", 100}, {"B", 85}, {" ", 100}, {"C", 85}, {"D", 100}}},
		{AllSmall, "Ab cD", []part{{"AB", 85}, {" ", 100}, {"CD", 85}}},
		{Petite, "Ab", []part{{"A", 100}, {"B", 70}}},
		{AllPetite, "Ab1", []part{{"AB", 70}, {"1", 100}}},
		{Unicase, "Ab", []part{{"A", 100}, {"B", 85}}},
		{Small, "éß", []part{{"É", 85}, {"ß", 100}}},                       // ß has no capital of one rune: it is left as it is
		{Small, "e\u0301a", []part{{"E\u0301A", 85}}},                      // a combining accent stays with its letter
		{Small, "Ae\u0301", []part{{"A", 100}, {"E\u0301", 85}}},           // and is small as its letter is
		{Small, "A\u0301b", []part{{"A\u0301", 100}, {"B", 85}}},           // or not
		{Small, "a\u0261b", []part{{"A", 85}, {"\u0261", 100}, {"B", 85}}}, // ɡ: the font lacks its capital
	} {
		text := []rune(c.text)
		runs, shaped := Synthesize(inputs(t, text), text, c.v)
		if len(shaped) != len(text) {
			t.Errorf("%q: the text has %d runes, it had %d", c.text, len(shaped), len(text))
		}
		got := parts(runs)
		if len(got) != len(c.want) {
			t.Errorf("variant %d of %q: %v, want %v", c.v, c.text, got, c.want)
			continue
		}
		for i := range got {
			if got[i] != c.want[i] {
				t.Errorf("variant %d of %q: %v, want %v", c.v, c.text, got, c.want)
				break
			}
		}
	}
}

// Nothing is copied when there is nothing to simulate.
func TestSynthesizeLeavesAlone(t *testing.T) {
	text := []rune("Ab")
	in := inputs(t, text)
	for _, v := range []Variant{None, Titling} {
		runs, shaped := Synthesize(in, text, v)
		if len(runs) != 1 || &runs[0] != &in[0] || &shaped[0] != &text[0] {
			t.Errorf("variant %d must return its arguments", v)
		}
	}
	// a run without a face is kept as it is
	bare := []shaping.Input{{Text: text, RunEnd: len(text), Size: fixed.I(100)}}
	if runs, _ := Synthesize(bare, text, Small); len(runs) != 1 || runs[0].Size != fixed.I(100) {
		t.Errorf("a run without a face must be left alone")
	}
}

// A size is rounded to the nearest 64th of a pixel, not cut.
func TestSynthesizeRoundsTheSize(t *testing.T) {
	text := []rune("ab")
	in := inputs(t, text)
	in[0].Size = fixed.Int26_6(11*64 + 16) // 11.25 px
	runs, _ := Synthesize(in, text, Petite)
	if want := fixed.Int26_6(504); len(runs) != 1 || runs[0].Size != want { // 7.875 px
		t.Errorf("11.25 px at 0.7 is %d/64 px, want %d", runs[0].Size, want)
	}
}

// Turkish and Azeri have a dotted capital for i.
func TestSynthesizeTurkishI(t *testing.T) {
	text := []rune("i")
	for lang, want := range map[language.Language]string{"en": "I", "tr": "İ", "az-Latn": "İ", "trv": "I"} {
		in := inputs(t, text)
		in[0].Language = lang
		if _, shaped := Synthesize(in, text, Small); string(shaped) != want {
			t.Errorf("%s: i gives %q, want %q", lang, string(shaped), want)
		}
	}
}

// EB Garamond has small capitals for the Latin and Greek letters and none
// for the Cyrillic ones: the first are left to the font, the others are
// simulated, in the same run.
func TestSynthesizeLetterByLetter(t *testing.T) {
	data, err := os.ReadFile("../sfnt/resources/EBGaramond12-Regular.otf")
	if err != nil {
		t.Fatal(err)
	}
	f, err := face.Parse(data, 0)
	if err != nil {
		t.Fatal(err)
	}
	if !Supported(f.GoText(), Small) {
		t.Fatal("the test font has no small capitals")
	}
	input := func(s string) ([]shaping.Input, []rune) {
		text := []rune(s)
		return []shaping.Input{{Text: text, RunEnd: len(text), Face: f.GoText(), Size: fixed.I(100)}}, text
	}

	in, text := input("abc")
	if runs, shaped := Synthesize(in, text, Small); len(runs) != 1 || &runs[0] != &in[0] || string(shaped) != "abc" {
		t.Errorf("Latin letters the font has small capitals for are changed: %v %q", parts(runs), string(shaped))
	}

	in, text = input("ab \u0430\u0431") // Latin ab, Cyrillic аб
	runs, _ := Synthesize(in, text, Small)
	want := []part{{"ab ", 100}, {"\u0410\u0411", 85}}
	if got := parts(runs); len(got) != 2 || got[0] != want[0] || got[1] != want[1] {
		t.Errorf("Latin then Cyrillic: %v, want %v", got, want)
	}

	// with the capitals too: those the font has the feature for are its own
	in, text = input("AB")
	if runs, _ := Synthesize(in, text, AllSmall); len(runs) != 1 || runs[0].Size != fixed.I(100) {
		t.Errorf("capitals the font has small capitals for are made smaller: %v", parts(runs))
	}
}

// The runs Synthesize returns shape to the glyphs of the capitals, at the
// reduced size, and their clusters point back into the original text: what a
// renderer needs to draw them and to say what they stand for.
func TestSynthesizeShapes(t *testing.T) {
	data, err := os.ReadFile("../sfnt/resources/DejaVuSerif.ttf")
	if err != nil {
		t.Fatal(err)
	}
	f, err := face.Parse(data, 0)
	if err != nil {
		t.Fatal(err)
	}
	ft := f.GoText()
	text := []rune("Small Caps, éa")
	const size = 100
	in := []shaping.Input{{Text: text, RunEnd: len(text), Face: ft, Size: fixed.I(size), Direction: di.DirectionLTR, Script: language.Latin, Language: language.NewLanguage("en")}}
	runs, shaped := Synthesize(in, text, Small)
	if len(shaped) != len(text) {
		t.Fatalf("shaped text has %d runes, want %d", len(shaped), len(text))
	}
	var shaper shaping.HarfbuzzShaper
	covered := 0
	for _, run := range runs {
		if string(run.Text) != string(shaped) {
			t.Errorf("run text %q is not the shaped text %q", string(run.Text), string(shaped))
		}
		out := shaper.Shape(run)
		for _, g := range out.Glyphs {
			if g.ClusterIndex < run.RunStart || g.ClusterIndex >= run.RunEnd {
				t.Errorf("cluster %d outside the run %d..%d", g.ClusterIndex, run.RunStart, run.RunEnd)
				continue
			}
			covered++
			orig := text[g.ClusterIndex]
			want := orig
			wantSize := fixed.I(size)
			if unicode.IsLower(orig) {
				want = unicode.ToUpper(orig)
				wantSize = fixed.Int26_6(float64(fixed.I(size)) * SmallScale)
			}
			gid, ok := ft.NominalGlyph(want)
			if !ok {
				t.Fatalf("no glyph for %q", want)
			}
			if g.GlyphID != gid {
				t.Errorf("%q (cluster %d): glyph %d, want glyph %d of %q", orig, g.ClusterIndex, g.GlyphID, gid, want)
			}
			if run.Size != wantSize {
				t.Errorf("%q: run size %v, want %v", orig, run.Size, wantSize)
			}
			// the advance is the one of the capital at the run's size
			wantAdvance := float64(ft.HorizontalAdvance(gid)) * float64(run.Size) / float64(ft.Upem())
			if d := float64(g.XAdvance) - wantAdvance; d < -1 || 1 < d {
				t.Errorf("%q: advance %v, want %v", orig, g.XAdvance, wantAdvance)
			}
		}
	}
	if covered != len(text) {
		t.Errorf("%d glyphs for %d characters", covered, len(text))
	}
}
