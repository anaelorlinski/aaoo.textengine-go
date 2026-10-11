// Package layout lays text out on lines: it finds the fonts of a text,
// shapes it, and breaks it where a line ends.
//
// It knows nothing of CSS: a [Style] is plain values, which the caller
// fills from whatever describes its text.
package layout

import (
	"bytes"
	"encoding/binary"
	"math"
	"sort"
)

// Inf is the width of a line with no limit.
var Inf = float32(math.Inf(+1))

// WhiteSpace is how the white space of a text is handled, as the CSS
// property white-space says.
type WhiteSpace uint8

const (
	WhiteSpaceNormal WhiteSpace = iota
	WhiteSpaceNowrap
	WhiteSpacePre
	WhiteSpacePreWrap
	WhiteSpacePreLine
	// WhiteSpaceBreakSpaces neither wraps nor collapses here: it is not
	// supported.
	WhiteSpaceBreakSpaces
)

// Direction is the overall direction of a text.
type Direction uint8

const (
	LTR Direction = iota
	RTL
)

// Limits is the fewest characters hyphenation leaves: in the word, before
// a break and after it. A field is [LimitAuto] when the choice is left to
// the hyphenation dictionary.
type Limits struct {
	Total, Left, Right int
}

// LimitAuto is the field of a [Limits] left to the implementation.
const LimitAuto = -1

type LineMetrics struct {
	// Distance from the baseline to the logical top of a line of text.
	// (The logical top may be above or below the top of the
	// actual drawn ink. It is necessary to lay out the text to figure
	// where the ink will be.)
	Ascent float32

	// Distance above the baseline of the top of the underline.
	// Since most fonts have underline positions beneath the baseline, this value is typically negative.
	UnderlinePosition float32

	// Suggested thickness to draw for the underline.
	UnderlineThickness float32

	// Distance above the baseline of the top of the strikethrough.
	StrikethroughPosition float32
	// Suggested thickness to draw for the strikethrough.
	StrikethroughThickness float32
}

// FontDescription stores the settings influencing
// font resolution and metrics.
type FontDescription struct {
	Family            []string
	Style             FontStyle
	Stretch           FontStretch
	Weight            uint16
	Size              float32
	VariationSettings []Variation // empty for 'normal'
	VariantCaps       FontVariantCaps
}

// Binary appends a serialization of the description to dst.
func (fd FontDescription) Binary(dst []byte, includeSize bool) []byte {
	for _, f := range fd.Family {
		dst = append(dst, f...)
	}
	dst = append(dst, byte(fd.Style), byte(fd.Stretch), byte(fd.VariantCaps))
	dst = binary.BigEndian.AppendUint16(dst, fd.Weight)
	if includeSize {
		dst = binary.BigEndian.AppendUint32(dst, math.Float32bits(float32(fd.Size)))
	}
	for _, v := range fd.VariationSettings {
		dst = append(dst, v.Tag[:]...)
		dst = binary.BigEndian.AppendUint32(dst, math.Float32bits(float32(v.Value)))
	}
	return dst
}

// TextWrap returns true if the "white-space" property allows wrapping
func (ts *Style) TextWrap() bool {
	ws := ts.WhiteSpace
	return ws == WhiteSpaceNormal || ws == WhiteSpacePreWrap || ws == WhiteSpacePreLine
}

// SpaceCollapse returns true if the "white-space" property collapses spaces
func (ts *Style) SpaceCollapse() bool {
	ws := ts.WhiteSpace
	return ws == WhiteSpaceNormal || ws == WhiteSpaceNowrap || ws == WhiteSpacePreLine
}

// preservesNewlines returns true if the "white-space" property makes a
// newline a forced line break
func (ts *Style) preservesNewlines() bool {
	ws := ts.WhiteSpace
	return ws == WhiteSpacePre || ws == WhiteSpacePreWrap || ws == WhiteSpacePreLine
}

// Style is what the layout of a text depends on, besides the text.
// The zero value lays out left to right, with the white space rules of
// `white-space: normal`.
type Style struct {
	FontDescription

	// Decorated is set when the text is underlined, overlined or struck:
	// the metrics of these lines are then computed, see [Line.Metrics].
	Decorated bool

	// FontFeatures stores the resolved value
	// for all the CSS properties related, sorted by tag :
	// 	"font-kerning"
	// 	"font-variant-ligatures"
	// 	"font-variant-position"
	// 	"font-variant-caps"
	// 	"font-variant-numeric"
	// 	"font-variant-alternates"
	// 	"font-variant-east-asian"
	// 	"font-feature-settings"
	FontFeatures []Feature

	FontLanguageOverride FontLanguageOverride
	Lang                 string

	WhiteSpace   WhiteSpace
	OverflowWrap OverflowWrap
	WordBreak    WordBreak

	Hyphens             Hyphens
	HyphenateCharacter  string
	HyphenateLimitChars Limits
	HyphenateLimitZone  HyphenateZone

	WordSpacing   float32
	LetterSpacing float32 // 0 for 'normal'
	TabSize       TabSize

	// overall direction of the text
	Direction Direction
}

type StyleKey struct {
	FontDescription string // serialized
	FontFeatures    string // serialized

	// Decorated is set when the text is underlined, overlined or struck:
	// the metrics of these lines are then computed, see [Line.Metrics].
	Decorated bool

	FontLanguageOverride FontLanguageOverride
	Lang                 string

	WhiteSpace   WhiteSpace
	OverflowWrap OverflowWrap
	WordBreak    WordBreak

	Hyphens             Hyphens
	HyphenateCharacter  string
	HyphenateLimitChars Limits
	HyphenateLimitZone  HyphenateZone

	WordSpacing   float32
	LetterSpacing float32 // 0 for 'normal'
	TabSize       TabSize

	Direction Direction
}

func (ts *Style) Key() StyleKey {
	return StyleKey{
		string(ts.FontDescription.Binary(nil, true)),
		string(FeaturesBinary(ts.FontFeatures)),
		ts.Decorated,
		ts.FontLanguageOverride,
		ts.Lang,
		ts.WhiteSpace,
		ts.OverflowWrap,
		ts.WordBreak,
		ts.Hyphens,
		ts.HyphenateCharacter,
		ts.HyphenateLimitChars,
		ts.HyphenateLimitZone,
		ts.WordSpacing,
		ts.LetterSpacing,
		ts.TabSize,
		ts.Direction,
	}
}

type TabSize struct {
	Width      float32
	IsMultiple bool // true to use Width * <space character width>
}

// Feature is an OpenType feature and its value.
type Feature struct {
	Tag   [4]byte
	Value uint32
}

func FeaturesBinary(ls []Feature) []byte {
	out := make([]byte, len(ls)*8)
	for i, v := range ls {
		out[8*i+0] = v.Tag[0]
		out[8*i+1] = v.Tag[1]
		out[8*i+2] = v.Tag[2]
		out[8*i+3] = v.Tag[3]
		binary.BigEndian.PutUint32(out[8*i+4:], v.Value)
	}
	return out
}

// FeatureSet is a set of features, by tag.
type FeatureSet map[[4]byte]uint32

func NewFeatureSet(fs []Feature) FeatureSet {
	out := make(FeatureSet)
	for _, f := range fs {
		out[f.Tag] = f.Value
	}
	return out
}

// other is applied on top of fs
func (fs FeatureSet) Merge(other []Feature) {
	for _, f := range other {
		fs[f.Tag] = f.Value
	}
}

func (fs FeatureSet) List() []Feature {
	out := make([]Feature, 0, len(fs))
	for k, v := range fs {
		out = append(out, Feature{Tag: k, Value: v})
	}
	sort.Slice(out, func(i, j int) bool { return bytes.Compare(out[i].Tag[:], out[j].Tag[:]) == -1 })
	return out
}

type WordBreak uint8

const (
	WBNormal WordBreak = iota
	WBBreakAll
)

type OverflowWrap uint8

const (
	ONormal OverflowWrap = iota
	OAnywhere
	OBreakWord
)

type Hyphens uint8

const (
	HManual Hyphens = iota
	HNone
	HAuto
)

type HyphenateZone struct {
	Limit        float32
	IsPercentage bool
}

type FontStyle uint8

const (
	FSty_Normal FontStyle = iota
	FSty_Oblique
	FSty_Italic
)

type FontStretch uint8

const (
	FStr_UltraCondensed FontStretch = iota // ultra condensed width
	FStr_ExtraCondensed                    // extra condensed width
	FStr_Condensed                         // condensed width
	FStr_SemiCondensed                     // semi condensed width
	FStr_Normal                            // the normal width
	FStr_SemiExpanded                      // semi expanded width
	FStr_Expanded                          // expanded width
	FStr_ExtraExpanded                     // extra expanded width
	FStr_UltraExpanded                     // ultra expanded width
)

type FontVariantCaps uint8

const (
	FVC_Normal FontVariantCaps = iota
	FVC_SmallCaps
	FVC_AllSmallCaps
	FVC_PetiteCaps
	FVC_AllPetiteCaps
	FVC_Unicase
	FVC_TitlingCaps
)

type Variation struct {
	Tag   [4]byte
	Value float32
}

// FontLanguageOverride is either 'normal' (coded as the zero value)
// or a 4 byte tag, normalized to lower case
type FontLanguageOverride [4]byte

// Language system tags
// From https://docs.microsoft.com/typography/opentype/spec/languagetags
var LstToISO = map[FontLanguageOverride]string{
	{'a', 'b', 'a'}:      "abq",
	{'a', 'f', 'k'}:      "afr",
	{'a', 'f', 'r'}:      "aar",
	{'a', 'g', 'w'}:      "ahg",
	{'a', 'l', 's'}:      "gsw",
	{'a', 'l', 't'}:      "atv",
	{'a', 'r', 'i'}:      "aiw",
	{'a', 'r', 'k'}:      "mhv",
	{'a', 't', 'h'}:      "apk",
	{'a', 'v', 'r'}:      "ava",
	{'b', 'a', 'd'}:      "bfq",
	{'b', 'a', 'd', '0'}: "bad",
	{'b', 'a', 'g'}:      "bfy",
	{'b', 'a', 'l'}:      "krc",
	{'b', 'a', 'u'}:      "bci",
	{'b', 'c', 'h'}:      "bcq",
	{'b', 'g', 'r'}:      "bul",
	{'b', 'i', 'l'}:      "byn",
	{'b', 'k', 'f'}:      "bla",
	{'b', 'l', 'i'}:      "bal",
	{'b', 'l', 'n'}:      "bjt",
	{'b', 'l', 't'}:      "bft",
	{'b', 'm', 'b'}:      "bam",
	{'b', 'r', 'i'}:      "bra",
	{'b', 'r', 'm'}:      "mya",
	{'b', 's', 'h'}:      "bak",
	{'b', 't', 'i'}:      "btb",
	{'c', 'h', 'g'}:      "sgw",
	{'c', 'h', 'h'}:      "hne",
	{'c', 'h', 'i'}:      "nya",
	{'c', 'h', 'k'}:      "ckt",
	{'c', 'h', 'k', '0'}: "chk",
	{'c', 'h', 'u'}:      "chv",
	{'c', 'h', 'y'}:      "chy",
	{'c', 'm', 'r'}:      "swb",
	{'c', 'r', 'r'}:      "crx",
	{'c', 'r', 't'}:      "crh",
	{'c', 's', 'l'}:      "chu",
	{'c', 's', 'y'}:      "ces",
	{'d', 'c', 'r'}:      "cwd",
	{'d', 'g', 'r'}:      "doi",
	{'d', 'j', 'r'}:      "dje",
	{'d', 'j', 'r', '0'}: "djr",
	{'d', 'n', 'g'}:      "ada",
	{'d', 'n', 'k'}:      "din",
	{'d', 'r', 'i'}:      "prs",
	{'d', 'u', 'n'}:      "dng",
	{'d', 'z', 'n'}:      "dzo",
	{'e', 'b', 'i'}:      "igb",
	{'e', 'c', 'r'}:      "crj",
	{'e', 'd', 'o'}:      "bin",
	{'e', 'r', 'z'}:      "myv",
	{'e', 's', 'p'}:      "spa",
	{'e', 't', 'i'}:      "est",
	{'e', 'u', 'q'}:      "eus",
	{'e', 'v', 'k'}:      "evn",
	{'e', 'v', 'n'}:      "eve",
	{'f', 'a', 'n'}:      "acf",
	{'f', 'a', 'n', '0'}: "fan",
	{'f', 'a', 'r'}:      "fas",
	{'f', 'j', 'i'}:      "fij",
	{'f', 'l', 'e'}:      "vls",
	{'f', 'n', 'e'}:      "enf",
	{'f', 'o', 's'}:      "fao",
	{'f', 'r', 'i'}:      "fry",
	{'f', 'r', 'l'}:      "fur",
	{'f', 'r', 'p'}:      "frp",
	{'f', 't', 'a'}:      "fuf",
	{'g', 'a', 'd'}:      "gaa",
	{'g', 'a', 'e'}:      "gla",
	{'g', 'a', 'l'}:      "glg",
	{'g', 'a', 'w'}:      "gbm",
	{'g', 'i', 'l'}:      "niv",
	{'g', 'i', 'l', '0'}: "gil",
	{'g', 'm', 'z'}:      "guk",
	{'g', 'r', 'n'}:      "kal",
	{'g', 'r', 'o'}:      "grt",
	{'g', 'u', 'a'}:      "grn",
	{'h', 'a', 'i'}:      "hat",
	{'h', 'a', 'l'}:      "flm",
	{'h', 'a', 'r'}:      "hoj",
	{'h', 'b', 'n'}:      "amf",
	{'h', 'm', 'a'}:      "mrj",
	{'h', 'n', 'd'}:      "hno",
	{'h', 'o'}:           "hoc",
	{'h', 'r', 'i'}:      "har",
	{'h', 'y', 'e', '0'}: "hye",
	{'i', 'j', 'o'}:      "ijc",
	{'i', 'n', 'g'}:      "inh",
	{'i', 'n', 'u'}:      "iku",
	{'i', 'r', 'i'}:      "gle",
	{'i', 'r', 't'}:      "gle",
	{'i', 's', 'm'}:      "smn",
	{'i', 'w', 'r'}:      "heb",
	{'j', 'a', 'n'}:      "jpn",
	{'j', 'i', 'i'}:      "yid",
	{'j', 'u', 'd'}:      "lad",
	{'j', 'u', 'l'}:      "dyu",
	{'k', 'a', 'b'}:      "kbd",
	{'k', 'a', 'b', '0'}: "kab",
	{'k', 'a', 'c'}:      "kfr",
	{'k', 'a', 'l'}:      "kln",
	{'k', 'a', 'r'}:      "krc",
	{'k', 'e', 'b'}:      "ktb",
	{'k', 'g', 'e'}:      "kat",
	{'k', 'h', 'a'}:      "kjh",
	{'k', 'h', 'k'}:      "kca",
	{'k', 'h', 's'}:      "kca",
	{'k', 'h', 'v'}:      "kca",
	{'k', 'i', 's'}:      "kqs",
	{'k', 'k', 'n'}:      "kex",
	{'k', 'l', 'm'}:      "xal",
	{'k', 'm', 'b'}:      "kam",
	{'k', 'm', 'n'}:      "kfy",
	{'k', 'm', 'o'}:      "kmw",
	{'k', 'm', 's'}:      "kxc",
	{'k', 'n', 'r'}:      "kau",
	{'k', 'o', 'd'}:      "kfa",
	{'k', 'o', 'h'}:      "okm",
	{'k', 'o', 'n'}:      "ktu",
	{'k', 'o', 'n', '0'}: "kon",
	{'k', 'o', 'p'}:      "koi",
	{'k', 'o', 'z'}:      "kpv",
	{'k', 'p', 'l'}:      "kpe",
	{'k', 'r', 'k'}:      "kaa",
	{'k', 'r', 'm'}:      "kdr",
	{'k', 'r', 'n'}:      "kar",
	{'k', 'r', 't'}:      "kqy",
	{'k', 's', 'h'}:      "kas",
	{'k', 's', 'h', '0'}: "ksh",
	{'k', 's', 'i'}:      "kha",
	{'k', 's', 'm'}:      "sjd",
	{'k', 'u', 'i'}:      "kxu",
	{'k', 'u', 'l'}:      "kfx",
	{'k', 'u', 'u'}:      "kru",
	{'k', 'u', 'y'}:      "kdt",
	{'k', 'y', 'k'}:      "kpy",
	{'l', 'a', 'd'}:      "lld",
	{'l', 'a', 'h'}:      "bfu",
	{'l', 'a', 'k'}:      "lbe",
	{'l', 'a', 'm'}:      "lmn",
	{'l', 'a', 'z'}:      "lzz",
	{'l', 'c', 'r'}:      "crm",
	{'l', 'd', 'k'}:      "lbj",
	{'l', 'm', 'a'}:      "mhr",
	{'l', 'm', 'b'}:      "lif",
	{'l', 'm', 'w'}:      "ngl",
	{'l', 's', 'b'}:      "dsb",
	{'l', 's', 'm'}:      "smj",
	{'l', 't', 'h'}:      "lit",
	{'l', 'u', 'h'}:      "luy",
	{'l', 'v', 'i'}:      "lav",
	{'m', 'a', 'j'}:      "mpe",
	{'m', 'a', 'k'}:      "vmw",
	{'m', 'a', 'n'}:      "mns",
	{'m', 'a', 'p'}:      "arn",
	{'m', 'a', 'w'}:      "mwr",
	{'m', 'b', 'n'}:      "kmb",
	{'m', 'c', 'h'}:      "mnc",
	{'m', 'c', 'r'}:      "crm",
	{'m', 'd', 'e'}:      "men",
	{'m', 'e', 'n'}:      "mym",
	{'m', 'i', 'z'}:      "lus",
	{'m', 'k', 'r'}:      "mak",
	{'m', 'l', 'e'}:      "mdy",
	{'m', 'l', 'n'}:      "mlq",
	{'m', 'l', 'r'}:      "mal",
	{'m', 'l', 'y'}:      "msa",
	{'m', 'n', 'd'}:      "mnk",
	{'m', 'n', 'g'}:      "mon",
	{'m', 'n', 'k'}:      "man",
	{'m', 'n', 'x'}:      "glv",
	{'m', 'o', 'k'}:      "mdf",
	{'m', 'o', 'n'}:      "mnw",
	{'m', 't', 'h'}:      "mai",
	{'m', 't', 's'}:      "mlt",
	{'m', 'u', 'n'}:      "unr",
	{'n', 'a', 'n'}:      "gld",
	{'n', 'a', 's'}:      "nsk",
	{'n', 'c', 'r'}:      "csw",
	{'n', 'd', 'g'}:      "ndo",
	{'n', 'h', 'c'}:      "csw",
	{'n', 'i', 's'}:      "dap",
	{'n', 'k', 'l'}:      "nyn",
	{'n', 'k', 'o'}:      "nqo",
	{'n', 'o', 'r'}:      "nob",
	{'n', 's', 'm'}:      "sme",
	{'n', 't', 'a'}:      "nod",
	{'n', 't', 'o'}:      "epo",
	{'n', 'y', 'n'}:      "nno",
	{'o', 'c', 'r'}:      "ojs",
	{'o', 'j', 'b'}:      "oji",
	{'o', 'r', 'o'}:      "orm",
	{'p', 'a', 'a'}:      "sam",
	{'p', 'a', 'l'}:      "pli",
	{'p', 'a', 'p'}:      "plp",
	{'p', 'a', 'p', '0'}: "pap",
	{'p', 'a', 's'}:      "pus",
	{'p', 'g', 'r'}:      "ell",
	{'p', 'i', 'l'}:      "fil",
	{'p', 'l', 'g'}:      "pce",
	{'p', 'l', 'k'}:      "pol",
	{'p', 't', 'g'}:      "por",
	{'q', 'i', 'n'}:      "bgr",
	{'r', 'b', 'u'}:      "bxr",
	{'r', 'c', 'r'}:      "atj",
	{'r', 'm', 's'}:      "roh",
	{'r', 'o', 'm'}:      "ron",
	{'r', 'o', 'y'}:      "rom",
	{'r', 's', 'y'}:      "rue",
	{'r', 'u', 'a'}:      "kin",
	{'s', 'a', 'd'}:      "sck",
	{'s', 'a', 'y'}:      "chp",
	{'s', 'e', 'k'}:      "xan",
	{'s', 'e', 'l'}:      "sel",
	{'s', 'g', 'o'}:      "sag",
	{'s', 'g', 's'}:      "sgs",
	{'s', 'i', 'b'}:      "sjo",
	{'s', 'i', 'g'}:      "xst",
	{'s', 'k', 's'}:      "sms",
	{'s', 'k', 'y'}:      "slk",
	{'s', 'l', 'a'}:      "scs",
	{'s', 'm', 'l'}:      "som",
	{'s', 'n', 'a'}:      "seh",
	{'s', 'n', 'a', '0'}: "sna",
	{'s', 'n', 'h'}:      "sin",
	{'s', 'o', 'g'}:      "gru",
	{'s', 'r', 'b'}:      "srp",
	{'s', 's', 'l'}:      "xsl",
	{'s', 's', 'm'}:      "sma",
	{'s', 'u', 'r'}:      "suq",
	{'s', 'v', 'e'}:      "swe",
	{'s', 'w', 'a'}:      "aii",
	{'s', 'w', 'k'}:      "swa",
	{'s', 'w', 'z'}:      "ssw",
	{'s', 'x', 't'}:      "ngo",
	{'t', 'a', 'j'}:      "tgk",
	{'t', 'c', 'r'}:      "cwd",
	{'t', 'g', 'n'}:      "ton",
	{'t', 'g', 'r'}:      "tig",
	{'t', 'g', 'y'}:      "tir",
	{'t', 'h', 't'}:      "tah",
	{'t', 'i', 'b'}:      "bod",
	{'t', 'k', 'm'}:      "tuk",
	{'t', 'm', 'n'}:      "tem",
	{'t', 'n', 'a'}:      "tsn",
	{'t', 'n', 'e'}:      "enh",
	{'t', 'n', 'g'}:      "toi",
	{'t', 'o', 'd'}:      "xal",
	{'t', 'o', 'd', '0'}: "tod",
	{'t', 'r', 'k'}:      "tur",
	{'t', 's', 'g'}:      "tso",
	{'t', 'u', 'a'}:      "tru",
	{'t', 'u', 'l'}:      "tcy",
	{'t', 'u', 'v'}:      "tyv",
	{'t', 'w', 'i'}:      "aka",
	{'u', 's', 'b'}:      "hsb",
	{'u', 'y', 'g'}:      "uig",
	{'v', 'i', 't'}:      "vie",
	{'v', 'r', 'o'}:      "vro",
	{'w', 'a'}:           "wbm",
	{'w', 'a', 'g'}:      "wbr",
	{'w', 'c', 'r'}:      "crk",
	{'w', 'e', 'l'}:      "cym",
	{'w', 'l', 'f'}:      "wol",
	{'x', 'b', 'd'}:      "khb",
	{'x', 'h', 's'}:      "xho",
	{'y', 'a', 'k'}:      "sah",
	{'y', 'b', 'a'}:      "yor",
	{'y', 'c', 'r'}:      "cre",
	{'y', 'i', 'm'}:      "iii",
	{'z', 'h', 'h'}:      "zho",
	{'z', 'h', 'p'}:      "zho",
	{'z', 'h', 's'}:      "zho",
	{'z', 'h', 't'}:      "zho",
	{'z', 'n', 'd'}:      "zne",
}

// DefaultStyle returns the style of a text nothing is said about: a serif
// font of 16 pixels, regular and upright, and the initial values of CSS for
// the rest. The zero [Style] differs: it has no family, no size, and the
// narrowest stretch.
func DefaultStyle() Style {
	return Style{
		FontDescription: FontDescription{
			Family:  []string{"serif"},
			Style:   FSty_Normal,
			Stretch: FStr_Normal,
			Weight:  400,
			Size:    16,
		},
		HyphenateCharacter:  "‐",
		HyphenateLimitChars: Limits{Total: 5, Left: LimitAuto, Right: LimitAuto},
		TabSize:             TabSize{Width: 8, IsMultiple: true},
	}
}
