package host

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/Herrscherd/herrscher-contracts/i18n"
	"github.com/Herrscherd/herrscher/core/clitext"
)

func notesUnderACeiledDotfilesRepository(t *testing.T) (string, string) {
	t.Helper()
	home := tempRepo(t)
	notes := filepath.Join(home, "notes")
	if err := os.MkdirAll(notes, 0o750); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GIT_CEILING_DIRECTORIES", home)
	t.Setenv("LANGUAGE", "fr")
	return home, notes
}

func TestMigrationRefusesAFolderUnderARepositoryGitIsToldNotToSeeAndNamesTheWayOut(t *testing.T) {
	home, notes := notesUnderACeiledDotfilesRepository(t)
	path := localSessionAt(t, notes)
	err := migrateHubState(t.Context(), newHostMessages(), discardAnnouncer(), loadForMigration(t, path), migrationInputs{optID: "inst", defaultGateways: []string{"chat"}})
	refusal, ok := errors.AsType[*stateMigrationRefusedError](err)
	if !ok || refusal.key != clitext.MigrationCheckpointHidden {
		t.Fatalf("err = %v, want a %s refusal", err, clitext.MigrationCheckpointHidden)
	}
	entry := filepath.Join(home, ".git")
	wants := map[i18n.Language][]string{
		i18n.English: {entry, notes, "GIT_CEILING_DIRECTORIES", "GIT_DISCOVERY_ACROSS_FILESYSTEM=1", "repair or remove it", "move the folder out of its path", "herrscher serve"},
		i18n.French:  {entry, notes, "GIT_CEILING_DIRECTORIES", "GIT_DISCOVERY_ACROSS_FILESYSTEM=1", "répare-le ou retire-le", "sors le dossier de son chemin", "herrscher serve"},
	}
	for lang, want := range wants {
		text, _ := stateFormatText(clitext.NewMessages(), lang, err)
		for _, part := range want {
			if !strings.Contains(text, part) {
				t.Errorf("%s refusal %q does not carry %q", lang, text, part)
			}
		}
	}
	if reloaded := loadForMigration(t, path); !reloaded.Outdated() {
		t.Fatal("the refused migration wrote the state")
	}
}

func TestMigrationUnderTheDotfilesRepositoryMovesOnlyTheSessionRefsOnceTheCeilingIsLifted(t *testing.T) {
	home, notes := notesUnderACeiledDotfilesRepository(t)
	head := gitOutput(t, home, "rev-parse", "HEAD")
	for _, ref := range []string{"refs/herrscher/checkpoints/alpha/1", "refs/herrscher/checkpoints/alphabet/1", "refs/heads/alpha", "refs/tags/alpha"} {
		gitOutput(t, home, "update-ref", ref, head)
	}
	path := localSessionAt(t, notes)
	t.Setenv("GIT_CEILING_DIRECTORIES", "")
	if err := migrateHubState(t.Context(), newHostMessages(), discardAnnouncer(), loadForMigration(t, path), migrationInputs{optID: "inst", defaultGateways: []string{"chat"}}); err != nil {
		t.Fatalf("the migration started without the ceiling still refuses: %v", err)
	}
	got := refsMatching(t, home, "refs/")
	want := []string{"refs/heads/alpha", "refs/heads/main", "refs/herrscher/checkpoints/alphabet/1", "refs/herrscher/checkpoints/" + migratedIncarnation + "/1", "refs/tags/alpha"}
	slices.Sort(got)
	if !slices.Equal(got, want) {
		t.Fatalf("refs of the dotfiles repository after the migration = %v, want %v", got, want)
	}
}
