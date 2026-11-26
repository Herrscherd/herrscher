package approval

import (
	"regexp"
	"strings"
)

var administrationVerb = regexp.MustCompile(`\bherrscher\b(?:\s+-\S+(?:\s+[^\s-]\S*)?)*\s+(?:device|role)\b`)

var evaluation = regexp.MustCompile(`\beval\b`)

// RequiresHuman reports whether req runs herrscher's device or role verbs from a shell, which only a person may
// decide: no rule, no remembered answer and no mode lets them through. The check reads the command text after
// removing quoting and backslashes, so it is a best effort: a command that builds the verb through indirection
// the text does not name can still get past it, and the user account is the real boundary.
func RequiresHuman(req Request) bool {
	if CanonicalTool(req.Tool) != toolBash {
		return false
	}
	text := normalizeShell(req.Subject)
	if administrationVerb.MatchString(text) {
		return true
	}
	expands := strings.ContainsAny(req.Subject, "$`") || evaluation.MatchString(text)
	return expands && namesAdministration(text)
}

func normalizeShell(command string) string {
	unquoted := strings.NewReplacer(`\`, "", `"`, "", `'`, "", "`", "").Replace(command)
	return strings.NewReplacer("(", " ", ")", " ", "{", " ", "}", " ", ";", " ", "&", " ", "|", " ", "<", " ", ">", " ").Replace(unquoted)
}

func namesAdministration(text string) bool {
	letters := strings.Map(func(char rune) rune {
		if char >= 'a' && char <= 'z' {
			return char
		}
		return -1
	}, strings.ToLower(text))
	for _, fragment := range []string{"herr", "scher", "device", "role"} {
		if strings.Contains(letters, fragment) {
			return true
		}
	}
	return false
}
