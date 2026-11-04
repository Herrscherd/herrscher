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

func renderDiff(body string, width int, view view) string {
	lines := strings.Split(body, "\n")
	out := make([]string, 0, len(lines)+1)
	inHunk := len(lines) > 0 && !isDiffHeader(lines[0])
	spent := 0
	for lineIndex, diffLine := range lines {
		if !inHunk && !isDiffHeader(diffLine) {
			inHunk = true
		}
		class := diffClass(diffLine, inHunk)
		if class != diffMeta {
			if !view.expand && spent == diffHunkBudget {
				out = append(out, resultLine(fmt.Sprintf("%s %d lignes de plus (alt+e)", view.st.glyphs.fold, len(lines)-lineIndex), view.st.dim, view.st.glyphs))
				break
			}
			spent++
		}
		out = append(out, paintDiff(truncate(diffLine, width), class, view.caps, view.st))
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
	for rowIndex, row := range rows {
		if rowIndex > 0 {
			builder.WriteByte('\n')
		}
		if rowIndex == 1 {
			builder.WriteString(styles.dim.Render(strings.Repeat("─", min(width, tableWidth(widths)))) + "\n")
		}
		builder.WriteString(tableRow(row, widths, rowIndex == 0, styles))
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

func tableRow(rowCells []string, widths []int, header bool, styles uiStyles) string {
	cells := make([]string, len(widths))
	for index := range widths {
		cell := ""
		if index < len(rowCells) {
			cell = rowCells[index]
		}
		cell = truncate(cell, widths[index])
		pad := widths[index] - lipgloss.Width(cell)
		if pad > 0 && index < len(widths)-1 {
			cell += strings.Repeat(" ", pad)
		}
		if header {
			cell = styles.accent.Render(cell)
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
		for cellIndex, c := range cells {
			cells[cellIndex] = strings.TrimSpace(c)
			if strings.Trim(cells[cellIndex], ":- ") != "" {
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
	for _, line := range strings.Split(text, "\n") {
		fence := strings.TrimSpace(line)
		switch {
		case cur == nil && strings.HasPrefix(fence, codeFence):
			cur = &codeBlock{lang: strings.TrimPrefix(fence, codeFence)}
		case cur != nil && fence == codeFence:
			cur.body = strings.Join(buf, "\n")
			out, cur, buf = append(out, *cur), nil, nil
		case cur != nil:
			buf = append(buf, line)
		}
	}
	if cur != nil {
		cur.body = strings.Join(buf, "\n")
		out = append(out, *cur)
	}
	return out
}

const foldThreshold = 12

func foldCodeBlocks(text string, glyphs glyphSet) string {
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
				out = append(out, foldSummary(lang, len(buf), glyphs))
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

func foldSummary(lang string, lines int, glyphs glyphSet) string {
	if lang == "" {
		lang = "text"
	}
	return fmt.Sprintf("%s %s · %d lines (ctrl+f)", glyphs.fold, lang, lines)
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
