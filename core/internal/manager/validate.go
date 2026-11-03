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
	slug := slugInvalidRe.ReplaceAllString(strings.ToLower(strings.TrimSpace(name)), "-")
	slug = strings.Trim(slug, "-_")
	if len(slug) > maxSlugLen {
		slug = strings.Trim(slug[:maxSlugLen], "-_")
	}
	return slug
}

var projectRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]{0,127}$`)

func repoFor(workspace, project string) string {
	if workspace == "" || project == "" {
		return workspace
	}
	return filepath.Join(workspace, project)
}
