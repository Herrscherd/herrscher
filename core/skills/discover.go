package skills

import (
	"bufio"
	"bytes"
	"os"
	"path/filepath"
	"strings"
)

const (
	frontmatterFence = "---"
)

// Discover scans each root for immediate <root>/<name>/SKILL.md entries, parses
// their frontmatter, and returns the valid skills. De-duplication is by Name
// with earlier roots winning, so a repo skill overrides a global one of the same
// name. A missing root, an unreadable entry, or a SKILL.md without a name is
// skipped, never fatal.
func Discover(roots []string) []Skill {
	out, _ := discover(roots, nil)
	return out
}

func discover(roots []string, prev map[string]Skill) ([]Skill, []string) {
	seen := map[string]bool{}
	var out []Skill
	var reread []string
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
			info, err := os.Stat(path)
			if err != nil {
				continue
			}
			now := fileStamp{mod: info.ModTime(), size: info.Size()}
			if s, ok := prev[path]; ok && s.stamp == now {
				if seen[s.Name] {
					continue
				}
				seen[s.Name] = true
				out = append(out, s)
				continue
			}
			md, err := os.ReadFile(filepath.Clean(path))
			if err != nil {
				continue
			}
			name, desc, ok := parseFrontmatter(md)
			if !ok || name == "" || seen[name] {
				continue
			}
			seen[name] = true
			reread = append(reread, path)
			out = append(out, Skill{Name: name, Description: desc, Dir: dir, bodyPath: path, stamp: now})
		}
	}
	return out, reread
}

func parseFrontmatter(md []byte) (string, string, bool) {
	var name, description string
	sc := bufio.NewScanner(bytes.NewReader(md))
	if !sc.Scan() || strings.TrimSpace(sc.Text()) != frontmatterFence {
		return "", "", false
	}
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == frontmatterFence {
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
