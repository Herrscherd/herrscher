package host

import (
	"bytes"
	"errors"
	"io"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Herrscherd/herrscher/core/internal/state"
)

func TestARefusalDoesNotCarryOverToAReplacementSession(t *testing.T) {
	st := state.NewState(filepath.Join(t.TempDir(), "state.json"))
	var warnings bytes.Buffer
	h := newHub(st, nil, nil, t.TempDir(), sessionListRegistry(t), nil, newApprovalTable(io.Discard), newSessionTable(newHostMessages()), withWarnings(&warnings))
	startErr := errors.New("bridge refused")
	if err := st.AddSession(state.Session{Name: "reused", ChannelID: "c1", Incarnation: "first"}); err != nil {
		t.Fatal(err)
	}
	h.journalStart("reused", startErr)
	if err := st.RemoveSession("reused"); err != nil {
		t.Fatal(err)
	}
	if err := st.AddSession(state.Session{Name: "reused", ChannelID: "c1", Incarnation: "second"}); err != nil {
		t.Fatal(err)
	}
	h.journalStart("reused", startErr)

	if got := strings.Count(warnings.String(), "\n"); got != 2 {
		t.Fatalf("two refusals of two sessions sharing a name wrote %d warnings, want 2:\n%s", got, warnings.String())
	}
}
