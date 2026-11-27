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

func TestMigrationRefusesTwoSessionsThatShareAFolderOfTheInstanceLayout(t *testing.T) {
	repo := tempRepo(t)
	shared := filepath.Join(repo, ".herrscher-sessions", "inst", "alpha")
	gitOutput(t, repo, "worktree", "add", "-q", shared, "-b", "session/inst/alpha")
	path := writeFormat0State(t, []map[string]any{
		{"name": "beta", "channelID": "c1", "type": "channel", "cmd": "claude", "worktree": shared, "dir": shared, "gateways": []string{"chat"}},
		{"name": "alpha", "channelID": "c2", "type": "channel", "cmd": "claude", "worktree": shared, "dir": shared, "gateways": []string{"chat"}},
	}, map[string]any{"instanceID": "inst"})
	err := migrateHubState(t.Context(), newHostMessages(), discardAnnouncer(), loadForMigration(t, path), migrationInputs{defaultGateways: []string{"chat"}})
	refusal, ok := errors.AsType[*stateMigrationRefusedError](err)
	if !ok || refusal.key != clitext.MigrationSharedWorktree {
		t.Fatalf("migration of two sessions on one folder of the instance layout = %v, want a %s refusal", err, clitext.MigrationSharedWorktree)
	}
	assertRefusedUntouched(t, path, err, shared)
}

func TestMigrationRefusesTwoSessionsWhoseFoldersAreTheSameFileOnDisk(t *testing.T) {
	repo, from := bareSessionRepo(t, "alpha")
	link := filepath.Join(t.TempDir(), "alpha-link")
	if err := os.Symlink(from, link); err != nil {
		t.Fatal(err)
	}
	path := writeFormat0State(t, []map[string]any{
		{"name": "alpha", "channelID": "c1", "type": "channel", "cmd": "claude", "worktree": from, "dir": from, "gateways": []string{"chat"}},
		{"name": "beta", "channelID": "c2", "type": "channel", "cmd": "claude", "worktree": link, "dir": link, "gateways": []string{"chat"}},
	}, nil)
	err := migrateHubState(t.Context(), newHostMessages(), discardAnnouncer(), loadForMigration(t, path), migrationInputs{optID: "inst", defaultGateways: []string{"chat"}})
	if refusal, ok := errors.AsType[*stateMigrationRefusedError](err); !ok || refusal.key != clitext.MigrationSharedWorktree {
		t.Fatalf("migration of two sessions on one folder reached by a link = %v, want a %s refusal", err, clitext.MigrationSharedWorktree)
	}
	for _, lang := range []i18n.Language{i18n.English, i18n.French} {
		text, _ := stateFormatText(clitext.NewMessages(), lang, err)
		for _, recorded := range []string{from, link} {
			if !strings.Contains(text, recorded) {
				t.Errorf("%s refusal %q does not name the recorded folder %q", lang, text, recorded)
			}
		}
	}
	if branch := gitOutput(t, repo, "branch", "--list", "session/inst/*"); branch != "" {
		t.Fatalf("a branch was renamed: %q", branch)
	}
}

func TestAStoppedMigrationListsNoStatePointerItHasNotWritten(t *testing.T) {
	repo := tempRepo(t)
	scratch := readOnlyScratchSession(t, repo)
	remote := "/srv/app/.herrscher-sessions/alpha"
	path := writeFormat0State(t, []map[string]any{
		{"name": "alpha", "channelID": "c1", "type": "channel", "cmd": "claude", "host": "build1", "worktree": remote, "dir": remote, "resumeToken": "conv-1", "gateways": []string{"chat"}},
		{"name": "beta", "channelID": "c2", "type": "channel", "cmd": "claude", "worktree": scratch, "scratch": true, "dir": scratch, "gateways": []string{"chat"}},
	}, nil)
	announcer, out, _ := recordingAnnouncer()
	err := migrateHubState(t.Context(), newHostMessages(), announcer, loadForMigration(t, path), migrationInputs{optID: "inst", defaultGateways: []string{"chat"}})
	if _, ok := errors.AsType[*stateMigrationInterruptedError](err); !ok {
		t.Fatalf("migration with a scratch folder it cannot move = %v, want it stopped partway", err)
	}
	text, _ := stateFormatText(clitext.NewMessages(), i18n.English, err)
	for _, unwritten := range []string{"the state now points to", "starts a new conversation"} {
		if strings.Contains(text, unwritten) || strings.Contains(out.String(), unwritten) {
			t.Errorf("the stopped migration reports %q although the state still points to the old folder:\n%s\n%s", unwritten, text, out)
		}
	}
	if !strings.Contains(text, "no folder moved") {
		t.Errorf("the stopped migration does not say nothing moved: %q", text)
	}
}
