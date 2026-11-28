package bridge

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Herrscherd/herrscher-contracts/i18n"
	"github.com/Herrscherd/herrscher/core/internal/agent"
	"github.com/Herrscherd/herrscher/core/skills"
)

func writeAgentBudget(t *testing.T, agentsRoot, name, content string) {
	t.Helper()
	dir := filepath.Join(agentsRoot, name)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "skills-budget"), []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestSkillMenuNoticeIsLocalized(t *testing.T) {
	cases := []struct {
		lang i18n.Language
		want []string
	}{
		{lang: i18n.English, want: []string{"Skills hidden from this list: 3", "/work/.claude/skills, /home/.claude/skills"}},
		{lang: i18n.French, want: []string{"Compétences masquées de cette liste : 3", "/work/.claude/skills, /home/.claude/skills"}},
	}
	for _, tc := range cases {
		got := newSkillNotices(tc.lang, newBridgeMessages()).MenuMore(3, []string{"/work/.claude/skills", "/home/.claude/skills"})
		for _, want := range tc.want {
			if !strings.Contains(got, want) {
				t.Fatalf("%s menu notice must contain %q, got %q", tc.lang, want, got)
			}
		}
	}
}

func TestSkillExpansionCutIsLocalized(t *testing.T) {
	cases := []struct {
		lang i18n.Language
		want string
	}{
		{lang: i18n.English, want: "The pdf-fill skill is cut here"},
		{lang: i18n.French, want: "La compétence pdf-fill est coupée"},
	}
	for _, tc := range cases {
		got := newSkillNotices(tc.lang, newBridgeMessages()).ExpansionCut("pdf-fill", "/skills/pdf-fill/SKILL.md")
		if !strings.Contains(got, tc.want) || !strings.Contains(got, "/skills/pdf-fill/SKILL.md") {
			t.Fatalf("%s expansion notice must name the skill and its file, got %q", tc.lang, got)
		}
	}
}

func TestAgentSkillBudgetFollowsEachAgent(t *testing.T) {
	root := t.TempDir()
	writeAgentBudget(t, root, "alpha", `{"menuTokens":300,"expansionTokens":2000}`)
	writeAgentBudget(t, root, "beta", `{"menuTokens":900}`)
	if err := os.MkdirAll(filepath.Join(root, "gamma"), 0o700); err != nil {
		t.Fatal(err)
	}

	cases := []struct {
		name string
		want skills.Budget
	}{
		{name: "alpha", want: skills.Budget{Menu: 300, Expansions: 2000}},
		{name: "beta", want: skills.Budget{Menu: 900}},
		{name: "gamma", want: skills.Budget{}},
		{name: "", want: skills.Budget{}},
	}
	for _, tc := range cases {
		got, err := AgentSkillBudget(root, tc.name)
		if err != nil {
			t.Fatalf("agent %q: %v", tc.name, err)
		}
		if got != tc.want {
			t.Fatalf("agent %q budget = %+v, want %+v", tc.name, got, tc.want)
		}
	}
}

func TestAgentSkillBudgetMalformedIsALocalizedError(t *testing.T) {
	root := t.TempDir()
	writeAgentBudget(t, root, "alpha", `{"menuToken":300}`)

	_, err := AgentSkillBudget(root, "alpha")
	if err == nil {
		t.Fatal("a skills-budget file with an unknown field must be refused, not ignored")
	}
	if _, ok := errors.AsType[*agent.SkillsBudgetError](err); !ok {
		t.Fatalf("the error must be the typed skills budget error, got %T", err)
	}
	english := LocalizeSkillBudgetError(err, i18n.English).Error()
	if !strings.Contains(english, "The skills budget of agent alpha cannot be read") {
		t.Fatalf("english error must name the agent, got %q", english)
	}
	french := LocalizeSkillBudgetError(err, i18n.French).Error()
	if !strings.Contains(french, "Le budget des compétences de l'agent alpha est illisible") {
		t.Fatalf("french error must name the agent, got %q", french)
	}
}

func TestSkillMenuBelowItsFloorIsRefusedAtLoad(t *testing.T) {
	t.Setenv("HERRSCHER_STATE_DIR", t.TempDir())
	workspace := t.TempDir()
	writeBridgeSkill(t, workspace, "demo", "name: demo\ndescription: a demo\n", "DEMO BODY\n")

	_, err := newSkillEngineIn(plainBackend{}, workspace, skills.Budget{Menu: 1}, newBridgeMessages())
	if err == nil {
		t.Fatal("a menu budget below its fixed cost must be refused, not overflowed")
	}
	english := LocalizeSkillBudgetError(err, i18n.English).Error()
	if !strings.Contains(english, "is below the") || !strings.Contains(english, "menuTokens") {
		t.Fatalf("english refusal must say the budget is too small, got %q", english)
	}
	french := LocalizeSkillBudgetError(err, i18n.French).Error()
	if !strings.Contains(french, "est inférieur aux") {
		t.Fatalf("french refusal must say the budget is too small, got %q", french)
	}
}

func TestSkillMenuAboveItsFloorIsAccepted(t *testing.T) {
	t.Setenv("HERRSCHER_STATE_DIR", t.TempDir())
	workspace := t.TempDir()
	writeBridgeSkill(t, workspace, "demo", "name: demo\ndescription: a demo\n", "DEMO BODY\n")
	eng, err := newSkillEngineIn(plainBackend{}, workspace, skills.Budget{Menu: 100_000}, newBridgeMessages())
	if err != nil || eng == nil {
		t.Fatalf("a menu budget above its floor must build the engine, got %v, %v", eng, err)
	}
}
