package worktree

import (
	"context"
	"os/exec"
	"strings"
	"testing"
)

// fakeCommander answers with a canned stdout instead of running anything, so
// the façade can be tested without ssh, without git and without a second
// machine. `sh -c` is used because the value under test is the argv the façade
// asks for, and the fake still has to produce a runnable command.
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
	r := NewRemote(context.Background(), f, "/home/me/.herrscher/bin/herrscher", "inst")

	got, err := r.Create("/srv/work/proj", "s1", "session/other")
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
	// The verb takes the repo as a flag, so the command needs no working
	// directory of its own: asking to cd into a repo that may not exist yet
	// would fail before the verb could say so.
	if f.dirs[0] != "" {
		t.Fatalf("dir = %q, want empty", f.dirs[0])
	}
}

func TestRemoteCreateOmitsAnEmptyBase(t *testing.T) {
	f := &fakeCommander{stdout: `{"path":"/p"}`}
	if _, err := NewRemote(context.Background(), f, "hs", "inst").Create("/srv/work/proj", "s1", ""); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(strings.Join(f.seen[0], " "), "--base") {
		t.Fatalf("argv carries an empty --base: %v", f.seen[0])
	}
}

func TestRemoteCreateSurfacesTheRemoteError(t *testing.T) {
	f := &fakeCommander{fail: true}
	_, err := NewRemote(context.Background(), f, "hs", "inst").Create("/srv/work/proj", "s1", "")
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
	if !NewRemote(context.Background(), f, "hs", "inst").PreExisting("/srv/work/proj", "s1") {
		t.Fatal("want true")
	}
}

// An unreachable host must not be read as "no worktree there". PreExisting has
// no error return, and false is the answer that makes a rollback delete a
// worktree it did not create, so the safe direction is true.
func TestRemotePreExistingIsCautiousWhenItCannotAsk(t *testing.T) {
	f := &fakeCommander{fail: true}
	if !NewRemote(context.Background(), f, "hs", "inst").PreExisting("/srv/work/proj", "s1") {
		t.Fatal("an unanswerable question must not be read as 'nothing there'")
	}
}

func TestRemoteRemove(t *testing.T) {
	f := &fakeCommander{stdout: `{"removed":true}`}
	if err := NewRemote(context.Background(), f, "hs", "inst").Remove("/srv/work/proj", "s1", true); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(strings.Join(f.seen[0], " "), "--force") {
		t.Fatalf("argv = %v", f.seen[0])
	}
}

func TestRemoteBranchDoesNotTravel(t *testing.T) {
	f := &fakeCommander{}
	if got := NewRemote(context.Background(), f, "hs", "inst").Branch("s1"); got != "session/inst/s1" {
		t.Fatalf("Branch = %q", got)
	}
	if len(f.seen) != 0 {
		t.Fatalf("Branch made %d remote calls, want 0", len(f.seen))
	}
}

func TestRemoteMaterializeFeedsThePayloadOnStdin(t *testing.T) {
	f := &fakeCommander{stdout: `{"materialized":true}`}
	err := NewRemote(context.Background(), f, "hs", "inst").Materialize(context.Background(), "/srv/work/wt", strings.NewReader("tar bytes"))
	if err != nil {
		t.Fatal(err)
	}
	argv := strings.Join(f.seen[0], " ")
	if argv != "hs worktree materialize --worktree /srv/work/wt" {
		t.Fatalf("argv = %q", argv)
	}
}
