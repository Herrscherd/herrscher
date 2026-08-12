package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// A tool line is the most repeated shape in a long session and the one the TUI
// treated as prose: the bus hands over the command exactly as it was run, and a
// compound shell command runs to ten wrapped lines of one colour. Ten lines of
// cyan next to ten more is not a transcript a reader can scan — so the line gets a
// budget, its folds hang under their target, and the session paths that eat the
// first line and a half are said in the two segments that identify them. alt+e
// lifts all three: nothing here is destroyed, only spent.

// toolFamilies maps a backend tool name to the family it is rendered as. The
// vocabulary is closed on purpose: the bus carries a tool line as one opaque
// string (the hub flattens name and detail together in emitBackendEvent), so
// the name can only be recovered by reading the first word. Guessing wider than
// a known list would mistype ordinary prose that happens to start with a verb.
var toolFamilies = map[string]string{
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

// The tool families, each with its own glyph so a scan down the gutter reads as
// what the agent has been doing rather than as one undifferentiated column.
const (
	familyRead   = "read"
	familyWrite  = "write"
	familyRun    = "run"
	familySearch = "search"
	familyWeb    = "web"
	familyAgent  = "agent"
)

// toolVerb splits a tool status line into the family it belongs to and the rest
// of the line. An unrecognised first word yields an empty family and the line
// untouched — an unknown tool must stay readable, never disappear.
func toolVerb(text string) (family, rest string) {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return "", ""
	}
	head, tail, _ := strings.Cut(trimmed, " ")
	fam, ok := toolFamilies[strings.ToLower(head)]
	if !ok {
		return "", trimmed
	}
	return fam, strings.TrimSpace(tail)
}

// familyWidth pads the family column so targets line up down the gutter. It is
// the longest family name plus a separating space.
const familyWidth = 7

// toolTargetColumn is the cell a typed tool line's target starts in: the glyph, a
// space, and the family column. A fold resumes here, which is what makes a folded
// line read as the same line rather than as the next one.
const toolTargetColumn = 2 + familyWidth

// toolLineCap is how many lines one part of a tool line may occupy. Two, because
// one is not always enough to reach the target of a nested path and three is
// already a paragraph — and a column of paragraphs is what made the transcript
// unreadable.
const toolLineCap = 2

// minHangWidth is how much room a hanging indent must leave for the text beside
// it. Below this the indent is what gives way: a target rendered two cells at a
// time is less readable than one that starts back at the gutter.
const minHangWidth = 8

// renderTool renders a tool status entry: a typed glyph and the tool's target on
// the first line, any following lines as dim continuations. A line whose first
// word is not a known tool keeps the generic glyph and its full text.
//
// The line is styled as a whole rather than by segment: a nested style ends with
// its own reset, which would strip the colour from everything after it.
func renderTool(text string, width int, v view) string {
	lines := strings.Split(text, "\n")
	family, rest := toolVerb(lines[0])
	gutter := familyGlyph(family) + " "
	if family != "" {
		gutter += family + strings.Repeat(" ", max(1, familyWidth-len(family)))
	}
	var b strings.Builder
	b.WriteString(toolPart(gutter, rest, toolStyle, width, v))
	for _, ln := range lines[1:] {
		b.WriteByte('\n')
		b.WriteString(toolPart("  "+glyphResult+" ", ln, dimStyle, width, v))
	}
	return b.String()
}

// toolPart lays out one part of a tool line — the call itself, or one of the
// results under it — as its gutter followed by body, folded under the gutter and
// spent against the budget. Both parts obey the same rule: a fifteen-line result
// is the same illegibility as a ten-line command.
func toolPart(gutter, body string, style lipgloss.Style, width int, v view) string {
	lines := hangingLines(gutter, body, width)
	if !v.expand && len(lines) > toolLineCap {
		// The abbreviation is spent to buy lines back, so it is only spent once the
		// line has run out of them. A target that already fits keeps every segment of
		// its path: shortening it there would cost fidelity and save nothing.
		lines = hangingLines(gutter, abbreviatePaths(body), width)
		if len(lines) > toolLineCap {
			hidden := len(lines) - toolLineCap
			lines = lines[:toolLineCap]
			lines[len(lines)-1] = withMoreMarker(lines[len(lines)-1], hidden, width)
		}
	}
	for i, ln := range lines {
		lines[i] = style.Render(ln)
	}
	return strings.Join(lines, "\n")
}

// hangingLines folds gutter+body to width with every line after the first indented
// to the gutter's own width. It works on unstyled text so that the caller can
// style each finished line as a whole.
func hangingLines(gutter, body string, width int) []string {
	offset := lipgloss.Width(gutter)
	if width-offset < minHangWidth {
		// No room to hang: the body would come out a few cells at a time. The whole
		// line folds back at the gutter instead, which at this width is all there is.
		return wrapPlain(gutter+body, width)
	}
	wrapped := wrapPlain(body, width-offset)
	out := make([]string, len(wrapped))
	for i, ln := range wrapped {
		if i == 0 {
			out[i] = gutter + ln
			continue
		}
		out[i] = strings.Repeat(" ", offset) + ln
	}
	return out
}

// wrapPlain word-wraps s to width and returns the lines, unstyled and unpadded.
// The wrap is lipgloss's so that a folded tool line breaks exactly where the rest
// of the transcript would break, glyph widths included.
func wrapPlain(s string, width int) []string {
	if width < 1 {
		width = 1
	}
	out := strings.Split(lipgloss.NewStyle().Width(width).Render(s), "\n")
	for i := range out {
		out[i] = strings.TrimRight(out[i], " ")
	}
	return out
}

// withMoreMarker right-aligns the count of what was kept back on the last visible
// line, clipping that line if it needs the room. Hiding lines without saying how
// many would be the TUI deciding what the operator may know it is missing.
//
// The marker carries no style of its own: it is rendered as part of its line,
// because a dim run inside a coloured line ends with a reset that strips the
// colour from everything after it.
func withMoreMarker(line string, hidden, width int) string {
	marker := fmt.Sprintf("+%d %s", hidden, glyphMoreLines)
	room := width - lipgloss.Width(marker) - 1
	if room < 1 {
		return line // too narrow to say anything; the line itself is worth more
	}
	line = truncate(line, room)
	return line + strings.Repeat(" ", room-lipgloss.Width(line)+1) + marker
}

// The path abbreviation. A session path — eight segments of host layout before the
// first thing the reader is looking for — is the single biggest consumer of a tool
// line, and it is the same on every call.
const (
	// pathDepth is how many segments an absolute path may have before it is worth
	// abbreviating. Three still reads as a place: /home/shan/dev.
	pathDepth = 4

	// pathKeep is how much of it survives. Two, because two segments identify a
	// target — …/ch-8688/core says where the command ran, …/core says nothing.
	pathKeep = 2
)

// abbreviatePaths shortens every deep absolute path in s, leaving everything else
// exactly as the agent wrote it: the spacing, the flags, and the relative paths,
// which are short because they are relative to something.
func abbreviatePaths(s string) string {
	// Split on the single space and rejoin with it: runs of spaces survive as the
	// empty fields between them, so the command's own spacing is not reflowed.
	fields := strings.Split(s, " ")
	for i, f := range fields {
		fields[i] = abbreviatePath(f)
	}
	return strings.Join(fields, " ")
}

// abbreviatePath abbreviates one token if it is a deep absolute path. Punctuation
// around it belongs to the command — a trailing semicolon separates two commands,
// a quote encloses one — so it is set aside and put back.
func abbreviatePath(token string) string {
	cut := len(token) - len(strings.TrimLeft(token, `"'(`))
	opening, rest := token[:cut], token[cut:]
	trimmed := strings.TrimRight(rest, `;,)"'`)
	closing := rest[len(trimmed):]

	body, ok := strings.CutPrefix(trimmed, "/")
	if !ok {
		// A tilde is the root of the path it opens, so ~/a/b/c/d is four deep.
		if body, ok = strings.CutPrefix(trimmed, "~/"); !ok {
			return token
		}
	}
	segs := strings.Split(body, "/")
	if n := len(segs); n > 0 && segs[n-1] == "" {
		segs = segs[:n-1] // a trailing slash is not a segment
	}
	if len(segs) < pathDepth {
		return token
	}
	return opening + "…/" + strings.Join(segs[len(segs)-pathKeep:], "/") + closing
}
