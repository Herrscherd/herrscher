package main

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	contracts "github.com/Herrscherd/herrscher-contracts"
)

func TestTheCompiledInPluginsShipUsableSkills(t *testing.T) {
	dst := t.TempDir()
	out, notes := installPluginSkills(context.Background(), testTranslator(), contracts.Default.Plugins(), os.Getenv, dst)

	if len(out.Installed) == 0 && len(notes) == 0 {
		t.Fatal("this build compiles in plugins that carry playbooks; none installed and nothing explained why")
	}
	for _, name := range out.Installed {
		if _, err := os.Stat(filepath.Join(dst, name, "SKILL.md")); err != nil {
			t.Errorf("installed skill %q has no SKILL.md: %v", name, err)
		}
	}
	entries, err := os.ReadDir(dst)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if e.Name() == "skills" {
			t.Error("a plugin shipped its tree with the repo's own skills/ prefix still on it")
		}
	}

	for _, p := range contracts.Default.Skills() {
		kind := p.Manifest.Kind
		if p.Skills == nil {
			t.Errorf("plugin %q is registered under the skills category and carries none", kind)
			continue
		}
		alone, why := installPluginSkills(context.Background(), testTranslator(), []contracts.Plugin{p}, os.Getenv, t.TempDir())
		if alone.Empty() && len(why) == 0 {
			t.Errorf("plugin %q installed nothing and never said why", kind)
		}
	}
}
