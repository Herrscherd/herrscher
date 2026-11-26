package host

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Herrscherd/herrscher/core/clitext"
	"github.com/Herrscherd/herrscher/core/internal/agent"
	"github.com/Herrscherd/herrscher/core/internal/mcpweight"
)

const hostFakeMCPEnv = "HERRSCHER_HOST_FAKE_MCP"

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

func TestHostFakeMCP(t *testing.T) {
	if os.Getenv(hostFakeMCPEnv) == "" {
		t.Skip("helper server of the mcp weight deep test, started only as a child process")
	}
	serveHostFakeMCP(os.Stdin, os.Stdout)
}

func serveHostFakeMCP(in io.Reader, out io.Writer) {
	scanner := bufio.NewScanner(in)
	encoder := json.NewEncoder(out)
	for scanner.Scan() {
		var request struct {
			ID     *int   `json:"id"`
			Method string `json:"method"`
		}
		if json.Unmarshal(scanner.Bytes(), &request) != nil || request.ID == nil {
			continue
		}
		if err := encoder.Encode(map[string]any{"jsonrpc": "2.0", "id": *request.ID, "result": hostFakeMCPResult(request.Method)}); err != nil {
			return
		}
	}
}

func hostFakeMCPResult(method string) map[string]any {
	switch method {
	case "initialize":
		return map[string]any{
			"protocolVersion": "2025-06-18",
			"capabilities":    map[string]any{"tools": make(map[string]any)},
			"serverInfo":      map[string]any{"name": "host-fake", "version": "0"},
		}
	case "tools/list":
		return map[string]any{"tools": []map[string]any{{"name": "echo", "description": "echoes", "inputSchema": map[string]any{"type": "object"}}}}
	}
	return make(map[string]any)
}

func TestMCPWeightUnknownAgentFails(t *testing.T) {
	agents := singleAgent{name: "known", home: t.TempDir()}
	_, err := mcpWeightReport(context.Background(), clitext.NewMessages(), testLanguage()(), agents, mcpweight.NewMeasurer(), mcpWeightRequest{agent: "ghost"})
	unknown, ok := errors.AsType[*UnknownError](err)
	if !ok || unknown.Kind != kindAgent || unknown.Name != "ghost" {
		t.Fatalf("mcpWeightReport(unknown agent) error = %v, want an UnknownError for the agent ghost", err)
	}
}

func TestMCPWeightWithoutServersReportsAnEmptyList(t *testing.T) {
	agents := singleAgent{name: "bare", home: t.TempDir()}
	out, err := mcpWeightReport(context.Background(), clitext.NewMessages(), testLanguage()(), agents, mcpweight.NewMeasurer(), mcpWeightRequest{agent: "bare", asJSON: true})
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
	out, err := mcpWeightReport(context.Background(), clitext.NewMessages(), testLanguage()(), agents, mcpweight.NewMeasurer(), mcpWeightRequest{agent: "described", asJSON: true})
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

func TestMCPWeightDeepMeasuresTheServer(t *testing.T) {
	config := map[string]any{"mcpServers": map[string]any{"fake": map[string]any{
		"command": os.Args[0],
		"args":    []string{"-test.run=^TestHostFakeMCP$"},
		"env":     map[string]string{hostFakeMCPEnv: "1"},
	}}}
	raw, err := json.Marshal(config)
	if err != nil {
		t.Fatal(err)
	}
	agents := singleAgent{name: "deep", home: writeMCPHome(t, string(raw))}
	out, err := mcpWeightReport(context.Background(), clitext.NewMessages(), testLanguage()(), agents, mcpweight.NewMeasurer(), mcpWeightRequest{agent: "deep", deep: true, asJSON: true})
	if err != nil {
		t.Fatal(err)
	}
	var results []mcpweight.Result
	if err := json.Unmarshal([]byte(out), &results); err != nil {
		t.Fatalf("report is not JSON: %v\n%s", err, out)
	}
	if len(results) != 1 || !results[0].Measured || results[0].Tools != 1 || results[0].Cause != "" {
		t.Fatalf("results = %+v, want the one tool of the deep server measured", results)
	}
}

func TestMCPWeightTextReportRendersEachLineKind(t *testing.T) {
	results := []mcpweight.Result{
		{Name: "fast", Transport: agent.TransportStdio, Measured: true, Tools: 2, Bytes: 40, Tokens: 10},
		{Name: "slow", Transport: agent.TransportStdio, Cause: mcpweight.CauseTimeout, Detail: "no answer"},
		{Name: "remote", Transport: "http", Cause: mcpweight.CauseNotStdio},
		{Name: "stopper", Transport: agent.TransportStdio, Measured: true, Tools: 1, Cause: mcpweight.CauseStop, Detail: "EOF"},
		{Name: "waiting", Transport: agent.TransportStdio, Cause: mcpweight.CauseCanceled, Detail: "context canceled"},
	}
	out := renderMCPWeight(clitext.NewMessages(), testLanguage()(), "text", results)
	wants := []string{
		"fast (stdio): 2 tools, 40 bytes, about 10 tokens (estimate)",
		"slow (stdio): not measured, the server did not answer in time: no answer",
		"remote (http): not measured, the transport is not stdio, so the server is not started",
		"stopper: measured, but the server did not stop cleanly: EOF",
		"waiting (stdio): not measured, the measure was canceled before the server answered: context canceled",
	}
	for _, want := range wants {
		if !strings.Contains(out, want) {
			t.Errorf("text report misses %q:\n%s", want, out)
		}
	}
}

func TestMCPWeightCauseKeyMapsCanceledToItsOwnText(t *testing.T) {
	if got := mcpWeightCauseKey(mcpweight.CauseCanceled); got != clitext.ToolWeightCauseCanceled {
		t.Fatalf("mcpWeightCauseKey(canceled) = %q, want %q", got, clitext.ToolWeightCauseCanceled)
	}
}
