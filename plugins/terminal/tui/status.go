package tui

import (
	"fmt"
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
		return styles.dim
	}
}

func renderContext(used int, vendor string, styles uiStyles) string {
	if used <= 0 {
		return ""
	}
	limit := contextLimit(vendor)
	text := fmt.Sprintf("%s/%s", formatTokens(used), formatLimit(limit))
	return palierStyle(contextRatio(used, limit), styles).Render(text)
}

const compactAt = 0.92

func contextLeft(used, limit int) float64 {
	left := (compactAt - contextRatio(used, limit)) / compactAt
	if left < 0 {
		return 0
	}
	return left
}

const verboseStatus = 100

func renderGauge(used int, vendor string, width int, styles uiStyles) string {
	if used <= 0 {
		return ""
	}
	limit := contextLimit(vendor)
	row := contextRatio(used, limit)
	style := palierStyle(row, styles)
	label := fmt.Sprintf(" reste %d %%", int(contextLeft(used, limit)*percentScale))
	if width >= verboseStatus {
		label += " avant compaction"
	}
	return gauge(row, style, styles.dim) + style.Render(label)
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

func formatLimit(limit int) string {
	if limit >= 1000 && limit%1000 == 0 {
		return fmt.Sprintf("%dk", limit/thousand)
	}
	return formatTokens(limit)
}

func (m *model) statusBar(tab *tab, width int) string {
	info, known := m.activeInfo()
	segs := []string{m.modeChip(tab, width), m.statusHead(tab, info, known, width)}
	if width >= narrowStatus && known {
		if label := modelLabel(info); label != "" {
			segs = append(segs, m.st.text.Render(label))
		}
	}
	used := m.contextTokens(tab)
	if ctx := renderContext(used, info.Vendor, m.st); ctx != "" {
		segs = append(segs, ctx)
	}
	if width >= narrowStatus {
		segs = append(segs, m.statusDetails(tab, info, known, used, width)...)
	}
	return strings.Join(segs, m.st.sep)
}

func (m *model) statusHead(tab *tab, info contracts.SessionInfo, known bool, width int) string {
	if known && info.Vendor != "" && width >= narrowStatus {
		return m.st.accent.Render(glyphBolt+" "+info.Vendor) + m.st.dim.Render(" "+tab.label)
	}
	return m.st.accent.Render(glyphBolt + " " + tab.label)
}

func (m *model) statusDetails(tab *tab, info contracts.SessionInfo, known bool, used, width int) []string {
	var segs []string
	if g := renderGauge(used, info.Vendor, width, m.st); g != "" {
		segs = append(segs, g)
	}
	if known && info.Project != "" {
		segs = append(segs, m.st.dim.Render(info.Project))
	}
	if ms := m.mouseStatus(); ms != "" {
		segs = append(segs, ms)
	}
	if tab.costTotal > 0 {
		segs = append(segs, m.st.dim.Render(formatCost(tab.costTotal)))
	}
	if !tab.openedAt.IsZero() {
		segs = append(segs, m.st.dim.Render(formatDuration(time.Since(tab.openedAt))))
	}
	return segs
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
		ratio := contextRatio(used, limit)
		lines = append(lines,
			fmt.Sprintf("  context  %s / %s  (%d%%)", formatTokens(used), formatLimit(limit), int(ratio*percentScale)),
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
