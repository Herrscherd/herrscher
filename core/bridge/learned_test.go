package bridge

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	contracts "github.com/Herrscherd/herrscher-contracts"
	"github.com/Herrscherd/herrscher/core/eventwire"
	"github.com/Herrscherd/herrscher/core/skills"
)

type learnedOrch struct {
	plainOrch

	nodes []contracts.Node
	err   error
	used  [][]string
}

func (o *learnedOrch) LearnedSkills(context.Context) ([]contracts.Node, error) {
	return o.nodes, o.err
}

func (o *learnedOrch) SkillUsed(_ context.Context, names []string) {
	o.used = append(o.used, append([]string(nil), names...))
}

func learnedNode(key, title, body string) contracts.Node {
	return contracts.Node{Key: key, Kind: contracts.KindSkill, Title: title, Body: body}
}

func TestProjectionRoundTripsThroughDiscover(t *testing.T) {
	cwd := t.TempDir()
	orch := &learnedOrch{nodes: []contracts.Node{
		learnedNode("agents/a/skills/retry-http", "wait out a 429 before retrying", "Read Retry-After, sleep, retry once."),
	}}

	ProjectLearnedSkills(context.Background(), orch, cwd)

	found := skills.Discover([]string{learnedRoot(cwd)})
	if len(found) != 1 {
		t.Fatalf("Discover found %d skills, want 1", len(found))
	}
	if found[0].Name != "retry-http" {
		t.Errorf("Name = %q, want %q", found[0].Name, "retry-http")
	}
	if found[0].Description != "wait out a 429 before retrying" {
		t.Errorf("Description = %q", found[0].Description)
	}
	body, err := found[0].Body()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(body, "Read Retry-After") {
		t.Errorf("Body = %q", body)
	}
}

func TestProjectionIsStableAcrossRuns(t *testing.T) {
	cwd := t.TempDir()
	orch := &learnedOrch{nodes: []contracts.Node{learnedNode("agents/a/skills/x", "t", "b")}}
	path := filepath.Join(learnedRoot(cwd), "x", "SKILL.md")

	ProjectLearnedSkills(context.Background(), orch, cwd)
	first, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	ProjectLearnedSkills(context.Background(), orch, cwd)
	second, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(first, second) {
		t.Errorf("the same node rendered differently twice:\n%q\n%q", first, second)
	}
}

func TestProjectionRemovesWhatIsNoLongerProjected(t *testing.T) {
	cwd := t.TempDir()
	orch := &learnedOrch{nodes: []contracts.Node{learnedNode("agents/a/skills/gone", "t", "b")}}
	ProjectLearnedSkills(context.Background(), orch, cwd)
	if len(skills.Discover([]string{learnedRoot(cwd)})) != 1 {
		t.Fatal("setup: the skill was not projected")
	}

	orch.nodes = nil
	ProjectLearnedSkills(context.Background(), orch, cwd)

	if got := skills.Discover([]string{learnedRoot(cwd)}); len(got) != 0 {
		t.Errorf("an archived skill still lives on disk: %v", got)
	}
}

func TestProjectionTouchesNothingOutsideItsOwnRoot(t *testing.T) {
	cwd := t.TempDir()
	handWritten := filepath.Join(cwd, ".claude", "skills", "mine")
	if err := os.MkdirAll(handWritten, 0o755); err != nil {
		t.Fatal(err)
	}
	md := "---\nname: mine\ndescription: written by a human\n---\nhands off\n"
	if err := os.WriteFile(filepath.Join(handWritten, "SKILL.md"), []byte(md), 0o644); err != nil {
		t.Fatal(err)
	}

	orch := &learnedOrch{nodes: []contracts.Node{learnedNode("agents/a/skills/learned", "t", "b")}}
	ProjectLearnedSkills(context.Background(), orch, cwd)
	orch.nodes = nil
	ProjectLearnedSkills(context.Background(), orch, cwd)

	if _, err := os.Stat(filepath.Join(handWritten, "SKILL.md")); err != nil {
		t.Fatalf("the projection destroyed a hand-written skill: %v", err)
	}
}

func TestProjectionIsSilentWithoutTheCapability(t *testing.T) {
	cwd := t.TempDir()
	ProjectLearnedSkills(context.Background(), plainOrch{}, cwd)
	ProjectLearnedSkills(context.Background(), nil, cwd)
	if _, err := os.Stat(learnedRoot(cwd)); !os.IsNotExist(err) {
		t.Errorf("an orchestrator without the capability still made a root: %v", err)
	}
}

func TestProjectionSurvivesAnUnreachableVault(t *testing.T) {
	cwd := t.TempDir()
	ProjectLearnedSkills(context.Background(), &learnedOrch{err: errors.New("vault unreachable")}, cwd)
	if _, err := os.Stat(learnedRoot(cwd)); !os.IsNotExist(err) {
		t.Errorf("wrote a root despite the error: %v", err)
	}
}

func TestRenderSkillRefusesWhatMustNotBeProjected(t *testing.T) {
	cases := []struct {
		name string
		node contracts.Node
		ok   bool
	}{
		{"plain", learnedNode("agents/a/skills/x", "a description", "body"), true},
		{"no name in the key", learnedNode("agents/a/skills/", "d", "b"), false},
		{"empty body", learnedNode("agents/a/skills/x", "d", "   "), false},
		{"not a skill", contracts.Node{Key: "agents/a/skills/x", Kind: contracts.KindDecision, Body: "b"}, false},
		{"a traversal in the tail", contracts.Node{Key: "agents/a/skills/..", Kind: contracts.KindSkill, Body: "b"}, false},
		{"a separator in the tail", contracts.Node{Key: `agents/a/skills/x\y`, Kind: contracts.KindSkill, Body: "b"}, false},
		{"a newline in the tail", contracts.Node{Key: "agents/a/skills/x\ndescription: hijacked", Kind: contracts.KindSkill, Body: "b"}, false},
		{"a space in the tail", contracts.Node{Key: "agents/a/skills/x y", Kind: contracts.KindSkill, Body: "b"}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, _, ok := renderSkill(tc.node); ok != tc.ok {
				t.Errorf("ok = %v, want %v", ok, tc.ok)
			}
		})
	}
}

func TestRenderSkillCollapsesADescriptionThatWouldEscape(t *testing.T) {
	n := learnedNode("agents/a/skills/x", "line one\n---\nname: impostor\ndescription: hijacked", "body")

	_, md, ok := renderSkill(n)
	if !ok {
		t.Fatal("refused a renderable skill")
	}
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "x"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "x", "SKILL.md"), []byte(md), 0o644); err != nil {
		t.Fatal(err)
	}
	found := skills.Discover([]string{dir})
	if len(found) != 1 {
		t.Fatalf("unparseable frontmatter:\n%s", md)
	}
	if found[0].Name != "x" {
		t.Errorf("name = %q, want %q; the description broke out of its block", found[0].Name, "x")
	}
	if strings.Contains(found[0].Description, "\n") {
		t.Errorf("description spans lines: %q", found[0].Description)
	}
}

func TestSkillRootsPutsLearnedBetweenRepoAndGlobal(t *testing.T) {
	roots := skillRoots("/wt", []string{"/extra"})

	if len(roots) != 4 {
		t.Fatalf("%d roots, want 4: %v", len(roots), roots)
	}
	if roots[0] != filepath.Join(string(filepath.Separator), "wt", ".claude", "skills") {
		t.Errorf("roots[0] = %q; a repository skill must win", roots[0])
	}
	if roots[1] != learnedRoot("/wt") {
		t.Errorf("roots[1] = %q, want the projection root", roots[1])
	}
	if strings.HasPrefix(roots[2], "/wt") || !strings.HasSuffix(roots[2], filepath.Join(".claude", "skills")) {
		t.Errorf("roots[2] = %q, want the user-global root", roots[2])
	}
	if roots[3] != "/extra" {
		t.Errorf("roots[3] = %q", roots[3])
	}
}

func TestReportSkillUseForwardsOnlyWhatWasActivated(t *testing.T) {
	o := &learnedOrch{}

	reportSkillUse(context.Background(), o, []string{"alpha", "beta"})
	reportSkillUse(context.Background(), o, nil)

	if len(o.used) != 1 {
		t.Fatalf("forwarded %d times, want 1 (an idle turn must not cost a memory write): %v", len(o.used), o.used)
	}
	if len(o.used[0]) != 2 || o.used[0][0] != "alpha" || o.used[0][1] != "beta" {
		t.Errorf("forwarded %v", o.used[0])
	}
}

func TestReportSkillUseIsSilentWithoutTheCapability(_ *testing.T) {
	reportSkillUse(context.Background(), plainOrch{}, []string{"alpha"})
	reportSkillUse(context.Background(), nil, []string{"alpha"})
}

func TestProjectionRemovesTheFormerRoot(t *testing.T) {
	cwd := t.TempDir()
	legacy := filepath.Join(skills.LegacyLearnedRoot(cwd), "retry-http", "SKILL.md")
	if err := os.MkdirAll(filepath.Dir(legacy), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(legacy, []byte("---\nname: retry-http\n---\nold\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	orch := &learnedOrch{nodes: []contracts.Node{learnedNode("agents/a/skills/retry-http", "wait out a 429", "Retry once.")}}

	ProjectLearnedSkills(context.Background(), orch, cwd)

	if _, err := os.Stat(skills.LegacyLearnedRoot(cwd)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("former root still present (%v), so a stale copy of the skill outlives its node", err)
	}
	if got := skills.Discover([]string{learnedRoot(cwd)}); len(got) != 1 {
		t.Fatalf("Discover found %d skills under the new root, want 1", len(got))
	}
}

func TestProjectionRefusesALinkedHome(t *testing.T) {
	outside := t.TempDir()
	kept := filepath.Join(outside, "skills", "mine", "SKILL.md")
	if err := os.MkdirAll(filepath.Dir(kept), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(kept, []byte("---\nname: mine\n---\nmine\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	cwd := t.TempDir()
	if err := os.Symlink(outside, skills.LearnedHome(cwd)); err != nil {
		t.Fatal(err)
	}
	orch := &learnedOrch{nodes: []contracts.Node{learnedNode("agents/a/skills/retry-http", "wait out a 429", "Retry once.")}}

	ProjectLearnedSkills(context.Background(), orch, cwd)

	if _, err := os.Stat(kept); err != nil {
		t.Fatalf("a file outside the worktree was removed through a linked .herrscher: %v", err)
	}
	if _, err := os.Stat(filepath.Join(outside, ".claude")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("the projection wrote outside the worktree through a linked .herrscher (%v)", err)
	}
}

func TestProjectionRefusesALinkedSkillDirectory(t *testing.T) {
	outside := t.TempDir()
	cwd := t.TempDir()
	if err := os.MkdirAll(learnedRoot(cwd), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(learnedRoot(cwd), "retry-http")); err != nil {
		t.Fatal(err)
	}
	orch := &learnedOrch{nodes: []contracts.Node{learnedNode("agents/a/skills/retry-http", "wait out a 429", "Retry once.")}}

	ProjectLearnedSkills(context.Background(), orch, cwd)

	if _, err := os.Stat(filepath.Join(outside, "SKILL.md")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("the skill was written outside the worktree through a linked skill directory (%v)", err)
	}
}

func TestProjectionReplacesALinkedSkillFileWithoutWritingItsTarget(t *testing.T) {
	victim := filepath.Join(t.TempDir(), "precious")
	if err := os.WriteFile(victim, []byte("precious\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	cwd := t.TempDir()
	dir := filepath.Join(learnedRoot(cwd), "retry-http")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(victim, filepath.Join(dir, "SKILL.md")); err != nil {
		t.Fatal(err)
	}
	orch := &learnedOrch{nodes: []contracts.Node{learnedNode("agents/a/skills/retry-http", "wait out a 429", "Retry once.")}}

	ProjectLearnedSkills(context.Background(), orch, cwd)

	if got, err := os.ReadFile(victim); err != nil || string(got) != "precious\n" {
		t.Fatalf("the target of a linked SKILL.md was overwritten: %q, %v", got, err)
	}
	info, err := os.Lstat(filepath.Join(dir, "SKILL.md"))
	if err != nil || info.Mode()&os.ModeSymlink != 0 {
		t.Fatalf("SKILL.md is still a link (%v, %v), want the projected file", info, err)
	}
}

func TestProjectionKeepsTheFormerRootWhenTheVaultFails(t *testing.T) {
	cwd := t.TempDir()
	legacy := filepath.Join(skills.LegacyLearnedRoot(cwd), "retry-http", "SKILL.md")
	if err := os.MkdirAll(filepath.Dir(legacy), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(legacy, []byte("---\nname: retry-http\n---\nold\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	ProjectLearnedSkills(context.Background(), &learnedOrch{err: errors.New("vault down")}, cwd)

	if _, err := os.Stat(legacy); err != nil {
		t.Fatalf("the former projection was removed while nothing replaced it: %v", err)
	}
}

func TestRunUntilClosedInListsTheSkillsOfItsDirectory(t *testing.T) {
	t.Setenv("HERRSCHER_STATE_DIR", t.TempDir())
	t.Chdir(t.TempDir())
	dir := t.TempDir()
	ProjectLearnedSkills(context.Background(), &learnedOrch{nodes: []contracts.Node{learnedNode("agents/a/skills/brask-release", "release the Brask repository", "Run make tag, then make ship ZONE=4.")}}, dir)
	in := make(chan contracts.Event, 1)
	in <- contracts.Event{T: eventwire.EventInput, Text: "release it"}
	close(in)
	be := &captureBackend{reply: "ok"}

	if err := RunUntilClosedIn(context.Background(), dir, func(string) (contracts.Backend, error) { return be, nil }, nil, "channel", in, make(chan contracts.Event, 8)); err != nil {
		t.Fatalf("RunUntilClosedIn: %v", err)
	}
	if len(be.contexts) != 1 || !strings.Contains(be.contexts[0], "brask-release") {
		t.Fatalf("turn context %q lists no skill of the session directory, the working directory being elsewhere", be.contexts)
	}
}
