package host

import (
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	contracts "github.com/Herrscherd/herrscher-contracts"

	"github.com/Herrscherd/herrscher/core/internal/runner"
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
	access, err := migrationOriginAccess(t.Context(), fixedPRCredentials{token: token}, repo, 12)
	if err != nil {
		t.Fatal(err)
	}
	if access.Limit != sessionPRForgeLimit {
		t.Fatalf("limit = %s, want %s", access.Limit, sessionPRForgeLimit)
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
	access, err := migrationOriginAccess(t.Context(), nil, repo, 12)
	if err != nil {
		t.Fatal(err)
	}
	if slices.ContainsFunc(access.Env, func(entry string) bool { return strings.HasPrefix(entry, "GIT_SSH_COMMAND=") }) {
		t.Fatalf("env %q overrides the configured core.sshCommand", access.Env)
	}
}
