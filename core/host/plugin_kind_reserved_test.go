package host

import (
	"context"
	"errors"
	"io"
	"path/filepath"
	"testing"

	contracts "github.com/Herrscherd/herrscher-contracts"
	"github.com/Herrscherd/herrscher-contracts/i18n"
	"github.com/Herrscherd/herrscher/core/internal/state"
	"github.com/Herrscherd/herrscher/core/internal/supervisor"
)

func buildRegistryWithPluginCommands(t *testing.T, extra ...contracts.Cmd) error {
	t.Helper()
	ctx := t.Context()
	dir := t.TempDir()
	store := state.NewState(filepath.Join(dir, "state.json"))
	sup := supervisor.NewSupervisor(ctx, "/nonexistent/herrscher", testRuntime(t))
	options := Options{Runtime: testRuntime(t), StatePath: filepath.Join(dir, "state.json"), DefaultCmd: "claude", Language: i18n.English}
	_, _, err := buildRegistry(ctx, Deps{}, options, store, sup, "", extra, newApprovalTable(io.Discard), newSessionTable(newHostMessages()))
	return err
}

func TestPluginKindReusingACoreFamilyIsRefused(t *testing.T) {
	plugin := contracts.New("session", "channel", "read").Do(func(context.Context, contracts.Input) (contracts.Result, error) {
		return contracts.Result{}, nil
	})
	err := buildRegistryWithPluginCommands(t, plugin)
	var reserved *pluginKindReservedError
	if !errors.As(err, &reserved) || reserved.Kind != "session" {
		t.Fatalf("a plugin Kind reusing the core family session was accepted: err = %v", err)
	}
}

func TestPluginKindWithAFreshFamilyIsAccepted(t *testing.T) {
	plugin := contracts.New("acmeplug", "channel", "read").Do(func(context.Context, contracts.Input) (contracts.Result, error) {
		return contracts.Result{}, nil
	})
	if err := buildRegistryWithPluginCommands(t, plugin); err != nil {
		t.Fatalf("a plugin Kind with its own family was refused: %v", err)
	}
}
