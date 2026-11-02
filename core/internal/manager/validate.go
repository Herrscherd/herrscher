package manager

import (
	"path/filepath"
	"regexp"
	"strings"
)

var sessionNameRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_-]{0,63}$`)

var slugInvalidRe = regexp.MustCompile(`[^a-z0-9_-]+`)

const maxSlugLen = 64

func slugify(name string) string {
	s := slugInvalidRe.ReplaceAllString(strings.ToLower(strings.TrimSpace(name)), "-")
	s = strings.Trim(s, "-_")
	if len(s) > maxSlugLen {
		s = strings.Trim(s[:maxSlugLen], "-_")
	}
	return s
}

var projectRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]{0,127}$`)

func repoFor(workspace, project string) string {
	if workspace == "" || project == "" {
		return workspace
	}
	return filepath.Join(workspace, project)
}
