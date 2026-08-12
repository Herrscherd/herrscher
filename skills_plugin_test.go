package main

import (
	"os"
	"path/filepath"
	"testing"
	"testing/fstest"

	contracts "github.com/Herrscherd/herrscher-contracts"
)

// A plugin's playbook lands where every backend looks for skills. This is what
// makes an alpha-gateway skill exist only on a machine whose build has it in it.
func TestPluginSkillsAreInstalled(t *testing.T) {
	dst := t.TempDir()
	out := installPluginSkills([]contracts.Plugin{{
		Manifest: contracts.Manifest{Kind: "fake", Category: contracts.CategoryGateway},
		Skills:   fstest.MapFS{"demo/SKILL.md": &fstest.MapFile{Data: []byte("# demo")}},
	}}, dst)
	if len(out.Installed) != 1 || out.Installed[0] != "demo" {
		t.Fatalf("the plugin's skill must be installed, got %+v", out)
	}
	b, err := os.ReadFile(filepath.Join(dst, "demo", "SKILL.md"))
	if err != nil {
		t.Fatalf("the skill must be readable where backends look: %v", err)
	}
	if string(b) != "# demo" {
		t.Fatalf("the skill's content must survive, got %q", b)
	}
}

// Yours stay yours: a file already there is never overwritten, matching what the
// shipped skills already promise.
func TestPluginSkillsNeverOverwrite(t *testing.T) {
	dst := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dst, "demo"), 0o755); err != nil {
		t.Fatal(err)
	}
	mine := filepath.Join(dst, "demo", "SKILL.md")
	if err := os.WriteFile(mine, []byte("mine"), 0o644); err != nil {
		t.Fatal(err)
	}
	out := installPluginSkills([]contracts.Plugin{{
		Manifest: contracts.Manifest{Kind: "fake"},
		Skills:   fstest.MapFS{"demo/SKILL.md": &fstest.MapFile{Data: []byte("# demo")}},
	}}, dst)
	b, _ := os.ReadFile(mine)
	if string(b) != "mine" {
		t.Fatalf("an existing skill must be left alone, got %q", b)
	}
	// Left alone, but not in silence: the plugin's own playbook is not in effect.
	if len(out.Diverged) != 1 || out.Diverged[0] != "demo" {
		t.Fatalf("diverged = %v, want demo reported", out.Diverged)
	}
}

// A plugin contributing no skills is the common case and must be free.
func TestPluginWithoutSkillsIsSkipped(t *testing.T) {
	dst := t.TempDir()
	if out := installPluginSkills([]contracts.Plugin{{Manifest: contracts.Manifest{Kind: "bare"}}}, dst); !out.Empty() {
		t.Fatalf("a plugin with no skills installs nothing, got %+v", out)
	}
	entries, _ := os.ReadDir(dst)
	if len(entries) != 0 {
		t.Fatalf("nothing must be created, found %d entries", len(entries))
	}
}
