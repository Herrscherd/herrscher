package terminal

import (
	"context"
	"testing"

	contracts "github.com/Herrscherd/herrscher-contracts"
)

// cfgOf resolves the plugin's own declared settings against a fake environment,
// so the test exercises the defaults the manifest actually ships rather than a
// second copy of them.
func cfgOf(t *testing.T, env map[string]string) contracts.PluginConfig {
	t.Helper()
	cfg, err := contracts.Resolve(settings(), func(k string) string { return env[k] })
	if err != nil {
		t.Fatal(err)
	}
	return cfg
}

func TestDefaultSessionAsksToLearn(t *testing.T) {
	c := &fakeSessionControl{}
	if _, err := openDefaultSession(context.Background(), c, cfgOf(t, nil)); err != nil {
		t.Fatal(err)
	}
	if len(c.created) != 1 {
		t.Fatalf("created %d sessions, want 1", len(c.created))
	}
	spec := c.created[0]
	if spec.Extractor != "llm" {
		t.Fatalf("Extractor = %q, want llm — without one nothing is ever distilled", spec.Extractor)
	}
	if spec.ConsolidateEvery != 10 {
		t.Fatalf("ConsolidateEvery = %d, want 10", spec.ConsolidateEvery)
	}
	if spec.MemoryAgent != "tui" {
		t.Fatalf("MemoryAgent = %q, want tui", spec.MemoryAgent)
	}
	if spec.ProjectPinned {
		t.Fatal("a project resolved from a directory is a guess, not a choice")
	}
	if !spec.TerminalOnly || !spec.Shared {
		t.Fatal("a learning session is still a terminal tab in the main checkout")
	}
}

func TestLearnFalseCreatesTodaysSession(t *testing.T) {
	c := &fakeSessionControl{}
	if _, err := openDefaultSession(context.Background(), c, cfgOf(t, map[string]string{"TERMINAL_LEARN": "false"})); err != nil {
		t.Fatal(err)
	}
	spec := c.created[0]
	if spec.Extractor != "" || spec.ConsolidateEvery != 0 || spec.MemoryAgent != "" || spec.MemoryProject != "" {
		t.Fatalf("learn=false must send exactly what it sends today, got %+v", spec)
	}
}

func TestAnExplicitProjectIsPinned(t *testing.T) {
	c := &fakeSessionControl{}
	cfg := cfgOf(t, map[string]string{"TERMINAL_PROJECT": "neublox"})
	if _, err := openDefaultSession(context.Background(), c, cfg); err != nil {
		t.Fatal(err)
	}
	if c.created[0].MemoryProject != "neublox" || !c.created[0].ProjectPinned {
		t.Fatalf("an operator who named the project must not be second-guessed: %+v", c.created[0])
	}
}

func TestPinAtLaunchNeverAsksTheFirstPrompt(t *testing.T) {
	c := &fakeSessionControl{}
	cfg := cfgOf(t, map[string]string{"TERMINAL_PROJECT_PIN": "launch"})
	if _, err := openDefaultSession(context.Background(), c, cfg); err != nil {
		t.Fatal(err)
	}
	if !c.created[0].ProjectPinned {
		t.Fatal("project-pin=launch must pin what the directory gave")
	}
}

func TestAMemoryAgentIsOverridable(t *testing.T) {
	c := &fakeSessionControl{}
	cfg := cfgOf(t, map[string]string{"TERMINAL_MEMORY_AGENT": "scout"})
	if _, err := openDefaultSession(context.Background(), c, cfg); err != nil {
		t.Fatal(err)
	}
	if c.created[0].MemoryAgent != "scout" {
		t.Fatalf("MemoryAgent = %q, want scout", c.created[0].MemoryAgent)
	}
}

// A launch must survive a setting a human typed. session create validates the
// project name, so a value it would refuse has to be folded here or the window
// opens on nothing.
func TestAProjectNameWithASpaceIsFoldedNotRefused(t *testing.T) {
	c := &fakeSessionControl{}
	cfg := cfgOf(t, map[string]string{"TERMINAL_PROJECT": "Mon Projet"})
	if _, err := openDefaultSession(context.Background(), c, cfg); err != nil {
		t.Fatalf("openDefaultSession: %v", err)
	}
	if got := c.created[0].MemoryProject; got != "mon-projet" {
		t.Fatalf("MemoryProject = %q, want %q", got, "mon-projet")
	}
	if !c.created[0].ProjectPinned {
		t.Fatal("a project a human named must be pinned")
	}
}
