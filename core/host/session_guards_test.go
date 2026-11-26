package host

import (
	"context"
	"errors"
	"log/slog"
	"path/filepath"
	"testing"
	"time"

	contracts "github.com/Herrscherd/herrscher-contracts"
	"github.com/Herrscherd/herrscher/core/internal/state"
)

func TestASessionRestartsTheDaemonOncePerWindowAcrossRestarts(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	st, err := state.LoadState(path)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 11, 26, 9, 0, 0, 0, time.UTC)
	clock := func() time.Time { return now }
	restarts := newSelfRestarts(st)
	restarts.now = clock
	if err = restarts.admit("s1", verbServiceRestart); err != nil {
		t.Fatalf("first restart: %v", err)
	}
	reloaded, err := state.LoadState(path)
	if err != nil {
		t.Fatal(err)
	}
	next := newSelfRestarts(reloaded)
	next.now = clock
	now = now.Add(selfRestartWindow / 2)
	if err = next.admit("s1", verbServiceUpdate); err == nil {
		t.Fatal("an update inside the window, from the daemon that came back, was admitted")
	}
	if err = next.admit("s2", verbServiceRestart); err != nil {
		t.Fatalf("another session's restart: %v", err)
	}
	now = now.Add(selfRestartWindow)
	if err = next.admit("s1", verbServiceRestart); err != nil {
		t.Fatalf("restart after the window: %v", err)
	}
	resumes, err := reloaded.TakeRestartResumes()
	if err != nil || len(resumes) != 2 {
		t.Fatalf("resumes = %v, %v, want s1 and s2 told to carry on", resumes, err)
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

func TestTheHealthProbeReadsTheToolsTheServerListed(t *testing.T) {
	out := []byte(`{"jsonrpc":"2.0","id":1,"result":{"protocolVersion":"2025-06-18"}}
{"jsonrpc":"2.0","id":2,"result":{"tools":[{"name":"herrscher_commands"},{"name":"herrscher_run"},{"name":"session_list"}]}}
`)
	if tools, err := listedTools(out); err != nil || tools != 3 {
		t.Fatalf("listedTools = %d, %v, want 3", tools, err)
	}
	if _, err := listedTools([]byte(`{"jsonrpc":"2.0","id":1,"result":{}}`)); !errors.Is(err, errSelfToolsSilent) {
		t.Fatalf("a server that listed nothing: %v, want errSelfToolsSilent", err)
	}
}
