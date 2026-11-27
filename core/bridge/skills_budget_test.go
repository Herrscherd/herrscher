package bridge

import (
	"strings"
	"testing"

	"github.com/Herrscherd/herrscher-contracts/i18n"
	"github.com/Herrscherd/herrscher/core/config"
	"github.com/Herrscherd/herrscher/core/skills"
)

func TestSkillMenuNoticeIsLocalized(t *testing.T) {
	cases := []struct {
		lang i18n.Language
		want []string
	}{
		{lang: i18n.English, want: []string{"3 more skills exist", "/skills"}},
		{lang: i18n.French, want: []string{"3 autres compétences", "/skills"}},
	}
	for _, tc := range cases {
		got := newSkillNotices(tc.lang).MenuMore(3)
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
		got := newSkillNotices(tc.lang).ExpansionCut("pdf-fill", "/skills/pdf-fill/SKILL.md")
		if !strings.Contains(got, tc.want) || !strings.Contains(got, "/skills/pdf-fill/SKILL.md") {
			t.Fatalf("%s expansion notice must name the skill and its file, got %q", tc.lang, got)
		}
	}
}

func TestSkillBudgetFromConfig(t *testing.T) {
	if got := skillBudget(nil); got != (skills.Budget{}) {
		t.Fatalf("no skills config must mean no budget, got %+v", got)
	}
	if got := skillBudget(&config.SkillsConfig{}); got != (skills.Budget{}) {
		t.Fatalf("skills config without budget must mean no budget, got %+v", got)
	}
	cfg := &config.SkillsConfig{Budget: &config.SkillsBudgetConfig{MenuTokens: 300, ExpansionTokens: 2000}}
	if got := skillBudget(cfg); got.Menu != 300 || got.Expansions != 2000 {
		t.Fatalf("budget must follow config, got %+v", got)
	}
}
