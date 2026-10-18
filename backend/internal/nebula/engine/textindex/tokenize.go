// Package textindex defines the shared lexical representation for retrieval.
package textindex

import (
	"strings"
	"unicode"
)

// Tokens keeps words and CJK unigrams/bigrams. This supports Chinese substring
// queries without downloading a dictionary, and separates code identifiers.
func Tokens(text string) []string {
	var out []string
	var word []rune
	var previous rune
	flush := func() {
		if len(word) > 0 {
			out = append(out, string(word))
			word = nil
		}
	}
	for _, r := range strings.ToLower(text) {
		if unicode.Is(unicode.Han, r) || unicode.Is(unicode.Hiragana, r) || unicode.Is(unicode.Katakana, r) {
			flush()
			out = append(out, string(r))
			if previous != 0 {
				out = append(out, string([]rune{previous, r}))
			}
			previous = r
		} else {
			previous = 0
			if unicode.IsLetter(r) || unicode.IsDigit(r) {
				word = append(word, r)
			} else {
				flush()
			}
		}
	}
	flush()
	return out
}
