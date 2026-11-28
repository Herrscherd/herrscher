package host

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/Herrscherd/herrscher/core/config"
	"github.com/Herrscherd/herrscher/core/internal/issuetrigger"
)

type fakeIssuePoller struct {
	mu    sync.Mutex
	calls []string
	fail  int
}

func (f *fakeIssuePoller) Poll(_ context.Context, target issuetrigger.Target) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, target.Repo.String())
	if f.fail > 0 {
		f.fail--
		return errors.New("forge down")
	}
	return nil
}

func (f *fakeIssuePoller) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.calls)
}

func movingScheduler(poller issuePoller, targets []issuetrigger.Target, clock *time.Time) *scheduler {
	store := &fakeScheduleStore{}
	var seeded []string
	return newScheduler(store, &fakeScheduleSessions{}, &fakeScheduleCreator{}, func(sess, task string) bool {
		seeded = append(seeded, sess)
		return true
	}, func() time.Time { return *clock }, schedulerTick).withIssues(poller, targets)
}

func oneRepoTargets(t *testing.T, period time.Duration) []issuetrigger.Target {
	t.Helper()
	targets, err := issueTriggerTargets(map[string]config.IssueTriggerConfig{
		"github.com/Herrscherd/herrscher": {Labels: []string{"ready"}, Period: period.String()},
	})
	if err != nil {
		t.Fatal(err)
	}
	return targets
}

func TestTickWithoutIssueTriggersPollsNothing(t *testing.T) {
	poller := &fakeIssuePoller{}
	clock := mkHostTime(t, "2026-08-25 09:00")
	sch := movingScheduler(poller, nil, &clock)
	sch.tick(context.Background())
	sch.wait()
	if poller.count() != 0 {
		t.Fatalf("polls = %d, want none when no repository configures a trigger", poller.count())
	}
}

func TestIssueTriggersAreOffUnlessConfigured(t *testing.T) {
	targets, err := issueTriggerTargets(nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(targets) != 0 {
		t.Fatalf("targets = %v, want none by default", targets)
	}
}

func TestTickReadsARepositoryOnlyWhenItsPeriodPassed(t *testing.T) {
	poller := &fakeIssuePoller{}
	clock := mkHostTime(t, "2026-08-25 09:00")
	sch := movingScheduler(poller, oneRepoTargets(t, 10*time.Minute), &clock)
	ctx := context.Background()
	sch.tick(ctx)
	sch.wait()
	clock = clock.Add(time.Minute)
	sch.tick(ctx)
	sch.wait()
	if poller.count() != 1 {
		t.Fatalf("polls inside the period = %d, want 1", poller.count())
	}
	clock = clock.Add(9 * time.Minute)
	sch.tick(ctx)
	sch.wait()
	if poller.count() != 2 {
		t.Fatalf("polls after the period = %d, want 2", poller.count())
	}
}

func TestAFailedReadIsRetriedOnTheNextTick(t *testing.T) {
	poller := &fakeIssuePoller{fail: 1}
	clock := mkHostTime(t, "2026-08-25 09:00")
	sch := movingScheduler(poller, oneRepoTargets(t, 10*time.Minute), &clock)
	ctx := context.Background()
	sch.tick(ctx)
	sch.wait()
	clock = clock.Add(time.Minute)
	sch.tick(ctx)
	sch.wait()
	if poller.count() != 2 {
		t.Fatalf("polls = %d, want the failed read retried one minute later", poller.count())
	}
}

func TestIssueTargetsFollowTheConfiguredTrigger(t *testing.T) {
	targets, err := issueTriggerTargets(map[string]config.IssueTriggerConfig{
		"github.com/Herrscherd/b": {Mentions: []string{" alice "}, Agent: "builder"},
		"github.com/Herrscherd/a": {Labels: []string{" ready "}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(targets) != 2 || targets[0].Repo.String() != "github.com/Herrscherd/a" {
		t.Fatalf("targets = %+v, want the repositories sorted", targets)
	}
	if targets[0].Period != config.DefaultIssuePeriod || len(targets[0].Filters) != 1 {
		t.Fatalf("first target = %+v, want the default period and one filter", targets[0])
	}
	if targets[1].Agent != "builder" || targets[1].Filters[0].Key() != "mention:alice" {
		t.Fatalf("second target = %+v, want the mention filter and its agent", targets[1])
	}
}
