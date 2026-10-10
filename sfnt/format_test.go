package sfnt

import (
	"os"
	"testing"
)

func TestToSFNT(t *testing.T) {
	for _, tc := range []struct{ file, mediatype, ext string }{
		{"DejaVuSerif.ttf", "font/truetype", ".ttf"},
		{"EBGaramond12-Regular.otf", "font/opentype", ".otf"},
		{"DejaVuSerif.woff", "font/woff", ".woff"},
		{"DejaVuSerif.woff2", "font/woff2", ".woff2"},
		{"DejaVuSerif.eot", "application/vnd.ms-fontobject", ".eot"},
	} {
		b, err := os.ReadFile("resources/" + tc.file)
		if err != nil {
			t.Fatal(err)
		}
		if got, err := MediaType(b); err != nil || got != tc.mediatype {
			t.Errorf("%s: media type %q, %v; want %q", tc.file, got, err, tc.mediatype)
		}
		if got := Extension(b); got != tc.ext {
			t.Errorf("%s: extension %q, want %q", tc.file, got, tc.ext)
		}
		out, err := ToSFNT(b)
		if err != nil {
			t.Errorf("%s: %v", tc.file, err)
			continue
		}
		if _, err := ParseSFNT(out, 0); err != nil {
			t.Errorf("%s: the decoded font does not parse: %v", tc.file, err)
		}
	}
	if _, err := ToSFNT([]byte("not a font, but long enough to reach the EOT magic check")); err == nil {
		t.Errorf("garbage must fail")
	}
}
