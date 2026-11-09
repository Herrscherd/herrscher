package usage

import (
	"math"
	"strings"
	"time"

	"github.com/Herrscherd/herrscher-contracts/i18n"

	"github.com/Herrscherd/herrscher/core/clitext"
	"github.com/Herrscherd/herrscher/core/termpaint"
)

const (
	chartRows       = 3
	chartMaxCell    = 4
	chartAxis       = " ┤"
	chartBase       = " ┼"
	chartRule       = "─"
	panelColumnGap  = "    "
	panelModels     = 6
	vendorPrefixSep = "-"
	decimalBase     = 10
)

func chartSteps() []float64 {
	return []float64{1, 2, 5}
}

// CostPanel writes the spend of the period for a panel width cells wide: the total, the
// sessions and the cost by model on the left, the cost by bucket as a bar chart with its
// axis on the right, then the tokens and what the cache saved under both.
func CostPanel(snapshot Snapshot, lang i18n.Language, zone *time.Location, pal termpaint.Palette, width int) ([]string, error) {
	text := &renderer{lang: lang, pal: pal}
	spend := snapshot.Spend
	if snapshot.LedgerTruncated {
		return []string{termpaint.Paint(pal.Bad, text.text(clitext.SpendLedgerTruncated, nil))}, text.err
	}
	if spend.Sessions == 0 {
		return append([]string{termpaint.Paint(pal.Dim, text.text(clitext.SpendEmpty, nil))}, costNotes(snapshot, text)...), text.err
	}
	left := panelSummary(spend, text)
	leftWidth := 0
	for _, line := range left {
		leftWidth = max(leftWidth, termpaint.Width(line))
	}
	right := costChart(snapshot, text, zone, width-leftWidth-len(panelColumnGap))
	rows := max(len(left), len(right))
	lines := make([]string, 0, rows)
	for index := range rows {
		cell := ""
		if index < len(left) {
			cell = left[index]
		}
		line := termpaint.PadRight(cell, leftWidth) + panelColumnGap
		if index < len(right) {
			line += right[index]
		}
		lines = append(lines, strings.TrimRight(line, " "))
	}
	lines = append(lines, "", panelTokens(spend, text))
	return append(lines, costNotes(snapshot, text)...), text.err
}

func panelSummary(spend Spend, text *renderer) []string {
	sessions := text.text(clitext.SpendSessions, clitext.Params{clitext.ParamCount: spend.Sessions})
	lines := []string{
		termpaint.Paint(text.pal.Strong, formatMoney(text.lang, spend.Cost)),
		termpaint.Paint(text.pal.Dim, sessions+summarySep+text.text(clitext.SpendRate, nil)),
		"",
	}
	rows := &termpaint.Table{Right: map[int]bool{1: true}}
	for index := range spend.Models[:min(len(spend.Models), panelModels)] {
		share := spend.Models[index]
		rows.Add(termpaint.Paint(text.pal.Accent, "●")+" "+shortModel(modelLabel(share, text)), formatMoney(text.lang, share.Cost))
	}
	return append(lines, rows.Lines()...)
}

func shortModel(name string) string {
	for _, vendor := range vendorOrder() {
		if trimmed, ok := strings.CutPrefix(name, vendor+vendorPrefixSep); ok {
			return trimmed
		}
	}
	return name
}

func panelTokens(spend Spend, text *renderer) string {
	tokens := spend.Tokens
	return termpaint.Paint(text.pal.Dim, text.text(clitext.SpendPanelVolume, clitext.Params{
		clitext.ParamTotal: termpaint.Paint(text.pal.Strong, formatTokens(text.lang, tokens.Total(), text)),
		clitext.ParamRead:  formatTokens(text.lang, tokens.CacheRead, text),
		clitext.ParamIn:    formatTokens(text.lang, tokens.In, text),
		clitext.ParamOut:   formatTokens(text.lang, tokens.Out, text),
		clitext.ParamSaved: formatMoneyShort(text.lang, spend.Saved),
	}))
}

func costChart(snapshot Snapshot, text *renderer, zone *time.Location, width int) []string {
	buckets := snapshot.Spend.Buckets
	if len(buckets) == 0 {
		return nil
	}
	step := chartStep(peakCost(buckets))
	labels := make([]string, chartRows+1)
	labelWidth := 0
	for row := range chartRows {
		labels[row] = formatMoneyShort(text.lang, step*float64(chartRows-row))
		labelWidth = max(labelWidth, termpaint.Width(labels[row]))
	}
	labels[chartRows] = clitext.FormatNumber(text.lang, 0, 0)
	room := max(width-labelWidth-len(chartAxis), 1)
	values, cell := chartColumns(buckets, room)
	var titleKey clitext.Key = clitext.SpendChartDaily
	if snapshot.Range == Range24h {
		titleKey = clitext.SpendChartHourly
	}
	lines := []string{strings.Repeat(" ", labelWidth+len(chartAxis)) + termpaint.Paint(text.pal.Strong, text.text(titleKey, nil))}
	for row := range chartRows {
		floor := step * float64(chartRows-row-1)
		bars := chartRow(values, floor, step, cell)
		lines = append(lines, termpaint.Paint(text.pal.Dim, termpaint.PadLeft(labels[row], labelWidth)+chartAxis)+termpaint.Paint(text.pal.Accent, bars))
	}
	span := len(values) * cell
	lines = append(lines, termpaint.Paint(text.pal.Dim, termpaint.PadLeft(labels[chartRows], labelWidth)+chartBase+strings.Repeat(chartRule, span)))
	return append(lines, chartDates(snapshot, text, zone, labelWidth+len(chartAxis), span))
}

func peakCost(buckets []Bucket) float64 {
	var peak float64
	for _, bucket := range buckets {
		peak = max(peak, bucket.Cost)
	}
	return peak
}

func chartStep(peak float64) float64 {
	raw := peak / chartRows
	if raw <= 0 {
		return 1
	}
	scale := math.Pow(decimalBase, math.Floor(math.Log10(raw)))
	for _, step := range chartSteps() {
		if step*scale >= raw {
			return step * scale
		}
	}
	return decimalBase * scale
}

func chartColumns(buckets []Bucket, room int) ([]float64, int) {
	group := (len(buckets) + room - 1) / room
	values := make([]float64, 0, len(buckets)/group+1)
	for start := 0; start < len(buckets); start += group {
		var total float64
		for _, bucket := range buckets[start:min(start+group, len(buckets))] {
			total += bucket.Cost
		}
		values = append(values, total)
	}
	return values, min(max(room/len(values), 1), chartMaxCell)
}

func chartRow(values []float64, floor, step float64, cell int) string {
	runes := []rune(sparkCells)
	var bars strings.Builder
	for _, value := range values {
		part := (value - floor) / step
		glyph := " "
		switch {
		case part >= 1:
			glyph = string(runes[len(runes)-1])
		case part > 0:
			glyph = string(runes[min(int(math.Ceil(part*float64(len(runes))))-1, len(runes)-1)])
		}
		bars.WriteString(strings.Repeat(glyph, cell))
	}
	return bars.String()
}

func chartDates(snapshot Snapshot, text *renderer, zone *time.Location, indent, span int) string {
	buckets := snapshot.Spend.Buckets
	label := func(start time.Time) string {
		if snapshot.Range == Range24h {
			return start.In(zone).Format("15:04")
		}
		return text.dayLabel(start.In(zone))
	}
	first, last := label(buckets[0].Start), label(buckets[len(buckets)-1].Start)
	gap := max(span-termpaint.Width(first)-termpaint.Width(last), 1)
	return strings.Repeat(" ", indent) + termpaint.Paint(text.pal.Dim, first+strings.Repeat(" ", gap)+last)
}
