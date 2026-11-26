package usage

import (
	"math"

	"github.com/Herrscherd/herrscher/core/clitext"
	"github.com/Herrscherd/herrscher/core/termpaint"
)

const shownSessionRows = 8

func sessionCostTable(rows []SessionRow, text *renderer) []string {
	if len(rows) == 0 {
		return nil
	}
	table := &termpaint.Table{Right: map[int]bool{1: true, 2: true}}
	table.Add(text.dimRow(text.text(clitext.SpendColSession, nil), text.text(clitext.SpendColCost, nil), text.text(clitext.SpendColCacheRead, nil))...)
	for _, row := range shownSessions(rows) {
		table.Add("  "+termpaint.Escape(row.Session).String(), formatMoney(text.lang, row.Cost), formatPercent(text.lang, row.CacheRatio))
	}
	return append(table.Lines(), moreSessionsLine(rows, text)...)
}

func sessionTokenTable(rows []SessionRow, text *renderer) []string {
	if len(rows) == 0 {
		return nil
	}
	table := &termpaint.Table{Right: map[int]bool{1: true, 2: true}}
	table.Add(text.dimRow(text.text(clitext.SpendColSession, nil), text.text(clitext.SpendColVolume, nil), text.text(clitext.SpendColInjected, nil))...)
	estimated := false
	for _, row := range shownSessions(rows) {
		injected := text.text(clitext.SpendNotSized, nil)
		if average, ok := row.Injection.Average(); ok {
			injected = text.text(clitext.SpendInjectedTokens, clitext.Params{clitext.ParamTokens: formatTokens(text.lang, int64(math.Round(average)), text)})
			estimated = true
		}
		table.Add("  "+termpaint.Escape(row.Session).String(), formatTokens(text.lang, row.Tokens.Total(), text), injected)
	}
	lines := append(table.Lines(), moreSessionsLine(rows, text)...)
	if !estimated {
		return lines
	}
	return append(lines, termpaint.Paint(text.pal.Dim, text.text(clitext.SpendInjectedEstimate, nil)))
}

func shownSessions(rows []SessionRow) []SessionRow {
	return rows[:min(len(rows), shownSessionRows)]
}

func moreSessionsLine(rows []SessionRow, text *renderer) []string {
	hidden := len(rows) - len(shownSessions(rows))
	if hidden == 0 {
		return nil
	}
	return []string{termpaint.Paint(text.pal.Dim, text.text(clitext.SpendMoreSessions, clitext.Params{clitext.ParamCount: hidden}))}
}
