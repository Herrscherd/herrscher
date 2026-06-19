package agent

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestStoreCreateSeedsFiles(t *testing.T) {
	s := NewStore(t.TempDir())
	a, err := s.Create(CreateSpec{Name: "roblox", Soul: "You are Roblox.", MCP: "neublox serve --project {{WORKTREE}}"})
	if err != nil {
		t.Fatal(err)
	}
	if a.Name != "roblox" {
		t.Fatalf("name = %q", a.Name)
	}

	soul, err := os.ReadFile(filepath.Join(a.Home, "SOUL.md"))
	if err != nil || string(soul) != "You are Roblox." {
		t.Fatalf("SOUL.md = %q err=%v", soul, err)
	}

	mcp, err := os.ReadFile(filepath.Join(a.Home, "mcp.json"))
	if err != nil {
		t.Fatal(err)
	}
	var cfg struct {
		MCPServers map[string]struct {
			Type    string   `json:"type"`
			Command string   `json:"command"`
			Args    []string `json:"args"`
		} `json:"mcpServers"`
	}
	if err := json.Unmarshal(mcp, &cfg); err != nil {
		t.Fatalf("mcp.json invalid: %v\n%s", err, mcp)
	}
	srv, ok := cfg.MCPServers["neublox"]
	if !ok || srv.Type != "stdio" || srv.Command != "neublox" {
		t.Fatalf("neublox server wrong: %+v", cfg.MCPServers)
	}
	if strings.Join(srv.Args, " ") != "serve --project {{WORKTREE}}" {
		t.Fatalf("args = %v", srv.Args)
	}

	settings, err := os.ReadFile(filepath.Join(a.Home, "settings.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(settings), `"enableAllProjectMcpServers": true`) {
		t.Fatalf("settings missing enable flag:\n%s", settings)
	}
	if !strings.Contains(string(settings), "mcp__neublox__*") {
		t.Fatalf("settings missing mcp allow:\n%s", settings)
	}
}

func TestStoreCreateDefaultSoulAndNoMCP(t *testing.T) {
	s := NewStore(t.TempDir())
	a, err := s.Create(CreateSpec{Name: "plain"})
	if err != nil {
		t.Fatal(err)
	}
	soul, _ := os.ReadFile(filepath.Join(a.Home, "SOUL.md"))
	if !strings.Contains(string(soul), "companion") {
		t.Fatalf("default soul not seeded:\n%s", soul)
	}
	mcp, _ := os.ReadFile(filepath.Join(a.Home, "mcp.json"))
	if !strings.Contains(string(mcp), `"mcpServers"`) || strings.Contains(string(mcp), "stdio") {
		t.Fatalf("expected empty mcpServers, got:\n%s", mcp)
	}
}

func TestStoreCreateDuplicate(t *testing.T) {
	s := NewStore(t.TempDir())
	if _, err := s.Create(CreateSpec{Name: "x"}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Create(CreateSpec{Name: "x"}); err == nil {
		t.Fatal("expected duplicate error")
	}
}

func TestStoreGetAndList(t *testing.T) {
	s := NewStore(t.TempDir())
	if _, ok := s.Get("nope"); ok {
		t.Fatal("Get on missing should be false")
	}
	if _, err := s.Create(CreateSpec{Name: "b"}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Create(CreateSpec{Name: "a"}); err != nil {
		t.Fatal(err)
	}
	got, err := s.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].Name != "a" || got[1].Name != "b" {
		t.Fatalf("List = %+v (want sorted a,b)", got)
	}
	if a, ok := s.Get("a"); !ok || a.Name != "a" {
		t.Fatalf("Get a = %+v ok=%v", a, ok)
	}
}

func TestStoreCreateRejectsBadName(t *testing.T) {
	s := NewStore(t.TempDir())
	for _, n := range []string{"", "a/b", "..", "../x"} {
		if _, err := s.Create(CreateSpec{Name: n}); err == nil {
			t.Fatalf("name %q should be rejected", n)
		}
	}
}
