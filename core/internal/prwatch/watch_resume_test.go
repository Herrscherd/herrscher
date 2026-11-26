package prwatch

import (
	"errors"
	"strings"
	"testing"
	"time"

	contracts "github.com/Herrscherd/herrscher-contracts"
)

func TestRateLimitWithAPastResetStillWaits(t *testing.T) {
	clock := newFakeClock()
	past := clock.Now().Add(-time.Second)
	replies := make([]reply, 20)
	for index := range replies {
		replies[index] = reply{err: &contracts.ForgeRateLimitedError{Reset: past}}
	}
	reader := newFakeReader(clock, replies...)
	store := &fakeStore{}
	stop, _ := startWatch(t, Watch{Ref: testRef(t), Reader: reader, Save: store.save, Limit: &Limit{}, Clock: clock, Policy: testPolicy(time.Minute)})

	for range replies {
		reader.awaitRead(t)
		if d := clock.tick(t); d <= 0 {
			t.Fatalf("wait after a limited read with a past reset = %v, want a positive wait", d)
		}
	}
	stop()
}

func TestIdenticalReadIsNotSavedTwice(t *testing.T) {
	clock := newFakeClock()
	reader := newFakeReader(clock, reply{pull: open("r1")}, reply{pull: open("r1")})
	store := &fakeStore{}
	stop, _ := startWatch(t, Watch{Ref: testRef(t), Reader: reader, Save: store.save, Limit: &Limit{}, Clock: clock, Policy: testPolicy(time.Minute)})

	reader.awaitRead(t)
	clock.tick(t)
	reader.awaitRead(t)
	clock.awaitArm(t)
	if got := len(store.persisted()); got != 1 {
		t.Fatalf("persisted = %d, want 1: an identical read is not saved again", got)
	}
	stop()
}

func TestRevisionAloneChangeIsSaved(t *testing.T) {
	clock := newFakeClock()
	reader := newFakeReader(clock, reply{pull: open("r1")}, reply{pull: open("r2")})
	store := &fakeStore{}
	stop, _ := startWatch(t, Watch{Ref: testRef(t), Reader: reader, Save: store.save, Limit: &Limit{}, Clock: clock, Policy: testPolicy(time.Minute)})

	reader.awaitRead(t)
	clock.tick(t)
	reader.awaitRead(t)
	clock.awaitArm(t)
	persisted := store.persisted()
	if len(persisted) != 2 {
		t.Fatalf("persisted = %d, want 2: a new revision with the same state is saved", len(persisted))
	}
	if last := persisted[1].Revision; last != "r2" {
		t.Fatalf("last persisted revision = %q, want r2", last)
	}
	stop()
}

func TestFieldChangingAloneIsSaved(t *testing.T) {
	head := mustCommitSHA(t, strings.Repeat("a", 40))
	pushed := mustCommitSHA(t, strings.Repeat("b", 40))
	base := contracts.PullRequest{Revision: "r1", State: contracts.PullRequestOpen, URL: "u", HeadSHA: head, Merge: contracts.MergeStateClean, Checks: contracts.ChecksPassed, LatestReview: "v1"}
	changes := map[string]func(*contracts.PullRequest){
		"head sha":      func(pull *contracts.PullRequest) { pull.HeadSHA = pushed },
		"merge":         func(pull *contracts.PullRequest) { pull.Merge = contracts.MergeStateBlocked },
		"checks":        func(pull *contracts.PullRequest) { pull.Checks = contracts.ChecksFailed },
		"latest review": func(pull *contracts.PullRequest) { pull.LatestReview = "v2" },
	}
	for name, change := range changes {
		t.Run(name, func(t *testing.T) {
			clock := newFakeClock()
			changed := base
			change(&changed)
			reader := newFakeReader(clock, reply{pull: base}, reply{pull: changed})
			store := &fakeStore{}
			stop, _ := startWatch(t, Watch{Ref: testRef(t), Reader: reader, Save: store.save, Limit: &Limit{}, Clock: clock, Policy: testPolicy(time.Minute)})

			reader.awaitRead(t)
			clock.tick(t)
			reader.awaitRead(t)
			clock.awaitArm(t)
			if got := len(store.persisted()); got != 2 {
				t.Fatalf("persisted = %d, want 2: a change of the %s alone is saved", got, name)
			}
			stop()
		})
	}
}

func mustCommitSHA(t *testing.T, text string) contracts.CommitSHA {
	t.Helper()
	sha, err := contracts.ParseCommitSHA(text)
	if err != nil {
		t.Fatal(err)
	}
	return sha
}

func TestPersistedRevisionMakesTheFirstReadConditional(t *testing.T) {
	clock := newFakeClock()
	reader := newFakeReader(clock, reply{err: contracts.ErrForgeNotModified})
	store := &fakeStore{}
	stop, _ := startWatch(t, Watch{Ref: testRef(t), Since: "r9", Reader: reader, Save: store.save, Limit: &Limit{}, Clock: clock, Policy: testPolicy(time.Minute)})

	if first := reader.awaitRead(t); first.since != "r9" {
		t.Fatalf("first read since = %q, want the revision persisted before the restart", first.since)
	}
	stop()
}

func TestNotModifiedWithoutAStoredStateReadsInFull(t *testing.T) {
	clock := newFakeClock()
	reader := newFakeReader(clock, reply{err: contracts.ErrForgeNotModified}, reply{pull: open("r2")})
	store := &fakeStore{}
	stop, _ := startWatch(t, Watch{Ref: testRef(t), Since: "r1", Reader: reader, Save: store.save, Limit: &Limit{}, Clock: clock, Policy: testPolicy(time.Minute)})

	reader.awaitRead(t)
	if second := reader.awaitRead(t); second.since != "" {
		t.Fatalf("read after a 304 with nothing stored since = %q, want the whole request", second.since)
	}
	stop()
}

func TestFirstReadWaitsForTheStagger(t *testing.T) {
	clock := newFakeClock()
	start := clock.Now()
	reader := newFakeReader(clock, reply{pull: open("r1")})
	store := &fakeStore{}
	watch := Watch{Ref: testRef(t), Stagger: 20 * time.Second, Reader: reader, Save: store.save, Limit: &Limit{}, Clock: clock, Policy: testPolicy(time.Minute)}
	stop, _ := startWatch(t, watch)

	if d := clock.tick(t); d != 20*time.Second {
		t.Fatalf("wait before the first read = %v, want the stagger", d)
	}
	if call := reader.awaitRead(t); !call.at.Equal(start.Add(20 * time.Second)) {
		t.Fatalf("first read at %v, want the end of the stagger", call.at)
	}
	stop()
}

func TestWatchWithoutALimitIsRefused(t *testing.T) {
	clock := newFakeClock()
	reader := newFakeReader(clock)
	store := &fakeStore{}
	_, err := NewWatch(Watch{Ref: testRef(t), Reader: reader, Save: store.save, Clock: clock, Policy: testPolicy(time.Minute)})
	if !errors.Is(err, ErrLimitRequired) {
		t.Fatalf("NewWatch without a limit: err = %v, want ErrLimitRequired", err)
	}
}

func TestWatchWithALimitIsAccepted(t *testing.T) {
	clock := newFakeClock()
	reader := newFakeReader(clock)
	store := &fakeStore{}
	if _, err := NewWatch(Watch{Ref: testRef(t), Reader: reader, Save: store.save, Limit: &Limit{}, Clock: clock, Policy: testPolicy(time.Minute)}); err != nil {
		t.Fatalf("NewWatch with a limit: err = %v, want nil", err)
	}
}
