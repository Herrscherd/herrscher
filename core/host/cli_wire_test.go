package host

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	contracts "github.com/Herrscherd/herrscher-contracts"
	"github.com/Herrscherd/herrscher/core/accounts"
	"github.com/Herrscherd/herrscher/core/internal/agent"
	"github.com/Herrscherd/herrscher/core/internal/manager"
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
	hdl := newManagerHandler(Deps{}, Options{DefaultCmd: "claude"}, t.TempDir(), st, nil, stoppingWorktrees{}, serviceUpdater{}, agent.NewStore(t.TempDir()), nil)
	create := mustCommand(t, hdl.Commands(), "session", "create")
	if _, runErr := create.Run(t.Context(), sessionCreateInput("ghost-session", "ghost")); !errors.Is(runErr, accounts.ErrNotFound) {
		t.Fatalf("session create with an unknown account: err %v, want accounts.ErrNotFound", runErr)
	}
	t.Setenv("HERRSCHER_BACKEND", "")
	noVendor := contracts.Input{Args: map[string]string{"name": "ghost-novendor", "account": "ghost"}}
	if _, runErr := create.Run(t.Context(), noVendor); !errors.Is(runErr, manager.ErrAccountVendor) {
		t.Fatalf("session create with an account and no resolvable vendor: err %v, want manager.ErrAccountVendor", runErr)
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

func TestWiredManagerRefusesAccountWithRemoteBackend(t *testing.T) {
	st := state.NewState(filepath.Join(t.TempDir(), "state.json"))
	if err := st.SetHome(state.HomeRef{ID: "home", Type: "forum"}); err != nil {
		t.Fatal(err)
	}
	remote := make(map[contracts.Category]bool)
	remote[contracts.CategoryBackend] = true
	options := Options{DefaultCmd: "claude", RemoteCategories: remote}
	hdl := newManagerHandler(Deps{}, options, t.TempDir(), st, nil, stoppingWorktrees{}, serviceUpdater{}, agent.NewStore(t.TempDir()), nil)
	create := mustCommand(t, hdl.Commands(), "session", "create")
	if _, runErr := create.Run(t.Context(), sessionCreateInput("remote-session", "perso")); !errors.Is(runErr, manager.ErrAccountRemote) {
		t.Fatalf("session create with an account and a remote backend: err %v, want manager.ErrAccountRemote before any placement", runErr)
	}
}

func TestWiredManagerResolvesTheVendorFromTheBackendEnvironment(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("HERRSCHER_BACKEND", "codex")
	name, err := contracts.ParseProfileName("pro")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := (accounts.Store{Root: accounts.DefaultRoot(home)}).Create("codex", name); err != nil {
		t.Fatal(err)
	}
	st := state.NewState(filepath.Join(t.TempDir(), "state.json"))
	if err := st.SetHome(state.HomeRef{ID: "home", Type: "forum"}); err != nil {
		t.Fatal(err)
	}
	hdl := newManagerHandler(Deps{}, Options{DefaultCmd: "codex"}, t.TempDir(), st, nil, stoppingWorktrees{}, serviceUpdater{}, agent.NewStore(t.TempDir()), nil)
	create := mustCommand(t, hdl.Commands(), "session", "create")
	noVendor := contracts.Input{Args: map[string]string{"name": "pro-session", "account": "pro"}}
	if _, runErr := create.Run(t.Context(), noVendor); !errors.Is(runErr, errStopAfterGuard) {
		t.Fatalf("session create with account pro and vendor from HERRSCHER_BACKEND=codex: err %v, want the placement to be reached", runErr)
	}
}

func TestWiredManagerChecksTheAccountOfAnAgent(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	name, err := contracts.ParseProfileName("pro")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := (accounts.Store{Root: accounts.DefaultRoot(home)}).Create("claude", name); err != nil {
		t.Fatal(err)
	}
	agents := agent.NewStore(t.TempDir())
	if _, err := agents.Create(agent.CreateSpec{Name: "bot"}); err != nil {
		t.Fatal(err)
	}
	setAgentAccount := func(profile string) {
		if err := os.WriteFile(filepath.Join(agents.Root(), "bot", "account"), []byte(profile+"\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	st := state.NewState(filepath.Join(t.TempDir(), "state.json"))
	if err := st.SetHome(state.HomeRef{ID: "home", Type: "forum"}); err != nil {
		t.Fatal(err)
	}
	hdl := newManagerHandler(Deps{}, Options{DefaultCmd: "claude"}, t.TempDir(), st, nil, stoppingWorktrees{}, serviceUpdater{}, agents, nil)
	create := mustCommand(t, hdl.Commands(), "session", "create")
	agentInput := contracts.Input{Args: map[string]string{"name": "bot-session", "agent": "bot", "vendor": "claude"}}

	setAgentAccount("ghost")
	_, runErr := create.Run(t.Context(), agentInput)
	if !errors.Is(runErr, accounts.ErrNotFound) {
		t.Fatalf("session create for an agent whose account is missing: err %v, want accounts.ErrNotFound", runErr)
	}
	if !strings.Contains(runErr.Error(), `the account of agent "bot"`) {
		t.Fatalf("err %q must say the account comes from the agent", runErr)
	}

	setAgentAccount("pro")
	if _, runErr := create.Run(t.Context(), agentInput); !errors.Is(runErr, errStopAfterGuard) {
		t.Fatalf("session create for an agent whose account exists: err %v, want the placement to be reached", runErr)
	}
}

func TestWiredManagerValidatesMemoryProjectBeforeTheAccount(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	st := state.NewState(filepath.Join(t.TempDir(), "state.json"))
	if err := st.SetHome(state.HomeRef{ID: "home", Type: "forum"}); err != nil {
		t.Fatal(err)
	}
	hdl := newManagerHandler(Deps{}, Options{DefaultCmd: "claude"}, t.TempDir(), st, nil, stoppingWorktrees{}, serviceUpdater{}, agent.NewStore(t.TempDir()), nil)
	create := mustCommand(t, hdl.Commands(), "session", "create")
	input := contracts.Input{Args: map[string]string{"name": "mp-session", "vendor": "claude", "account": "ghost", "memory_project": "a/b"}}
	_, runErr := create.Run(t.Context(), input)
	var invalid *manager.InvalidValueError
	if !errors.As(runErr, &invalid) || invalid.Field != "memory_project" {
		t.Fatalf("err %v, want the memory_project validation before the account check", runErr)
	}
}

func TestWiredManagerWrapsEveryRefusalOfAnAgentAccount(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	name, err := contracts.ParseProfileName("pro")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := (accounts.Store{Root: accounts.DefaultRoot(home)}).Create("claude", name); err != nil {
		t.Fatal(err)
	}
	agents := agent.NewStore(t.TempDir())
	if _, err := agents.Create(agent.CreateSpec{Name: "far", Host: "elsewhere"}); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(agents.Root(), "far", "account"), []byte("pro\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	st := state.NewState(filepath.Join(t.TempDir(), "state.json"))
	if err := st.SetHome(state.HomeRef{ID: "home", Type: "forum"}); err != nil {
		t.Fatal(err)
	}
	hdl := newManagerHandler(Deps{}, Options{DefaultCmd: "claude"}, t.TempDir(), st, nil, stoppingWorktrees{}, serviceUpdater{}, agents, nil)
	create := mustCommand(t, hdl.Commands(), "session", "create")
	_, runErr := create.Run(t.Context(), contracts.Input{Args: map[string]string{"name": "far-session", "agent": "far", "vendor": "claude"}})
	if !errors.Is(runErr, manager.ErrAccountHost) {
		t.Fatalf("session create for an agent on another host with its account: err %v, want manager.ErrAccountHost", runErr)
	}
	if !strings.Contains(runErr.Error(), `the account of agent "far"`) {
		t.Fatalf("err %q must say the host refusal comes from the agent's account", runErr)
	}
}
