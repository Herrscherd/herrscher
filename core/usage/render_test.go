package usage

import (
	"strings"
	"testing"
	"time"

	contracts "github.com/Herrscherd/herrscher-contracts"
	"github.com/Herrscherd/herrscher-contracts/i18n"

	"github.com/Herrscherd/herrscher/core/termpaint"
)

func limitsSnapshot(now time.Time) Snapshot {
	week := 7 * hoursPerDay * time.Hour
	return Snapshot{
		At: now,
		Limits: []AccountLimits{
			{Vendor: "codex", Status: LimitsUnavailable},
			{Vendor: "claude", Account: "pro", Default: true, Status: LimitsReady, Limits: []contracts.Limit{
				{Window: contracts.LimitWeekly, UsedPercent: 49, Span: week, ResetsAt: now.Add(4 * hoursPerDay * time.Hour)},
				{Window: contracts.LimitWeekly, Scope: "fable", UsedPercent: 100, Span: week, ResetsAt: now.Add(4 * hoursPerDay * time.Hour)},
			}},
			{Vendor: "claude", Account: "perso", Status: LimitsReady, Limits: []contracts.Limit{
				{Window: contracts.LimitFiveHour, UsedPercent: 0, Span: 5 * time.Hour},
			}},
		},
	}
}

func TestLimitLinesPutsClaudeFirstAndAlignsTheColumns(t *testing.T) {
	lines, err := LimitLines(limitsSnapshot(time.Now()), i18n.French, termpaint.Palette{})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(lines[0], "✳ CLAUDE") {
		t.Fatalf("first vendor = %q, want Claude", lines[0])
	}
	percentColumns := make(map[int]bool)
	for _, line := range lines[2:5] {
		before, _, found := strings.Cut(line, " %")
		if !found {
			t.Fatalf("row %q has no percent", line)
		}
		percentColumns[termpaint.Width(before)] = true
	}
	if len(percentColumns) != 1 {
		t.Errorf("the percent column is not aligned:\n%s", strings.Join(lines, "\n"))
	}
	joined := strings.Join(lines, "\n")
	for _, want := range []string{"0 %", "épuisé", "─", "─ non disponible", "◎ CODEX"} {
		if !strings.Contains(joined, want) {
			t.Errorf("limits miss %q:\n%s", want, joined)
		}
	}
	if strings.Contains(joined, "0,0 %") {
		t.Errorf("limits show a decimal percent:\n%s", joined)
	}
}

func TestPaletteColorsTheRolesWithoutBreakingTheAlignment(t *testing.T) {
	now := time.Now()
	plain, err := LimitLines(limitsSnapshot(now), i18n.French, termpaint.Palette{})
	if err != nil {
		t.Fatal(err)
	}
	colored, err := LimitLines(limitsSnapshot(now), i18n.French, termpaint.ANSIPalette())
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(strings.Join(colored, "\n"), "\x1b[") {
		t.Fatal("the ANSI palette painted nothing")
	}
	for index := range plain {
		if termpaint.Width(colored[index]) != termpaint.Width(plain[index]) {
			t.Errorf("line %d is %d cells colored, %d plain", index, termpaint.Width(colored[index]), termpaint.Width(plain[index]))
		}
	}
}

func TestCostLinesDrawsAShareBarPerModel(t *testing.T) {
	snapshot := Snapshot{Range: Range30d, Spend: Spend{
		Cost: 10, Sessions: 2,
		Accounts: []Share{{Vendor: "claude", Account: "pro", Cost: 10, Tokens: 2000}},
		Models:   []Share{{Model: "claude-opus-5-5", Cost: 9, Tokens: 1500}, {Model: "claude-haiku-5-5", Cost: 1, Tokens: 500}},
	}}
	lines, err := CostLines(snapshot, i18n.French, time.UTC, termpaint.Palette{})
	if err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(lines, "\n")
	for _, want := range []string{"PAR COMPTE", "PAR MODÈLE", "claude · pro", strings.Repeat(fillCell, 18)} {
		if !strings.Contains(joined, want) {
			t.Errorf("cost misses %q:\n%s", want, joined)
		}
	}
}

func TestLimitBarKeepsItsWidthWithTheMarkerAtEitherEnd(t *testing.T) {
	limit := contracts.Limit{Window: contracts.LimitWeekly, UsedPercent: 50, Span: time.Hour, ResetsAt: time.Now()}
	for _, elapsed := range []float64{0, 50, percentScale} {
		bar := limitBar(limit, Pace{Elapsed: elapsed}, termpaint.Palette{})
		if width := termpaint.Width(bar); width != barWidth {
			t.Errorf("bar at %v %% elapsed is %d cells, want %d: %q", elapsed, width, barWidth, bar)
		}
		if strings.Count(bar, markerCell) != 1 {
			t.Errorf("bar at %v %% elapsed has no single marker: %q", elapsed, bar)
		}
	}
}

func TestShareBarDrawsNothingForAZeroShare(t *testing.T) {
	if bar := shareBar(0, termpaint.Palette{}); strings.Contains(bar, fillCell) || termpaint.Width(bar) != barWidth {
		t.Fatalf("zero share bar = %q", bar)
	}
	if bar := shareBar(0.1, termpaint.Palette{}); strings.Count(bar, fillCell) != 1 {
		t.Fatalf("a tiny share must still show one cell: %q", bar)
	}
}

func TestLimitPercentShowsTheRoundedZeroAsExhausted(t *testing.T) {
	bad := func(text string) string { return "<" + text + ">" }
	text := &renderer{lang: i18n.French, pal: termpaint.Palette{Bad: bad}}
	if got := limitPercent(contracts.Limit{UsedPercent: 99.6}, text); got != "<0 %>" {
		t.Fatalf("limitPercent at 0,4 %% left = %q, want the exhausted style", got)
	}
}
