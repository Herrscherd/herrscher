package approval

import (
	"path"
	"strings"
)

// RequiresHuman reports whether req runs herrscher's device or role verbs from a shell, or names them with a
// command that can reach them, which only a person may decide: no rule, no remembered answer and no mode lets them
// through. It is a text barrier. It splits the command on every punctuation mark and every blank, so it sees the
// name wherever the shell puts it, and it over-blocks on purpose: a command that names herrscher with device or
// role needs a person even when it only reads. It does not see a script written to a file and run afterwards, a
// file downloaded and run, or an encoding it does not name.
func RequiresHuman(req Request) bool {
	if CanonicalTool(req.Tool) != toolBash {
		return false
	}
	words := strings.Fields(normalizeShell(req.Subject))
	if namesHerrscher(req.Subject, words) && (hasWord(words, "device") || hasWord(words, "role") || hasWord(words, "xargs")) {
		return true
	}
	if (strings.ContainsAny(req.Subject, "$`") || hasWord(words, "eval")) && assemblesHerrscher(words) {
		return true
	}
	return runsUnreadable(req.Subject, words)
}

func normalizeShell(command string) string {
	unquoted := strings.NewReplacer(`\n`, " ", `\t`, " ", `\`, "", `"`, "", `'`, "", "`", "").Replace(command)
	return strings.ToLower(strings.Map(func(char rune) rune {
		if strings.ContainsRune("[]{}(),;:&|<>=/.!", char) {
			return ' '
		}
		return char
	}, unquoted))
}

func namesHerrscher(command string, words []string) bool {
	if hasWord(words, "herrscher") {
		return true
	}
	for _, field := range strings.Fields(strings.NewReplacer(`\`, "", `"`, "", `'`, "", "`", "").Replace(command)) {
		if !strings.ContainsAny(field, "*?[") {
			continue
		}
		segment := field[strings.LastIndex(field, "/")+1:]
		if matched, err := path.Match(segment, "herrscher"); err == nil && matched {
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
