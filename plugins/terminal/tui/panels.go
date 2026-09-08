package tui

import (
	"fmt"
	"strings"
	"time"

	contracts "github.com/Herrscherd/herrscher-contracts"
)

const todoPanelMax = 6

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
	return style.Render(truncate("  "+mark+" "+it.Text, width))
}

func subagentPanel(agents []liveAgent, now time.Time, width int) []string {
	if len(agents) == 0 {
		return nil
	}
	out := make([]string, 0, len(agents))
	for _, a := range agents {
		cols := []string{a.Name}
		if a.Kind != "" {
			cols = append(cols, a.Kind)
		}
		cols = append(cols, fmt.Sprintf("%ds", int(now.Sub(a.since).Seconds())))
		line := "  " + familyGlyph(familyAgent) + " " + strings.Join(cols, "  ")
		out = append(out, accentStyle.Render(truncate(line, width)))
	}
	return out
}
