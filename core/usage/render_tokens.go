package usage

import (
	"cmp"
	"slices"

	"github.com/Herrscherd/herrscher-contracts/i18n"

	"github.com/Herrscherd/herrscher/core/clitext"
	"github.com/Herrscherd/herrscher/core/termpaint"
)

// TokenLines writes the tokens of the period by type, with the share of the cache read in
// the input, then the tokens by model.
func TokenLines(snapshot Snapshot, messages *clitext.Messages, lang i18n.Language, pal termpaint.Palette) ([]string, error) {
	text := &renderer{messages: messages, lang: lang, pal: pal}
	lines := append(truncatedNotice(snapshot, text), tokenBody(snapshot, text)...)
	return lines, text.err
}

func tokenBody(snapshot Snapshot, text *renderer) []string {
	lang, pal := text.lang, text.pal
	tokens := snapshot.Spend.Tokens
	if tokens.Total() == 0 {
		return append([]string{emptyLine(snapshot, clitext.SpendEmpty, text)}, costNotes(snapshot, text)...)
	}
	lines := []string{
		text.text(clitext.SpendVolumeTotal, clitext.Params{
			clitext.ParamTotal: termpaint.Paint(pal.Strong, formatTokens(lang, tokens.Total(), text)),
			clitext.ParamIn:    formatTokens(lang, tokens.In, text),
			clitext.ParamOut:   formatTokens(lang, tokens.Out, text),
		}),
		termpaint.Paint(pal.Dim, text.text(clitext.SpendVolumeCache, clitext.Params{
			clitext.ParamRead:   formatTokens(lang, tokens.CacheRead, text),
			clitext.ParamCreate: formatTokens(lang, tokens.CacheCreate, text),
			clitext.ParamRatio:  formatPercent(lang, cacheRatio(tokens)),
		})),
	}
	lines = append(lines, "")
	models := slices.Clone(snapshot.Spend.Models)
	slices.SortStableFunc(models, func(a, b Share) int { return cmp.Compare(b.Tokens, a.Tokens) })
	lines = append(lines, modelTable(models, float64(tokens.Total()), text, clitext.SpendColVolume, func(share Share) string {
		return formatTokens(lang, share.Tokens, text)
	}, func(share Share) float64 { return float64(share.Tokens) })...)
	if rows := sessionTokenTable(snapshot.Spend.Rows, text); len(rows) > 0 {
		lines = append(lines, "")
		lines = append(lines, rows...)
	}
	return append(lines, costNotes(snapshot, text)...)
}

func cacheRatio(tokens Tokens) float64 {
	input := tokens.In + tokens.CacheRead + tokens.CacheCreate
	if input == 0 {
		return 0
	}
	return float64(tokens.CacheRead) / float64(input) * percentScale
}
