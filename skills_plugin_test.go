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

func tree(name string, seen *contracts.PluginConfig) contracts.SkillsFactory {
	return func(_ context.Context, cfg contracts.PluginConfig) (fs.FS, error) {
		if seen != nil {
			*seen = cfg
		}
		return fstest.MapFS{name + "/SKILL.md": &fstest.MapFile{Data: []byte("# " + name)}}, nil
	}
}

func noEnv(string) string { return "" }

func treeOf(files map[string]string) contracts.SkillsFactory {
	return func(context.Context, contracts.PluginConfig) (fs.FS, error) {
		m := fstest.MapFS{}
		for name, body := range files {
			m[name+"/SKILL.md"] = &fstest.MapFile{Data: []byte(body)}
		}
		return m, nil
	}
}

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
	b := mustReadFile(t, mine)
	if string(b) != "mine" {
		t.Fatalf("an existing skill must be left alone, got %q", b)
	}
	if len(out.Diverged) != 1 || out.Diverged[0] != "demo" {
		t.Fatalf("diverged = %v, want demo reported", out.Diverged)
	}
}

func TestPluginWithoutSkillsIsSkipped(t *testing.T) {
	dst := t.TempDir()
	out, notes := installPluginSkills(context.Background(), []contracts.Plugin{{
		Manifest: contracts.Manifest{Kind: "bare"},
	}}, noEnv, dst)
	if !out.Empty() || len(notes) != 0 {
		t.Fatalf("a plugin with no skills installs nothing and says nothing, got %+v %v", out, notes)
	}
	entries := mustReadDir(t, dst)
	if len(entries) != 0 {
		t.Fatalf("nothing must be created, found %d entries", len(entries))
	}
}

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

func TestTwoPluginsClaimingOneSkillNameDoNotFightOverIt(t *testing.T) {
	dst := t.TempDir()
	plugs := []contracts.Plugin{
		{Manifest: contracts.Manifest{Kind: "first"}, Skills: treeOf(map[string]string{"demo": "# from first"})},
		{Manifest: contracts.Manifest{Kind: "second"}, Skills: treeOf(map[string]string{"demo": "# from second"})},
	}

	out, notes := installPluginSkills(context.Background(), plugs, noEnv, dst)
	if len(out.Installed) != 1 || out.Installed[0] != "demo" {
		t.Fatalf("the first claim must be installed once, got %+v", out)
	}
	if len(notes) != 1 || !strings.Contains(notes[0], "second") || !strings.Contains(notes[0], "first") {
		t.Fatalf("the clash must name both plugins, got %v", notes)
	}
	if b := mustReadFile(t, filepath.Join(dst, "demo", "SKILL.md")); string(b) != "# from first" {
		t.Fatalf("the first claim must be the one on disk, got %q", b)
	}

	again, _ := installPluginSkills(context.Background(), plugs, noEnv, dst)
	if !again.Empty() {
		t.Fatalf("a second start must find nothing to do, got %+v", again)
	}
}

func TestOnlyTheClashingSkillIsLeftOut(t *testing.T) {
	dst := t.TempDir()
	out, notes := installPluginSkills(context.Background(), []contracts.Plugin{
		{Manifest: contracts.Manifest{Kind: "first"}, Skills: treeOf(map[string]string{"demo": "# from first"})},
		{Manifest: contracts.Manifest{Kind: "second"}, Skills: treeOf(map[string]string{
			"demo": "# from second", "other": "# other",
		})},
	}, noEnv, dst)

	if len(out.Installed) != 2 {
		t.Fatalf("the clash costs one skill, not the plugin, got %+v (%v)", out, notes)
	}
	if b := mustReadFile(t, filepath.Join(dst, "other", "SKILL.md")); string(b) != "# other" {
		t.Fatalf("the plugin's own skill must still land, got %q", b)
	}
	if b := mustReadFile(t, filepath.Join(dst, "demo", "SKILL.md")); string(b) != "# from first" {
		t.Fatalf("the clashing one must stay with its first claimant, got %q", b)
	}
}
