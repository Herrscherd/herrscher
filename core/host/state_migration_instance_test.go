package host

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Herrscherd/herrscher/core/clitext"
	"github.com/Herrscherd/herrscher/core/internal/state"
)

func alphaFormat0State(t *testing.T, from string) string {
	t.Helper()
	return writeFormat0State(t, []map[string]any{{"name": "alpha", "channelID": "c1", "type": "channel", "cmd": "claude", "worktree": from, "gateways": []string{"chat"}}}, nil)
}

func TestMigrationRefusesAnInstanceIDAnotherDaemonOnThisMachineHolds(t *testing.T) {
	_, from := bareSessionRepo(t, "alpha")
	path := alphaFormat0State(t, from)
	layout := testRuntime(t)
	release, err := lockInstance(newHostMessages(), layout, "alice")
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	inputs := hubMigrationInputs(t.Context(), newHostMessages(), Options{InstanceID: "alice", DefaultGateways: []string{"chat"}, Runtime: layout})
	err = migrateHubState(t.Context(), newHostMessages(), discardAnnouncer(), loadForMigration(t, path), inputs)
	assertRefusedUntouched(t, path, err, "HERRSCHER_INSTANCE_ID")
	if refusal, _ := errors.AsType[*stateMigrationRefusedError](err); refusal.key != clitext.MigrationInstanceRunning {
		t.Fatalf("refusal key = %s, want %s", refusal.key, clitext.MigrationInstanceRunning)
	}
	if _, statErr := os.Stat(from); statErr != nil {
		t.Fatalf("the worktree moved despite the refusal: %v", statErr)
	}
}

func TestMigrationRefusesAnInstanceIDWhoseFolderHoldsWorktreesOfNoSessionOfThisState(t *testing.T) {
	for _, foreign := range []string{"worktree", "branch"} {
		repo, from := bareSessionRepo(t, "alpha")
		if foreign == "worktree" {
			gitIn(t.Context(), t, repo, "worktree", "add", "-q", filepath.Join(repo, ".herrscher-sessions", "alice", "y"), "-b", "session/alice/y")
		} else {
			gitIn(t.Context(), t, repo, "branch", "session/alice/y")
		}
		path := alphaFormat0State(t, from)
		err := migrateHubState(t.Context(), newHostMessages(), discardAnnouncer(), loadForMigration(t, path), migrationInputs{optID: "alice", defaultGateways: []string{"chat"}})
		assertRefusedUntouched(t, path, err, "HERRSCHER_INSTANCE_ID")
		if refusal, _ := errors.AsType[*stateMigrationRefusedError](err); refusal.key != clitext.MigrationInstanceInUse || !strings.Contains(fmt.Sprint(refusal.params[clitext.ParamTarget]), "y") {
			t.Fatalf("%s: refusal = %v, want %s naming y", foreign, err, clitext.MigrationInstanceInUse)
		}
	}
}

func TestAResumedMigrationCountsTheSessionsItAlreadyMovedAsItsOwn(t *testing.T) {
	repo, from := bareSessionRepo(t, "alpha")
	to := filepath.Join(repo, ".herrscher-sessions", "alice", "alpha")
	if err := os.MkdirAll(filepath.Dir(to), 0o750); err != nil {
		t.Fatal(err)
	}
	gitIn(t.Context(), t, repo, "worktree", "move", from, to)
	gitIn(t.Context(), t, repo, "branch", "-m", "session/alpha", "session/alice/alpha")
	path := alphaFormat0State(t, from)
	if err := migrateHubState(t.Context(), newHostMessages(), discardAnnouncer(), loadForMigration(t, path), migrationInputs{optID: "alice", defaultGateways: []string{"chat"}}); err != nil {
		t.Fatal(err)
	}
}

func TestAnOperatorCommandOnAFreshStateWritesNoIdAndServeThenStartsWithAnOwner(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	_, err := NewRegistry(t.Context(), clitext.NewMessages(), NewMessages(), nil, Options{Runtime: testRuntime(t), StatePath: path, Warnings: io.Discard})
	if unset, ok := errors.AsType[*instanceUnsetError](err); !ok || unset.path != path {
		t.Fatalf("NewRegistry with no id anywhere = %v, want the refusal that asks to start serve", err)
	}
	if _, statErr := os.Stat(path); !os.IsNotExist(statErr) {
		t.Fatalf("a refused operator command wrote %s: %v", path, statErr)
	}
	if _, configuredErr := NewRegistry(t.Context(), clitext.NewMessages(), NewMessages(), nil, Options{Runtime: testRuntime(t), StatePath: path, InstanceID: "configured", Warnings: io.Discard}); configuredErr != nil {
		t.Fatal(configuredErr)
	}
	if stored := loadForMigration(t, path).InstanceID; stored != "" {
		t.Fatalf("an operator command wrote the instance id %q into the state", stored)
	}
	got, err := resolveInstanceID(newHostMessages(), discardAnnouncer(), loadForMigration(t, path), "", "999912345678")
	if err != nil || got != "u12345678" {
		t.Fatalf("serve with an owner after the command = %q, %v, want u12345678", got, err)
	}
}

func TestARegistryThatOwnsTheStateMigratesAFormat0State(t *testing.T) {
	path := writeFormat0State(t, []map[string]any{}, nil)
	if _, err := NewRegistry(t.Context(), clitext.NewMessages(), NewMessages(), nil, Options{Runtime: testRuntime(t), StatePath: path, InstanceID: "inst", Warnings: io.Discard, OwnsState: true}); err != nil {
		t.Fatalf("NewRegistry owning a format 0 state = %v, want it migrated", err)
	}
	migrated := loadForMigration(t, path)
	if migrated.Outdated() || migrated.InstanceID != "inst" || migrated.Format != state.CurrentFormat {
		t.Fatalf("state after the registry = format %d id %q, want format %d id inst", migrated.Format, migrated.InstanceID, state.CurrentFormat)
	}
}
