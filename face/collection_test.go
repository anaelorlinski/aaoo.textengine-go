package face

import (
	"testing"

	"github.com/go-text/typesetting/font"
	"github.com/go-text/typesetting/fontscan"
)

func TestStyle(t *testing.T) {
	if got := (Style{}).Aspect(); got.Weight != 400 || got.Style != font.StyleNormal || got.Stretch != font.StretchNormal {
		t.Errorf("the zero style is %+v, want regular, upright, normal width", got)
	}
	bold := Style{Weight: WeightBold, Italic: true, Width: WidthCondensed}
	if got := StyleOf(bold.Aspect()); got != bold {
		t.Errorf("round trip through go-text: %+v, want %+v", got, bold)
	}
	// a ratio between two classes goes to the closest
	if got := StyleOf(font.Aspect{Weight: 400, Stretch: 0.8}).Width; got != WidthCondensed {
		t.Errorf("width of 0.8 is %d, want condensed", got)
	}
}

func TestDescribe(t *testing.T) {
	f, err := Parse(read(t, "DejaVuSerif.ttf"), 0)
	if err != nil {
		t.Fatal(err)
	}
	if d := f.Describe(); d.Family != "DejaVu Serif" || d.Weight != WeightRegular || d.Italic || d.Width != WidthNormal {
		t.Errorf("description %+v", d)
	}
}

type quiet struct{}

func (quiet) Printf(string, ...any) {}

func TestCollection(t *testing.T) {
	fm := fontscan.NewFontMap(quiet{})
	c := NewCollection(fm)
	data := read(t, "DejaVuSerif.woff2")

	// the font is what it is added as, not what it says
	f, err := c.Add("key", data, Description{Family: "mine", Style: Style{Weight: WeightBold}})
	if err != nil {
		t.Fatal(err)
	}
	again, err := c.Add("key", nil, Description{Family: "alias"})
	if err != nil || again != f {
		t.Fatalf("the same key must give the same face, parsed once: %v", err)
	}
	if got, ok := c.Lookup("key"); !ok || got != f {
		t.Errorf("Lookup does not find the font")
	}
	if style, ok := c.Registered(f.GoText().Font); !ok || style.Weight != WeightRegular {
		t.Errorf("registered style %+v, want the last one added", style)
	}

	fm.SetQuery(fontscan.Query{Families: []string{"mine"}, Aspect: Style{Weight: WeightBold}.Aspect()})
	resolved := fm.ResolveFace('A')
	if resolved == nil || c.Face(resolved) != f {
		t.Fatalf("the font map does not resolve the family to the font added")
	}
	// the WOFF2 file is kept decoded, ready to be embedded
	if err := f.Embeddable(); err != nil {
		t.Errorf("Embeddable: %v", err)
	}
	if got := c.Data(fm.FontLocation(resolved.Font)); len(got) == 0 || &got[0] != &f.Data()[0] {
		t.Errorf("Data does not return the bytes of the font")
	}

	if _, err := c.Add("empty", nil, Description{Family: "x"}); err == nil {
		t.Errorf("empty content must fail")
	}
	if _, err := c.Add("bad", []byte("not a font"), Description{Family: "x"}); err == nil {
		t.Errorf("garbage must fail")
	}
}

// The same bytes under two keys are two fonts, each with its style, and one
// file to embed.
func TestCollectionSharesContent(t *testing.T) {
	c := NewCollection(fontscan.NewFontMap(quiet{}))
	data := read(t, "DejaVuSerif.ttf")
	regular, err := c.Add("rule 1", data, Description{Family: "mine", Style: Style{Weight: WeightRegular}})
	if err != nil {
		t.Fatal(err)
	}
	// another slice holding the same bytes, as another rule naming the file
	bold, err := c.Add("rule 2", append([]byte(nil), data...), Description{Family: "mine", Style: Style{Weight: WeightBold}})
	if err != nil {
		t.Fatal(err)
	}
	if regular == bold {
		t.Fatalf("two keys gave one face")
	}
	if r, _ := c.Registered(regular.GoText().Font); r.Weight != WeightRegular {
		t.Errorf("the first font is registered with weight %d", r.Weight)
	}
	if b, _ := c.Registered(bold.GoText().Font); b.Weight != WeightBold {
		t.Errorf("the second font is registered with weight %d", b.Weight)
	}
	if regular.File() != bold.File() || regular.File() == (File{}) {
		t.Errorf("the two fonts are not one file")
	}
	other, err := c.Add("rule 3", read(t, "EBGaramond12-Regular.otf"), Description{Family: "mine"})
	if err != nil {
		t.Fatal(err)
	}
	if other.File() == regular.File() {
		t.Errorf("two different fonts are one file")
	}
}
