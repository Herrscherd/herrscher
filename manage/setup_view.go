package manage

import (
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/Herrscherd/herrscher/core/clitext"
	"github.com/Herrscherd/herrscher/core/termpaint"
)

const (
	labelWidth = 26
	infoWidth  = 13
	rowIndent  = "  "
	pointer    = "▸ "
)

type setupStyles struct {
	tones   map[lineTone]lipgloss.Style
	title   lipgloss.Style
	active  lipgloss.Style
	hint    lipgloss.Style
	on      lipgloss.Style
	keyHint lipgloss.Style
}

func newSetupStyles() setupStyles {
	base := lipgloss.NewStyle()
	return setupStyles{
		tones: map[lineTone]lipgloss.Style{
			toneText:  base,
			toneDim:   base.Faint(true),
			toneBold:  base.Bold(true),
			toneGroup: base.Bold(true).Foreground(lipgloss.Color("14")),
			toneInfo:  base.Faint(true),
			toneOK:    base.Foreground(lipgloss.Color("10")),
			toneBad:   base.Foreground(lipgloss.Color("9")),
			toneWarn:  base.Foreground(lipgloss.Color("11")),
		},
		title:   base.Bold(true).Foreground(lipgloss.Color("13")),
		active:  base.Bold(true).Foreground(lipgloss.Color("13")),
		hint:    base.Faint(true),
		on:      base.Foreground(lipgloss.Color("10")),
		keyHint: base.Faint(true),
	}
}

func (model *setupModel) View() string {
	styles := newSetupStyles()
	var out strings.Builder
	out.WriteString("\n" + rowIndent + styles.title.Render("herrscher · "+model.text(clitext.SetupTitle, nil)))
	out.WriteString("  " + styles.hint.Render(model.text(model.screenSub(), nil)) + "\n\n")
	selected := -1
	if _, ok := model.current(); ok {
		selected = model.cursor
	}
	index := 0
	for _, row := range model.rows() {
		focused := false
		if row.kind != rowLine {
			focused = index == selected
			index++
		}
		out.WriteString(model.renderRow(styles, row, focused) + "\n")
	}
	out.WriteString("\n" + rowIndent + styles.keyHint.Render(model.keyHints()) + "\n")
	return out.String()
}

func (model *setupModel) renderRow(styles setupStyles, row setupRow, focused bool) string {
	lead := rowIndent + "  "
	if focused {
		lead = rowIndent + styles.active.Render(pointer)
	}
	switch row.kind {
	case rowLine:
		return model.renderLine(styles, row)
	case rowField:
		return lead + model.renderField(styles, row)
	case rowCheck, rowChoice:
		return lead + renderBox(styles, row) + " " + padded(styles, row, focused)
	case rowAction:
		if row.dot {
			return lead + styles.tones[row.tone].Render("●") + " " + padded(styles, row, focused)
		}
		return lead + padded(styles, row, focused)
	}
	return ""
}

func (model *setupModel) renderLine(styles setupStyles, row setupRow) string {
	if row.text == "" {
		return ""
	}
	text := styles.tones[row.tone].Render(safe(row.text))
	if row.hint == "" {
		return rowIndent + text
	}
	if row.tone == toneInfo {
		return rowIndent + pad(text, infoWidth) + " " + safe(row.hint)
	}
	return rowIndent + text + "  " + styles.hint.Render(safe(row.hint))
}

func renderBox(styles setupStyles, row setupRow) string {
	switch {
	case row.kind == rowCheck && row.on:
		return styles.on.Render("[x]")
	case row.kind == rowCheck:
		return "[ ]"
	case row.on:
		return styles.on.Render("(•)")
	}
	return "( )"
}

func padded(styles setupStyles, row setupRow, focused bool) string {
	label := safe(row.text)
	if focused {
		label = styles.active.Render(label)
	}
	if row.tag != "" {
		label += " " + styles.hint.Render(safe(row.tag))
	}
	if row.hint == "" {
		return label
	}
	return pad(label, labelWidth) + " " + styles.hint.Render(safe(row.hint))
}

func (model *setupModel) renderField(styles setupStyles, row setupRow) string {
	input := model.fields[row.field]
	view := pad(safe(row.text), labelWidth) + " › " + input.View()
	if row.hint == "" {
		return view
	}
	return view + "  " + styles.hint.Render(safe(row.hint))
}

func pad(text string, width int) string {
	if gap := width - lipgloss.Width(text); gap > 0 {
		return text + strings.Repeat(" ", gap)
	}
	return text
}

func safe(text string) string { return termpaint.Escape(text).String() }

func (model *setupModel) screenSub() clitext.Key {
	switch model.screen {
	case screenWelcome:
		return clitext.SetupWelcomeSub
	case screenModules:
		return clitext.SetupModulesSub
	case screenDiscord, screenTelegram:
		return clitext.SetupBotSub
	case screenClaude, screenCodex, screenCursor, screenGrok, screenAntigravity:
		return clitext.SetupBackendSub
	case screenObsidian:
		return clitext.SetupMemorySub
	case screenRoute:
		return clitext.SetupRouteSub
	case screenReview:
		return clitext.SetupReviewSub
	case screenInstall:
		return clitext.SetupInstallSub
	case screenConnections:
		return clitext.SetupConnectionsSub
	case screenHub:
		return clitext.SetupHubSub
	}
	return clitext.SetupHubSub
}

func (model *setupModel) keyHints() string {
	hints := []clitext.Key{clitext.SetupKeysMove}
	if model.screen == screenModules {
		hints = append(hints, clitext.SetupKeysToggle)
	}
	hints = append(hints, clitext.SetupKeysChoose)
	if model.screen != screenWelcome && model.screen != screenHub {
		hints = append(hints, clitext.SetupKeysBack)
	}
	hints = append(hints, clitext.SetupKeysQuit)
	parts := make([]string, 0, len(hints))
	for _, key := range hints {
		parts = append(parts, model.text(key, nil))
	}
	return strings.Join(parts, " · ")
}
