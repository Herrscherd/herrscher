package usage

import (
	"strings"

	contracts "github.com/Herrscherd/herrscher-contracts"
	"github.com/Herrscherd/herrscher-contracts/i18n"

	"github.com/Herrscherd/herrscher/core/clitext"
	"github.com/Herrscherd/herrscher/core/termpaint"
)

// CompactionLines writes the compactions of the snapshot: how often Jev succeeded and how
// often the backend had to run behind it, then one block per stage with its runs and
// their outcomes, the calls it examined and removed, what its model requests cost, and
// the dollars its removed tokens saved.
func CompactionLines(snapshot Snapshot, lang i18n.Language, pal termpaint.Palette) ([]string, error) {
	text := &renderer{lang: lang, pal: pal}
	lines := append(truncatedNotice(snapshot, text), compactionBody(snapshot, text)...)
	return lines, text.err
}

func compactionBody(snapshot Snapshot, text *renderer) []string {
	stages := snapshot.Spend.Stages
	if len(stages) == 0 {
		return []string{emptyLine(snapshot, clitext.CompactionEmpty, text)}
	}
	var lines []string
	if summary := jevLines(snapshot.Spend.Jev, text); len(summary) > 0 {
		lines = append(lines, summary...)
		lines = append(lines, "")
	}
	for index := range stages {
		if index > 0 {
			lines = append(lines, "")
		}
		lines = append(lines, compactionBlock(stages[index], text)...)
	}
	return lines
}

func jevLines(jev JevUsage, text *renderer) []string {
	var lines []string
	if jev.Runs > 0 {
		lines = append(lines, "  "+text.text(clitext.CompactionJevShare, clitext.Params{
			clitext.ParamSucceeded: jev.Succeeded,
			clitext.ParamCount:     jev.Runs,
			clitext.ParamPercent:   formatPercent(text.lang, jev.SucceededShare()),
		}))
	}
	if jev.Sessions > 0 {
		lines = append(lines, "  "+text.text(clitext.CompactionJevBehind, clitext.Params{
			clitext.ParamBehind:   jev.Behind,
			clitext.ParamSessions: jev.Sessions,
			clitext.ParamPercent:  formatPercent(text.lang, jev.BehindShare()),
		}))
	}
	return lines
}

func compactionBlock(stage StageUsage, text *renderer) []string {
	lang := text.lang
	return []string{
		termpaint.Paint(text.pal.Accent, stage.Stage),
		"  " + text.text(clitext.CompactionRuns, clitext.Params{clitext.ParamCount: stage.Runs}) + summarySep + outcomeLine(stage, text),
		"  " + text.text(clitext.CompactionCalls, clitext.Params{
			clitext.ParamCount:     stage.Calls,
			clitext.ParamKept:      stage.Kept,
			clitext.ParamTruncated: stage.Truncated,
			clitext.ParamPruned:    stage.Pruned,
		}),
		"  " + text.text(clitext.CompactionChars, clitext.Params{clitext.ParamPercent: formatPercent(lang, stage.CharsRemoved())}) +
			summarySep + text.text(clitext.CompactionSaved, clitext.Params{clitext.ParamTokens: formatTokens(lang, stage.TokensRemoved, text)}),
		"  " + text.text(clitext.CompactionRequests, clitext.Params{
			clitext.ParamCount:  stage.Requests,
			clitext.ParamTokens: formatTokens(lang, stage.RequestTokens, text),
		}) + summarySep + text.text(clitext.CompactionAverage, clitext.Params{clitext.ParamDuration: clitext.FormatDuration(lang, stage.AverageDuration())}),
		"  " + savingLine(stage, text),
	}
}

func savingLine(stage StageUsage, text *renderer) string {
	if stage.UnpricedRuns >= stage.Runs {
		return text.text(clitext.CompactionMoneyUnknown, nil)
	}
	line := text.text(clitext.CompactionMoney, clitext.Params{clitext.ParamMoney: formatMoney(text.lang, stage.Saved)})
	if stage.UnpricedRuns > 0 {
		line += summarySep + text.text(clitext.CompactionMoneyPartial, clitext.Params{
			clitext.ParamCount: stage.UnpricedRuns,
			clitext.ParamRuns:  stage.Runs,
		})
	}
	return line
}

func outcomeLine(stage StageUsage, text *renderer) string {
	var parts []string
	for _, outcome := range contracts.CompactOutcomes() {
		key, known := clitext.CompactOutcomeKey(outcome)
		if !known || stage.Outcomes[outcome] == 0 {
			continue
		}
		parts = append(parts, text.text(clitext.CompactionOutcomeCount, clitext.Params{
			clitext.ParamLabel: text.text(key, nil),
			clitext.ParamCount: stage.Outcomes[outcome],
		}))
	}
	return strings.Join(parts, summarySep)
}
