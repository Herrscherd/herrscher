package host

import (
	"bytes"
	"encoding/json"
	"errors"
	"path/filepath"
	"testing"

	"github.com/Herrscherd/herrscher/core/cli"
	"github.com/Herrscherd/herrscher/core/clitext"
	"github.com/Herrscherd/herrscher/core/internal/checkpoint"
	"github.com/Herrscherd/herrscher/core/internal/state"
)

type checkpointFixture struct {
	t        *testing.T
	repo     string
	st       *state.State
	hub      *hub
	reg      *cli.Registry
	warnings *bytes.Buffer
}

func newCheckpointFixture(t *testing.T) *checkpointFixture {
	t.Helper()
	repo := checkpointRepo(t)
	st := checkpointState(t, repo)
	sessions := newSessionTable(newHostMessages(), clitext.NewMessages())
	var warnings bytes.Buffer
	h := &hub{st: st, checkpoints: &sessions.checkpoints, persists: newPersistWrites(), messages: newHostMessages(), warnings: &warnings}
	return &checkpointFixture{t: t, repo: repo, st: st, hub: h, reg: checkpointRegistry(st, sessions), warnings: &warnings}
}

func (f *checkpointFixture) takeAlpha(text string) {
	f.t.Helper()
	sess, ok := f.st.FindSession("alpha")
	if !ok {
		f.t.Fatal("no session alpha to take a checkpoint of")
	}
	f.hub.takeCheckpoint(f.t.Context(), sess, newTurnID(), text)
	if f.warnings.Len() > 0 {
		f.t.Fatalf("taking a checkpoint of alpha warned: %s", f.warnings)
	}
}

func (f *checkpointFixture) list(name string) []checkpointView {
	f.t.Helper()
	out, err := cli.Plain(f.reg.Dispatch(f.t.Context(), []string{nsSession, verbCheckpoints, "--name", name, "--json"}))
	if err != nil {
		f.t.Fatal(err)
	}
	var views []checkpointView
	if err := json.Unmarshal([]byte(out), &views); err != nil {
		f.t.Fatalf("not json: %q: %v", out, err)
	}
	return views
}

func (f *checkpointFixture) rewind(name, position string) error {
	f.t.Helper()
	_, err := f.reg.Dispatch(f.t.Context(), []string{nsSession, verbRewind, "--name", name, "--to", position, "--what", "code"})
	return err
}

func (f *checkpointFixture) addSessionIn(name string) string {
	f.t.Helper()
	worktree := filepath.Join(f.t.TempDir(), name)
	gitIn(f.t.Context(), f.t, f.repo, "worktree", "add", "-q", worktree, "-b", "work/"+name)
	if err := f.st.AddSession(state.Session{Name: name, ChannelID: "chan-" + name, Worktree: worktree}); err != nil {
		f.t.Fatal(err)
	}
	return worktree
}

func (f *checkpointFixture) rename(from, to string) {
	f.t.Helper()
	if err := f.st.RenameSession(from, to); err != nil {
		f.t.Fatal(err)
	}
}

func TestARenamedSessionListsAndRewindsTheCheckpointsItTookBefore(t *testing.T) {
	f := newCheckpointFixture(t)
	f.takeAlpha("premier")
	writeFrame(t, f.repo, "v2\n")
	f.takeAlpha("second")
	f.rename("alpha", "beta")
	writeFrame(t, f.repo, "v3\n")

	if views := f.list("beta"); len(views) != 2 || views[0].Text != "premier" || views[1].Text != "second" {
		t.Fatalf("checkpoints of the renamed session = %+v, want the two it took as alpha", views)
	}
	if err := f.rewind("beta", "1"); err != nil {
		t.Fatalf("rewind of the renamed session to a checkpoint taken before the rename: %v", err)
	}
	if got := readFrame(t, f.repo); got != "v1\n" {
		t.Fatalf("frame after rewind = %q, want the v1 of checkpoint 1", got)
	}
}

func TestASessionRenamedToAFreedNameInheritsNoCheckpoint(t *testing.T) {
	f := newCheckpointFixture(t)
	f.takeAlpha("premier")
	f.rename("alpha", "beta")
	gamma := f.addSessionIn("gamma")
	writeFrame(t, gamma, "gamma\n")
	f.rename("gamma", "alpha")

	if views := f.list("alpha"); len(views) != 0 {
		t.Fatalf("the session renamed to alpha lists %+v, the checkpoints of beta", views)
	}
	if err := f.rewind("alpha", "1"); !errors.Is(err, checkpoint.ErrNoCheckpoint) {
		t.Fatalf("rewind of the session renamed to alpha: err = %v, want %v", err, checkpoint.ErrNoCheckpoint)
	}
	if got := readFrame(t, gamma); got != "gamma\n" {
		t.Fatalf("the worktree of the session renamed to alpha holds %q, the tree of beta", got)
	}
	if views := f.list("beta"); len(views) != 1 {
		t.Fatalf("beta lists %+v, want its own checkpoint", views)
	}
}

func TestANewSessionNamedLikeAClosedOneInheritsNoCheckpoint(t *testing.T) {
	f := newCheckpointFixture(t)
	f.takeAlpha("premier")
	if err := f.st.RemoveSession("alpha"); err != nil {
		t.Fatal(err)
	}
	f.addSessionIn("alpha")

	if views := f.list("alpha"); len(views) != 0 {
		t.Fatalf("the new alpha lists %+v, the checkpoints of the closed alpha", views)
	}
}
