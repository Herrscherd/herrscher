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
	"github.com/Herrscherd/herrscher/core/termpaint"
)

const (
	barWidth     = 20
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
	wholeFrom    = 100
	monthsInYear = 12
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
	pal  termpaint.Palette
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
		painted[index] = termpaint.Paint(r.pal.Dim, cell)
	}
	return painted
}

// LimitLines writes the limits of the accounts of the snapshot, one table per vendor, each
// window of an account on a row with its bar, its reset and its pace.
func LimitLines(snapshot Snapshot, lang i18n.Language, pal termpaint.Palette) ([]string, error) {
	text := &renderer{lang: lang, pal: pal}
	if len(snapshot.Limits) == 0 {
		return []string{termpaint.Paint(pal.Dim, text.text(clitext.QuotaEmpty, nil))}, text.err
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
		return termpaint.Paint(text.pal.Accent, title)
	}
	return termpaint.Paint(text.pal.Dim, title)
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
	rows := &termpaint.Table{Right: map[int]bool{limitPercentColumn: true}}
	if vendorReady(snapshot.Limits, vendor) {
		rows.Add(text.dimRow("  "+text.text(clitext.QuotaColAccount, nil), text.text(clitext.QuotaColWindow, nil), text.text(clitext.QuotaColRemaining, nil), "", text.text(clitext.QuotaColReset, nil))...)
	}
	for index := range snapshot.Limits {
		account := snapshot.Limits[index]
		if account.Vendor != vendor {
			continue
		}
		addAccountRows(rows, account, snapshot.At, text)
	}
	return rows.Lines()
}

const limitPercentColumn = 3

func addAccountRows(rows *termpaint.Table, account AccountLimits, now time.Time, text *renderer) {
	name := accountName(account, text)
	if account.Status == LimitsFailed {
		rows.Add("  "+name, termpaint.Paint(text.pal.Bad, text.text(clitext.QuotaFailed, nil)))
		return
	}
	if account.Status != LimitsReady || len(account.Limits) == 0 {
		rows.Add("  "+name, termpaint.Paint(text.pal.Dim, unavailableCell+" "+text.text(clitext.QuotaUnavailable, nil)))
		return
	}
	for index, limit := range account.Limits {
		label := name
		if index > 0 {
			label = ""
		}
		pace := PaceOf(limit, now)
		rows.Add("  "+label, windowLabel(limit, text), limitBar(limit, pace, text.pal), limitPercent(limit, text), resetLabel(limit, now, text)+paceLabel(limit, pace, text))
	}
}

const unavailableCell = "─"

func accountName(account AccountLimits, text *renderer) string {
	name := account.Account
	if name == "" {
		name = text.text(clitext.QuotaAccountNative, nil)
	}
	if account.Default {
		name += " " + termpaint.Paint(text.pal.Accent, defaultMark)
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
	label := clitext.FormatNumber(text.lang, remaining, 0) + percentSuffix
	if remaining <= 0 {
		return termpaint.Paint(text.pal.Bad, label)
	}
	return termpaint.Paint(text.pal.Strong, label)
}

func limitBar(limit contracts.Limit, pace Pace, pal termpaint.Palette) string {
	return sizedBar(limit, pace, pal, barWidth)
}

func sizedBar(limit contracts.Limit, pace Pace, pal termpaint.Palette, width int) string {
	filled := int(math.Round(Remaining(limit) / percentScale * float64(width)))
	marker := noMarker
	if !limit.ResetsAt.IsZero() && limit.Span > 0 {
		marker = int(math.Round(pace.Elapsed / percentScale * float64(width)))
	}
	var cells strings.Builder
	run := func(count int, cell string, style termpaint.Style) {
		cells.WriteString(termpaint.Paint(style, strings.Repeat(cell, max(count, 0))))
	}
	fillRun := func(from, until int) {
		run(min(until, filled)-from, fillCell, pal.Fill)
		run(until-max(from, filled), emptyCell, pal.Dim)
	}
	if marker < 0 || marker > width {
		fillRun(0, width)
		return cells.String()
	}
	marker = min(marker, width-1)
	fillRun(0, marker)
	cells.WriteString(termpaint.Paint(pal.Marker, markerCell))
	fillRun(marker+1, width)
	return cells.String()
}

func resetLabel(limit contracts.Limit, now time.Time, text *renderer) string {
	if limit.ResetsAt.IsZero() {
		return termpaint.Paint(text.pal.Dim, text.text(clitext.QuotaResetUnknown, nil))
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
	return text.text(clitext.QuotaResetMinutes, clitext.Params{clitext.ParamMinutes: int((left + time.Minute - 1) / time.Minute)})
}

func paceLabel(limit contracts.Limit, pace Pace, text *renderer) string {
	if Remaining(limit) <= 0 {
		return "  " + termpaint.Paint(text.pal.Bad, text.text(clitext.QuotaExhausted, nil))
	}
	if !pace.Shown {
		return ""
	}
	delta := clitext.FormatNumber(text.lang, math.Abs(pace.Delta), 0)
	if pace.Delta > 0 {
		return "  " + termpaint.Paint(text.pal.Warn, text.text(clitext.QuotaPaceFaster, clitext.Params{clitext.ParamDelta: "+" + delta}))
	}
	return "  " + termpaint.Paint(text.pal.Good, text.text(clitext.QuotaPaceSlower, clitext.Params{clitext.ParamDelta: "-" + delta}))
}

// CostLines writes the total of the spend of the period, its daily sparkline in the zone
// of the display, and its cost by account and by model.
func CostLines(snapshot Snapshot, lang i18n.Language, zone *time.Location, pal termpaint.Palette) ([]string, error) {
	text := &renderer{lang: lang, pal: pal}
	lines := append(truncatedNotice(snapshot, text), costBody(snapshot, text, zone)...)
	return lines, text.err
}

func truncatedNotice(snapshot Snapshot, text *renderer) []string {
	if !snapshot.LedgerTruncated {
		return nil
	}
	return []string{termpaint.Paint(text.pal.Bad, text.text(clitext.SpendLedgerTruncated, nil))}
}

func emptyLine(snapshot Snapshot, empty clitext.Key, text *renderer) string {
	key := empty
	if snapshot.LedgerTruncated {
		key = clitext.SpendUnreadable
	}
	return termpaint.Paint(text.pal.Dim, text.text(key, nil))
}

func costBody(snapshot Snapshot, text *renderer, zone *time.Location) []string {
	lang, pal := text.lang, text.pal
	spend := snapshot.Spend
	if spend.Sessions == 0 {
		return append([]string{emptyLine(snapshot, clitext.SpendEmpty, text)}, costNotes(snapshot, text)...)
	}
	sessions := text.text(clitext.SpendSessions, clitext.Params{clitext.ParamCount: spend.Sessions})
	summary := sessions + summarySep + text.text(rangeKeys()[snapshot.Range], nil) + summarySep + text.text(clitext.SpendEstimate, nil)
	lines := []string{termpaint.Paint(pal.Strong, formatMoney(lang, spend.Cost)) + "  " + termpaint.Paint(pal.Dim, summary)}
	if spark := sparkline(snapshot, text, zone); spark != "" {
		lines = append(lines, spark)
	}
	lines = append(lines, "")
	lines = append(lines, accountTable(spend.Accounts, spend.Cost, text)...)
	lines = append(lines, "")
	lines = append(lines, modelTable(spend.Models, spend.Cost, text, clitext.SpendColCost, func(share Share) string {
		return formatMoney(lang, share.Cost)
	}, func(share Share) float64 { return share.Cost })...)
	if detail := costDetail(spend, text); len(detail) > 0 {
		lines = append(lines, "")
		lines = append(lines, detail...)
	}
	return append(lines, costNotes(snapshot, text)...)
}

func costDetail(spend Spend, text *renderer) []string {
	rows := &termpaint.Table{}
	if spend.Split.Total() > 0 {
		rows.Add(termpaint.Paint(text.pal.Dim, text.text(clitext.SpendByType, nil)), text.text(clitext.SpendTypeSplit, clitext.Params{
			clitext.ParamIn:     formatMoneyShort(text.lang, spend.Split.In),
			clitext.ParamRead:   formatMoneyShort(text.lang, spend.Split.CacheRead),
			clitext.ParamCreate: formatMoneyShort(text.lang, spend.Split.CacheWrite),
			clitext.ParamOut:    formatMoneyShort(text.lang, spend.Split.Out),
		}))
	}
	if spend.Tokens.CacheRead > 0 {
		rows.Add(termpaint.Paint(text.pal.Dim, text.text(clitext.SpendCacheTitle, nil)), text.text(clitext.SpendCacheSaved, clitext.Params{
			clitext.ParamRead:  formatTokens(text.lang, spend.Tokens.CacheRead, text),
			clitext.ParamSaved: formatMoneyShort(text.lang, spend.Saved),
		}))
	}
	return rows.Lines()
}

// CostNotes writes what the totals of the snapshot still miss: transcripts not read yet,
// a price list not loaded yet, and the models no price is known for.
func CostNotes(snapshot Snapshot, lang i18n.Language, pal termpaint.Palette) ([]string, error) {
	text := &renderer{lang: lang, pal: pal}
	return costNotes(snapshot, text), text.err
}

func costNotes(snapshot Snapshot, text *renderer) []string {
	var notes []string
	note := func(key clitext.Key, params clitext.Params) {
		notes = append(notes, termpaint.Paint(text.pal.Warn, "! ")+termpaint.Paint(text.pal.Dim, text.text(key, params)))
	}
	if snapshot.TranscriptsPending {
		note(clitext.SpendTranscriptsPending, nil)
	}
	if snapshot.Unreadable > 0 {
		note(clitext.SpendTranscriptsUnreadable, clitext.Params{clitext.ParamCount: snapshot.Unreadable})
	}
	if snapshot.PricesMissing {
		note(clitext.SpendPricesMissing, nil)
	}
	if len(snapshot.Spend.Unpriced) > 0 {
		note(clitext.SpendUnpriced, clitext.Params{clitext.ParamModels: strings.Join(snapshot.Spend.Unpriced, ", ")})
	}
	if len(notes) == 0 {
		return nil
	}
	return append([]string{""}, notes...)
}

// TokenLines writes the tokens of the period by type, with the share of the cache read in
// the input, then the tokens by model.
func TokenLines(snapshot Snapshot, lang i18n.Language, pal termpaint.Palette) ([]string, error) {
	text := &renderer{lang: lang, pal: pal}
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
	return append(lines, costNotes(snapshot, text)...)
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
	day := peakAt.In(zone).Format("15:04")
	if snapshot.Range != Range24h {
		day = text.dayLabel(peakAt.In(zone))
	}
	peakText := text.text(clitext.SpendPeak, clitext.Params{clitext.ParamCost: formatMoney(text.lang, peak), clitext.ParamDay: day})
	return termpaint.Paint(text.pal.Accent, cells.String()) + "  " + termpaint.Paint(text.pal.Dim, peakText)
}

func (r *renderer) dayLabel(day time.Time) string {
	months := strings.Split(r.text(clitext.SpendMonths, nil), ",")
	if len(months) != monthsInYear {
		return day.Format("02/01")
	}
	return r.text(clitext.SpendPeakDay, clitext.Params{clitext.ParamDay: day.Day(), clitext.ParamMonth: months[day.Month()-1]})
}

func shareOf(value, total float64) float64 {
	if total <= 0 {
		return 0
	}
	return value / total * percentScale
}

func accountTable(shares []Share, total float64, text *renderer) []string {
	rows := &termpaint.Table{Right: map[int]bool{1: true, 2: true, 3: true}}
	rows.Add(text.dimRow(text.text(clitext.SpendColAccount, nil), text.text(clitext.SpendColCost, nil), text.text(clitext.SpendColShare, nil), text.text(clitext.SpendColVolume, nil))...)
	for index := range shares {
		share := shares[index]
		rows.Add("  "+termpaint.Paint(text.pal.Accent, "●")+" "+accountLabel(share, text), formatMoney(text.lang, share.Cost), formatPercent(text.lang, shareOf(share.Cost, total)), formatTokens(text.lang, share.Tokens, text))
	}
	return rows.Lines()
}

func modelTable(shares []Share, total float64, text *renderer, valueHeader clitext.Key, amount func(Share) string, value func(Share) float64) []string {
	rows := &termpaint.Table{Right: map[int]bool{1: true, 3: true}}
	rows.Add(text.dimRow(text.text(clitext.SpendColModel, nil), text.text(valueHeader, nil), "", text.text(clitext.SpendColShare, nil))...)
	var largest float64
	for index := range shares {
		largest = max(largest, value(shares[index]))
	}
	for index := range shares {
		share := shares[index]
		part := shareOf(value(share), total)
		rows.Add("  "+modelLabel(share, text), amount(share), shareBar(shareOf(value(share), largest), text.pal), formatPercent(text.lang, part))
	}
	return rows.Lines()
}

func shareBar(part float64, pal termpaint.Palette) string {
	if part <= 0 {
		return strings.Repeat(" ", barWidth)
	}
	filled := min(max(int(math.Round(part/percentScale*barWidth)), 1), barWidth)
	return termpaint.Paint(pal.Fill, strings.Repeat(fillCell, filled)) + strings.Repeat(" ", barWidth-filled)
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
	value, unit := float64(count), ""
	for _, key := range []clitext.Key{clitext.SpendUnitKilo, clitext.SpendUnitMega, clitext.SpendUnitGiga} {
		if math.Round(value) < kilo {
			break
		}
		value, unit = value/kilo, text.text(key, nil)
	}
	if unit == "" {
		return clitext.FormatNumber(lang, value, 0)
	}
	return clitext.FormatNumber(lang, value, scaledDecimals(value)) + " " + unit
}

func scaledDecimals(value float64) int {
	if math.Round(value*decimalBase)/decimalBase >= wholeFrom {
		return 0
	}
	return 1
}
