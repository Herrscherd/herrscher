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
			changes: nil,
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

func loadPinned(t *testing.T, path, instanceID string) *State {
	t.Helper()
	loaded := loadOutdated(t, path)
	loaded.InstanceID = instanceID
	return loaded
}

func TestMigrateFormatRefusesAStateWhoseInstanceIDIsNotPinned(t *testing.T) {
	path, original := copyFixture(t, "format0-bare")
	if _, err := loadOutdated(t, path).MigrateFormat(FormatMigration{InstanceID: "h7k2m9q4x", DefaultGateways: []string{"chat"}}); !errors.Is(err, errNoInstanceID) {
		t.Fatalf("MigrateFormat = %v, want %v", err, errNoInstanceID)
	}
	if got := mustRead(t, path); !bytes.Equal(got, original) {
		t.Fatalf("a refused migration rewrote the file:\n%s", got)
	}
}

func TestAMigrationStartedAgainNamesTheFirstCopyAndWritesNoOther(t *testing.T) {
	path, original := copyFixture(t, "format0-bare")
	first := loadOutdated(t, path)
	backup, err := first.Backup()
	if err != nil {
		t.Fatal(err)
	}
	must(t, first.PinMigration("h7k2m9q4x"))

	again := loadOutdated(t, path)
	if again.InstanceID != "h7k2m9q4x" {
		t.Fatalf("pinned state reloads with instance id %q", again.InstanceID)
	}
	resumed, err := again.Backup()
	if err != nil || resumed != backup {
		t.Fatalf("Backup after a stop = %q, %v, want the first copy %q", resumed, err, backup)
	}
	if copies, globErr := filepath.Glob(path + ".before-format-*"); globErr != nil || len(copies) != 1 {
		t.Fatalf("copies after a stop = %v, want only %s", copies, backup)
	}
	if got := mustRead(t, backup); !bytes.Equal(got, original) {
		t.Fatalf("the copy named after a stop holds:\n%s\nwant the file before any change:\n%s", got, original)
	}
	if _, err := again.MigrateFormat(FormatMigration{InstanceID: "h7k2m9q4x", DefaultGateways: []string{"chat"}}); err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(mustRead(t, path), []byte("migration_backup")) {
		t.Fatalf("the finished migration keeps naming its copy:\n%s", mustRead(t, path))
	}
}

func TestAMigrationStartedAgainRefusesToCopyThePinnedFileWhenItsCopyIsGone(t *testing.T) {
	path, _ := copyFixture(t, "format0-bare")
	first := loadOutdated(t, path)
	backup, err := first.Backup()
	if err != nil {
		t.Fatal(err)
	}
	must(t, first.PinMigration("h7k2m9q4x"))
	must(t, os.Remove(backup))

	_, err = loadOutdated(t, path).Backup()
	missing, ok := errors.AsType[*BackupMissingError](err)
	if !ok || missing.File != backup || missing.Path != path {
		t.Fatalf("Backup once the copy is gone = %v, want a BackupMissingError naming %s", err, backup)
	}
	if copies, globErr := filepath.Glob(path + ".before-format-*"); globErr != nil || len(copies) != 0 {
		t.Fatalf("copies once the first is gone = %v, want none written from the pinned file", copies)
	}
}

func TestMigrateFormatBringsEachOldShapeToTheExpectedFile(t *testing.T) {
	for _, fixture := range formatFixtures() {
		t.Run(fixture.name, func(t *testing.T) {
			path, _ := copyFixture(t, fixture.name)
			changes, err := loadPinned(t, path, fixture.migration.InstanceID).MigrateFormat(fixture.migration)
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
	if _, err := loadPinned(t, path, "alice").MigrateFormat(FormatMigration{InstanceID: "alice"}); err != nil {
		t.Fatal(err)
	}
	if backup := mustRead(t, path+".before-format-1.bak"); !bytes.Equal(backup, original) {
		t.Fatalf("backup:\n%s\nwant the original file:\n%s", backup, original)
	}
}

func TestBackupCopiesTheBytesReadAtLoadEvenAfterTheFileIsRewritten(t *testing.T) {
	original := []byte(`{"home":{"id":"h","type":"category"},"sessions":[{"name":"alpha","channelID":"c1","type":"channel","cmd":"claude","gateways":["chat"]}]}`)
	path := filepath.Join(t.TempDir(), "state.json")
	must(t, os.WriteFile(path, original, privateFileMode))
	loaded := loadOutdated(t, path)
	must(t, loaded.SetInstanceID("alice"))
	if bytes.Equal(mustRead(t, path), original) {
		t.Fatal("the file was expected to be rewritten with the instance id")
	}
	backup, err := loaded.Backup()
	if err != nil {
		t.Fatal(err)
	}
	if got := mustRead(t, backup); !bytes.Equal(got, original) {
		t.Fatalf("backup:\n%s\nwant the file as it was before the load:\n%s", got, original)
	}
	if info, err := os.Stat(backup); err != nil || info.Mode().Perm() != privateFileMode {
		t.Fatalf("backup mode = %v, %v, want %v", info.Mode().Perm(), err, os.FileMode(privateFileMode))
	}
}

func TestBackupNeverOverwritesAnEarlierCopy(t *testing.T) {
	path, original := copyFixture(t, "format0-repo")
	earlier := []byte("an earlier migration")
	must(t, os.WriteFile(path+".before-format-1.bak", earlier, privateFileMode))
	backup, err := loadOutdated(t, path).Backup()
	if err != nil {
		t.Fatal(err)
	}
	if backup != path+".before-format-1.2.bak" || !bytes.Equal(mustRead(t, backup), original) {
		t.Fatalf("backup %s holds %q, want %s.before-format-1.2.bak with the file just read", backup, mustRead(t, backup), path)
	}
	if !bytes.Equal(mustRead(t, path+".before-format-1.bak"), earlier) {
		t.Fatal("the earlier copy was overwritten")
	}
}

func TestABackupThatCannotBeWrittenLeavesNoCopyAndNoMigration(t *testing.T) {
	path, original := copyFixture(t, "format0-repo")
	migrated := loadPinned(t, path, "alice")
	errNoLinks := errors.New("operation not permitted")
	migrated.write = func(name string, data []byte, perm os.FileMode) error {
		if strings.HasSuffix(name, ".bak.tmp") {
			return errNoLinks
		}
		return os.WriteFile(name, data, perm)
	}
	if _, err := migrated.MigrateFormat(FormatMigration{InstanceID: "alice"}); !errors.Is(err, errNoLinks) {
		t.Fatalf("MigrateFormat = %v, want the backup failure", err)
	}
	if got := mustRead(t, path); !bytes.Equal(got, original) {
		t.Fatalf("a failed backup let the migration rewrite the file:\n%s", got)
	}
	matches, err := filepath.Glob(path + ".before-format-*")
	if err != nil || len(matches) != 0 {
		t.Fatalf("a failed backup left %v behind", matches)
	}
}

func TestMigrateFormatASecondTimeChangesNothing(t *testing.T) {
	for _, fixture := range formatFixtures() {
		t.Run(fixture.name, func(t *testing.T) {
			path, _ := copyFixture(t, fixture.name)
			if _, err := loadPinned(t, path, fixture.migration.InstanceID).MigrateFormat(fixture.migration); err != nil {
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
	_, err := loadPinned(t, path, "alice").MigrateFormat(FormatMigration{InstanceID: "alice"})
	if unbound, ok := errors.AsType[*NoGatewayToBindError](err); !ok || unbound.Session != "triage" {
		t.Fatalf("MigrateFormat = %v, want a NoGatewayToBindError naming triage", err)
	}
	if got := mustRead(t, path); !bytes.Equal(got, original) {
		t.Fatalf("a refused migration rewrote the file:\n%s", got)
	}
}

func TestMigrateFormatDropsTheRepoWhenTheConfigSetsAWorkspace(t *testing.T) {
	path, _ := copyFixture(t, "format0-repo")
	migrated := loadPinned(t, path, "alice")
	changes, err := migrated.MigrateFormat(FormatMigration{InstanceID: "alice", ConfigWorkspace: "/home/u/dev"})
	if err != nil {
		t.Fatal(err)
	}
	want := []FormatChange{{Kind: ChangeRepoDropped, Value: "/srv/work/herrscher"}}
	if !slices.Equal(changes, want) || migrated.Workspace != "" || bytes.Contains(mustRead(t, path), []byte(`"repo"`)) {
		t.Fatalf("changes = %+v workspace = %q file:\n%s\nwant the repo dropped and the workspace left to the config", changes, migrated.Workspace, mustRead(t, path))
	}
}

func TestAnOldFileSavedBeforeItsMigrationKeepsItsRepo(t *testing.T) {
	path, _ := copyFixture(t, "format0-repo")
	must(t, loadOutdated(t, path).SetOperatorLang("fr"))
	changes, err := loadPinned(t, path, "alice").MigrateFormat(FormatMigration{InstanceID: "alice"})
	want := []FormatChange{{Kind: ChangeWorkspaceFromRepo, Value: "/srv/work/herrscher"}}
	if err != nil || !slices.Equal(changes, want) {
		t.Fatalf("migration after a save = %+v, %v, want the repo still there to become the workspace", changes, err)
	}
}

func TestAMigrationThatCannotWriteLeavesTheOldFileInPlace(t *testing.T) {
	path, original := copyFixture(t, "format0-repo")
	migrated := loadPinned(t, path, "alice")
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
