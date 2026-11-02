package bridge

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"unicode"

	contracts "github.com/Herrscherd/herrscher-contracts"
)

const projectionDir = ".herrscher"

func learnedRoot(cwd string) string {
	return filepath.Join(cwd, projectionDir, "skills")
}

type skillSource interface {
	LearnedSkills(ctx context.Context) ([]contracts.Node, error)
}

type skillUser interface {
	SkillUsed(ctx context.Context, names []string)
}

func projectLearnedSkills(ctx context.Context, orch contracts.Orchestrator, cwd string) {
	src, ok := orch.(skillSource)
	if !ok {
		return
	}
	nodes, err := src.LearnedSkills(ctx)
	if err != nil {
		logger.Warn("learned skills unavailable; this session runs on the skills already on disk", "err", err)
		return
	}
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
		name, md, ok := renderSkill(n)
		if !ok {
			continue
		}
		dir := filepath.Join(root, name)
		if err := os.MkdirAll(dir, dirShared); err != nil {
			logger.Warn("cannot create a learned-skill directory", "dir", dir, "err", err)
			continue
		}
		if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(md), fileSecret); err != nil {
			logger.Warn("cannot write a learned skill", "dir", dir, "err", err)
			continue
		}
		kept[name] = true
	}
	pruneProjection(cwd, kept)
}

func pruneProjection(cwd string, kept map[string]bool) {
	root := learnedRoot(cwd)
	entries, err := os.ReadDir(root)
	if err != nil {
		return
	}
	for _, e := range entries {
		if kept[e.Name()] {
			continue
		}
		if err := os.RemoveAll(filepath.Join(root, e.Name())); err != nil {
			logger.Warn("cannot remove a learned skill that is no longer active", "name", e.Name(), "err", err)
		}
	}
}

func renderSkill(n contracts.Node) (string, string, bool) {
	if n.Kind != contracts.KindSkill {
		return "", "", false
	}
	name := n.Key[strings.LastIndex(n.Key, "/")+1:]
	body := strings.TrimSpace(n.Body)
	if body == "" || name == "" || name == "." || name == ".." ||
		strings.ContainsAny(name, `/\`) || strings.ContainsFunc(name, unsafeInName) {
		return "", "", false
	}
	var b strings.Builder
	b.WriteString("---\nname: ")
	b.WriteString(name)
	b.WriteString("\ndescription: ")
	b.WriteString(strings.Join(strings.Fields(n.Title), " "))
	b.WriteString("\n---\n")
	b.WriteString(body)
	b.WriteByte('\n')
	return name, b.String(), true
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
