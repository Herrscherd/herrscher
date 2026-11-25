package state

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

type formatFixture struct {
	name      string
	migration FormatMigration
	changes   []FormatChange
}

func formatFixtures() []formatFixture {
	return []formatFixture{
		{
			name:      "format0-repo",
			migration: FormatMigration{InstanceID: "alice", DefaultGateways: []string{"chat"}},
			changes:   []FormatChange{{Kind: ChangeWorkspaceFromRepo, Value: "/srv/work/herrscher"}},
		},
		{
			name:      "format0-unbound",
			migration: FormatMigration{InstanceID: "alice", DefaultGateways: []string{"chat"}},
			changes:   []FormatChange{{Kind: ChangeGatewaysBound, Session: "triage", Value: "chat"}},
		},
		{
			name: "format0-bare",
			migration: FormatMigration{
				InstanceID:      "h7k2m9q4x",
				DefaultGateways: []string{"chat"},
				Relocated: map[string]Relocation{
					"alpha": {Worktree: "/srv/work/app/.herrscher-sessions/h7k2m9q4x/alpha", Dir: "/srv/work/app/.herrscher-sessions/h7k2m9q4x/alpha/web"},
					"notes": {Worktree: "/srv/work/.herrscher-scratch/h7k2m9q4x/notes", Dir: "/srv/work/.herrscher-scratch/h7k2m9q4x/notes"},
				},
			},
			changes: []FormatChange{
				{Kind: ChangeInstanceAssigned, Value: "h7k2m9q4x"},
				{Kind: ChangeSessionRelocated, Session: "alpha", Value: "/srv/work/app/.herrscher-sessions/h7k2m9q4x/alpha"},
				{Kind: ChangeSessionRelocated, Session: "notes", Value: "/srv/work/.herrscher-scratch/h7k2m9q4x/notes"},
			},
		},
	}
}

func copyFixture(t *testing.T, name string) (string, []byte) {
	t.Helper()
	original, err := os.ReadFile(filepath.Join("testdata", name+".json"))
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "state.json")
	if err := os.WriteFile(path, original, privateFileMode); err != nil {
		t.Fatal(err)
	}
	return path, original
}

func loadOutdated(t *testing.T, path string) *State {
	t.Helper()
	loaded, err := LoadState(path)
	if err != nil {
		t.Fatal(err)
	}
	if !loaded.Outdated() {
		t.Fatalf("%s loaded as current, want outdated", path)
	}
	return loaded
}

func TestMigrateFormatBringsEachOldShapeToTheExpectedFile(t *testing.T) {
	for _, fixture := range formatFixtures() {
		t.Run(fixture.name, func(t *testing.T) {
			path, _ := copyFixture(t, fixture.name)
			changes, err := loadOutdated(t, path).MigrateFormat(fixture.migration)
			if err != nil {
				t.Fatal(err)
			}
			if !slices.Equal(changes, fixture.changes) {
				t.Errorf("changes = %+v, want %+v", changes, fixture.changes)
			}
			got := mustRead(t, path)
			want := mustRead(t, filepath.Join("testdata", fixture.name+".want.json"))
			if !bytes.Equal(got, want) {
				t.Errorf("migrated file:\n%s\nwant:\n%s", got, want)
			}
		})
	}
}

func TestMigrateFormatKeepsTheOldFileNextToTheNewOne(t *testing.T) {
	path, original := copyFixture(t, "format0-repo")
	migrated := loadOutdated(t, path)
	if _, err := migrated.MigrateFormat(FormatMigration{InstanceID: "alice"}); err != nil {
		t.Fatal(err)
	}
	if backup := mustRead(t, migrated.BackupPath()); !bytes.Equal(backup, original) {
		t.Fatalf("backup:\n%s\nwant the original file:\n%s", backup, original)
	}
}

func TestMigrateFormatASecondTimeChangesNothing(t *testing.T) {
	for _, fixture := range formatFixtures() {
		t.Run(fixture.name, func(t *testing.T) {
			path, _ := copyFixture(t, fixture.name)
			if _, err := loadOutdated(t, path).MigrateFormat(fixture.migration); err != nil {
				t.Fatal(err)
			}
			first := mustRead(t, path)
			reloaded, err := LoadState(path)
			if err != nil {
				t.Fatal(err)
			}
			changes, err := reloaded.MigrateFormat(fixture.migration)
			if err != nil || changes != nil {
				t.Fatalf("second migration = %+v, %v, want nothing", changes, err)
			}
			if second := mustRead(t, path); !bytes.Equal(first, second) {
				t.Fatalf("second migration rewrote the file:\n%s\nwas:\n%s", second, first)
			}
		})
	}
}

func TestLoadStateRefusesANewerFormat(t *testing.T) {
	path, _ := copyFixture(t, "format2")
	_, err := LoadState(path)
	newer, ok := errors.AsType[*NewerFormatError](err)
	if !ok || newer.Found != 2 || newer.Supported != CurrentFormat || newer.Path != path {
		t.Fatalf("LoadState = %v, want a NewerFormatError naming format 2 and %s", err, path)
	}
}

func TestMigrateFormatRefusesAChannelSessionWithNoGatewayToBind(t *testing.T) {
	path, original := copyFixture(t, "format0-unbound")
	_, err := loadOutdated(t, path).MigrateFormat(FormatMigration{InstanceID: "alice"})
	if !errors.Is(err, ErrNoGatewayToBind) || !strings.Contains(err.Error(), "triage") {
		t.Fatalf("MigrateFormat = %v, want ErrNoGatewayToBind naming triage", err)
	}
	if got := mustRead(t, path); !bytes.Equal(got, original) {
		t.Fatalf("a refused migration rewrote the file:\n%s", got)
	}
}

func TestMigrateFormatDropsTheRepoWhenTheConfigSetsAWorkspace(t *testing.T) {
	path, _ := copyFixture(t, "format0-repo")
	migrated := loadOutdated(t, path)
	changes, err := migrated.MigrateFormat(FormatMigration{InstanceID: "alice", ConfigWorkspace: "/home/u/dev"})
	if err != nil {
		t.Fatal(err)
	}
	want := []FormatChange{{Kind: ChangeRepoDropped, Value: "/srv/work/herrscher"}}
	if !slices.Equal(changes, want) || migrated.Workspace != "" || migrated.LegacyRepo() != "" {
		t.Fatalf("changes = %+v workspace = %q repo = %q, want the repo dropped and the workspace left to the config", changes, migrated.Workspace, migrated.LegacyRepo())
	}
}

func TestAnOldFileSavedBeforeItsMigrationKeepsItsRepo(t *testing.T) {
	path, _ := copyFixture(t, "format0-repo")
	must(t, loadOutdated(t, path).SetOperatorLang("fr"))
	reloaded := loadOutdated(t, path)
	if reloaded.LegacyRepo() != "/srv/work/herrscher" {
		t.Fatalf("repo after a save = %q, want it kept for the migration", reloaded.LegacyRepo())
	}
}

func TestAMigrationThatCannotWriteLeavesTheOldFileInPlace(t *testing.T) {
	path, original := copyFixture(t, "format0-repo")
	migrated := loadOutdated(t, path)
	errDiskFull := errors.New("disk full")
	migrated.write = func(name string, data []byte, perm os.FileMode) error {
		if strings.HasSuffix(name, ".tmp") {
			return errDiskFull
		}
		return os.WriteFile(name, data, perm)
	}
	if _, err := migrated.MigrateFormat(FormatMigration{InstanceID: "alice"}); !errors.Is(err, errDiskFull) {
		t.Fatalf("MigrateFormat = %v, want the write failure", err)
	}
	if got := mustRead(t, path); !bytes.Equal(got, original) {
		t.Fatalf("a failed write changed the file:\n%s", got)
	}
}

func TestAFreshStateIsOfTheCurrentFormat(t *testing.T) {
	fresh, err := LoadState(filepath.Join(t.TempDir(), "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	if fresh.Outdated() || fresh.Format != CurrentFormat {
		t.Fatalf("fresh state format = %d, want %d", fresh.Format, CurrentFormat)
	}
}

func mustRead(t *testing.T, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return data
}
