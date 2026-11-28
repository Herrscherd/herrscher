package host

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	contracts "github.com/Herrscherd/herrscher-contracts"

	"github.com/Herrscherd/herrscher/core/internal/state"
)

func TestAnUnwritableStateSendsNothing(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root ignores directory permissions")
	}
	dir := t.TempDir()
	st, err := state.LoadState(filepath.Join(dir, "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := st.AddSession(state.Session{Name: "alpha", PullRequest: prTestNumber, PullRequestState: contracts.PullRequestOpen}); err != nil {
		t.Fatal(err)
	}
	h := newCIHarnessOn(t, st)
	h.failingWithJob()
	h.record(h.pull("1", contracts.ChecksFailed))
	lockDir(t, dir)
	for _, head := range []string{"1", "2", "3", "4", "5", "6"} {
		h.loop.observe(h.t.Context(), "alpha", h.forge, h.pull(head, contracts.ChecksFailed))
	}

	if len(h.seeds.tasks) != 0 || h.nudge().Attempts != 0 {
		t.Fatalf("messages %d, attempts %d, want none", len(h.seeds.tasks), h.nudge().Attempts)
	}
}

func lockDir(t *testing.T, dir string) {
	t.Helper()
	if err := os.Chmod(dir, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.Chmod(dir, 0o700); err != nil {
			t.Error(err)
		}
	})
}

func TestAFailedRefundTellsAHumanAndStopsTheLoop(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root ignores directory permissions")
	}
	dir := t.TempDir()
	st, err := state.LoadState(filepath.Join(dir, "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := st.AddSession(state.Session{Name: "alpha", PullRequest: prTestNumber, PullRequestState: contracts.PullRequestOpen}); err != nil {
		t.Fatal(err)
	}
	h := newCIHarnessOn(t, st)
	h.failingWithJob()
	h.record(h.pull("1", contracts.ChecksFailed))
	h.loop.seed = func(_, task string) bool {
		h.seeds.tasks = append(h.seeds.tasks, task)
		lockDir(t, dir)
		return false
	}
	h.loop.observe(h.t.Context(), "alpha", h.forge, h.pull("1", contracts.ChecksFailed))

	if len(h.notices) != 1 || h.notices[0].Code != contracts.CodeUnexpected {
		t.Fatalf("notices = %+v, want one unexpected notice to a human", h.notices)
	}
	if got := h.nudge(); got.Attempts != 1 || got.Head != commitOf(t, "1").String() {
		t.Fatalf("record = %+v, want the spent attempt kept on head 1", got)
	}
	h.loop.observe(h.t.Context(), "alpha", h.forge, h.pull("1", contracts.ChecksFailed))
	if len(h.seeds.tasks) != 1 {
		t.Fatalf("messages = %d after a failed refund, want the loop to stop at 1", len(h.seeds.tasks))
	}
}

func TestTheAttemptIsRecordedBeforeTheMessageLeaves(t *testing.T) {
	h := newCIHarness(t)
	h.failingWithJob()
	var during state.CINudge
	h.loop.seed = func(_, _ string) bool {
		during = h.nudge()
		return true
	}
	h.observe(h.pull("1", contracts.ChecksFailed))

	if during.Attempts != 1 || during.Head != commitOf(h.t, "1").String() {
		t.Fatalf("record while the message is sent = %+v, want attempt 1 on head 1", during)
	}
}

func TestAFailureBeyondTheCheckPagesIsAnnouncedOnceAndNotReread(t *testing.T) {
	h := newCIHarness(t)
	pages := make([][]contracts.Check, ciChecksPageLimit+1)
	for i := range ciChecksPageLimit {
		pages[i] = []contracts.Check{{Name: "lint", Status: contracts.CheckCompleted, Conclusion: contracts.ConclusionSuccess}}
	}
	pages[ciChecksPageLimit] = []contracts.Check{{Name: "unit", Status: contracts.CheckCompleted, Conclusion: contracts.ConclusionFailure}}
	h.forge.checkPages = pages
	pull := h.pull("1", contracts.ChecksFailed)
	h.observe(pull)
	h.observe(pull)

	if len(h.seeds.tasks) != 1 || !strings.Contains(h.seeds.tasks[0], "cannot locate") {
		t.Fatalf("messages = %q, want one unlocated notice", h.seeds.tasks)
	}
	if h.forge.checkCalls != ciChecksPageLimit {
		t.Fatalf("check pages read = %d, want %d", h.forge.checkCalls, ciChecksPageLimit)
	}
}

func TestTheLoopOnlyReadsTheForge(t *testing.T) {
	h := newCIHarness(t)
	h.failingWithJob()
	for _, head := range []string{"1", "2", "3", "4"} {
		h.observe(h.pull(head, contracts.ChecksFailed))
	}

	if h.forge.writes != 0 {
		t.Fatalf("forge writes = %d, want none", h.forge.writes)
	}
	if len(h.seeds.tasks) != 4 {
		t.Fatalf("messages = %d, want 3 nudges and 1 stop", len(h.seeds.tasks))
	}
}
