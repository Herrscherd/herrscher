package usage

import (
	"strings"
	"testing"
	"time"

	contracts "github.com/Herrscherd/herrscher-contracts"
	"github.com/Herrscherd/herrscher-contracts/i18n"
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
	lines, err := LimitLines(limitsSnapshot(time.Now()), i18n.French, Palette{})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(lines[0], "✳ CLAUDE") {
		t.Fatalf("first vendor = %q, want Claude", lines[0])
	}
	resets := make(map[int]bool)
	for _, line := range lines[2:5] {
		before, _, found := strings.Cut(line, " %")
		if !found {
			t.Fatalf("row %q has no percent", line)
		}
		resets[visibleWidth(before)] = true
	}
	if len(resets) != 1 {
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
	plain, err := LimitLines(limitsSnapshot(now), i18n.French, Palette{})
	if err != nil {
		t.Fatal(err)
	}
	colored, err := LimitLines(limitsSnapshot(now), i18n.French, ANSIPalette())
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(strings.Join(colored, "\n"), "\x1b[") {
		t.Fatal("the ANSI palette painted nothing")
	}
	for index := range plain {
		if visibleWidth(colored[index]) != visibleWidth(plain[index]) {
			t.Errorf("line %d is %d cells colored, %d plain", index, visibleWidth(colored[index]), visibleWidth(plain[index]))
		}
	}
}

func TestCostLinesDrawsAShareBarPerModel(t *testing.T) {
	snapshot := Snapshot{Range: Range30d, Spend: Spend{
		Cost: 10, Sessions: 2,
		Accounts: []Share{{Vendor: "claude", Account: "pro", Cost: 10, Tokens: 2000}},
		Models:   []Share{{Model: "claude-opus-5-5", Cost: 9, Tokens: 1500}, {Model: "claude-haiku-5-5", Cost: 1, Tokens: 500}},
	}}
	lines, err := CostLines(snapshot, i18n.French, time.UTC, Palette{})
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
