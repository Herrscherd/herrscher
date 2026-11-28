package agent

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func writeSkillsBudget(t *testing.T, root, name, content string) {
	t.Helper()
	dir := filepath.Join(root, name)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, skillsBudgetFile), []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestSkillsBudgetAbsentFileMeansOff(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "alpha"), 0o700); err != nil {
		t.Fatal(err)
	}
	got, err := NewStore(root).SkillsBudget("alpha")
	if err != nil || got != (SkillsBudget{}) {
		t.Fatalf("an agent without the file must have no budget, got %+v, %v", got, err)
	}
}

func TestSkillsBudgetReadsBothCaps(t *testing.T) {
	root := t.TempDir()
	writeSkillsBudget(t, root, "alpha", `{"menuTokens": 300, "expansionTokens": 2000}`)
	got, err := NewStore(root).SkillsBudget("alpha")
	if err != nil {
		t.Fatalf("SkillsBudget: %v", err)
	}
	if got != (SkillsBudget{MenuTokens: 300, ExpansionTokens: 2000}) {
		t.Fatalf("budget = %+v", got)
	}
}

func TestSkillsBudgetZeroFieldLeavesThatBlockUnbounded(t *testing.T) {
	root := t.TempDir()
	writeSkillsBudget(t, root, "alpha", `{"menuTokens": 300}`)
	got, err := NewStore(root).SkillsBudget("alpha")
	if err != nil || got != (SkillsBudget{MenuTokens: 300}) {
		t.Fatalf("a missing cap must read as zero, got %+v, %v", got, err)
	}
}

func TestSkillsBudgetRefusesBrokenFiles(t *testing.T) {
	cases := map[string]string{
		"not json":        `{not json`,
		"unknown field":   `{"menuToken": 300}`,
		"trailing data":   `{"menuTokens": 300} {}`,
		"negative menu":   `{"menuTokens": -1}`,
		"negative expand": `{"expansionTokens": -5}`,
		"wrong type":      `{"menuTokens": "300"}`,
	}
	for label, content := range cases {
		t.Run(label, func(t *testing.T) {
			root := t.TempDir()
			writeSkillsBudget(t, root, "alpha", content)
			_, err := NewStore(root).SkillsBudget("alpha")
			var broken *SkillsBudgetError
			if !errors.As(err, &broken) {
				t.Fatalf("%s must be a *SkillsBudgetError, got %v", label, err)
			}
			if broken.Name != "alpha" {
				t.Fatalf("the error must name the agent, got %q", broken.Name)
			}
		})
	}
}

func TestSkillsBudgetRejectsInvalidAgentName(t *testing.T) {
	_, err := NewStore(t.TempDir()).SkillsBudget("../escape")
	if _, ok := errors.AsType[*InvalidNameError](err); !ok {
		t.Fatalf("an invalid agent name must be refused before any read, got %v", err)
	}
}
