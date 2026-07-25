package manager

import (
	"strings"
	"testing"

	"github.com/Herrscherd/herrscher/core/internal/state"
)

func entries(n int) []state.TranscriptEntry {
	out := make([]state.TranscriptEntry, 0, n)
	for i := 0; i < n; i++ {
		role := "user"
		if i%2 == 1 {
			role = "assistant"
		}
		out = append(out, state.TranscriptEntry{Role: role, Text: "msg" + string(rune('A'+i))})
	}
	return out
}

func TestBuildHandoffSeedFullReplaysEverything(t *testing.T) {
	seed := buildHandoffSeed(entries(3), "full")
	for _, want := range []string{"msgA", "msgB", "msgC"} {
		if !strings.Contains(seed, want) {
			t.Fatalf("full seed missing %q:\n%s", want, seed)
		}
	}
}

func TestBuildHandoffSeedSummaryKeepsTailAndNotesElision(t *testing.T) {
	seed := buildHandoffSeed(entries(handoffSummaryTail+3), "summary")
	if strings.Contains(seed, "msgA") {
		t.Fatalf("summary must drop the oldest turn:\n%s", seed)
	}
	if !strings.Contains(seed, "3 tours antérieurs") {
		t.Fatalf("summary must note elided turns:\n%s", seed)
	}
}

func TestBuildHandoffSeedEmptyTranscriptIsEmpty(t *testing.T) {
	if buildHandoffSeed(nil, "full") != "" {
		t.Fatal("no transcript must yield empty seed")
	}
}
