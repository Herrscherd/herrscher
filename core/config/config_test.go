package config

import (
	"os"
	"path/filepath"
	"strings"
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

func TestValidateContextLimits(t *testing.T) {
	cases := []struct {
		name    string
		limits  map[string]int
		wantErr bool
	}{
		{name: "absent table is accepted", limits: nil},
		{name: "positive sizes are accepted", limits: map[string]int{"codex": 400_000, "claude": 1}},
		{name: "zero is refused", limits: map[string]int{"codex": 0}, wantErr: true},
		{name: "negative is refused", limits: map[string]int{"codex": -1}, wantErr: true},
		{name: "empty vendor is refused", limits: map[string]int{"": 400_000}, wantErr: true},
		{name: "blank vendor is refused", limits: map[string]int{"  ": 400_000}, wantErr: true},
		{name: "one vendor in two cases is refused", limits: map[string]int{"codex": 400_000, "Codex": 300_000}, wantErr: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := ValidateContextLimits(tc.limits); (err != nil) != tc.wantErr {
				t.Fatalf("ValidateContextLimits(%v) = %v, want error %t", tc.limits, err, tc.wantErr)
			}
		})
	}
}

func TestLoadRefusesInvalidContextLimitNamingTheVendor(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte(`{"contextLimits": {"codex": 0}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := Load(path)
	if err == nil {
		t.Fatal("Load accepted a zero window size")
	}
	if !strings.Contains(err.Error(), `"codex"`) {
		t.Fatalf("Load error %q does not name the vendor", err)
	}
}

func TestLoadReadsContextLimits(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte(`{"contextLimits": {"codex": 400000}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.ContextLimits["codex"] != 400_000 {
		t.Fatalf("ContextLimits = %v, want codex 400000", cfg.ContextLimits)
	}
}

func TestAccountFallbackIsOffUnlessEnabled(t *testing.T) {
	cases := map[string]struct {
		json string
		want bool
	}{
		"absent":   {json: `{}`, want: false},
		"no flag":  {json: `{"accounts":{}}`, want: false},
		"disabled": {json: `{"accounts":{"fallback":false}}`, want: false},
		"enabled":  {json: `{"accounts":{"fallback":true}}`, want: true},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "config.json")
			if err := os.WriteFile(path, []byte(tc.json), 0o644); err != nil {
				t.Fatal(err)
			}
			c, err := Load(path)
			if err != nil {
				t.Fatalf("Load: %v", err)
			}
			if got := c.AccountFallback(); got != tc.want {
				t.Fatalf("AccountFallback() = %v, want %v", got, tc.want)
			}
		})
	}
}
