package manager

import (
	"errors"
	"strings"
	"testing"

	"github.com/Herrscherd/herrscher/core/internal/agent"
	"github.com/Herrscherd/herrscher/core/internal/state"
)

func TestSessionAccountPriorityIsSessionThenAgentThenDefault(t *testing.T) {
	cases := []struct {
		name, explicit, agent, want string
	}{
		{"explicit session account wins over the agent", "pro", "perso", "pro"},
		{"agent account when the session names none", "", "perso", "perso"},
		{"empty when neither names one: vendor default", "", "", ""},
	}
	for _, tc := range cases {
		if got := chooseSessionAccount(tc.explicit, tc.agent); got != tc.want {
			t.Fatalf("%s: chooseSessionAccount(%q, %q) = %q, want %q", tc.name, tc.explicit, tc.agent, got, tc.want)
		}
	}
}

func TestAccountGateRefusesAnUnknownProfileBeforeCreate(t *testing.T) {
	h := &Handler{validateAccount: func(_, profile string) error {
		if profile == "ghost" {
			return errors.New("no such profile")
		}
		return nil
	}}
	if err := h.checkAccount(runtimeChoice{vendor: "claude"}, "ghost"); err == nil {
		t.Fatal("checkAccount accepted an unknown profile")
	}
	if err := h.checkAccount(runtimeChoice{vendor: "claude"}, "perso"); err != nil {
		t.Fatalf("checkAccount refused a known profile: %v", err)
	}
	if err := h.checkAccount(runtimeChoice{vendor: "claude"}, ""); err != nil {
		t.Fatalf("checkAccount refused the vendor default: %v", err)
	}
}

func newAccountHandler(t *testing.T, validate func(vendor, profile string) error, resolve func(vendor, modelID, cmd string) (string, error)) *Handler {
	t.Helper()
	return NewHandler(nil, nil, nil, nil, nil, agent.NewStore(t.TempDir()), nil, "", "", nil, validate, resolve)
}

func TestAccountWithoutAResolvableVendorIsRefused(t *testing.T) {
	accept := func(_, _ string) error { return nil }
	if err := newAccountHandler(t, accept, nil).checkAccount(runtimeChoice{}, "ghost"); !errors.Is(err, ErrAccountVendor) {
		t.Fatalf("checkAccount without a vendor: err %v, want ErrAccountVendor", err)
	}
	unresolved := func(string, string, string) (string, error) { return "", errors.New("no backend") }
	if err := newAccountHandler(t, accept, unresolved).checkAccount(runtimeChoice{}, "ghost"); !errors.Is(err, ErrAccountVendor) {
		t.Fatalf("checkAccount with an unresolved vendor: err %v, want ErrAccountVendor", err)
	}
}

func TestAccountIsCheckedAgainstTheResolvedVendor(t *testing.T) {
	var checked string
	h := newAccountHandler(t, func(vendor, _ string) error {
		checked = vendor
		return nil
	}, func(string, string, string) (string, error) { return "codex", nil })
	if err := h.checkAccount(runtimeChoice{}, "perso"); err != nil {
		t.Fatal(err)
	}
	if checked != "codex" {
		t.Fatalf("account checked against %q, want the resolved vendor codex", checked)
	}
}

func TestAccountResolutionIsGivenTheSessionCommand(t *testing.T) {
	var given string
	h := newAccountHandler(t, func(string, string) error { return nil }, func(_, _, cmd string) (string, error) {
		given = cmd
		return "claude", nil
	})
	if err := h.checkAccount(runtimeChoice{cmd: "claude --effort medium"}, "perso"); err != nil {
		t.Fatal(err)
	}
	if given != "claude --effort medium" {
		t.Fatalf("vendor resolved from cmd %q, want the session command", given)
	}
}

func TestAccountOnAnotherHostIsRefusedBeforeTheLocalCheck(t *testing.T) {
	h, _, _, _, _, st := newTestHandler(t, "forum")
	st.Home = state.HomeRef{ID: "home", Type: "forum"}
	h.validateAccount = func(_, _ string) error { return nil }
	_, err := h.parseCreateRequest(t.Context(), args("name", "remote-session", "vendor", "claude", "account", "perso", "host", "box"))
	if !errors.Is(err, ErrAccountHost) {
		t.Fatalf("account on a remote host: err %v, want ErrAccountHost", err)
	}
	if !strings.Contains(err.Error(), `"box"`) {
		t.Fatalf("error does not name the host: %v", err)
	}
}

func TestCreateRefusesAnUnknownAccountThroughParseCreateRequest(t *testing.T) {
	h, _, _, _, _, st := newTestHandler(t, "forum")
	st.Home = state.HomeRef{ID: "home", Type: "forum"}
	h.validateAccount = func(_, profile string) error {
		if profile == "ghost" {
			return errors.New("no such profile")
		}
		return nil
	}
	if _, err := h.parseCreateRequest(t.Context(), args("name", "control", "vendor", "claude", "account", "perso")); err != nil {
		t.Fatalf("known account refused: %v", err)
	}
	if _, err := h.parseCreateRequest(t.Context(), args("name", "ghost-session", "vendor", "claude", "account", "ghost")); err == nil {
		t.Fatal("parseCreateRequest accepted an unknown account")
	}
}
