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

// task is what an argv asked for when it asked for a task rather than a verb.
type task struct {
	Text string
	// Print is the non-interactive form: run one turn and write the reply to
	// stdout. The default is a window, so this is set only when --print asked
	// for it, or when there is no terminal to draw one on.
	Print bool
}

// printsTo reports whether this task runs non-interactively, given whether a
// window could be drawn at all.
//
// Two things force it, and only one of them is a preference: --print is the
// operator saying so, and a window that cannot exist is the build or the
// terminal saying so. A run being piped wants the answer rather than a
// Bubbletea error about a missing TTY, and a build with no frontend has nothing
// to open at all.
func (t task) printsTo(canDrawWindow bool) bool { return t.Print || !canDrawWindow }

// promptOf decides whether an argv is a free-text task rather than a verb.
//
// The rule is the one thing that keeps `herrscher sesion` an honest typo instead
// of a billed agent turn: a single unrecognised WORD stays a verb, and only text
// that could not be a verb becomes a prompt. No verb in the registry contains a
// space — contracts.New takes path segments and the registry matches them one by
// one — so whitespace is the discriminator, and -p is the escape hatch for the
// one-word prompt the rule necessarily refuses.
//
// --print carries that escape hatch too, and for the same reason: an operator who
// spelled out which mode they want has already said the argument is a task, so
// `herrscher --print refactor` is one rather than a mistyped verb.
//
// A flag with nothing after it returns ("", true): the caller asked for a prompt
// and gave none, which is a mistake worth naming, not an argv to fall through to
// the verb switch.
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

// nameStemMax bounds the readable part of a session name. The guard downstream
// allows 64 characters and the suffix takes 5 of them; stopping at 40 leaves the
// name legible in `session list` without the branch name running off the line.
const nameStemMax = 40

// nameStemWords is how much of a prompt makes it into the name. Five content
// words (see nameFiller) are enough to recognise the task in a list and short
// enough that the name does not become a paraphrase of the prompt.
const nameStemWords = 5

// slugInvalid matches any run of characters a session name cannot carry. The
// name becomes both a filesystem path and a git ref, which is why the allowed
// set is this narrow.
var slugInvalid = regexp.MustCompile(`[^a-z0-9_-]+`)

// nameFiller is the closed set of words a session name gains nothing from:
// articles, pronouns, prepositions, auxiliaries, conjunctions, and the
// politeness an instruction opens on.
//
// The name has five words to spend, and these are precisely the words two
// prompts are most likely to share. "read the auth module and propose a split"
// and "read the auth module and write tests" spend all five on the half they
// have in common and list as the same name twice; on content words they read as
// what they are.
//
// English and French, which is the latin-alphabet assumption slugInvalid already
// makes. A prompt in neither simply keeps all its words — the safe direction to
// be wrong in, since it is what this did before.
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

// fillerTrim is the punctuation a word may be wearing while still being that
// word, so "please," and "please" both match the filler set.
const fillerTrim = `.,;:!?'"()[]{}«»…`

// foldAccents rewrites a letter carrying a diacritic as the bare letter.
//
// slugInvalid keeps only [a-z0-9_-], so without this an accent is not dropped
// but replaced: "résume la discussion" would name a session "r-sume", and a
// French prompt would arrive in fragments. Folding first costs the accent
// nothing, and it is also what lets nameFiller list its French words once, in
// the form they fold to.
//
// The transformer is built per call because transform.Chain carries state; one
// package-level value would be a race between two prompts named at once.
func foldAccents(s string) string {
	t := transform.Chain(norm.NFD, runes.Remove(runes.In(unicode.Mn)), norm.NFC)
	out, _, err := transform.String(t, s)
	if err != nil {
		return s
	}
	return out
}

// nameWords reduces a prompt to the words worth naming a session after.
//
// Filtering hands the words back untouched when it would leave nothing, so a
// prompt made entirely of filler still names its session after itself rather
// than falling through to the anonymous "s-" stem.
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

// sessionNameFor derives a session name from a free-text prompt.
//
// The random suffix is not decoration. `session create` refuses a name already
// taken, so two prompts opening on the same words would collide — and it also
// stops the prompt's exact wording from deciding a git branch name.
//
// The result already satisfies the session-name guard; `session create`
// slugifies again and stays the final word, but it is never handed something it
// would have to repair.
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
		stem = "s" // an all-emoji or all-CJK prompt leaves nothing to name it with
	}
	return stem + "-" + strings.ToLower(rand.Text()[:4])
}
