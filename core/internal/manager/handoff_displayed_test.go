package manager

import (
	"strings"
	"testing"

	"github.com/Herrscherd/herrscher/core/internal/state"
)

func TestHandoffSeedReadsTextNotDisplayedText(t *testing.T) {
	seed := buildHandoffSeed([]state.TranscriptEntry{
		{Role: "assistant", Text: "hello", Displayed: "hello\n\nSHOWN-ONLY-NOTICE"},
	}, "full")
	if strings.Contains(seed, "SHOWN-ONLY-NOTICE") {
		t.Fatalf("handoff seed carries the notice shown to the user, want the model text only:\n%s", seed)
	}
	if !strings.Contains(seed, "assistant: hello") {
		t.Fatalf("handoff seed lacks the model text:\n%s", seed)
	}
}
