package host

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	contracts "github.com/Herrscherd/herrscher-contracts"

	"github.com/Herrscherd/herrscher/core/clitext"
	"github.com/Herrscherd/herrscher/core/internal/runner"
	"github.com/Herrscherd/herrscher/core/internal/shellquote"
	"github.com/Herrscherd/herrscher/core/internal/worktree"
)

func TestMigrationPrintsRemoteCommandsTheShellRunsAsWritten(t *testing.T) {
	root := filepath.Join(t.TempDir(), "my $HOME", "it's `id`")
	from := filepath.Join(root, ".herrscher-scratch", "far")
	if err := os.MkdirAll(from, 0o750); err != nil {
		t.Fatal(err)
	}
	path := writeFormat0State(t, []map[string]any{{"name": "far", "channelID": "c1", "type": "channel", "cmd": "claude", "host": "build1", "scratch": true, "worktree": from, "dir": from, "gateways": []string{"chat"}}}, nil)
	announcer, out, _ := recordingAnnouncer()
	if err := migrateHubState(t.Context(), newHostMessages(), announcer, loadForMigration(t, path), migrationInputs{optID: "alice", defaultGateways: []string{"chat"}}); err != nil {
		t.Fatal(err)
	}
	_, command, found := strings.Cut(strings.TrimSpace(out.String()), "keeps retrying: ")
	if !found {
		t.Fatalf("serve output carries no command to run on the host:\n%s", out)
	}
	command, _, _ = strings.Cut(command, "\n")
	if runOut, err := exec.CommandContext(t.Context(), "sh", "-c", command).CombinedOutput(); err != nil {
		t.Fatalf("printed command %q failed: %v\n%s", command, err, runOut)
	}
	if _, err := os.Stat(filepath.Join(root, ".herrscher-scratch", "alice", "far")); err != nil {
		t.Fatalf("printed command did not move the scratch folder where the state points: %v", err)
	}
}

func gitHubOriginRepo(t *testing.T) string {
	t.Helper()
	repo := tempRepo(t)
	if out, err := exec.CommandContext(t.Context(), "git", "-C", repo, "remote", "add", "origin", "https://github.com/acme/app.git").CombinedOutput(); err != nil {
		t.Fatalf("remote add: %v\n%s", err, out)
	}
	return repo
}

func TestMigrationAsksOriginWithTheForgeTokenAndABatchSSH(t *testing.T) {
	t.Setenv("GIT_SSH_COMMAND", "")
	t.Setenv("GIT_SSH", "")
	repo := gitHubOriginRepo(t)
	token, err := contracts.NewForgeToken(prTestSecret, "bot", contracts.TokenSourceEnv)
	if err != nil {
		t.Fatal(err)
	}
	access := migrationOriginAccess(t.Context(), fixedPRCredentials{token: token}, repo, 12)
	if access.Unavailable != nil {
		t.Fatal(access.Unavailable)
	}
	if !slices.Contains(access.Env, "GIT_SSH_COMMAND="+runner.GitSSHCommand) {
		t.Fatalf("env %q does not run ssh in batch mode", access.Env)
	}
	if !slices.ContainsFunc(access.Env, func(entry string) bool { return strings.Contains(entry, "http.https://github.com/.extraheader") }) {
		t.Fatalf("env %q does not carry the forge token for github.com", access.Env)
	}
}

func TestMigrationKeepsTheSSHCommandTheRepoConfigures(t *testing.T) {
	t.Setenv("GIT_SSH_COMMAND", "")
	t.Setenv("GIT_SSH", "")
	repo := gitHubOriginRepo(t)
	if out, err := exec.CommandContext(t.Context(), "git", "-C", repo, "config", "core.sshCommand", "ssh -i /keys/deploy").CombinedOutput(); err != nil {
		t.Fatalf("config: %v\n%s", err, out)
	}
	access := migrationOriginAccess(t.Context(), nil, repo, 12)
	if access.Unavailable != nil {
		t.Fatal(access.Unavailable)
	}
	if slices.ContainsFunc(access.Env, func(entry string) bool { return strings.HasPrefix(entry, "GIT_SSH_COMMAND=") }) {
		t.Fatalf("env %q overrides the configured core.sshCommand", access.Env)
	}
}

type failingPRCredentials struct{}

var errPRTokenRefresh = errors.New("token refresh: network is unreachable")

func (failingPRCredentials) Token(context.Context, contracts.ForgeHost) (contracts.ForgeToken, error) {
	return contracts.ForgeToken{}, errPRTokenRefresh
}

func (failingPRCredentials) Invalidate(context.Context, contracts.ForgeHost) {}

func bareSessionWithOrigin(t *testing.T, url string) (string, string) {
	t.Helper()
	repo, from := bareSessionRepo(t, "alpha")
	if out, err := exec.CommandContext(t.Context(), "git", "-C", repo, "remote", "add", "origin", url).CombinedOutput(); err != nil {
		t.Fatalf("remote add: %v\n%s", err, out)
	}
	return repo, from
}

func TestMigrationRefusesAsUnreachableWhenTheForgeTokenCannotBeHad(t *testing.T) {
	_, from := bareSessionWithOrigin(t, "https://github.com/acme/app.git")
	path := writeFormat0State(t, []map[string]any{{"name": "alpha", "channelID": "c1", "type": "channel", "cmd": "claude", "worktree": from, "dir": from, "gateways": []string{"chat"}, "pullRequest": 12}}, nil)
	err := migrateHubState(t.Context(), newHostMessages(), discardAnnouncer(), loadForMigration(t, path), migrationInputs{optID: "inst", defaultGateways: []string{"chat"}, forge: failingPRCredentials{}})
	if !errors.Is(err, errPRTokenRefresh) {
		t.Fatalf("err = %v, want the token failure as its cause", err)
	}
	failure, ok := errors.AsType[worktree.FailureError](err)
	if !ok || failure.Key != clitext.WorktreeRelocateRemoteUnreachable {
		t.Fatalf("err = %v, want a %s refusal", err, clitext.WorktreeRelocateRemoteUnreachable)
	}
	text := err.Error()
	for _, want := range []string{`"alpha"`, "worktree move", "branch -m 'session/alpha' 'session/inst/alpha'"} {
		if !strings.Contains(text, want) {
			t.Errorf("refusal %q does not carry %q", text, want)
		}
	}
	if _, statErr := os.Stat(from); statErr != nil {
		t.Fatalf("the refused migration moved the session: %v", statErr)
	}
}

func fakeSSHRecorder(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	argv := filepath.Join(dir, "argv")
	script := "#!/bin/sh\nprintf '%s\\n' \"$@\" > " + shellquote.Quote(argv) + "\nexit 255\n"
	if err := os.WriteFile(filepath.Join(dir, "ssh"), []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	return argv
}

func unsetEnv(t *testing.T, keys ...string) {
	t.Helper()
	for _, key := range keys {
		t.Setenv(key, "")
		if err := os.Unsetenv(key); err != nil {
			t.Fatal(err)
		}
	}
}

func TestMigrationRunsTheOriginQuestionThroughABatchSSH(t *testing.T) {
	unsetEnv(t, "GIT_SSH_COMMAND", "GIT_SSH")
	repo, from := bareSessionWithOrigin(t, "ssh://git@github.com/acme/app.git")
	argv := fakeSSHRecorder(t)
	worktrees := worktree.NewWorktreer(clitext.NewMessages(), "inst")
	_, err := planLocalBareRelocation(t.Context(), worktrees, nil, repo, worktree.BareSession{Name: "alpha", Path: from, PullRequest: 12}, sessionPRForgeLimit)
	if failure, ok := errors.AsType[worktree.FailureError](err); !ok || failure.Key != clitext.WorktreeRelocateRemoteUnreachable {
		t.Fatalf("err = %v, want a %s refusal", err, clitext.WorktreeRelocateRemoteUnreachable)
	}
	recorded, readErr := os.ReadFile(argv)
	if readErr != nil {
		t.Fatalf("ls-remote never reached ssh: %v", readErr)
	}
	if !strings.Contains(string(recorded), "BatchMode=yes") {
		t.Fatalf("ssh ran with %q, want BatchMode=yes", recorded)
	}
}
