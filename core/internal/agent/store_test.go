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
	soul, err := os.ReadFile(filepath.Join(a.Home, "SOUL.md"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(soul), "companion") {
		t.Fatalf("default soul not seeded:\n%s", soul)
	}
	mcp, err := os.ReadFile(filepath.Join(a.Home, "mcp.json"))
	if err != nil {
		t.Fatal(err)
	}
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
	if _, ok := s.Get("../x"); ok {
		t.Fatal("Get must reject traversal names")
	}
}

func TestStoreCreateRejectsBadName(t *testing.T) {
	s := NewStore(t.TempDir())
	for _, n := range []string{"", ".", "a/b", "..", "../x"} {
		if _, err := s.Create(CreateSpec{Name: n}); err == nil {
			t.Fatalf("name %q should be rejected", n)
		}
	}
}

func TestGetReadsTags(t *testing.T) {
	root := t.TempDir()
	s := NewStore(root)
	if _, err := s.Create(CreateSpec{Name: "netter", Soul: "x"}); err != nil {
		t.Fatalf("create: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "netter", "TAGS"), []byte("network, Sockets\nhttp"), 0o644); err != nil {
		t.Fatalf("write TAGS: %v", err)
	}
	a, ok := s.Get("netter")
	if !ok {
		t.Fatal("Get netter = !ok")
	}
	got := strings.Join(a.Tags, ",")
	if got != "network,sockets,http" {
		t.Fatalf("Tags = %q, want network,sockets,http", got)
	}
}

func TestGetNoTagsFileYieldsNil(t *testing.T) {
	root := t.TempDir()
	s := NewStore(root)
	if _, err := s.Create(CreateSpec{Name: "bare", Soul: "x"}); err != nil {
		t.Fatalf("create: %v", err)
	}
	a, ok := s.Get("bare")
	if !ok || a.Tags != nil {
		t.Fatalf("bare Tags = %v, want nil", a.Tags)
	}
}

func TestListReadsTags(t *testing.T) {
	root := t.TempDir()
	s := NewStore(root)
	for _, n := range []string{"a", "b"} {
		if _, err := s.Create(CreateSpec{Name: n, Soul: "x"}); err != nil {
			t.Fatalf("create %s: %v", n, err)
		}
	}
	if err := os.WriteFile(filepath.Join(root, "a", "TAGS"), []byte("lua  roblox"), 0o644); err != nil {
		t.Fatalf("write TAGS: %v", err)
	}
	list, err := s.List()
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(list) != 2 || list[0].Name != "a" {
		t.Fatalf("list = %+v", list)
	}
	if strings.Join(list[0].Tags, ",") != "lua,roblox" {
		t.Fatalf("a.Tags = %v", list[0].Tags)
	}
	if list[1].Tags != nil {
		t.Fatalf("b.Tags = %v, want nil", list[1].Tags)
	}
}
