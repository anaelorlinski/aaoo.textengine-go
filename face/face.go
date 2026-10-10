// Package face reads a font for layout and drawing, and prepares it for
// embedding.
//
// Reading goes through go-text: the same parsed font serves shaping, metrics
// and outlines, variable instances included. Writing, which go-text does not
// do, goes through the sfnt package of this module: subsetting and
// desubroutinizing. Converting CFF outlines to TrueType is plugged in by the
// caller, see EmbedOptions.
package face

import (
	"bytes"
	"errors"
	"fmt"
	"sync"

	"github.com/anaelorlinski/aaoo.textengine-go/sfnt"
	"github.com/go-text/typesetting/font"
	ot "github.com/go-text/typesetting/font/opentype"
)

// GID is a glyph index in a font file.
type GID = font.GID

// Face is one font of a font file, with the bytes it was read from.
type Face struct {
	data  []byte // the SFNT bytes: TTF or OTF, WOFF, WOFF2 and EOT decoded
	index int
	ft    *font.Face
	vert  Vertical
	// glyf reports TrueType outlines; cff2 reports CFF2 ones, which the
	// sfnt package does not write.
	glyf, cff2 bool

	// the font as the sfnt package reads it, for embedding: see program
	once     sync.Once
	parsed   *sfnt.SFNT
	parseErr error
	embed    sync.Mutex
}

// Parse reads the font at the given index of a TTF, OTF, TTC, WOFF, WOFF2 or
// EOT file. The index is 0 for a file holding one font.
func Parse(data []byte, index int) (*Face, error) {
	data, err := sfnt.ToSFNT(data)
	if err != nil {
		return nil, err
	}
	ld, err := loader(data, index)
	if err != nil {
		return nil, err
	}
	ft, err := font.NewFont(ld)
	if err != nil {
		return nil, err
	}
	return newFace(font.NewFace(ft), data, index, ld), nil
}

// FromGoText returns the face for a font go-text has already read, so that
// it is not read again: data is the file ft comes from and index its index
// in it. The face shares the variation settings of ft.
//
// The vertical metrics and the embedding need data to be the right file:
// with other bytes, Vertical is zero and Embed fails.
func FromGoText(ft *font.Face, data []byte, index int) *Face {
	var ld *ot.Loader
	if plain, err := sfnt.ToSFNT(data); err == nil {
		data = plain
		ld, _ = loader(data, index)
	}
	return newFace(ft, data, index, ld)
}

func loader(data []byte, index int) (*ot.Loader, error) {
	all, err := loaders(data)
	if err != nil {
		return nil, err
	}
	if index < 0 || index >= len(all) {
		return nil, fmt.Errorf("font index %d out of range: the file holds %d fonts", index, len(all))
	}
	return all[index], nil
}

// loaders returns the readers of the fonts of an SFNT file.
func loaders(data []byte) ([]*ot.Loader, error) {
	return ot.NewLoaders(bytes.NewReader(data))
}

var (
	tagGlyf = ot.MustNewTag("glyf")
	tagCFF2 = ot.MustNewTag("CFF2")
)

func newFace(ft *font.Face, data []byte, index int, ld *ot.Loader) *Face {
	f := &Face{data: data, index: index, ft: ft}
	if ld != nil {
		f.vert = readVertical(ld)
		f.glyf = ld.HasTable(tagGlyf)
		f.cff2 = ld.HasTable(tagCFF2)
	}
	return f
}

// TrueType reports whether the outlines are TrueType ones (glyf): their
// outer contours turn clockwise, those of CFF outlines counter-clockwise.
func (f *Face) TrueType() bool { return f.glyf }

// GoText returns the go-text face, for shaping. It shares the variation
// settings of f.
func (f *Face) GoText() *font.Face { return f.ft }

// Data returns the SFNT bytes of the file the face comes from. It must not
// be modified.
func (f *Face) Data() []byte { return f.data }

// Index returns the index of the face in its file.
func (f *Face) Index() int { return f.index }

// UnitsPerEm returns the number of font units in one em.
func (f *Face) UnitsPerEm() uint16 { return f.ft.Upem() }

// SetVariations selects an instance of a variable font. It has no effect on
// a static font.
func (f *Face) SetVariations(variations []font.Variation) { f.ft.SetVariations(variations) }

// GlyphIndex returns the glyph a font maps the rune to, and whether it has one.
func (f *Face) GlyphIndex(r rune) (GID, bool) { return f.ft.NominalGlyph(r) }

// Advance returns the horizontal advance of a glyph, in font units, for the
// current variation settings.
func (f *Face) Advance(gid GID) float32 { return f.ft.HorizontalAdvance(gid) }

// Metrics are the vertical metrics of a face, in font units, with the Y axis
// pointing up: Descent and UnderlinePosition are usually negative.
type Metrics struct {
	Ascent, Descent, LineGap                      float32
	XHeight, CapHeight                            float32
	UnderlinePosition, UnderlineThickness         float32
	StrikethroughPosition, StrikethroughThickness float32
}

// Vertical returns the vertical metrics the font states in its tables.
func (f *Face) Vertical() Vertical { return f.vert }

// Metrics returns the vertical metrics, with the ascent, the descent and the
// line gap the rule selects.
//
// They are for the current variation settings, except the ascent, the
// descent and the line gap of MicrosoftRule, which are those of the default
// instance.
func (f *Face) Metrics(rule VerticalRule) Metrics {
	ext, _ := f.ft.FontHExtents()
	out := Metrics{
		Ascent:                 ext.Ascender,
		Descent:                ext.Descender,
		LineGap:                ext.LineGap,
		XHeight:                f.ft.LineMetric(font.XHeight),
		CapHeight:              f.ft.LineMetric(font.CapHeight),
		UnderlinePosition:      f.ft.LineMetric(font.UnderlinePosition),
		UnderlineThickness:     f.ft.LineMetric(font.UnderlineThickness),
		StrikethroughPosition:  f.ft.LineMetric(font.StrikethroughPosition),
		StrikethroughThickness: f.ft.LineMetric(font.StrikethroughThickness),
	}
	if rule == MicrosoftRule {
		ascent, descent, lineGap := f.vert.Resolve(rule)
		out.Ascent, out.Descent, out.LineGap = ascent, -descent, lineGap
	}
	return out
}

// EmbedOptions selects how a font program is prepared for embedding.
type EmbedOptions struct {
	// ConvertCFF, when set, is given the subset of a font with CFF outlines
	// and returns it with TrueType outlines, which printer RIPs handle more
	// reliably. github.com/anaelorlinski/otf2ttf-go provides it. When it is
	// nil or returns an error, the CFF outlines are embedded as they are.
	ConvertCFF func(*sfnt.SFNT) (*sfnt.SFNT, error)
	// KeepSubroutines keeps the charstring subroutines of a CFF font that is
	// embedded as CFF. By default they are inlined, for the same reason.
	KeepSubroutines bool
	// KeepAllGlyphs embeds the whole font instead of the requested glyphs.
	// Glyphs, Advances and Runes of the result still describe the requested
	// glyphs only, and a glyph keeps its index in Program.
	KeepAllGlyphs bool
}

// Embedded is a font program ready to be embedded in a PDF or an SVG, with
// what a PDF font descriptor says of it.
type Embedded struct {
	// Program is a complete SFNT file holding only the requested glyphs.
	Program []byte
	// TrueType reports whether the outlines are TrueType (glyf); otherwise
	// they are CFF.
	TrueType bool
	// Glyphs are the glyphs of Program in order: the glyph at position i of
	// Program is Glyphs[i] of the original font. Glyphs[0] is always 0.
	Glyphs []uint16
	// Advances are the horizontal advances of Glyphs, in font units.
	Advances []uint16
	// Runes are the characters Glyphs stand for, as the font maps them: the
	// first one of each glyph, and 0 for a glyph no character maps to.
	Runes []rune

	// PostScriptName is the name of the font: its PostScript name, or its
	// full name, or its family name; empty when it states none.
	PostScriptName string
	UnitsPerEm     uint16
	// BBox is the box of all the glyphs of the font: xMin, yMin, xMax, yMax.
	BBox        [4]int16
	ItalicAngle float64
	// Ascender and Descender are those of the hhea table; the descender is
	// negative.
	Ascender, Descender int16
	CapHeight           int16
}

// program returns the font as the sfnt package reads it. It is read once.
func (f *Face) program() (*sfnt.SFNT, error) {
	f.once.Do(func() {
		switch {
		case len(f.data) == 0:
			f.parseErr = errors.New("the font file is not known")
		case f.cff2:
			f.parseErr = errors.New("CFF2 outlines cannot be embedded")
		default:
			f.parsed, f.parseErr = sfnt.ParseSFNT(f.data, f.index)
		}
	})
	return f.parsed, f.parseErr
}

// Embeddable returns nil when Embed can write the glyphs as they are drawn.
// It cannot for a font it does not read, and for an instance of a variable
// font other than the default one, since the default instance is what is
// embedded: such text is to be drawn from its outlines.
//
// The first call reads the font for embedding; the result is kept.
func (f *Face) Embeddable() error {
	if len(f.ft.Coords()) != 0 {
		return errors.New("an instance of a variable font cannot be embedded")
	}
	_, err := f.program()
	return err
}

// EmbedAdvance returns the horizontal advance of a glyph in the program
// Embed writes, in font units: the one of the default instance. It is 0 for
// a font that is not embeddable.
func (f *Face) EmbedAdvance(gid uint16) uint16 {
	parsed, err := f.program()
	if err != nil {
		return 0
	}
	return parsed.GlyphAdvance(gid)
}

// Embed returns a font program holding the given glyphs only, for the
// default instance of the font. The glyph 0 (.notdef) is always kept, first.
//
// A variable font is embedded as its default instance: its variation tables
// are not carried over, and the outlines are not those of the instance
// selected with SetVariations. See Embeddable.
func (f *Face) Embed(glyphs []uint16, opts EmbedOptions) (Embedded, error) {
	parsed, err := f.program()
	if err != nil {
		return Embedded{}, err
	}
	// Subsetting reads the font, and the glyph names are dropped from it.
	f.embed.Lock()
	defer f.embed.Unlock()

	list := make([]uint16, 0, len(glyphs)+1)
	list = append(list, 0)
	seen := map[uint16]bool{0: true}
	for _, g := range glyphs {
		if !seen[g] {
			seen[g] = true
			list = append(list, g)
		}
	}
	out := Embedded{
		Glyphs:     list,
		Advances:   make([]uint16, len(list)),
		Runes:      make([]rune, len(list)),
		UnitsPerEm: parsed.Head.UnitsPerEm,
		BBox:       [4]int16{parsed.Head.XMin, parsed.Head.YMin, parsed.Head.XMax, parsed.Head.YMax},
	}
	if parsed.Post != nil {
		out.ItalicAngle = float64(parsed.Post.ItalicAngle)
	}
	if parsed.Hhea != nil {
		out.Ascender, out.Descender = parsed.Hhea.Ascender, parsed.Hhea.Descender
	}
	if parsed.OS2 != nil {
		out.CapHeight = parsed.OS2.SCapHeight
	}
	for i, g := range list {
		out.Advances[i] = parsed.GlyphAdvance(g)
		if rs := parsed.GlyphToUnicode(g); len(rs) != 0 {
			out.Runes[i] = rs[0]
		}
	}
names:
	for _, id := range []sfnt.NameID{sfnt.NamePostScript, sfnt.NameFull, sfnt.NameFontFamily} {
		if parsed.Name == nil {
			break
		}
		for _, record := range parsed.Name.Get(id) {
			out.PostScriptName = record.String()
			break names
		}
	}

	program := parsed
	if !opts.KeepAllGlyphs {
		if parsed.IsCFF && parsed.CFF != nil {
			parsed.CFF.SetGlyphNames(nil)
		}
		program, err = parsed.Subset(list, sfnt.SubsetOptions{
			Tables:         sfnt.KeepPDFTables,
			Desubroutinize: !opts.KeepSubroutines,
		})
		if err != nil {
			return Embedded{}, fmt.Errorf("subsetting the font: %w", err)
		}
	}
	if program.IsCFF && opts.ConvertCFF != nil {
		// A charstring the converter cannot interpret leaves the CFF as it is.
		if ttf, err := opts.ConvertCFF(program); err == nil {
			program = ttf
		}
	}
	out.Program, out.TrueType = program.Write(), program.IsTrueType
	return out, nil
}
