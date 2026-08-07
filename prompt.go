package main

import "strings"

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
