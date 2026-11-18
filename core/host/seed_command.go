package host

import (
	"path/filepath"
	"strings"
	"unicode"

	contracts "github.com/Herrscherd/herrscher-contracts"
)

func vendorOfCmd(cmd string, plugins []contracts.Plugin) string {
	first := commandBinary(commandWords(cmd))
	if first == "" {
		return ""
	}
	binary := filepath.Base(first)
	for idx := range plugins {
		plugin := &plugins[idx]
		if plugin.Backend == nil {
			continue
		}
		for _, setting := range plugin.Manifest.Config {
			if setting.Key == "cmd" && setting.Default == binary {
				return plugin.Manifest.Kind
			}
		}
	}
	return ""
}

func commandWords(cmd string) []string {
	var words []string
	var word strings.Builder
	var quote rune
	inWord := false
	for _, char := range cmd {
		switch {
		case quote != 0:
			if char == quote {
				quote = 0
			} else {
				word.WriteRune(char)
			}
		case char == '\'' || char == '"':
			quote = char
			inWord = true
		case unicode.IsSpace(char):
			if inWord {
				words = append(words, word.String())
				word.Reset()
				inWord = false
			}
		default:
			word.WriteRune(char)
			inWord = true
		}
	}
	if inWord {
		words = append(words, word.String())
	}
	return words
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

func commandBinary(words []string) string {
	return envNamedCommand(words, 0)
}

func skipEnvAssignments(words []string) int {
	idx := 0
	for idx < len(words) && isEnvAssignment(words[idx]) {
		idx++
	}
	return idx
}

const envNestingLimit = 8

func envCommandAt(args []string, depth int) string {
	if depth > envNestingLimit {
		return ""
	}
	for idx := 0; idx < len(args); idx++ {
		word := args[idx]
		switch {
		case word == "--":
			return envNamedCommand(args[idx+1:], depth)
		case word == "--unset" || word == "--chdir":
			idx++
		case word == "--split-string" || strings.HasPrefix(word, "--split-string="):
			return envSplitLong(word, args[idx+1:], depth+1)
		case strings.HasPrefix(word, "--"):
		case word == "-":
		case strings.HasPrefix(word, "-") && len(word) > 1:
			if at := shortEnvSplitAt(word); at > 0 {
				return envSplitCommand(word[at+1:], args[idx+1:], depth+1)
			}
			if shortEnvOptionTakesNext(word) {
				idx++
			}
		case isEnvAssignment(word):
		default:
			return envNamedCommand(args[idx:], depth)
		}
	}
	return ""
}

func envSplitLong(word string, rest []string, depth int) string {
	if value, inline := strings.CutPrefix(word, "--split-string="); inline {
		return envCommandAt(append(commandWords(value), rest...), depth)
	}
	if len(rest) == 0 {
		return ""
	}
	return envCommandAt(append(commandWords(rest[0]), rest[1:]...), depth)
}

func envNamedCommand(words []string, depth int) string {
	idx := skipEnvAssignments(words)
	if idx >= len(words) {
		return ""
	}
	if filepath.Base(words[idx]) == "env" {
		return envCommandAt(words[idx+1:], depth+1)
	}
	return words[idx]
}

func envSplitCommand(value string, rest []string, depth int) string {
	if value == "" {
		if len(rest) == 0 {
			return ""
		}
		value, rest = rest[0], rest[1:]
	}
	return envCommandAt(append(commandWords(value), rest...), depth)
}

func shortEnvSplitAt(word string) int {
	for idx := 1; idx < len(word); idx++ {
		switch word[idx] {
		case 'S':
			return idx
		case 'u', 'C':
			return -1
		}
	}
	return -1
}

func shortEnvOptionTakesNext(word string) bool {
	for idx := 1; idx < len(word); idx++ {
		if word[idx] == 'u' || word[idx] == 'C' {
			return idx == len(word)-1
		}
	}
	return false
}

func isEnvAssignment(word string) bool {
	name, _, found := strings.Cut(word, "=")
	return found && name != "" && !strings.HasPrefix(word, "-")
}
