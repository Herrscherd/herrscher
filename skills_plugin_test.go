package main

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"

	contracts "github.com/Herrscherd/herrscher-contracts"
)

// tree is a skills factory that hands over a one-skill tree and records the
// config it was given.
func tree(name string, seen *contracts.PluginConfig) contracts.SkillsFactory {
	return func(_ context.Context, cfg contracts.PluginConfig) (fs.FS, error) {
		if seen != nil {
			*seen = cfg
		}
		return fstest.MapFS{name + "/SKILL.md": &fstest.MapFile{Data: []byte("# " + name)}}, nil
	}
}

func noEnv(string) string { return "" }

// A plugin's playbook lands where every backend looks for skills. This is what
// makes an alpha-gateway skill exist only on a machine whose build has it in it.
func TestPluginSkillsAreInstalled(t *testing.T) {
	dst := t.TempDir()
	out, notes := installPluginSkills(context.Background(), []contracts.Plugin{{
		Manifest: contracts.Manifest{Kind: "fake", Category: contracts.CategoryGateway},
		Skills:   tree("demo", nil),
	}}, noEnv, dst)
	if len(notes) != 0 {
		t.Fatalf("a plugin that shipped its skill has nothing to explain, got %v", notes)
	}
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
	out, _ := installPluginSkills(context.Background(), []contracts.Plugin{{
		Manifest: contracts.Manifest{Kind: "fake"},
		Skills:   tree("demo", nil),
	}}, noEnv, dst)
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
	out, notes := installPluginSkills(context.Background(), []contracts.Plugin{{
		Manifest: contracts.Manifest{Kind: "bare"},
	}}, noEnv, dst)
	if !out.Empty() || len(notes) != 0 {
		t.Fatalf("a plugin with no skills installs nothing and says nothing, got %+v %v", out, notes)
	}
	entries, _ := os.ReadDir(dst)
	if len(entries) != 0 {
		t.Fatalf("nothing must be created, found %d entries", len(entries))
	}
}

// The regression this whole change could introduce. A gateway declares a required
// token; on a machine that has none it never instantiates — and its playbook must
// still land, because the skill teaches an agent about a capability this build
// has, not about a channel that happens to be reachable today. The factory gets
// what could be resolved and nothing more.
func TestAGatewayMissingItsTokenStillShipsItsPlaybook(t *testing.T) {
	dst := t.TempDir()
	var seen contracts.PluginConfig
	out, notes := installPluginSkills(context.Background(), []contracts.Plugin{{
		Manifest: contracts.Manifest{
			Kind:     "chat",
			Category: contracts.CategoryGateway,
			Config: []contracts.Setting{
				{Key: "token", Env: "CHAT_TOKEN", Required: true},
				{Key: "verbosity", Env: "CHAT_VERBOSITY", Default: "quiet"},
			},
		},
		Skills: tree("chatting", &seen),
	}}, noEnv, dst)
	if len(out.Installed) != 1 || out.Installed[0] != "chatting" {
		t.Fatalf("a tokenless gateway must still ship its playbook, got %+v (%v)", out, notes)
	}
	if seen.Get("verbosity") != "quiet" {
		t.Errorf("the factory should still see what did resolve, got %+v", seen.Settings)
	}
	if seen.Get("token") != "" {
		t.Errorf("nothing should be invented for the missing key, got %q", seen.Get("token"))
	}
}

// A plugin whose whole contribution is skills owns its own configuration, so an
// unconfigured one is skipped and named — in the same words the gateway hub uses,
// because the operator fixes it the same way.
func TestAnUnconfiguredSkillsPluginIsSkipped(t *testing.T) {
	dst := t.TempDir()
	out, notes := installPluginSkills(context.Background(), []contracts.Plugin{{
		Manifest: contracts.Manifest{
			Kind:     "superset",
			Category: contracts.CategorySkills,
			Config:   []contracts.Setting{{Key: "home", Env: "SUPERSET_HOME", Required: true}},
		},
		Skills: tree("superset-session", nil),
	}}, noEnv, dst)
	if !out.Empty() {
		t.Fatalf("an unconfigured skills plugin installs nothing, got %+v", out)
	}
	if len(notes) != 1 {
		t.Fatalf("it must be named once, got %v", notes)
	}
	for _, want := range []string{"superset", "missing required config", "home", "SUPERSET_HOME"} {
		if !strings.Contains(notes[0], want) {
			t.Errorf("the note should carry %q, got %q", want, notes[0])
		}
	}
}

// A factory declining is the point of the contract: no Superset on this machine,
// no Superset playbook in every agent's context. It is reported, and it does not
// take the other plugins' skills down with it.
func TestASkillsFactoryThatDeclinesIsReportedAndIsolated(t *testing.T) {
	dst := t.TempDir()
	out, notes := installPluginSkills(context.Background(), []contracts.Plugin{
		{
			Manifest: contracts.Manifest{Kind: "superset", Category: contracts.CategorySkills},
			Skills: func(context.Context, contracts.PluginConfig) (fs.FS, error) {
				return nil, errors.New("no Superset install at /nowhere")
			},
		},
		{
			Manifest: contracts.Manifest{Kind: "fake", Category: contracts.CategoryGateway},
			Skills:   tree("demo", nil),
		},
	}, noEnv, dst)
	if len(out.Installed) != 1 || out.Installed[0] != "demo" {
		t.Fatalf("one plugin declining must not stop the next, got %+v", out)
	}
	if len(notes) != 1 || !strings.Contains(notes[0], "superset") || !strings.Contains(notes[0], "/nowhere") {
		t.Fatalf("the refusal must be reported with its reason, got %v", notes)
	}
	if _, err := os.Stat(filepath.Join(dst, "superset-session")); !os.IsNotExist(err) {
		t.Error("a declined plugin must leave nothing behind")
	}
}

// A factory that returns neither a tree nor an error is a broken plugin, not a
// panic in the daemon's startup path.
func TestASkillsFactoryReturningNothingIsReported(t *testing.T) {
	dst := t.TempDir()
	out, notes := installPluginSkills(context.Background(), []contracts.Plugin{{
		Manifest: contracts.Manifest{Kind: "empty", Category: contracts.CategorySkills},
		Skills: func(context.Context, contracts.PluginConfig) (fs.FS, error) {
			return nil, nil
		},
	}}, noEnv, dst)
	if !out.Empty() {
		t.Fatalf("nothing to install, got %+v", out)
	}
	if len(notes) != 1 || !strings.Contains(notes[0], "empty") {
		t.Fatalf("a factory handing over no tree must be named, got %v", notes)
	}
}
