package layout

import (
	"os"
	"slices"
	"testing"

	"github.com/anaelorlinski/aaoo.textengine-go/face"
	"github.com/go-text/typesetting/fontscan"
)

func readFont(t *testing.T, name string) []byte {
	t.Helper()
	content, err := os.ReadFile("../sfnt/resources/" + name)
	if err != nil {
		t.Fatal(err)
	}
	return content
}

// familyOf returns the family, as the font file names it, of the font a
// text of the style is set in.
func familyOf(t *testing.T, engine *Engine, style *Style) string {
	t.Helper()
	line := engine.SplitFirstLine(map[HyphenDictKey]Hyphener{}, []rune("a"), style, 0, false, false, true)
	if len(line.Layout.Runs) == 0 || line.Layout.Runs[0].Face == nil {
		t.Fatal("the text has no font")
	}
	return line.Layout.Runs[0].Face.Describe().Family
}

func styleOfFamily(families ...string) *Style {
	style := DefaultStyle()
	style.Family = families
	return &style
}

// A family the engine was not given is set in the fallback family.
func TestMissingToFallback(t *testing.T) {
	engine := NewEngine(fontscan.NewFontMap(quiet{}))
	regular := face.Style{Weight: 400, Width: face.WidthNormal}
	if err := engine.LoadFont("zebra", readFont(t, "EBGaramond12-Regular.otf"), regular); err != nil {
		t.Fatal(err)
	}
	if err := engine.LoadFont("alpha", readFont(t, "DejaVuSerif.ttf"), regular); err != nil {
		t.Fatal(err)
	}
	engine.Missing = MissingToFallback

	dejavu := familyOf(t, engine, styleOfFamily("alpha"))
	garamond := familyOf(t, engine, styleOfFamily("zebra"))
	if dejavu == garamond {
		t.Fatalf("the two fonts are the same family %q", dejavu)
	}

	engine.Fallback = "zebra"
	if got := familyOf(t, engine, styleOfFamily("unknown")); got != garamond {
		t.Errorf("unknown family set in %q, want the fallback %q", got, garamond)
	}
	// a known family further in the list is used
	if got := familyOf(t, engine, styleOfFamily("unknown", "alpha")); got != dejavu {
		t.Errorf("second family: set in %q, want %q", got, dejavu)
	}

	// without a fallback, the style is left as it is
	engine.Fallback = ""
	style := styleOfFamily("unknown")
	if got := engine.resolve(style); got != style {
		t.Errorf("no fallback: the style was changed to %v", got.Family)
	}
}

// By default the style is left to the font map.
func TestMissingToFontMap(t *testing.T) {
	engine := newEngine(t)
	style := styleOfFamily("unknown")
	if got := engine.resolve(style); got != style {
		t.Errorf("the style was changed: %v", got.Family)
	}
}

// A font of AddFontFile comes before a font of LoadFont of the same family.
func TestAddFontFileOverrides(t *testing.T) {
	engine := NewEngine(fontscan.NewFontMap(quiet{}))
	regular := face.Style{Weight: 400, Width: face.WidthNormal}
	if err := engine.LoadFont("family", readFont(t, "DejaVuSerif.ttf"), regular); err != nil {
		t.Fatal(err)
	}
	style := styleOfFamily("Family")
	loaded := familyOf(t, engine, style)

	desc := face.Description{Family: "Family", Style: regular}
	if err := engine.AddFontFile("rule", readFont(t, "EBGaramond12-Regular.otf"), desc, nil, Stated{}); err != nil {
		t.Fatal(err)
	}
	added := familyOf(t, engine, style)
	if added == loaded {
		t.Fatalf("the text is still set in %q", loaded)
	}

	// resolving twice changes nothing more
	once := engine.resolve(style)
	twice := engine.resolve(once)
	if !slices.Equal(once.Family, twice.Family) {
		t.Errorf("resolved twice: %v, then %v", once.Family, twice.Family)
	}
	if len(once.Family) != 2 || once.Family[1] != "Family" {
		t.Errorf("resolved families %v: want the fonts added, then the family", once.Family)
	}
}

func TestStyleOf(t *testing.T) {
	engine := NewEngine(fontscan.NewFontMap(quiet{}))
	bold := face.Style{Weight: 700, Italic: true, Width: face.WidthNormal}
	content := readFont(t, "DejaVuSerif.ttf") // a regular, upright font

	// loaded: what the caller says
	if err := engine.LoadFont("loaded", content, bold); err != nil {
		t.Fatal(err)
	}
	f := engine.ResolveFace('a', styleOfFamily("loaded").FontDescription)
	if got := engine.StyleOf(f); got.Weight != 700 || !got.Italic {
		t.Errorf("loaded font: %+v, want what it was loaded as", got)
	}

	// added: what its source states, and the file for the rest
	for _, test := range []struct {
		family     string
		stated     Stated
		wantWeight int
		wantItalic bool
	}{
		{"nothing", Stated{}, 400, false},
		{"weight", Stated{Weight: true}, 700, false},
		{"italic", Stated{Italic: true}, 400, true},
		{"both", Stated{Weight: true, Italic: true}, 700, true},
	} {
		desc := face.Description{Family: test.family, Style: bold}
		// the content is copied: one font file per key
		if err := engine.AddFontFile("key-"+test.family, slices.Clone(content), desc, nil, test.stated); err != nil {
			t.Fatal(err)
		}
		fd := styleOfFamily(test.family).FontDescription
		fd.Weight, fd.Style = 700, FSty_Italic
		f := engine.ResolveFace('a', fd)
		if got := engine.StyleOf(f); got.Weight != test.wantWeight || got.Italic != test.wantItalic {
			t.Errorf("%s stated: weight %d italic %v, want %d %v", test.family, got.Weight, got.Italic, test.wantWeight, test.wantItalic)
		}
	}
}
