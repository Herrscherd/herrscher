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
