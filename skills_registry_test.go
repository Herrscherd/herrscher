package main

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	contracts "github.com/Herrscherd/herrscher-contracts"
)

// The whole chain against the real composition: every plugin this binary was
// built with, through its own factory, onto disk. The unit tests use fakes and
// would not catch a plugin that registers a nil tree, embeds its playbooks under
// the wrong root, or ships a directory with no SKILL.md in it.
func TestTheCompiledInPluginsShipUsableSkills(t *testing.T) {
	dst := t.TempDir()
	out, notes := installPluginSkills(context.Background(), contracts.Default.Plugins(), os.Getenv, dst)

	if len(out.Installed) == 0 && len(notes) == 0 {
		t.Fatal("this build compiles in plugins that carry playbooks; none installed and nothing explained why")
	}
	for _, name := range out.Installed {
		if _, err := os.Stat(filepath.Join(dst, name, "SKILL.md")); err != nil {
			t.Errorf("installed skill %q has no SKILL.md: %v", name, err)
		}
	}
	// Nothing lands outside a skill directory: a plugin embedding its tree one
	// level too high would install a "skills" directory instead of its skills.
	entries, err := os.ReadDir(dst)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if e.Name() == "skills" {
			t.Error("a plugin shipped its tree with the repo's own skills/ prefix still on it")
		}
	}

	// The skills-category plugins are the point of this release: each one either
	// installed something or said why not, and neither is a silent no-op. Each is
	// run alone into its own directory, so the answer is that plugin's own — asked
	// of the aggregate, a plugin that quietly does nothing hides behind any other
	// plugin that installed.
	for _, p := range contracts.Default.Skills() {
		kind := p.Manifest.Kind
		if p.Skills == nil {
			t.Errorf("plugin %q is registered under the skills category and carries none", kind)
			continue
		}
		alone, why := installPluginSkills(context.Background(), []contracts.Plugin{p}, os.Getenv, t.TempDir())
		if alone.Empty() && len(why) == 0 {
			t.Errorf("plugin %q installed nothing and never said why", kind)
		}
	}
}
