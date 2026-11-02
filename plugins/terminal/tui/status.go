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

func contextLimits() map[string]int {
	return map[string]int{
		"claude": 200_000,
		"codex":  400_000,
		"cursor": 200_000,
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

func palierStyle(ratio float64, st uiStyles) lipgloss.Style {
	switch {
	case ratio >= ctxAlarm:
		return st.red
	case ratio >= ctxWarn:
		return st.warn
	default:
		return st.dim
	}
}

func renderContext(used int, vendor string, st uiStyles) string {
	if used <= 0 {
		return ""
	}
	limit := contextLimit(vendor)
	text := fmt.Sprintf("%s/%s", formatTokens(used), formatLimit(limit))
	return palierStyle(contextRatio(used, limit), st).Render(text)
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

func renderGauge(used int, vendor string, width int, st uiStyles) string {
	if used <= 0 {
		return ""
	}
	limit := contextLimit(vendor)
	r := contextRatio(used, limit)
	style := palierStyle(r, st)
	label := fmt.Sprintf(" reste %d %%", int(contextLeft(used, limit)*100))
	if width >= verboseStatus {
		label += " avant compaction"
	}
	return gauge(r, style, st.dim) + style.Render(label)
}

func modelLabel(info contracts.SessionInfo) string {
	model := info.Model
	if info.Vendor != "" {
		model = strings.TrimPrefix(model, strings.ToLower(info.Vendor)+"-")
	}
	return joinNonEmpty(model, info.Effort)
}

func (m *model) contextTokens(tb *tab) int {
	if tb.ctxTokens > 0 {
		return tb.ctxTokens
	}
	if info, ok := m.activeInfo(); ok {
		return info.ContextTokens
	}
	return 0
}

func formatLimit(n int) string {
	if n >= 1000 && n%1000 == 0 {
		return fmt.Sprintf("%dk", n/1000)
	}
	return formatTokens(n)
}

func (m *model) statusBar(tb *tab, width int) string {
	info, known := m.activeInfo()

	head := m.st.accent.Render(glyphBolt + " " + tb.label)
	if known && info.Vendor != "" && width >= narrowStatus {
		head = m.st.accent.Render(glyphBolt+" "+info.Vendor) + m.st.dim.Render(" "+tb.label)
	}
	segs := []string{head}
	if width >= narrowStatus && known {
		if label := modelLabel(info); label != "" {
			segs = append(segs, m.st.text.Render(label))
		}
	}
	used := m.contextTokens(tb)
	if ctx := renderContext(used, info.Vendor, m.st); ctx != "" {
		segs = append(segs, ctx)
	}
	if width >= narrowStatus {
		if g := renderGauge(used, info.Vendor, width, m.st); g != "" {
			segs = append(segs, g)
		}
		if known && info.Project != "" {
			segs = append(segs, m.st.dim.Render(info.Project))
		}
		if ms := m.mouseStatus(); ms != "" {
			segs = append(segs, ms)
		}
		if tb.costTotal > 0 {
			segs = append(segs, m.st.dim.Render(formatCost(tb.costTotal)))
		}
		if !tb.openedAt.IsZero() {
			segs = append(segs, m.st.dim.Render(formatDuration(time.Since(tb.openedAt))))
		}
	}
	return strings.Join(segs, m.st.sep)
}

func (m *model) usageReport(tb *tab) string {
	info, known := m.activeInfo()
	vendor := ""
	if known {
		vendor = info.Vendor
	}
	limit := contextLimit(vendor)
	lines := []string{"usage · " + tb.label}
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
	if used := m.contextTokens(tb); used > 0 {
		r := contextRatio(used, limit)
		lines = append(lines,
			fmt.Sprintf("  context  %s / %s  (%d%%)", formatTokens(used), formatLimit(limit), int(r*100)),
			fmt.Sprintf("  reste    %d %% avant compaction", int(contextLeft(used, limit)*100)),
		)
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
