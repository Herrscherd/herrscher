package main

import (
	"crypto/rand"
	"regexp"
	"strings"
)

// promptOf decides whether an argv is a free-text task rather than a verb.
//
// The rule is the one thing that keeps `herrscher sesion` an honest typo instead
// of a billed agent turn: a single unrecognised WORD stays a verb, and only text
// that could not be a verb becomes a prompt. No verb in the registry contains a
// space — contracts.New takes path segments and the registry matches them one by
// one — so whitespace is the discriminator, and -p is the escape hatch for the
// one-word prompt the rule necessarily refuses.
//
// A -p with nothing after it returns ("", true): the caller asked for a prompt
// and gave none, which is a mistake worth naming, not an argv to fall through to
// the verb switch.
func promptOf(cmd string, args []string) (string, bool) {
	if cmd == "-p" || cmd == "--prompt" {
		return strings.TrimSpace(strings.Join(args, " ")), true
	}
	if strings.HasPrefix(cmd, "-") {
		return "", false
	}
	if !strings.ContainsAny(cmd, " \t\n\r") {
		return "", false
	}
	text := strings.TrimSpace(strings.Join(append([]string{cmd}, args...), " "))
	if text == "" {
		return "", false
	}
	return text, true
}

// nameStemMax bounds the readable part of a session name. The guard downstream
// allows 64 characters and the suffix takes 5 of them; stopping at 40 leaves the
// name legible in `session list` without the branch name running off the line.
const nameStemMax = 40

// nameStemWords is how much of a prompt makes it into the name. Five words is
// enough to recognise the task in a list and short enough that the name does not
// become a paraphrase of the prompt.
const nameStemWords = 5

// slugInvalid matches any run of characters a session name cannot carry. The
// name becomes both a filesystem path and a git ref, which is why the allowed
// set is this narrow.
var slugInvalid = regexp.MustCompile(`[^a-z0-9_-]+`)

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
	words := strings.Fields(prompt)
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
