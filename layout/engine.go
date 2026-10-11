package layout

import (
	"fmt"
	"log/slog"
	"math"
	"slices"
	"sort"
	"unicode"

	"github.com/anaelorlinski/aaoo.textengine-go/caps"
	"github.com/anaelorlinski/aaoo.textengine-go/face"
	"github.com/go-text/typesetting/di"
	"github.com/go-text/typesetting/font"
	"github.com/go-text/typesetting/font/opentype"
	"github.com/go-text/typesetting/fontscan"
	"github.com/go-text/typesetting/language"

	"github.com/go-text/typesetting/segmenter"
	"github.com/go-text/typesetting/shaping"
	"golang.org/x/image/math/fixed"
)

// Engine lays text out with the fonts of a font map. It is not safe for
// concurrent use.
type Engine struct {
	fm         *fontscan.FontMap
	shaper     shaping.HarfbuzzShaper
	unicodeSeg segmenter.Segmenter
	inputSeg   shaping.Segmenter

	lineWrapper shaping.LineWrapper

	// fonts holds the fonts added at run time, and the file of every font
	// in use, to be embedded in the target
	fonts         *face.Collection
	fontsFeatures map[*font.Font][]Feature // as requested by @font-face
	// memoryContents holds the contents given to AddFont, whose addresses
	// are their keys.
	memoryContents [][]byte

	textLayoutCache map[textKey]FirstLine

	// families holds the normalized names of the families given to the
	// engine.
	families map[string]bool
	// bundled holds the families given with LoadFont, and overrides the
	// name under which the fonts of AddFontFile naming one of them are
	// kept.
	bundled   map[string]bool
	overrides map[string]string
	// given holds the keys of the fonts given with LoadFont, and stated
	// what the source of a font of AddFontFile states of it, by the key of
	// the font: see StyleOf.
	given  map[string]bool
	stated map[string]Stated

	systemFontsLoaded bool

	// Missing says what a text is set in when its style names no family
	// given to the engine. By default the font map decides.
	Missing Missing
	// SystemFallbacks are the families of the system tried, in this order,
	// under [MissingToSystem], when no family of a style is installed.
	SystemFallbacks []string
	// Fallback is the family a text is set in when everything else failed,
	// under [MissingToSystem] and [MissingToFallback]. It should be a
	// family given to the engine. Empty, the style is left as it is.
	Fallback string

	// BreakRules chooses the rules of [Engine.CanBreakText]: those of
	// browsers by default.
	BreakRules BreakRules

	// Hyphenation is what `hyphens: auto` breaks words with. When it is
	// nil, as when it is [NoHyphenation], a word breaks only at the soft
	// hyphens of its author.
	Hyphenation Hyphenation
}

// hyphenation returns the hyphenation in use, never nil.
func (fc *Engine) hyphenation() Hyphenation {
	if fc.Hyphenation == nil {
		return NoHyphenation{}
	}
	return fc.Hyphenation
}

// NewEngine returns an engine using the fonts of fm.
func NewEngine(fm *fontscan.FontMap) *Engine {
	out := Engine{
		fm:              fm,
		fonts:           face.NewCollection(fm),
		fontsFeatures:   make(map[*font.Font][]Feature), // as loaded by loadOneFont
		textLayoutCache: make(map[textKey]FirstLine),
		families:        map[string]bool{},
		bundled:         map[string]bool{},
		overrides:       map[string]string{},
		given:           map[string]bool{},
		stated:          map[string]Stated{},
	}
	out.shaper.SetFontCacheSize(64)
	return &out
}

// FindSystemFont returns the file of a font of the system, for a family.
func (f *Engine) FindSystemFont(family string) (file string, ok bool) {
	location, ok := f.fm.FindSystemFont(family)
	return location.File, ok
}

// AddFontFile makes the font held in [content] available as [desc] says,
// under the given key, which identifies the content: it is found in the
// [fontscan.Location] of the font and is what [face.Collection.Lookup]
// expects. The features are applied to every text set in the font, before
// those of its style. [stated] tells what of [desc] its source states, see
// [Engine.StyleOf].
//
// It is meant for the fonts a document brings, as with an @font-face rule.
// Such a font is used before a font of [Engine.LoadFont] of the same
// family, as CSS asks: a family defined by @font-face comes before a font
// of the same name known otherwise.
func (f *Engine) AddFontFile(key string, content []byte, desc face.Description, features []Feature, stated Stated) error {
	// The font map of go-text gives the first font added the priority, so
	// the fonts of a family LoadFont also gave are kept under a name of
	// their own, which resolve puts before the name of the family.
	name := font.NormalizeFamily(desc.Family)
	private := ""
	if f.bundled[name] {
		private = name + privateSuffix
		desc.Family = private
	}

	ft, err := f.fonts.Add(key, content, desc)
	if err != nil {
		return err
	}
	if private != "" {
		f.overrides[name] = private
	} else {
		f.families[name] = true
	}
	f.stated[key] = stated

	// track the font features to apply
	f.fontsFeatures[ft.GoText().Font] = features

	// the layouts in cache were resolved without this font
	clear(f.textLayoutCache)
	return nil
}

// AddFont makes the font held in [content] (one font, not a collection)
// available under the family and the style of [desc], whatever the font file
// says about itself. The same content may be added under several families:
// it is then parsed and stored once.
//
// It returns the key identifying the content, as found in the
// [fontscan.Location] of the font and as [face.Collection.Lookup] expects.
func (f *Engine) AddFont(content []byte, desc face.Description) (string, error) {
	if len(content) == 0 {
		return "", fmt.Errorf("empty font content for family %s", desc.Family)
	}
	// the address of the content identifies it: callers register the same
	// slice under each alias of a family
	key := fmt.Sprintf("mem:%p:%d", &content[0], len(content))
	if _, known := f.fonts.Lookup(key); !known {
		// The key is an address: the content is kept, so that no other
		// font is ever allocated there. The collection may hold a decoded
		// copy only, as for a WOFF2 file.
		f.memoryContents = append(f.memoryContents, content)
	}
	if _, err := f.fonts.Add(key, content, desc); err != nil {
		return "", fmt.Errorf("failed to parse font for family %s: %s", desc.Family, err)
	}
	// the layouts in cache were resolved without this font
	clear(f.textLayoutCache)
	return key, nil
}

func (f *Engine) FontLocation(font *font.Font) fontscan.Location {
	return f.fm.FontLocation(font)
}

// Fonts returns the fonts of the configuration: those added at run time and
// the file of every font in use.
func (f *Engine) Fonts() *face.Collection { return f.fonts }

// FontContent returns the content of the given face, which may be needed
// in the final output: an SFNT file, whatever the format the font was given
// in.
func (f *Engine) FontContent(font fontscan.Location) []byte {
	// either the font is registred at run time or is loaded from disk
	b := f.fonts.Data(font)
	if b == nil {
		slog.Warn("FontContent: cannot read font", "file", font.File)
	}
	return b
}

// faceStyle returns the style of a font for the CSS values of its
// descriptors.
func faceStyle(style FontStyle, weight uint16, stretch FontStretch) face.Style {
	out := face.Style{
		Weight: int(weight),
		Italic: style == FSty_Italic || style == FSty_Oblique,
		Width:  face.WidthNormal,
	}
	switch stretch {
	case FStr_UltraCondensed:
		out.Width = face.WidthUltraCondensed
	case FStr_ExtraCondensed:
		out.Width = face.WidthExtraCondensed
	case FStr_Condensed:
		out.Width = face.WidthCondensed
	case FStr_SemiCondensed:
		out.Width = face.WidthSemiCondensed
	case FStr_SemiExpanded:
		out.Width = face.WidthSemiExpanded
	case FStr_Expanded:
		out.Width = face.WidthExpanded
	case FStr_ExtraExpanded:
		out.Width = face.WidthExtraExpanded
	case FStr_UltraExpanded:
		out.Width = face.WidthUltraExpanded
	}
	return out
}

func newAspect(style FontStyle, weight uint16, stretch FontStretch) font.Aspect {
	// A weight of 0 is kept: it is not a request for the regular weight.
	aspect := faceStyle(style, weight, stretch).Aspect()
	aspect.Weight = font.Weight(weight)
	return aspect
}

func newQuery(fd FontDescription) fontscan.Query {
	return fontscan.Query{
		Families: fd.Family,
		Aspect:   newAspect(fd.Style, fd.Weight, fd.Stretch),
	}
}

func newFeatures(features []Feature) []shaping.FontFeature {
	fts := make([]shaping.FontFeature, len(features))
	for i, f := range features {
		fts[i] = shaping.FontFeature{
			Tag:   opentype.NewTag(f.Tag[0], f.Tag[1], f.Tag[2], f.Tag[3]),
			Value: f.Value,
		}
	}
	return fts
}

// ResolveFace returns the face a rune is set in for a font description,
// or nil when the engine has no font at all.
func (fc *Engine) ResolveFace(r rune, font FontDescription) *font.Face {
	query := newQuery(font)
	fc.fm.SetQuery(query)
	return fc.fm.ResolveFace(r)
}

// uses sizeFactor * font.Size and returns sizeFactor
func (fc *Engine) shapeRune(r rune, desc FontDescription, features []Feature) ([]shaping.Glyph, shaping.Bounds, float32) {
	face := fc.ResolveFace(r, desc)
	if face == nil { // fontmap is broken
		return nil, shaping.Bounds{}, 1
	}

	// sizeFactor is used to get a better precision
	sizeFactor := fixed.Int26_6(1000)
	if desc.Size >= 400 { // avoid overflowing go-text Int26_6 upper limit
		sizeFactor = 1
	} else if desc.Size >= 200 {
		sizeFactor = 5
	} else if desc.Size >= 100 {
		sizeFactor = 10
	}

	out := fc.shaper.Shape(shaping.Input{
		Text:     []rune{r},
		RunStart: 0, RunEnd: 1,
		Direction:    di.DirectionLTR,
		FontFeatures: newFeatures(features),
		Script:       language.Latin,
		Language:     language.NewLanguage("en"),
		Face:         face,
		// float to fixed, the size factor is to get a better precision
		Size: floatToFixed(desc.Size) * sizeFactor,
	})
	return out.Glyphs, out.LineBounds, float32(sizeFactor)
}

// Heightx returns the height of the letter x: its top above the baseline.
//
// It is read from the outline of the glyph, in font units, and scaled once.
// A shaped glyph gives it in 26.6 fixed point, which is off by a few
// hundred-thousandths of a pixel even at a thousand times the size: enough
// to differ from other renderers at the last digit.
func (fc *Engine) Heightx(style *Style) float32 {
	style = fc.resolve(style)
	line := fc.wrapWordBreak([]rune{'x'}, style, Inf, false).Layout.Runs
	if len(line) != 0 && len(line[0].Glyphs) != 0 && line[0].Face != nil {
		face := line[0].Face
		if extents, ok := face.GlyphExtents(line[0].Glyphs[0].GlyphID); ok && face.Upem() != 0 {
			return extents.YBearing * style.FontDescription.Size / float32(face.Upem())
		}
	}

	// a glyph with no outline to measure: the shaped one
	glyphs, _, sizeFactor := fc.shapeRune('x', style.FontDescription, style.FontFeatures)

	if len(glyphs) == 0 { // fontmap is broken, return a 'reasonnable' value
		return style.FontDescription.Size
	}

	return float32(fixedToFloat(glyphs[0].YBearing) / sizeFactor) // fixed to float
}

func (fc *Engine) Width0(style *Style) float32 {
	style = fc.resolve(style)
	glyphs, _, sizeFactor := fc.shapeRune('0', style.FontDescription, style.FontFeatures)

	if len(glyphs) == 0 { // fontmap is broken, return a 'reasonnable' value
		return style.FontDescription.Size
	}
	return float32(fixedToFloat(glyphs[0].Advance) / sizeFactor) // fixed to float
}

func (fc *Engine) SpaceHeight(style *Style) (height, baseline float32) {
	style = fc.resolve(style)
	// Fork: read FontHExtents directly (rather than going through the
	// shaper's LineBounds). The `wrap` path computes per-line metrics
	// from FontHExtents the same way; routing strut metrics through the
	// shaper introduces 1/64-px quantisation that disagrees with `wrap`,
	// inflating the line box height by ~1/300 of an em and pushing
	// lines past page boundaries in tightly-sized layouts.
	face := fc.ResolveFace(' ', style.FontDescription)
	if face == nil {
		return 0, 0
	}
	ext, _ := face.FontHExtents()
	size := float32(style.FontDescription.Size)
	upem := float32(face.Upem())
	ascent := float32(ext.Ascender) * size / upem
	descent := float32(ext.Descender) * size / upem
	return ascent - descent, ascent
}

func (fc *Engine) widthSpace(style *Style) float32 {
	glyphs, _, sizeFactor := fc.shapeRune(' ', style.FontDescription, style.FontFeatures)

	if len(glyphs) == 0 { // fontmap is broken, return a 'reasonnable' value
		return style.FontDescription.Size
	}

	return float32(fixedToFloat(glyphs[0].Advance) / sizeFactor) // fixed to float
}

// BreakRules are the rules telling where a line may be broken between two
// texts laid out separately, see [Engine.CanBreakText].
type BreakRules uint8

const (
	// BrowserBreaks are the rules of Unicode as browsers apply them: a line
	// may be broken at a space before a solidus or a closing bracket.
	//
	// Rule LB13 of UAX #14 forbids a break there "even after spaces". ICU,
	// which browsers break lines with, leaves the space out of the rule, so
	// that "a /b" and "word )" may be broken at the space; Pango does the
	// same.
	BrowserBreaks BreakRules = iota
	// UnicodeBreaks are the rules of UAX #14 to the letter, as go-text
	// implements them.
	UnicodeBreaks
)

// CanBreakText reports whether a line may be broken strictly inside t,
// which has two runes at least, under the [BreakRules] of the engine.
//
// It is meant for the few runes around the boundary of two texts laid out
// separately. The lines of a single text are broken by go-text itself, with
// the rules of Unicode whatever the engine is set to.
func (fc *Engine) CanBreakText(t []rune) bool {
	if len(t) < 2 {
		return false
	}
	fc.unicodeSeg.Init(t)
	iter := fc.unicodeSeg.LineIterator()
	if iter.Next() {
		line := iter.Line()
		end := line.Offset + len(line.Text)
		if end < len(t) {
			return true
		}
	}
	if fc.BreakRules == UnicodeBreaks {
		return false
	}
	for i := 1; i < len(t); i++ {
		if t[i-1] == ' ' && (t[i] == '/' || unicode.Is(unicode.Pe, t[i])) {
			return true
		}
	}
	return false
}

// returns nil or a slice [wordStart:wordEnd]
func (fc *Engine) wordBoundaries(t []rune) *[2]int {
	if len(t) < 2 {
		return nil
	}
	fc.unicodeSeg.Init(t)
	iter := fc.unicodeSeg.WordIterator()
	if iter.Next() {
		word := iter.Word()
		return &[2]int{word.Offset, word.Offset + len(word.Text)}
	}
	return nil
}

// return -1 if not found
func (fc *Engine) getNextBreakPoint(text []rune) int {
	fc.unicodeSeg.Init(text)
	iter := fc.unicodeSeg.LineIterator()
	if iter.Next() {
		line := iter.Line()
		end := line.Offset + len(line.Text)
		return end
	}
	return -1
}

// GetLastWordEnd returns the index in `t` of the end of the before-last word,
// or -1
func (fc *Engine) GetLastWordEnd(t []rune) int {
	if len(t) < 2 {
		return -1
	}
	fc.unicodeSeg.Init(t)
	iter := fc.unicodeSeg.WordIterator()
	var word1, word2 segmenter.Word
	for iter.Next() {
		word1 = word2
		word2 = iter.Word()
	}
	return word1.Offset + len(word1.Text)
}

// returns the first occurence of c, or -1 if not found
func index(text []rune, c rune) int {
	for i, r := range text {
		if r == c {
			return i
		}
	}
	return -1
}

// returns the last occurence of c, or -1 if not found
func lastIndex(text []rune, c rune) int {
	for i := len(text) - 1; i >= 0; i-- {
		if text[i] == c {
			return i
		}
	}
	return -1
}

func hasSuffix(text []rune, c rune) bool {
	return len(text) != 0 && text[len(text)-1] == c
}

// equivalent of python rstrip(' ')
func trimTrailingSpaces(text []rune) []rune {
	i := len(text) - 1
	for ; i >= 0; i-- {
		if text[i] != ' ' {
			break
		}
	}
	return text[:i+1]
}

// secondLineIndex is -1 if the whole [text] fits into the first line
// pass Inf to remove width constraint
func (fc *Engine) wrap(text []rune, style *Style, maxWidth float32) FirstLine {
	return fc.wrapWordBreak(text, style, maxWidth, false)
}

func floatToFixed(v float32) fixed.Int26_6 { return fixed.Int26_6(v * 64) }
func fixedToFloat(v fixed.Int26_6) float32 { return float32(v) / 64 }

type textKey struct {
	text           string
	style          StyleKey
	maxWidth       float32
	allowWordBreak bool
}

// WrapWordBreak lays out the first line of text within maxWidth, [Inf] for
// no limit, breaking inside words when allowWordBreak is set.
//
// The line returned is kept by the engine, which returns the same one for
// the same text: it must not be modified.
func (fc *Engine) WrapWordBreak(text []rune, style *Style, maxWidth float32, allowWordBreak bool) FirstLine {
	return fc.wrapWordBreak(text, fc.resolve(style), maxWidth, allowWordBreak)
}

func (fc *Engine) wrapWordBreak(text []rune, style *Style, maxWidth float32, allowWordBreak bool) FirstLine {
	if len(text) == 0 {
		return FirstLine{
			Layout:   &Line{Style: style, MaxWidth: maxWidth},
			Length:   0,
			ResumeAt: -1,
			Width:    0, Height: 0, Baseline: 0,
			FirstLineRTL: false,
		}
	}

	key := textKey{string(text), style.Key(), maxWidth, allowWordBreak}
	if l, ok := fc.textLayoutCache[key]; ok {
		return l
	}

	// Fork: preserved newlines (pre/pre-wrap/pre-line/break-spaces) are
	// forced line breaks. The shaper wraps the whole text into one line
	// without flagging embedded "\n" as a break, so handle them here:
	//   - leading "\n":  emit a zero-length first line that resumes after the \n.
	//   - internal "\n": shape only the text up to the first "\n", then expose
	//     a ResumeAt past the newline and Length=position-of-newline so
	//     splitTextBox sees a preserved line break (length != resumeIndex,
	//     between == "\n").
	// Mirrors the first text engine of the compositor.
	preservesNewlines := style.preservesNewlines()
	if preservesNewlines && text[0] == '\n' {
		var height, baseline float32
		face := fc.ResolveFace(' ', style.FontDescription)
		if face != nil {
			out := fc.shaper.Shape(shaping.Input{
				Text:     []rune{' '},
				RunStart: 0, RunEnd: 1,
				Direction:    di.DirectionLTR,
				FontFeatures: newFeatures(style.FontFeatures),
				Script:       language.Latin,
				Language:     language.NewLanguage("en"),
				Face:         face,
				Size:         floatToFixed(style.FontDescription.Size),
			})
			height = fixedToFloat(out.LineBounds.Ascent - out.LineBounds.Descent)
			baseline = fixedToFloat(out.LineBounds.Ascent)
		}
		out := FirstLine{
			Layout:   &Line{Style: style, MaxWidth: maxWidth},
			Length:   0,
			ResumeAt: 1,
			Width:    0,
			Height:   height,
			Baseline: baseline,
		}
		fc.textLayoutCache[key] = out
		return out
	}
	if preservesNewlines {
		if nl := IndexRune(text, '\n'); nl != -1 {
			// Recurse on the prefix (no internal newlines) to do real shaping,
			// then patch Length/ResumeAt so the caller sees the forced break.
			prefix := text[:nl]
			sub := fc.wrapWordBreak(prefix, style, maxWidth, allowWordBreak)
			sub.Length = nl
			sub.ResumeAt = nl + 1
			fc.textLayoutCache[key] = sub
			return sub
		}
	}

	textWrap, spaceCollapse := style.TextWrap(), style.SpaceCollapse()
	var lang language.Language
	if flo := style.FontLanguageOverride; (flo != FontLanguageOverride{}) {
		lang = language.NewLanguage(LstToISO[flo])
	} else if lg := style.Lang; lg != "" {
		lang = language.NewLanguage(lg)
	} else {
		lang = language.DefaultLanguage()
	}

	// select the proper fonts
	fc.fm.SetQuery(newQuery(style.FontDescription))

	// segment the input text, with proper lang and size
	dir := di.DirectionLTR
	if style.Direction == RTL {
		dir = di.DirectionRTL
	}
	inputs := fc.inputSeg.Split(shaping.Input{
		Text:      text,
		RunEnd:    len(text),
		Language:  lang,
		Size:      floatToFixed(style.FontDescription.Size),
		Direction: dir, // default, will be overriden
	}, fc.fm)

	// simulate the capitals of font-variant-caps the fonts do not have
	inputs, _ = caps.Synthesize(inputs, text, capsVariant(style.FontDescription.VariantCaps))

	// TODO: lazy iterator
	outputs := make(shaping.Line, len(inputs))
	for i, input := range inputs {
		// the features are comming either from the style,
		// or registred via CSS @font-face rule
		defaults := NewFeatureSet(fc.fontsFeatures[input.Face.Font])
		defaults.Merge(style.FontFeatures)
		input.FontFeatures = newFeatures(defaults.List())

		// shape !
		output := fc.shaper.Shape(input)
		outputs[i] = output
	}

	// do we have some tabs ?
	if IndexRune(text, '\t') != -1 {
		tabWidth := floatToFixed(style.TabSize.Width)
		if style.TabSize.IsMultiple {
			spaceW := fc.widthSpace(style)
			tabWidth = floatToFixed(spaceW * float32(style.TabSize.Width))
		}
		AlignTabs(outputs, text, tabWidth, 0)
	}

	if style.LetterSpacing != 0 || style.WordSpacing != 0 {
		ws, ls := floatToFixed(style.WordSpacing), floatToFixed(style.LetterSpacing)
		shaping.AddSpacing(outputs, text, ws, ls)
		// add letter spacing at the end, like other browers do
		lastRun := &outputs[len(outputs)-1]
		lastRun.Glyphs[len(lastRun.Glyphs)-1].Advance += ls
		lastRun.RecomputeAdvance()
	}

	// now we can wrap the runs
	// resolve the wraping width, ignoring the constraint when not wraping
	wrapingMaxWidth := maxWidth
	if !textWrap {
		wrapingMaxWidth = Inf
	}
	mw, wLine, fitsOnFirstLine := fc.LineWrap(text, style, outputs, wrapingMaxWidth, allowWordBreak)
	line := wLine.Line

	if len(line) == 0 {
		return FirstLine{
			Layout:   &Line{Style: style, MaxWidth: maxWidth},
			Length:   0,
			ResumeAt: -1,
			Width:    0, Height: 0, Baseline: 0,
			FirstLineRTL: false,
		}
	}

	resumeAt := wLine.NextLine
	firstLineLength := resumeAt
	if resumeAt == len(text) {
		resumeAt = -1
	}

	// handle mandatory break properly : go-text keeps it on the first line,
	// while weasyprint skips it
	// See https://unicode.org/reports/tr14/#BK
	// 000B LINE TABULATION (VT)
	// 000C FORM FEED (FF)
	// 2028 LINE SEPARATOR
	// 2029 PARAGRAPH SEPARATOR
	if r := text[wLine.NextLine-1]; r == '\n' || r == '\u000B' || r == '\u000C' || r == '\u2028' || r == '\u2029' {
		resumeAt = wLine.NextLine
		firstLineLength = wLine.NextLine - 1
		lastRun := &line[len(line)-1]
		lastRun.Glyphs = lastRun.Glyphs[:len(lastRun.Glyphs)-1]
		lastRun.RecalculateAll()
		wLine.TrimmedTrailingWhitespace = 0 // not to interfere with latter space handling
	}

	// sort the line by visual order
	sort.Slice(line, func(i, j int) bool { return line[i].VisualIndex < line[j].VisualIndex })

	if !fitsOnFirstLine && spaceCollapse {
		// remove the space runes...
		text = trimTrailingSpaces(text[:firstLineLength])
		trimmed := firstLineLength - len(text)
		firstLineLength = len(text)
		// and the matching glyphs
		lastRun := &line[len(line)-1]
		lastRun.Runes.Count -= trimmed // we assume all spaces are on the same run
		i := len(lastRun.Glyphs) - 1
		for ; i >= 0; i-- {
			if lastRun.Glyphs[i].Width != 0 {
				break
			}
		}
		lastRun.Glyphs = lastRun.Glyphs[:i+1]
		lastRun.RecalculateAll()
	}

	// copy the line, owned by lineWrapper
	outLine := slices.Clone(line)

	var (
		width fixed.Int26_6
		// directly fetch line bounds, to avoid rounding errors
		height, top float32
	)
	for _, run := range outLine {
		width += run.Advance

		extents, _ := run.Face.FontHExtents()
		ascent := float32(extents.Ascender) * float32(style.FontDescription.Size) / float32(run.Face.Upem())
		descent := float32(extents.Descender) * float32(style.FontDescription.Size) / float32(run.Face.Upem())

		bottom := top - height

		if ascent > top {
			top = ascent
		}
		if descent < bottom {
			bottom = descent
		}
		height = top - bottom
	}

	if fitsOnFirstLine && spaceCollapse {
		// weasyprint puts the collapsed space on the line...
		if (width + wLine.TrimmedTrailingWhitespace) <= mw {
			width += wLine.TrimmedTrailingWhitespace
			// in RTL direction, the collapsed char must be expanded again
			// Note that the line is already sorted in visual order
			if dir.Progression() == di.TowardTopLeft {
				glyphs := outLine[0].Glyphs
				if len(glyphs) != 0 {
					glyphs[0].Advance += wLine.TrimmedTrailingWhitespace
				}
			}
		}
	}

	out := FirstLine{
		Layout:       &Line{text: text[:firstLineLength], Style: style, Runs: outLine, MaxWidth: maxWidth},
		Length:       firstLineLength,
		ResumeAt:     resumeAt,
		FirstLineRTL: style.Direction == RTL,
		Width:        fixedToFloat(width),
		Height:       height,
		Baseline:     top,
	}

	fc.textLayoutCache[key] = out

	return out
}

// LineWrap wraps the shaped runs of [text] and returns its first line.
func (fc *Engine) LineWrap(text []rune, style *Style, runs shaping.Line, maxWidth float32, allowWordBreak bool) (usedMaxWidth fixed.Int26_6, _ shaping.WrappedLine, _ bool) {
	dir := di.DirectionLTR
	if style.Direction == RTL {
		dir = di.DirectionRTL
	}

	// convert to fixed; properly handling large overflow
	const maxFixed = math.MaxInt32 >> 6 // max value for fixed.Int26_6
	mw := fixed.I(maxFixed)
	if maxWidth <= maxFixed {
		mw = floatToFixed(float32(maxWidth))
	}

	config := shaping.WrapConfig{
		Direction:                     dir,           // overall direction of the text
		BreakPolicy:                   shaping.Never, // mimic the default pango behavior
		DisableTrailingWhitespaceTrim: !style.SpaceCollapse(),
	}
	if allowWordBreak {
		config.BreakPolicy = shaping.Always
	}

	fc.lineWrapper.Prepare(config, text, shaping.NewSliceIterator(runs))
	line, done := fc.lineWrapper.WrapNextLineF(mw)
	return mw, line, done
}

// SplitFirstLine fits as much of text as possible in maxWidth, which is
// only a limit when limited is set. minimum is set when measuring the
// narrowest the text can be, isLineStart when the text starts its line.
//
// The line returned is the caller's: it may be modified, justified for
// instance.
func (fc *Engine) SplitFirstLine(hyphenCache map[HyphenDictKey]Hyphener, text []rune, style *Style,
	maxWidth float32, limited, minimum, isLineStart bool,
) FirstLine {
	out := fc.splitFirstLine(hyphenCache, text, fc.resolve(style), maxWidth, limited, minimum, isLineStart)
	// the layouts are kept in a cache, and the same one is returned for the
	// same text
	out.Layout = out.Layout.clone()
	return out
}

func (fc *Engine) splitFirstLine(hyphenCache map[HyphenDictKey]Hyphener, text []rune, style *Style,
	maxWidth float32, limited, minimum, isLineStart bool,
) FirstLine {
	// See https://www.w3.org/TR/css-text-3/#white-space-property
	var (
		textWrap         = style.TextWrap()
		originalMaxWidth = maxWidth
		fontSize         = style.Size
		firstLine        FirstLine
	)
	if !limited {
		originalMaxWidth = Inf
	}
	if !textWrap {
		limited = false
	}
	// Step #1: Get a draft layout with the first line
	if limited && maxWidth != Inf && fontSize != 0 {
		// Try to use a small amount of text instead of the whole text
		shortText := ShortTextHintRunes(text, maxWidth, fontSize)

		firstLine = fc.wrap(shortText, style, maxWidth)
		if firstLine.ResumeAt == -1 && len(shortText) != len(text) {
			// The small amount of text fits in one line, give up and use the whole text
			firstLine = fc.wrap(text, style, maxWidth)
		} else {
			// If the second line of the short text can break, we have the next
			// line break point required for step #3 in it, drop the end of the text.
			if firstLine.ResumeAt != -1 && firstLine.ResumeAt != len(shortText) {
				firstLineText := shortText[:firstLine.ResumeAt]
				start := len(firstLineText) + 1
				if fc.getNextBreakPoint(shortText[start:]) != -1 {
					text = shortText
				}
			}
		}
	} else {
		firstLine = fc.wrap(text, style, originalMaxWidth)
	}

	// Step #2: Don't split lines when it's not needed
	if !limited || len(text) == 0 {
		// The first line can take all the place needed
		return firstLine
	}
	maxWidthV := maxWidth

	if firstLine.ResumeAt == -1 && firstLine.Width <= maxWidthV {
		// The first line really fits in the available width
		return firstLine
	}

	// Step #3: Try to put the first word of the second line on the first line
	// https://mail.gnome.org/archives/gtk-i18n-list/2013-September/msg00006
	// is a good thread related to this problem.
	var firstLineText, secondLineText []rune
	if firstLine.Width <= maxWidthV {
		// the first line fits, but,
		//	since we wrap without using work breaks, the first word of the second line
		// 	could, after hyphenation, fit (partially) on the first line
		if firstLine.ResumeAt != -1 {
			firstLineText, secondLineText = text[:firstLine.ResumeAt], text[firstLine.ResumeAt:]
		}
	} else {
		// the first line is too long (only possible with one word)
		firstLineText = []rune{}
		secondLineText = text
	}

	// Fork: mirror Pango's firstLineFits handling — if the first line
	// doesn't fit and can't be broken earlier (no internal space after
	// trimming), treat the entire text as the second line so hyphenation
	// can be tried on the first (overflowing) word.
	if firstLine.Width > maxWidthV {
		trimmed := firstLineText
		for len(trimmed) > 0 && trimmed[len(trimmed)-1] == ' ' {
			trimmed = trimmed[:len(trimmed)-1]
		}
		for len(trimmed) > 0 && trimmed[0] == ' ' {
			trimmed = trimmed[1:]
		}
		hasInternalSpace := index(trimmed, ' ') != -1
		if !hasInternalSpace {
			firstLineText = nil
			secondLineText = text
		}
	}

	breakPoint := len(secondLineText)
	if len(secondLineText) != 0 {
		// Find the second line's first break point.
		breakPoint = fc.getNextBreakPoint(secondLineText)
		if breakPoint == -1 {
			breakPoint = len(secondLineText)
		}
	}

	nextWord := trimTrailingSpaces(secondLineText[:breakPoint])
	if len(nextWord) != 0 {
		if style.SpaceCollapse() && hasSuffix(secondLineText[:breakPoint], ' ') {
			// Next word might fit without a space afterwards; only try when
			// space collapsing is allowed.
			newFirstLineText := slices.Concat(firstLineText, nextWord)
			firstLine = fc.wrap(newFirstLineText, style, maxWidthV)
			if firstLine.ResumeAt == -1 {
				if len(firstLineText) != 0 {
					// The next word fits in the first line, keep the layout.
					return firstLine
				} else {
					// Second line is None.
					firstLine.ResumeAt = firstLine.Length + 1
					if firstLine.ResumeAt >= len(text) {
						firstLine.ResumeAt = -1
					}
				}
			}
		}
	} else if len(firstLineText) != 0 {
		// We found something on the first line but we did not find a word on
		// the next line, no need to hyphenate, we can keep the current layout.
		return firstLine
	}

	// Step #4: Try to hyphenate
	hyphens := style.Hyphens
	// the language whose rules hyphenate the language of the text, or ""
	lang := fc.hyphenation().Language(style.Lang)
	hyphenLimit := style.HyphenateLimitChars
	hyphenateCharacter := []rune(style.HyphenateCharacter)
	hyphenated := false
	const softHyphen = '\u00ad'

	autoHyphenation, manualHyphenation := false, false
	if hyphens != HNone {
		manualHyphenation = index(firstLineText, softHyphen) != -1 || index(nextWord, softHyphen) != -1
	}

	var startWord, stopWord, nextTextIndex int
	if hyphens == HAuto && lang != "" {
		// Get text until next line break opportunity.
		nextText := secondLineText
		if nextBreakPoint := fc.getNextBreakPoint(secondLineText); nextBreakPoint != -1 {
			nextText = nextText[:nextBreakPoint]
		}

		// Try all words included in this text.
		for len(nextText) != 0 {
			nextWordBoundaries := fc.wordBoundaries(nextText)
			if nextWordBoundaries != nil {
				// We have a word to hyphenate
				startWord, stopWord = nextWordBoundaries[0], nextWordBoundaries[1]
				nextWord = nextText[startWord:stopWord]
				if stopWord-startWord >= hyphenLimit.Total {
					// This word is long enough
					space := float32(maxWidthV - firstLine.Width)
					zone := style.HyphenateLimitZone
					limitZone := zone.Limit
					if zone.IsPercentage {
						limitZone = (float32(maxWidthV) * zone.Limit / 100.)
					}
					if space > limitZone || space < 0 {
						// Available space is worth the try, or the line is even too
						// long to fit: try to hyphenate
						autoHyphenation = true
						nextTextIndex += startWord
						break
					}
				}
				// This word doesn't qualify, advance to after this word and try the next.
				nextText = nextText[stopWord:]
				nextTextIndex += stopWord
			} else {
				break
			}
		}
	}

	// Automatic hyphenation opportunities within a word must be ignored if the
	// word contains a conditional hyphen, in favor of the conditional
	// hyphen(s).
	// See https://drafts.csswg.org/css-text-3/#valdef-hyphens-auto
	var dictionaryIterations []string
	if manualHyphenation {
		// Manual hyphenation: check that the line ends with a soft
		// hyphen and add the missing hyphen
		if hasSuffix(firstLineText, softHyphen) {
			// The first line has been split on a soft hyphen
			if i := lastIndex(firstLineText, ' '); i != -1 {
				firstLineText, nextWord = firstLineText[:i], firstLineText[i:] // next word start with a space
				firstLine = fc.wrap(firstLineText, style, maxWidthV)
				firstLine.ResumeAt = len(firstLineText) + 1 // track the space we have remove
			} else {
				firstLineText, nextWord = nil, firstLineText
			}
		}
		dictionaryIterations = HyphenDictionaryIterations(nextWord, softHyphen)
	} else if autoHyphenation {
		dictionaryKey := NewHyphenDictKey(lang, hyphenLimit)
		dictionary, ok := hyphenCache[dictionaryKey]
		if !ok {
			dictionary = fc.hyphenation().Hyphener(lang, hyphenLimit.Left, hyphenLimit.Right)
			hyphenCache[dictionaryKey] = dictionary
		}
		previousWords := secondLineText[:nextTextIndex]
		if dictionary != nil {
			dictionaryIterations = dictionary.IterateRunes(nextWord, string(previousWords))
		}
	}

	var hyphenatedFirstLineText []rune
	if len(dictionaryIterations) != 0 {
		var newFirstLineText []rune
		for _, firstWordPart := range dictionaryIterations {
			newFirstLineText = slices.Concat(firstLineText, []rune(firstWordPart))
			hyphenatedFirstLineText = append(newFirstLineText, hyphenateCharacter...)
			newFirstLine := fc.wrap(hyphenatedFirstLineText, style, maxWidthV)
			newSpace := maxWidthV - newFirstLine.Width
			hyphenated = newFirstLine.ResumeAt == -1 && (newSpace >= 0 || firstWordPart == dictionaryIterations[len(dictionaryIterations)-1])
			if hyphenated {
				firstLine = newFirstLine
				firstLine.ResumeAt = len(newFirstLineText)
				if firstLine.ResumeAt < len(text) && text[firstLine.ResumeAt] == softHyphen {
					// Fork: recreate the layout with no maxWidth to be
					// sure that we don't break before the soft hyphen.
					firstLine.Layout = fc.wrap(hyphenatedFirstLineText, style, Inf).Layout
					firstLine.ResumeAt += 1
				}
				break
			}
		}

		if !hyphenated && len(firstLineText) == 0 {
			// Recreate the layout with no maxWidth to be sure that
			// we don't break before or inside the hyphenate character
			hyphenated = true
			firstLine = fc.wrap(hyphenatedFirstLineText, style, Inf)
			// ResumeAt is the rune count of the unhyphenated portion in
			// the original text. Length should match after the
			// post-loop `Length -= len(hyphenateCharacter)` adjustment,
			// so set it to len(newFirstLineText)+len(hyphenateCharacter)
			// here.
			firstLine.Length = len(newFirstLineText) + len(hyphenateCharacter)
			firstLine.ResumeAt = len(newFirstLineText)
			if firstLine.ResumeAt < len(text) && text[firstLine.ResumeAt] == softHyphen {
				firstLine.ResumeAt += 1
			}
		}
	}

	if !hyphenated && hasSuffix(firstLineText, softHyphen) {
		// Recreate the layout with no maxWidth to be sure that
		// we don't break inside the hyphenate-character string
		hyphenated = true
		hyphenatedFirstLineText = slices.Concat(firstLineText, hyphenateCharacter)
		firstLine = fc.wrap(hyphenatedFirstLineText, style, Inf)
		firstLine.Length = len(firstLineText)
		firstLine.ResumeAt = len(firstLineText)
	}

	// Step #5: Try to break word if it's too long for the line
	overflowWrap, wordBreak := style.OverflowWrap, style.WordBreak
	space := maxWidthV - firstLine.Width
	// If we can break words and the first line is too long
	canBreak := wordBreak == WBBreakAll ||
		(isLineStart && (overflowWrap == OAnywhere || (overflowWrap == OBreakWord && !minimum)))
	if space < 0 && canBreak {
		// Is it really OK to remove hyphenation for word-break ?
		hyphenated = false
		firstLine = fc.wrapWordBreak(text, style, maxWidthV, true)
	}

	if hyphenated {
		firstLine.Length -= len(hyphenateCharacter)
	}

	return firstLine
}

// tabs support

func applyTabs(out *shaping.Output, text []rune, columnWidth, runStart fixed.Int26_6) {
	columnWidthF := float64(columnWidth) / 64
	var advance fixed.Int26_6
	for i, g := range out.Glyphs {
		gAdvance := g.Advance
		isTab := g.RunesCount() == 1 && g.GlyphsCount() == 1 && text[g.TextIndex()] == '\t'
		if !isTab {
			advance += gAdvance
			continue
		}

		var updatedTabAdvance fixed.Int26_6
		if columnWidth == 0 {
			// simply trim the advance, nothing else to do
		} else {
			// update the advance of the glyph so that the next glyph is "tab-aligned" :
			// we want the "end" of the tab to be a multiple of columnWidth, that is :
			// (runStart + advance + updatedTabAdvance) % columnWith == 0
			glyphStartF := float64(runStart+advance) / 64
			remainder := math.Mod(glyphStartF, columnWidthF)
			updatedTabAdvance = fixed.Int26_6((columnWidthF - remainder) * 64)
		}

		out.Glyphs[i].Advance = updatedTabAdvance

		advance += updatedTabAdvance
	}

	// no need to call RecomputeAdvance
	out.Advance = advance
}

// AlignTabs updates the advance of glyphs mapped to '\t' runes,
// so that tabs are aligned on columns defined by [columnWidth].
//
// [lineOffset] may be non zero if the line starts after the first column.
//
// As a special case, if [columnWidth] is zero,
// tabs are trimmed (their advance is set to 0).
func AlignTabs(l shaping.Line, text []rune, columnWidth, lineOffset fixed.Int26_6) {
	runsAdvance := lineOffset // the position of the start of the current run
	for i := range l {
		applyTabs(&l[i], text, columnWidth, runsAdvance)
		runsAdvance += l[i].Advance
	}
}

// capsVariant returns the capitals of the text engine for a value of
// font-variant-caps: those a font lacks are simulated, see [caps.Synthesize].
func capsVariant(v FontVariantCaps) caps.Variant {
	switch v {
	case FVC_SmallCaps:
		return caps.Small
	case FVC_AllSmallCaps:
		return caps.AllSmall
	case FVC_PetiteCaps:
		return caps.Petite
	case FVC_AllPetiteCaps:
		return caps.AllPetite
	case FVC_Unicase:
		return caps.Unicase
	case FVC_TitlingCaps:
		return caps.Titling
	}
	return caps.None
}
