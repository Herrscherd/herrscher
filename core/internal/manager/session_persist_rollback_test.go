package manager

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/Herrscherd/herrscher/core/clitext"
	"github.com/Herrscherd/herrscher/core/internal/agent"
	"github.com/Herrscherd/herrscher/core/internal/state"
)

func unwritableState(t *testing.T) *state.State {
	t.Helper()
	blocker := filepath.Join(t.TempDir(), "blocker")
	if err := os.WriteFile(blocker, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	st := state.NewState(filepath.Join(blocker, "state.json"))
	st.InstanceID = testInstance
	return st
}

func TestSessionCreateRollsBackWhenPersistFails(t *testing.T) {
	cases := []struct {
		name      string
		home      state.HomeRef
		channelID string
	}{
		{name: "category", home: state.HomeRef{ID: "cat1", Type: "category"}, channelID: "new-inst__demo"},
		{name: "forum", home: state.HomeRef{ID: "f1", Type: "forum"}, channelID: "post-inst__demo"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			d := &fakeChannelAdmin{homeType: tc.home.Type}
			sup := &fakeSup{}
			wt := &fakeWT{path: t.TempDir(), scratchBase: t.TempDir()}
			st := unwritableState(t)
			st.Home = tc.home
			h := NewHandler(clitext.NewMessages(), d, sup, wt, &fakeForge{}, &fakeUpdater{}, agent.NewStore(t.TempDir()), st, "claude", t.TempDir(), []string{"chat"}, nil, nil)

			if _, err := h.sessionCreateRun(context.Background(), args("name", "demo")); err == nil {
				t.Fatal("expected a persist error")
			}
			if len(wt.removed) != 1 || wt.removed[0] != wt.path {
				t.Fatalf("worktree must be rolled back, removed = %+v", wt.removed)
			}
			if len(d.archived) != 1 || d.archived[0] != tc.channelID {
				t.Fatalf("owned channel must be tidied away, archived = %+v", d.archived)
			}
			if len(sup.started) != 0 {
				t.Fatalf("no bridge must be started for an unpersisted session: %+v", sup.started)
			}
		})
	}
}
