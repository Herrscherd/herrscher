package tui

import (
	"fmt"
	"strings"
	"time"
	"unicode"

	"github.com/charmbracelet/x/ansi"

	contracts "github.com/Herrscherd/herrscher-contracts"
)

const (
	todoPanelMax     = 6
	subagentPanelMax = 3
)

func todoPanel(items []contracts.TodoItem, width int, styles uiStyles) []string {
	if len(items) == 0 {
		return nil
	}
	done, active := 0, 0
	for index, it := range items {
		switch it.State {
		case stateDone:
			done++
		case "active":
			active = index
		}
	}
	head := styles.green.Render(truncate(fmt.Sprintf("  %s todos %d/%d", styles.glyphs.success, done, len(items)), width))
	if width < narrowStatus {
		return []string{head}
	}
	out := []string{head}
	start, end := paletteWindow(len(items), active, todoPanelMax)
	for _, it := range items[start:end] {
		out = append(out, todoRow(it, width, styles))
	}
	if hidden := len(items) - (end - start); hidden > 0 {
		out = append(out, styles.dim.Render(truncate(fmt.Sprintf("  %s +%d", styles.glyphs.notice, hidden), width)))
	}
	return out
}

func todoRow(todo contracts.TodoItem, width int, styles uiStyles) string {
	mark, style := styles.glyphs.notice, styles.dim
	switch todo.State {
	case stateDone:
		mark, style = styles.glyphs.success, styles.green
	case "active":
		mark, style = styles.glyphs.active, styles.accent
	}
	return style.Render(truncate("  "+mark+" "+oneLine(todo.Text), width))
}

func oneLine(s string) string {
	fields := strings.FieldsFunc(ansi.Strip(s), func(r rune) bool {
		return unicode.IsControl(r) || unicode.IsSpace(r)
	})
	return strings.Join(fields, " ")
}

func subagentPanel(agents []liveAgent, now time.Time, width int, styles uiStyles) []string {
	if len(agents) == 0 {
		return nil
	}
	mark := styles.glyphs.familyGlyph(familyAgent)
	if width < narrowStatus {
		label := fmt.Sprintf("  %s %d sous-agent", mark, len(agents))
		if len(agents) > 1 {
			label += "s"
		}
		return []string{styles.accent.Render(truncate(label, width))}
	}
	shown := agents
	if len(shown) > subagentPanelMax {
		shown = shown[:subagentPanelMax]
	}
	out := make([]string, 0, len(shown)+1)
	for _, area := range shown {
		cols := []string{oneLine(area.Name)}
		if area.Kind != "" {
			cols = append(cols, oneLine(area.Kind))
		}
		cols = append(cols, elapsed(now.Sub(area.since)))
		line := "  " + mark + " " + strings.Join(cols, "  ")
		out = append(out, styles.accent.Render(truncate(line, width)))
	}
	if hidden := len(agents) - len(shown); hidden > 0 {
		out = append(out, styles.dim.Render(truncate(fmt.Sprintf("  %s +%d", styles.glyphs.notice, hidden), width)))
	}
	return out
}

const secondsPerMinute = 60

func elapsed(d time.Duration) string {
	secs := int(d.Seconds())
	if secs < 0 {
		secs = 0
	}
	if secs < secondsPerMinute {
		return fmt.Sprintf("%ds", secs)
	}
	return fmt.Sprintf("%dm%02ds", secs/secondsPerMinute, secs%secondsPerMinute)
}
