package skills

import (
	"strings"
	"unicode"
	"unicode/utf8"

	"golang.org/x/text/unicode/norm"
)

const minWordRunes = 3

var stopWords = map[string]bool{
	"les": true, "des": true, "une": true, "pour": true, "dans": true, "avec": true,
	"sur": true, "par": true, "que": true, "qui": true, "est": true, "sont": true,
	"son": true, "sa": true, "ses": true, "aux": true, "leur": true, "leurs": true,
	"ces": true, "cet": true, "cette": true, "tout": true, "tous": true, "elle": true,
	"ils": true, "nous": true, "vous": true, "pas": true, "plus": true, "mais": true,
	"donc": true, "the": true, "and": true, "for": true, "with": true, "from": true,
	"this": true, "that": true, "are": true, "was": true, "not": true, "you": true,
	"your": true, "but": true, "can": true, "have": true, "has": true, "into": true,
	"its": true, "who": true, "what": true, "when": true, "where": true, "which": true,
	"all": true, "any": true, "how": true, "also": true, "than": true, "then": true,
	"them": true, "they": true, "our": true,
}

func wordSet(text string) map[string]bool {
	words := make(map[string]bool)
	fields := strings.FieldsFunc(strings.ToLower(text), func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	})
	for _, field := range fields {
		folded := foldAccents(field)
		if utf8.RuneCountInString(folded) < minWordRunes || stopWords[folded] {
			continue
		}
		words[stemWord(folded)] = true
	}
	return words
}

func foldAccents(word string) string {
	return strings.Map(func(r rune) rune {
		if unicode.Is(unicode.Mn, r) {
			return -1
		}
		return r
	}, norm.NFD.String(word))
}

func stemWord(word string) string {
	if utf8.RuneCountInString(word) <= minWordRunes {
		return word
	}
	if strings.HasSuffix(word, "s") || strings.HasSuffix(word, "x") {
		return word[:len(word)-1]
	}
	return word
}
