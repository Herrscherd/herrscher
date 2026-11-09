package usage

import (
	"strings"
	"testing"
	"time"

	"github.com/Herrscherd/herrscher-contracts/i18n"

	"github.com/Herrscherd/herrscher/core/termpaint"
)

func TestCostLinesShowsThePeakInTheDisplayZone(t *testing.T) {
	peak := time.Date(2026, time.November, 8, 23, 0, 0, 0, time.UTC)
	snapshot := Snapshot{
		Range: Range24h,
		Spend: Spend{Cost: 1, Sessions: 1, Buckets: []Bucket{{Start: peak, Cost: 1}}},
	}
	plus3 := time.FixedZone("plus3", 3*60*60)

	inZone, err := CostLines(snapshot, i18n.English, plus3, termpaint.Palette{})
	if err != nil {
		t.Fatalf("CostLines: %v", err)
	}
	if !strings.Contains(inZone[1], "02:00") {
		t.Fatalf("peak line %q does not show the hour in the display zone", inZone[1])
	}
	inUTC, err := CostLines(snapshot, i18n.English, time.UTC, termpaint.Palette{})
	if err != nil {
		t.Fatalf("CostLines: %v", err)
	}
	if !strings.Contains(inUTC[1], "23:00") {
		t.Fatalf("peak line %q does not show the hour in UTC", inUTC[1])
	}
}
