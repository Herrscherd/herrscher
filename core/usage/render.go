package usage

import (
	"bytes"
	"cmp"
	"fmt"
	"io"
	"math"
	"slices"
	"strings"
	"text/tabwriter"
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
	fillCell     = "█"
	emptyCell    = "░"
	defaultMark  = "★"
	hoursPerDay  = 24
	kilo         = 1_000
	mega         = 1_000_000
	giga         = 1_000_000_000
	wholeFrom    = 100
)

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
	err  error
}

func (r *renderer) row(w io.Writer, cells ...string) {
	_, err := fmt.Fprintln(w, strings.Join(cells, "\t"))
	if r.err == nil {
		r.err = err
	}
}

func (r *renderer) text(key clitext.Key, params clitext.Params) string {
	value, err := clitext.Text(r.lang, key, params)
	if r.err == nil {
		r.err = err
	}
	return value
}

// LimitLines writes the limits of the accounts of the snapshot, one table per vendor, each
// window of an account on a row with its bar, its reset and its pace.
func LimitLines(snapshot Snapshot, lang i18n.Language) ([]string, error) {
	text := &renderer{lang: lang}
	if len(snapshot.Limits) == 0 {
		return []string{text.text(clitext.QuotaEmpty, nil)}, text.err
	}
	var lines []string
	for _, vendor := range vendorsOf(snapshot.Limits) {
		lines = append(lines, vendorGlyph(vendor)+" "+strings.ToUpper(vendor))
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
	return vendors
}

func vendorGlyph(vendor string) string {
	if glyph, ok := vendorGlyphs()[vendor]; ok {
		return glyph
	}
	return "●"
}

func vendorTable(snapshot Snapshot, vendor string, text *renderer) []string {
	var buf bytes.Buffer
	tabs := tabwriter.NewWriter(&buf, 0, 0, tabPadding, ' ', 0)
	text.row(tabs, "  "+text.text(clitext.QuotaColAccount, nil), text.text(clitext.QuotaColWindow, nil), text.text(clitext.QuotaColRemaining, nil), text.text(clitext.QuotaColReset, nil))
	for index := range snapshot.Limits {
		account := snapshot.Limits[index]
		if account.Vendor != vendor {
			continue
		}
		writeAccountRows(tabs, account, snapshot.At, text)
	}
	if err := tabs.Flush(); err != nil && text.err == nil {
		text.err = err
	}
	return strings.Split(strings.TrimRight(buf.String(), "\n"), "\n")
}

func writeAccountRows(tabs *tabwriter.Writer, account AccountLimits, now time.Time, text *renderer) {
	name := accountName(account, text)
	if account.Status != LimitsReady {
		status := text.text(clitext.QuotaUnavailable, nil)
		if account.Status == LimitsFailed {
			status = text.text(clitext.QuotaFailed, nil)
		}
		text.row(tabs, "  "+name, status)
		return
	}
	if len(account.Limits) == 0 {
		text.row(tabs, "  "+name, text.text(clitext.QuotaUnavailable, nil))
		return
	}
	for index, limit := range account.Limits {
		label := name
		if index > 0 {
			label = ""
		}
		pace := PaceOf(limit, now)
		text.row(tabs, "  "+label, windowLabel(limit, text), limitBar(limit, pace)+" "+formatPercent(text.lang, Remaining(limit)), resetLabel(limit, now, text)+paceLabel(limit, pace, text))
	}
}

func accountName(account AccountLimits, text *renderer) string {
	name := account.Account
	if name == "" {
		name = text.text(clitext.QuotaAccountNative, nil)
	}
	if account.Default {
		name += " " + defaultMark
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

func limitBar(limit contracts.Limit, pace Pace) string {
	filled := int(math.Round(Remaining(limit) / percentScale * barWidth))
	marker := int(math.Round(pace.Elapsed / percentScale * barWidth))
	var cells strings.Builder
	for index := range barWidth {
		if index == marker {
			cells.WriteString(markerCell)
		}
		if index < filled {
			cells.WriteString(fillCell)
		} else {
			cells.WriteString(emptyCell)
		}
	}
	if marker >= barWidth {
		cells.WriteString(markerCell)
	}
	return cells.String()
}

func resetLabel(limit contracts.Limit, now time.Time, text *renderer) string {
	if limit.ResetsAt.IsZero() {
		return text.text(clitext.QuotaResetUnknown, nil)
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
	if pace.Shown {
		delta := formatNumber(text.lang, math.Abs(pace.Delta), 0)
		if pace.Delta > 0 {
			return "  " + text.text(clitext.QuotaPaceFaster, clitext.Params{clitext.ParamDelta: "+" + delta})
		}
		return "  " + text.text(clitext.QuotaPaceSlower, clitext.Params{clitext.ParamDelta: "-" + delta})
	}
	if Remaining(limit) <= 0 {
		return "  " + text.text(clitext.QuotaExhausted, nil)
	}
	return ""
}

// CostLines writes the total of the spend of the period, its daily sparkline in the zone
// of the display, and its cost by account and by model.
func CostLines(snapshot Snapshot, lang i18n.Language, zone *time.Location) ([]string, error) {
	text := &renderer{lang: lang}
	spend := snapshot.Spend
	if spend.Sessions == 0 {
		return []string{text.text(clitext.SpendEmpty, nil)}, text.err
	}
	sessions := text.text(clitext.SpendSessions, clitext.Params{clitext.ParamCount: spend.Sessions})
	head := formatMoney(lang, spend.Cost) + "  " + sessions + summarySep + text.text(rangeKeys()[snapshot.Range], nil) + summarySep + text.text(clitext.SpendEstimate, nil)
	lines := []string{head, sparkline(snapshot, text, zone)}
	lines = append(lines, "")
	lines = append(lines, shareTable(spend.Accounts, spend.Cost, text, clitext.SpendColAccount, func(share Share) string {
		return accountLabel(share, text)
	}, func(share Share) float64 { return share.Cost })...)
	lines = append(lines, "")
	lines = append(lines, shareTable(spend.Models, spend.Cost, text, clitext.SpendColModel, func(share Share) string {
		return modelLabel(share, text)
	}, func(share Share) float64 { return share.Cost })...)
	return lines, text.err
}

// TokenLines writes the tokens of the period by type, with the share of the cache read in
// the input, then the tokens by model.
func TokenLines(snapshot Snapshot, lang i18n.Language) ([]string, error) {
	text := &renderer{lang: lang}
	tokens := snapshot.Spend.Tokens
	if tokens.Total() == 0 {
		return []string{text.text(clitext.SpendEmpty, nil)}, text.err
	}
	total := formatTokens(lang, tokens.Total(), text)
	lines := []string{
		text.text(clitext.SpendVolumeTotal, clitext.Params{
			clitext.ParamTotal: total,
			clitext.ParamIn:    formatTokens(lang, tokens.In, text),
			clitext.ParamOut:   formatTokens(lang, tokens.Out, text),
		}),
		text.text(clitext.SpendVolumeCache, clitext.Params{
			clitext.ParamRead:   formatTokens(lang, tokens.CacheRead, text),
			clitext.ParamCreate: formatTokens(lang, tokens.CacheCreate, text),
			clitext.ParamRatio:  formatPercent(lang, cacheRatio(tokens)),
		}),
	}
	lines = append(lines, "")
	models := slices.Clone(snapshot.Spend.Models)
	slices.SortStableFunc(models, func(a, b Share) int { return cmp.Compare(b.Tokens, a.Tokens) })
	lines = append(lines, shareTable(models, float64(snapshot.Spend.Tokens.Total()), text, clitext.SpendColModel, func(share Share) string {
		return modelLabel(share, text)
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
	return cells.String() + "  " + text.text(clitext.SpendPeak, clitext.Params{clitext.ParamCost: formatMoney(text.lang, peak), clitext.ParamDay: peakAt.In(zone).Format(layout)})
}

func shareTable(shares []Share, total float64, text *renderer, header clitext.Key, label func(Share) string, value func(Share) float64) []string {
	var buf bytes.Buffer
	tabs := tabwriter.NewWriter(&buf, 0, 0, tabPadding, ' ', 0)
	text.row(tabs, "  "+text.text(header, nil), text.text(clitext.SpendColCost, nil), text.text(clitext.SpendColShare, nil), text.text(clitext.SpendColVolume, nil))
	for index := range shares {
		share := shares[index]
		var part float64
		if total > 0 {
			part = value(share) / total * percentScale
		}
		text.row(tabs, "  "+label(share), formatMoney(text.lang, share.Cost), formatPercent(text.lang, part), formatTokens(text.lang, share.Tokens, text))
	}
	if err := tabs.Flush(); err != nil && text.err == nil {
		text.err = err
	}
	return strings.Split(strings.TrimRight(buf.String(), "\n"), "\n")
}

func accountLabel(share Share, text *renderer) string {
	name := share.Account
	if name == "" {
		name = text.text(clitext.QuotaAccountNative, nil)
	}
	return "● " + share.Vendor + summarySep + name
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
