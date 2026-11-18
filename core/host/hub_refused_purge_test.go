package host

import (
	"bytes"
	"errors"
	"io"
	"path/filepath"
	"testing"

	"github.com/Herrscherd/herrscher/core/clitext"
	"github.com/Herrscherd/herrscher/core/internal/state"
)

func TestRemovedSessionDropsItsRefusal(t *testing.T) {
	st := state.NewState(filepath.Join(t.TempDir(), "state.json"))
	var warnings bytes.Buffer
	h := newHub(st, nil, nil, t.TempDir(), sessionListRegistry(t), nil, newApprovalTable(io.Discard), newSessionTable(newHostMessages(), clitext.NewMessages()), withWarnings(&warnings))
	for _, name := range []string{"gone", "archived"} {
		if err := st.AddSession(state.Session{Name: name, ChannelID: "c1", Incarnation: name}); err != nil {
			t.Fatal(err)
		}
		journalStartUnderLock(h, name, errors.New("bridge refused"))
	}
	if err := st.SetArchived("archived", true); err != nil {
		t.Fatal(err)
	}
	if err := st.RemoveSession("gone"); err != nil {
		t.Fatal(err)
	}
	h.reconcile(t.Context())

	if _, ok := h.refused["gone"]; ok {
		t.Fatal("a removed session kept its refusal in the hub")
	}
	if _, ok := h.refused["archived"]; !ok {
		t.Fatal("the refusal of a session that still exists was purged")
	}
}
