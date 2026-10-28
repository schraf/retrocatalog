package main

import (
	"regexp"
	"strings"
)

// accentFolder folds the accented characters that appear in game titles. Input
// is lowercased first, so only lowercase forms are needed.
var accentFolder = strings.NewReplacer(
	"à", "a", "á", "a", "â", "a", "ã", "a", "ä", "a", "å", "a", "ā", "a",
	"ç", "c",
	"è", "e", "é", "e", "ê", "e", "ë", "e", "ē", "e",
	"ì", "i", "í", "i", "î", "i", "ï", "i",
	"ñ", "n",
	"ò", "o", "ó", "o", "ô", "o", "õ", "o", "ö", "o", "ø", "o",
	"ù", "u", "ú", "u", "û", "u", "ü", "u",
	"ý", "y", "ÿ", "y",
	"æ", "ae", "œ", "oe",
	"½", " 1 2 ",
)

var nonAlnum = regexp.MustCompile(`[^a-z0-9]+`)

// catalogKey is the strict title key used for exclusions. It lowercases,
// folds accents, expands "&", and drops punctuation, so it is stable across
// the punctuation variants used by IGDB and LaunchBox.
func catalogKey(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	s = accentFolder.Replace(s)
	s = strings.ReplaceAll(s, "&", " and ")
	s = nonAlnum.ReplaceAllString(s, " ")
	return strings.Join(strings.Fields(s), " ")
}

// publisherPrefix matches the possessive publisher prefixes that LaunchBox
// often keeps but IGDB often omits (or vice versa). It is applied after
// catalogKey, so "Disney's" appears as "disney s".
var publisherPrefix = regexp.MustCompile(`^(?:(?:walt )?disney s|disney|tom clancy s|john romero s|marvel s|james bond 007|james bond|dr seuss|barbie software) `)

var romanNumerals = map[string]string{
	"ii": "2", "iii": "3", "iv": "4", "vi": "6", "vii": "7", "viii": "8", "ix": "9",
}

var trailingBracket = regexp.MustCompile(`\s*[\[(][^\])]*[\])]\s*$`)

// matchKey is the looser key used to match an IGDB title against a LaunchBox
// title. On top of catalogKey it drops trailing bracketed qualifiers (region,
// year, publisher), publisher prefixes, and the "Upper" qualifier, and folds
// roman numerals so "II" matches "2".
func matchKey(s string) string {
	for {
		stripped := trailingBracket.ReplaceAllString(s, "")
		if stripped == s {
			break
		}
		s = stripped
	}

	key := catalogKey(s)
	key = publisherPrefix.ReplaceAllString(key, "")

	fields := strings.Fields(key)
	out := make([]string, 0, len(fields))
	for _, field := range fields {
		if field == "upper" {
			continue
		}
		if numeral, ok := romanNumerals[field]; ok {
			out = append(out, numeral)
			continue
		}
		out = append(out, field)
	}

	key = strings.Join(out, " ")
	// "Wolfenstein 3-D" normalizes to "3 d"; fold it back to "3d".
	return strings.ReplaceAll(key, "3 d", "3d")
}

func keyTokenSet(key string) map[string]struct{} {
	set := make(map[string]struct{})
	for _, field := range strings.Fields(key) {
		set[field] = struct{}{}
	}
	return set
}

func keyNumberSet(tokens map[string]struct{}) map[string]struct{} {
	numbers := make(map[string]struct{})
	for token := range tokens {
		if isNumber(token) {
			numbers[token] = struct{}{}
		}
	}
	return numbers
}

func isNumber(token string) bool {
	if token == "" {
		return false
	}
	for _, r := range token {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

func sameNumberSet(a, b map[string]struct{}) bool {
	if len(a) != len(b) {
		return false
	}
	for token := range a {
		if _, ok := b[token]; !ok {
			return false
		}
	}
	return true
}

func tokenStats(a, b map[string]struct{}) (inter, union int) {
	for token := range a {
		if _, ok := b[token]; ok {
			inter++
		}
	}
	union = len(a) + len(b) - inter
	return inter, union
}

// nameSimilarity is the Jaccard similarity of two token sets.
func nameSimilarity(a, b map[string]struct{}) float64 {
	inter, union := tokenStats(a, b)
	if union == 0 {
		return 0
	}
	return float64(inter) / float64(union)
}

func tokenSubset(a, b map[string]struct{}) bool {
	if len(a) == 0 || len(a) > len(b) {
		return false
	}
	for token := range a {
		if _, ok := b[token]; !ok {
			return false
		}
	}
	return true
}
