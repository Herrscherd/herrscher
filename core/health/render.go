package health

import (
	"fmt"
	"io"
	"strings"

	contracts "github.com/Herrscherd/herrscher-contracts"
	"github.com/Herrscherd/herrscher-contracts/i18n"

	"github.com/Herrscherd/herrscher/core/clitext"
	"github.com/Herrscherd/herrscher/core/termpaint"
)

const (
	rowIndent      = "  "
	accountSep     = " · "
	defaultMarker  = "★"
	missingLatency = "-"
	summaryGap     = "        "
	detailCommand  = "herrscher health "
)

// Table writes the banner of the daemon, the checks grouped as the report lists
// them, one aligned line each, and the summary line that counts the checks per
// state and names the worst one when something fails. pal paints each part; the
// zero Palette writes plain text with the same alignment.
func (s Snapshot) Table(writer io.Writer, lang i18n.Language, pal termpaint.Palette) error {
	view, err := s.View(lang)
	if err != nil {
		return err
	}
	out := &lineWriter{writer: writer}
	out.line(termpaint.Paint(pal.Strong, view.Title) + " " + termpaint.Paint(pal.Dim, view.Banner))
	out.line("")
	table := &termpaint.Table{Right: map[int]bool{2: true}}
	table.Add(paintAll(pal.Dim, view.Header)...)
	for _, section := range view.Sections {
		table.Span(termpaint.Paint(pal.Group, section.Title))
		for index := range section.Rows {
			row := &section.Rows[index]
			table.Add(rowIndent+RowLabel(*row, pal), StateCell(*row, pal), latencyCell(*row, pal), termpaint.Paint(detailStyle(row.State, pal), row.Detail()))
		}
	}
	for _, line := range table.Lines() {
		out.line(line)
	}
	out.line("")
	out.line(SummaryLine(view, pal))
	return out.err
}

// Details writes the detail view of checks: a head line with the name, the
// latency and the facts of each one, then its state, cause and remedy on aligned
// rows.
func (s Snapshot) Details(writer io.Writer, checks []Check, lang i18n.Language, pal termpaint.Palette) error {
	rows, err := s.Rows(checks, lang)
	if err != nil {
		return err
	}
	text := &renderer{lang: lang}
	labels := [...]string{
		text.key(clitext.HealthDetailState, nil),
		text.key(clitext.HealthDetailCause, nil),
		text.key(clitext.HealthDetailRemedy, nil),
	}
	if text.err != nil {
		return text.err
	}
	out := &lineWriter{writer: writer}
	for index := range rows {
		row := &rows[index]
		if index > 0 {
			out.line("")
		}
		head := termpaint.Paint(pal.Strong, plainLabel(*row))
		if facts := joinNonEmpty(row.Latency, row.Facts); facts != "" {
			head += "  " + termpaint.Paint(pal.Dim, facts)
		}
		out.line(head)
		table := &termpaint.Table{}
		table.Add(rowIndent+labels[0], StateCell(*row, pal))
		if row.Cause != "" {
			table.Add(rowIndent+labels[1], termpaint.Paint(detailStyle(row.State, pal), row.Cause))
		}
		if row.Remedy != "" {
			table.Add(rowIndent+labels[2], row.Remedy)
		}
		for _, line := range table.Lines() {
			out.line(line)
		}
	}
	return out.err
}

// RowLabel paints the name of a row: the service, then its account after a dim
// separator, and the default marker of the default account.
func RowLabel(row Row, pal termpaint.Palette) string {
	label := row.Name
	if row.Account != "" {
		label += termpaint.Paint(pal.Dim, accountSep) + row.Account
	}
	if row.Default {
		label += " " + termpaint.Paint(pal.Accent, defaultMarker)
	}
	return label
}

// StateCell paints the glyph and the word of the state of a row in the color of
// that state.
func StateCell(row Row, pal termpaint.Palette) string {
	return termpaint.Paint(StateStyle(row.State, pal), row.Glyph+" "+row.StateText)
}

// StateStyle is the style of state in pal: good, warn, bad, or dim when off.
func StateStyle(state contracts.HealthState, pal termpaint.Palette) termpaint.Style {
	switch state {
	case contracts.HealthOK:
		return pal.Good
	case contracts.HealthDegraded:
		return pal.Warn
	case contracts.HealthDown:
		return pal.Bad
	case contracts.HealthOff:
		return pal.Dim
	}
	return pal.Dim
}

// SummaryLine paints the counts of view in the color of their state, and the
// command that details the worst check when something fails.
func SummaryLine(view View, pal termpaint.Palette) string {
	counts := make([]string, 0, len(view.Counts))
	for _, count := range view.Counts {
		counts = append(counts, termpaint.Paint(StateStyle(count.State, pal), count.Text))
	}
	line := strings.Join(counts, termpaint.Paint(pal.Dim, factsSep))
	if view.Worst != "" {
		line += summaryGap + termpaint.Paint(pal.Dim, view.Hint) + " " + detailCommand + view.Worst
	}
	return line
}

func plainLabel(row Row) string {
	return RowLabel(row, termpaint.Palette{})
}

func latencyCell(row Row, pal termpaint.Palette) string {
	if row.Latency == "" {
		return termpaint.Paint(pal.Dim, missingLatency)
	}
	return row.Latency
}

func detailStyle(state contracts.HealthState, pal termpaint.Palette) termpaint.Style {
	switch state {
	case contracts.HealthDegraded:
		return pal.Warn
	case contracts.HealthDown:
		return pal.Bad
	case contracts.HealthOK, contracts.HealthOff:
		return pal.Dim
	}
	return pal.Dim
}

func paintAll(style termpaint.Style, texts []string) []string {
	painted := make([]string, len(texts))
	for index, text := range texts {
		painted[index] = termpaint.Paint(style, text)
	}
	return painted
}

func joinNonEmpty(texts ...string) string {
	kept := texts[:0:0]
	for _, text := range texts {
		if text != "" {
			kept = append(kept, text)
		}
	}
	return strings.Join(kept, factsSep)
}

func groupKey(group Group) clitext.Key {
	switch group {
	case GroupCore:
		return clitext.HealthGroupCore
	case GroupGateways:
		return clitext.HealthGroupGateways
	case GroupBackends:
		return clitext.HealthGroupBackends
	case GroupCompaction:
		return clitext.HealthGroupCompaction
	case GroupMemory:
		return clitext.HealthGroupMemory
	case GroupKeys:
		return clitext.HealthGroupKeys
	}
	return ""
}

func stateGlyph(state contracts.HealthState) string {
	switch state {
	case contracts.HealthOK:
		return "●"
	case contracts.HealthDegraded:
		return "◐"
	case contracts.HealthDown:
		return "✕"
	case contracts.HealthOff:
		return "○"
	}
	return "○"
}

func stateKey(state contracts.HealthState) clitext.Key {
	switch state {
	case contracts.HealthOK:
		return clitext.HealthStateOK
	case contracts.HealthDegraded:
		return clitext.HealthStateDegraded
	case contracts.HealthDown:
		return clitext.HealthStateDown
	case contracts.HealthOff:
		return clitext.HealthStateOff
	}
	return clitext.HealthStateOff
}

type lineWriter struct {
	writer io.Writer
	err    error
}

func (w *lineWriter) line(text string) {
	if w.err == nil {
		_, w.err = fmt.Fprintln(w.writer, text)
	}
}

type renderer struct {
	lang i18n.Language
	err  error
}

func (r *renderer) key(key clitext.Key, params clitext.Params) string {
	value, err := clitext.Text(r.lang, key, params)
	r.keep(err)
	return value
}

func (r *renderer) notice(notice contracts.Notice) string {
	value, err := contracts.NewRenderer().Render(r.lang, notice)
	r.keep(err)
	return value
}

func (r *renderer) keep(err error) {
	if r.err == nil {
		r.err = err
	}
}
