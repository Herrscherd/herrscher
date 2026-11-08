package usage

import (
	"testing"
	"time"

	contracts "github.com/Herrscherd/herrscher-contracts"
)

func TestPaceOf(t *testing.T) {
	week := 7 * 24 * time.Hour
	tests := []struct {
		name   string
		limit  contracts.Limit
		shown  bool
		delta  float64
		remain float64
	}{
		{
			name:   "ahead of the window",
			limit:  contracts.Limit{UsedPercent: 60, Span: week, ResetsAt: testNow().Add(week / 2)},
			shown:  true,
			delta:  10,
			remain: 40,
		},
		{
			name:   "on pace stays hidden",
			limit:  contracts.Limit{UsedPercent: 50, Span: week, ResetsAt: testNow().Add(week / 2)},
			delta:  0,
			remain: 50,
		},
		{
			name:  "scoped window stays hidden",
			limit: contracts.Limit{Scope: "opus", UsedPercent: 90, Span: week, ResetsAt: testNow().Add(week / 2)},
			delta: 40, remain: 10,
		},
		{
			name:  "short window stays hidden",
			limit: contracts.Limit{UsedPercent: 90, Span: 5 * time.Hour, ResetsAt: testNow().Add(time.Hour)},
			delta: 10, remain: 10,
		},
		{
			name:   "exhausted stays hidden",
			limit:  contracts.Limit{UsedPercent: 100, Span: week, ResetsAt: testNow().Add(week / 2)},
			delta:  50,
			remain: 0,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pace := PaceOf(tt.limit, testNow())
			if pace.Shown != tt.shown {
				t.Errorf("Shown = %v, want %v", pace.Shown, tt.shown)
			}
			if d := pace.Delta - tt.delta; d > 0.001 || d < -0.001 {
				t.Errorf("Delta = %v, want %v", pace.Delta, tt.delta)
			}
			if r := Remaining(tt.limit); r != tt.remain {
				t.Errorf("Remaining = %v, want %v", r, tt.remain)
			}
		})
	}
}

func TestPaceOfZeroSpan(t *testing.T) {
	if pace := PaceOf(contracts.Limit{UsedPercent: 80}, testNow()); pace != (Pace{}) {
		t.Fatalf("PaceOf without span = %+v, want zero", pace)
	}
}
