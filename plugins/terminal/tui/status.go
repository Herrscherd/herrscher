package tui

import (
	"fmt"
	"math"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"

	contracts "github.com/Herrscherd/herrscher-contracts"
)

const (
	contextWindowStandard = 200_000
	contextWindowLarge    = 400_000
	percentScale          = 100
	thousand              = 1000
	minutesPerHour        = 60
)

func contextLimits() map[string]int {
	return map[string]int{
		"claude": contextWindowStandard,
		"codex":  contextWindowLarge,
		"cursor": contextWindowStandard,
	}
}

const defaultContextLimit = 200_000

const contextLimitEnv = "HERRSCHER_CONTEXT_LIMIT"

func contextLimit(vendor string) int {
	if v := os.Getenv(contextLimitEnv); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			return n
		}
	}
	if n, ok := contextLimits()[strings.ToLower(vendor)]; ok {
		return n
	}
	return defaultContextLimit
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
		return styles.accent
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

func contextPercent(used, limit int) string {
	return fmt.Sprintf("%d%%", int(contextRatio(used, limit)*percentScale))
}

const contextCells = 10

func contextGauge(ratio float64, cells int, styles uiStyles) string {
	filled := int(math.Round(ratio * float64(cells)))
	filled = max(0, min(cells, filled))
	return palierStyle(ratio, styles).Render(strings.Repeat(gaugeFull, filled)) +
		styles.dim.Render(strings.Repeat(gaugeEmpty, cells-filled))
}

const compactAt = 0.92

func contextLeft(used, limit int) float64 {
	left := (compactAt - contextRatio(used, limit)) / compactAt
	if left < 0 {
		return 0
	}
	return left
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
			fmt.Sprintf("  reste    %d %% avant compaction", int(contextLeft(used, limit)*percentScale)),
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
