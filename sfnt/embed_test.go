package sfnt

import (
	"encoding/binary"
	"fmt"
	"math/rand"
	"os"
	"strings"
	"testing"
)

func mustParse(t *testing.T, name string) *SFNT {
	t.Helper()
	b, err := os.ReadFile("resources/" + name)
	if err != nil {
		t.Fatal(err)
	}
	f, err := ParseSFNT(b, 0)
	if err != nil {
		t.Fatalf("%s: %v", name, err)
	}
	return f
}

// A character map written for a subset maps each character to its glyph,
// whatever the runs of characters and of glyphs.
func TestCmapWriteMapsEveryRune(t *testing.T) {
	rng := rand.New(rand.NewSource(3))
	maps := []map[rune]uint16{
		{'a': 1, 'b': 2, 'c': 3},
		// consecutive characters, consecutive glyphs, then a break
		{'a': 1, 'b': 2, 'c': 3, 'd': 4, 'e': 5, 'f': 6, 'g': 7, 'h': 8, 'i': 9, 'j': 10, 'l': 12, 'k': 11},
		{'a': 5, 'b': 4, 'c': 3},
		{0x1F600: 2, 'a': 1}, // past the 16 bits of format 4
	}
	for i := 0; i < 300; i++ {
		m := map[rune]uint16{}
		r, g := rune(32+rng.Intn(200)), uint16(1)
		for j, n := 0, 1+rng.Intn(120); j < n; j++ {
			m[r] = g
			// runs of characters and of glyphs, broken at random
			r += rune(1 + rng.Intn(2)*rng.Intn(40))
			g += uint16(1 + rng.Intn(2)*rng.Intn(5))
			if rng.Intn(10) == 0 {
				g = uint16(1 + rng.Intn(60000))
			}
		}
		maps = append(maps, m)
	}
	for _, m := range maps {
		var rs []rune
		for r := range m {
			rs = append(rs, r, r) // a rune may be listed for several glyphs
		}
		f := &SFNT{Tables: map[string][]byte{"cmap": cmapWrite(rs, m)}, Maxp: &maxpTable{NumGlyphs: 65535}}
		if err := f.parseCmap(); err != nil {
			t.Fatalf("map of %d runes: the table is not read back: %v", len(m), err)
		}
		for r, want := range m {
			if got := f.Cmap.Get(r); got != want {
				t.Fatalf("map of %d runes: %U gives glyph %d, want %d", len(m), r, got, want)
			}
		}
	}
}

// The characters of a glyph come from the Unicode subtables: a Macintosh
// subtable lists codes of Mac Roman, where 0x8E is é.
func TestCmapIgnoresMacintoshCodes(t *testing.T) {
	unicode := &cmapFormat14{glyphIDMap: map[rune]uint16{'é': 7}}
	mac := &cmapFormat14{glyphIDMap: map[rune]uint16{0x8E: 7}}
	cmap := &cmapTable{
		EncodingRecords: []cmapEncodingRecord{{PlatformID: 1, EncodingID: 0, Subtable: 0}, {PlatformID: 3, EncodingID: 1, Subtable: 1}},
		Subtables:       []cmapSubtable{mac, unicode},
	}
	if rs := cmap.ToUnicode(7); len(rs) != 1 || rs[0] != 'é' {
		t.Errorf("glyph 7 stands for %U, want é alone", rs)
	}
	if g := cmap.Get(0x8E); g != 0 {
		t.Errorf("U+008E gives glyph %d through the Macintosh subtable", g)
	}
	// a font with nothing else is read through what it has
	only := &cmapTable{EncodingRecords: []cmapEncodingRecord{{PlatformID: 1}}, Subtables: []cmapSubtable{mac}}
	if g := only.Get(0x8E); g != 7 {
		t.Errorf("a font with a Macintosh subtable only maps nothing")
	}
}

// Subsets of every size are read back with their glyphs: the offsets a CFF
// table holds change size at 108, 1132 and 32768, and its INDEX at 64 KB.
func TestCFFSubsetsAreReadBack(t *testing.T) {
	f := mustParse(t, "EBGaramond12-Regular.otf")
	sizes := []int{1, 2, 3}
	for n := 4; n < 1400; n += 7 {
		sizes = append(sizes, n)
	}
	for _, n := range sizes {
		glyphs := make([]uint16, n)
		for i := range glyphs {
			glyphs[i] = uint16(i)
		}
		for _, desubroutinize := range []bool{true, false} {
			subset, err := f.Subset(glyphs, SubsetOptions{Tables: KeepPDFTables, Desubroutinize: desubroutinize})
			if err != nil {
				t.Fatalf("%d glyphs, Desubroutinize %v: %v", n, desubroutinize, err)
			}
			back := &SFNT{Tables: map[string][]byte{"CFF ": subset.Tables["CFF "]}, IsCFF: true}
			if err := back.parseCFF(); err != nil {
				t.Fatalf("%d glyphs, Desubroutinize %v: the table is not read back: %v", n, desubroutinize, err)
			}
			if got := back.CFF.charStrings.Len(); got != n {
				t.Fatalf("%d glyphs, Desubroutinize %v: read back %d", n, desubroutinize, got)
			}
			// every glyph draws: its subroutines are where the table says
			for g := 0; g < n; g += 1 + n/40 {
				p1, p2 := &testPather{}, &testPather{}
				err1 := back.CFF.ToPath(p1, uint16(g), 0, 0, 0, 1, NoHinting)
				err2 := f.CFF.ToPath(p2, uint16(g), 0, 0, 0, 1, NoHinting)
				if err1 != nil || err2 != nil || p1.String() != p2.String() {
					t.Fatalf("%d glyphs, Desubroutinize %v: glyph %d differs from the font (%v, %v)", n, desubroutinize, g, err1, err2)
				}
			}
		}
	}
}

// A subroutine number need not be written the shortest way: renumbering the
// subroutines rewrites the bytes it takes, whatever their count.
func TestReindexSubrsWithLongNumbers(t *testing.T) {
	f := mustParse(t, "EBGaramond12-Regular.otf")
	// every one-byte or two-byte number before a call, as three bytes
	rewrite := func(index *cffINDEX) *cffINDEX {
		out := &cffINDEX{}
		for i := 0; i < index.Len(); i++ {
			src := index.Get(uint16(i))
			var dst []byte
			for j := 0; j < len(src); {
				b0 := src[j]
				n := 1
				switch {
				case b0 == 28:
					n = 3
				case b0 == 255:
					n = 5
				case 247 <= b0 && b0 <= 254:
					n = 2
				case b0 == 19 || b0 == 20: // masks: bytes follow, of a count not tracked here
					return nil
				case b0 == 12:
					n = 2
				}
				if j+n > len(src) {
					return nil
				}
				next := j + n
				if next < len(src) && (src[next] == 10 || src[next] == 29) && (32 <= b0 && b0 <= 254) {
					var v int
					if b0 <= 246 {
						v = int(b0) - 139
					} else if b0 <= 250 {
						v = (int(b0)-247)*256 + int(src[j+1]) + 108
					} else {
						v = -(int(b0)-251)*256 - int(src[j+1]) - 108
					}
					dst = append(dst, 28, byte(uint16(int16(v))>>8), byte(uint16(int16(v))))
				} else {
					dst = append(dst, src[j:next]...)
				}
				j = next
			}
			out.Add(dst)
		}
		return out
	}
	cff := *f.CFF
	fonts := *cff.fonts
	charStrings, global, local := rewrite(cff.charStrings), rewrite(cff.globalSubrs), rewrite(fonts.localSubrs[0])
	if charStrings == nil || global == nil || local == nil {
		t.Skip("the font has hint masks: its charstrings are not rewritten by this test")
	}
	cff.charStrings, cff.globalSubrs = charStrings, global
	fonts.localSubrs = []*cffINDEX{local}
	cff.fonts = &fonts
	long := *f
	long.CFF = &cff

	glyphs := []uint16{0}
	for g := uint16(40); g < 80; g++ {
		glyphs = append(glyphs, g)
	}
	subset, err := long.Subset(glyphs, SubsetOptions{Tables: KeepPDFTables})
	if err != nil {
		t.Fatal(err)
	}
	for i, g := range glyphs {
		p1, p2 := &testPather{}, &testPather{}
		err1 := subset.CFF.ToPath(p1, uint16(i), 0, 0, 0, 1, NoHinting)
		err2 := f.CFF.ToPath(p2, g, 0, 0, 0, 1, NoHinting)
		if err1 != nil || err2 != nil || p1.String() != p2.String() {
			t.Fatalf("glyph %d differs from the font after the subroutines are renumbered (%v, %v)", g, err1, err2)
		}
	}
}

// Fonts that are not written by the book are read: the last table of a file
// without its padding, a font in a collection, tables that describe the font
// and cannot be read.
func TestParseLenient(t *testing.T) {
	data, err := os.ReadFile("resources/DejaVuSerif.ttf")
	if err != nil {
		t.Fatal(err)
	}
	numTables := int(binary.BigEndian.Uint16(data[4:]))

	// the file ends with its last table, cut to a length that needs padding
	last, lastOffset := 0, uint32(0)
	for i := 0; i < numTables; i++ {
		if offset := binary.BigEndian.Uint32(data[12+16*i+8:]); lastOffset < offset {
			last, lastOffset = i, offset
		}
	}
	unpadded := append([]byte(nil), data...)
	length := binary.BigEndian.Uint32(unpadded[12+16*last+12:])
	if length%4 == 0 {
		length--
		binary.BigEndian.PutUint32(unpadded[12+16*last+12:], length)
	}
	if _, err := ParseSFNT(unpadded[:lastOffset+length], 0); err != nil {
		t.Errorf("a file that ends with its last table: %v", err)
	}

	// a collection of one font: the last font of a collection
	const header = 16
	ttc := append([]byte{'t', 't', 'c', 'f', 0, 1, 0, 0, 0, 0, 0, 1, 0, 0, 0, header}, data...)
	for i := 0; i < numTables; i++ {
		at := header + 12 + 16*i + 8
		binary.BigEndian.PutUint32(ttc[at:], binary.BigEndian.Uint32(ttc[at:])+header)
	}
	if f, err := ParseSFNT(ttc, 0); err != nil {
		t.Errorf("a collection of one font: %v", err)
	} else if f.NumGlyphs() != 3528 {
		t.Errorf("a collection of one font: %d glyphs", f.NumGlyphs())
	}
	if _, err := ParseSFNT(ttc, 1); err == nil {
		t.Errorf("the second font of a collection of one is read")
	}

	// tables that cannot be read and that drawing does not need
	f := mustParse(t, "DejaVuSerif.ttf")
	for _, tag := range []string{"kern", "post", "name", "OS/2"} {
		broken := *f
		broken.Tables = map[string][]byte{}
		for k, v := range f.Tables {
			broken.Tables[k] = v
		}
		broken.Tables[tag] = []byte{0xFF, 0xFF, 0xFF}
		font := broken.Write()
		parsed, err := ParseSFNT(font, 0)
		if err != nil {
			t.Errorf("a font whose %s table is not read: %v", tag, err)
			continue
		}
		if _, err := parsed.Subset([]uint16{0, 36, 68}, SubsetOptions{Tables: KeepPDFTables}); err != nil {
			t.Errorf("subset of a font whose %s table is not read: %v", tag, err)
		}
		// and with every table kept, which reads them all
		if _, err := parsed.Subset([]uint16{0, 36, 68}, SubsetOptions{Tables: KeepAllTables}); err != nil {
			t.Errorf("subset with all tables of a font whose %s table is not read: %v", tag, err)
		}
	}
}

// testPather records the outline it is given, to compare two of them.
type testPather struct{ strings.Builder }

func (p *testPather) MoveTo(x, y float64)       { fmt.Fprintf(p, "M%g %g", x, y) }
func (p *testPather) LineTo(x, y float64)       { fmt.Fprintf(p, "L%g %g", x, y) }
func (p *testPather) QuadTo(a, b, x, y float64) { fmt.Fprintf(p, "Q%g %g %g %g", a, b, x, y) }
func (p *testPather) CubeTo(a, b, c, d, x, y float64) {
	fmt.Fprintf(p, "C%g %g %g %g %g %g", a, b, c, d, x, y)
}
func (p *testPather) Close() { p.WriteString("z") }

// The Windows ascent and descent are unsigned: their sum is not, for a font
// that stores a negative descent there.
func TestVerticalMetricsDoNotWrap(t *testing.T) {
	f := mustParse(t, "DejaVuSerif.ttf")
	hhea, os2 := *f.Hhea, *f.OS2
	hhea.Ascender, hhea.Descender, hhea.LineGap = 2294, -901, 0
	os2.FsSelection &^= 0x0080 // not USE_TYPO_METRICS
	os2.UsWinAscent, os2.UsWinDescent = 2294, 64143
	font := *f
	font.Hhea, font.OS2 = &hhea, &os2
	if a, d, g := font.VerticalMetrics(); a != 2294 || d != 64143 || g != 0 {
		t.Errorf("vertical metrics %d %d %d, want 2294 64143 0", a, d, g)
	}
}
