package face

import (
	"testing"

	"github.com/anaelorlinski/aaoo.textengine-go/sfnt"
)

// The Microsoft rule must give what the sfnt package gives: the documents
// laid out with it must keep their line heights.
func TestVerticalMicrosoftRuleMatchesSFNT(t *testing.T) {
	for _, name := range []string{"DejaVuSerif.ttf", "EBGaramond12-Regular.otf", "DejaVuSerif.woff2"} {
		data := read(t, name)
		v, err := ReadVertical(data, 0)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		plain, err := sfnt.ToSFNT(data)
		if err != nil {
			t.Fatal(err)
		}
		parsed, err := sfnt.ParseSFNT(plain, 0)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		wa, wd, wg := parsed.VerticalMetrics()
		a, d, g := v.Resolve(MicrosoftRule)
		if a != float32(wa) || d != float32(wd) || g != float32(wg) {
			t.Errorf("%s: %v %v %v, want %d %d %d", name, a, d, g, wa, wd, wg)
		}

		f, err := Parse(data, 0)
		if err != nil {
			t.Fatal(err)
		}
		if f.Vertical() != v {
			t.Errorf("%s: Parse and ReadVertical disagree", name)
		}
		// the shaper rule is the one of go-text
		m := f.Metrics(ShaperRule)
		a, d, g = v.Resolve(ShaperRule)
		if a != m.Ascent || d != -m.Descent || g != m.LineGap {
			t.Errorf("%s: shaper rule %v %v %v, go-text has %v %v %v", name, a, d, g, m.Ascent, -m.Descent, m.LineGap)
		}
		if m := f.Metrics(MicrosoftRule); m.Ascent != float32(wa) || m.Descent != -float32(wd) {
			t.Errorf("%s: Metrics(MicrosoftRule) = %v %v", name, m.Ascent, m.Descent)
		}
	}
}

func TestVerticalResolve(t *testing.T) {
	v := Vertical{
		HheaAscender: 700, HheaDescender: -300, HheaLineGap: 200,
		TypoAscender: 750, TypoDescender: -250, TypoLineGap: 100,
		WinAscent: 950, WinDescent: 240,
	}
	check := func(what string, v Vertical, rule VerticalRule, wa, wd, wg float32) {
		t.Helper()
		if a, d, g := v.Resolve(rule); a != wa || d != wd || g != wg {
			t.Errorf("%s: %v %v %v, want %v %v %v", what, a, d, g, wa, wd, wg)
		}
	}
	check("shaper", v, ShaperRule, 700, 300, 200)
	// the hhea line is 1200 and the Windows box 1190: 10 are left
	check("microsoft", v, MicrosoftRule, 950, 240, 10)

	typo := v
	typo.UseTypoMetrics = true
	check("typo, shaper", typo, ShaperRule, 750, 250, 100)
	check("typo, microsoft", typo, MicrosoftRule, 750, 250, 100)

	noWin := v
	noWin.WinAscent, noWin.WinDescent = 0, 0
	check("no Windows values", noWin, MicrosoftRule, 700, 300, 200)

	// A font that stores a negative descent in the unsigned Windows field:
	// the box is then larger than the hhea line, and no gap is left. The sum
	// of the two Windows values passes 65535.
	wrapped := Vertical{HheaAscender: 2294, HheaDescender: -901, HheaLineGap: 0, WinAscent: 2294, WinDescent: 64143}
	check("Windows descent stored negative", wrapped, MicrosoftRule, 2294, 64143, 0)
}
