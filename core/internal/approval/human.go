package approval

import (
	"regexp"
	"strings"
)

// RequiresHuman reports whether req runs herrscher's device or role verbs from a shell, which only a person may
// decide: no rule, no remembered answer and no mode lets them through. It is a text barrier: it reads the command
// after removing quoting and punctuation, and it does not see a script written to a file and run afterwards, a file
// downloaded and run, or an encoding it does not name.
func RequiresHuman(req Request) bool {
	if CanonicalTool(req.Tool) != toolBash {
		return false
	}
	words := strings.Fields(normalizeShell(req.Subject))
	if programHerrscher(req.Subject) && (hasWord(words, "device") || hasWord(words, "role") || hasWord(words, "xargs")) {
		return true
	}
	if (strings.ContainsAny(req.Subject, "$`") || hasWord(words, "eval")) && assemblesHerrscher(words) {
		return true
	}
	return runsUnreadable(req.Subject, words)
}

func normalizeShell(command string) string {
	spaced := strings.NewReplacer(`\n`, " ", `\t`, " ").Replace(command)
	return strings.ToLower(strings.Map(func(char rune) rune {
		if strings.ContainsRune("[]{}(),;:&|<>=\"'`\\/.", char) {
			return ' '
		}
		return char
	}, spaced))
}

func programPositionPatterns() []*regexp.Regexp {
	return []*regexp.Regexp{
		regexp.MustCompile(`(?:^|[;&|(` + "`" + `{])\s*(?:\S*/)?herrscher\b`),
		regexp.MustCompile(`(?:\b(?:env|sudo|exec|xargs|nohup|time|command|eval)\b|\s-[ce]\b)[^;&|` + "`" + `]*?[\s\[,(](?:\S*/)?herrscher\b`),
		regexp.MustCompile(`[\[,(]\s*(?:\S*/)?herrscher\b`),
	}
}

func programHerrscher(command string) bool {
	position := strings.NewReplacer(`\n`, " ", `\t`, " ", `\`, "", `"`, "", `'`, "").Replace(command)
	for _, pattern := range programPositionPatterns() {
		if pattern.MatchString(strings.ToLower(position)) {
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

func assemblesHerrscher(words []string) bool {
	const name = "herrscher"
	reachable := make([]bool, len(name)+1)
	reachable[len(name)] = true
	for offset := len(name) - 1; offset >= 0; offset-- {
		for _, word := range words {
			if word == "" || strings.Contains(word, "$") {
				continue
			}
			if strings.HasPrefix(name[offset:], word) && reachable[offset+len(word)] {
				reachable[offset] = true
				break
			}
		}
	}
	return reachable[0]
}

func runsUnreadable(raw string, words []string) bool {
	runs := hasWord(words, "sh") || hasWord(words, "bash") || hasWord(words, "zsh") || hasWord(words, "dash") || hasWord(words, "eval")
	if strings.Contains(raw, "python -c") || strings.Contains(raw, "perl -e") || strings.Contains(raw, "python3 -c") {
		runs = true
	}
	if !runs {
		return false
	}
	for _, decoder := range []string{"base64", "xxd", "gunzip", "zcat", "openssl", "uudecode", `\x`, "$'"} {
		if strings.Contains(raw, decoder) {
			return true
		}
	}
	return false
}
