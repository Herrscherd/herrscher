package host

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	contracts "github.com/Herrscherd/herrscher-contracts"

	"github.com/Herrscherd/herrscher/core/internal/state"
)

func TestPRStatusReadsTheRecordedPullRequest(t *testing.T) {
	dir := prFixtureRepo(t)
	st := prState(t, dir, "")
	if err := st.RecordPullRequest("alpha", state.PullRequestRecord{Number: 42, State: contracts.PullRequestOpen}); err != nil {
		t.Fatal(err)
	}
	reg := prRegistry(t, st, prForgeOpener(&fakePRForge{pullRequest: prNumbered(t)}))

	out, err := runPR(t, reg, verbPRStatus, "--name", "alpha")
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	if !strings.Contains(out, "pull request #42 (open") || !strings.Contains(out, "feat -> master") {
		t.Fatalf("status output %q lacks the number, state or branches", out)
	}
}

type orderedPRForge struct {
	contracts.Forge

	template contracts.PullRequest
	states   []contracts.PullRequestState
	entered  chan struct{}
	release  chan struct{}
	mu       sync.Mutex
	reads    int
}

func (f *orderedPRForge) PullRequest(_ context.Context, ref contracts.PullRequestRef, _ contracts.Revision) (contracts.PullRequest, error) {
	f.mu.Lock()
	index := f.reads
	f.reads++
	f.mu.Unlock()
	if index == 0 {
		f.entered <- struct{}{}
		<-f.release
	}
	pullRequest := f.template
	pullRequest.Ref = ref
	pullRequest.State = f.states[index]
	return pullRequest, nil
}

func TestPRStatusKeepsTheLastReadRecordedWhenReadsRace(t *testing.T) {
	dir := prFixtureRepo(t)
	st := prState(t, dir, "")
	if err := st.RecordPullRequest("alpha", state.PullRequestRecord{Number: prTestNumber, State: contracts.PullRequestOpen}); err != nil {
		t.Fatal(err)
	}
	remote := &orderedPRForge{
		template: prNumbered(t),
		states:   []contracts.PullRequestState{contracts.PullRequestOpen, contracts.PullRequestMerged},
		entered:  make(chan struct{}, 1),
		release:  make(chan struct{}),
	}
	reg := prRegistry(t, st, prForgeOpener(remote))

	first := make(chan error, 1)
	go func() {
		_, err := runPR(t, reg, verbPRStatus, "--name", "alpha")
		first <- err
	}()
	<-remote.entered

	second := make(chan error, 1)
	go func() {
		_, err := runPR(t, reg, verbPRStatus, "--name", "alpha")
		second <- err
	}()
	time.Sleep(50 * time.Millisecond)
	close(remote.release)

	if err := <-first; err != nil {
		t.Fatalf("first status: %v", err)
	}
	if err := <-second; err != nil {
		t.Fatalf("second status: %v", err)
	}
	got, _ := st.FindSession("alpha")
	if got.PullRequestState != contracts.PullRequestMerged {
		t.Fatalf("recorded state = %v after reads open then merged, want merged", got.PullRequestState)
	}
}

func TestPRStatusStillReadsThePullRequestWhenTheStateCannotBeSaved(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("a read-only directory does not stop root from writing")
	}
	dir := prFixtureRepo(t)
	stateDir := filepath.Join(t.TempDir(), "state")
	if err := os.Mkdir(stateDir, 0o700); err != nil {
		t.Fatal(err)
	}
	st, err := state.LoadState(filepath.Join(stateDir, "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err = st.AddSession(state.Session{Name: "alpha", ChannelID: "chan-alpha", Dir: dir}); err != nil {
		t.Fatal(err)
	}
	if err = st.RecordPullRequest("alpha", state.PullRequestRecord{Number: prTestNumber, State: contracts.PullRequestOpen}); err != nil {
		t.Fatal(err)
	}
	merged := prNumbered(t)
	merged.State = contracts.PullRequestMerged
	if err = os.Chmod(stateDir, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if restoreErr := os.Chmod(stateDir, 0o700); restoreErr != nil {
			t.Error(restoreErr)
		}
	})
	reg := prRegistry(t, st, prForgeOpener(&fakePRForge{pullRequest: merged}))

	out, err := runPR(t, reg, verbPRStatus, "--name", "alpha")
	if err != nil {
		t.Fatalf("status failed although the forge was read: %v", err)
	}
	if !strings.Contains(out, "pull request #42 (merged") {
		t.Fatalf("status output %q does not show the state read from the forge", out)
	}
	if !strings.Contains(out, "could not be saved") {
		t.Fatalf("status output %q does not say the state was not saved", out)
	}
}

func TestRefreshLockGivesUpTheWaitWhenItsContextEnds(t *testing.T) {
	locks := newRefreshLocks()
	release, err := locks.acquire(t.Context(), "alpha")
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := locks.acquire(ctx, "alpha"); !errors.Is(err, context.Canceled) {
		t.Fatalf("acquire behind a held refresh with a cancelled context = %v, want context.Canceled", err)
	}
}

func TestRefreshLockIsAvailableAgainOnceReleased(t *testing.T) {
	locks := newRefreshLocks()
	release, err := locks.acquire(t.Context(), "alpha")
	if err != nil {
		t.Fatal(err)
	}
	release()
	again, err := locks.acquire(t.Context(), "alpha")
	if err != nil {
		t.Fatalf("acquire after release = %v, want the lock free", err)
	}
	again()
}

func TestPRStatusJSONNamesAnUnknownStateAsUnknown(t *testing.T) {
	dir := prFixtureRepo(t)
	st := prState(t, dir, "")
	if err := st.RecordPullRequest("alpha", state.PullRequestRecord{Number: prTestNumber, State: contracts.PullRequestOpen}); err != nil {
		t.Fatal(err)
	}
	pullRequest := prNumbered(t)
	pullRequest.State = contracts.PullRequestState(99)
	reg := prRegistry(t, st, prForgeOpener(&fakePRForge{pullRequest: pullRequest}))

	out, err := runPR(t, reg, verbPRStatus, "--name", "alpha", "--json")
	if err != nil {
		t.Fatalf("status --json: %v", err)
	}
	if !strings.Contains(out, `"state":"unknown"`) {
		t.Fatalf("status --json %q lacks state unknown for a state no forge reports", out)
	}
}
