package usage

import (
	"math"
	"slices"
	"strings"
	"time"

	contracts "github.com/Herrscherd/herrscher-contracts"
	"github.com/Herrscherd/herrscher-contracts/i18n"

	"github.com/Herrscherd/herrscher/core/clitext"
	"github.com/Herrscherd/herrscher/core/termpaint"
)

const (
	cardBarWidth  = 12
	cardIndent    = "  "
	cardPad       = " "
	cardColumnGap = "  "
	accountMark   = "▐ "
	cardSides     = 2
	cardPadCells  = 2
	paceUp        = "↗"
	paceDown      = "↘"
)

// CardEdges holds the box drawing characters of the limit cards.
type CardEdges struct {
	TopLeft     string
	TopRight    string
	BottomLeft  string
	BottomRight string
	TeeLeft     string
	TeeRight    string
	Horizontal  string
	Vertical    string
}

// UnicodeEdges is the square box of a terminal that draws box characters.
func UnicodeEdges() CardEdges {
	return CardEdges{"┌", "┐", "└", "┘", "├", "┤", "─", "│"}
}

type windowKey struct {
	window contracts.LimitWindow
	scope  string
}

type windowAccount struct {
	account AccountLimits
	limit   contracts.Limit
}

// LimitCards writes the limits of the snapshot for a panel width cells wide: per vendor
// a box with one card per window, the accounts of that window side by side with their
// bar, their reset and their share left, and the vendors with nothing to read on one line.
func LimitCards(snapshot Snapshot, lang i18n.Language, pal termpaint.Palette, edges CardEdges, width int) ([]string, error) {
	text := &renderer{lang: lang, pal: pal}
	if len(snapshot.Limits) == 0 {
		return []string{termpaint.Paint(pal.Dim, text.text(clitext.QuotaEmpty, nil))}, text.err
	}
	var lines []string
	for index, vendor := range vendorsOf(snapshot.Limits) {
		if index > 0 {
			lines = append(lines, "")
		}
		lines = append(lines, vendorCards(snapshot, vendor, text, edges, width)...)
	}
	return lines, text.err
}

func vendorCards(snapshot Snapshot, vendor string, text *renderer, edges CardEdges, width int) []string {
	title := termpaint.Paint(text.pal.Accent, vendorGlyph(vendor)+" "+capitalize(vendor))
	idle := idleAccounts(snapshot.Limits, vendor, text)
	if !vendorReady(snapshot.Limits, vendor) {
		return []string{title + cardColumnGap + termpaint.Paint(text.pal.Dim, strings.Join(idle, summarySep))}
	}
	inner := max(width-cardSides-cardPadCells, 1)
	var parts []cardParts
	leftWidth := 0
	for _, key := range windowsOf(snapshot.Limits, vendor) {
		part := windowCard(windowAccounts(snapshot.Limits, vendor, key), snapshot.At, text)
		parts = append(parts, part)
		for _, line := range part.left {
			leftWidth = max(leftWidth, termpaint.Width(line))
		}
	}
	leftWidth += len(cardColumnGap)
	cards := make([][]string, 0, len(parts))
	for index := range parts {
		cards = append(cards, layoutCard(parts[index].left, parts[index].segments, leftWidth, inner))
	}
	lines := []string{title}
	lines = append(lines, boxed(cards, edges, text.pal, inner)...)
	if len(idle) > 0 {
		lines = append(lines, cardIndent+termpaint.Paint(text.pal.Dim, strings.Join(idle, summarySep)))
	}
	return lines
}

func idleAccounts(limits []AccountLimits, vendor string, text *renderer) []string {
	var idle []string
	for index := range limits {
		account := limits[index]
		if account.Vendor != vendor || (account.Status == LimitsReady && len(account.Limits) > 0) {
			continue
		}
		state := text.text(clitext.QuotaUnavailable, nil)
		if account.Status == LimitsFailed {
			state = text.text(clitext.QuotaFailed, nil)
		}
		idle = append(idle, accountName(account, text)+summarySep+state)
	}
	return idle
}

func windowsOf(limits []AccountLimits, vendor string) []windowKey {
	var keys []windowKey
	for index := range limits {
		if limits[index].Vendor != vendor || limits[index].Status != LimitsReady {
			continue
		}
		for _, limit := range limits[index].Limits {
			key := windowKey{window: limit.Window, scope: limit.Scope}
			if !slices.Contains(keys, key) {
				keys = append(keys, key)
			}
		}
	}
	return keys
}

func windowAccounts(limits []AccountLimits, vendor string, key windowKey) []windowAccount {
	var accounts []windowAccount
	for index := range limits {
		account := limits[index]
		if account.Vendor != vendor || account.Status != LimitsReady {
			continue
		}
		for _, limit := range account.Limits {
			if limit.Window == key.window && limit.Scope == key.scope {
				accounts = append(accounts, windowAccount{account: account, limit: limit})
				break
			}
		}
	}
	return accounts
}

type cardParts struct {
	left     []string
	segments [][2]string
}

func windowCard(accounts []windowAccount, now time.Time, text *renderer) cardParts {
	lead := leadAccount(accounts)
	pace := PaceOf(lead.limit, now)
	left := []string{
		termpaint.Paint(text.pal.Strong, capitalize(windowLabel(lead.limit, text))),
		text.text(clitext.QuotaCardLeft, clitext.Params{clitext.ParamRatio: limitPercent(lead.limit, text)}),
	}
	left = append(left[:1], cardPace(lead.limit, pace, left[1], text)...)
	segments := make([][2]string, 0, len(accounts))
	for index := range accounts {
		segments = append(segments, accountSegment(accounts[index], now, text))
	}
	return cardParts{left: left, segments: segments}
}

func cardPace(limit contracts.Limit, pace Pace, remaining string, text *renderer) []string {
	if Remaining(limit) <= 0 {
		return []string{remaining, termpaint.Paint(text.pal.Bad, text.text(clitext.QuotaExhausted, nil))}
	}
	if !pace.Shown {
		return []string{remaining}
	}
	delta := clitext.FormatNumber(text.lang, math.Abs(pace.Delta), 0)
	if pace.Delta > 0 {
		return []string{
			remaining + " " + termpaint.Paint(text.pal.Warn, paceUp),
			termpaint.Paint(text.pal.Warn, text.text(clitext.QuotaCardPace, clitext.Params{clitext.ParamDelta: "+" + delta})),
		}
	}
	return []string{remaining + " " + termpaint.Paint(text.pal.Good, paceDown)}
}

func leadAccount(accounts []windowAccount) windowAccount {
	for index := range accounts {
		if accounts[index].account.Default {
			return accounts[index]
		}
	}
	return accounts[0]
}

func accountSegment(entry windowAccount, now time.Time, text *renderer) [2]string {
	pace := PaceOf(entry.limit, now)
	head := termpaint.Paint(text.pal.Dim, accountMark) + accountName(entry.account, text) + " "
	bar := sizedBar(entry.limit, pace, text.pal, cardBarWidth)
	top := head + bar + " " + resetLabel(entry.limit, now, text)
	bottom := strings.Repeat(" ", termpaint.Width(head)) + termpaint.PadLeft(limitPercent(entry.limit, text), cardBarWidth)
	return [2]string{top, bottom}
}

func layoutCard(left []string, segments [][2]string, leftWidth, inner int) []string {
	segmentWidth := 0
	for _, segment := range segments {
		segmentWidth = max(segmentWidth, termpaint.Width(segment[0]), termpaint.Width(segment[1]))
	}
	perRow := max((inner-leftWidth+len(cardColumnGap))/(segmentWidth+len(cardColumnGap)), 1)
	var rows [][]string
	for start := 0; start < len(segments); start += perRow {
		chunk := segments[start:min(start+perRow, len(segments))]
		rows = append(rows, joinSegments(chunk, 0, segmentWidth), joinSegments(chunk, 1, segmentWidth))
	}
	lines := make([]string, max(len(rows), len(left)))
	for index := range lines {
		cell := ""
		if index < len(left) {
			cell = left[index]
		}
		line := termpaint.PadRight(cell, leftWidth)
		if index < len(rows) {
			line += strings.Join(rows[index], "")
		}
		lines[index] = strings.TrimRight(line, " ")
	}
	return lines
}

func joinSegments(chunk [][2]string, part, width int) []string {
	cells := make([]string, len(chunk))
	for index, segment := range chunk {
		cell := segment[part]
		if index < len(chunk)-1 {
			cell = termpaint.PadRight(cell, width) + cardColumnGap
		}
		cells[index] = cell
	}
	return cells
}

func boxed(cards [][]string, edges CardEdges, pal termpaint.Palette, inner int) []string {
	rule := strings.Repeat(edges.Horizontal, inner+cardPadCells)
	side := termpaint.Paint(pal.Dim, edges.Vertical)
	lines := []string{termpaint.Paint(pal.Dim, edges.TopLeft+rule+edges.TopRight)}
	for index, card := range cards {
		if index > 0 {
			lines = append(lines, termpaint.Paint(pal.Dim, edges.TeeLeft+rule+edges.TeeRight))
		}
		for _, line := range card {
			lines = append(lines, side+cardPad+termpaint.PadRight(line, inner)+cardPad+side)
		}
	}
	return append(lines, termpaint.Paint(pal.Dim, edges.BottomLeft+rule+edges.BottomRight))
}
