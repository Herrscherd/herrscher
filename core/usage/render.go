package usage

import (
	"cmp"
	"fmt"
	"math"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	contracts "github.com/Herrscherd/herrscher-contracts"
	"github.com/Herrscherd/herrscher-contracts/i18n"

	"github.com/Herrscherd/herrscher/core/clitext"
)

const (
	barWidth     = 20
	tabPadding   = 2
	summarySep   = " · "
	percentScale = 100
	sparkCells   = "▁▂▃▄▅▆▇█"
	markerCell   = "│"
	noMarker     = -1
	fillCell     = "█"
	emptyCell    = "░"
	defaultMark  = "★"
	hoursPerDay  = 24
	kilo         = 1_000
	mega         = 1_000_000
	giga         = 1_000_000_000
	wholeFrom    = 100
)

func vendorOrder() []string {
	return []string{"claude", "codex"}
}

func vendorGlyphs() map[string]string {
	return map[string]string{
		"claude": "✳",
		"codex":  "◎",
	}
}

func windowKeys() map[contracts.LimitWindow]clitext.Key {
	return map[contracts.LimitWindow]clitext.Key{
		contracts.LimitFiveHour: clitext.QuotaWindowFiveHour,
		contracts.LimitWeekly:   clitext.QuotaWindowWeekly,
		contracts.LimitMonthly:  clitext.QuotaWindowMonthly,
	}
}

func rangeKeys() map[Range]clitext.Key {
	return map[Range]clitext.Key{
		Range24h: clitext.SpendRange24h,
		Range7d:  clitext.SpendRange7d,
		Range30d: clitext.SpendRange30d,
		Range90d: clitext.SpendRange90d,
	}
}

type renderer struct {
	lang i18n.Language
	pal  Palette
	err  error
}

func (r *renderer) text(key clitext.Key, params clitext.Params) string {
	value, err := clitext.Text(r.lang, key, params)
	if r.err == nil {
		r.err = err
	}
	return value
}

func (r *renderer) dimRow(cells ...string) []string {
	painted := make([]string, len(cells))
	for index, cell := range cells {
		painted[index] = paint(r.pal.Dim, cell)
	}
	return painted
}

// LimitLines writes the limits of the accounts of the snapshot, one table per vendor, each
// window of an account on a row with its bar, its reset and its pace.
func LimitLines(snapshot Snapshot, lang i18n.Language, pal Palette) ([]string, error) {
	text := &renderer{lang: lang, pal: pal}
	if len(snapshot.Limits) == 0 {
		return []string{paint(pal.Dim, text.text(clitext.QuotaEmpty, nil))}, text.err
	}
	var lines []string
	for index, vendor := range vendorsOf(snapshot.Limits) {
		if index > 0 {
			lines = append(lines, "")
		}
		lines = append(lines, vendorHeader(snapshot.Limits, vendor, text))
		lines = append(lines, vendorTable(snapshot, vendor, text)...)
	}
	return lines, text.err
}

func vendorsOf(limits []AccountLimits) []string {
	var vendors []string
	for index := range limits {
		if !slices.Contains(vendors, limits[index].Vendor) {
			vendors = append(vendors, limits[index].Vendor)
		}
	}
	slices.SortStableFunc(vendors, func(a, b string) int {
		return cmp.Or(cmp.Compare(vendorRank(a), vendorRank(b)), cmp.Compare(a, b))
	})
	return vendors
}

func vendorRank(vendor string) int {
	if rank := slices.Index(vendorOrder(), vendor); rank >= 0 {
		return rank
	}
	return len(vendorOrder())
}

func vendorHeader(limits []AccountLimits, vendor string, text *renderer) string {
	title := vendorGlyph(vendor) + " " + strings.ToUpper(vendor)
	if vendorReady(limits, vendor) {
		return paint(text.pal.Accent, title)
	}
	return paint(text.pal.Dim, title)
}

func vendorReady(limits []AccountLimits, vendor string) bool {
	return slices.ContainsFunc(limits, func(account AccountLimits) bool {
		return account.Vendor == vendor && account.Status == LimitsReady && len(account.Limits) > 0
	})
}

func vendorGlyph(vendor string) string {
	if glyph, ok := vendorGlyphs()[vendor]; ok {
		return glyph
	}
	return "●"
}

func vendorTable(snapshot Snapshot, vendor string, text *renderer) []string {
	rows := &table{right: map[int]bool{limitPercentColumn: true}}
	if vendorReady(snapshot.Limits, vendor) {
		rows.add(text.dimRow("  "+text.text(clitext.QuotaColAccount, nil), text.text(clitext.QuotaColWindow, nil), text.text(clitext.QuotaColRemaining, nil), "", text.text(clitext.QuotaColReset, nil))...)
	}
	for index := range snapshot.Limits {
		account := snapshot.Limits[index]
		if account.Vendor != vendor {
			continue
		}
		addAccountRows(rows, account, snapshot.At, text)
	}
	return rows.lines()
}

const limitPercentColumn = 3

func addAccountRows(rows *table, account AccountLimits, now time.Time, text *renderer) {
	name := accountName(account, text)
	if account.Status == LimitsFailed {
		rows.add("  "+name, paint(text.pal.Bad, text.text(clitext.QuotaFailed, nil)))
		return
	}
	if account.Status != LimitsReady || len(account.Limits) == 0 {
		rows.add("  "+name, paint(text.pal.Dim, unavailableCell+" "+text.text(clitext.QuotaUnavailable, nil)))
		return
	}
	for index, limit := range account.Limits {
		label := name
		if index > 0 {
			label = ""
		}
		pace := PaceOf(limit, now)
		rows.add("  "+label, windowLabel(limit, text), limitBar(limit, pace, text.pal), limitPercent(limit, text), resetLabel(limit, now, text)+paceLabel(limit, pace, text))
	}
}

const unavailableCell = "─"

func accountName(account AccountLimits, text *renderer) string {
	name := account.Account
	if name == "" {
		name = text.text(clitext.QuotaAccountNative, nil)
	}
	if account.Default {
		name += " " + paint(text.pal.Accent, defaultMark)
	}
	return name
}

func windowLabel(limit contracts.Limit, text *renderer) string {
	label := text.text(windowKeys()[limit.Window], nil)
	if limit.Scope == "" {
		return label
	}
	return text.text(clitext.QuotaWindowScoped, clitext.Params{clitext.ParamWindow: label, clitext.ParamScope: capitalize(limit.Scope)})
}

func capitalize(name string) string {
	first, size := utf8.DecodeRuneInString(name)
	return strings.ToUpper(string(first)) + name[size:]
}

func limitPercent(limit contracts.Limit, text *renderer) string {
	remaining := math.Round(Remaining(limit))
	label := formatNumber(text.lang, remaining, 0) + percentSuffix
	if remaining <= 0 {
		return paint(text.pal.Bad, label)
	}
	return paint(text.pal.Strong, label)
}

func limitBar(limit contracts.Limit, pace Pace, pal Palette) string {
	filled := int(math.Round(Remaining(limit) / percentScale * barWidth))
	marker := noMarker
	if !limit.ResetsAt.IsZero() && limit.Span > 0 {
		marker = int(math.Round(pace.Elapsed / percentScale * barWidth))
	}
	var cells strings.Builder
	run := func(count int, cell string, style Style) {
		cells.WriteString(paint(style, strings.Repeat(cell, max(count, 0))))
	}
	fillRun := func(from, until int) {
		run(min(until, filled)-from, fillCell, pal.Fill)
		run(until-max(from, filled), emptyCell, pal.Dim)
	}
	if marker < 0 || marker > barWidth {
		fillRun(0, barWidth)
		return cells.String()
	}
	marker = min(marker, barWidth-1)
	fillRun(0, marker)
	cells.WriteString(paint(pal.Marker, markerCell))
	fillRun(marker+1, barWidth)
	return cells.String()
}

func resetLabel(limit contracts.Limit, now time.Time, text *renderer) string {
	if limit.ResetsAt.IsZero() {
		return paint(text.pal.Dim, text.text(clitext.QuotaResetUnknown, nil))
	}
	left := ResetIn(limit, now)
	switch {
	case left == 0:
		return text.text(clitext.QuotaResetNow, nil)
	case left >= hoursPerDay*time.Hour:
		days := int(left / (hoursPerDay * time.Hour))
		hours := int(left % (hoursPerDay * time.Hour) / time.Hour)
		return text.text(clitext.QuotaResetDays, clitext.Params{clitext.ParamDays: days, clitext.ParamHours: hours})
	case left >= time.Hour:
		hours := int(left / time.Hour)
		minutes := int(left % time.Hour / time.Minute)
		return text.text(clitext.QuotaResetHours, clitext.Params{clitext.ParamHours: hours, clitext.ParamMinutes: fmt.Sprintf("%02d", minutes)})
	}
	return text.text(clitext.QuotaResetMinutes, clitext.Params{clitext.ParamMinutes: int(left / time.Minute)})
}

func paceLabel(limit contracts.Limit, pace Pace, text *renderer) string {
	if Remaining(limit) <= 0 {
		return "  " + paint(text.pal.Bad, text.text(clitext.QuotaExhausted, nil))
	}
	if !pace.Shown {
		return ""
	}
	delta := formatNumber(text.lang, math.Abs(pace.Delta), 0)
	if pace.Delta > 0 {
		return "  " + paint(text.pal.Warn, text.text(clitext.QuotaPaceFaster, clitext.Params{clitext.ParamDelta: "+" + delta}))
	}
	return "  " + paint(text.pal.Good, text.text(clitext.QuotaPaceSlower, clitext.Params{clitext.ParamDelta: "-" + delta}))
}

// CostLines writes the total of the spend of the period, its daily sparkline in the zone
// of the display, and its cost by account and by model.
func CostLines(snapshot Snapshot, lang i18n.Language, zone *time.Location, pal Palette) ([]string, error) {
	text := &renderer{lang: lang, pal: pal}
	spend := snapshot.Spend
	if snapshot.LedgerTruncated {
		return []string{paint(pal.Bad, text.text(clitext.SpendLedgerTruncated, nil))}, text.err
	}
	if spend.Sessions == 0 {
		return []string{paint(pal.Dim, text.text(clitext.SpendEmpty, nil))}, text.err
	}
	sessions := text.text(clitext.SpendSessions, clitext.Params{clitext.ParamCount: spend.Sessions})
	summary := sessions + summarySep + text.text(rangeKeys()[snapshot.Range], nil) + summarySep + text.text(clitext.SpendEstimate, nil)
	lines := []string{paint(pal.Strong, formatMoney(lang, spend.Cost)) + "  " + paint(pal.Dim, summary)}
	if spark := sparkline(snapshot, text, zone); spark != "" {
		lines = append(lines, spark)
	}
	lines = append(lines, "")
	lines = append(lines, accountTable(spend.Accounts, spend.Cost, text)...)
	lines = append(lines, "")
	lines = append(lines, modelTable(spend.Models, spend.Cost, text, clitext.SpendColCost, func(share Share) string {
		return formatMoney(lang, share.Cost)
	}, func(share Share) float64 { return share.Cost })...)
	return lines, text.err
}

// TokenLines writes the tokens of the period by type, with the share of the cache read in
// the input, then the tokens by model.
func TokenLines(snapshot Snapshot, lang i18n.Language, pal Palette) ([]string, error) {
	text := &renderer{lang: lang, pal: pal}
	tokens := snapshot.Spend.Tokens
	if snapshot.LedgerTruncated {
		return []string{paint(pal.Bad, text.text(clitext.SpendLedgerTruncated, nil))}, text.err
	}
	if tokens.Total() == 0 {
		return []string{paint(pal.Dim, text.text(clitext.SpendEmpty, nil))}, text.err
	}
	lines := []string{
		text.text(clitext.SpendVolumeTotal, clitext.Params{
			clitext.ParamTotal: paint(pal.Strong, formatTokens(lang, tokens.Total(), text)),
			clitext.ParamIn:    formatTokens(lang, tokens.In, text),
			clitext.ParamOut:   formatTokens(lang, tokens.Out, text),
		}),
		paint(pal.Dim, text.text(clitext.SpendVolumeCache, clitext.Params{
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
	return lines, text.err
}

func cacheRatio(tokens Tokens) float64 {
	input := tokens.In + tokens.CacheRead + tokens.CacheCreate
	if input == 0 {
		return 0
	}
	return float64(tokens.CacheRead) / float64(input) * percentScale
}

func sparkline(snapshot Snapshot, text *renderer, zone *time.Location) string {
	var peak float64
	var peakAt time.Time
	for _, bucket := range snapshot.Spend.Buckets {
		if bucket.Cost > peak {
			peak, peakAt = bucket.Cost, bucket.Start
		}
	}
	if peak == 0 {
		return ""
	}
	runes := []rune(sparkCells)
	var cells strings.Builder
	for _, bucket := range snapshot.Spend.Buckets {
		index := int(math.Round(bucket.Cost / peak * float64(len(runes)-1)))
		cells.WriteRune(runes[index])
	}
	layout := "02/01"
	if snapshot.Range == Range24h {
		layout = "15:04"
	}
	peakText := text.text(clitext.SpendPeak, clitext.Params{clitext.ParamCost: formatMoney(text.lang, peak), clitext.ParamDay: peakAt.In(zone).Format(layout)})
	return paint(text.pal.Accent, cells.String()) + "  " + paint(text.pal.Dim, peakText)
}

func shareOf(value, total float64) float64 {
	if total <= 0 {
		return 0
	}
	return value / total * percentScale
}

func accountTable(shares []Share, total float64, text *renderer) []string {
	rows := &table{right: map[int]bool{1: true, 2: true, 3: true}}
	rows.add(text.dimRow(text.text(clitext.SpendColAccount, nil), text.text(clitext.SpendColCost, nil), text.text(clitext.SpendColShare, nil), text.text(clitext.SpendColVolume, nil))...)
	for index := range shares {
		share := shares[index]
		rows.add("  "+paint(text.pal.Accent, "●")+" "+accountLabel(share, text), formatMoney(text.lang, share.Cost), formatPercent(text.lang, shareOf(share.Cost, total)), formatTokens(text.lang, share.Tokens, text))
	}
	return rows.lines()
}

func modelTable(shares []Share, total float64, text *renderer, valueHeader clitext.Key, amount func(Share) string, value func(Share) float64) []string {
	rows := &table{right: map[int]bool{1: true, 3: true}}
	rows.add(text.dimRow(text.text(clitext.SpendColModel, nil), text.text(valueHeader, nil), "", text.text(clitext.SpendColShare, nil))...)
	for index := range shares {
		share := shares[index]
		part := shareOf(value(share), total)
		rows.add("  "+modelLabel(share, text), amount(share), shareBar(part, text.pal), formatPercent(text.lang, part))
	}
	return rows.lines()
}

func shareBar(part float64, pal Palette) string {
	if part <= 0 {
		return strings.Repeat(" ", barWidth)
	}
	filled := min(max(int(math.Round(part/percentScale*barWidth)), 1), barWidth)
	return paint(pal.Fill, strings.Repeat(fillCell, filled)) + strings.Repeat(" ", barWidth-filled)
}

func accountLabel(share Share, text *renderer) string {
	name := share.Account
	if name == "" {
		name = text.text(clitext.QuotaAccountNative, nil)
	}
	return share.Vendor + summarySep + name
}

func modelLabel(share Share, text *renderer) string {
	if share.Model == "" {
		return text.text(clitext.SpendModelUnknown, nil)
	}
	return share.Model
}

func formatTokens(lang i18n.Language, count int64, text *renderer) string {
	switch {
	case count >= giga:
		return scaled(lang, float64(count)/giga, text.text(clitext.SpendUnitGiga, nil))
	case count >= mega:
		return scaled(lang, float64(count)/mega, text.text(clitext.SpendUnitMega, nil))
	case count >= kilo:
		return scaled(lang, float64(count)/kilo, text.text(clitext.SpendUnitKilo, nil))
	}
	return formatNumber(lang, float64(count), 0)
}

func scaled(lang i18n.Language, value float64, unit string) string {
	decimals := 1
	if value >= wholeFrom {
		decimals = 0
	}
	return formatNumber(lang, value, decimals) + " " + unit
}
