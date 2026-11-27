package host

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Herrscherd/herrscher-contracts/i18n"
	"github.com/Herrscherd/herrscher/core/clitext"
	"github.com/Herrscherd/herrscher/core/internal/instanceid"
)

func readOnlyScratchSession(t *testing.T, repo string) string {
	t.Helper()
	if os.Geteuid() == 0 {
		t.Skip("root writes into a read-only folder")
	}
	scratchRoot := filepath.Join(repo, ".herrscher-scratch")
	scratch := filepath.Join(scratchRoot, "beta")
	if err := os.MkdirAll(scratch, 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(scratchRoot, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.Chmod(scratchRoot, 0o700); err != nil {
			t.Error(err)
		}
	})
	return scratch
}

func TestMigrationStoppedAfterAMoveKeepsItsGeneratedIDAndResumesWhereItStopped(t *testing.T) {
	repo, from := bareSessionRepo(t, "alpha")
	scratch := readOnlyScratchSession(t, repo)
	path := writeFormat0State(t, []map[string]any{
		{"name": "alpha", "channelID": "c1", "type": "channel", "cmd": "claude", "worktree": from, "dir": from, "gateways": []string{"chat"}},
		{"name": "beta", "channelID": "c2", "type": "channel", "cmd": "claude", "worktree": scratch, "scratch": true, "dir": scratch, "gateways": []string{"chat"}},
	}, nil)
	err := migrateHubState(t.Context(), newHostMessages(), discardAnnouncer(), loadForMigration(t, path), migrationInputs{defaultGateways: []string{"chat"}})
	if _, ok := errors.AsType[*stateMigrationInterruptedError](err); !ok {
		t.Fatalf("migration with a scratch folder it cannot move = %v, want it stopped partway", err)
	}
	stopped := loadForMigration(t, path)
	instID := stopped.InstanceID
	if !stopped.Outdated() || !instanceid.Validate(instID) {
		t.Fatalf("stopped migration left format outdated=%v id=%q, want the old format with the generated id written in", stopped.Outdated(), instID)
	}
	moved := filepath.Join(repo, ".herrscher-sessions", instID, "alpha")
	wants := map[i18n.Language][]string{
		i18n.English: {"stopped partway", from + " moved to " + moved, "session/alpha renamed session/" + instID + "/alpha", path + ".before-format-1.bak", "carries on from there"},
		i18n.French:  {"arrêtée en cours de route", from + " déplacé vers " + moved, "reprend à partir de là"},
	}
	for lang, parts := range wants {
		text, _ := stateFormatText(clitext.NewMessages(), lang, err)
		for _, part := range parts {
			if !strings.Contains(text, part) {
				t.Errorf("%s text of the stopped migration %q does not carry %q", lang, text, part)
			}
		}
	}
	again := migrateHubState(t.Context(), newHostMessages(), discardAnnouncer(), loadForMigration(t, path), migrationInputs{defaultGateways: []string{"chat"}})
	if text, _ := stateFormatText(clitext.NewMessages(), i18n.English, again); !strings.Contains(text, "before any change is "+path+".before-format-1.bak.") {
		t.Errorf("a second stop names another copy than the first: %q", text)
	}
	if copies, globErr := filepath.Glob(path + ".before-format-1*"); globErr != nil || len(copies) != 1 {
		t.Fatalf("copies after two stops = %v, want only the first", copies)
	}
	if chmodErr := os.Chmod(filepath.Join(repo, ".herrscher-scratch"), 0o700); chmodErr != nil {
		t.Fatal(chmodErr)
	}
	if err := migrateHubState(t.Context(), newHostMessages(), discardAnnouncer(), loadForMigration(t, path), migrationInputs{defaultGateways: []string{"chat"}}); err != nil {
		t.Fatalf("second run of the stopped migration: %v", err)
	}
	reloaded := loadForMigration(t, path)
	alpha, _ := reloaded.FindSession("alpha")
	beta, _ := reloaded.FindSession("beta")
	if reloaded.Outdated() || reloaded.InstanceID != instID || alpha.Worktree != moved || beta.Worktree != filepath.Join(repo, ".herrscher-scratch", instID, "beta") {
		t.Fatalf("resumed migration: outdated=%v id=%q (want %q) alpha=%q (want %q) beta=%q", reloaded.Outdated(), reloaded.InstanceID, instID, alpha.Worktree, moved, beta.Worktree)
	}
	if branch := gitOutput(t, moved, "symbolic-ref", "--short", "HEAD"); branch != "session/"+instID+"/alpha" {
		t.Fatalf("moved worktree branch = %q", branch)
	}
}

func TestMigrationRefusesTwoSessionsThatRecordTheSameFolderBeforeMovingEither(t *testing.T) {
	repo, from := bareSessionRepo(t, "alpha")
	path := writeFormat0State(t, []map[string]any{
		{"name": "beta", "channelID": "c1", "type": "channel", "cmd": "claude", "worktree": from, "dir": from, "gateways": []string{"chat"}},
		{"name": "alpha", "channelID": "c2", "type": "channel", "cmd": "claude", "worktree": from + string(filepath.Separator), "dir": from, "gateways": []string{"chat"}},
	}, nil)
	err := migrateHubState(t.Context(), newHostMessages(), discardAnnouncer(), loadForMigration(t, path), migrationInputs{optID: "inst", defaultGateways: []string{"chat"}})
	refusal, ok := errors.AsType[*stateMigrationRefusedError](err)
	if !ok || refusal.key != clitext.MigrationSharedWorktree {
		t.Fatalf("migration of two sessions on one folder = %v, want a %s refusal", err, clitext.MigrationSharedWorktree)
	}
	for _, lang := range []i18n.Language{i18n.English, i18n.French} {
		text, _ := stateFormatText(clitext.NewMessages(), lang, err)
		for _, part := range []string{"beta", "alpha", from, path, "herrscher serve"} {
			if !strings.Contains(text, part) {
				t.Errorf("%s refusal %q does not carry %q", lang, text, part)
			}
		}
	}
	assertRefusedUntouched(t, path, err, from)
	if _, statErr := os.Stat(from); statErr != nil {
		t.Fatalf("the shared folder moved: %v", statErr)
	}
	if branch := gitOutput(t, repo, "branch", "--list", "session/inst/*"); branch != "" {
		t.Fatalf("a branch was renamed: %q", branch)
	}
}

func TestMigrationRefusesAStoredInstanceIDThatWouldLeaveTheSessionsFolder(t *testing.T) {
	repo, from := bareSessionRepo(t, "alpha")
	path := writeFormat0State(t, []map[string]any{{"name": "alpha", "channelID": "c1", "type": "channel", "cmd": "claude", "worktree": from, "dir": from, "gateways": []string{"chat"}}}, map[string]any{"instanceID": "../x"})
	err := migrateHubState(t.Context(), newHostMessages(), discardAnnouncer(), loadForMigration(t, path), migrationInputs{defaultGateways: []string{"chat"}})
	refusal, ok := errors.AsType[*stateMigrationRefusedError](err)
	if !ok || refusal.key != clitext.InstanceStoredInvalid {
		t.Fatalf("migration with stored id ../x = %v, want a %s refusal", err, clitext.InstanceStoredInvalid)
	}
	for _, lang := range []i18n.Language{i18n.English, i18n.French} {
		text, _ := stateFormatText(clitext.NewMessages(), lang, err)
		if !strings.Contains(text, "../x") || !strings.Contains(text, path) {
			t.Errorf("%s refusal %q does not name the id and the state", lang, text)
		}
	}
	assertRefusedUntouched(t, path, err, "../x")
	if _, statErr := os.Stat(filepath.Join(repo, "x")); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("a folder was made outside .herrscher-sessions: %v", statErr)
	}
}
