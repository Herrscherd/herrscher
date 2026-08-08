package tui

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
)

// The status bar is what the terminal knows about the session and had been
// throwing away: how much of the context window is spent, what the session has
// cost so far, how long it has been open. All of it already rides the bus.

// contextLimits is the context window per vendor, in tokens. The wire does not
// carry the window size — only the counts that fill it — so this is a constant we
// keep honest by hand, and override by env when a model moves under us.
var contextLimits = map[string]int{
	"claude": 200_000,
	"codex":  400_000,
	"cursor": 200_000,
}

// defaultContextLimit is used for an unknown or empty vendor: showing a plausible
// denominator beats showing none, and the numerator is the true measurement.
const defaultContextLimit = 200_000

// contextLimitEnv overrides every vendor's window, for a model whose context
// changed between releases.
const contextLimitEnv = "HERRSCHER_CONTEXT_LIMIT"

// contextLimit is the context window to measure a vendor's occupancy against.
func contextLimit(vendor string) int {
	if v := os.Getenv(contextLimitEnv); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			return n
		}
	}
	if n, ok := contextLimits[strings.ToLower(vendor)]; ok {
		return n
	}
	return defaultContextLimit
}

// The occupancy paliers. Below warn the window is a non-issue and reads dim;
// from warn on it is worth planning around; from alarm on a compaction is
// imminent and the line takes the Herrscher red.
const (
	ctxWarn  = 0.80
	ctxAlarm = 0.95
)

// narrowStatus is the width below which the status bar drops to session and
// context only. Everything else is useful; those two are necessary.
const narrowStatus = 60

// contextRatio is how full a window of the given size is, in [0,1].
func contextRatio(used, limit int) float64 {
	if used <= 0 || limit <= 0 {
		return 0
	}
	if r := float64(used) / float64(limit); r < 1 {
		return r
	}
	return 1
}

// palierStyle is the style a given occupancy is drawn in.
func palierStyle(ratio float64) lipgloss.Style {
	switch {
	case ratio >= ctxAlarm:
		return redStyle
	case ratio >= ctxWarn:
		return warnStyle
	default:
		return dimStyle
	}
}

// renderContext renders the occupancy as `42k/200k`, coloured by palier. The
// limit is resolved once: it reads the environment, and this runs every frame.
func renderContext(used int, vendor string) string {
	if used <= 0 {
		return ""
	}
	limit := contextLimit(vendor)
	text := fmt.Sprintf("%s/%s", formatTokens(used), formatLimit(limit))
	return palierStyle(contextRatio(used, limit)).Render(text)
}

// renderGauge renders the occupancy bar and its percentage. It says the same
// thing as renderContext and that is the point: the numbers are the measurement,
// the bar is what is read without reading. Below narrowStatus it is dropped —
// two ways of saying one thing is the first luxury a cramped bar gives up.
func renderGauge(used int, vendor string) string {
	if used <= 0 {
		return ""
	}
	r := contextRatio(used, contextLimit(vendor))
	style := palierStyle(r)
	return gauge(r, style) + style.Render(fmt.Sprintf(" %d%%", int(r*100)))
}

// formatLimit renders a context window: a round thousand loses its decimal, so
// the denominator reads as the constant it is ("200k", not "200.0k").
func formatLimit(n int) string {
	if n >= 1000 && n%1000 == 0 {
		return fmt.Sprintf("%dk", n/1000)
	}
	return formatTokens(n)
}

// statusBar is the idle footer for a tab: who this session is, how full its
// context is, what it has cost and how long it has been open. Under narrowStatus
// columns it keeps the session and the context and drops the rest.
// It reads the hub's record for the *active* session, so it is the active tab's
// bar; the footer is its only caller.
func (m *model) statusBar(tb *tab, width int) string {
	info, known := m.activeInfo()

	// The vendor leads, marked, because it is the one fact that changes what every
	// other number on the bar means: a cost and a context are read against the
	// model that produced them.
	head := accentStyle.Render(glyphBolt + " " + tb.label)
	if known && info.Vendor != "" && width >= narrowStatus {
		head = accentStyle.Render(glyphBolt+" "+info.Vendor) + dimStyle.Render(" "+tb.label)
	}
	segs := []string{head}
	if width >= narrowStatus && known && info.Project != "" {
		segs = append(segs, dimStyle.Render(info.Project))
	}
	if ctx := renderContext(tb.ctxTokens, info.Vendor); ctx != "" {
		segs = append(segs, ctx)
	}
	if width >= narrowStatus {
		if g := renderGauge(tb.ctxTokens, info.Vendor); g != "" {
			segs = append(segs, g)
		}
		if ms := m.mouseStatus(); ms != "" {
			segs = append(segs, ms)
		}
		if tb.costTotal > 0 {
			segs = append(segs, dimStyle.Render(formatCost(tb.costTotal)))
		}
		if !tb.openedAt.IsZero() {
			segs = append(segs, dimStyle.Render(formatDuration(time.Since(tb.openedAt))))
		}
	}
	return strings.Join(segs, statusSep)
}

// usageReport is what /usage answers: the status bar's numbers, spelled out. The
// bar has to fit on one row and so says everything by abbreviation; a session
// worth asking about is one where the operator wants the counts named — which
// window, how full, what it has cost, how long it has run.
//
// It writes into the transcript rather than an overlay because it is a reading
// taken at a moment: an overlay would keep showing that moment after it passed,
// and scrolling back to compare two readings is the point of asking twice.
func (m *model) usageReport(tb *tab) string {
	info, known := m.activeInfo()
	vendor := ""
	if known {
		vendor = info.Vendor
	}
	limit := contextLimit(vendor)
	lines := []string{"usage — " + tb.label}
	if vendor != "" {
		lines = append(lines, "  model    "+vendor)
	}
	if known && info.Project != "" {
		lines = append(lines, "  project  "+info.Project)
	}
	if tb.ctxTokens > 0 {
		r := contextRatio(tb.ctxTokens, limit)
		lines = append(lines, fmt.Sprintf("  context  %s / %s  (%d%%)", formatTokens(tb.ctxTokens), formatLimit(limit), int(r*100)))
	} else {
		lines = append(lines, "  context  no measurement yet")
	}
	if tb.costTotal > 0 {
		lines = append(lines, "  cost     "+formatCost(tb.costTotal)+"  since this tab opened")
	}
	if !tb.openedAt.IsZero() {
		lines = append(lines, "  open     "+formatDuration(time.Since(tb.openedAt)))
	}
	return strings.Join(lines, "\n")
}

// statusSep divides the bar's segments. A pipe rather than the interpunct the
// bar used: the segments are now measurements of different kinds sitting side by
// side, and a rule between them reads as a table where a dot reads as a list.
var statusSep = dimStyle.Render(" │ ")

// formatDuration renders a session's age compactly: seconds under a minute,
// minutes under an hour, hours and minutes beyond.
func formatDuration(d time.Duration) string {
	switch {
	case d < time.Minute:
		return fmt.Sprintf("%ds", int(d.Seconds()))
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	default:
		return fmt.Sprintf("%dh%02dm", int(d.Hours()), int(d.Minutes())%60)
	}
}
