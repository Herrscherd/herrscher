package host

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	contracts "github.com/Herrscherd/herrscher-contracts"
	"github.com/Herrscherd/herrscher/core/cli"
	"github.com/Herrscherd/herrscher/core/internal/state"
)

func hostRegistry(t *testing.T) (*cli.Registry, *state.State) {
	t.Helper()
	st := state.NewState(filepath.Join(t.TempDir(), "state.json"))
	reg := &cli.Registry{}
	if err := addHostCommands(reg, st); err != nil {
		t.Fatal(err)
	}
	return reg, st
}

func runHostVerb(t *testing.T, reg *cli.Registry, verb string, kv map[string]string) (string, error) {
	t.Helper()
	in := contracts.Input{Args: map[string]string{}}
	for k, v := range kv {
		in.Args[k] = v
	}
	return reg.Run(context.Background(), []string{"host", verb}, in)
}

func TestHostAddRefusesTheNameLocal(t *testing.T) {
	reg, _ := hostRegistry(t)
	_, err := runHostVerb(t, reg, "add", map[string]string{"name": "local", "ssh": "me@b1", "workspace": "/srv/work"})
	if err == nil || !strings.Contains(err.Error(), "local") {
		t.Fatalf("want a refusal naming local, got %v", err)
	}
}

func TestHostAddNeedsAnSSHTargetAndAnAbsoluteWorkspace(t *testing.T) {
	reg, _ := hostRegistry(t)
	if _, err := runHostVerb(t, reg, "add", map[string]string{"name": "build1", "workspace": "/srv/work"}); err == nil {
		t.Fatal("want a refusal without --ssh")
	}
	if _, err := runHostVerb(t, reg, "add", map[string]string{"name": "build1", "ssh": "me@b1", "workspace": "work"}); err == nil {
		t.Fatal("want a refusal for a relative workspace")
	}
}

func TestHostListNamesLocalEvenWithNoRecords(t *testing.T) {
	reg, _ := hostRegistry(t)
	out, err := runHostVerb(t, reg, "list", nil)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "local") {
		t.Fatalf("list does not name local: %s", out)
	}
}

func TestHostCheckOnAnUnknownHostNamesWhatExists(t *testing.T) {
	reg, st := hostRegistry(t)
	if err := st.PutHost(state.Host{Name: "build1", SSH: "me@b1"}); err != nil {
		t.Fatal(err)
	}
	_, err := runHostVerb(t, reg, "check", map[string]string{"name": "ghost"})
	if err == nil || !strings.Contains(err.Error(), "build1") {
		t.Fatalf("the refusal does not name what exists: %v", err)
	}
}

func TestHostRmRefusesAHostThatCarriesSessions(t *testing.T) {
	reg, st := hostRegistry(t)
	if err := st.PutHost(state.Host{Name: "build1", SSH: "me@b1", Workspace: "/srv/work"}); err != nil {
		t.Fatal(err)
	}
	if err := st.AddSession(state.Session{Name: "s1", ChannelID: "c1", Host: "build1"}); err != nil {
		t.Fatal(err)
	}
	_, err := runHostVerb(t, reg, "rm", map[string]string{"name": "build1"})
	if err == nil {
		t.Fatal("want a refusal")
	}
	if !strings.Contains(err.Error(), "s1") {
		t.Fatalf("the refusal does not name the session: %v", err)
	}
	if _, ok := st.FindHost("build1"); !ok {
		t.Fatal("the host was removed anyway")
	}
}

func TestHostRmDropsAFreeHost(t *testing.T) {
	reg, st := hostRegistry(t)
	if err := st.PutHost(state.Host{Name: "build1", SSH: "me@b1"}); err != nil {
		t.Fatal(err)
	}
	if _, err := runHostVerb(t, reg, "rm", map[string]string{"name": "build1"}); err != nil {
		t.Fatal(err)
	}
	if _, ok := st.FindHost("build1"); ok {
		t.Fatal("the host is still there")
	}
}

// A local session is not on any host, so it cannot hold one hostage.
func TestHostRmIgnoresLocalSessions(t *testing.T) {
	reg, st := hostRegistry(t)
	if err := st.PutHost(state.Host{Name: "build1", SSH: "me@b1"}); err != nil {
		t.Fatal(err)
	}
	if err := st.AddSession(state.Session{Name: "here", ChannelID: "c1"}); err != nil {
		t.Fatal(err)
	}
	if _, err := runHostVerb(t, reg, "rm", map[string]string{"name": "build1"}); err != nil {
		t.Fatal(err)
	}
}
