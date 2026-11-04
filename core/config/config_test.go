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

func TestLoadCompactAtBounds(t *testing.T) {
	cases := []struct {
		name    string
		body    string
		want    int
		wantErr bool
	}{
		{name: "above 100 is refused", body: `{"compactAt": 150}`, wantErr: true},
		{name: "negative is refused", body: `{"compactAt": -1}`, wantErr: true},
		{name: "zero disables", body: `{"compactAt": 0}`, want: 0},
		{name: "default percent is accepted", body: `{"compactAt": 92}`, want: 92},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			checkCompactAt(t, tc.body, tc.want, tc.wantErr)
		})
	}
}

func checkCompactAt(t *testing.T, body string, want int, wantErr bool) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(path)
	if wantErr {
		if err == nil {
			t.Fatalf("Load(%s) accepted an out-of-range compactAt", body)
		}
		return
	}
	if err != nil {
		t.Fatalf("Load(%s): %v", body, err)
	}
	if cfg.CompactAt == nil || *cfg.CompactAt != want {
		t.Fatalf("CompactAt = %v, want %d", cfg.CompactAt, want)
	}
}

func TestValidateCompactStack(t *testing.T) {
	cases := []struct {
		name    string
		stack   []string
		wantErr bool
	}{
		{name: "empty stack is accepted", stack: nil},
		{name: "backend then plugins is accepted", stack: []string{"backend", "summarize"}},
		{name: "backend listed twice is refused", stack: []string{"backend", "backend"}, wantErr: true},
		{name: "plugin listed twice is refused", stack: []string{"a", "b", "a"}, wantErr: true},
		{name: "blank name is refused", stack: []string{"backend", " "}, wantErr: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := ValidateCompactStack(tc.stack); (err != nil) != tc.wantErr {
				t.Fatalf("ValidateCompactStack(%v) = %v, want error %t", tc.stack, err, tc.wantErr)
			}
		})
	}
}

func TestLoadRefusesDuplicateCompactStack(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte(`{"compactStack": ["backend", "backend"]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(path); err == nil {
		t.Fatal("Load accepted a compactStack listing backend twice")
	}
}
