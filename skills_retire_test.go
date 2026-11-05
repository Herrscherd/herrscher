package main

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"testing"
	"testing/fstest"

	contracts "github.com/Herrscherd/herrscher-contracts"
	"github.com/Herrscherd/herrscher/core/skills"
)

func TestPluginStillShippingASkillKeepsItAfterRetirement(t *testing.T) {
	dst := t.TempDir()
	plugin := []contracts.Plugin{{
		Manifest: contracts.Manifest{Kind: "fake"},
		Skills:   tree("demo", nil),
	}}
	first, _ := installPluginSkills(context.Background(), testTranslator(), plugin, noEnv, dst)
	if len(first.Installed) != 1 {
		t.Fatalf("first install = %+v, want demo installed", first)
	}
	out, _ := installPluginSkills(context.Background(), testTranslator(), plugin, noEnv, dst)
	retired, err := retireSkills(dst, out)
	if err != nil {
		t.Fatal(err)
	}
	if len(retired.Removed) != 0 {
		t.Fatalf("a skill a plugin still ships was retired: %+v", retired)
	}
	if _, err := os.Stat(filepath.Join(dst, "demo", "SKILL.md")); err != nil {
		t.Fatalf("the plugin's skill must survive: %v", err)
	}
}

func TestSkillOfAnUnloadedPluginIsNotRetired(t *testing.T) {
	dst := t.TempDir()
	installPluginSkills(context.Background(), testTranslator(), []contracts.Plugin{{
		Manifest: contracts.Manifest{Kind: "fake"},
		Skills:   tree("demo", nil),
	}}, noEnv, dst)
	broken := []contracts.Plugin{{
		Manifest: contracts.Manifest{Kind: "fake"},
		Skills: func(context.Context, contracts.PluginConfig) (fs.FS, error) {
			return nil, errors.New("the tool is not installed")
		},
	}}
	out, _ := installPluginSkills(context.Background(), testTranslator(), broken, noEnv, dst)
	if len(out.Partial) != 1 {
		t.Fatalf("a plugin that failed to load must be reported partial, got %+v", out)
	}
	retired, err := retireSkills(dst, out)
	if err != nil {
		t.Fatal(err)
	}
	if !retired.Empty() {
		t.Fatalf("nothing may be retired while a source is unread, got %+v", retired)
	}
	if _, err := os.Stat(filepath.Join(dst, "demo", "SKILL.md")); err != nil {
		t.Fatalf("a skill whose plugin failed this run must stay: %v", err)
	}
}

func TestRetiredSkillsAreReportedOnTheirOwn(t *testing.T) {
	dst := t.TempDir()
	if _, err := skills.Install(fstest.MapFS{"old/SKILL.md": &fstest.MapFile{Data: []byte("---\nname: old\n---\nx")}}, dst); err != nil {
		t.Fatal(err)
	}
	out, _ := installPluginSkills(context.Background(), testTranslator(), nil, noEnv, dst)
	retired, err := retireSkills(dst, out)
	if err != nil {
		t.Fatal(err)
	}
	if len(retired.Removed) != 1 || retired.Removed[0] != "old" {
		t.Fatalf("removed = %v, want old", retired.Removed)
	}
	if _, err := os.Stat(filepath.Join(dst, "old")); err == nil {
		t.Fatal("the retired skill must be gone from the directory")
	}
}
