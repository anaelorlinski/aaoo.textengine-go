package layout

import (
	"os"

	"github.com/anaelorlinski/aaoo.textengine-go/face"
	"github.com/go-text/typesetting/font"
)

// Missing says what a text is set in when its style names no family the
// engine was given, with [Engine.LoadFont] or [Engine.AddFontFile].
type Missing uint8

const (
	// MissingToFontMap leaves the choice to the font map of the engine,
	// with the fonts it has: go-text then substitutes as it sees fit.
	MissingToFontMap Missing = iota
	// MissingToSystem looks for the families on the system, whose fonts
	// are loaded when first needed. When none is installed, which is the
	// case of a generic name such as sans-serif, the text is set in the
	// first installed family of [Engine.SystemFallbacks], and failing that
	// in [Engine.Fallback].
	MissingToSystem
	// MissingToFallback sets the text in [Engine.Fallback]: a missing
	// family then gives the same font on every run and every machine.
	MissingToFallback
)

// Stated tells which parts of the description of a font its source states,
// an @font-face rule for instance. See [Engine.StyleOf].
type Stated struct {
	Weight, Italic bool
}

// privateSuffix names the family the fonts of [Engine.AddFontFile] are kept
// under when the family is also one of [Engine.LoadFont].
const privateSuffix = " (@font-face)"

// LoadFont makes a font available under a family name, with the style the
// caller says it has, whatever the font file says of itself.
//
// A condensed or expanded font is also made available under the family its
// own name table gives, with its width, when that is another family than the
// one asked for: "DejaVu Sans Condensed" then serves the family "DejaVu
// Sans" at a condensed stretch, as CSS expects of the widths of a family.
func (fc *Engine) LoadFont(family string, content []byte, style face.Style) error {
	key, err := fc.AddFont(content, face.Description{Family: family, Style: style})
	if err != nil {
		return err
	}
	fc.loaded(family)
	fc.given[key] = true

	if f, ok := fc.fonts.Lookup(key); ok {
		own := f.Describe()
		if own.Width != face.WidthNormal && font.NormalizeFamily(own.Family) != font.NormalizeFamily(family) {
			style.Width = own.Width
			if _, err := fc.AddFont(content, face.Description{Family: own.Family, Style: style}); err != nil {
				return err
			}
			fc.loaded(own.Family)
		}
	}
	return nil
}

// loaded records a family given with LoadFont.
func (fc *Engine) loaded(family string) {
	name := font.NormalizeFamily(family)
	fc.families[name] = true
	fc.bundled[name] = true
}

// StyleOf returns what a font is, for the synthetic bold and italic: a style
// asking for more than the font is gets it simulated.
//
//   - A font given with [Engine.LoadFont] is what the caller gave it as,
//     whatever its name table says.
//   - A font given with [Engine.AddFontFile] is what its description says,
//     as browsers do with the descriptors of an @font-face rule: font files
//     often state a wrong weight. A part the source did not state, see
//     [Stated], is read from the file instead, so that a bold file declared
//     without a weight is not made bolder again.
//   - Another font, of the system for instance, is what it describes itself
//     as.
func (fc *Engine) StyleOf(f *font.Face) face.Style {
	own := face.StyleOf(f.Describe().Aspect)
	registered, ok := fc.fonts.Registered(f.Font)
	if !ok {
		return own
	}
	key := fc.fm.FontLocation(f.Font).File
	if fc.given[key] {
		return registered
	}
	said := fc.stated[key]
	if said.Weight {
		own.Weight = registered.Weight
	}
	if said.Italic {
		own.Italic = registered.Italic
	}
	return own
}

// resolve returns the style to lay out with: the style itself, or a copy
// with other families, see [Missing] and [Engine.AddFontFile]. Resolving a
// resolved style changes nothing.
func (fc *Engine) resolve(style *Style) *Style {
	if len(fc.overrides) != 0 {
		// the fonts of AddFontFile before the ones of LoadFont of the same
		// family, which still serve the faces the former does not give
		var families []string
		for i, family := range style.Family {
			private, ok := fc.overrides[font.NormalizeFamily(family)]
			if ok && i != 0 && style.Family[i-1] == private {
				ok = false // already resolved
			}
			if ok {
				if families == nil {
					families = append(make([]string, 0, len(style.Family)+1), style.Family[:i]...)
				}
				families = append(families, private)
			}
			if families != nil {
				families = append(families, family)
			}
		}
		if families != nil {
			overridden := *style
			overridden.Family = families
			return &overridden
		}
	}
	if fc.Missing == MissingToFontMap {
		return style
	}
	for _, family := range style.Family {
		if fc.families[font.NormalizeFamily(family)] {
			return style
		}
	}
	if fc.Missing == MissingToSystem {
		if !fc.systemFontsLoaded {
			fc.systemFontsLoaded = true
			cacheDir, err := os.UserCacheDir()
			if err != nil {
				cacheDir = os.TempDir()
			}
			// A failure leaves the loaded fonts only.
			_ = fc.fm.UseSystemFonts(cacheDir)
		}
		for _, family := range style.Family {
			if len(fc.fm.FindSystemFonts(family)) != 0 {
				return style
			}
		}
		for _, family := range fc.SystemFallbacks {
			if len(fc.fm.FindSystemFonts(family)) != 0 {
				withSystem := *style
				withSystem.Family = []string{family}
				return &withSystem
			}
		}
	}
	if fc.Fallback == "" {
		return style
	}
	withFallback := *style
	withFallback.Family = []string{fc.Fallback}
	return &withFallback
}
