package tui

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
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

// renderContext renders the occupancy as `42k/200k`, coloured by palier. The
// limit is resolved once: it reads the environment, and this runs every frame.
func renderContext(used int, vendor string) string {
	if used <= 0 {
		return ""
	}
	limit := contextLimit(vendor)
	text := fmt.Sprintf("%s/%s", formatTokens(used), formatLimit(limit))
	switch r := contextRatio(used, limit); {
	case r >= ctxAlarm:
		return redStyle.Render(text)
	case r >= ctxWarn:
		return warnStyle.Render(text)
	default:
		return dimStyle.Render(text)
	}
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

	segs := []string{dimStyle.Render(tb.label)}
	if width >= narrowStatus && known {
		for _, f := range []string{info.Project, info.Vendor} {
			if f != "" {
				segs = append(segs, dimStyle.Render(f))
			}
		}
	}
	if ctx := renderContext(tb.ctxTokens, info.Vendor); ctx != "" {
		segs = append(segs, ctx)
	}
	if width >= narrowStatus {
		if tb.costTotal > 0 {
			segs = append(segs, dimStyle.Render(formatCost(tb.costTotal)))
		}
		if !tb.openedAt.IsZero() {
			segs = append(segs, dimStyle.Render(formatDuration(time.Since(tb.openedAt))))
		}
	}
	return strings.Join(segs, dimStyle.Render(" · "))
}

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
