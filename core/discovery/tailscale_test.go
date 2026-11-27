package discovery

import (
	"context"
	"errors"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
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

func TestTheTailscaleStatusCleansTheNamesItReads(t *testing.T) {
	body := []byte(`{"Peer":{"a":{"HostName":"evil\u001b]0;pwn\u0007","DNSName":"x\u202e.ts.net.","Online":true}}}`)
	found, err := candidatesFromTailscaleStatus(body)
	if err != nil {
		t.Fatal(err)
	}
	if len(found) != 1 {
		t.Fatalf("got %+v, want one candidate", found)
	}
	for _, text := range []string{found[0].Name, found[0].Address} {
		if strings.ContainsAny(text, "\x1b\x07\u202e") {
			t.Fatalf("a control reached the candidate: %q", text)
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
	socket := serveUnix(t, func(w http.ResponseWriter, _ *http.Request) error {
		_, writeErr := w.Write(body)
		return writeErr
	})

	found, err := Tailscale{Socket: socket}.Find(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(found) != 2 {
		t.Fatalf("got %d candidates, want 2: %+v", len(found), found)
	}
}

func TestTheTailscaleStatusRefusalIsTyped(t *testing.T) {
	socket := serveUnix(t, func(w http.ResponseWriter, _ *http.Request) error {
		w.WriteHeader(http.StatusInternalServerError)
		return nil
	})
	_, err := Tailscale{Socket: socket}.Find(context.Background())
	if !errors.Is(err, ErrTailscaleStatus) {
		t.Fatalf("got %v, want ErrTailscaleStatus", err)
	}
}

func TestTheTailscaleStatusIsReadWithinItsLimit(t *testing.T) {
	padding := strings.Repeat(" ", tailscaleMaxBody+1)
	socket := serveUnix(t, func(w http.ResponseWriter, _ *http.Request) error {
		_, err := w.Write([]byte(padding + "{}"))
		return err
	})
	if _, err := (Tailscale{Socket: socket}).Find(context.Background()); err == nil {
		t.Fatal("a status longer than the limit must be cut before the json is read")
	}
}

func TestTheTailscaleSourceGivesUpOnASilentSocket(t *testing.T) {
	socket := filepath.Join(t.TempDir(), "silent.sock")
	listener, err := (&net.ListenConfig{}).Listen(context.Background(), "unix", socket)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if closeErr := listener.Close(); closeErr != nil {
			t.Error(closeErr)
		}
	})

	started := time.Now()
	_, err = Tailscale{Socket: socket}.Find(context.Background())
	if err == nil {
		t.Fatal("a silent socket must end in an error")
	}
	if elapsed := time.Since(started); elapsed > 3*SourceDeadline {
		t.Fatalf("the source waited %v on a silent socket", elapsed)
	}
}

func serveUnix(t *testing.T, respond func(http.ResponseWriter, *http.Request) error) string {
	t.Helper()
	socket := filepath.Join(t.TempDir(), "tailscaled.sock")
	listener, err := (&net.ListenConfig{}).Listen(context.Background(), "unix", socket)
	if err != nil {
		t.Fatal(err)
	}
	server := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/localapi/v0/status" {
			http.NotFound(w, r)
			return
		}
		if err := respond(w, r); err != nil {
			t.Error(err)
		}
	})}
	served := make(chan error, 1)
	go func() { served <- server.Serve(listener) }()
	t.Cleanup(func() {
		if err := server.Close(); err != nil {
			t.Error(err)
		}
		if err := <-served; !errors.Is(err, http.ErrServerClosed) {
			t.Error(err)
		}
	})
	return socket
}
