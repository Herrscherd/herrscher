package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

const codeFence = "```"

const (
	diffAdd     = "add"
	diffDel     = "del"
	diffMeta    = "meta"
	diffContext = "context"
)

func diffClass(line string, inHunk bool) string {
	switch {
	case strings.HasPrefix(line, "@@"):
		return diffMeta
	case !inHunk && isDiffHeader(line):
		return diffMeta
	case !inHunk:
		return diffContext
	case strings.HasPrefix(line, "+"):
		return diffAdd
	case strings.HasPrefix(line, "-"):
		return diffDel
	}
	return diffContext
}

func isDiffHeader(line string) bool {
	return strings.HasPrefix(line, "@@") || strings.HasPrefix(line, "+++") ||
		strings.HasPrefix(line, "---") || strings.HasPrefix(line, "diff ") ||
		strings.HasPrefix(line, "index ")
}

const (
	ansiGreen = "\x1b[32m"
	ansiRed   = "\x1b[31m"
)

const diffHunkBudget = 4

func renderDiff(body string, width int, v view) string {
	lines := strings.Split(body, "\n")
	out := make([]string, 0, len(lines)+1)
	inHunk := len(lines) > 0 && !isDiffHeader(lines[0])
	spent := 0
	for line, ln := range lines {
		if !inHunk && !isDiffHeader(ln) {
			inHunk = true
		}
		class := diffClass(ln, inHunk)
		if class != diffMeta {
			if !v.expand && spent == diffHunkBudget {
				out = append(out, resultLine(fmt.Sprintf("%s %d lignes de plus (alt+e)", glyphFold, len(lines)-line), v.st.dim))
				break
			}
			spent++
		}
		out = append(out, paintDiff(truncate(ln, width), class, v.caps, v.st))
	}
	return strings.Join(out, "\n")
}

func paintDiff(line, class string, caps Capabilities, styles uiStyles) string {
	if caps.Colour == Colour16 {
		switch class {
		case diffAdd:
			return ansiGreen + line + ansiReset
		case diffDel:
			return ansiRed + line + ansiReset
		}
		return line
	}
	switch class {
	case diffAdd:
		return styles.green.Render(line)
	case diffDel:
		return styles.red.Render(line)
	case diffMeta:
		return styles.dim.Render(line)
	}
	return styles.text.Render(line)
}

const tableGap = "  "

func renderTable(rows [][]string, width int, styles uiStyles) string {
	if len(rows) == 0 {
		return ""
	}
	cols := 0
	for _, r := range rows {
		cols = max(cols, len(r))
	}
	widths := make([]int, cols)
	for _, r := range rows {
		for i, c := range r {
			widths[i] = max(widths[i], lipgloss.Width(c))
		}
	}
	shrinkColumns(widths, width-len(tableGap)*(cols-1))

	var builder strings.Builder
	for cells, r := range rows {
		if cells > 0 {
			builder.WriteByte('\n')
		}
		if cells == 1 {
			builder.WriteString(styles.dim.Render(strings.Repeat("─", min(width, tableWidth(widths)))) + "\n")
		}
		builder.WriteString(tableRow(r, widths, cells == 0, styles))
	}
	return builder.String()
}

func tableWidth(widths []int) int {
	total := len(tableGap) * (len(widths) - 1)
	for _, c := range widths {
		total += c
	}
	return total
}

func shrinkColumns(widths []int, budget int) {
	for tableWidth(widths) > budget {
		widest := 0
		for index := range widths {
			if widths[index] > widths[widest] {
				widest = index
			}
		}
		if widths[widest] <= 1 {
			return
		}
		widths[widest]--
	}
}

func tableRow(r []string, w []int, header bool, st uiStyles) string {
	cells := make([]string, len(w))
	for index := range w {
		cell := ""
		if index < len(r) {
			cell = r[index]
		}
		cell = truncate(cell, w[index])
		pad := w[index] - lipgloss.Width(cell)
		if pad > 0 && index < len(w)-1 {
			cell += strings.Repeat(" ", pad)
		}
		if header {
			cell = st.accent.Render(cell)
		}
		cells[index] = cell
	}
	return strings.Join(cells, tableGap)
}

func isTableRow(line string) bool {
	t := strings.TrimSpace(line)
	return len(t) > 1 && strings.HasPrefix(t, "|") && strings.HasSuffix(t, "|")
}

func parseTableRows(lines []string) [][]string {
	var rows [][]string
	for _, ln := range lines {
		cells := strings.Split(strings.Trim(strings.TrimSpace(ln), "|"), "|")
		rule := true
		for cell, c := range cells {
			cells[cell] = strings.TrimSpace(c)
			if strings.Trim(cells[cell], ":- ") != "" {
				rule = false
			}
		}
		if rule {
			continue
		}
		rows = append(rows, cells)
	}
	return rows
}

type codeBlock struct {
	lang string
	body string
}

func codeBlocks(text string) []codeBlock {
	var out []codeBlock
	var cur *codeBlock
	var buf []string
	for _, ln := range strings.Split(text, "\n") {
		fence := strings.TrimSpace(ln)
		switch {
		case cur == nil && strings.HasPrefix(fence, codeFence):
			cur = &codeBlock{lang: strings.TrimPrefix(fence, codeFence)}
		case cur != nil && fence == codeFence:
			cur.body = strings.Join(buf, "\n")
			out, cur, buf = append(out, *cur), nil, nil
		case cur != nil:
			buf = append(buf, ln)
		}
	}
	if cur != nil {
		cur.body = strings.Join(buf, "\n")
		out = append(out, *cur)
	}
	return out
}

const foldThreshold = 12

func foldCodeBlocks(text string) string {
	var out []string
	var buf []string
	lang := ""
	inFence := false
	for _, line := range strings.Split(text, "\n") {
		fence := strings.TrimSpace(line)
		switch {
		case !inFence && strings.HasPrefix(fence, codeFence):
			inFence, lang, buf = true, strings.TrimPrefix(fence, codeFence), nil
		case inFence && fence == codeFence:
			inFence = false
			if len(buf) > foldThreshold {
				out = append(out, foldSummary(lang, len(buf)))
				continue
			}
			out = append(out, codeFence+lang)
			out = append(out, buf...)
			out = append(out, codeFence)
		case inFence:
			buf = append(buf, line)
		default:
			out = append(out, line)
		}
	}
	if inFence {
		out = append(out, codeFence+lang)
		out = append(out, buf...)
	}
	return strings.Join(out, "\n")
}

func foldSummary(lang string, lines int) string {
	if lang == "" {
		lang = "text"
	}
	return fmt.Sprintf("%s %s · %d lines (ctrl+f)", glyphFold, lang, lines)
}

func (m *model) toggleFold() {
	m.foldCode = !m.foldCode
	m.tsCache.valid = false
	m.syncViewport()
}

func (m *model) toggleToolExpand() {
	m.expandTools = !m.expandTools
	m.tsCache.valid = false
	m.syncViewport()
}

func (m *model) copyLastCode() {
	tab := m.tabs[m.active]
	if tab == nil || m.clip == nil {
		return
	}
	for index := len(tab.entries) - 1; index >= 0; index-- {
		if tab.entries[index].role != roleAgent {
			continue
		}
		blocks := codeBlocks(tab.entries[index].text)
		if len(blocks) == 0 {
			break
		}
		body := blocks[len(blocks)-1].body
		if err := m.clip.WriteText(m.runContext(), body); err != nil {
			m.flash = "copie impossible : " + err.Error()
			return
		}
		n := len(strings.Split(body, "\n"))
		m.flash = fmt.Sprintf("%d ligne%s copiée%s", n, plural(n), plural(n))
		return
	}
	m.flash = "aucun bloc de code dans la dernière réponse"
}
