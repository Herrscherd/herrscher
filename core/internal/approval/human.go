package approval

import "strings"

var interpreters = []string{"sh", "bash", "zsh", "dash", "eval"}

var decoders = []string{"base64", "xxd", "gunzip", "zcat", "openssl", "uudecode", `\x`, "$'"}

// RequiresHuman reports whether req runs herrscher's device or role verbs from a shell, which only a person may
// decide: no rule, no remembered answer and no mode lets them through. The check reads the command text after
// removing quoting, backslashes and separators, so it is a text barrier. It does not see a script written to a
// file and run afterwards, a file downloaded and run, or an encoding it does not name.
func RequiresHuman(req Request) bool {
	if CanonicalTool(req.Tool) != toolBash {
		return false
	}
	text := normalizeShell(req.Subject)
	words := strings.Fields(text)
	if namesHerrscher(words) && (hasWord(words, "device") || hasWord(words, "role") || hasWord(words, "xargs")) {
		return true
	}
	if (strings.ContainsAny(req.Subject, "$`") || hasWord(words, "eval")) && spellsHerrscher(text) {
		return true
	}
	return runsUnreadable(req.Subject, words)
}

func normalizeShell(command string) string {
	spaced := strings.NewReplacer(`\n`, " ", `\t`, " ").Replace(command)
	unquoted := strings.NewReplacer(`\`, "", `"`, "", `'`, "", "`", "").Replace(spaced)
	return strings.NewReplacer("(", " ", ")", " ", "{", " ", "}", " ", ";", " ", "&", " ", "|", " ", "<", " ", ">", " ", "=", " ").Replace(unquoted)
}

func namesHerrscher(words []string) bool {
	for _, word := range words {
		if word == "herrscher" || strings.HasSuffix(word, "/herrscher") {
			return true
		}
	}
	return false
}

func hasWord(words []string, want string) bool {
	for _, word := range words {
		if word == want {
			return true
		}
	}
	return false
}

func spellsHerrscher(text string) bool {
	letters := strings.Map(func(char rune) rune {
		if char >= 'a' && char <= 'z' {
			return char
		}
		return -1
	}, strings.ToLower(text))
	const name = "herrscher"
	if strings.Contains(letters, name) {
		return true
	}
	for cut := 1; cut < len(name); cut++ {
		if strings.Contains(letters, name[:cut]) && strings.Contains(letters, name[cut:]) {
			return true
		}
	}
	return false
}

func runsUnreadable(raw string, words []string) bool {
	runs := false
	for _, interpreter := range interpreters {
		if hasWord(words, interpreter) {
			runs = true
		}
	}
	if strings.Contains(raw, "python -c") || strings.Contains(raw, "perl -e") || strings.Contains(raw, "python3 -c") {
		runs = true
	}
	if !runs {
		return false
	}
	for _, decoder := range decoders {
		if strings.Contains(raw, decoder) {
			return true
		}
	}
	return false
}
