package state

import (
	"path/filepath"
	"testing"
)

func TestHostsRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	s := NewState(path)
	h := Host{Name: "build1", SSH: "me@build1", Workspace: "/srv/work", Bin: "/home/me/.herrscher/bin/herrscher", Version: "abc1234", GOOS: "linux", GOARCH: "arm64"}
	if err := s.PutHost(h); err != nil {
		t.Fatal(err)
	}
	back, err := LoadState(path)
	if err != nil {
		t.Fatal(err)
	}
	got, ok := back.FindHost("build1")
	if !ok {
		t.Fatal("host did not survive the round trip")
	}
	if got != h {
		t.Fatalf("host = %+v, want %+v", got, h)
	}
}

func TestPutHostReplacesByName(t *testing.T) {
	s := NewState(filepath.Join(t.TempDir(), "state.json"))
	if err := s.PutHost(Host{Name: "build1", SSH: "me@old"}); err != nil {
		t.Fatal(err)
	}
	if err := s.PutHost(Host{Name: "build1", SSH: "me@new"}); err != nil {
		t.Fatal(err)
	}
	if got := s.SnapshotHosts(); len(got) != 1 || got[0].SSH != "me@new" {
		t.Fatalf("hosts = %+v", got)
	}
}

func TestRemoveHostReportsWhetherItWasThere(t *testing.T) {
	s := NewState(filepath.Join(t.TempDir(), "state.json"))
	if err := s.PutHost(Host{Name: "build1"}); err != nil {
		t.Fatal(err)
	}
	found, err := s.RemoveHost("build1")
	if err != nil || !found {
		t.Fatalf("found=%v err=%v", found, err)
	}
	found, err = s.RemoveHost("build1")
	if err != nil || found {
		t.Fatalf("second remove: found=%v err=%v", found, err)
	}
}

// local is implicit: it always exists, and it is not in the file.
func TestLocalIsNeverAStoredHost(t *testing.T) {
	s := NewState(filepath.Join(t.TempDir(), "state.json"))
	if _, ok := s.FindHost(LocalHost); ok {
		t.Fatal("local must not be a stored record")
	}
}

// A session written before hosts existed stays local, which is the safe
// direction: no field, no remote.
func TestSessionHostDefaultsToEmpty(t *testing.T) {
	if (Session{Name: "s"}).Host != "" {
		t.Fatal("a session must be local until it says otherwise")
	}
}
