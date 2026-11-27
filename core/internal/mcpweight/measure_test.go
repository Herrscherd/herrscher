package mcpweight

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/Herrscherd/herrscher/core/contextwindow"
	"github.com/Herrscherd/herrscher/core/internal/agent"
)

const missingCommand = "/nonexistent/herrscher-mcp-weight-missing"

func TestMeasureCountsToolsAcrossPagesExactly(t *testing.T) {
	result := Measure(context.Background(), fakeServer("two-pages"), DefaultTimeout)
	if !result.Measured || result.Cause != "" {
		t.Fatalf("Measure = %+v, want a measured server", result)
	}
	if result.Tools != fakeToolCount {
		t.Errorf("Tools = %d, want %d", result.Tools, fakeToolCount)
	}
	if result.Bytes != expectedBytes() {
		t.Errorf("Bytes = %d, want %d", result.Bytes, expectedBytes())
	}
	if want := contextwindow.EstimateTokens(strings.Join(fakeToolDefinitions(), "")); result.Tokens != want {
		t.Errorf("Tokens = %d, want %d", result.Tokens, want)
	}
}

func TestMeasureReportsSilentServerAsTimeout(t *testing.T) {
	started := time.Now()
	result := Measure(context.Background(), fakeServer("silent"), 300*time.Millisecond)
	if result.Measured || result.Cause != CauseTimeout {
		t.Fatalf("Measure = %+v, want a timeout", result)
	}
	if result.Detail == "" {
		t.Error("Detail is empty, the timeout must carry its reason")
	}
	if elapsed := time.Since(started); elapsed > 10*time.Second {
		t.Errorf("Measure took %s, the timeout did not stop the server", elapsed)
	}
}

func TestMeasureReportsParentCancellationAsCanceled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	timer := time.AfterFunc(500*time.Millisecond, cancel)
	defer timer.Stop()
	result := Measure(ctx, fakeServer("silent"), DefaultTimeout)
	if result.Measured || result.Cause != CauseCanceled {
		t.Fatalf("Measure = %+v, want a canceled measure, not a timeout", result)
	}
}

func TestMeasureReportsHTTPServerAsNotMeasuredWithoutStarting(t *testing.T) {
	remote := agent.MCPServer{Name: "remote", Type: "http", URL: "https://example.invalid/mcp", Command: missingCommand}
	result := Measure(context.Background(), remote, DefaultTimeout)
	if result.Measured || result.Cause != CauseNotStdio {
		t.Fatalf("Measure = %+v, want a server not measured", result)
	}
	if result.Transport != "http" {
		t.Errorf("Transport = %q, want http", result.Transport)
	}
}

func TestMeasureReportsMissingCommandAsStartFailure(t *testing.T) {
	result := Measure(context.Background(), agent.MCPServer{Name: "gone", Command: missingCommand}, DefaultTimeout)
	if result.Measured || result.Cause != CauseStart {
		t.Fatalf("Measure = %+v, want a start failure", result)
	}
}

func TestMeasureReportsEarlyExitAsExited(t *testing.T) {
	result := Measure(context.Background(), fakeServer("exit"), DefaultTimeout)
	if result.Measured || result.Cause != CauseExited {
		t.Fatalf("Measure = %+v, want an exited server", result)
	}
}

func TestMeasureKeepsTheServerStderrTailApartFromTheErrorText(t *testing.T) {
	result := Measure(context.Background(), fakeServer("stderr-exit"), DefaultTimeout)
	if result.Measured || result.Cause != CauseExited {
		t.Fatalf("Measure = %+v, want an exited server", result)
	}
	if result.Stderr != "missing token" {
		t.Fatalf("Stderr = %q, want the server's own stderr tail", result.Stderr)
	}
	if strings.Contains(result.Detail, "missing token") {
		t.Fatalf("Detail = %q, the server's stderr must not be mixed into the error text", result.Detail)
	}
}

func TestMeasureReportsRefusedRequestAsRejected(t *testing.T) {
	result := Measure(context.Background(), fakeServer("rejecting"), DefaultTimeout)
	if result.Measured || result.Cause != CauseRejected {
		t.Fatalf("Measure = %+v, want a rejected request", result)
	}
}

func TestMeasureReportsMalformedAnswerAsProtocol(t *testing.T) {
	result := Measure(context.Background(), fakeServer("garbage"), DefaultTimeout)
	if result.Measured || result.Cause != CauseProtocol {
		t.Fatalf("Measure = %+v, want a protocol failure", result)
	}
}

func TestMeasureRefusesToolsListWithoutToolsArray(t *testing.T) {
	result := Measure(context.Background(), fakeServer("no-tools-array"), DefaultTimeout)
	if result.Measured || result.Cause != CauseProtocol {
		t.Fatalf("Measure = %+v, want a protocol failure", result)
	}
}

func TestMeasureStopsAtThePageLimit(t *testing.T) {
	result := Measure(context.Background(), fakeServer("endless"), DefaultTimeout)
	if result.Measured || result.Cause != CauseTooManyPages {
		t.Fatalf("Measure = %+v, want the page limit", result)
	}
}

func TestMeasureRefusesDefinitionsOverTheTotalLimitAcrossPages(t *testing.T) {
	result := Measure(context.Background(), fakeServer("bulky"), DefaultTimeout)
	if result.Measured || result.Cause != CauseTooLarge {
		t.Fatalf("Measure = %+v, want the total size limit, not the page limit", result)
	}
}

func TestMeasureSkipsServerRequestsThatReuseAnID(t *testing.T) {
	result := Measure(context.Background(), fakeServer("server-request"), DefaultTimeout)
	if !result.Measured || result.Tools != fakeToolCount || result.Cause != "" {
		t.Fatalf("Measure = %+v, want the tools after a server request with the same id", result)
	}
}

func TestDescribeNamesWhyAServerIsNotStarted(t *testing.T) {
	stdio := Describe(agent.MCPServer{Name: "local", Command: missingCommand})
	if stdio.Measured || stdio.Cause != CauseNotStarted || stdio.Transport != agent.TransportStdio {
		t.Fatalf("Describe(stdio) = %+v, want a server not started", stdio)
	}
	sse := Describe(agent.MCPServer{Name: "stream", Type: "sse"})
	if sse.Cause != CauseNotStdio || sse.Transport != "sse" {
		t.Fatalf("Describe(sse) = %+v, want a server not stdio", sse)
	}
}

func TestReadServersSortsByNameAndReadsTransport(t *testing.T) {
	path := filepath.Join(t.TempDir(), "mcp.json")
	body := `{"mcpServers":{"zeta":{"type":"http","url":"https://example.invalid"},"alpha":{"command":"srv","args":["--root","{{WORKTREE}}"],"env":{"TOKEN_NAME":"x"}}}}`
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	servers, err := ReadServers(path)
	if err != nil {
		t.Fatal(err)
	}
	names := make([]string, 0, len(servers))
	for _, server := range servers {
		names = append(names, server.Name+":"+server.Transport())
	}
	if want := []string{"alpha:stdio", "zeta:http"}; !slices.Equal(names, want) {
		t.Fatalf("servers = %v, want %v", names, want)
	}
	if servers[0].Args[1] != worktreePlaceholder {
		t.Errorf("args = %v, the worktree token must reach the measurement unchanged", servers[0].Args)
	}
}

func TestReadServersOfAMissingFileIsEmpty(t *testing.T) {
	servers, err := ReadServers(filepath.Join(t.TempDir(), "mcp.json"))
	if err != nil || servers != nil {
		t.Fatalf("ReadServers = %v, %v, want nothing and no error", servers, err)
	}
}

func TestReadServersRefusesMalformedFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "mcp.json")
	if err := os.WriteFile(path, []byte(`{"mcpServers":`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadServers(path); err == nil || !strings.Contains(err.Error(), "parse mcp.json") {
		t.Fatalf("ReadServers error = %v, want a parse error", err)
	}
}

func TestReadServersRefusesStdioWithoutCommand(t *testing.T) {
	path := filepath.Join(t.TempDir(), "mcp.json")
	if err := os.WriteFile(path, []byte(`{"mcpServers":{"broken":{"type":"stdio"}}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadServers(path); !errors.Is(err, agent.ErrMissingMCPCommand) {
		t.Fatalf("ReadServers error = %v, want ErrMissingMCPCommand", err)
	}
}

func TestSubstituteReplacesWorktreeTokenInArguments(t *testing.T) {
	got := substituteAll([]string{"--project", worktreePlaceholder, "plain"}, "/scratch/dir")
	want := []string{"--project", "/scratch/dir", "plain"}
	if !slices.Equal(got, want) {
		t.Fatalf("substituteAll = %v, want %v", got, want)
	}
}
