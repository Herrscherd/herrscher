package host

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Herrscherd/herrscher-contracts/i18n"
	"github.com/Herrscherd/herrscher/core/clitext"
)

func folderOutsideAnyRepository(t *testing.T, rel string) string {
	t.Helper()
	root := t.TempDir()
	t.Setenv("GIT_CEILING_DIRECTORIES", root)
	t.Setenv("LANGUAGE", "fr")
	dir := filepath.Join(root, rel)
	if err := os.MkdirAll(dir, 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "notes.txt"), []byte("travail\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	return dir
}

func localSessionAt(t *testing.T, worktree string) string {
	t.Helper()
	return writeFormat0State(t, []map[string]any{{"name": "alpha", "incarnation": migratedIncarnation, "channelID": "c1", "type": "channel", "cmd": "claude", "worktree": worktree, "dir": worktree, "gateways": []string{"chat"}}}, nil)
}

func TestMigrationSkipsTheCheckpointsOfASessionInNoRepository(t *testing.T) {
	plain := folderOutsideAnyRepository(t, "plain")
	path := localSessionAt(t, plain)
	announcer, out, _ := recordingAnnouncer()
	if err := migrateHubState(t.Context(), newHostMessages(), announcer, loadForMigration(t, path), migrationInputs{optID: "inst", defaultGateways: []string{"chat"}}); err != nil {
		t.Fatalf("a session in a folder of no git repository stops the migration: %v", err)
	}
	if strings.Contains(out.String(), "checkpoint") {
		t.Fatalf("the migration announces checkpoints for a folder that holds none:\n%s", out)
	}
	if reloaded := loadForMigration(t, path); reloaded.Outdated() {
		t.Fatal("the state was not migrated")
	}
}

func TestMigrationLeavesABareWorktreeInNoRepositoryWhereItIs(t *testing.T) {
	from := folderOutsideAnyRepository(t, filepath.Join("gone", ".herrscher-sessions", "alpha"))
	path := localSessionAt(t, from)
	announcer, out, _ := recordingAnnouncer()
	if err := migrateHubState(t.Context(), newHostMessages(), announcer, loadForMigration(t, path), migrationInputs{optID: "inst", defaultGateways: []string{"chat"}}); err != nil {
		t.Fatalf("a bare worktree in no git repository stops the migration: %v", err)
	}
	if _, err := os.Stat(filepath.Join(from, "notes.txt")); err != nil {
		t.Fatalf("the folder left its place: %v", err)
	}
	if strings.Contains(out.String(), "moved") || strings.Contains(out.String(), "renamed") {
		t.Fatalf("the migration announces a move it did not make:\n%s", out)
	}
	if sess, _ := loadForMigration(t, path).FindSession("alpha"); sess.Worktree != from {
		t.Fatalf("worktree = %q, want it left at %q", sess.Worktree, from)
	}
}

func TestMigrationRefusesCheckpointRefsGitCannotReadAndSaysHowToRepairThem(t *testing.T) {
	broken := folderOutsideAnyRepository(t, "broken")
	if err := os.WriteFile(filepath.Join(broken, ".git"), []byte("gitdir: "+filepath.Join(broken, "nowhere")+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	path := localSessionAt(t, broken)
	err := migrateHubState(t.Context(), newHostMessages(), discardAnnouncer(), loadForMigration(t, path), migrationInputs{optID: "inst", defaultGateways: []string{"chat"}})
	refusal, ok := errors.AsType[*stateMigrationRefusedError](err)
	if !ok || refusal.key != clitext.MigrationCheckpointInspect {
		t.Fatalf("err = %v, want a %s refusal", err, clitext.MigrationCheckpointInspect)
	}
	for _, lang := range []i18n.Language{i18n.English, i18n.French} {
		text, _ := stateFormatText(clitext.NewMessages(), lang, err)
		for _, want := range []string{"git -C " + broken + " for-each-ref refs/herrscher/", "git worktree repair " + broken, "herrscher serve"} {
			if !strings.Contains(text, want) {
				t.Errorf("refusal %q does not carry %q", text, want)
			}
		}
	}
}
