package manager

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/Herrscherd/herrscher/core/internal/agent"
	"github.com/Herrscherd/herrscher/core/internal/state"
)

func TestARollbackThatCannotPersistIsReported(t *testing.T) {
	path := filepath.Join(t.TempDir(), "s.json")
	st := state.NewState(path)
	if err := st.AddSession(state.Session{Name: "beta", ChannelID: "42", Type: "text", Vendor: "claude"}); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(path+".tmp", 0o700); err != nil {
		t.Fatal(err)
	}
	sup := &fakeSup{}
	h := NewHandler(&fakeChannelAdmin{}, sup, &fakeWT{path: t.TempDir(), scratchBase: t.TempDir()}, &fakeForge{}, &fakeUpdater{}, agent.NewStore(t.TempDir()), st, "claude", t.TempDir(), []string{"chat"}, nil, nil)

	err := h.rollbackSwitch("beta", "claude", "", "", "")
	if err == nil {
		t.Fatal("a rollback whose target could not be saved reported success")
	}
	if len(sup.restarted) != 1 {
		t.Fatalf("the rollback did not restart the session: restarted=%d", len(sup.restarted))
	}
}
