package host

import (
	"context"
	"log/slog"
	"path/filepath"
	"testing"
	"time"

	contracts "github.com/Herrscherd/herrscher-contracts"
	"github.com/Herrscherd/herrscher/core/internal/state"
)

func TestASessionRestartsTheDaemonOncePerWindow(t *testing.T) {
	now := time.Date(2026, 11, 26, 9, 0, 0, 0, time.UTC)
	restarts := newSelfRestarts()
	restarts.now = func() time.Time { return now }
	if err := restarts.admit("s1", verbServiceRestart); err != nil {
		t.Fatalf("first restart: %v", err)
	}
	now = now.Add(selfRestartWindow / 2)
	if err := restarts.admit("s1", verbServiceRestart); err == nil {
		t.Fatal("a second restart inside the window was admitted")
	}
	if err := restarts.admit("s2", verbServiceRestart); err != nil {
		t.Fatalf("another session's restart: %v", err)
	}
	now = now.Add(selfRestartWindow)
	if err := restarts.admit("s1", verbServiceRestart); err != nil {
		t.Fatalf("restart after the window: %v", err)
	}
}

func TestASessionNeverSetsABudgetAboveItsParent(t *testing.T) {
	st, err := state.LoadState(filepath.Join(t.TempDir(), "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	for _, sess := range []state.Session{{Name: "lead", CostCap: 5, TokenCap: 1000}, {Name: "worker", Parent: "lead"}, {Name: "free"}} {
		if err := st.AddSession(sess); err != nil {
			t.Fatal(err)
		}
	}
	cases := []struct {
		target string
		args   map[string]string
		ok     bool
	}{
		{"worker", map[string]string{"cost_cap": "4"}, true},
		{"worker", map[string]string{"cost_cap": "6"}, false},
		{"worker", map[string]string{"cost_cap": "0"}, false},
		{"worker", map[string]string{"token_cap": "2000"}, false},
		{"free", map[string]string{"cost_cap": "100"}, true},
	}
	for _, tc := range cases {
		args := map[string]string{paramName: tc.target}
		for key, value := range tc.args {
			args[key] = value
		}
		err := budgetWithinParent(st, "lead", contracts.Input{Args: args})
		if (err == nil) != tc.ok {
			t.Errorf("%s %v: err = %v, want ok=%v", tc.target, tc.args, err, tc.ok)
		}
	}
}

func TestTheSelfToolsPreparerSkipsASessionWithoutAWorktree(t *testing.T) {
	var provisioned []string
	prepare := selfToolsPreparer(func(_ context.Context, worktree string) error {
		provisioned = append(provisioned, worktree)
		return nil
	}, slog.New(slog.DiscardHandler))
	prepare(t.Context(), state.Session{Name: "shared", Dir: "/repo"})
	prepare(t.Context(), state.Session{Name: "isolated", Worktree: "/wt/isolated"})
	if len(provisioned) != 1 || provisioned[0] != "/wt/isolated" {
		t.Fatalf("provisioned = %q, want the isolated worktree alone", provisioned)
	}
	selfToolsPreparer(nil, slog.New(slog.DiscardHandler))(t.Context(), state.Session{Worktree: "/wt/x"})
}
