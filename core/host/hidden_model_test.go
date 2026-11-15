package host

import (
	"bytes"
	"context"
	"log/slog"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Herrscherd/herrscher/core/cli"

	contracts "github.com/Herrscherd/herrscher-contracts"
)

func TestModelsListAllShowsHiddenWhenRequested(t *testing.T) {
	saved := contracts.Default
	t.Cleanup(func() { contracts.Default = saved })
	contracts.Default = contracts.Registry{}
	for _, plugin := range pluginsWithRetiredModel() {
		contracts.Default.Register(plugin)
	}
	t.Setenv("HERRSCHER_STATE_DIR", t.TempDir())
	statePath := filepath.Join(t.TempDir(), "state.json")
	list := modelsVerb(t, "list")

	plain, err := cli.Plain(list.Run(context.Background(), contracts.Input{JSON: true, Args: map[string]string{"state": statePath}}))
	if err != nil {
		t.Fatalf("models list --json: %v", err)
	}
	if strings.Contains(plain, `"id":"a-retired"`) {
		t.Fatalf("default listing offers the hidden model: %s", plain)
	}

	all, err := cli.Plain(list.Run(context.Background(), contracts.Input{JSON: true, Args: map[string]string{"state": statePath, "all": "true"}}))
	if err != nil {
		t.Fatalf("models list --all --json: %v", err)
	}
	if !strings.Contains(all, `"id":"a-retired"`) || !strings.Contains(all, `"hidden":true`) {
		t.Fatalf("--all does not list the hidden model marked hidden: %s", all)
	}
}

func TestHiddenModelFromEnvResolvesWithWarningAndNeverSwitches(t *testing.T) {
	var logs bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logs, nil)))
	t.Cleanup(func() { slog.SetDefault(previous) })
	t.Setenv(envModelsHidden, "a-visible")
	models := ModelDiscoveryFor(filepath.Join(t.TempDir(), "state.json"))

	vendor, entry, err := chooseVendor("", "", "a-visible", pluginsWithRetiredModel(), models)
	if err != nil {
		t.Fatalf("chooseVendor on an env-hidden model: %v", err)
	}
	if vendor != "alpha" || entry.ID != "a-visible" {
		t.Fatalf("resolved %q on %q, want a-visible on alpha: the host must not switch", entry.ID, vendor)
	}
	if !strings.Contains(logs.String(), "hidden from the selector") || !strings.Contains(logs.String(), "a-visible") {
		t.Fatalf("no warning for the env-hidden model, logs: %s", logs.String())
	}
}
