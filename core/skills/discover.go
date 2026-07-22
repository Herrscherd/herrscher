package skills

import (
	"bufio"
	"bytes"
	"os"
	"path/filepath"
	"strings"
)

// Discover scans each root for immediate <root>/<name>/SKILL.md entries, parses
// their frontmatter, and returns the valid skills. De-duplication is by Name
// with earlier roots winning, so a repo skill overrides a global one of the same
// name. A missing root, an unreadable entry, or a SKILL.md without a name is
// skipped, never fatal.
func Discover(roots []string) []Skill {
	var out []Skill
	seen := map[string]bool{}
	for _, root := range roots {
		entries, err := os.ReadDir(root)
		if err != nil {
			continue
		}
		for _, e := range entries {
			if !e.IsDir() {
				continue
			}
			dir := filepath.Join(root, e.Name())
			path := filepath.Join(dir, "SKILL.md")
			md, err := os.ReadFile(path)
			if err != nil {
				continue
			}
			name, desc, ok := parseFrontmatter(md)
			if !ok || name == "" || seen[name] {
				continue
			}
			seen[name] = true
			out = append(out, Skill{Name: name, Description: desc, Dir: dir, bodyPath: path})
		}
	}
	return out
}

// parseFrontmatter reads a leading --- fenced block of key: value lines and
// returns the name and description. ok is false when the file does not open with
// a --- fence.
func parseFrontmatter(md []byte) (name, description string, ok bool) {
	sc := bufio.NewScanner(bytes.NewReader(md))
	if !sc.Scan() || strings.TrimSpace(sc.Text()) != "---" {
		return "", "", false
	}
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "---" {
			return name, description, true
		}
		key, val, found := strings.Cut(line, ":")
		if !found {
			continue
		}
		switch strings.TrimSpace(key) {
		case "name":
			name = strings.TrimSpace(val)
		case "description":
			description = strings.TrimSpace(val)
		}
	}
	return "", "", false
}
