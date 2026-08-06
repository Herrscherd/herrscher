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
	out, _ := discover(roots, nil)
	return out
}

// discover is Discover with the previous scan's result, keyed by SKILL.md path.
// A file whose stamp is unchanged is taken from prev — one Stat instead of a
// read and a frontmatter parse — which matters because the engine rescans on
// every single turn. The second return names the paths that were actually
// re-read, so a caller caching anything else derived from the file knows what to
// drop; a file missing from it either did not change or is gone.
func discover(roots []string, prev map[string]Skill) (out []Skill, reread []string) {
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
			md, err := os.ReadFile(path)
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
