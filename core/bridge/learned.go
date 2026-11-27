package bridge

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"unicode"

	contracts "github.com/Herrscherd/herrscher-contracts"
	"github.com/Herrscherd/herrscher/core/internal/atomicfile"
	"github.com/Herrscherd/herrscher/core/skills"
)

func learnedRoot(cwd string) string {
	return skills.LearnedRoot(cwd)
}

type skillSource interface {
	LearnedSkills(ctx context.Context) ([]contracts.Node, error)
}

type skillUser interface {
	SkillUsed(ctx context.Context, names []string)
}

// ProjectLearnedSkills writes the active learned skills under the learned root of cwd.
func ProjectLearnedSkills(ctx context.Context, orch contracts.Orchestrator, cwd string) {
	src, ok := orch.(skillSource)
	if !ok {
		return
	}
	if linked := skills.LinkedLearnedDir(cwd); linked != "" {
		logger.Warn("learned skills not projected: a directory of the projection is a symbolic link", "path", linked)
		return
	}
	nodes, err := src.LearnedSkills(ctx)
	if err != nil {
		logger.Warn("learned skills unavailable; this session runs on the skills already on disk", "err", err)
		return
	}
	removeLegacyProjection(cwd)
	if len(nodes) == 0 {
		pruneProjection(cwd, nil)
		return
	}
	root := learnedRoot(cwd)
	if err := os.MkdirAll(root, dirShared); err != nil {
		logger.Warn("cannot create the learned-skill root", "root", root, "err", err)
		return
	}
	kept := make(map[string]bool, len(nodes))
	for _, n := range nodes {
		name, body, ok := renderSkill(n)
		if !ok {
			continue
		}
		dir := filepath.Join(root, name)
		if err := writeSkillFile(dir, body); err != nil {
			logger.Warn("cannot write a learned skill", "dir", dir, "err", err)
			continue
		}
		kept[name] = true
	}
	pruneProjection(cwd, kept)
}

var errLinkedSkillDir = errors.New("the skill directory is a symbolic link")

func writeSkillFile(dir, body string) error {
	if info, err := os.Lstat(dir); err == nil && info.Mode()&os.ModeSymlink != 0 {
		return errLinkedSkillDir
	}
	if err := os.MkdirAll(dir, dirShared); err != nil {
		return err
	}
	return atomicfile.Write(filepath.Join(dir, "SKILL.md"), []byte(body), fileSecret)
}

func removeLegacyProjection(cwd string) {
	legacy := skills.LegacyLearnedRoot(cwd)
	if err := os.RemoveAll(legacy); err != nil {
		logger.Warn("cannot remove the learned skills projected under the former root", "root", legacy, "err", err)
	}
}

func pruneProjection(cwd string, kept map[string]bool) {
	root := learnedRoot(cwd)
	entries, err := os.ReadDir(root)
	if err != nil {
		return
	}
	for _, entry := range entries {
		if kept[entry.Name()] {
			continue
		}
		if err := os.RemoveAll(filepath.Join(root, entry.Name())); err != nil {
			logger.Warn("cannot remove a learned skill that is no longer active", "name", entry.Name(), "err", err)
		}
	}
}

func renderSkill(node contracts.Node) (string, string, bool) {
	if node.Kind != contracts.KindSkill {
		return "", "", false
	}
	name := node.Key[strings.LastIndex(node.Key, "/")+1:]
	body := strings.TrimSpace(node.Body)
	if body == "" || name == "" || name == "." || name == ".." ||
		strings.ContainsAny(name, `/\`) || strings.ContainsFunc(name, unsafeInName) {
		return "", "", false
	}
	var out strings.Builder
	out.WriteString("---\nname: ")
	out.WriteString(name)
	out.WriteString("\ndescription: ")
	out.WriteString(strings.Join(strings.Fields(node.Title), " "))
	out.WriteString("\n---\n")
	out.WriteString(body)
	_ = out.WriteByte('\n')
	return name, out.String(), true
}

func unsafeInName(r rune) bool { return unicode.IsSpace(r) || unicode.IsControl(r) }

func reportSkillUse(ctx context.Context, orch contracts.Orchestrator, names []string) {
	if len(names) == 0 {
		return
	}
	if u, ok := orch.(skillUser); ok {
		u.SkillUsed(ctx, names)
	}
}
