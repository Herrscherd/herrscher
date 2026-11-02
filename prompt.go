package main

import (
	"crypto/rand"
	"regexp"
	"strings"
	"unicode"

	"golang.org/x/text/runes"
	"golang.org/x/text/transform"
	"golang.org/x/text/unicode/norm"
)

type task struct {
	Text  string
	Print bool
}

func (t task) printsTo(canDrawWindow bool) bool { return t.Print || !canDrawWindow }

func promptOf(cmd string, args []string) (task, bool) {
	switch cmd {
	case "-p", "--prompt":
		return task{Text: strings.TrimSpace(strings.Join(args, " "))}, true
	case "--print":
		return task{Text: strings.TrimSpace(strings.Join(args, " ")), Print: true}, true
	}
	if strings.HasPrefix(cmd, "-") {
		return task{}, false
	}
	if !strings.ContainsAny(cmd, " \t\n\r") {
		return task{}, false
	}
	text := strings.TrimSpace(strings.Join(append([]string{cmd}, args...), " "))
	if text == "" {
		return task{}, false
	}
	return task{Text: text}, true
}

const nameStemMax = 40

const nameStemWords = 5

var slugInvalid = regexp.MustCompile(`[^a-z0-9_-]+`)

var nameFiller = map[string]bool{
	"a": true, "ai": true, "an": true, "and": true, "are": true, "as": true,
	"at": true, "au": true, "aux": true, "be": true, "been": true, "but": true,
	"by": true, "can": true, "ce": true, "ces": true, "cet": true, "cette": true,
	"could": true, "dans": true, "de": true, "des": true, "do": true,
	"does": true, "du": true, "en": true, "est": true, "et": true, "for": true,
	"from": true, "had": true, "has": true, "have": true, "i": true, "if": true,
	"il": true, "ils": true, "in": true, "into": true, "is": true, "it": true,
	"its": true, "je": true, "la": true, "le": true, "les": true, "leur": true,
	"lui": true, "ma": true, "mais": true, "me": true, "mes": true, "moi": true,
	"mon": true, "my": true, "ne": true, "nos": true, "notre": true,
	"nous": true, "of": true, "on": true, "ont": true, "or": true, "ou": true,
	"our": true, "par": true, "pas": true, "please": true, "pour": true,
	"que": true, "qui": true, "sa": true, "se": true, "ses": true,
	"should": true, "so": true, "son": true, "sont": true, "stp": true,
	"sur": true, "ta": true, "te": true, "tes": true, "that": true,
	"the": true, "their": true, "them": true, "then": true, "there": true,
	"these": true, "this": true, "to": true, "toi": true, "ton": true,
	"tu": true, "un": true, "une": true, "us": true, "vos": true,
	"votre": true, "vous": true, "was": true, "we": true, "were": true,
	"will": true, "with": true, "would": true, "y": true, "you": true,
	"your": true,
}

const fillerTrim = `.,;:!?'"()[]{}«»…`

func foldAccents(s string) string {
	t := transform.Chain(norm.NFD, runes.Remove(runes.In(unicode.Mn)), norm.NFC)
	out, _, err := transform.String(t, s)
	if err != nil {
		return s
	}
	return out
}

func nameWords(prompt string) []string {
	all := strings.Fields(foldAccents(prompt))
	kept := make([]string, 0, len(all))
	for _, w := range all {
		if !nameFiller[strings.Trim(strings.ToLower(w), fillerTrim)] {
			kept = append(kept, w)
		}
	}
	if len(kept) == 0 {
		return all
	}
	return kept
}

func sessionNameFor(prompt string) string {
	words := nameWords(prompt)
	if len(words) > nameStemWords {
		words = words[:nameStemWords]
	}
	stem := slugInvalid.ReplaceAllString(strings.ToLower(strings.Join(words, " ")), "-")
	if len(stem) > nameStemMax {
		stem = stem[:nameStemMax]
	}
	stem = strings.Trim(stem, "-_")
	if stem == "" {
		stem = "s"
	}
	return stem + "-" + strings.ToLower(rand.Text()[:4])
}
