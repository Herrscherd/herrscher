package worktree

import (
	"context"
	"errors"
	"os/exec"
	"strings"
	"testing"

	"github.com/Herrscherd/herrscher/core/clitext"
)

type fakeCommander struct {
	stdout string
	fail   bool
	seen   [][]string
	dirs   []string
}

func (f *fakeCommander) Command(ctx context.Context, dir string, argv ...string) *exec.Cmd {
	f.seen = append(f.seen, argv)
	f.dirs = append(f.dirs, dir)
	if f.fail {
		return exec.CommandContext(ctx, "sh", "-c", "echo boom >&2; exit 1")
	}
	return exec.CommandContext(ctx, "sh", "-c", "printf '%s' '"+f.stdout+"'")
}

func (f *fakeCommander) Describe() string { return "fake" }

func TestRemoteCreateAsksForTheVerbAndReadsThePath(t *testing.T) {
	f := &fakeCommander{stdout: `{"path":"/srv/work/proj/.herrscher-sessions/inst/s1"}`}
	r := NewRemote(clitext.NewMessages(), f, "/home/me/.herrscher/bin/herrscher", "inst")

	got, err := r.Create(context.Background(), "/srv/work/proj", "s1", "session/other")
	if err != nil {
		t.Fatal(err)
	}
	if got != "/srv/work/proj/.herrscher-sessions/inst/s1" {
		t.Fatalf("path = %q", got)
	}
	argv := strings.Join(f.seen[0], " ")
	want := "/home/me/.herrscher/bin/herrscher worktree create --repo /srv/work/proj --name s1 --instance inst --base session/other"
	if argv != want {
		t.Fatalf("argv = %q, want %q", argv, want)
	}
	if f.dirs[0] != "" {
		t.Fatalf("dir = %q, want empty", f.dirs[0])
	}
}

func TestRemoteCreateOmitsAnEmptyBase(t *testing.T) {
	f := &fakeCommander{stdout: `{"path":"/p"}`}
	if _, err := NewRemote(clitext.NewMessages(), f, "hs", "inst").Create(context.Background(), "/srv/work/proj", "s1", ""); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(strings.Join(f.seen[0], " "), "--base") {
		t.Fatalf("argv carries an empty --base: %v", f.seen[0])
	}
}

func TestRemoteCreateSurfacesTheRemoteError(t *testing.T) {
	f := &fakeCommander{fail: true}
	_, err := NewRemote(clitext.NewMessages(), f, "hs", "inst").Create(context.Background(), "/srv/work/proj", "s1", "")
	if err == nil {
		t.Fatal("want an error")
	}
	if !strings.Contains(err.Error(), "boom") {
		t.Fatalf("the remote message is lost: %v", err)
	}
	if !strings.Contains(err.Error(), "fake") {
		t.Fatalf("the error does not say where it happened: %v", err)
	}
}

func TestRemotePreExisting(t *testing.T) {
	f := &fakeCommander{stdout: `{"preExisting":true}`}
	if !NewRemote(clitext.NewMessages(), f, "hs", "inst").PreExisting(context.Background(), "/srv/work/proj", "s1") {
		t.Fatal("want true")
	}
}

func TestRemotePreExistingIsCautiousWhenItCannotAsk(t *testing.T) {
	f := &fakeCommander{fail: true}
	if !NewRemote(clitext.NewMessages(), f, "hs", "inst").PreExisting(context.Background(), "/srv/work/proj", "s1") {
		t.Fatal("an unanswerable question must not be read as 'nothing there'")
	}
}

func TestRemoteRemove(t *testing.T) {
	f := &fakeCommander{stdout: `{"removed":true}`}
	if err := NewRemote(clitext.NewMessages(), f, "hs", "inst").RemoveAt(context.Background(), "/srv/work/proj", "/srv/work/proj/.herrscher-sessions/inst/s1", true); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(strings.Join(f.seen[0], " "), "--force") {
		t.Fatalf("argv = %v", f.seen[0])
	}
}

func TestRemoteRemoveAtRefusesAPathOutsideTheSessionsRoot(t *testing.T) {
	f := &fakeCommander{stdout: `{"removed":true}`}
	if err := NewRemote(clitext.NewMessages(), f, "hs", "inst").RemoveAt(context.Background(), "/srv/work/proj", "/srv/work/elsewhere", true); err == nil {
		t.Fatal("a recorded path outside the sessions root must be refused")
	}
	if len(f.seen) != 0 {
		t.Fatalf("the refusal made %d remote calls, want 0", len(f.seen))
	}
}

func TestRemoteBranchDoesNotTravel(t *testing.T) {
	f := &fakeCommander{}
	if got := NewRemote(clitext.NewMessages(), f, "hs", "inst").Branch("s1"); got != "session/inst/s1" {
		t.Fatalf("Branch = %q", got)
	}
	if len(f.seen) != 0 {
		t.Fatalf("Branch made %d remote calls, want 0", len(f.seen))
	}
}

func TestRemoteMaterializeFeedsThePayloadOnStdin(t *testing.T) {
	f := &fakeCommander{stdout: `{"materialized":true}`}
	err := NewRemote(clitext.NewMessages(), f, "hs", "inst").Materialize(context.Background(), "/srv/work/wt", strings.NewReader("tar bytes"))
	if err != nil {
		t.Fatal(err)
	}
	argv := strings.Join(f.seen[0], " ")
	if argv != "hs worktree materialize --worktree /srv/work/wt" {
		t.Fatalf("argv = %q", argv)
	}
}

func TestRemoteBranchAtReadsTheCheckedOutBranchOnTheHost(t *testing.T) {
	f := &fakeCommander{stdout: "session/inst/alpha\n"}
	got, err := NewRemote(clitext.NewMessages(), f, "hs", "inst").BranchAt(t.Context(), "/srv/work/proj/.herrscher-sessions/inst/alpha")
	if err != nil {
		t.Fatal(err)
	}
	if got != "session/inst/alpha" {
		t.Fatalf("branch = %q, want session/inst/alpha", got)
	}
	argv := strings.Join(f.seen[0], " ")
	if want := "git -C /srv/work/proj/.herrscher-sessions/inst/alpha symbolic-ref --short HEAD"; argv != want {
		t.Fatalf("argv = %q, want %q", argv, want)
	}
}

func TestRemoteBranchAtRefusesWithTheHostError(t *testing.T) {
	f := &fakeCommander{fail: true}
	_, err := NewRemote(clitext.NewMessages(), f, "hs", "inst").BranchAt(t.Context(), "/srv/p")
	failure, ok := errors.AsType[FailureError](err)
	if !ok || failure.Key != clitext.WorktreeBranchUnreadable {
		t.Fatalf("err = %v, want a %s refusal", err, clitext.WorktreeBranchUnreadable)
	}
	if !strings.Contains(err.Error(), "boom") {
		t.Fatalf("the remote message is lost: %v", err)
	}
}
