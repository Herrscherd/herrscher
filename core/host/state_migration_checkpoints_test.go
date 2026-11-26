package host

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Herrscherd/herrscher-contracts/i18n"
	"github.com/Herrscherd/herrscher/core/clitext"
	"github.com/Herrscherd/herrscher/core/internal/checkpoint"
	"github.com/Herrscherd/herrscher/core/internal/worktree"
)

const migratedIncarnation = "inc_ALPHA"

func gitOutput(t *testing.T, dir string, args ...string) string {
	t.Helper()
	out, err := exec.CommandContext(t.Context(), "git", append([]string{"-C", dir}, args...)...).CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

func refsMatching(t *testing.T, dir, pattern string) []string {
	t.Helper()
	listed := gitOutput(t, dir, "for-each-ref", "--format=%(refname)", pattern)
	return strings.Fields(listed)
}

func checkpointedBareSession(t *testing.T) (string, string, string) {
	t.Helper()
	repo, from := bareSessionRepo(t, "alpha")
	head := gitOutput(t, repo, "rev-parse", "HEAD")
	for _, ref := range []string{"refs/herrscher/checkpoints/alpha/1", "refs/herrscher/checkpoints/alpha/2", "refs/herrscher/undo/alpha"} {
		gitOutput(t, repo, "update-ref", ref, head)
	}
	path := writeFormat0State(t, []map[string]any{{"name": "alpha", "incarnation": migratedIncarnation, "channelID": "c1", "type": "channel", "cmd": "claude", "worktree": from, "dir": from, "gateways": []string{"chat"}}}, nil)
	return repo, from, path
}

func TestMigrationFilesTheCheckpointsOfASessionUnderItsIncarnation(t *testing.T) {
	repo, _, path := checkpointedBareSession(t)
	original, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	announcer, out, _ := recordingAnnouncer()
	if err = migrateHubState(t.Context(), newHostMessages(), announcer, loadForMigration(t, path), migrationInputs{optID: "inst", defaultGateways: []string{"chat"}}); err != nil {
		t.Fatal(err)
	}
	if left := refsMatching(t, repo, "refs/herrscher/checkpoints/alpha/"); len(left) != 0 {
		t.Fatalf("refs left under the session name: %v", left)
	}
	if left := refsMatching(t, repo, "refs/herrscher/undo/alpha"); len(left) != 0 {
		t.Fatalf("undo point left under the session name: %v", left)
	}
	for _, want := range []string{
		"session alpha, 2 checkpoint refs moved from refs/herrscher/checkpoints/alpha/ to refs/herrscher/checkpoints/" + migratedIncarnation + "/",
		"session alpha, undo point moved from refs/herrscher/undo/alpha to refs/herrscher/undo/" + migratedIncarnation,
	} {
		if !strings.Contains(out.String(), want) {
			t.Fatalf("serve output does not announce %q:\n%s", want, out)
		}
	}
	sess, _ := loadForMigration(t, path).FindSession("alpha")
	var store checkpoint.Store
	entries, err := store.List(t.Context(), sess.Worktree, sess.Incarnation)
	if err != nil || len(entries) != 2 || !store.HasUndo(t.Context(), sess.Worktree, sess.Incarnation) {
		t.Fatalf("after the migration alpha lists %+v (err %v), want its two checkpoints and its undo point", entries, err)
	}

	if err := os.WriteFile(path, original, 0o600); err != nil {
		t.Fatal(err)
	}
	announcer, out, _ = recordingAnnouncer()
	if err := migrateHubState(t.Context(), newHostMessages(), announcer, loadForMigration(t, path), migrationInputs{optID: "inst", defaultGateways: []string{"chat"}}); err != nil {
		t.Fatalf("a migration started again after its refs moved: %v", err)
	}
	if strings.Contains(out.String(), "checkpoint ref") || strings.Contains(out.String(), "undo point") {
		t.Fatalf("the second run announces a move it did not make:\n%s", out)
	}
	if kept := refsMatching(t, repo, "refs/herrscher/checkpoints/"+migratedIncarnation+"/"); len(kept) != 2 {
		t.Fatalf("after the second run the incarnation holds %v", kept)
	}
}

func TestMigrationAnnouncesARefAlreadyAtItsNewNameAsFinishedNotMoved(t *testing.T) {
	repo, _, path := checkpointedBareSession(t)
	gitOutput(t, repo, "update-ref", "refs/herrscher/checkpoints/"+migratedIncarnation+"/1", gitOutput(t, repo, "rev-parse", "HEAD"))
	announcer, out, _ := recordingAnnouncer()
	if err := migrateHubState(t.Context(), newHostMessages(), announcer, loadForMigration(t, path), migrationInputs{optID: "inst", defaultGateways: []string{"chat"}}); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"session alpha, 1 checkpoint ref moved from refs/herrscher/checkpoints/alpha/",
		"session alpha, undo point moved",
		"session alpha, 1 checkpoint ref was already at its new name, its old name is deleted",
	} {
		if !strings.Contains(out.String(), want) {
			t.Fatalf("serve output does not announce %q:\n%s", want, out)
		}
	}
	if left := refsMatching(t, repo, "refs/herrscher/checkpoints/alpha/"); len(left) != 0 {
		t.Fatalf("refs left under the session name: %v", left)
	}
}

func TestMigrationRefusesACheckpointRefThatAlreadyHoldsAnotherCommit(t *testing.T) {
	repo, from, path := checkpointedBareSession(t)
	other := gitOutput(t, repo, "commit-tree", "HEAD^{tree}", "-m", "autre")
	gitOutput(t, repo, "update-ref", "refs/herrscher/checkpoints/"+migratedIncarnation+"/1", other)

	err := migrateHubState(t.Context(), newHostMessages(), discardAnnouncer(), loadForMigration(t, path), migrationInputs{optID: "inst", defaultGateways: []string{"chat"}})
	refusal, ok := errors.AsType[*stateMigrationRefusedError](err)
	if !ok || refusal.key != clitext.MigrationCheckpointConflict || !errors.Is(err, checkpoint.ErrRekeyConflict) {
		t.Fatalf("err = %v, want a %s refusal", err, clitext.MigrationCheckpointConflict)
	}
	text, _ := stateFormatText(clitext.NewMessages(), i18n.English, err)
	for _, want := range []string{`"alpha"`, "refs/herrscher/checkpoints/" + migratedIncarnation + "/1", other, "update-ref -d"} {
		if !strings.Contains(text, want) {
			t.Errorf("refusal %q does not carry %q", text, want)
		}
	}
	if left := refsMatching(t, repo, "refs/herrscher/checkpoints/alpha/"); len(left) != 2 {
		t.Fatalf("the refused migration moved checkpoint refs: %v left", left)
	}
	if _, statErr := os.Stat(from); statErr != nil {
		t.Fatalf("the refused migration moved the worktree: %v", statErr)
	}
	backups, globErr := filepath.Glob(path + ".before-format-1*")
	if globErr != nil {
		t.Fatal(globErr)
	}
	if len(backups) != 0 {
		t.Fatalf("the refused migration wrote a copy %v before refusing", backups)
	}
}

func checkpointedGoneWorktree(t *testing.T, gone string) string {
	t.Helper()
	repo, _ := worktree.LayoutRepository(gone)
	gitOutput(t, repo, "worktree", "add", "-q", gone, "-b", "session/alpha")
	head := gitOutput(t, gone, "rev-parse", "HEAD")
	gitOutput(t, gone, "update-ref", "refs/herrscher/checkpoints/alpha/1", head)
	if err := os.RemoveAll(gone); err != nil {
		t.Fatal(err)
	}
	return writeFormat0State(t, []map[string]any{{"name": "alpha", "incarnation": migratedIncarnation, "channelID": "c1", "type": "channel", "cmd": "claude", "worktree": gone, "dir": gone, "gateways": []string{"chat"}}}, nil)
}

func TestMigrationFilesTheCheckpointsOfAGoneWorktreeFromItsRepository(t *testing.T) {
	for _, layout := range []struct {
		name string
		path func(repo string) string
	}{
		{"id-less layout", func(repo string) string { return filepath.Join(repo, ".herrscher-sessions", "alpha") }},
		{"instance layout", func(repo string) string { return filepath.Join(repo, ".herrscher-sessions", "inst", "alpha") }},
	} {
		t.Run(layout.name, func(t *testing.T) {
			repo := tempRepo(t)
			path := checkpointedGoneWorktree(t, layout.path(repo))
			announcer, out, _ := recordingAnnouncer()
			if err := migrateHubState(t.Context(), newHostMessages(), announcer, loadForMigration(t, path), migrationInputs{optID: "inst", defaultGateways: []string{"chat"}}); err != nil {
				t.Fatal(err)
			}
			if left := refsMatching(t, repo, "refs/herrscher/checkpoints/alpha/"); len(left) != 0 {
				t.Fatalf("refs of the gone worktree left under the session name: %v", left)
			}
			if moved := refsMatching(t, repo, "refs/herrscher/checkpoints/"+migratedIncarnation+"/"); len(moved) != 1 {
				t.Fatalf("refs under the incarnation = %v, want the one checkpoint of the gone worktree", moved)
			}
			if !strings.Contains(out.String(), "session alpha, 1 checkpoint ref moved") {
				t.Fatalf("serve output does not announce the moved checkpoint:\n%s", out)
			}
		})
	}
}
