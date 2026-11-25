package host

import (
	"errors"
	"io"
	"testing"

	"github.com/Herrscherd/herrscher/core/clitext"
	"github.com/Herrscherd/herrscher/core/internal/state"
)

func TestSeedTranscriptWriteErrorReachesTheTurn(t *testing.T) {
	driver := newSessionDriver(newHostMessages(), clitext.NewMessages(), "main", nil, nil, nil, io.Discard)
	runtime := oneShotSeedRuntime{
		persists:    newPersistWrites(),
		messages:    newHostMessages(),
		cliMessages: clitext.NewMessages(),
		record:      func(string, state.TranscriptEntry) error { return errTraceRefused },
	}
	attachOneShotTaps(driver, state.Session{Name: "main"}, runtime)

	if err := driver.sink.Transcript(state.TranscriptEntry{Role: "user", Text: "question"}); !errors.Is(err, errTraceRefused) {
		t.Fatalf("Transcript returned %v, want the refused write to reach the turn", err)
	}
}
