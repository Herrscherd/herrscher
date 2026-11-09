package usage

import (
	"strings"

	contracts "github.com/Herrscherd/herrscher-contracts"
	"github.com/Herrscherd/herrscher-contracts/i18n"

	"github.com/Herrscherd/herrscher/core/clitext"
	"github.com/Herrscherd/herrscher/core/termpaint"
)

// CompactionLines writes the compactions of the snapshot, one block per stage: its runs
// and their outcomes, the calls it examined and removed, and what its model requests cost.
func CompactionLines(snapshot Snapshot, lang i18n.Language, pal termpaint.Palette) ([]string, error) {
	text := &renderer{lang: lang, pal: pal}
	lines := append(truncatedNotice(snapshot, text), compactionBody(snapshot, text)...)
	return lines, text.err
}

func compactionBody(snapshot Snapshot, text *renderer) []string {
	stages := snapshot.Spend.Stages
	if len(stages) == 0 {
		return []string{termpaint.Paint(text.pal.Dim, text.text(clitext.CompactionEmpty, nil))}
	}
	var lines []string
	for index := range stages {
		if index > 0 {
			lines = append(lines, "")
		}
		lines = append(lines, compactionBlock(stages[index], text)...)
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
	}
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
