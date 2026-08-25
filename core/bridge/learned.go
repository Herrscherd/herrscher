package bridge

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"unicode"

	contracts "github.com/Herrscherd/herrscher-contracts"
)

// projectionDir is the worktree directory the learned-skill projection owns.
// It is deliberately not .claude/skills: the projection deletes what it no
// longer projects, so it must never share a directory with files a human wrote.
// Telling its own files from someone else's by a marker in the frontmatter would
// rest a deletion on a heuristic.
const projectionDir = ".herrscher"

// learnedRoot is the skill root the projection owns under a session worktree.
func learnedRoot(cwd string) string {
	return filepath.Join(cwd, projectionDir, "skills")
}

// skillSource is the OPTIONAL orchestrator capability that names the skills this
// session has learned. It is declared structurally rather than imported: the
// bridge must not depend on a concrete orchestrator, and the type assertion is
// the same idiom SetScope already uses.
type skillSource interface {
	LearnedSkills(ctx context.Context) ([]contracts.Node, error)
}

// skillUser is the OPTIONAL orchestrator capability that records a skill having
// been reached for. Structural, like skillSource, for the same reason.
type skillUser interface {
	SkillUsed(ctx context.Context, names []string)
}

// projectLearnedSkills renders every skill the orchestrator reports into the
// projection root and removes whatever it did not render, so an archived skill
// leaves the disk instead of outliving its own node.
//
// It runs once per session, before the skill engine reads its roots, so the very
// first turn's menu already carries them. A skill this session writes with the
// <skill> marker therefore appears at the next session, which is deliberate: the
// agent that just worked a procedure out already knows it, and re-rendering
// every turn would cost a memory query per turn for a directory that almost
// never changes.
//
// Best-effort throughout. An orchestrator without the capability, an unreachable
// vault, or an unwritable worktree all leave the session running on the skills
// already on disk, which is what it did before this existed.
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
		// Nothing to project, but an earlier session may have left files here.
		pruneProjection(cwd, nil)
		return
	}
	root := learnedRoot(cwd)
	if err := os.MkdirAll(root, 0o755); err != nil {
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
		if err := os.MkdirAll(dir, 0o755); err != nil {
			logger.Warn("cannot create a learned-skill directory", "dir", dir, "err", err)
			continue
		}
		if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(md), 0o644); err != nil {
			logger.Warn("cannot write a learned skill", "dir", dir, "err", err)
			continue
		}
		kept[name] = true
	}
	pruneProjection(cwd, kept)
}

// pruneProjection removes every entry of the projection root not named in kept.
// Being this blunt is safe precisely because the root belongs to nobody else; a
// missing root is not an error, it is a session that never learned anything.
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

// renderSkill turns one node into the name of its directory and the SKILL.md to
// write there. ok is false for anything that must not be projected.
//
// The description is collapsed to a single line first. Title arrives from the
// vault, which is multi-writer and fed by a journal carrying chat messages and
// web pages, so a newline in it would close the frontmatter block early and hand
// the rest of the string to the model as instructions.
func renderSkill(n contracts.Node) (name, md string, ok bool) {
	if n.Kind != contracts.KindSkill {
		return "", "", false
	}
	name = n.Key[strings.LastIndex(n.Key, "/")+1:]
	body := strings.TrimSpace(n.Body)
	// A name that is not a single plain path segment is refused rather than
	// sanitised: the orchestrator normalises every key it writes, so anything else
	// reaching here is a vault someone edited by hand, and guessing what they
	// meant would be how the projection writes outside its own root.
	//
	// Whitespace is refused for the reason the description is collapsed, one line
	// lower and with the roles reversed: a newline in the name closes the
	// frontmatter block early, and everything after it reaches the model as
	// instructions it believes came from the harness.
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

// unsafeInName reports whether r has no business in a skill's directory name:
// any space (a newline breaks out of the frontmatter, an ordinary space makes a
// path nobody can type) or any control character.
func unsafeInName(r rune) bool { return unicode.IsSpace(r) || unicode.IsControl(r) }

// reportSkillUse tells the orchestrator which skills this turn activated, so the
// staleness machine can tell a skill that serves from one nobody wants. A turn
// that activated nothing reports nothing rather than an empty call, so a session
// that never touches a skill costs no memory traffic at all.
func reportSkillUse(ctx context.Context, orch contracts.Orchestrator, names []string) {
	if len(names) == 0 {
		return
	}
	if u, ok := orch.(skillUser); ok {
		u.SkillUsed(ctx, names)
	}
}
