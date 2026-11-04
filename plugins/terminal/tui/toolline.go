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

const resultGutter = "    " + glyphResult + " "

const (
	pathDepth = 4
	pathKeep  = 2
)

const (
	stateTimeout      = "timeout"
	stateFailed       = "failed"
	subagentRetryNote = "délai dépassé, relancé"
)

func renderTool(item entry, width int, v view) string {
	lines := strings.Split(item.text, "\n")
	family, target := toolVerb(lines[0])
	name := ""
	if family != "" {
		name = toolName(lines[0])
	}
	out := []string{toolHead(item, family, name, target, width, v)}
	if item.open {
		out = append(out, toolBody(lines[1:], target, width, v)...)
	}
	return strings.Join(out, "\n")
}

const diffGutter = "    "

func toolBody(results []string, target string, width int, v view) []string {
	if !isDiffBody(results) {
		return toolResults(results, width, v)
	}
	out := []string{diffGutter + truncate(diffHeader(target, results, v), width-len(diffGutter))}
	for _, row := range diffRows(results, width-len(diffGutter), v) {
		out = append(out, diffGutter+row)
	}
	return append(out, diffGutter+v.st.dim.Render("╰"))
}

func familyStyle(st uiStyles, family string) lipgloss.Style {
	switch family {
	case familyWrite:
		return st.warn
	case familyAgent:
		return st.violet
	}
	return st.accent
}

func toolHead(item entry, family, name, target string, width int, v view) string {
	mark := familyStyle(v.st, family)
	stateMark := v.st.green.Render(glyphOK)
	switch {
	case item.failed:
		mark = v.st.red
		stateMark = v.st.red.Render(glyphError)
	case item.live:
		stateMark = v.st.spinner.Render(v.spin)
	}
	lefts := []string{toolLeft(mark, family, name, target, v)}
	if short := abbreviatePaths(target); short != target {
		lefts = append(lefts, toolLeft(mark, family, name, short, v))
	}
	arrow := ""
	if item.foldable {
		arrow = " " + v.st.dim.Render(glyphCollapsed)
		if item.open {
			arrow = " " + v.st.dim.Render(glyphExpanded)
		}
	}
	rights := []string{stateMark + arrow}
	if item.span > 0 || item.live {
		rights = append([]string{v.st.dim.Render(formatSpan(item.span)) + " " + stateMark + arrow}, rights...)
	}
	return spreadRight(lefts, rights, width)
}

func toolLeft(mark lipgloss.Style, family, name, target string, v view) string {
	left := mark.Render(glyphTool)
	if family == "" {
		return left + " " + v.st.text.Render(target)
	}
	left += " " + v.st.strong.Render(padCells(name, toolVerbWidth))
	if target != "" {
		left += " " + v.st.dim.Render(target)
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

func formatSpan(d time.Duration) string {
	if d < 10*time.Second {
		return fmt.Sprintf("%.1fs", d.Seconds())
	}
	return elapsed(d)
}

func padCells(s string, n int) string {
	return s + strings.Repeat(" ", max(0, n-lipgloss.Width(s)))
}

func toolResults(results []string, width int, v view) []string {
	shown := len(results)
	if !v.expand {
		shown = min(shown, toolLineCap)
	}
	out := make([]string, 0, shown+1)
	for _, line := range results[:shown] {
		out = append(out, truncate(v.st.dim.Render(resultGutter+line), width))
	}
	if hidden := len(results) - shown; hidden > 0 {
		out = append(out, truncate(v.st.dim.Render(resultGutter+fmt.Sprintf("%s +%d", glyphMoreLines, hidden)), width))
	}
	return out
}

func abbreviatePaths(s string) string {
	fields := strings.Split(s, " ")
	for i, f := range fields {
		fields[i] = abbreviatePath(f)
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

func subagentRows(agents []liveAgent, now time.Time, width int, v view) []string {
	out := make([]string, 0, len(agents))
	for index, agent := range agents {
		branch := glyphBranchMid
		if index == len(agents)-1 {
			branch = glyphBranchEnd
		}
		mark, note := subagentMark(agent, v)
		left := v.st.dim.Render("    "+branch+" ") + mark + " " + v.st.text.Render(padCells(oneLine(agent.Name), 10))
		if kind := oneLine(agent.Kind); kind != "" {
			left += " " + v.st.dim.Render(kind)
		}
		if note != "" {
			left += " " + v.st.red.Render(note)
		}
		end := now
		if !agent.ended.IsZero() {
			end = agent.ended
		}
		out = append(out, spreadRight([]string{left}, []string{v.st.dim.Render(elapsed(end.Sub(agent.since)))}, width))
	}
	return out
}

func subagentMark(agent liveAgent, v view) (string, string) {
	switch agent.State {
	case stateDone:
		return v.st.green.Render(glyphOK), ""
	case stateTimeout:
		return v.st.red.Render(glyphError), subagentRetryNote
	case stateFailed:
		return v.st.red.Render(glyphError), ""
	}
	return v.st.spinner.Render(v.spin), ""
}

func renderThinking(item entry, width int, v view) string {
	arrow, hint := glyphCollapsed, "ctrl+o"
	if item.open {
		arrow = glyphExpanded
	}
	label := "Réflexion · " + formatSpan(item.span)
	if item.tokens > 0 {
		label += " · " + formatTokens(item.tokens) + " tokens"
	}
	left := v.st.dim.Render(arrow+" ") + v.st.thinking.Render(label)
	head := spreadRight([]string{left}, []string{v.st.dim.Render(arrow + " " + hint)}, width)
	if !item.open {
		return head
	}
	lines := []string{head}
	for _, ln := range strings.Split(wrapWith(v.st.thinking, item.text, max(1, width-len(resultGutter))), "\n") {
		lines = append(lines, "    "+ln)
	}
	return strings.Join(lines, "\n")
}

func busyLine(width int, st uiStyles, spin, elapsedText, tokens, cost string) string {
	parts := []string{elapsedText}
	if tokens != "" {
		parts = append(parts, "↑ "+tokens)
	}
	if cost != "" {
		parts = append(parts, cost)
	}
	left := st.spinner.Render(spin) + " " + st.accent.Render(workingVerb) + "  " + st.dim.Render(strings.Join(parts, " · "))
	return spreadRight([]string{left}, []string{st.dim.Render("Échap interrompre")}, width)
}
