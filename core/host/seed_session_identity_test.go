package host

import (
	"context"
	"strings"
	"testing"

	contracts "github.com/Herrscherd/herrscher-contracts"
	"github.com/Herrscherd/herrscher/core/internal/control"
)

func TestSeedBackendCarriesTheSessionIdentity(t *testing.T) {
	settings := make(map[string]string)
	applyBackendRequest(settings, BackendRequest{Session: "first"}, modelRoute{})
	want := contracts.EncodeEnvSetting(map[string]string{control.SessionVar: "first"})
	if settings["env"] != want {
		t.Fatalf("env setting %q, want %q", settings["env"], want)
	}
}

func TestSeedBackendCarriesTheSessionToken(t *testing.T) {
	settings := make(map[string]string)
	applyBackendRequest(settings, BackendRequest{Session: "first", SessionToken: "tok-first"}, modelRoute{})
	want := contracts.EncodeEnvSetting(map[string]string{control.SessionVar: "first", control.ApprovalKeyVar: "tok-first"})
	if settings["env"] != want {
		t.Fatalf("env setting %q, want the session and its token %q", settings["env"], want)
	}
}

func TestBuildBackendForRefusesASessionOnARemoteBackend(t *testing.T) {
	fake := &fakeRemoteResolver{}
	_, err := BuildBackendFor(context.Background(), NewMessages(), BackendRequest{Vendor: "claude", Cmd: "claude", Session: "first", resolver: fake})
	if err == nil {
		t.Fatal("a session on a remote backend must be refused: its identity cannot reach the remote side, so its approvals would be silently absent")
	}
	if !strings.Contains(err.Error(), "first") {
		t.Fatalf("error does not name the session: %v", err)
	}
	if !fake.called {
		t.Fatal("the remote resolver seam was never consulted; the test proves nothing")
	}
}
