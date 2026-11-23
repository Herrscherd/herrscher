package bridge

import (
	"strings"
	"testing"

	contracts "github.com/Herrscherd/herrscher-contracts"
)

func TestRefusedToolCallsOfATurnAreLoggedOnce(t *testing.T) {
	buf := captureLog(t)
	var guard toolCallGuard
	refused := contracts.BackendEvent{Kind: backendKindText, Detail: "hello", ToolCall: startedCall()}
	for range 3 {
		guard.receive(refused)
	}
	guard.report()
	lines := strings.Split(strings.TrimSpace(buf.String()), "\n")
	if len(lines) != 1 {
		t.Fatalf("log has %d lines, want one line for the turn:\n%s", len(lines), buf.String())
	}
	if !strings.Contains(lines[0], "count=3") {
		t.Fatalf("log line %q does not count the three refusals", lines[0])
	}
}

func TestTurnWithoutRefusedToolCallsLogsNothing(t *testing.T) {
	buf := captureLog(t)
	var guard toolCallGuard
	guard.receive(contracts.BackendEvent{Kind: backendKindTool, Tool: "read", Detail: "env.go", ToolCall: startedCall()})
	guard.report()
	if buf.Len() != 0 {
		t.Fatalf("log = %q, want nothing for a turn with no refusal", buf.String())
	}
}
