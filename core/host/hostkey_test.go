package host

import (
	"context"
	"errors"
	"io"
	"testing"
	"time"

	contracts "github.com/Herrscherd/herrscher-contracts"
	"github.com/Herrscherd/herrscher/core/clitext"
	"github.com/Herrscherd/herrscher/core/internal/state"
)

func placerWithSession(t *testing.T, h state.Host) hostPlacer {
	t.Helper()
	p := placerWith(t, h, "")
	if err := p.st.AddSession(state.Session{Name: h.Name + "-run-a", Host: h.Name}); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestPlacerRefusesAChangedHostKeyBeforeAnyConnection(t *testing.T) {
	p := placerWith(t, state.Host{Name: "build1", SSH: "me@build1", Workspace: "/srv/work", Bin: "/srv/bin/herrscher", HostKey: "SHA256:old", KnownHosts: "/nowhere", KeyChanged: "SHA256:new"}, "")
	err := p.Ready(t.Context(), "build1")
	if !errors.Is(err, errHostKeyChanged) {
		t.Fatalf("a host whose key changed must refuse new sessions, got %v", err)
	}
}

func TestPlacerRefusesAFullHostUnderRefuse(t *testing.T) {
	p := placerWithSession(t, state.Host{Name: "build1", SSH: "me@build1", Workspace: "/srv/work", Bin: "/srv/bin/herrscher", MaxSessions: 1})
	if err := p.Ready(t.Context(), "build1"); !errors.Is(err, errHostFull) {
		t.Fatalf("a host at its cap must refuse under the default policy, got %v", err)
	}
}

func TestPlacerAdmitsBelowTheCap(t *testing.T) {
	p := placerWithSession(t, state.Host{Name: "build1", SSH: "me@build1", Workspace: "/srv/work", Bin: "/srv/bin/herrscher", MaxSessions: 2})
	if err := p.Ready(t.Context(), "build1"); err != nil {
		t.Fatalf("a host below its cap must take a session, got %v", err)
	}
}

func TestPlacerWaitsForARoomWhenAskedTo(t *testing.T) {
	p := placerWithSession(t, state.Host{Name: "build1", SSH: "me@build1", Workspace: "/srv/work", Bin: "/srv/bin/herrscher", MaxSessions: 1, WhenFull: state.FullWait})
	p.pollInterval = 5 * time.Millisecond
	p.waitLimit = 5 * time.Second
	go func() {
		time.Sleep(30 * time.Millisecond)
		if err := p.st.RemoveSession("build1-run-a"); err != nil {
			t.Error(err)
		}
	}()
	if err := p.Ready(t.Context(), "build1"); err != nil {
		t.Fatalf("a waiting session must start once a slot frees, got %v", err)
	}
}

func TestPlacerGivesUpWaitingAtTheLimit(t *testing.T) {
	p := placerWithSession(t, state.Host{Name: "build1", SSH: "me@build1", Workspace: "/srv/work", Bin: "/srv/bin/herrscher", MaxSessions: 1, WhenFull: state.FullWait})
	p.pollInterval = 5 * time.Millisecond
	p.waitLimit = 30 * time.Millisecond
	if err := p.Ready(t.Context(), "build1"); !errors.Is(err, errHostWaitLimit) || !errors.Is(err, errHostFull) {
		t.Fatalf("a host that stays full must end the wait with the limit and the cause, got %v", err)
	}
}

func TestPlacerWaitStopsWithItsContext(t *testing.T) {
	p := placerWithSession(t, state.Host{Name: "build1", SSH: "me@build1", Workspace: "/srv/work", Bin: "/srv/bin/herrscher", MaxSessions: 1, WhenFull: state.FullWait})
	p.pollInterval = 5 * time.Millisecond
	p.waitLimit = time.Minute
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Millisecond)
	defer cancel()
	if err := p.Ready(ctx, "build1"); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("a cancelled create must stop waiting, got %v", err)
	}
}

func TestPlacerRefusesAProjectOffTheHostList(t *testing.T) {
	p := placerWith(t, state.Host{Name: "build1", SSH: "me@build1", Workspace: "/srv/work", Bin: "/srv/bin/herrscher", Projects: []string{"app"}}, "")
	if err := p.EnsureProject(t.Context(), "build1", "other", "/srv/work/other"); !errors.Is(err, errProjectNotAllowed) {
		t.Fatalf("a project missing from the host list must be refused before any clone, got %v", err)
	}
}

func TestProjectAllowedWhenTheListIsEmpty(t *testing.T) {
	if !projectAllowed(state.Host{}, "anything") {
		t.Fatal("a host with no project list keeps its behaviour: every project is allowed")
	}
	if projectAllowed(state.Host{Projects: []string{"app"}}, "other") {
		t.Fatal("a project outside a non-empty list must be refused")
	}
}

func TestApplySettingsParsesTheHostFlags(t *testing.T) {
	var host state.Host
	in := contracts.Input{Args: map[string]string{paramAllowProject: " app, api ,, ", paramMaxSessions: "3", paramWhenFull: "wait"}}
	if err := applySettings(&host, in); err != nil {
		t.Fatal(err)
	}
	if len(host.Projects) != 2 || host.Projects[0] != "app" || host.Projects[1] != "api" {
		t.Fatalf("the project list must be split on commas and trimmed, got %q", host.Projects)
	}
	if host.MaxSessions != 3 || host.WhenFull != state.FullWait {
		t.Fatalf("cap and policy must be read, got %d %q", host.MaxSessions, host.WhenFull)
	}
}

func TestApplySettingsRefusesBadValues(t *testing.T) {
	cases := map[string]contracts.Input{
		"negative cap":        {Args: map[string]string{paramMaxSessions: "-1"}},
		"text cap":            {Args: map[string]string{paramMaxSessions: "many"}},
		"unknown policy":      {Args: map[string]string{paramWhenFull: "queue"}},
		"projects with clear": {Args: map[string]string{paramAllowProject: "app", paramAllowAll: "true"}},
	}
	for name, in := range cases {
		var host state.Host
		if err := applySettings(&host, in); err == nil {
			t.Fatalf("%s must be refused", name)
		}
	}
}

func TestApplySettingsAllowAllClearsTheList(t *testing.T) {
	host := state.Host{Projects: []string{"app"}}
	if err := applySettings(&host, contracts.Input{Args: map[string]string{paramAllowAll: "true"}}); err != nil {
		t.Fatal(err)
	}
	if len(host.Projects) != 0 {
		t.Fatalf("--allow-all-projects must empty the list, got %q", host.Projects)
	}
}

func TestHostKeyStateNamesTheBlockedAndUnpinnedHosts(t *testing.T) {
	cases := map[string]struct {
		host state.Host
		want clitext.Key
	}{
		"pinned":    {state.Host{HostKey: "SHA256:a"}, clitext.HostKeyStatePinned},
		"unpinned":  {state.Host{}, clitext.HostKeyStateNotPinned},
		"blocked":   {state.Host{HostKey: "SHA256:a", KeyChanged: "SHA256:b"}, clitext.HostKeyStateBlocked},
		"no record": {state.Host{Name: "x"}, clitext.HostKeyStateNotPinned},
	}
	for name, c := range cases {
		if got := hostKeyStateKey(c.host); got != c.want {
			t.Fatalf("%s: want %q, got %q", name, c.want, got)
		}
	}
}

func TestPlacerNamesAnUnpinnedHostAndStillAdmitsIt(t *testing.T) {
	p := placerWith(t, state.Host{Name: "build1", SSH: "me@build1", Workspace: "/srv/work", Bin: "/srv/bin/herrscher"}, "")
	p.warn = io.Discard
	if err := p.Ready(t.Context(), "build1"); err != nil {
		t.Fatalf("a host registered before pinning stays usable, got %v", err)
	}
}
