package host

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	contracts "github.com/Herrscherd/herrscher-contracts"
	"github.com/Herrscherd/herrscher/core/clitext"
	"github.com/Herrscherd/herrscher/core/internal/state"
	"github.com/Herrscherd/herrscher/core/internal/worktree"
)

func renamedCoordinator(t *testing.T, sessions []state.Session, cr *fakeCreator, seeded *[]string) *coordinator {
	t.Helper()
	seed := func(sess, task string) bool { *seeded = append(*seeded, sess+"|"+task); return true }
	wt := worktree.NewWorktreer(clitext.NewMessages(), "inst")
	return newCoordinator(newHostMessages(), cr, fakeAgents{known: map[string]bool{"scripter": true}}, wt, fakeSessions{list: sessions}, &fakeCloser{}, seed)
}

func sessionWorktree(t *testing.T, repo, name string) string {
	t.Helper()
	path, err := worktree.NewWorktreer(clitext.NewMessages(), "inst").Create(t.Context(), repo, name, "")
	if err != nil {
		t.Fatal(err)
	}
	return path
}

func commitIn(t *testing.T, dir, file string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, file), []byte(file), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, argv := range [][]string{{"add", file}, {"commit", "-q", "-m", file}} {
		if out, err := exec.CommandContext(t.Context(), "git", append([]string{"-C", dir}, argv...)...).CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", argv, err, out)
		}
	}
}

func TestASpawnFromARenamedLeadStartsFromItsRealBranch(t *testing.T) {
	repo := tempRepo(t)
	lead := sessionWorktree(t, repo, "alpha")
	cr := &fakeCreator{}
	var seeded []string
	c := renamedCoordinator(t, []state.Session{{Name: "beta", Worktree: lead}}, cr, &seeded)

	if _, err := c.Handoff(context.Background(), contracts.HandoffRequest{FromSession: "beta", ToAgent: "scripter", Task: "go"}); err != nil {
		t.Fatal(err)
	}
	if cr.spec.Base != "session/inst/alpha" {
		t.Fatalf("base = %q, want the branch the lead has checked out, session/inst/alpha", cr.spec.Base)
	}
}

func TestMergingARenamedWorkerMergesItsRealBranch(t *testing.T) {
	repo := tempRepo(t)
	lead := sessionWorktree(t, repo, "lead")
	worker := sessionWorktree(t, repo, "w1")
	commitIn(t, worker, "feature.txt")
	var seeded []string
	c := renamedCoordinator(t, []state.Session{{Name: "lead", Worktree: lead}, {Name: "w2", Worktree: worker, Parent: "lead"}}, &fakeCreator{}, &seeded)

	if _, err := c.Merge(context.Background(), contracts.MergeRequest{FromSession: "lead", Worker: "w2"}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(lead, "feature.txt")); err != nil {
		t.Fatalf("the lead did not get the work of the renamed worker: %v (seeded %q)", err, seeded)
	}
}

func TestAReportFromARenamedWorkerNamesItsRealBranch(t *testing.T) {
	repo := tempRepo(t)
	lead := sessionWorktree(t, repo, "lead")
	worker := sessionWorktree(t, repo, "w1")
	var seeded []string
	c := renamedCoordinator(t, []state.Session{{Name: "lead", Worktree: lead}, {Name: "w2", Worktree: worker, Parent: "lead"}}, &fakeCreator{}, &seeded)

	if _, err := c.Report(context.Background(), contracts.ReportRequest{FromSession: "w2", Summary: "done"}); err != nil {
		t.Fatal(err)
	}
	if len(seeded) != 1 || !strings.Contains(seeded[0], "session/inst/w1") {
		t.Fatalf("report %q, want it to name session/inst/w1", seeded)
	}
}
