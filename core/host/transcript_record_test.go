package host

import (
	"io"
	"testing"

	contracts "github.com/Herrscherd/herrscher-contracts"
	"github.com/Herrscherd/herrscher/core/internal/state"
)

func TestDriverRecordsTranscript(t *testing.T) {
	var got []state.TranscriptEntry
	d := newSessionDriver("s", nil, make(chan contracts.Event, 8), make(chan contracts.Event, 8), io.Discard)
	d.sink.Transcript = func(e state.TranscriptEntry) { got = append(got, e) }

	d.recordEntry("user", "hello", 0, turnUsage{})
	d.recordEntry("assistant", "hi there", 0.02, turnUsage{})
	d.recordEntry("assistant", "", 0, turnUsage{})

	if len(got) != 2 {
		t.Fatalf("want 2 entries, got %d", len(got))
	}
	if got[0].Role != "user" || got[0].Text != "hello" {
		t.Fatalf("user entry wrong: %+v", got[0])
	}
	if got[1].Role != "assistant" || got[1].Cost != 0.02 {
		t.Fatalf("assistant entry wrong: %+v", got[1])
	}
	if got[0].Ts == "" {
		t.Fatalf("timestamp not set")
	}
}
