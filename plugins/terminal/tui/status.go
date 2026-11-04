package tui

import (
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"

	contracts "github.com/Herrscherd/herrscher-contracts"
	"github.com/Herrscherd/herrscher/core/contextwindow"
)

const (
	percentScale   = 100
	thousand       = 1000
	minutesPerHour = 60
)

func contextLimit(vendor string) int {
	return contextwindow.Limit(vendor)
}

const (
	ctxWarn  = 0.80
	ctxAlarm = 0.95
)

const narrowStatus = 60

func contextRatio(used, limit int) float64 {
	if used <= 0 || limit <= 0 {
		return 0
	}
	if r := float64(used) / float64(limit); r < 1 {
		return r
	}
	return 1
}

func palierStyle(ratio float64, styles uiStyles) lipgloss.Style {
	switch {
	case ratio >= ctxAlarm:
		return styles.red
	case ratio >= ctxWarn:
		return styles.warn
	default:
		return styles.dim
	}
}

func formatContextTokens(tokens int) string {
	if tokens < thousand {
		return strconv.Itoa(tokens)
	}
	return strconv.Itoa(int(math.Round(float64(tokens)/thousand))) + "k"
}

func formatContext(used, limit int) string {
	return formatContextTokens(used) + "/" + formatContextTokens(limit)
}

func formatLimit(limit int) string {
	if limit >= 1000 && limit%1000 == 0 {
		return fmt.Sprintf("%dk", limit/thousand)
	}
	return formatTokens(limit)
}

func contextPercent(used, limit int) string {
	return fmt.Sprintf("%d%%", int(contextRatio(used, limit)*percentScale))
}

const (
	contextCells    = 16
	barContextCells = 8
)

func contextGauge(ratio float64, styles uiStyles) string {
	return contextGaugeCells(ratio, contextCells, styles)
}

func contextGaugeCells(ratio float64, cells int, styles uiStyles) string {
	filled := max(0, min(cells, int(ratio*float64(cells))))
	fill := styles.accent
	if ratio >= ctxWarn {
		fill = palierStyle(ratio, styles)
	}
	return fill.Render(strings.Repeat(styles.glyphs.gaugeFull, filled)) +
		styles.dim.Render(strings.Repeat(styles.glyphs.gaugeEmpty, cells-filled))
}

func compactThresholdLabel(percent int) string {
	if percent == 0 {
		return "pas de compaction auto"
	}
	return fmt.Sprintf("compacte seul à %d%%", percent)
}

func contextLeft(used, limit, percent int) float64 {
	if percent <= 0 {
		return 0
	}
	threshold := float64(percent) / percentScale
	left := (threshold - contextRatio(used, limit)) / threshold
	if left < 0 {
		return 0
	}
	return left
}

func contextRemainingLabel(used, limit, percent int) string {
	if percent == 0 {
		return "pas de compaction auto"
	}
	return fmt.Sprintf("%d %% avant compaction", int(contextLeft(used, limit, percent)*percentScale))
}

func modelLabel(info contracts.SessionInfo) string {
	model := info.Model
	if info.Vendor != "" {
		model = strings.TrimPrefix(model, strings.ToLower(info.Vendor)+"-")
	}
	return joinNonEmpty(model, info.Effort)
}

func (m *model) contextTokens(tab *tab) int {
	if tab.ctxTokens > 0 {
		return tab.ctxTokens
	}
	if info, ok := m.activeInfo(); ok {
		return info.ContextTokens
	}
	return 0
}

func (m *model) usageReport(tab *tab) string {
	info, known := m.activeInfo()
	vendor := ""
	if known {
		vendor = info.Vendor
	}
	limit := contextLimit(vendor)
	lines := []string{"usage · " + tab.label}
	if vendor != "" {
		lines = append(lines, "  vendor   "+vendor)
	}
	if known && info.Model != "" {
		lines = append(lines, "  model    "+info.Model)
	}
	if known && info.Effort != "" {
		lines = append(lines, "  effort   "+info.Effort)
	}
	if known && info.Project != "" {
		lines = append(lines, "  project  "+info.Project)
	}
	if used := m.contextTokens(tab); used > 0 {
		lines = append(lines,
			fmt.Sprintf("  context  %s  (%s)", formatContext(used, limit), contextPercent(used, limit)),
			"  reste    "+contextRemainingLabel(used, limit, info.AutoCompactPercent),
		)
	} else {
		lines = append(lines, "  context  no measurement yet")
	}
	if tab.costTotal > 0 {
		lines = append(lines, "  cost     "+formatCost(tab.costTotal)+"  since this tab opened")
	}
	if !tab.openedAt.IsZero() {
		lines = append(lines, "  open     "+formatDuration(time.Since(tab.openedAt)))
	}
	return strings.Join(lines, "\n")
}

func formatDuration(span time.Duration) string {
	switch {
	case span < time.Minute:
		return fmt.Sprintf("%ds", int(span.Seconds()))
	case span < time.Hour:
		return fmt.Sprintf("%dm", int(span.Minutes()))
	default:
		return fmt.Sprintf("%dh%02dm", int(span.Hours()), int(span.Minutes())%minutesPerHour)
	}
}
