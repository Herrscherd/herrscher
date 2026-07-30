package manager

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"

	contracts "github.com/Herrscherd/herrscher-contracts"
	"github.com/Herrscherd/herrscher/core/internal/state"
)

func TestSessionListJSONCarriesIncarnation(t *testing.T) {
	st := state.NewState(filepath.Join(t.TempDir(), "state.json"))
	if err := st.AddSession(state.Session{Name: "demo", ChannelID: "c1", Type: "text"}); err != nil {
		t.Fatal(err)
	}
	persisted, ok := st.FindSession("demo")
	if !ok {
		t.Fatal("session demo missing")
	}

	h := &Handler{st: st}
	out, err := h.sessionListRun(context.Background(), contracts.Input{JSON: true})
	if err != nil {
		t.Fatal(err)
	}
	var rows []struct {
		Incarnation string `json:"incarnation"`
	}
	if err := json.Unmarshal([]byte(out), &rows); err != nil {
		t.Fatalf("unmarshal session list %q: %v", out, err)
	}
	if len(rows) != 1 || rows[0].Incarnation == "" || rows[0].Incarnation != persisted.Incarnation {
		t.Fatalf("session list incarnation = %+v, persisted = %q", rows, persisted.Incarnation)
	}
}
