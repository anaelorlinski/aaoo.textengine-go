# aaoo.textengine-go

Fonts, glyphs and text layout for a document renderer: read a font, lay a
text out on lines, turn positioned glyphs into geometry, and write the font
program a PDF or an SVG embeds.

Shaping, bidi, line breaking and font fallback are
[go-text](https://github.com/go-text/typesetting)'s. The `layout` package
drives them; the others hold what go-text does not have.

| Package | |
|---|---|
| `face` | a font read through go-text (TTF, OTF, TTC, WOFF, WOFF2, EOT): its metrics, with the choice of the ascent and descent a renderer uses (`VerticalRule`); what it says of itself (`Describe`); `Embed`, the subset to put in a document; and `Collection`, the fonts a program draws with, added from memory under the family and style it chooses or found on the system |
| `glyph` | glyph outlines at a size as plain segments; `Run`, the positioned glyphs layout hands to drawing; synthetic italic and bold; the decoration lines a font asks for; vertical grid snap |
| `layout` | text laid out on lines: `Style`, plain values saying how a text is set (font, spacing, white space, hyphenation); `Engine`, which finds the fonts, shapes, and breaks the first line of a text at a width (`SplitFirstLine`), with hyphenation supplied through an interface; the fonts a program gives it (`LoadFont`) and those a document brings (`AddFontFile`, which come first), what a missing family is set in (`Missing`), and what a font really is when bold or italic must be simulated (`StyleOf`); `Line`, the shaped runs of a line. It knows nothing of CSS: callers fill a `Style` from whatever describes their text |
| `caps` | small and petite capitals simulated for a font that has none, on the runs handed to the shaper |
| `sfnt` | the SFNT tables for writing: subsetting of glyf and CFF fonts, CFF desubroutinizing, WOFF, WOFF2 and EOT, OpenType feature queries |

```go
f, err := face.Parse(data, 0)
gid, _ := f.GlyphIndex('A')
outline, _ := glyph.FromFace(f, gid, 12, glyph.Style{Slant: glyph.FauxItalicSlant})

program, err := f.Embed(usedGlyphs, face.EmbedOptions{
	ConvertCFF: func(s *sfnt.SFNT) (*sfnt.SFNT, error) { return otf2ttf.Convert(s, 0) },
})
```

Converting CFF outlines to TrueType is
[otf2ttf-go](https://github.com/anaelorlinski/otf2ttf-go), plugged in through
`EmbedOptions.ConvertCFF`.

The module produces geometry and numbers; it does not draw. Expanding an
outline for a synthetic bold needs a path offset, which is the renderer's:
[aaoo.drawengine-go](https://github.com/anaelorlinski/aaoo.drawengine-go)
does it.

Not done yet: color and bitmap glyphs, and embedding an instance of a variable
font other than the default one.

License: MIT, see `LICENSE` and `NOTICE`.
