package tui

import (
	"strings"

	"github.com/charmbracelet/glamour"
	"github.com/charmbracelet/glamour/ansi"
	"github.com/charmbracelet/glamour/styles"
)

var (
	mdRenderer *glamour.TermRenderer
	mdWidth    int
)

func mdStyle() ansi.StyleConfig {
	s := styles.DarkStyleConfig
	zero := uint(0)
	s.Document.Margin = &zero
	s.Document.BlockPrefix = ""
	s.Document.BlockSuffix = ""
	s.Document.Color = nil
	s.Heading.Color = strPtr(colViolet)
	s.H1.BackgroundColor = nil
	s.H1.Color = strPtr(colViolet)
	s.CodeBlock.Margin = &zero
	return s
}

func strPtr(s string) *string { return &s }

func renderMarkdown(text string, width int, v view) string {
	var b strings.Builder
	for i, seg := range splitDiffs(text) {
		if i > 0 {
			b.WriteByte('\n')
		}
		if seg.diff {
			b.WriteString(renderDiff(seg.text, width, v))
			continue
		}
		b.WriteString(renderProse(seg.text, width))
	}
	return b.String()
}

func renderProse(text string, width int) string {
	var b strings.Builder
	var prose, table []string
	flushProse := func() {
		if len(prose) > 0 {
			b.WriteString(renderGlamour(strings.Join(prose, "\n"), width))
			prose = nil
		}
	}
	flushTable := func() {
		if len(table) > 0 {
			b.WriteString(renderTable(parseTableRows(table), width) + "\n")
			table = nil
		}
	}
	for _, ln := range strings.Split(text, "\n") {
		if isTableRow(ln) {
			flushProse()
			table = append(table, ln)
			continue
		}
		flushTable()
		prose = append(prose, ln)
	}
	flushTable()
	flushProse()
	return strings.Trim(b.String(), "\n")
}

type segment struct {
	text string
	diff bool
}

func splitDiffs(text string) []segment {
	lines := strings.Split(text, "\n")
	var segs []segment
	var buf []string
	inDiff := false
	flush := func(diff bool) {
		if len(buf) > 0 {
			segs = append(segs, segment{text: strings.Join(buf, "\n"), diff: diff})
			buf = nil
		}
	}
	for _, ln := range lines {
		fence := strings.TrimSpace(ln)
		switch {
		case !inDiff && (fence == "```diff" || fence == "~~~diff"):
			flush(false)
			inDiff = true
		case inDiff && (fence == "```" || fence == "~~~"):
			flush(true)
			inDiff = false
		default:
			buf = append(buf, ln)
		}
	}
	flush(inDiff)
	if len(segs) == 0 {
		return []segment{{text: text}}
	}
	return segs
}

func renderGlamour(text string, width int) string {
	if width < 1 {
		width = 1
	}
	if mdRenderer == nil || mdWidth != width {
		r, err := glamour.NewTermRenderer(
			glamour.WithStyles(mdStyle()),
			glamour.WithWordWrap(width),
			glamour.WithEmoji(),
		)
		if err != nil {
			return wrapWith(textStyle, text, width)
		}
		mdRenderer, mdWidth = r, width
	}
	out, err := mdRenderer.Render(text)
	if err != nil {
		return wrapWith(textStyle, text, width)
	}
	lines := strings.Split(out, "\n")
	for i, ln := range lines {
		lines[i] = strings.TrimRight(ln, " ")
	}
	return strings.Trim(strings.Join(lines, "\n"), "\n")
}
