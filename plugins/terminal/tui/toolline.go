package tui

import (
	"fmt"
	"strings"
	"time"
	"unicode"

	"github.com/charmbracelet/lipgloss"
)

func toolFamilies() map[string]string {
	return map[string]string{
		"read":         familyRead,
		"glob":         familyRead,
		"grep":         familySearch,
		"websearch":    familySearch,
		"webfetch":     familyWeb,
		"edit":         familyWrite,
		"write":        familyWrite,
		"notebookedit": familyWrite,
		"bash":         familyRun,
		"task":         familyAgent,
		"agent":        familyAgent,
		"skill":        familyAgent,
	}
}

const (
	familyRead   = "read"
	familyWrite  = "write"
	familyRun    = "run"
	familySearch = "search"
	familyWeb    = "web"
	familyAgent  = "agent"
)

func toolVerb(text string) (string, string) {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return "", ""
	}
	head, tail, _ := strings.Cut(trimmed, " ")
	fam, ok := toolFamilies()[strings.ToLower(head)]
	if !ok {
		return "", trimmed
	}
	return fam, strings.TrimSpace(tail)
}

func toolName(text string) string {
	head, _, _ := strings.Cut(strings.TrimSpace(text), " ")
	runes := []rune(head)
	if len(runes) > 0 {
		runes[0] = unicode.ToUpper(runes[0])
	}
	return string(runes)
}

const toolVerbWidth = 6

const toolLineCap = 2

const agentNameWidth = 10

func resultGutter(glyphs glyphSet) string {
	return "    " + glyphs.result + " "
}

const (
	pathDepth = 4
	pathKeep  = 2
)

const (
	stateTimeout = "timeout"
)

func renderTool(item entry, width int, display view) string {
	lines := strings.Split(item.text, "\n")
	family, target := toolVerb(lines[0])
	name := ""
	if family != "" {
		name = toolName(lines[0])
	}
	out := []string{toolHead(item, family, name, target, width, display)}
	if item.open {
		out = append(out, toolBody(lines[1:], target, width, display)...)
	}
	return strings.Join(out, "\n")
}

const diffGutter = "    "

func toolBody(results []string, target string, width int, display view) []string {
	if !isDiffBody(results) {
		return toolResults(results, width, display)
	}
	out := []string{diffGutter + truncate(diffHeader(target, results, display), width-len(diffGutter))}
	for _, row := range diffRows(results, width-len(diffGutter), display) {
		out = append(out, diffGutter+row)
	}
	return append(out, diffGutter+display.st.dim.Render("╰"))
}

func familyStyle(styles uiStyles, family string) lipgloss.Style {
	switch family {
	case familyWrite:
		return styles.warn
	case familyAgent:
		return styles.violet
	}
	return styles.accent
}

func toolHead(item entry, family, name, target string, width int, display view) string {
	mark := familyStyle(display.st, family)
	stateMark := display.st.green.Render(display.st.glyphs.success)
	switch {
	case item.failed:
		mark = display.st.red
		stateMark = display.st.red.Render(display.st.glyphs.failure)
	case item.live:
		stateMark = display.st.spinner.Render(display.spin)
	}
	lefts := []string{toolLeft(mark, family, name, target, display)}
	if short := abbreviatePaths(target); short != target {
		lefts = append(lefts, toolLeft(mark, family, name, short, display))
	}
	arrow := ""
	if item.foldable {
		arrow = " " + display.st.dim.Render(display.st.glyphs.fold)
		if item.open {
			arrow = " " + display.st.dim.Render(display.st.glyphs.expanded)
		}
	}
	rights := []string{stateMark + arrow}
	if item.span > 0 || item.live {
		rights = append([]string{display.st.dim.Render(formatSpan(item.span)) + " " + stateMark + arrow}, rights...)
	}
	return spreadRight(lefts, rights, width)
}

func toolLeft(mark lipgloss.Style, family, name, target string, display view) string {
	left := mark.Render(display.st.glyphs.tool)
	if family == "" {
		return left + " " + display.st.text.Render(target)
	}
	left += " " + display.st.strong.Render(padCells(name, toolVerbWidth))
	if target != "" {
		left += " " + display.st.dim.Render(target)
	}
	return left
}

func spreadRight(lefts, rights []string, width int) string {
	for _, left := range lefts {
		for _, right := range rights {
			if gap := width - lipgloss.Width(left) - lipgloss.Width(right); gap >= 1 {
				return left + strings.Repeat(" ", gap) + right
			}
		}
	}
	right := rights[len(rights)-1]
	room := width - lipgloss.Width(right) - 1
	if room < 1 {
		return truncate(right, width)
	}
	return truncate(lefts[len(lefts)-1], room) + " " + right
}

func formatSpan(span time.Duration) string {
	if span < 10*time.Second {
		return fmt.Sprintf("%.1fs", span.Seconds())
	}
	return elapsed(span)
}

func toolResults(results []string, width int, display view) []string {
	shown := len(results)
	if !display.expand {
		shown = min(shown, toolLineCap)
	}
	out := make([]string, 0, shown+1)
	for _, line := range results[:shown] {
		out = append(out, truncate(display.st.dim.Render(resultGutter(display.st.glyphs)+line), width))
	}
	if hidden := len(results) - shown; hidden > 0 {
		out = append(out, truncate(display.st.dim.Render(resultGutter(display.st.glyphs)+fmt.Sprintf("%s +%d", display.st.glyphs.moreLines, hidden)), width))
	}
	return out
}

func abbreviatePaths(s string) string {
	fields := strings.Split(s, " ")
	for index, f := range fields {
		fields[index] = abbreviatePath(f)
	}
	return strings.Join(fields, " ")
}

func abbreviatePath(token string) string {
	cut := len(token) - len(strings.TrimLeft(token, `"'(`))
	opening, rest := token[:cut], token[cut:]
	trimmed := strings.TrimRight(rest, `;,)"'`)
	closing := rest[len(trimmed):]

	body, ok := strings.CutPrefix(trimmed, "/")
	if !ok {
		if body, ok = strings.CutPrefix(trimmed, "~/"); !ok {
			return token
		}
	}
	segs := strings.Split(body, "/")
	if n := len(segs); n > 0 && segs[n-1] == "" {
		segs = segs[:n-1]
	}
	if len(segs) < pathDepth {
		return token
	}
	return opening + "…/" + strings.Join(segs[len(segs)-pathKeep:], "/") + closing
}

func subagentRows(agents []liveAgent, now time.Time, width int, display view) []string {
	out := make([]string, 0, len(agents))
	for index, agent := range agents {
		branch := display.st.glyphs.branchMid
		if index == len(agents)-1 {
			branch = display.st.glyphs.branchEnd
		}
		mark, note := subagentMark(agent, display)
		left := display.st.dim.Render("    "+branch+" ") + mark + " " + display.st.text.Render(padCells(oneLine(agent.Name), agentNameWidth))
		if kind := oneLine(agent.Kind); kind != "" {
			left += " " + display.st.dim.Render(kind)
		}
		if note != "" {
			left += " " + display.st.red.Render(note)
		}
		end := now
		if !agent.ended.IsZero() {
			end = agent.ended
		}
		out = append(out, spreadRight([]string{left}, []string{display.st.dim.Render(elapsed(end.Sub(agent.since)))}, width))
	}
	return out
}

func subagentMark(agent liveAgent, display view) (string, string) {
	switch agent.State {
	case stateDone:
		return display.st.green.Render(display.st.glyphs.success), ""
	case stateTimeout:
		return display.st.red.Render(display.st.glyphs.failure), display.tr.t("tui.render.subagent_retry", nil)
	case stateFailed:
		return display.st.red.Render(display.st.glyphs.failure), ""
	}
	return display.st.spinner.Render(display.spin), ""
}

func renderThinking(item entry, width int, display view) string {
	arrow, verb := display.st.glyphs.fold, display.tr.t("tui.render.unfold", nil)
	if item.open {
		arrow, verb = display.st.glyphs.expanded, display.tr.t("tui.render.fold", nil)
	}
	label := display.st.dim.Render(arrow+" ") + display.st.text.Italic(true).Render(display.tr.t("tui.render.thinking", nil)) + display.st.dim.Render(" · "+formatSpan(item.span))
	if item.tokens > 0 {
		label += display.st.dim.Render(" · " + formatTokens(item.tokens) + " " + display.tr.t("tui.render.tokens_unit", nil))
	}
	head := spreadRight([]string{label}, []string{display.st.dim.Render(joinNonEmpty(arrow+" "+verb, display.glyph(actToggleTranscript)))}, width)
	if !item.open {
		return head
	}
	lines := []string{head}
	for _, ln := range strings.Split(wrapWith(display.st.violet.Italic(true), item.text, max(1, width-len(resultGutter(display.st.glyphs)))), "\n") {
		lines = append(lines, "    "+ln)
	}
	return strings.Join(lines, "\n")
}

func busyLine(trans translator, width int, styles uiStyles, spin, elapsedText, tokens, cost, interrupt string) string {
	parts := []string{elapsedText}
	if tokens != "" {
		parts = append(parts, "↑ "+tokens)
	}
	if cost != "" {
		parts = append(parts, cost)
	}
	left := styles.spinner.Render(spin) + " " + styles.text.Render(trans.t("tui.common.working", nil)) + "  " + styles.dim.Render(strings.Join(parts, " · "))
	return spreadRight([]string{left}, []string{styles.dim.Render(interrupt)}, width)
}
