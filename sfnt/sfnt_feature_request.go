package sfnt

import (
	"strconv"
	"strings"
)

// FeatureToEnd is the End of a feature request that applies to the rest of the
// run.
const FeatureToEnd = -1

// Feature is one entry of an OpenType feature request string: which feature is
// asked for, whether it is on or off, and over which part of the run.
//
// This describes what a caller has *requested*, which is a different question
// from what a font provides -- see SFNT.SupportsFeature for that. Both are needed
// at once to decide whether an effect has to be synthesized, and they live
// together here so a caller gets the two halves of that decision from one place.
// What to do about a mismatch is the caller's policy, not this package's.
type Feature struct {
	// Tag is the 4-character OpenType feature tag, such as "smcp".
	Tag FeatureTag
	// Value is 0 to disable the feature, 1 to enable it, or higher to select
	// an alternate.
	Value uint32
	// Start and End bound the runes the request applies to. End is
	// FeatureToEnd when the request runs to the end of the text.
	Start, End int
}

// WholeRun reports whether the request applies to the entire run rather than a
// sub-range of it.
func (f Feature) WholeRun() bool { return f.Start == 0 && f.End == FeatureToEnd }

// Features is a parsed feature request string.
type Features []Feature

// Enabled reports whether the requests turn tag on for the whole run. A later
// entry overrides an earlier one for the same tag, matching how shapers combine
// them, and a request bounded to a sub-range does not count: callers use this
// to decide run-wide behaviour, which a partial request cannot justify.
func (fs Features) Enabled(tag FeatureTag) bool {
	on := false
	for _, f := range fs {
		if f.Tag != tag {
			continue
		}
		on = f.Value != 0 && f.WholeRun()
	}
	return on
}

// ParseFeatures parses a comma-separated OpenType feature request string.
//
// It accepts the forms shapers accept, so that the same string can be handed to
// a shaper and inspected here without the two disagreeing:
//
//	smcp        +smcp       smcp=1      'smcp' 1     "smcp" 1     // on
//	-smcp       smcp=0      'smcp' 0                              // off
//	aalt=2      'aalt' 2                                          // alternate 2
//	smcp[3:5]   smcp[3:]    smcp[:5]    smcp[3]      smcp[]       // ranges
//
// Entries that cannot be parsed are skipped rather than reported: this runs on
// author-supplied CSS, where one malformed entry must not discard the rest.
func ParseFeatures(s string) Features {
	if s == "" {
		return nil
	}
	var out Features
	for _, part := range strings.Split(s, ",") {
		if f, ok := parseFeature(strings.TrimSpace(part)); ok {
			out = append(out, f)
		}
	}
	return out
}

func parseFeature(s string) (Feature, bool) {
	f := Feature{Value: 1, Start: 0, End: FeatureToEnd}
	if s == "" {
		return f, false
	}

	switch s[0] {
	case '+':
		s = s[1:]
	case '-':
		f.Value = 0
		s = s[1:]
	}

	// tag, optionally quoted
	if s == "" {
		return f, false
	}
	if q := s[0]; q == '\'' || q == '"' {
		end := strings.IndexByte(s[1:], q)
		if end < 0 {
			return f, false
		}
		f.Tag = FeatureTag(s[1 : 1+end])
		s = s[2+end:]
	} else {
		i := strings.IndexAny(s, "[= \t")
		if i < 0 {
			i = len(s)
		}
		f.Tag = FeatureTag(s[:i])
		s = s[i:]
	}
	if len(f.Tag) == 0 || 4 < len(f.Tag) {
		return f, false
	}

	// optional [start:end] range
	if strings.HasPrefix(s, "[") {
		end := strings.IndexByte(s, ']')
		if end < 0 {
			return f, false
		}
		if !parseFeatureRange(s[1:end], &f) {
			return f, false
		}
		s = s[end+1:]
	}

	// optional value, as "=N" or as whitespace followed by N
	s = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(s), "="))
	if s != "" {
		v, err := strconv.ParseUint(s, 10, 32)
		if err != nil {
			return f, false
		}
		f.Value = uint32(v)
	}
	return f, true
}

// parseFeatureRange parses the inside of a [..] range: "", ":", "3", "3:",
// ":5" or "3:5".
func parseFeatureRange(s string, f *Feature) bool {
	lo, hi, hasColon := strings.Cut(s, ":")
	lo, hi = strings.TrimSpace(lo), strings.TrimSpace(hi)
	if lo != "" {
		v, err := strconv.Atoi(lo)
		if err != nil || v < 0 {
			return false
		}
		f.Start = v
	}
	if !hasColon {
		// a single index applies to that one rune
		if lo != "" {
			f.End = f.Start + 1
		}
		return true
	}
	if hi != "" {
		v, err := strconv.Atoi(hi)
		if err != nil || v < 0 {
			return false
		}
		f.End = v
	}
	return true
}

// CapsVariant is a CSS font-variant-caps value, named here so callers do not
// each have to know which OpenType tags spell it.
//
// This says what is being asked for and, with SupportsCaps, whether the font
// can honour it. It deliberately says nothing about what to do when it cannot:
// synthesizing small capitals by scaling and re-casing is a renderer's policy,
// and depends on facilities this package does not have.
type CapsVariant int

const (
	CapsNormal CapsVariant = iota
	CapsSmall
	CapsAllSmall
	CapsPetite
	CapsAllPetite
	CapsUnicase
	CapsTitling
)

// Tags returns the OpenType features that express the variant. A variant is
// provided by a font only if it provides all of them.
func (c CapsVariant) Tags() []FeatureTag {
	switch c {
	case CapsSmall:
		return []FeatureTag{"smcp"}
	case CapsAllSmall:
		return []FeatureTag{"smcp", "c2sc"}
	case CapsPetite:
		return []FeatureTag{"pcap"}
	case CapsAllPetite:
		return []FeatureTag{"pcap", "c2pc"}
	case CapsUnicase:
		return []FeatureTag{"unic"}
	case CapsTitling:
		return []FeatureTag{"titl"}
	}
	return nil
}

func (c CapsVariant) String() string {
	switch c {
	case CapsSmall:
		return "small-caps"
	case CapsAllSmall:
		return "all-small-caps"
	case CapsPetite:
		return "petite-caps"
	case CapsAllPetite:
		return "all-petite-caps"
	case CapsUnicase:
		return "unicase"
	case CapsTitling:
		return "titling-caps"
	}
	return "normal"
}

// Caps reports which font-variant-caps value the request expresses.
//
// The all-* forms are recognised by their c2* tag, so they are tested first: a
// request carrying both smcp and c2sc is all-small-caps, not small-caps. When
// the request names none of them the result is CapsNormal.
func (fs Features) Caps() CapsVariant {
	for _, c := range []CapsVariant{CapsAllSmall, CapsAllPetite, CapsSmall, CapsPetite, CapsUnicase, CapsTitling} {
		enabled := true
		for _, tag := range c.Tags() {
			if !fs.Enabled(tag) {
				enabled = false
				break
			}
		}
		if enabled {
			return c
		}
	}
	return CapsNormal
}

// SupportsCaps reports whether the font provides every feature the variant
// needs. CapsNormal is always supported: it asks for nothing.
func (sfnt *SFNT) SupportsCaps(c CapsVariant) bool {
	for _, tag := range c.Tags() {
		if !sfnt.SupportsFeature(tag) {
			return false
		}
	}
	return true
}
