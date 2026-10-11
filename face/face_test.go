package face

import (
	"bytes"
	"encoding/binary"
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

// collection wraps a font in a TrueType collection of one font. The tables
// keep their place after the header of the collection, so their offsets,
// which are from the start of the file, move by its length.
func collection(t *testing.T, font []byte) []byte {
	t.Helper()
	const header = 16
	ttc := []byte{'t', 't', 'c', 'f', 0, 1, 0, 0, 0, 0, 0, 1, 0, 0, 0, header}
	ttc = append(ttc, font...)
	numTables := int(binary.BigEndian.Uint16(ttc[header+4:]))
	for i := 0; i < numTables; i++ {
		at := header + 12 + 16*i + 8
		binary.BigEndian.PutUint32(ttc[at:], binary.BigEndian.Uint32(ttc[at:])+header)
	}
	return ttc
}

// The last font of a collection is embedded like the others.
func TestEmbedFromCollection(t *testing.T) {
	plain, err := Parse(read(t, "DejaVuSerif.ttf"), 0)
	if err != nil {
		t.Fatal(err)
	}
	f, err := Parse(collection(t, read(t, "DejaVuSerif.ttf")), 0)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.Embeddable(); err != nil {
		t.Fatalf("the font of a collection is not embeddable: %v", err)
	}
	a, _ := f.GlyphIndex('A')
	got, err := f.Embed([]uint16{uint16(a)}, EmbedOptions{})
	if err != nil {
		t.Fatal(err)
	}
	want, err := plain.Embed([]uint16{uint16(a)}, EmbedOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got.Program, want.Program) {
		t.Errorf("the program differs from the one of the font alone")
	}
}

// A CFF font is embedded whatever the number of glyphs: past 229 it needs a
// charset of its own, and past 64 KB of outlines offsets of three bytes.
func TestEmbedManyCFFGlyphs(t *testing.T) {
	f, err := Parse(read(t, "EBGaramond12-Regular.otf"), 0)
	if err != nil {
		t.Fatal(err)
	}
	for _, n := range []int{50, 229, 230, 231, 500, 1200} {
		glyphs := make([]uint16, n)
		for i := range glyphs {
			glyphs[i] = uint16(i + 1)
		}
		for _, keep := range []bool{false, true} {
			e, err := f.Embed(glyphs, EmbedOptions{KeepSubroutines: keep})
			if err != nil {
				t.Errorf("%d glyphs, KeepSubroutines %v: %v", n, keep, err)
				continue
			}
			// the program is read back by go-text, and draws the same glyphs
			subset, err := Parse(e.Program, 0)
			if err != nil {
				t.Errorf("%d glyphs, KeepSubroutines %v: the program is not read back: %v", n, keep, err)
				continue
			}
			for i, g := range e.Glyphs {
				if i == 0 {
					continue
				}
				want, _ := f.GoText().GlyphExtents(GID(g))
				got, _ := subset.GoText().GlyphExtents(GID(i))
				if got != want {
					t.Errorf("%d glyphs, KeepSubroutines %v: glyph %d has extents %+v, the font has %+v", n, keep, g, got, want)
					break
				}
			}
		}
	}
}

// The glyphs a composite glyph is made of are part of the program, and of
// the list that describes it.
func TestEmbedCompositeGlyph(t *testing.T) {
	f, err := Parse(read(t, "DejaVuSerif.ttf"), 0)
	if err != nil {
		t.Fatal(err)
	}
	eacute, _ := f.GlyphIndex('é')
	glyphs := make([]uint16, 1, 8) // room after it: nothing is written there
	glyphs[0] = uint16(eacute)
	e, err := f.Embed(glyphs, EmbedOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if spare := glyphs[:cap(glyphs)]; spare[1] != 0 {
		t.Errorf("the caller's slice was written past its length: %v", spare)
	}
	if len(e.Glyphs) < 3 || e.Glyphs[1] != uint16(eacute) {
		t.Fatalf("glyphs %v: want .notdef, é and its components", e.Glyphs)
	}
	if len(e.Advances) != len(e.Glyphs) || len(e.Runes) != len(e.Glyphs) {
		t.Errorf("%d glyphs, %d advances, %d runes", len(e.Glyphs), len(e.Advances), len(e.Runes))
	}
	// the program holds as many glyphs as the list says
	subset, err := sfnt.ParseEmbeddedSFNT(e.Program, 0)
	if err != nil {
		t.Fatalf("the program is not read back: %v", err)
	}
	if n := int(subset.NumGlyphs()); n != len(e.Glyphs) {
		t.Errorf("the program has %d glyphs, the list %d", n, len(e.Glyphs))
	}
}

// The same glyphs give the same bytes, now and later.
func TestEmbedIsReproducible(t *testing.T) {
	for _, name := range []string{"DejaVuSerif.ttf", "EBGaramond12-Regular.otf"} {
		data := read(t, name)
		first, err := Parse(data, 0)
		if err != nil {
			t.Fatal(err)
		}
		second, _ := Parse(data, 0)
		a, err := first.Embed([]uint16{5, 6, 7}, EmbedOptions{})
		if err != nil {
			t.Fatal(err)
		}
		// the date of the font is the one it was made, not the time it is
		b, _ := second.Embed([]uint16{5, 6, 7}, EmbedOptions{})
		if !bytes.Equal(a.Program, b.Program) {
			t.Errorf("%s: two faces of the font give different programs", name)
		}
		if got := a.Program; !bytes.Contains(got, sfntHeadDates(t, data)) {
			t.Errorf("%s: the program does not keep the dates of the font", name)
		}
	}
}

// sfntHeadDates returns the created and modified dates of the head table.
func sfntHeadDates(t *testing.T, data []byte) []byte {
	t.Helper()
	numTables := int(binary.BigEndian.Uint16(data[4:]))
	for i := 0; i < numTables; i++ {
		record := data[12+16*i:]
		if string(record[:4]) == "head" {
			offset := binary.BigEndian.Uint32(record[8:])
			return data[offset+20 : offset+36]
		}
	}
	t.Fatal("no head table")
	return nil
}

// An EOT whose font is XOR-ed is read without changing the bytes given.
func TestParseObfuscatedEOT(t *testing.T) {
	plain := read(t, "DejaVuSerif.eot")
	// set the flag and encode the font, as the format says: the font is the
	// last FontDataSize bytes
	eot := append([]byte(nil), plain...)
	size := int(binary.LittleEndian.Uint32(eot[4:]))
	binary.LittleEndian.PutUint32(eot[12:], binary.LittleEndian.Uint32(eot[12:])|0x10000000)
	for i := len(eot) - size; i < len(eot); i++ {
		eot[i] ^= 0x50
	}
	given := append([]byte(nil), eot...)
	for i := 0; i < 2; i++ {
		f, err := Parse(eot, 0)
		if err != nil {
			t.Fatalf("parse %d: %v", i+1, err)
		}
		if f.UnitsPerEm() != 2048 {
			t.Errorf("parse %d: %d units per em", i+1, f.UnitsPerEm())
		}
		if !bytes.Equal(eot, given) {
			t.Fatalf("parse %d changed the bytes it was given", i+1)
		}
	}
}

func TestCharacterOf(t *testing.T) {
	for _, c := range []struct {
		runes []rune
		want  rune
	}{
		{nil, 0},
		{[]rune{'A'}, 'A'},
		{[]rune{0x80, '€'}, '€'},      // a Windows code page beside the character
		{[]rune{0xF020, ' '}, ' '},    // a symbol encoding beside it
		{[]rune{0xE001}, 0xE001},      // private use when there is nothing else
		{[]rune{0xFFFF, 0, 'x'}, 'x'}, // the end of a format 4 table is not a character
	} {
		if got := characterOf(c.runes); got != c.want {
			t.Errorf("characterOf(%U) = %U, want %U", c.runes, got, c.want)
		}
	}
}

// Without its file a face has no tables to read the Microsoft values from:
// it keeps the ones of go-text rather than giving zeros.
func TestMetricsWithoutFile(t *testing.T) {
	parsed, err := Parse(read(t, "DejaVuSerif.ttf"), 0)
	if err != nil {
		t.Fatal(err)
	}
	bare := FromGoText(parsed.GoText(), nil, 0)
	m, shaper := bare.Metrics(MicrosoftRule), bare.Metrics(ShaperRule)
	if m.Ascent == 0 || m != shaper {
		t.Errorf("metrics %+v, want those of go-text %+v", m, shaper)
	}
}
