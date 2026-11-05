package host

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	contracts "github.com/Herrscherd/herrscher-contracts"
	"github.com/Herrscherd/herrscher/core/cli"
	"github.com/Herrscherd/herrscher/core/internal/checkpoint"
	"github.com/Herrscherd/herrscher/core/internal/conversation"
	"github.com/Herrscherd/herrscher/core/internal/state"
)

const frameFile = "frame.go"

func gitIn(ctx context.Context, t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %s: %v", args, out, err)
	}
}

func checkpointRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	ctx := t.Context()
	gitIn(ctx, t, dir, "init", "-q")
	writeFrame(t, dir, "v1\n")
	gitIn(ctx, t, dir, "add", "-A")
	gitIn(ctx, t, dir, "-c", "user.name=t", "-c", "user.email=t@t", "commit", "-q", "-m", "init")
	return dir
}

func writeFrame(t *testing.T, dir, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, frameFile), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func readFrame(t *testing.T, dir string) string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(dir, frameFile))
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

func checkpointState(t *testing.T, worktree string) *state.State {
	t.Helper()
	const name = "alpha"
	st, err := state.LoadState(filepath.Join(t.TempDir(), "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := st.AddSession(state.Session{Name: name, ChannelID: "chan-" + name, Worktree: worktree}); err != nil {
		t.Fatal(err)
	}
	return st
}

func checkpointRegistry(st *state.State, sessions *sessionTable) *cli.Registry {
	reg := &cli.Registry{}
	if err := addCheckpointCommands(reg, st, sessions); err != nil {
		panic(err)
	}
	return reg
}

func TestCheckpointsListsMessagesWithTheFilesTheyTouched(t *testing.T) {
	repo := checkpointRepo(t)
	ctx := context.Background()
	checkpoints := &checkpoint.Store{}
	st := checkpointState(t, repo)
	if _, err := checkpoints.Take(ctx, repo, "alpha", checkpoint.Meta{Turn: "t1", Text: "ajoute une file"}); err != nil {
		t.Fatal(err)
	}
	writeFrame(t, repo, "v2\nv2b\n")
	reg := checkpointRegistry(st, newSessionTable())

	out, err := cli.Plain(reg.Dispatch(ctx, []string{nsSession, verbCheckpoints, "--name", "alpha", "--json"}))
	if err != nil {
		t.Fatal(err)
	}
	var views []checkpointView
	if err := json.Unmarshal([]byte(out), &views); err != nil {
		t.Fatalf("not json: %q: %v", out, err)
	}
	if len(views) != 1 || views[0].Index != 1 || views[0].Text != "ajoute une file" || views[0].Turn != "t1" {
		t.Fatalf("views = %+v", views)
	}
	if len(views[0].Files) != 1 || views[0].Files[0].Path != "frame.go" || views[0].Files[0].Added != 2 {
		t.Fatalf("files = %+v", views[0].Files)
	}
}

func TestRewindRestoresCodeAndRefusesConversation(t *testing.T) {
	repo := checkpointRepo(t)
	ctx := context.Background()
	checkpoints := &checkpoint.Store{}
	st := checkpointState(t, repo)
	if _, err := checkpoints.Take(ctx, repo, "alpha", checkpoint.Meta{Turn: "t1", Text: "avant"}); err != nil {
		t.Fatal(err)
	}
	writeFrame(t, repo, "v2\n")
	reg := checkpointRegistry(st, newSessionTable())

	_, err := reg.Dispatch(ctx, []string{nsSession, verbRewind, "--name", "alpha", "--to", "1", "--what", "both"})
	if err == nil || !strings.Contains(err.Error(), "not available") {
		t.Fatalf("conversation restore must be refused explicitly, err = %v", err)
	}
	if got := readFrame(t, repo); got != "v2\n" {
		t.Fatalf("refused rewind still touched the code: %q", got)
	}

	if _, err := reg.Dispatch(ctx, []string{nsSession, verbRewind, "--name", "alpha", "--to", "1", "--what", "code"}); err != nil {
		t.Fatal(err)
	}
	if got := readFrame(t, repo); got != "v1\n" {
		t.Fatalf("code not restored: %q", got)
	}

	if _, err := reg.Dispatch(ctx, []string{nsSession, verbUndo, "--name", "alpha"}); err != nil {
		t.Fatal(err)
	}
	if got := readFrame(t, repo); got != "v2\n" {
		t.Fatalf("undo did not bring the code back: %q", got)
	}
}

func TestRewindRefusedWhileTurnInFlight(t *testing.T) {
	repo := checkpointRepo(t)
	ctx := context.Background()
	checkpoints := &checkpoint.Store{}
	st := checkpointState(t, repo)
	if _, err := checkpoints.Take(ctx, repo, "alpha", checkpoint.Meta{Turn: "t1", Text: "avant"}); err != nil {
		t.Fatal(err)
	}
	sessions := newSessionTable()
	driver := newSessionDriver("alpha", nil, nil, nil)
	if !driver.claimTurn() {
		t.Fatal("the fixture must put a turn in flight")
	}
	sessions.register("alpha", driver)
	reg := checkpointRegistry(st, sessions)

	_, err := reg.Dispatch(ctx, []string{nsSession, verbRewind, "--name", "alpha", "--to", "1", "--what", "code"})
	if err == nil || !strings.Contains(err.Error(), "turn in flight") {
		t.Fatalf("rewind during a turn must be refused, err = %v", err)
	}
}

type fakeForkHost struct {
	t        *testing.T
	repo     string
	st       *state.State
	restarts []restart
}

type restart struct {
	name, token string
}

type fakeRestarter struct {
	restarts []restart
}

func (f *fakeRestarter) RestartWithResume(name, token string) error {
	f.restarts = append(f.restarts, restart{name: name, token: token})
	return nil
}

func (f *fakeForkHost) RestartWithResume(name, token string) error {
	f.restarts = append(f.restarts, restart{name: name, token: token})
	return nil
}

func (f *fakeForkHost) CreateCoordinated(ctx context.Context, spec contracts.CreateSession, _ string) (contracts.Result, error) {
	dir := filepath.Join(f.t.TempDir(), spec.Name)
	gitIn(ctx, f.t, f.repo, "worktree", "add", "-q", dir, "-b", spec.Name, spec.Base)
	if err := f.st.AddSession(state.Session{Name: spec.Name, ChannelID: "chan-" + spec.Name, Worktree: dir}); err != nil {
		return contracts.Result{}, err
	}
	return contracts.DataResult(spec.Name), nil
}

func (f *fakeForkHost) Close(_ context.Context, _ string, _ bool) (contracts.Result, error) {
	return contracts.Result{}, nil
}

func TestForkCreatesOwnWorktreeAtCheckpointAndLeavesOriginal(t *testing.T) {
	repo := checkpointRepo(t)
	ctx := context.Background()
	checkpoints := &checkpoint.Store{}
	st := checkpointState(t, repo)
	if _, err := checkpoints.Take(ctx, repo, "alpha", checkpoint.Meta{Turn: "t1", Text: "avant", Conversation: conversation.Start}); err != nil {
		t.Fatal(err)
	}
	writeFrame(t, repo, "v2\n")
	host := &fakeForkHost{t: t, repo: repo, st: st}

	out, err := forkCheckpoint(ctx, host, st, checkpoints, contracts.Input{Args: map[string]string{paramName: "alpha", paramTo: "1"}})
	if err != nil {
		t.Fatal(err)
	}
	if out != "alpha-fork" {
		t.Fatalf("fork name = %q", out)
	}
	fork, ok := st.FindSession("alpha-fork")
	if !ok || fork.Worktree == "" || fork.Worktree == repo {
		t.Fatalf("fork has no worktree of its own: %+v", fork)
	}
	if got := readFrame(t, fork.Worktree); got != "v1\n" {
		t.Fatalf("fork not at checkpoint: %q", got)
	}
	if got := readFrame(t, repo); got != "v2\n" {
		t.Fatalf("original session was touched: %q", got)
	}
}

func writeTranscript(t *testing.T, home, dir, token string, uuids ...string) {
	t.Helper()
	var b strings.Builder
	for _, id := range uuids {
		fmt.Fprintf(&b, `{"uuid":%q,"sessionId":%q,"type":"user"}`+"\n", id, token)
	}
	path := conversation.TranscriptPath(home, dir, token)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(b.String()), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestRewindConversationResumesAtTheCheckpointPosition(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	repo := checkpointRepo(t)
	ctx := context.Background()
	checkpoints := &checkpoint.Store{}
	st := checkpointState(t, repo)
	if err := st.SetResumeToken("alpha", "tok-1"); err != nil {
		t.Fatal(err)
	}
	writeTranscript(t, home, repo, "tok-1", "u1", "u2", "u3")
	if _, err := checkpoints.Take(ctx, repo, "alpha", checkpoint.Meta{Turn: "t1", Text: "avant", Conversation: "u2"}); err != nil {
		t.Fatal(err)
	}
	restarter := &fakeRestarter{}
	reg := checkpointRegistry(st, newSessionTable())

	if _, err := reg.Dispatch(withRestarter(ctx, restarter), []string{nsSession, verbRewind, "--name", "alpha", "--to", "1", "--what", "conversation"}); err != nil {
		t.Fatal(err)
	}
	if len(restarter.restarts) != 1 || restarter.restarts[0].name != "alpha" || restarter.restarts[0].token == "" || restarter.restarts[0].token == "tok-1" {
		t.Fatalf("rewind must restart alpha on a forked conversation, restarts = %+v", restarter.restarts)
	}
	forked := restarter.restarts[0].token
	raw, err := os.ReadFile(conversation.TranscriptPath(home, repo, forked))
	if err != nil {
		t.Fatal(err)
	}
	if lines := strings.Count(string(raw), "\n"); lines != 2 || !strings.Contains(string(raw), `"sessionId":"`+forked+`"`) {
		t.Fatalf("forked transcript must stop at u2 under its new id, got %d lines: %s", lines, raw)
	}
	original, err := os.ReadFile(conversation.TranscriptPath(home, repo, "tok-1"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(string(original), "\n") != 3 {
		t.Fatal("the original transcript must be left untouched")
	}
}

func TestForkCarriesTheConversationToTheCheckpoint(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	repo := checkpointRepo(t)
	ctx := context.Background()
	checkpoints := &checkpoint.Store{}
	st := checkpointState(t, repo)
	if err := st.SetResumeToken("alpha", "tok-1"); err != nil {
		t.Fatal(err)
	}
	writeTranscript(t, home, repo, "tok-1", "u1", "u2", "u3")
	if _, err := checkpoints.Take(ctx, repo, "alpha", checkpoint.Meta{Turn: "t1", Text: "avant", Conversation: "u1"}); err != nil {
		t.Fatal(err)
	}
	host := &fakeForkHost{t: t, repo: repo, st: st}

	if _, err := forkCheckpoint(ctx, host, st, checkpoints, contracts.Input{Args: map[string]string{paramName: "alpha", paramTo: "1"}}); err != nil {
		t.Fatal(err)
	}
	fork, _ := st.FindSession("alpha-fork")
	if len(host.restarts) != 1 || host.restarts[0].name != "alpha-fork" {
		t.Fatalf("the fork must resume its own conversation, restarts = %+v", host.restarts)
	}
	raw, err := os.ReadFile(conversation.TranscriptPath(home, fork.Worktree, host.restarts[0].token))
	if err != nil {
		t.Fatalf("the fork transcript must live in the fork's own project directory: %v", err)
	}
	if strings.Count(string(raw), "\n") != 1 {
		t.Fatalf("the fork must stop at u1, got %q", raw)
	}
}

func TestForkRefusedWhenTheCheckpointHasNoConversationPosition(t *testing.T) {
	repo := checkpointRepo(t)
	ctx := context.Background()
	checkpoints := &checkpoint.Store{}
	st := checkpointState(t, repo)
	if _, err := checkpoints.Take(ctx, repo, "alpha", checkpoint.Meta{Turn: "t1", Text: "avant"}); err != nil {
		t.Fatal(err)
	}
	host := &fakeForkHost{t: t, repo: repo, st: st}

	_, err := forkCheckpoint(ctx, host, st, checkpoints, contracts.Input{Args: map[string]string{paramName: "alpha", paramTo: "1"}})
	if err == nil || !strings.Contains(err.Error(), "nothing was created") {
		t.Fatalf("fork without a conversation position must be refused, err = %v", err)
	}
	if _, ok := st.FindSession("alpha-fork"); ok {
		t.Fatal("a refused fork must not create a session")
	}
}
