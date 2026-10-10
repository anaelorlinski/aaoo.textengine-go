package caps

import (
	"os"
	"testing"

	"github.com/anaelorlinski/aaoo.textengine-go/face"
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
		{Small, "éß", []part{{"Éß", 85}}}, // ß has no capital of one rune: it stays, at the small size
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
