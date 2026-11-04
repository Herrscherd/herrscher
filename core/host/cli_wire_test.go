package host

import (
	"errors"
	"path/filepath"
	"slices"
	"testing"

	contracts "github.com/Herrscherd/herrscher-contracts"
	"github.com/Herrscherd/herrscher/core/accounts"
	"github.com/Herrscherd/herrscher/core/internal/agent"
	"github.com/Herrscherd/herrscher/core/internal/state"
)

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
	options := Options{StatePath: filepath.Join(t.TempDir(), "state.json"), DefaultCmd: "claude"}
	hdl := newManagerHandler(Deps{}, options, st, nil, nil, serviceUpdater{}, agent.NewStore(t.TempDir()))
	create := findCommand(t, hdl.Commands(), "session", "create")
	_, runErr := create.Run(t.Context(), contracts.Input{Args: map[string]string{"name": "ghost-session", "vendor": "claude", "account": "ghost"}})
	if !errors.Is(runErr, accounts.ErrNotFound) {
		t.Fatalf("session create with an unknown account: err %v, want accounts.ErrNotFound", runErr)
	}
}

func findCommand(t *testing.T, cmds []contracts.Cmd, path ...string) contracts.Cmd {
	t.Helper()
	for _, cmd := range cmds {
		if slices.Equal(cmd.Path, path) {
			return cmd
		}
	}
	t.Fatalf("command %v is not registered", path)
	return contracts.Cmd{}
}
