package discovery

import (
	"context"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"testing"
)

func TestTheTailscaleStatusListsOnlyOnlinePeersWithAnAddress(t *testing.T) {
	body, err := os.ReadFile(filepath.Join("testdata", "tailscale_status.json"))
	if err != nil {
		t.Fatal(err)
	}
	found, err := candidatesFromTailscaleStatus(body)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{
		"build1": "build1.tailnet.example.ts.net",
		"phone":  "100.64.0.4",
	}
	if len(found) != len(want) {
		t.Fatalf("got %d candidates, want %d: %+v", len(found), len(want), found)
	}
	for _, candidate := range found {
		if candidate.Route != RouteTailnet {
			t.Errorf("%s: route %v, want tailnet", candidate.Name, candidate.Route)
		}
		if !candidate.Fingerprint.IsZero() {
			t.Errorf("%s: a tailnet peer has no announced fingerprint", candidate.Name)
		}
		if want[candidate.Name] != candidate.Address {
			t.Errorf("%s: address %q, want %q", candidate.Name, candidate.Address, want[candidate.Name])
		}
	}
}

func TestTheTailscaleStatusRejectsAnInvalidBody(t *testing.T) {
	if _, err := candidatesFromTailscaleStatus([]byte("not json")); err == nil {
		t.Fatal("an invalid status body must be an error")
	}
}

func TestAMissingTailscaleSocketIsAnEmptySource(t *testing.T) {
	source := Tailscale{Socket: filepath.Join(t.TempDir(), "absent.sock")}
	found, err := source.Find(context.Background())
	if err != nil {
		t.Fatalf("a machine without tailscale must not fail discovery: %v", err)
	}
	if len(found) != 0 {
		t.Fatalf("got candidates from an absent socket: %+v", found)
	}
}

func TestTheTailscaleSourceReadsTheLocalAPIStatus(t *testing.T) {
	body, err := os.ReadFile(filepath.Join("testdata", "tailscale_status.json"))
	if err != nil {
		t.Fatal(err)
	}
	socket := filepath.Join(t.TempDir(), "tailscaled.sock")
	listener, err := net.Listen("unix", socket)
	if err != nil {
		t.Fatal(err)
	}
	server := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/localapi/v0/status" {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write(body)
	})}
	go func() { _ = server.Serve(listener) }()
	t.Cleanup(func() { _ = server.Close() })

	found, err := Tailscale{Socket: socket}.Find(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(found) != 2 {
		t.Fatalf("got %d candidates, want 2: %+v", len(found), found)
	}
}
