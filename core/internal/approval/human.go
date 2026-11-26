package approval

import (
	"regexp"
	"strings"
)

var administrationVerb = regexp.MustCompile(`\bherrscher\s+(device|role)\b`)

// RequiresHuman reports whether req runs herrscher's device or role verbs from a shell.
func RequiresHuman(req Request) bool {
	if CanonicalTool(req.Tool) != toolBash {
		return false
	}
	quoting := strings.NewReplacer(`"`, " ", `'`, " ", "\\", " ", "`", " ", "(", " ", ")", " ")
	return administrationVerb.MatchString(quoting.Replace(req.Subject))
}
