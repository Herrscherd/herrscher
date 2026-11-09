package usage

import (
	"context"
	"errors"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	contracts "github.com/Herrscherd/herrscher-contracts"
)

type fakeReader struct {
	calls  int
	limits []contracts.Limit
	err    error
}

func (f *fakeReader) read(context.Context, contracts.ProbeAccess) ([]contracts.Limit, error) {
	f.calls++
	return f.limits, f.err
}

func TestLimitsCacheStatuses(t *testing.T) {
	limit := contracts.Limit{Window: contracts.LimitFiveHour, UsedPercent: 40}
	tests := []struct {
		name   string
		err    error
		status LimitsStatus
		limits int
	}{
		{name: "ready", status: LimitsReady, limits: 1},
		{name: "unavailable", err: contracts.ErrLimitsUnavailable, status: LimitsUnavailable},
		{name: "wrapped unavailable", err: fmt.Errorf("oauth: %w", contracts.ErrLimitsUnavailable), status: LimitsUnavailable},
		{name: "failed", err: errors.New("timeout"), status: LimitsFailed},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			reader := &fakeReader{limits: []contracts.Limit{limit}, err: tt.err}
			cache := &LimitsCache{Now: clock(testNow())}
			got := cache.Read(context.Background(), []Target{{Vendor: "claude", Read: reader.read}})
			if len(got) != 1 {
				t.Fatalf("Read returned %d entries, want 1", len(got))
			}
			if got[0].Status != tt.status {
				t.Errorf("Status = %v, want %v", got[0].Status, tt.status)
			}
			if len(got[0].Limits) != tt.limits {
				t.Errorf("Limits = %d, want %d", len(got[0].Limits), tt.limits)
			}
		})
	}
}

func TestLimitsCacheReusesReadingWithinTTL(t *testing.T) {
	now := testNow()
	reader := &fakeReader{limits: []contracts.Limit{{UsedPercent: 10}}}
	cache := &LimitsCache{Now: func() time.Time { return now }}
	target := []Target{{Vendor: "claude", Account: "work", Read: reader.read}}

	cache.Read(context.Background(), target)
	now = testNow().Add(LimitsTTL - time.Second)
	cache.Read(context.Background(), target)
	if reader.calls != 1 {
		t.Fatalf("reads within TTL = %d, want 1", reader.calls)
	}
	now = testNow().Add(LimitsTTL)
	cache.Read(context.Background(), target)
	if reader.calls != 2 {
		t.Fatalf("reads after TTL = %d, want 2", reader.calls)
	}
}

func TestLimitsCacheKeepsTargetOrder(t *testing.T) {
	cache := &LimitsCache{Now: clock(testNow())}
	targets := []Target{
		{Vendor: "claude", Account: "a", Read: (&fakeReader{}).read},
		{Vendor: "codex", Account: "b", Read: (&fakeReader{}).read},
	}
	got := cache.Read(context.Background(), targets)
	if got[0].Vendor != "claude" || got[0].Account != "a" || got[1].Vendor != "codex" || got[1].Account != "b" {
		t.Fatalf("order = %+v", got)
	}
}

func TestLimitsCacheReusesFailureOnlyWithinFailureTTL(t *testing.T) {
	now := testNow()
	reader := &fakeReader{err: errors.New("timeout")}
	cache := &LimitsCache{Now: func() time.Time { return now }}
	target := []Target{{Vendor: "claude", Read: reader.read}}

	cache.Read(context.Background(), target)
	now = testNow().Add(LimitsFailureTTL - time.Second)
	cache.Read(context.Background(), target)
	if reader.calls != 1 {
		t.Fatalf("reads within failure TTL = %d, want 1", reader.calls)
	}
	now = testNow().Add(LimitsFailureTTL)
	cache.Read(context.Background(), target)
	if reader.calls != 2 {
		t.Fatalf("reads after failure TTL = %d, want 2", reader.calls)
	}
}

func TestLimitsCacheKeepsSuccessWithinTTLAfterFailureTTL(t *testing.T) {
	now := testNow()
	reader := &fakeReader{limits: []contracts.Limit{{UsedPercent: 10}}}
	cache := &LimitsCache{Now: func() time.Time { return now }}
	target := []Target{{Vendor: "claude", Read: reader.read}}

	cache.Read(context.Background(), target)
	now = testNow().Add(LimitsFailureTTL)
	cache.Read(context.Background(), target)
	if reader.calls != 1 {
		t.Fatalf("reads of a success after failure TTL = %d, want 1", reader.calls)
	}
}

func TestLimitsCacheKeysEachAccountOfAVendor(t *testing.T) {
	work := &fakeReader{limits: []contracts.Limit{{UsedPercent: 10}}}
	home := &fakeReader{limits: []contracts.Limit{{UsedPercent: 90}}}
	cache := &LimitsCache{Now: clock(testNow())}
	targets := []Target{
		{Vendor: "claude", Account: "work", Read: work.read},
		{Vendor: "claude", Account: "home", Read: home.read},
	}

	first := cache.Read(context.Background(), targets)
	second := cache.Read(context.Background(), targets)
	if first[0].Limits[0].UsedPercent != 10 || first[1].Limits[0].UsedPercent != 90 {
		t.Fatalf("first read mixed the accounts: %+v", first)
	}
	if second[0].Limits[0].UsedPercent != 10 || second[1].Limits[0].UsedPercent != 90 {
		t.Fatalf("cached read mixed the accounts: %+v", second)
	}
	if work.calls != 1 || home.calls != 1 {
		t.Fatalf("reads = %d work, %d home; want one each", work.calls, home.calls)
	}
}

func TestLimitsCacheFailsAReaderThatIgnoresItsContext(t *testing.T) {
	block := make(chan struct{})
	t.Cleanup(func() { close(block) })
	reader := func(context.Context, contracts.ProbeAccess) ([]contracts.Limit, error) {
		<-block
		return []contracts.Limit{{UsedPercent: 10}}, nil
	}
	cache := &LimitsCache{Now: clock(testNow()), Timeout: 20 * time.Millisecond}

	got := readWithDeadline(t, cache, []Target{{Vendor: "claude", Read: reader}})
	if got[0].Status != LimitsFailed || got[0].Limits != nil {
		t.Fatalf("status = %v with limits %v, want failed without limits", got[0].Status, got[0].Limits)
	}
}

func TestLimitsCacheFailsAReaderBlockedUntilItsContextEnds(t *testing.T) {
	reader := func(ctx context.Context, _ contracts.ProbeAccess) ([]contracts.Limit, error) {
		<-ctx.Done()
		return nil, ctx.Err()
	}
	cache := &LimitsCache{Now: clock(testNow()), Timeout: 20 * time.Millisecond}

	got := readWithDeadline(t, cache, []Target{{Vendor: "claude", Read: reader}})
	if got[0].Status != LimitsFailed {
		t.Fatalf("status = %v, want failed", got[0].Status)
	}
}

func readWithDeadline(t *testing.T, cache *LimitsCache, targets []Target) []AccountLimits {
	t.Helper()
	done := make(chan []AccountLimits, 1)
	go func() { done <- cache.Read(context.Background(), targets) }()
	select {
	case got := <-done:
		return got
	case <-time.After(5 * time.Second):
		t.Fatal("Read did not return after its timeout")
		return nil
	}
}

func TestLimitsCacheKeepsNoFailureWhenTheCallerGaveUp(t *testing.T) {
	read := func(context.Context, contracts.ProbeAccess) ([]contracts.Limit, error) {
		return []contracts.Limit{{UsedPercent: 10}}, nil
	}
	cache := &LimitsCache{Now: clock(testNow())}
	target := []Target{{Vendor: "claude", Read: read}}
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	cache.Read(canceled, target)
	got := cache.Read(context.Background(), target)
	if got[0].Status != LimitsReady {
		t.Fatalf("status = %v, want a fresh ready read", got[0].Status)
	}
}

func TestLimitsCacheShareOneReadBetweenConcurrentCallers(t *testing.T) {
	release := make(chan struct{})
	var calls atomic.Int32
	reader := func(context.Context, contracts.ProbeAccess) ([]contracts.Limit, error) {
		calls.Add(1)
		<-release
		return []contracts.Limit{{UsedPercent: 10}}, nil
	}
	cache := &LimitsCache{Now: clock(testNow()), Timeout: 20 * time.Millisecond}
	target := []Target{{Vendor: "claude", Read: reader}}
	readWithDeadline(t, cache, target)
	readWithDeadline(t, cache, target)
	close(release)
	if got := calls.Load(); got != 1 {
		t.Fatalf("reads = %d, want one read left running for both callers", got)
	}
}

func TestLimitsCacheKeepsTheSharedReadWhenItsFirstCallerLeaves(t *testing.T) {
	started, release := make(chan struct{}), make(chan struct{})
	seen := make(chan error, 1)
	reader := func(ctx context.Context, _ contracts.ProbeAccess) ([]contracts.Limit, error) {
		close(started)
		<-release
		seen <- ctx.Err()
		return []contracts.Limit{{UsedPercent: 10}}, ctx.Err()
	}
	cache := &LimitsCache{Now: clock(testNow())}
	caller, leave := context.WithCancel(context.Background())
	left := make(chan struct{})
	go func() {
		defer close(left)
		cache.Read(caller, []Target{{Vendor: "claude", Read: reader}})
	}()
	<-started
	leave()
	<-left
	close(release)
	if err := <-seen; err != nil {
		t.Fatalf("shared read context = %v, want it alive once its first caller left", err)
	}
}
