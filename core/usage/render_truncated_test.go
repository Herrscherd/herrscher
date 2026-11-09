package usage

import (
	"slices"
	"strings"
	"testing"
	"time"

	contracts "github.com/Herrscherd/herrscher-contracts"
	"github.com/Herrscherd/herrscher-contracts/i18n"

	"github.com/Herrscherd/herrscher/core/termpaint"
)

func TestTruncatedLedgerShowsTheNoticeBeforeThePartialData(t *testing.T) {
	readable := Snapshot{
		Range: Range7d,
		Spend: Spend{
			Cost:     4.2,
			Sessions: 3,
			Tokens:   Tokens{In: 100},
			Stages:   []StageUsage{{Stage: "jev", Runs: 1, Calls: 2}},
		},
	}
	truncated := readable
	truncated.LedgerTruncated = true
	raw, err := Encode(truncated)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := Decode(raw)
	if err != nil {
		t.Fatal(err)
	}
	renders := map[string]func(Snapshot) ([]string, error){
		"cost":       func(s Snapshot) ([]string, error) { return CostLines(s, i18n.English, nil, termpaint.Palette{}) },
		"tokens":     func(s Snapshot) ([]string, error) { return TokenLines(s, i18n.English, termpaint.Palette{}) },
		"panel":      func(s Snapshot) ([]string, error) { return CostPanel(s, i18n.English, nil, termpaint.Palette{}, 80) },
		"compaction": func(s Snapshot) ([]string, error) { return CompactionLines(s, i18n.English, termpaint.Palette{}) },
	}
	for name, render := range renders {
		want, err := render(readable)
		if err != nil {
			t.Fatalf("%s readable: %v", name, err)
		}
		got, err := render(decoded)
		if err != nil {
			t.Fatalf("%s truncated: %v", name, err)
		}
		if len(want) == 0 || len(got) != len(want)+1 {
			t.Fatalf("%s truncated lines = %q, want the notice then the %d readable lines", name, got, len(want))
		}
		if !strings.Contains(got[0], "partial") {
			t.Errorf("%s first line = %q, want the partial notice first", name, got[0])
		}
		if !slices.Equal(got[1:], want) {
			t.Errorf("%s data after the notice = %q, want the readable data %q", name, got[1:], want)
		}
	}
}

func TestLimitBarDrawsNoPaceMarkerWithoutAKnownReset(t *testing.T) {
	limit := contracts.Limit{Window: contracts.LimitWeekly, UsedPercent: 30, Span: 7 * hoursPerDay * time.Hour}
	if bar := limitBar(limit, PaceOf(limit, time.Now()), termpaint.Palette{}); strings.Contains(bar, markerCell) {
		t.Errorf("bar = %q, want no pace marker without a reset time", bar)
	}
}
