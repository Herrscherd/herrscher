package tui

import (
	"fmt"
	"strings"
	"time"
	"unicode"

	contracts "github.com/Herrscherd/herrscher-contracts"
	"github.com/charmbracelet/x/ansi"
)

const (
	todoPanelMax     = 6
	subagentPanelMax = 3
)

func todoPanel(items []contracts.TodoItem, width int) []string {
	if len(items) == 0 {
		return nil
	}
	done, active := 0, 0
	for i, it := range items {
		switch it.State {
		case "done":
			done++
		case "active":
			active = i
		}
	}
	head := greenStyle.Render(truncate(fmt.Sprintf("  ✓ todos %d/%d", done, len(items)), width))
	if width < narrowStatus {
		return []string{head}
	}
	out := []string{head}
	start, end := paletteWindow(len(items), active, todoPanelMax)
	for _, it := range items[start:end] {
		out = append(out, todoRow(it, width))
	}
	if hidden := len(items) - (end - start); hidden > 0 {
		out = append(out, dimStyle.Render(truncate(fmt.Sprintf("  · +%d", hidden), width)))
	}
	return out
}

func todoRow(it contracts.TodoItem, width int) string {
	mark, style := "·", dimStyle
	switch it.State {
	case "done":
		mark, style = "✓", greenStyle
	case "active":
		mark, style = "◆", accentStyle
	}
	return style.Render(truncate("  "+mark+" "+oneLine(it.Text), width))
}

func oneLine(s string) string {
	fields := strings.FieldsFunc(ansi.Strip(s), func(r rune) bool {
		return unicode.IsControl(r) || unicode.IsSpace(r)
	})
	return strings.Join(fields, " ")
}

func subagentPanel(agents []liveAgent, now time.Time, width int) []string {
	if len(agents) == 0 {
		return nil
	}
	mark := familyGlyph(familyAgent)
	if width < narrowStatus {
		label := fmt.Sprintf("  %s %d sous-agent", mark, len(agents))
		if len(agents) > 1 {
			label += "s"
		}
		return []string{accentStyle.Render(truncate(label, width))}
	}
	shown := agents
	if len(shown) > subagentPanelMax {
		shown = shown[:subagentPanelMax]
	}
	out := make([]string, 0, len(shown)+1)
	for _, a := range shown {
		cols := []string{oneLine(a.Name)}
		if a.Kind != "" {
			cols = append(cols, oneLine(a.Kind))
		}
		cols = append(cols, elapsed(now.Sub(a.since)))
		line := "  " + mark + " " + strings.Join(cols, "  ")
		out = append(out, accentStyle.Render(truncate(line, width)))
	}
	if hidden := len(agents) - len(shown); hidden > 0 {
		out = append(out, dimStyle.Render(truncate(fmt.Sprintf("  · +%d", hidden), width)))
	}
	return out
}

func elapsed(d time.Duration) string {
	secs := int(d.Seconds())
	if secs < 0 {
		secs = 0
	}
	if secs < 60 {
		return fmt.Sprintf("%ds", secs)
	}
	return fmt.Sprintf("%dm%02ds", secs/60, secs%60)
}
