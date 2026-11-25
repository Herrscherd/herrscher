package main

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/Herrscherd/herrscher/core/scope"
)

func sandboxAt(t *testing.T, parent string) sandbox {
	t.Helper()
	box, err := newSandbox(t.Context(), parent)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if closeErr := box.close(); closeErr != nil {
			t.Error(closeErr)
		}
	})
	return box
}

func TestEnvironDropsInheritedSecrets(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "leaked")
	box := sandboxAt(t, t.TempDir())
	env := box.environ("claude-haiku-5-5", "alpha", []string{"MEMORY_RAW_ARCHIVE=true"})
	for _, entry := range env {
		if strings.HasPrefix(entry, "ANTHROPIC_API_KEY=") {
			t.Fatalf("an inherited key reached the run: %s", entry)
		}
	}
	for _, want := range []string{
		"HOME=" + box.home,
		"CLAUDE_CONFIG_DIR=" + box.config,
		"HERRSCHER_STATE_DIR=" + box.state,
		"HERRSCHER_DEFAULT_MODEL=claude-haiku-5-5",
		"MEMORY_RAW_ARCHIVE=true",
	} {
		if !slices.Contains(env, want) {
			t.Errorf("environment lacks %q", want)
		}
	}
}

func TestEveryArmRunsInARepositoryThatNamesAProject(t *testing.T) {
	box := sandboxAt(t, t.TempDir())
	if got := scope.ProjectFromDir(t.Context(), box.repo); got != repoDir {
		t.Errorf("project of the run directory %q, want %q so facts land in a shared project scope", got, repoDir)
	}
}

func TestEnvironNamesTheMemoryAgentAfterTheStep(t *testing.T) {
	box := sandboxAt(t, t.TempDir())
	for _, agent := range []string{agentAlpha, agentBeta} {
		if env := box.environ("claude-haiku-5-5", agent, nil); !slices.Contains(env, memoryAgentEnv+"="+agent) {
			t.Errorf("environment of %s lacks its memory agent, so two agents would share one private scope", agent)
		}
	}
}

func TestEnvironKeepsTheVendorsOwnMemoryAndAccountOut(t *testing.T) {
	box := sandboxAt(t, t.TempDir())
	env := box.environ("claude-haiku-5-5", "alpha", nil)
	for _, want := range []string{
		"CLAUDE_CODE_DISABLE_AUTO_MEMORY=1",
		"ENABLE_CLAUDEAI_MCP_SERVERS=false",
		"CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC=1",
	} {
		if !slices.Contains(env, want) {
			t.Errorf("environment lacks %q, so the vendor could answer from its own memory or account", want)
		}
	}
}

func TestSandboxesAreDistinctAndClosed(t *testing.T) {
	parent := t.TempDir()
	first, err := newSandbox(t.Context(), parent)
	if err != nil {
		t.Fatal(err)
	}
	second, err := newSandbox(t.Context(), parent)
	if err != nil {
		t.Fatal(err)
	}
	if first.root == second.root || first.state == second.state {
		t.Fatalf("two arms share a sandbox: %q and %q", first.root, second.root)
	}
	if filepath.Dir(first.root) != parent {
		t.Errorf("sandbox %q is not under the run directory %q", first.root, parent)
	}
	if err = first.close(); err != nil {
		t.Fatal(err)
	}
	if _, statErr := os.Stat(first.root); !os.IsNotExist(statErr) {
		t.Errorf("closed sandbox still exists: %v", statErr)
	}
	if err = second.close(); err != nil {
		t.Fatal(err)
	}
}

func TestInstallCredentialsIsPrivate(t *testing.T) {
	source := filepath.Join(t.TempDir(), "credentials.json")
	const payload = `{"token":"placeholder"}`
	if err := os.WriteFile(source, []byte(payload), 0o600); err != nil {
		t.Fatal(err)
	}
	box := sandboxAt(t, t.TempDir())
	if err := box.installCredentials(source); err != nil {
		t.Fatal(err)
	}
	installed := filepath.Join(box.config, credentialsFile)
	info, err := os.Stat(installed)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Errorf("credentials mode %v, want 0600", info.Mode().Perm())
	}
	data, err := os.ReadFile(installed)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != payload {
		t.Errorf("installed credentials differ from the source")
	}
}

func TestPlantAgedNodeWritesTheRealFormat(t *testing.T) {
	box, err := newSandbox(t.Context(), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err = box.plantAgedNode(); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(box.vault(), agedNodeFile))
	if err != nil {
		t.Fatal(err)
	}
	for _, line := range []string{"type: fact", "title: old fact", "lastSeen: 2001-01-01T00:00:00Z"} {
		if !strings.Contains(string(data), line) {
			t.Errorf("aged node lacks %q:\n%s", line, data)
		}
	}
}
