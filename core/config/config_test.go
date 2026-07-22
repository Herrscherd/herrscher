package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadSessionMaxIdleDays(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "config.json")
	if err := os.WriteFile(p, []byte(`{"sessionMaxIdleDays": 7}`), 0o600); err != nil {
		t.Fatal(err)
	}
	c, err := Load(p)
	if err != nil {
		t.Fatal(err)
	}
	if c.SessionMaxIdleDays != 7 {
		t.Fatalf("SessionMaxIdleDays = %d, want 7", c.SessionMaxIdleDays)
	}
}

func TestLoadSkillsConfig(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	if err := os.WriteFile(path, []byte(`{"skills":{"enabled":false,"roots":["/opt/skills"]}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	c, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if c.Skills == nil || c.Skills.Enabled == nil || *c.Skills.Enabled {
		t.Fatalf("want skills.enabled=false, got %+v", c.Skills)
	}
	if len(c.Skills.Roots) != 1 || c.Skills.Roots[0] != "/opt/skills" {
		t.Fatalf("want roots=[/opt/skills], got %+v", c.Skills)
	}
}
