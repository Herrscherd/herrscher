package host

import (
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
