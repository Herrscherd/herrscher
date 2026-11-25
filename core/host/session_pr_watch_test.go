package host

import (
	"context"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	contracts "github.com/Herrscherd/herrscher-contracts"
	"github.com/Herrscherd/herrscher-contracts/i18n"

	"github.com/Herrscherd/herrscher/core/clitext"
	"github.com/Herrscherd/herrscher/core/internal/state"
)

const prWatchTestTimeout = 5 * time.Second

type prWatchTestClock struct {
	mu  sync.Mutex
	now time.Time
}

func (c *prWatchTestClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *prWatchTestClock) Advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(d)
}

func (c *prWatchTestClock) After(d time.Duration) <-chan time.Time {
	fired := make(chan time.Time, 1)
	if d <= 0 {
		fired <- c.Now()
	}
	return fired
}

type countingPRForge struct {
	fakePRForge

	reads atomic.Int32
}

func (f *countingPRForge) PullRequest(ctx context.Context, ref contracts.PullRequestRef, since contracts.Revision) (contracts.PullRequest, error) {
	f.reads.Add(1)
	return f.fakePRForge.PullRequest(ctx, ref, since)
}

func newPRWatchTestWatcher(t *testing.T, st *state.State, client contracts.Forge, clock *prWatchTestClock) *prWatcher {
	t.Helper()
	w := newSessionPRWatcher(clitext.NewMessages(), fixedLanguage(i18n.English), st, prForgeOpener(client), nil)
	w.clock = clock
	w.stagger = func() time.Duration { return 0 }
	t.Cleanup(func() { w.watches.Wait() })
	return w
}

func prWatchTestContext(t *testing.T) context.Context {
	t.Helper()
	ctx, cancel := context.WithCancel(t.Context())
	t.Cleanup(cancel)
	return ctx
}

func openPRSession(t *testing.T, st *state.State, name, dir string, number int) {
	t.Helper()
	sess := state.Session{Name: name, ChannelID: "chan-" + name, Dir: dir, PullRequest: number, PullRequestState: contracts.PullRequestOpen}
	if err := st.AddSession(sess); err != nil {
		t.Fatal(err)
	}
}

func followOf(w *prWatcher, name string) (*sessionFollow, bool) {
	w.mu.Lock()
	defer w.mu.Unlock()
	follow, ok := w.follows[name]
	return follow, ok
}

func betaRefusal(w *prWatcher) (prRefusal, bool) {
	w.mu.Lock()
	defer w.mu.Unlock()
	refusal, ok := w.refusals["beta"]
	if !ok {
		return prRefusal{}, false
	}
	return *refusal, true
}

func groupCount(w *prWatcher) int {
	w.mu.Lock()
	defer w.mu.Unlock()
	return len(w.groups)
}

func persistedRevision(st *state.State, name string) contracts.Revision {
	sess, _ := st.FindSession(name)
	return sess.PullRequestRevision
}

func waitForPRWatch(t *testing.T, what string, condition func() bool) {
	t.Helper()
	deadline := time.Now().Add(prWatchTestTimeout)
	for !condition() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(time.Millisecond)
	}
}

func TestScanWatchesOnlyOpenSessionsWithAPullRequest(t *testing.T) {
	dir := prFixtureRepo(t)
	st := prState(t, dir, "")
	openPRSession(t, st, "beta", dir, prTestNumber)
	if err := st.AddSession(state.Session{Name: "gamma", ChannelID: "chan-gamma", Dir: dir, PullRequest: 43, PullRequestState: contracts.PullRequestMerged}); err != nil {
		t.Fatal(err)
	}
	if err := st.AddSession(state.Session{Name: "delta", ChannelID: "chan-delta", Dir: dir, PullRequest: 44, PullRequestState: contracts.PullRequestClosed}); err != nil {
		t.Fatal(err)
	}
	client := &countingPRForge{fakePRForge: fakePRForge{pullRequest: contracts.PullRequest{State: contracts.PullRequestOpen, Revision: "r1"}}}
	w := newPRWatchTestWatcher(t, st, client, &prWatchTestClock{now: time.Unix(0, 0)})
	ctx := prWatchTestContext(t)

	w.scan(ctx)

	wanted := w.wanted()
	if len(wanted) != 1 || wanted["beta"] != prTestNumber {
		t.Fatalf("wanted = %v, want only beta on PR %d", wanted, prTestNumber)
	}
	waitForPRWatch(t, "beta to be followed", func() bool {
		_, ok := followOf(w, "beta")
		return ok
	})
	if _, ok := followOf(w, "gamma"); ok {
		t.Fatal("an ended pull request was followed")
	}
}

func TestRemovedSessionStopsItsWatch(t *testing.T) {
	dir := prFixtureRepo(t)
	st := prState(t, dir, "")
	openPRSession(t, st, "beta", dir, prTestNumber)
	client := &countingPRForge{fakePRForge: fakePRForge{pullRequest: contracts.PullRequest{State: contracts.PullRequestOpen, Revision: "r1"}}}
	w := newPRWatchTestWatcher(t, st, client, &prWatchTestClock{now: time.Unix(0, 0)})
	ctx := prWatchTestContext(t)
	w.scan(ctx)
	waitForPRWatch(t, "beta to be followed", func() bool {
		_, ok := followOf(w, "beta")
		return ok
	})
	follow, _ := followOf(w, "beta")

	if err := st.RemoveSession("beta"); err != nil {
		t.Fatal(err)
	}
	w.scan(ctx)

	select {
	case <-follow.done:
	case <-time.After(prWatchTestTimeout):
		t.Fatal("the watch of a removed session did not stop")
	}
	if _, ok := followOf(w, "beta"); ok {
		t.Fatal("the watch of a removed session was not forgotten")
	}
}

func TestStartingTwiceKeepsOneWatchPerSession(t *testing.T) {
	dir := prFixtureRepo(t)
	st := prState(t, dir, "")
	openPRSession(t, st, "beta", dir, prTestNumber)
	client := &countingPRForge{fakePRForge: fakePRForge{pullRequest: contracts.PullRequest{State: contracts.PullRequestOpen, Revision: "r1"}}}
	w := newPRWatchTestWatcher(t, st, client, &prWatchTestClock{now: time.Unix(0, 0)})
	ctx := prWatchTestContext(t)
	w.scan(ctx)
	waitForPRWatch(t, "beta to be followed", func() bool {
		_, ok := followOf(w, "beta")
		return ok
	})
	first, _ := followOf(w, "beta")
	waitForPRWatch(t, "beta to join its pull request", func() bool {
		return groupCount(w) == 1
	})

	w.scan(ctx)

	second, ok := followOf(w, "beta")
	if !ok || second != first {
		t.Fatal("a second scan started another watch for the same session")
	}
	if groups := groupCount(w); groups != 1 {
		t.Fatalf("groups = %d, want 1", groups)
	}
}

func TestChangedPullRequestNumberRestartsTheWatchAfterTheOldOneEnds(t *testing.T) {
	dir := prFixtureRepo(t)
	st := prState(t, dir, "")
	openPRSession(t, st, "beta", dir, prTestNumber)
	client := &countingPRForge{fakePRForge: fakePRForge{pullRequest: contracts.PullRequest{State: contracts.PullRequestOpen, Revision: "r1"}}}
	w := newPRWatchTestWatcher(t, st, client, &prWatchTestClock{now: time.Unix(0, 0)})
	ctx := prWatchTestContext(t)
	w.scan(ctx)
	waitForPRWatch(t, "beta to be followed", func() bool {
		_, ok := followOf(w, "beta")
		return ok
	})
	old, _ := followOf(w, "beta")

	if err := st.RemoveSession("beta"); err != nil {
		t.Fatal(err)
	}
	openPRSession(t, st, "beta", dir, prTestNumber+1)
	w.scan(ctx)

	select {
	case <-old.done:
	case <-time.After(prWatchTestTimeout):
		t.Fatal("the watch on the previous number did not end before its replacement")
	}
	waitForPRWatch(t, "the replacement watch", func() bool {
		follow, ok := followOf(w, "beta")
		return ok && follow != old && follow.number == prTestNumber+1
	})
}

func TestPullRequestStateIsPersistedThroughRefresh(t *testing.T) {
	dir := prFixtureRepo(t)
	st := prState(t, dir, "")
	openPRSession(t, st, "beta", dir, prTestNumber)
	client := &countingPRForge{fakePRForge: fakePRForge{pullRequest: contracts.PullRequest{State: contracts.PullRequestMerged, Draft: false, Revision: "r7"}}}
	w := newPRWatchTestWatcher(t, st, client, &prWatchTestClock{now: time.Unix(0, 0)})
	ctx := prWatchTestContext(t)

	w.scan(ctx)

	waitForPRWatch(t, "the merged state to be persisted", func() bool {
		sess, _ := st.FindSession("beta")
		return sess.PullRequestState == contracts.PullRequestMerged && sess.PullRequestRevision == "r7"
	})
}

func TestRefusedTargetIsRetriedAtTheNextScanWithAGrowingCooldown(t *testing.T) {
	st := prState(t, t.TempDir(), "")
	missing := filepath.Join(t.TempDir(), "missing")
	openPRSession(t, st, "beta", missing, prTestNumber)
	clock := &prWatchTestClock{now: time.Unix(0, 0)}
	client := &countingPRForge{}
	w := newPRWatchTestWatcher(t, st, client, clock)
	ctx := prWatchTestContext(t)

	w.scan(ctx)
	waitForPRWatch(t, "the first refusal", func() bool {
		_, ok := betaRefusal(w)
		return ok
	})
	waitForPRWatch(t, "the refused watch to be forgotten", func() bool {
		_, ok := followOf(w, "beta")
		return !ok
	})
	first, _ := betaRefusal(w)
	firstCooldown := first.until.Sub(clock.Now())

	w.scan(ctx)
	if _, ok := followOf(w, "beta"); ok {
		t.Fatal("a refused target was tried again inside its cooldown")
	}

	clock.Advance(prWatchScan)
	w.scan(ctx)
	waitForPRWatch(t, "the second refusal", func() bool {
		refusal, ok := betaRefusal(w)
		return ok && refusal.until.After(first.until)
	})
	second, _ := betaRefusal(w)
	secondCooldown := second.until.Sub(clock.Now())
	if secondCooldown <= firstCooldown {
		t.Fatalf("cooldown = %v after a refusal of %v, want it to grow", secondCooldown, firstCooldown)
	}
}

func TestMemberJoiningAReadGroupGetsTheCurrentStateWithoutANewRead(t *testing.T) {
	dir := prFixtureRepo(t)
	st := prState(t, dir, "")
	openPRSession(t, st, "beta", dir, prTestNumber)
	client := &countingPRForge{fakePRForge: fakePRForge{pullRequest: contracts.PullRequest{State: contracts.PullRequestOpen, Revision: "r1"}}}
	w := newPRWatchTestWatcher(t, st, client, &prWatchTestClock{now: time.Unix(0, 0)})
	ctx := prWatchTestContext(t)
	w.scan(ctx)
	waitForPRWatch(t, "beta to be read and saved", func() bool {
		return persistedRevision(st, "beta") == "r1"
	})

	openPRSession(t, st, "gamma", dir, prTestNumber)
	w.scan(ctx)

	waitForPRWatch(t, "gamma to receive the current state", func() bool {
		return persistedRevision(st, "gamma") == "r1"
	})
	if reads := client.reads.Load(); reads != 1 {
		t.Fatalf("reads = %d, want one read shared by both sessions", reads)
	}
	if groups := groupCount(w); groups != 1 {
		t.Fatalf("groups = %d, want one group for the pull request", groups)
	}
}

func TestRunReturnsOnlyAfterEveryWatchEnds(t *testing.T) {
	dir := prFixtureRepo(t)
	st := prState(t, dir, "")
	openPRSession(t, st, "beta", dir, prTestNumber)
	client := &countingPRForge{fakePRForge: fakePRForge{pullRequest: contracts.PullRequest{State: contracts.PullRequestOpen, Revision: "r1"}}}
	w := newPRWatchTestWatcher(t, st, client, &prWatchTestClock{now: time.Unix(0, 0)})
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	returned := make(chan struct{})
	go func() {
		w.Run(ctx)
		close(returned)
	}()
	waitForPRWatch(t, "beta to be followed", func() bool {
		_, ok := followOf(w, "beta")
		return ok
	})

	cancel()

	select {
	case <-returned:
	case <-time.After(prWatchTestTimeout):
		t.Fatal("Run did not return after its context ended")
	}
	if _, ok := followOf(w, "beta"); ok {
		t.Fatal("a watch outlived Run")
	}
	if groups := groupCount(w); groups != 0 {
		t.Fatalf("groups = %d after Run returned, want 0", groups)
	}
}
