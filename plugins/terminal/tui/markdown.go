package tui

import (
	"strings"

	"github.com/charmbracelet/glamour"
	"github.com/charmbracelet/glamour/ansi"
	"github.com/charmbracelet/glamour/styles"
)

type markdownRenderer struct {
	r     *glamour.TermRenderer
	width int
}

func (md *markdownRenderer) termRenderer(width int) (*glamour.TermRenderer, error) {
	if md.r == nil || md.width != width {
		renderer, err := glamour.NewTermRenderer(
			glamour.WithStyles(mdStyle()),
			glamour.WithWordWrap(width),
			glamour.WithEmoji(),
		)
		if err != nil {
			return nil, err
		}
		md.r, md.width = renderer, width
	}
	return md.r, nil
}

func mdStyle() ansi.StyleConfig {
	style := styles.DarkStyleConfig
	zero := uint(0)
	style.Document.Margin = &zero
	style.Document.BlockPrefix = ""
	style.Document.BlockSuffix = ""
	style.Document.Color = nil
	style.Heading.Color = strPtr(colViolet)
	style.H1.BackgroundColor = nil
	style.H1.Color = strPtr(colViolet)
	style.CodeBlock.Margin = &zero
	return style
}

func strPtr(s string) *string { return &s }

func renderMarkdown(text string, width int, view view) string {
	var builder strings.Builder
	for i, seg := range splitDiffs(text) {
		if i > 0 {
			builder.WriteByte('\n')
		}
		if seg.diff {
			builder.WriteString(renderDiff(seg.text, width, view))
			continue
		}
		builder.WriteString(renderProse(seg.text, width, view))
	}
	return builder.String()
}

func renderProse(text string, width int, view view) string {
	var block strings.Builder
	var prose, table []string
	flushProse := func() {
		if len(prose) > 0 {
			block.WriteString(renderGlamour(strings.Join(prose, "\n"), width, view))
			prose = nil
		}
	}
	flushTable := func() {
		if len(table) > 0 {
			block.WriteString(renderTable(parseTableRows(table), width, view.st) + "\n")
			table = nil
		}
	}
	for _, line := range strings.Split(text, "\n") {
		if isTableRow(line) {
			flushProse()
			table = append(table, line)
			continue
		}
		flushTable()
		prose = append(prose, line)
	}
	flushTable()
	flushProse()
	return strings.Trim(block.String(), "\n")
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
	for _, line := range lines {
		fence := strings.TrimSpace(line)
		switch {
		case !inDiff && (fence == "```diff" || fence == "~~~diff"):
			flush(false)
			inDiff = true
		case inDiff && (fence == codeFence || fence == "~~~"):
			flush(true)
			inDiff = false
		default:
			buf = append(buf, line)
		}
	}
	flush(inDiff)
	if len(segs) == 0 {
		return []segment{{text: text}}
	}
	return segs
}

func renderGlamour(text string, width int, view view) string {
	if width < 1 {
		width = 1
	}
	markdown := view.md
	if markdown == nil {
		markdown = &markdownRenderer{}
	}
	renderer, err := markdown.termRenderer(width)
	if err != nil {
		return wrapWith(view.st.text, text, width)
	}
	out, err := renderer.Render(text)
	if err != nil {
		return wrapWith(view.st.text, text, width)
	}
	lines := strings.Split(out, "\n")
	for i, ln := range lines {
		lines[i] = strings.TrimRight(ln, " ")
	}
	return strings.Trim(strings.Join(lines, "\n"), "\n")
}
