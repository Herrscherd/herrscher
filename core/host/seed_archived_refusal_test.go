package host

import (
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/Herrscherd/herrscher/core/internal/state"
)

func TestOneShotSeedRefusesAnArchivedSession(t *testing.T) {
	st := state.NewState(filepath.Join(t.TempDir(), "state.json"))
	if err := st.AddSession(state.Session{Name: "doomed", ChannelID: "c-doomed", Incarnation: "doomed"}); err != nil {
		t.Fatal(err)
	}
	if err := st.SetArchived("doomed", true); err != nil {
		t.Fatal(err)
	}
	_, err := runOneShotSeedIDWithRuntime(t.Context(), st, "doomed", "write the digest", "turn-1", oneShotSeedRuntime{timeout: time.Millisecond})
	if !errors.Is(err, errSeedArchivedSession) {
		t.Fatalf("a seed on an archived session returned %v, want the archived refusal", err)
	}
}
