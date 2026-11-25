package manager

import (
	"slices"
	"testing"

	contracts "github.com/Herrscherd/herrscher-contracts"
	"github.com/Herrscherd/herrscher/core/internal/state"
)

func TestSessionHistoryJSONCarriesTheInjectedSizesOfATurn(t *testing.T) {
	dir := t.TempDir()
	injected := []contracts.InjectedPart{{Part: contracts.ContextMemory, Tokens: 900}, {Part: contracts.ContextSkills, Tokens: 120}}
	seedTranscript(t, dir, []state.TranscriptEntry{
		{Ts: "t1", Role: "user", Text: "look", TurnID: "turn_t"},
		{Ts: "t2", Role: "assistant", Text: "found", TurnID: "turn_t", Injected: injected},
	})
	turns := historyJSON(t, &Handler{partDir: dir}, map[string]string{"name": "alpha"})
	if len(turns) != 1 || !slices.Equal(turns[0].Injected, injected) {
		t.Fatalf("turns = %+v, want one turn carrying %+v", turns, injected)
	}
}
