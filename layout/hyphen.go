package layout

// HyphenDictionaryIterations splits word on each hyphen occurrence, starting by the end.
func HyphenDictionaryIterations(word []rune, hyphen rune) (out []string) {
	for i := len(word) - 1; i >= 0; i-- {
		if word[i] == hyphen {
			out = append(out, string(word[:i+1]))
		}
	}
	return out
}

// Hyphener finds where the words of one language may be hyphenated.
type Hyphener interface {
	// IterateRunes returns the ways of breaking the word, the longest first
	// part first: each element is prefix followed by the part of the word
	// before a break.
	IterateRunes(word []rune, prefix string) []string
}

// Hyphenation gives the hyphener of a language: it is what `hyphens: auto`
// breaks words with. A text engine is given one; without it, as with
// [NoHyphenation], a word is hyphenated only where the author wrote a soft
// hyphen, which needs no knowledge of the language.
//
// The interface keeps the rules of the languages, and the data they need,
// out of this module: github.com/anaelorlinski/aaoo.hyphendict-go implements
// it with the patterns of TeX.
type Hyphenation interface {
	// Language returns the language whose rules hyphenate the language with
	// the given tag: that language, or a more general one ("en" for
	// "en-AU"). It returns the empty string when there is none.
	Language(tag string) string

	// Hyphener returns the hyphener of a language [Language] returned. left
	// and right are the fewest letters to leave before and after a break,
	// the values of hyphenate-limit-chars; [LimitAuto] leaves one to the
	// implementation, which may know better for the language.
	Hyphener(lang string, left, right int) Hyphener
}

// NoHyphenation is the [Hyphenation] that knows no language.
type NoHyphenation struct{}

func (NoHyphenation) Language(string) string { return "" }

func (NoHyphenation) Hyphener(string, int, int) Hyphener { return nil }

// HyphenDictKey identifies a hyphener in the cache of a layout: a language
// and the limits around a break.
type HyphenDictKey struct {
	lang  string
	limit Limits
}

// NewHyphenDictKey creates a HyphenDictKey from a language and hyphenation limits.
func NewHyphenDictKey(lang string, limit Limits) HyphenDictKey {
	return HyphenDictKey{lang, limit}
}
