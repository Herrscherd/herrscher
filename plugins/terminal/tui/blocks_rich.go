package tui

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

const codeFence = "```"

const (
	ansiGreen = "\x1b[32m"
	ansiRed   = "\x1b[31m"
)

const (
	diffHunkBudget     = 4
	diffNumberWidth    = 5
	diffTwoColumnsFrom = 70
	diffTabSpaces      = "  "
)

func renderDiff(body string, width int, display view) string {
	return strings.Join(diffRows(strings.Split(body, "\n"), width, display), "\n")
}

func diffRows(lines []string, width int, display view) []string {
	out := make([]string, 0, len(lines))
	oldNo, newNo := 1, 1
	spent := 0
	for index, line := range lines {
		if strings.HasPrefix(line, "@@") {
			oldNo, newNo = hunkStart(line)
			out = append(out, diffHunk(line, width, display))
			continue
		}
		if isFileHeader(line) {
			continue
		}
		if !display.expand && spent == diffHunkBudget {
			out = append(out, display.st.dim.Render(fmt.Sprintf("%s %d %s", display.st.glyphs.fold, countDiffRows(lines[index:]), display.hint(actToggleToolExpand, "lignes de plus"))))
			break
		}
		sign, code := diffSign(line)
		oldCell, newCell := "", ""
		switch sign {
		case "+":
			newCell = strconv.Itoa(newNo)
			newNo++
		case "-":
			oldCell = strconv.Itoa(oldNo)
			oldNo++
		default:
			oldCell, newCell = strconv.Itoa(oldNo), strconv.Itoa(newNo)
			oldNo++
			newNo++
		}
		out = append(out, diffRow(sign, diffNumbers(oldCell, newCell, width), code, width, display))
		spent++
	}
	return out
}

func diffNumbers(oldCell, newCell string, width int) string {
	if width < diffTwoColumnsFrom {
		if newCell != "" {
			return padLeftCells(newCell)
		}
		return padLeftCells(oldCell)
	}
	return padLeftCells(oldCell) + padLeftCells(newCell)
}

func padLeftCells(s string) string {
	return strings.Repeat(" ", max(0, diffNumberWidth-lipgloss.Width(s))) + s
}

func diffRow(sign, numbers, code string, width int, display view) string {
	gutter := "│" + numbers + " "
	body := truncate(sign+" "+strings.ReplaceAll(code, "\t", diffTabSpaces), max(1, width-lipgloss.Width(gutter)))
	switch {
	case display.caps.Colour == Colour16:
		return paintColour16(sign, gutter+body, width)
	case sign == "+":
		return display.st.addRow.Render(padCells(gutter+body, width))
	case sign == "-":
		return display.st.delRow.Render(padCells(gutter+body, width))
	}
	return display.st.dim.Render(gutter) + display.st.text.Render(body)
}

func paintColour16(sign, line string, width int) string {
	switch sign {
	case "+":
		return ansiGreen + padCells(line, width) + ansiReset
	case "-":
		return ansiRed + padCells(line, width) + ansiReset
	}
	return line
}

func diffHunk(line string, width int, display view) string {
	code := ""
	if _, after, found := strings.Cut(strings.TrimPrefix(line, "@@"), "@@"); found {
		code = strings.TrimSpace(after)
	}
	gutter := "│" + diffNumbers(display.st.glyphs.omitted, display.st.glyphs.omitted, width) + " "
	text := truncate("@@ "+code, max(1, width-lipgloss.Width(gutter)))
	return display.st.dim.Render(gutter) + display.st.violet.Render(text)
}

func diffHeader(path string, body []string, display view) string {
	added, removed := 0, 0
	for _, line := range body {
		sign, _ := diffSign(line)
		switch {
		case sign == "+" && !isFileHeader(line):
			added++
		case sign == "-" && !isFileHeader(line):
			removed++
		}
	}
	return display.st.dim.Render("╭ ") + display.st.text.Render(path) + display.st.dim.Render(
		fmt.Sprintf("  +%d −%d   ancien · nouveau   %s", added, removed, display.hint(actOpenDiff, "panneau de diff")),
	)
}

func isFileHeader(line string) bool {
	return strings.HasPrefix(line, "+++") || strings.HasPrefix(line, "---") ||
		strings.HasPrefix(line, "diff ") || strings.HasPrefix(line, "index ")
}

func isDiffBody(lines []string) bool {
	for _, line := range lines {
		if strings.HasPrefix(line, "@@") || strings.HasPrefix(line, "+++ ") {
			return true
		}
	}
	return false
}

func diffSign(line string) (string, string) {
	if strings.HasPrefix(line, "+") || strings.HasPrefix(line, "-") {
		return line[:1], line[1:]
	}
	if strings.HasPrefix(line, " ") {
		return " ", line[1:]
	}
	return " ", line
}

func hunkStart(line string) (int, int) {
	oldNo, newNo := 1, 1
	for _, field := range strings.Fields(line) {
		switch {
		case strings.HasPrefix(field, "-"):
			oldNo = leadingInt(field[1:], oldNo)
		case strings.HasPrefix(field, "+"):
			newNo = leadingInt(field[1:], newNo)
		}
	}
	return oldNo, newNo
}

func leadingInt(s string, fallback int) int {
	head, _, _ := strings.Cut(s, ",")
	number, err := strconv.Atoi(head)
	if err != nil {
		return fallback
	}
	return number
}

func countDiffRows(lines []string) int {
	count := 0
	for _, line := range lines {
		if !strings.HasPrefix(line, "@@") && !isFileHeader(line) {
			count++
		}
	}
	return count
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
			cell = styles.strong.Render(cell)
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

func foldCodeBlocks(text string, glyphs glyphSet, toggle string) string {
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
				out = append(out, foldSummary(lang, len(buf), glyphs, toggle))
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

func foldSummary(lang string, lines int, glyphs glyphSet, toggle string) string {
	if lang == "" {
		lang = "text"
	}
	return fmt.Sprintf("%s %s · %d lines (%s)", glyphs.fold, lang, lines, toggle)
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
