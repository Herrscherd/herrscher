package usage

import (
	"strings"
	"testing"
	"time"

	contracts "github.com/Herrscherd/herrscher-contracts"
	"github.com/Herrscherd/herrscher-contracts/i18n"
)

func TestTruncatedLedgerShowsNoTotals(t *testing.T) {
	snapshot := Snapshot{
		Range:           Range7d,
		LedgerTruncated: true,
		Spend:           Spend{Cost: 4.2, Sessions: 3, Tokens: Tokens{In: 100}},
	}
	raw, err := Encode(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := Decode(raw)
	if err != nil {
		t.Fatal(err)
	}
	for name, render := range map[string]func(Snapshot) ([]string, error){
		"cost":   func(s Snapshot) ([]string, error) { return CostLines(s, i18n.English, nil) },
		"tokens": func(s Snapshot) ([]string, error) { return TokenLines(s, i18n.English) },
	} {
		lines, err := render(decoded)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if len(lines) != 1 || !strings.Contains(lines[0], "too large") {
			t.Errorf("%s lines = %q, want only the truncation notice", name, lines)
		}
	}
}

func TestLimitBarDrawsNoPaceMarkerWithoutAKnownReset(t *testing.T) {
	limit := contracts.Limit{Window: contracts.LimitWeekly, UsedPercent: 30, Span: 7 * hoursPerDay * time.Hour}
	if bar := limitBar(limit, PaceOf(limit, time.Now())); strings.Contains(bar, markerCell) {
		t.Errorf("bar = %q, want no pace marker without a reset time", bar)
	}
}
