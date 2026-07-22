// Package skills discovers SKILL.md skills and runs a progressive-disclosure
// engine that injects a skill menu into a turn and expands a skill's body on
// demand — so backends that do not load skills natively can still use them.
package skills

import (
	"bufio"
	"bytes"
	"os"
	"strings"
)

// Skill is one discovered SKILL.md: its frontmatter name/description, the
// directory holding it (so an expanded body can point the model at bundled
// resource files by absolute path), and the path to the markdown body.
type Skill struct {
	Name        string
	Description string
	Dir         string
	bodyPath    string
}

// Body reads the skill's markdown body, with the leading --- frontmatter block
// stripped (the name and description already surface in the menu). When there is
// no frontmatter fence the whole file is returned.
func (s Skill) Body() (string, error) {
	b, err := os.ReadFile(s.bodyPath)
	if err != nil {
		return "", err
	}
	return stripFrontmatter(b), nil
}

// stripFrontmatter drops a leading ---…--- block and returns the remaining body
// verbatim. Input without an opening --- fence is returned unchanged.
func stripFrontmatter(md []byte) string {
	sc := bufio.NewScanner(bytes.NewReader(md))
	if !sc.Scan() || strings.TrimSpace(sc.Text()) != "---" {
		return string(md)
	}
	var body strings.Builder
	inBody := false
	for sc.Scan() {
		if !inBody {
			if strings.TrimSpace(sc.Text()) == "---" {
				inBody = true
			}
			continue
		}
		body.WriteString(sc.Text())
		body.WriteByte('\n')
	}
	return body.String()
}
