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
	for i, ln := range lines {
		if !inHunk && !isDiffHeader(ln) {
			inHunk = true
		}
		class := diffClass(ln, inHunk)
		if class != diffMeta {
			if !v.expand && spent == diffHunkBudget {
				out = append(out, resultLine(fmt.Sprintf("%s %d lignes de plus (alt+e)", glyphFold, len(lines)-i), v.st.dim))
				break
			}
			spent++
		}
		out = append(out, paintDiff(truncate(ln, width), class, v.caps, v.st))
	}
	return strings.Join(out, "\n")
}

func paintDiff(line, class string, caps Capabilities, st uiStyles) string {
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
		return st.green.Render(line)
	case diffDel:
		return st.red.Render(line)
	case diffMeta:
		return st.dim.Render(line)
	}
	return st.text.Render(line)
}

const tableGap = "  "

func renderTable(rows [][]string, width int, st uiStyles) string {
	if len(rows) == 0 {
		return ""
	}
	cols := 0
	for _, r := range rows {
		cols = max(cols, len(r))
	}
	w := make([]int, cols)
	for _, r := range rows {
		for i, c := range r {
			w[i] = max(w[i], lipgloss.Width(c))
		}
	}
	shrinkColumns(w, width-len(tableGap)*(cols-1))

	var b strings.Builder
	for i, r := range rows {
		if i > 0 {
			b.WriteByte('\n')
		}
		if i == 1 {
			b.WriteString(st.dim.Render(strings.Repeat("─", min(width, tableWidth(w)))) + "\n")
		}
		b.WriteString(tableRow(r, w, i == 0, st))
	}
	return b.String()
}

func tableWidth(w []int) int {
	total := len(tableGap) * (len(w) - 1)
	for _, c := range w {
		total += c
	}
	return total
}

func shrinkColumns(w []int, budget int) {
	for tableWidth(w) > budget {
		widest := 0
		for i := range w {
			if w[i] > w[widest] {
				widest = i
			}
		}
		if w[widest] <= 1 {
			return
		}
		w[widest]--
	}
}

func tableRow(r []string, w []int, header bool, st uiStyles) string {
	cells := make([]string, len(w))
	for i := range w {
		cell := ""
		if i < len(r) {
			cell = r[i]
		}
		cell = truncate(cell, w[i])
		pad := w[i] - lipgloss.Width(cell)
		if pad > 0 && i < len(w)-1 {
			cell += strings.Repeat(" ", pad)
		}
		if header {
			cell = st.accent.Render(cell)
		}
		cells[i] = cell
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
		for i, c := range cells {
			cells[i] = strings.TrimSpace(c)
			if strings.Trim(cells[i], ":- ") != "" {
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
	for _, ln := range strings.Split(text, "\n") {
		fence := strings.TrimSpace(ln)
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
			buf = append(buf, ln)
		default:
			out = append(out, ln)
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
	tb := m.tabs[m.active]
	if tb == nil || m.clip == nil {
		return
	}
	for i := len(tb.entries) - 1; i >= 0; i-- {
		if tb.entries[i].role != roleAgent {
			continue
		}
		blocks := codeBlocks(tb.entries[i].text)
		if len(blocks) == 0 {
			break
		}
		body := blocks[len(blocks)-1].body
		if err := m.clip.WriteText(body); err != nil {
			m.flash = "copie impossible : " + err.Error()
			return
		}
		n := len(strings.Split(body, "\n"))
		m.flash = fmt.Sprintf("%d ligne%s copiée%s", n, plural(n), plural(n))
		return
	}
	m.flash = "aucun bloc de code dans la dernière réponse"
}
