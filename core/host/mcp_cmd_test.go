package host

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Herrscherd/herrscher/core/clitext"
	"github.com/Herrscherd/herrscher/core/internal/agent"
	"github.com/Herrscherd/herrscher/core/internal/mcpweight"
)

type singleAgent struct {
	name string
	home string
}

func (s singleAgent) Get(name string) (agent.Agent, bool) {
	if name != s.name {
		return agent.Agent{}, false
	}
	return agent.Agent{Name: name, Home: s.home}, true
}

func (s singleAgent) List() ([]agent.Agent, error) {
	return []agent.Agent{{Name: s.name, Home: s.home}}, nil
}

func writeMCPHome(t *testing.T, body string) string {
	t.Helper()
	home := t.TempDir()
	if err := os.WriteFile(filepath.Join(home, "mcp.json"), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return home
}

func TestMCPWeightUnknownAgentFails(t *testing.T) {
	agents := singleAgent{name: "known", home: t.TempDir()}
	_, err := mcpWeightReport(context.Background(), clitext.NewMessages(), testLanguage()(), agents, mcpWeightRequest{agent: "ghost"})
	if err == nil {
		t.Fatal("mcpWeightReport(unknown agent) returned no error")
	}
}

func TestMCPWeightWithoutServersReportsAnEmptyList(t *testing.T) {
	agents := singleAgent{name: "bare", home: t.TempDir()}
	out, err := mcpWeightReport(context.Background(), clitext.NewMessages(), testLanguage()(), agents, mcpWeightRequest{agent: "bare", asJSON: true})
	if err != nil {
		t.Fatal(err)
	}
	if out != "[]" {
		t.Fatalf("report = %q, want an empty JSON list", out)
	}
}

func TestMCPWeightWithoutDeepDescribesEachServerWithoutStartingIt(t *testing.T) {
	home := writeMCPHome(t, `{"mcpServers":{"local":{"command":"/nonexistent/herrscher-mcp-cmd-test"},"remote":{"type":"http","url":"https://example.invalid/mcp"}}}`)
	agents := singleAgent{name: "described", home: home}
	out, err := mcpWeightReport(context.Background(), clitext.NewMessages(), testLanguage()(), agents, mcpWeightRequest{agent: "described", asJSON: true})
	if err != nil {
		t.Fatal(err)
	}
	var results []mcpweight.Result
	if err := json.Unmarshal([]byte(out), &results); err != nil {
		t.Fatalf("report is not JSON: %v\n%s", err, out)
	}
	if len(results) != 2 {
		t.Fatalf("results = %+v, want two servers", results)
	}
	if results[0].Name != "local" || results[0].Measured || results[0].Cause != mcpweight.CauseNotStarted {
		t.Errorf("local = %+v, want a stdio server not started", results[0])
	}
	if results[1].Name != "remote" || results[1].Measured || results[1].Cause != mcpweight.CauseNotStdio {
		t.Errorf("remote = %+v, want a server not stdio", results[1])
	}
}

func TestMCPWeightTextReportListsEachServer(t *testing.T) {
	home := writeMCPHome(t, `{"mcpServers":{"remote":{"type":"http","url":"https://example.invalid/mcp"}}}`)
	agents := singleAgent{name: "text", home: home}
	out, err := mcpWeightReport(context.Background(), clitext.NewMessages(), testLanguage()(), agents, mcpWeightRequest{agent: "text"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "remote") || !strings.Contains(out, "http") {
		t.Fatalf("text report misses the server:\n%s", out)
	}
}
