package host

import (
	"strings"
	"testing"

	"github.com/Herrscherd/herrscher/core/internal/state"
)

// placerWith returns a placer over a state holding one host record.
func placerWith(t *testing.T, h state.Host, sourceVersion string) hostPlacer {
	t.Helper()
	st := state.NewState(t.TempDir() + "/s.json")
	if err := st.PutHost(h); err != nil {
		t.Fatal(err)
	}
	return hostPlacer{st: st, instanceID: "inst", sourceVersion: func() string { return sourceVersion }}
}

func TestPlacerLocalNeedsNoRecord(t *testing.T) {
	p := placerWith(t, state.Host{Name: "build1", SSH: "me@build1", Workspace: "/srv/work", Bin: "/srv/bin/herrscher"}, "")
	ws, err := p.Workspace("")
	if err != nil {
		t.Fatal(err)
	}
	if ws != p.st.WorkspaceRoot() {
		t.Fatalf("the empty host must be this machine, got %q", ws)
	}
}

func TestPlacerRefusesAnUnprovisionedHost(t *testing.T) {
	p := placerWith(t, state.Host{Name: "build1", SSH: "me@build1", Workspace: "/srv/work"}, "")
	_, err := p.Workspace("build1")
	if err == nil || !strings.Contains(err.Error(), "host provision") {
		t.Fatalf("a host with no binary must point at `host provision`, got %v", err)
	}
}

func TestPlacerRefusesAVersionDrift(t *testing.T) {
	p := placerWith(t, state.Host{Name: "build1", SSH: "me@build1", Workspace: "/srv/work", Bin: "/srv/bin/herrscher", Version: "abc1234"}, "def5678")
	err := p.Ready("build1")
	if err == nil {
		t.Fatal("a host running another build must refuse")
	}
	for _, want := range []string{"abc1234", "def5678", "host provision"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("the refusal must name %q, got %v", want, err)
		}
	}
	// And refuses nothing else: a session already running over there still has
	// to be closable, which is a worktree removal and no argv agreement at all.
	if _, err := p.Worktrees("build1"); err != nil {
		t.Fatalf("a drifted host must still answer worktree work: %v", err)
	}
	if _, err := p.Workspace("build1"); err != nil {
		t.Fatalf("a drifted host must still name its workspace: %v", err)
	}
}

func TestPlacerReadyRefusesAnUnprovisionedHost(t *testing.T) {
	p := placerWith(t, state.Host{Name: "build1", SSH: "me@build1", Workspace: "/srv/work"}, "")
	if err := p.Ready("build1"); err == nil || !strings.Contains(err.Error(), "host provision") {
		t.Fatalf("a host with no binary must point at `host provision`, got %v", err)
	}
	if err := p.Ready(""); err != nil {
		t.Fatalf("this machine is always ready, got %v", err)
	}
}

// A daemon with no source checkout has nothing to compare against. Refusing
// every session then would be worse than running one that is probably current.
func TestPlacerAcceptsWhenTheSourceVersionIsUnknown(t *testing.T) {
	p := placerWith(t, state.Host{Name: "build1", SSH: "me@build1", Workspace: "/srv/work", Bin: "/srv/bin/herrscher", Version: "abc1234"}, "")
	if err := p.Ready("build1"); err != nil {
		t.Fatalf("nothing to compare against is not a drift, got %v", err)
	}
	ws, err := p.Workspace("build1")
	if err != nil {
		t.Fatal(err)
	}
	if ws != "/srv/work" {
		t.Fatalf("workspace must come from the host record, got %q", ws)
	}
}

func TestPlacerRefusesAnUnknownHost(t *testing.T) {
	p := placerWith(t, state.Host{Name: "build1", SSH: "me@build1", Workspace: "/srv/work", Bin: "/srv/bin/herrscher"}, "")
	_, err := p.Worktrees("nowhere")
	if err == nil || !strings.Contains(err.Error(), "nowhere") {
		t.Fatalf("the refusal must name the host asked for, got %v", err)
	}
	if !strings.Contains(err.Error(), "build1") {
		t.Fatalf("the refusal must name what does exist, got %v", err)
	}
}

func TestPlacerEnsureProjectIgnoresLocal(t *testing.T) {
	p := placerWith(t, state.Host{Name: "build1", SSH: "me@build1", Workspace: "/srv/work", Bin: "/srv/bin/herrscher"}, "")
	if err := p.EnsureProject(t.Context(), "", "app", "/w/app"); err != nil {
		t.Fatalf("the daemon's own workspace needs no clone, got %v", err)
	}
	if err := p.EnsureProject(t.Context(), "build1", "", "/srv/work"); err != nil {
		t.Fatalf("a session with no project has nothing to clone, got %v", err)
	}
}
