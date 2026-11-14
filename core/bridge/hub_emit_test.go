package bridge

import (
	"testing"

	contracts "github.com/Herrscherd/herrscher-contracts"
)

func TestEmitBackendEventForwardsTheResetAsTheResetEvent(t *testing.T) {
	var sink recordSink
	emitBackendEvent(&sink, contracts.BackendEvent{Kind: backendKindReset}, 0, 0, 0, 0)
	if len(sink.events) != 1 || sink.events[0].T != "reset" {
		t.Fatalf("a backend reset must reach the TUI as the reset event: %+v", sink.events)
	}
}
