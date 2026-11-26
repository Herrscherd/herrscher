package host

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Herrscherd/herrscher-contracts/i18n"
	"github.com/Herrscherd/herrscher/core/clitext"
	"github.com/Herrscherd/herrscher/core/internal/instanceid"
	"github.com/Herrscherd/herrscher/core/internal/obs"
	"github.com/Herrscherd/herrscher/core/internal/state"
)

func discardAnnouncer() stateAnnouncer {
	return stateAnnouncer{cliMessages: clitext.NewMessages(), lang: i18n.English, log: obs.NewLogger(io.Discard, slog.LevelInfo), out: io.Discard}
}

func recordingAnnouncer() (stateAnnouncer, *bytes.Buffer, *bytes.Buffer) {
	var out, journal bytes.Buffer
	announcer := discardAnnouncer()
	announcer.out = &out
	announcer.log = obs.NewLogger(&journal, slog.LevelInfo)
	return announcer, &out, &journal
}

func writeFormat0State(t *testing.T, sessions []map[string]any, extra map[string]any) string {
	t.Helper()
	doc := map[string]any{"home": map[string]string{"id": "h", "type": "category"}, "sessions": sessions}
	for key, value := range extra {
		doc[key] = value
	}
	data, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "state.json")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func loadForMigration(t *testing.T, path string) *state.State {
	t.Helper()
	store, err := state.LoadState(path)
	if err != nil {
		t.Fatal(err)
	}
	return store
}

func bareSessionRepo(t *testing.T, name string) (string, string) {
	t.Helper()
	repo := tempRepo(t)
	path := filepath.Join(repo, ".herrscher-sessions", name)
	if out, err := exec.CommandContext(t.Context(), "git", "-C", repo, "worktree", "add", "-q", path, "-b", "session/"+name).CombinedOutput(); err != nil {
		t.Fatalf("worktree add: %v\n%s", err, out)
	}
	return repo, path
}

func TestMigrationMovesABareSessionUnderTheConfiguredInstance(t *testing.T) {
	repo, from := bareSessionRepo(t, "alpha")
	path := writeFormat0State(t, []map[string]any{{"name": "alpha", "channelID": "c1", "type": "channel", "cmd": "claude", "worktree": from, "dir": filepath.Join(from, "web"), "gateways": []string{"chat"}}}, nil)
	store := loadForMigration(t, path)
	announcer, out, journal := recordingAnnouncer()
	if err := migrateHubState(t.Context(), newHostMessages(), announcer, store, migrationInputs{optID: "alice", defaultGateways: []string{"chat"}}); err != nil {
		t.Fatal(err)
	}
	to := filepath.Join(repo, ".herrscher-sessions", "alice", "alpha")
	reloaded := loadForMigration(t, path)
	sess, _ := reloaded.FindSession("alpha")
	if reloaded.Outdated() || reloaded.InstanceID != "alice" || sess.Worktree != to || sess.Dir != filepath.Join(to, "web") {
		t.Fatalf("migrated state: format outdated=%v id=%q worktree=%q dir=%q, want alice and %s", reloaded.Outdated(), reloaded.InstanceID, sess.Worktree, sess.Dir, to)
	}
	branch, err := exec.CommandContext(t.Context(), "git", "-C", to, "symbolic-ref", "--short", "HEAD").Output()
	if err != nil || strings.TrimSpace(string(branch)) != "session/alice/alpha" {
		t.Fatalf("moved worktree branch = %q, %v", branch, err)
	}
	for _, want := range []string{to, "session/alice/alpha", "alice"} {
		if !strings.Contains(out.String(), want) || !strings.Contains(journal.String(), want) {
			t.Errorf("announcement of %q missing\nserve output:\n%s\njournal:\n%s", want, out, journal)
		}
	}
}

func TestMigrationGeneratesAnInstanceIDWhenNothingIsConfigured(t *testing.T) {
	path := writeFormat0State(t, []map[string]any{}, nil)
	store := loadForMigration(t, path)
	announcer, out, _ := recordingAnnouncer()
	if err := migrateHubState(t.Context(), newHostMessages(), announcer, store, migrationInputs{}); err != nil {
		t.Fatal(err)
	}
	reloaded := loadForMigration(t, path)
	if !instanceid.Validate(reloaded.InstanceID) {
		t.Fatalf("instance id after migration = %q, want a generated valid id", reloaded.InstanceID)
	}
	if !strings.Contains(out.String(), "generated") || !strings.Contains(out.String(), reloaded.InstanceID) {
		t.Fatalf("serve output does not announce the generated id:\n%s", out)
	}
}

func TestMigrationBindsAnUnboundSessionToTheBuiltChannelGateways(t *testing.T) {
	path := writeFormat0State(t, []map[string]any{{"name": "triage", "channelID": "c1", "type": "thread", "cmd": "claude"}}, map[string]any{"instanceID": "alice"})
	store := loadForMigration(t, path)
	if err := migrateHubState(t.Context(), newHostMessages(), discardAnnouncer(), store, migrationInputs{defaultGateways: []string{"chat"}}); err != nil {
		t.Fatal(err)
	}
	sess, _ := loadForMigration(t, path).FindSession("triage")
	if len(sess.Gateways) != 1 || sess.Gateways[0] != "chat" {
		t.Fatalf("gateways after migration = %v, want [chat]", sess.Gateways)
	}
}

func assertRefusedUntouched(t *testing.T, path string, err error, want string) {
	t.Helper()
	if _, ok := errors.AsType[*stateMigrationRefusedError](err); !ok {
		t.Fatalf("migration = %v, want a refusal", err)
	}
	text, _ := stateFormatText(clitext.NewMessages(), i18n.English, err)
	if !strings.Contains(text, want) {
		t.Fatalf("refusal text %q does not name %q", text, want)
	}
	if !loadForMigration(t, path).Outdated() {
		t.Fatal("a refused migration rewrote the state")
	}
}

func TestMigrationRefusesAChannelSessionWhenNoChannelGatewayIsBuilt(t *testing.T) {
	path := writeFormat0State(t, []map[string]any{{"name": "triage", "channelID": "c1", "type": "thread", "cmd": "claude"}}, map[string]any{"instanceID": "alice"})
	err := migrateHubState(t.Context(), newHostMessages(), discardAnnouncer(), loadForMigration(t, path), migrationInputs{})
	assertRefusedUntouched(t, path, err, "triage")
}

func TestMigrationPointsARemoteSessionAtItsNewPlaceAndSaysWhatToRunThere(t *testing.T) {
	path := writeFormat0State(t, []map[string]any{{"name": "far", "channelID": "c1", "type": "channel", "cmd": "claude", "host": "build1", "worktree": "/srv/work/app/.herrscher-sessions/far", "dir": "/srv/work/app/.herrscher-sessions/far", "gateways": []string{"chat"}}}, nil)
	announcer, out, _ := recordingAnnouncer()
	if err := migrateHubState(t.Context(), newHostMessages(), announcer, loadForMigration(t, path), migrationInputs{optID: "alice", defaultGateways: []string{"chat"}}); err != nil {
		t.Fatal(err)
	}
	sess, _ := loadForMigration(t, path).FindSession("far")
	if sess.Worktree != "/srv/work/app/.herrscher-sessions/alice/far" || sess.Dir != sess.Worktree {
		t.Fatalf("remote session worktree = %q dir = %q, want the instance layout path", sess.Worktree, sess.Dir)
	}
	for _, want := range []string{"build1", "worktree move '/srv/work/app/.herrscher-sessions/far' '/srv/work/app/.herrscher-sessions/alice/far'", "branch -m 'session/far' 'session/alice/far'", "fails to start there and keeps retrying"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("serve output does not tell the operator %q:\n%s", want, out)
		}
	}
}

func TestMigrationThatCannotKeepACopyMovesNothing(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root writes into a read-only folder")
	}
	_, from := bareSessionRepo(t, "alpha")
	path := writeFormat0State(t, []map[string]any{{"name": "alpha", "channelID": "c1", "type": "channel", "cmd": "claude", "worktree": from, "gateways": []string{"chat"}}}, nil)
	store := loadForMigration(t, path)
	stateDir := filepath.Dir(path)
	if err := os.Chmod(stateDir, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if chmodErr := os.Chmod(stateDir, 0o700); chmodErr != nil {
			t.Error(chmodErr)
		}
	})
	err := migrateHubState(t.Context(), newHostMessages(), discardAnnouncer(), store, migrationInputs{optID: "alice", defaultGateways: []string{"chat"}})
	assertRefusedUntouched(t, path, err, "could not be written")
	if _, statErr := os.Stat(from); statErr != nil {
		t.Fatalf("the worktree moved although no copy of the old state was kept: %v", statErr)
	}
}

func TestMigrationAnnouncesTheConversationAMovedSessionLeavesBehind(t *testing.T) {
	_, from := bareSessionRepo(t, "alpha")
	path := writeFormat0State(t, []map[string]any{{"name": "alpha", "channelID": "c1", "type": "channel", "cmd": "claude", "worktree": from, "dir": from, "resumeToken": "tok-1", "gateways": []string{"chat"}}}, nil)
	announcer, out, _ := recordingAnnouncer()
	if err := migrateHubState(t.Context(), newHostMessages(), announcer, loadForMigration(t, path), migrationInputs{optID: "alice", defaultGateways: []string{"chat"}}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "session alpha now works in") || !strings.Contains(out.String(), "new conversation") {
		t.Fatalf("serve output does not announce the conversation reset:\n%s", out)
	}
}

func TestMigrationAnnouncesOnlyWhatItMoved(t *testing.T) {
	repo := tempRepo(t)
	gone := filepath.Join(repo, ".herrscher-sessions", "gone")
	path := writeFormat0State(t, []map[string]any{{"name": "gone", "channelID": "c1", "type": "channel", "cmd": "claude", "worktree": gone, "gateways": []string{"chat"}}}, nil)
	announcer, out, _ := recordingAnnouncer()
	if err := migrateHubState(t.Context(), newHostMessages(), announcer, loadForMigration(t, path), migrationInputs{optID: "alice", defaultGateways: []string{"chat"}}); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out.String(), "moved to") || strings.Contains(out.String(), "renamed") {
		t.Fatalf("serve output announces a move that did not happen:\n%s", out)
	}
	if sess, _ := loadForMigration(t, path).FindSession("gone"); sess.Worktree != gone {
		t.Fatalf("worktree of a session found nowhere = %q, want it left at %q", sess.Worktree, gone)
	}
}

func TestMigrationRefusesASessionNamedLikeTheInstanceBeforeMovingAnyOther(t *testing.T) {
	repo, dev := bareSessionRepo(t, "dev")
	other := filepath.Join(repo, ".herrscher-sessions", "x")
	if out, err := exec.CommandContext(t.Context(), "git", "-C", repo, "worktree", "add", "-q", other, "-b", "session/x").CombinedOutput(); err != nil {
		t.Fatalf("worktree add: %v\n%s", err, out)
	}
	path := writeFormat0State(t, []map[string]any{
		{"name": "x", "channelID": "c1", "type": "channel", "cmd": "claude", "worktree": other, "gateways": []string{"chat"}},
		{"name": "dev", "channelID": "c2", "type": "channel", "cmd": "claude", "worktree": dev, "gateways": []string{"chat"}},
	}, nil)
	err := migrateHubState(t.Context(), newHostMessages(), discardAnnouncer(), loadForMigration(t, path), migrationInputs{optID: "dev", defaultGateways: []string{"chat"}})
	assertRefusedUntouched(t, path, err, "HERRSCHER_INSTANCE_ID")
	if _, statErr := os.Stat(other); statErr != nil {
		t.Fatalf("session x moved before the refusal: %v", statErr)
	}
}

func TestMigrationAnnouncesTheNumberedCopyItKeeps(t *testing.T) {
	path := writeFormat0State(t, []map[string]any{}, map[string]any{"instanceID": "alice"})
	if err := os.WriteFile(path+".before-format-1.bak", []byte("earlier"), 0o600); err != nil {
		t.Fatal(err)
	}
	announcer, out, _ := recordingAnnouncer()
	if err := migrateHubState(t.Context(), newHostMessages(), announcer, loadForMigration(t, path), migrationInputs{}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), path+".before-format-1.2.bak") {
		t.Fatalf("serve output does not name the copy it kept:\n%s", out)
	}
}

func TestMigrationRefusesAndMovesNothingWhenATargetIsTaken(t *testing.T) {
	repo, from := bareSessionRepo(t, "alpha")
	if err := os.MkdirAll(filepath.Join(repo, ".herrscher-sessions", "alice", "alpha"), 0o750); err != nil {
		t.Fatal(err)
	}
	path := writeFormat0State(t, []map[string]any{{"name": "alpha", "channelID": "c1", "type": "channel", "cmd": "claude", "worktree": from, "gateways": []string{"chat"}}}, nil)
	err := migrateHubState(t.Context(), newHostMessages(), discardAnnouncer(), loadForMigration(t, path), migrationInputs{optID: "alice", defaultGateways: []string{"chat"}})
	assertRefusedUntouched(t, path, err, "already exists")
	if _, statErr := os.Stat(from); statErr != nil {
		t.Fatalf("the old worktree moved despite the refusal: %v", statErr)
	}
}

func TestNewRegistryRefusesAStateThatIsNotMigratedYet(t *testing.T) {
	path := writeFormat0State(t, []map[string]any{}, nil)
	_, err := NewRegistry(t.Context(), clitext.NewMessages(), NewMessages(), nil, Options{StatePath: path, Warnings: io.Discard})
	if err == nil || !strings.Contains(err.Error(), "restart the daemon") || !strings.Contains(err.Error(), "stop the herrscher serve still running") {
		t.Fatalf("NewRegistry = %v, want the refusal that asks to restart the daemon", err)
	}
}

func TestANewerStateFormatIsRefusedWithItsPathAndFormats(t *testing.T) {
	err := &state.NewerFormatError{Path: "/x/state.json", Found: 9, Supported: state.CurrentFormat}
	text, ok := stateFormatText(clitext.NewMessages(), i18n.French, err)
	if !ok || !strings.Contains(text, "/x/state.json") || !strings.Contains(text, "9") {
		t.Fatalf("text = %q, want the path and the format of the file", text)
	}
}
