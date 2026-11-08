package usage

import (
	"context"
	"errors"
	"fmt"
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
