package glyph

import (
	"encoding/binary"
	"math"
	"os"
	"testing"

	"github.com/anaelorlinski/aaoo.textengine-go/face"
)

// zeroDecorationMetrics returns the font with its underline (post) and
// strikeout (OS/2) metrics set to zero, as a font that states none.
func zeroDecorationMetrics(t *testing.T, data []byte) []byte {
	t.Helper()
	out := append([]byte(nil), data...)
	numTables := int(binary.BigEndian.Uint16(out[4:]))
	found := 0
	for i := 0; i < numTables; i++ {
		rec := out[12+16*i:]
		off := binary.BigEndian.Uint32(rec[8:])
		switch string(rec[:4]) {
		case "post":
			binary.BigEndian.PutUint16(out[off+8:], 0)  // underlinePosition
			binary.BigEndian.PutUint16(out[off+10:], 0) // underlineThickness
			found++
		case "OS/2":
			binary.BigEndian.PutUint16(out[off+26:], 0) // yStrikeoutSize
			binary.BigEndian.PutUint16(out[off+28:], 0) // yStrikeoutPosition
			found++
		}
	}
	if found != 2 {
		t.Fatal("post or OS/2 table not found")
	}
	return out
}

// A font that states no underline or strikeout gets the fallbacks the
// documentation promises: 5% of the ascent thick, the underline 10% of the
// ascent under the baseline, the strikethrough centred on half the x-height.
func TestDecorationsFallback(t *testing.T) {
	data, err := os.ReadFile("../sfnt/resources/DejaVuSerif.ttf")
	if err != nil {
		t.Fatal(err)
	}
	f, err := face.Parse(zeroDecorationMetrics(t, data), 0)
	if err != nil {
		t.Fatal(err)
	}
	m := f.Metrics(face.ShaperRule)
	if m.UnderlinePosition != 0 || m.UnderlineThickness != 0 || m.StrikethroughPosition != 0 || m.StrikethroughThickness != 0 {
		t.Fatalf("the patched font still states decoration metrics: %+v", m)
	}
	const size = 100.0
	scale := size / float64(f.UnitsPerEm())
	ascent := float64(m.Ascent) * scale
	xHeight := float64(m.XHeight) * scale
	d := DecorationsOf(f, size, face.ShaperRule)

	eq := func(what string, got, want float64) {
		t.Helper()
		if math.Abs(got-want) > 1e-9 {
			t.Errorf("%s: %v, want %v", what, got, want)
		}
	}
	eq("underline top", d.Underline.Top, -0.1*ascent)
	eq("underline thickness", d.Underline.Thickness, 0.05*ascent)
	eq("strikethrough thickness", d.Strikethrough.Thickness, d.Underline.Thickness)
	eq("strikethrough centre", d.Strikethrough.Center(), xHeight/2)
	eq("overline top", d.Overline.Top, ascent)
	eq("overline thickness", d.Overline.Thickness, d.Underline.Thickness)
	if d.Underline.Top >= 0 || d.Underline.Thickness <= 0 || d.Strikethrough.Top <= 0 {
		t.Errorf("implausible fallbacks %+v", d)
	}
}
