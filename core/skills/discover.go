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
			s, fresh, ok := loadSkill(filepath.Join(root, e.Name()), prev)
			if !ok || seen[s.Name] {
				continue
			}
			seen[s.Name] = true
			if fresh {
				reread = append(reread, s.bodyPath)
			}
			out = append(out, s)
		}
	}
	return out, reread
}

func loadSkill(dir string, prev map[string]Skill) (Skill, bool, bool) {
	path := filepath.Join(dir, "SKILL.md")
	info, err := os.Stat(path)
	if err != nil {
		return Skill{}, false, false
	}
	now := fileStamp{mod: info.ModTime(), size: info.Size()}
	if s, ok := prev[path]; ok && s.stamp == now {
		return s, false, true
	}
	md, err := os.ReadFile(filepath.Clean(path))
	if err != nil {
		return Skill{}, false, false
	}
	name, desc, ok := parseFrontmatter(md)
	if !ok || name == "" {
		return Skill{}, false, false
	}
	return Skill{Name: name, Description: desc, Dir: dir, bodyPath: path, stamp: now}, true, true
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
