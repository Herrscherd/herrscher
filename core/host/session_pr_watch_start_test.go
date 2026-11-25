package host

import (
	"log/slog"
	"strings"
	"testing"
	"time"

	contracts "github.com/Herrscherd/herrscher-contracts"
)

func TestWatchThatCannotStartRefusesTheSessionWithoutAGroup(t *testing.T) {
	dir := prFixtureRepo(t)
	st := prState(t, dir, "")
	openPRSession(t, st, "beta", dir, prTestNumber)
	client := &countingPRForge{fakePRForge: fakePRForge{pullRequest: contracts.PullRequest{State: contracts.PullRequestOpen, Revision: "r1"}}}
	w := newPRWatchTestWatcher(t, st, client, &prWatchTestClock{now: time.Unix(0, 0)})
	w.limit = nil
	logs := &lockedBuffer{}
	saved := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(logs, nil)))
	t.Cleanup(func() { slog.SetDefault(saved) })
	ctx := prWatchTestContext(t)

	w.scan(ctx)

	waitForPRWatch(t, "beta to be refused", func() bool {
		_, ok := betaRefusal(w)
		return ok
	})
	if _, ok := followOf(w, "beta"); ok {
		t.Fatal("a session whose watch cannot start is still followed")
	}
	if groups := groupCount(w); groups != 0 {
		t.Fatalf("groups = %d, want 0 when the watch cannot start", groups)
	}
	if !strings.Contains(logs.String(), "session pull request watch not started") {
		t.Fatalf("the failed start is not logged, logs = %q", logs.String())
	}
}
