package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// Tool output is the bulk of a long session and the part the TUI treated as
// prose: a diff painted as source, a table wrapped into confetti, a two-hundred
// line file pushing the answer off the screen. Each gets the shape it already
// had on paper.

// The diff line classes. A class is what the line means, not how it is drawn:
// the drawing depends on the terminal, the meaning does not.
const (
	diffAdd     = "add"
	diffDel     = "del"
	diffMeta    = "meta"
	diffContext = "context"
)

// diffClass says what one diff line is. inHunk matters because the file headers
// of a unified diff open with the same characters as its content — "+++ b/x" is
// not two hundred lines of addition, it is the name of the file they are in.
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

// isDiffHeader matches the preamble a full unified diff carries before its first
// hunk. Only there does a leading +++ or --- mean a filename.
func isDiffHeader(line string) bool {
	return strings.HasPrefix(line, "@@") || strings.HasPrefix(line, "+++") ||
		strings.HasPrefix(line, "---") || strings.HasPrefix(line, "diff ") ||
		strings.HasPrefix(line, "index ")
}

// The basic ANSI pair, written raw. On a 16-colour terminal the palette's hex
// green is approximated to whichever of eight colours is nearest, which for a
// diff can be no distinction at all — and the one thing a diff must never lose
// is which line was added.
const (
	ansiGreen = "\x1b[32m"
	ansiRed   = "\x1b[31m"
)

// renderDiff colours a diff body by line class. Lines are clipped rather than
// wrapped: a folded continuation carries no leading +/- and reads as context,
// which inverts the meaning of the line it came from.
func renderDiff(body string, width int, caps Capabilities) string {
	lines := strings.Split(body, "\n")
	out := make([]string, len(lines))
	// An agent quoting a change usually writes the hunk without the preamble, so
	// content is the default: the header block only exists while the lines still
	// look like one.
	inHunk := len(lines) > 0 && !isDiffHeader(lines[0])
	for i, ln := range lines {
		if !inHunk && !isDiffHeader(ln) {
			inHunk = true
		}
		out[i] = paintDiff(truncate(ln, width), diffClass(ln, inHunk), caps)
	}
	return strings.Join(out, "\n")
}

// paintDiff applies the colour for a class, in the depth the terminal has.
func paintDiff(line, class string, caps Capabilities) string {
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
		return greenStyle.Render(line)
	case diffDel:
		return redStyle.Render(line)
	case diffMeta:
		return dimStyle.Render(line)
	}
	return textStyle.Render(line)
}

// tableGap separates two columns: enough to read as a boundary, cheap enough
// that a narrow terminal still has room for the cells.
const tableGap = "  "

// renderTable draws rows as aligned columns inside width, with the first row as
// the header. A table that wraps is not a table — the columns stop lining up and
// the reader has to reassemble each row by eye — so it truncates instead, and it
// takes the width out of the widest column first, which is the one carrying the
// prose rather than the identifier.
func renderTable(rows [][]string, width int) string {
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
			// The rule under the header, drawn once and to the same width as the
			// columns it separates.
			b.WriteString(dimStyle.Render(strings.Repeat("─", min(width, tableWidth(w)))) + "\n")
		}
		b.WriteString(tableRow(r, w, i == 0))
	}
	return b.String()
}

// tableWidth is the total drawn width of the columns plus their gaps.
func tableWidth(w []int) int {
	total := len(tableGap) * (len(w) - 1)
	for _, c := range w {
		total += c
	}
	return total
}

// shrinkColumns takes width off the widest column until the row fits, so a long
// description gives ground before a short identifier does. A column is never
// taken below one cell: a column of nothing tells the reader less than an
// ellipsis does.
func shrinkColumns(w []int, budget int) {
	for tableWidth(w) > budget {
		widest := 0
		for i := range w {
			if w[i] > w[widest] {
				widest = i
			}
		}
		if w[widest] <= 1 {
			return // nothing left to give; the row is clipped by the caller
		}
		w[widest]--
	}
}

// tableRow renders one row, padding each cell to its column and clipping the
// ones that outgrew it.
func tableRow(r []string, w []int, header bool) string {
	cells := make([]string, len(w))
	for i := range w {
		cell := ""
		if i < len(r) {
			cell = r[i]
		}
		cell = truncate(cell, w[i])
		pad := w[i] - lipgloss.Width(cell)
		if pad > 0 && i < len(w)-1 {
			cell += strings.Repeat(" ", pad) // the last column needs no trailing pad
		}
		if header {
			cell = accentStyle.Render(cell)
		}
		cells[i] = cell
	}
	return strings.Join(cells, tableGap)
}

// isTableRow reports whether a line is a markdown table row: a pipe at each end
// is the shape every generator emits, and requiring both keeps a sentence with a
// pipe in it out of the table.
func isTableRow(line string) bool {
	t := strings.TrimSpace(line)
	return len(t) > 1 && strings.HasPrefix(t, "|") && strings.HasSuffix(t, "|")
}

// parseTableRows turns markdown table lines into cells, dropping the alignment
// rule: renderTable draws its own, and the dashes are notation rather than data.
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

// codeBlock is one fenced block of a message, kept as it was written. The raw
// body is what a copy has to hand over: a rendered one has escape sequences in
// it and does not compile.
type codeBlock struct {
	lang string
	body string
}

// codeBlocks returns the fenced blocks in text, in order. An unclosed fence takes
// the rest of the message — the agent is mid-stream, and those lines are code.
func codeBlocks(text string) []codeBlock {
	var out []codeBlock
	var cur *codeBlock
	var buf []string
	for _, ln := range strings.Split(text, "\n") {
		fence := strings.TrimSpace(ln)
		switch {
		case cur == nil && strings.HasPrefix(fence, "```"):
			cur = &codeBlock{lang: strings.TrimPrefix(fence, "```")}
		case cur != nil && fence == "```":
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

// foldThreshold is how many lines a block may have before it is worth hiding.
// Roughly a short terminal's worth: below it the block is read, above it the
// block is scrolled past, and scrolling past it is what folding automates.
const foldThreshold = 12

// foldCodeBlocks replaces every long fenced block with a one-line summary. It
// works on the message text rather than on the rendered lines, so unfolding is
// not a re-derivation: the original text was never modified and rendering it
// again gives back exactly what was there.
func foldCodeBlocks(text string) string {
	var out []string
	var buf []string
	lang := ""
	inFence := false
	for _, ln := range strings.Split(text, "\n") {
		fence := strings.TrimSpace(ln)
		switch {
		case !inFence && strings.HasPrefix(fence, "```"):
			inFence, lang, buf = true, strings.TrimPrefix(fence, "```"), nil
		case inFence && fence == "```":
			inFence = false
			if len(buf) > foldThreshold {
				out = append(out, foldSummary(lang, len(buf)))
				continue
			}
			out = append(out, "```"+lang)
			out = append(out, buf...)
			out = append(out, "```")
		case inFence:
			buf = append(buf, ln)
		default:
			out = append(out, ln)
		}
	}
	if inFence { // an unclosed fence keeps its lines: it is still arriving
		out = append(out, "```"+lang)
		out = append(out, buf...)
	}
	return strings.Join(out, "\n")
}

// foldSummary is what a folded block says: what it is and how much of it there
// is, plus the key that brings it back. Hiding content without saying how to see
// it again would be the TUI deciding what the operator may read.
func foldSummary(lang string, lines int) string {
	if lang == "" {
		lang = "text"
	}
	return fmt.Sprintf("%s %s · %d lines (ctrl+f)", glyphFold, lang, lines)
}

// toggleFold flips folding for the whole transcript. One switch rather than a
// per-block state: the operator either wants the code or wants the conversation,
// and a transcript where some blocks are folded and others are not is harder to
// read than either.
func (m *model) toggleFold() {
	m.foldCode = !m.foldCode
	m.tsCache.valid = false // the cache key cannot see a fold change
	m.syncViewport()
}

// copyLastCode puts the last code block of the active tab's last agent answer on
// the system clipboard. The last block is the one the answer ended on, which is
// nearly always the one being asked for; anything more selective needs a
// selection UI, and that is not what a copy key is for.
func (m *model) copyLastCode() {
	tb := m.tabs[m.active]
	if tb == nil {
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
			m.flash = "copy failed: " + err.Error()
			return
		}
		m.flash = fmt.Sprintf("copied %d lines", len(strings.Split(body, "\n")))
		return
	}
	m.flash = "no code block in the last answer"
}
