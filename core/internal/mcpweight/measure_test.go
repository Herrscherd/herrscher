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
)

const missingCommand = "/nonexistent/herrscher-mcp-weight-missing"

func TestMeasureCountsToolsAcrossPagesExactly(t *testing.T) {
	result := Measure(context.Background(), fakeServer("two-pages"), DefaultTimeout)
	if !result.Measured || result.Cause != "" {
		t.Fatalf("Measure = %+v, want a measured server", result)
	}
	if result.Tools != len(fakeToolDefinitions()) {
		t.Errorf("Tools = %d, want %d", result.Tools, len(fakeToolDefinitions()))
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

func TestMeasureReportsHTTPServerAsNotMeasuredWithoutStarting(t *testing.T) {
	remote := Server{Name: "remote", Type: "http", URL: "https://example.invalid/mcp", Command: missingCommand}
	result := Measure(context.Background(), remote, DefaultTimeout)
	if result.Measured || result.Cause != CauseNotStdio {
		t.Fatalf("Measure = %+v, want a server not measured", result)
	}
	if result.Transport != "http" {
		t.Errorf("Transport = %q, want http", result.Transport)
	}
}

func TestMeasureReportsMissingCommandAsStartFailure(t *testing.T) {
	result := Measure(context.Background(), Server{Name: "gone", Command: missingCommand}, DefaultTimeout)
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

func TestDescribeNamesWhyAServerIsNotStarted(t *testing.T) {
	stdio := Describe(Server{Name: "local", Command: missingCommand})
	if stdio.Measured || stdio.Cause != CauseNotStarted || stdio.Transport != TransportStdio {
		t.Fatalf("Describe(stdio) = %+v, want a server not started", stdio)
	}
	sse := Describe(Server{Name: "stream", Type: "sse"})
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
	if _, err := ReadServers(path); !errors.Is(err, ErrConfig) {
		t.Fatalf("ReadServers error = %v, want ErrConfig", err)
	}
}

func TestSubstituteReplacesWorktreeTokenInArguments(t *testing.T) {
	got := substituteAll([]string{"--project", worktreePlaceholder, "plain"}, "/scratch/dir")
	want := []string{"--project", "/scratch/dir", "plain"}
	if !slices.Equal(got, want) {
		t.Fatalf("substituteAll = %v, want %v", got, want)
	}
}
