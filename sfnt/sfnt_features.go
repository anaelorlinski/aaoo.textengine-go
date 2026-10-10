package sfnt

import (
	"sort"

	"github.com/anaelorlinski/aaoo.textengine-go/internal/bin"
)

// featureTags holds the OpenType feature tags advertised by the font's GSUB and
// GPOS tables. It is populated at parse time by scanning each table's
// FeatureList, and is nil for a font with neither table.
//
// Only the FeatureList is read. The tags are all a capability query needs, and
// the full parseGPOSGSUB path additionally decodes every lookup subtable --
// which costs real time on every font load and rejects fonts that are otherwise
// perfectly usable. Keeping this scan separate is what lets the query be
// answered for every font rather than only for those whose lookups happen to
// parse.
type featureTags map[FeatureTag]struct{}

// SupportsFeature reports whether the font's GSUB or GPOS table advertises the
// given OpenType feature, such as "smcp" for small capitals or "liga" for
// standard ligatures.
//
// This asks what the font *provides*, which is a different question from which
// features a caller has *requested* of a shaper. A caller that wants small caps
// asks for "smcp" either way; SupportsFeature is how it finds out whether the
// font can honour that natively or whether the effect has to be synthesized.
//
// An unparseable or absent layout table reads as "not supported" rather than as
// an error: a font that advertises nothing and a font whose feature list cannot
// be read are indistinguishable to a caller, and neither can honour the feature.
func (sfnt *SFNT) SupportsFeature(tag FeatureTag) bool {
	if sfnt == nil || sfnt.featureTags == nil {
		return false
	}
	_, ok := sfnt.featureTags[tag]
	return ok
}

// FeatureTags returns the OpenType feature tags advertised by the font's GSUB
// and GPOS tables, sorted and deduplicated. A feature listed in both tables
// appears once.
func (sfnt *SFNT) FeatureTags() []FeatureTag {
	if sfnt == nil || len(sfnt.featureTags) == 0 {
		return nil
	}
	tags := make([]FeatureTag, 0, len(sfnt.featureTags))
	for tag := range sfnt.featureTags {
		tags = append(tags, tag)
	}
	sort.Slice(tags, func(i, j int) bool { return tags[i] < tags[j] })
	return tags
}

// parseFeatureTags scans the FeatureList of the named layout table ("GSUB" or
// "GPOS") and records the tags it advertises. Malformed input is skipped
// silently: this runs for every font at load time, and a layout table that
// cannot be read must not prevent the font from being used for everything else.
func (sfnt *SFNT) parseFeatureTags(name string) {
	b, ok := sfnt.Tables[name]
	if !ok || len(b) < 10 {
		return
	}

	// header: majorVersion, minorVersion, scriptListOffset, featureListOffset,
	// lookupListOffset. The version is not checked -- a major version other
	// than 1 is not defined by the spec, but the FeatureList offset lives at a
	// fixed position regardless, and refusing to read it would only lose
	// features for a font that other implementations still read.
	r := bin.NewBinaryReaderBytes(b)
	_ = r.ReadUint16() // majorVersion
	_ = r.ReadUint16() // minorVersion
	_ = r.ReadUint16() // scriptListOffset
	featureListOffset := int(r.ReadUint16())
	if featureListOffset == 0 || len(b) < featureListOffset+2 {
		return
	}

	f := bin.NewBinaryReaderBytes(b[featureListOffset:])
	featureCount := int(f.ReadUint16())
	// each FeatureRecord is a 4-byte tag plus a 2-byte offset
	if len(b)-featureListOffset-2 < featureCount*6 {
		return
	}
	for i := 0; i < featureCount; i++ {
		tag := FeatureTag(f.ReadString(4))
		_ = f.ReadUint16() // featureOffset, not needed to answer the query
		if len(tag) != 4 {
			return // short read; the reader is exhausted
		}
		if sfnt.featureTags == nil {
			sfnt.featureTags = featureTags{}
		}
		sfnt.featureTags[tag] = struct{}{}
	}
}
