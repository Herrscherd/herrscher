// Package skills discovers SKILL.md skills and runs a progressive-disclosure
// engine that injects a skill menu into a turn and expands a skill's body on
// demand, so backends that do not load skills natively can still use them.
package skills

import (
	"bufio"
	"bytes"
	"os"
	"strings"
	"time"
)

// Skill is one discovered SKILL.md: its frontmatter name/description, the
// directory holding it (so an expanded body can point the model at bundled
// resource files by absolute path), and the path to the markdown body.
type Skill struct {
	Name        string
	Description string
	Dir         string
	bodyPath    string
	stamp       fileStamp
}

type fileStamp struct {
	mod  time.Time
	size int64
}

// Body reads the skill's markdown body, with the leading --- frontmatter block
// stripped (the name and description already surface in the menu). When there is
// no frontmatter fence the whole file is returned.
func (s Skill) Body() (string, error) {
	payload, err := os.ReadFile(s.bodyPath)
	if err != nil {
		return "", err
	}
	return stripFrontmatter(payload), nil
}

func stripFrontmatter(content []byte) string {
	scanner := bufio.NewScanner(bytes.NewReader(content))
	if !scanner.Scan() || strings.TrimSpace(scanner.Text()) != frontmatterFence {
		return string(content)
	}
	var body strings.Builder
	inBody := false
	for scanner.Scan() {
		if !inBody {
			if strings.TrimSpace(scanner.Text()) == frontmatterFence {
				inBody = true
			}
			continue
		}
		body.WriteString(scanner.Text())
		body.WriteByte('\n')
	}
	return body.String()
}
