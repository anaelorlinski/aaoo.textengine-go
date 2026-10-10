package sfnt

import (
	"os"
	"reflect"
	"testing"
)

func TestParseFeatures(t *testing.T) {
	for _, tt := range []struct {
		in   string
		want Features
	}{
		{"", nil},
		{"smcp", Features{{Tag: "smcp", Value: 1, End: FeatureToEnd}}},
		{"+smcp", Features{{Tag: "smcp", Value: 1, End: FeatureToEnd}}},
		{"-smcp", Features{{Tag: "smcp", Value: 0, End: FeatureToEnd}}},
		{"smcp=1", Features{{Tag: "smcp", Value: 1, End: FeatureToEnd}}},
		{"smcp=0", Features{{Tag: "smcp", Value: 0, End: FeatureToEnd}}},
		{"aalt=2", Features{{Tag: "aalt", Value: 2, End: FeatureToEnd}}},
		// the CSS form a caller building font-feature-settings emits
		{"'smcp' 1", Features{{Tag: "smcp", Value: 1, End: FeatureToEnd}}},
		{"'smcp' 0", Features{{Tag: "smcp", Value: 0, End: FeatureToEnd}}},
		{`"smcp" 1`, Features{{Tag: "smcp", Value: 1, End: FeatureToEnd}}},
		{"'aalt' 2", Features{{Tag: "aalt", Value: 2, End: FeatureToEnd}}},
		// ranges
		{"smcp[3:5]", Features{{Tag: "smcp", Value: 1, Start: 3, End: 5}}},
		{"smcp[3:]", Features{{Tag: "smcp", Value: 1, Start: 3, End: FeatureToEnd}}},
		{"smcp[:5]", Features{{Tag: "smcp", Value: 1, Start: 0, End: 5}}},
		{"smcp[3]", Features{{Tag: "smcp", Value: 1, Start: 3, End: 4}}},
		{"smcp[]", Features{{Tag: "smcp", Value: 1, End: FeatureToEnd}}},
		{"smcp[:]", Features{{Tag: "smcp", Value: 1, End: FeatureToEnd}}},
		// several, with the spacing a joined CSS list produces
		{"'smcp' 1, 'c2sc' 0", Features{
			{Tag: "smcp", Value: 1, End: FeatureToEnd},
			{Tag: "c2sc", Value: 0, End: FeatureToEnd},
		}},
		{"kern,liga", Features{
			{Tag: "kern", Value: 1, End: FeatureToEnd},
			{Tag: "liga", Value: 1, End: FeatureToEnd},
		}},
		// a malformed entry is skipped, the rest survive
		{"'smcp, liga", Features{{Tag: "liga", Value: 1, End: FeatureToEnd}}},
		{"toolongtag, liga", Features{{Tag: "liga", Value: 1, End: FeatureToEnd}}},
		{"smcp=x, liga", Features{{Tag: "liga", Value: 1, End: FeatureToEnd}}},
	} {
		got := ParseFeatures(tt.in)
		if !reflect.DeepEqual(got, tt.want) {
			t.Errorf("ParseFeatures(%q)\n got %+v\nwant %+v", tt.in, got, tt.want)
		}
	}
}

func TestFeaturesEnabled(t *testing.T) {
	for _, tt := range []struct {
		in   string
		tag  FeatureTag
		want bool
	}{
		{"smcp", "smcp", true},
		{"'smcp' 1", "smcp", true},
		// the case that motivated this: a disabled feature must not read as
		// requested, which a substring match on the raw string gets wrong
		{"-smcp", "smcp", false},
		{"smcp=0", "smcp", false},
		{"'smcp' 0", "smcp", false},
		// a tag that is merely absent
		{"'liga' 1", "smcp", false},
		{"", "smcp", false},
		// last entry wins, as shapers combine them
		{"smcp, -smcp", "smcp", false},
		{"-smcp, smcp", "smcp", true},
		// a sub-range request is not a run-wide one
		{"smcp[3:5]", "smcp", false},
		{"smcp[3:5], smcp", "smcp", true},
		// tags must match whole, not as substrings of one another
		{"'c2sc' 1", "smcp", false},
	} {
		if got := ParseFeatures(tt.in).Enabled(tt.tag); got != tt.want {
			t.Errorf("ParseFeatures(%q).Enabled(%q) = %v, want %v", tt.in, tt.tag, got, tt.want)
		}
	}
}

func TestFeaturesCaps(t *testing.T) {
	for _, tt := range []struct {
		in   string
		want CapsVariant
	}{
		{"", CapsNormal},
		{"'liga' 1", CapsNormal},
		{"smcp", CapsSmall},
		{"'smcp' 1", CapsSmall},
		{"pcap", CapsPetite},
		{"unic", CapsUnicase},
		{"titl", CapsTitling},
		// the all-* forms need both tags and must win over the lower-only form
		{"smcp, c2sc", CapsAllSmall},
		{"'smcp' 1, 'c2sc' 1", CapsAllSmall},
		{"pcap, c2pc", CapsAllPetite},
		// c2sc alone is not all-small-caps: the variant needs smcp too
		{"c2sc", CapsNormal},
		{"c2pc", CapsNormal},
		// a disabled request is not a request
		{"'smcp' 0", CapsNormal},
		{"-smcp", CapsNormal},
		{"smcp, -c2sc", CapsSmall},
		{"smcp, c2sc, -smcp", CapsNormal},
	} {
		if got := ParseFeatures(tt.in).Caps(); got != tt.want {
			t.Errorf("ParseFeatures(%q).Caps() = %v, want %v", tt.in, got, tt.want)
		}
	}
}

func TestCapsVariantTags(t *testing.T) {
	if got := CapsNormal.Tags(); got != nil {
		t.Errorf("CapsNormal.Tags() = %v, want nil", got)
	}
	if got := CapsAllSmall.Tags(); len(got) != 2 || got[0] != "smcp" || got[1] != "c2sc" {
		t.Errorf("CapsAllSmall.Tags() = %v", got)
	}
	if CapsSmall.String() != "small-caps" || CapsNormal.String() != "normal" {
		t.Error("CapsVariant.String is wrong")
	}
}

func TestSupportsCaps(t *testing.T) {
	b, err := os.ReadFile("resources/EBGaramond12-Regular.otf")
	if err != nil {
		t.Skip(err)
	}
	garamond, err := ParseSFNT(b, 0)
	if err != nil {
		t.Fatal(err)
	}
	b, err = os.ReadFile("resources/DejaVuSerif.ttf")
	if err != nil {
		t.Skip(err)
	}
	dejavu, err := ParseSFNT(b, 0)
	if err != nil {
		t.Fatal(err)
	}

	// EBGaramond carries smcp and c2sc, DejaVuSerif carries neither
	if !garamond.SupportsCaps(CapsSmall) || !garamond.SupportsCaps(CapsAllSmall) {
		t.Error("EBGaramond should support small-caps and all-small-caps")
	}
	if dejavu.SupportsCaps(CapsSmall) || dejavu.SupportsCaps(CapsAllSmall) {
		t.Error("DejaVuSerif should support neither")
	}
	// CapsNormal asks for nothing, so every font supports it
	if !dejavu.SupportsCaps(CapsNormal) {
		t.Error("CapsNormal should always be supported")
	}
}
