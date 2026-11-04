package host

import (
	"context"
	"errors"
	"path/filepath"
	"slices"
	"testing"

	contracts "github.com/Herrscherd/herrscher-contracts"
	"github.com/Herrscherd/herrscher/core/accounts"
	"github.com/Herrscherd/herrscher/core/internal/agent"
	"github.com/Herrscherd/herrscher/core/internal/state"
)

var errStopAfterGuard = errors.New("stopped after the account guard")

type stoppingWorktrees struct{}

func (stoppingWorktrees) Create(context.Context, string, string, string) (string, error) {
	return "", errStopAfterGuard
}

func (stoppingWorktrees) PreExisting(context.Context, string, string) bool { return false }

func (stoppingWorktrees) Branch(string) string { return "" }

func (stoppingWorktrees) Remove(context.Context, string, string, bool) error {
	return errStopAfterGuard
}

func (stoppingWorktrees) Scratch(string, string) (string, error) { return "", errStopAfterGuard }

func (stoppingWorktrees) ScratchPath(string, string) string { return "" }

func (stoppingWorktrees) RemoveScratch(string, string) error { return errStopAfterGuard }

func TestWiredManagerRefusesAnUnknownAccount(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	name, err := contracts.ParseProfileName("perso")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := (accounts.Store{Root: accounts.DefaultRoot(home)}).Create("claude", name); err != nil {
		t.Fatal(err)
	}
	st := state.NewState(filepath.Join(t.TempDir(), "state.json"))
	if err := st.SetHome(state.HomeRef{ID: "home", Type: "forum"}); err != nil {
		t.Fatal(err)
	}
	hdl := newManagerHandler(Deps{}, Options{DefaultCmd: "claude"}, t.TempDir(), st, nil, stoppingWorktrees{}, serviceUpdater{}, agent.NewStore(t.TempDir()))
	create := mustCommand(t, hdl.Commands(), "session", "create")
	if _, runErr := create.Run(t.Context(), sessionCreateInput("ghost-session", "ghost")); !errors.Is(runErr, accounts.ErrNotFound) {
		t.Fatalf("session create with an unknown account: err %v, want accounts.ErrNotFound", runErr)
	}
	if _, runErr := create.Run(t.Context(), sessionCreateInput("perso-session", "perso")); !errors.Is(runErr, errStopAfterGuard) {
		t.Fatalf("session create with a known account: err %v, want the placement to be reached", runErr)
	}
}

func sessionCreateInput(name, account string) contracts.Input {
	return contracts.Input{Args: map[string]string{"name": name, "vendor": "claude", "account": account}}
}

func mustCommand(t *testing.T, cmds []contracts.Cmd, path ...string) contracts.Cmd {
	t.Helper()
	for _, cmd := range cmds {
		if slices.Equal(cmd.Path, path) {
			return cmd
		}
	}
	t.Fatalf("command %v is not registered", path)
	return contracts.Cmd{}
}
