package face

import (
	"os"
	"testing"

	"github.com/anaelorlinski/aaoo.textengine-go/sfnt"
)

func read(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile("../sfnt/resources/" + name)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestParseFormats(t *testing.T) {
	ttf, err := Parse(read(t, "DejaVuSerif.ttf"), 0)
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"DejaVuSerif.woff", "DejaVuSerif.woff2", "DejaVuSerif.eot"} {
		f, err := Parse(read(t, name), 0)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		// The WOFF sample is another build of the font: its underline sits
		// lower. The WOFF2 sample is the TTF, compressed.
		fm, tm := f.Metrics(ShaperRule), ttf.Metrics(ShaperRule)
		if f.UnitsPerEm() != ttf.UnitsPerEm() || fm.Ascent != tm.Ascent || fm.Descent != tm.Descent {
			t.Errorf("%s: metrics differ from the TTF", name)
		}
		if name == "DejaVuSerif.woff2" && fm != tm {
			t.Errorf("%s: metrics %+v, want %+v", name, fm, tm)
		}
		a, _ := f.GlyphIndex('A')
		b, _ := ttf.GlyphIndex('A')
		if f.Advance(a) != ttf.Advance(b) {
			t.Errorf("%s: advance of A differs from the TTF", name)
		}
	}
	if _, err := Parse(read(t, "DejaVuSerif.ttf"), 1); err == nil {
		t.Errorf("index 1 of a single font file must fail")
	}
	if _, err := Parse([]byte("not a font"), 0); err == nil {
		t.Errorf("garbage must fail")
	}
}

func TestMetrics(t *testing.T) {
	f, err := Parse(read(t, "DejaVuSerif.ttf"), 0)
	if err != nil {
		t.Fatal(err)
	}
	m := f.Metrics(ShaperRule)
	if f.UnitsPerEm() != 2048 || m.Ascent != 1901 || m.Descent != -483 {
		t.Errorf("upem %d ascent %v descent %v, want 2048, 1901, -483", f.UnitsPerEm(), m.Ascent, m.Descent)
	}
	if m.XHeight <= 0 || m.CapHeight <= m.XHeight || m.UnderlinePosition >= 0 || m.UnderlineThickness <= 0 {
		t.Errorf("implausible metrics %+v", m)
	}
}

func TestEmbed(t *testing.T) {
	for _, tc := range []struct {
		name     string
		trueType bool
	}{{"DejaVuSerif.ttf", true}, {"EBGaramond12-Regular.otf", false}} {
		f, err := Parse(read(t, tc.name), 0)
		if err != nil {
			t.Fatal(err)
		}
		var want []uint16
		for _, r := range "Hello" {
			g, _ := f.GlyphIndex(r)
			want = append(want, uint16(g))
		}
		e, err := f.Embed(want, EmbedOptions{})
		if err != nil {
			t.Fatalf("%s: %v", tc.name, err)
		}
		if e.TrueType != tc.trueType {
			t.Errorf("%s: TrueType = %v", tc.name, e.TrueType)
		}
		// H, e, l, o and .notdef: the second l is not repeated.
		if len(e.Glyphs) != 5 || e.Glyphs[0] != 0 {
			t.Errorf("%s: glyphs %v, want .notdef first and 5 in all", tc.name, e.Glyphs)
		}
		if len(e.Program) >= len(f.Data())/4 {
			t.Errorf("%s: subset is %d bytes of %d, not much smaller", tc.name, len(e.Program), len(f.Data()))
		}
		if !tc.trueType {
			// A CFF subset for embedding keeps the CFF table and little else,
			// too little for the SFNT reader of this module to load it back.
			if string(e.Program[:4]) != "OTTO" {
				t.Errorf("%s: the subset is not an OpenType CFF file", tc.name)
			}
			continue
		}
		// A subset for embedding has no cmap: the document maps codes to glyphs.
		sub, err := sfnt.ParseEmbeddedSFNT(e.Program, 0)
		if err != nil {
			t.Fatalf("%s: the subset does not parse: %v", tc.name, err)
		}
		if int(sub.NumGlyphs()) != len(e.Glyphs) {
			t.Errorf("%s: subset has %d glyphs, want %d", tc.name, sub.NumGlyphs(), len(e.Glyphs))
		}
		full, _ := sfnt.ParseSFNT(f.Data(), 0)
		for i, g := range e.Glyphs {
			if sub.GlyphAdvance(uint16(i)) != full.GlyphAdvance(g) {
				t.Errorf("%s: glyph %d of the subset does not have the advance of glyph %d", tc.name, i, g)
			}
		}
	}
}

func TestEmbedConvertCFFHook(t *testing.T) {
	f, err := Parse(read(t, "EBGaramond12-Regular.otf"), 0)
	if err != nil {
		t.Fatal(err)
	}
	called := false
	_, err = f.Embed([]uint16{5}, EmbedOptions{ConvertCFF: func(s *sfnt.SFNT) (*sfnt.SFNT, error) {
		called = true
		if !s.IsCFF {
			t.Errorf("the hook must receive the CFF subset")
		}
		return s, nil
	}})
	if err != nil || !called {
		t.Errorf("hook called: %v, err: %v", called, err)
	}
}

func TestFromGoText(t *testing.T) {
	data := read(t, "DejaVuSerif.ttf")
	parsed, err := Parse(data, 0)
	if err != nil {
		t.Fatal(err)
	}
	f := FromGoText(parsed.GoText(), data, 0)
	if f.GoText() != parsed.GoText() {
		t.Errorf("the go-text face must be shared, not read again")
	}
	if f.Vertical() != parsed.Vertical() || !f.TrueType() {
		t.Errorf("vertical metrics %+v, TrueType %v", f.Vertical(), f.TrueType())
	}
	if err := f.Embeddable(); err != nil {
		t.Errorf("Embeddable: %v", err)
	}
	// without its file, the face still shapes and draws, but is not embedded
	bare := FromGoText(parsed.GoText(), nil, 0)
	if bare.Embeddable() == nil {
		t.Errorf("a face without its file must not be embeddable")
	}
	if _, err := bare.Embed([]uint16{1}, EmbedOptions{}); err == nil {
		t.Errorf("Embed must fail without the file")
	}
}

func TestEmbedDescribesTheFont(t *testing.T) {
	f, err := Parse(read(t, "DejaVuSerif.ttf"), 0)
	if err != nil {
		t.Fatal(err)
	}
	a, _ := f.GlyphIndex('A')
	b, _ := f.GlyphIndex('b')
	e, err := f.Embed([]uint16{uint16(b), uint16(a), uint16(b)}, EmbedOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(e.Glyphs) != 3 || e.Glyphs[0] != 0 || e.Glyphs[1] != uint16(b) || e.Glyphs[2] != uint16(a) {
		t.Fatalf("glyphs %v, want .notdef, b, A", e.Glyphs)
	}
	if e.Runes[1] != 'b' || e.Runes[2] != 'A' {
		t.Errorf("runes %q", e.Runes)
	}
	if e.Advances[2] != uint16(f.Advance(a)) || e.Advances[2] != f.EmbedAdvance(uint16(a)) {
		t.Errorf("advance of A: %d, the face has %v", e.Advances[2], f.Advance(a))
	}
	if e.PostScriptName != "DejaVuSerif" || e.UnitsPerEm != 2048 || !e.TrueType {
		t.Errorf("name %q, upem %d, TrueType %v", e.PostScriptName, e.UnitsPerEm, e.TrueType)
	}
	if e.Ascender <= 0 || e.Descender >= 0 || e.BBox[2] <= e.BBox[0] {
		t.Errorf("ascender %d, descender %d, bbox %v", e.Ascender, e.Descender, e.BBox)
	}
	whole, err := f.Embed([]uint16{uint16(a)}, EmbedOptions{KeepAllGlyphs: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(whole.Program) <= len(e.Program) || len(whole.Glyphs) != 2 {
		t.Errorf("the whole font is %d bytes, the subset %d", len(whole.Program), len(e.Program))
	}
}

func TestVariableInstanceIsNotEmbeddable(t *testing.T) {
	f, err := Parse(read(t, "AdobeVFPrototype.otf"), 0)
	if err != nil {
		t.Fatal(err)
	}
	if f.Embeddable() == nil {
		t.Errorf("CFF2 outlines must not be embeddable")
	}
}
