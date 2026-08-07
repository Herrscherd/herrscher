package tui

import (
	"regexp"
	"strings"
)

// A transcript is already full of references — URLs the agent quoted, files it
// touched — and the terminal rendered every one of them as inert characters. This
// file finds them; open.go acts on them, and only when asked.

// LinkKind separates what a reference points at, because the two are opened by
// different things: a URL by the system handler, a file by the editor.
type LinkKind int

const (
	LinkURL LinkKind = iota
	LinkFile
)

// Link is one reference found in the transcript. Label is what the reader sees
// and Target is where it actually goes — a markdown label can name one
// destination and point at another, and collapsing the two is precisely how a
// transcript would come to lie. Line, Start and End locate the whole matched
// span in the rendered lines, so it can be replaced in place.
type Link struct {
	Label  string
	Target string
	Line   int
	Start  int
	End    int
	Kind   LinkKind
}

// linkURLPattern matches a bare http(s) URL. The trailing class excludes the
// punctuation that ends a sentence rather than a URL — a full stop after a link
// belongs to the prose, and swallowing it produces a target that 404s.
var linkURLPattern = regexp.MustCompile(`https?://[^\s<>"'\x1b]*[^\s<>"'.,;:!?)\]\x1b]`)

// linkMarkdownPattern matches [label](target).
var linkMarkdownPattern = regexp.MustCompile(`\[([^\]\n]+)\]\(([^)\s]+)\)`)

// linkFilePattern matches a path/file.ext:line reference — the shape a compiler,
// a linter and a stack trace all print, and the one an operator most wants to
// jump to. The extension is required: without it every "word:12" in prose would
// become a link.
var linkFilePattern = regexp.MustCompile(`[\w.~/-]*[\w~-]\.[A-Za-z]\w*:\d+`)

// findLinks returns every reference in lines, in reading order. Lines inside a
// fenced code block are skipped: there the characters are the content, not a
// reference to somewhere else, and linking them misreads the block.
func findLinks(lines []string) []Link {
	var out []Link
	inFence := false
	for i, ln := range lines {
		if fence := strings.TrimSpace(ln); strings.HasPrefix(fence, "```") || strings.HasPrefix(fence, "~~~") {
			inFence = !inFence
			continue
		}
		if inFence {
			continue
		}
		out = append(out, lineLinks(i, ln)...)
	}
	return out
}

// lineLinks finds the references in one line. Markdown links are matched first
// and their spans claimed, so the URL inside one is not also reported bare; file
// references are matched last and skip anything already claimed, so the ":80" of
// a host:port inside a URL is not mistaken for a line number.
func lineLinks(line int, s string) []Link {
	var out []Link
	var claimed [][2]int
	claim := func(lo, hi int) { claimed = append(claimed, [2]int{lo, hi}) }
	overlaps := func(lo, hi int) bool {
		for _, c := range claimed {
			if lo < c[1] && c[0] < hi {
				return true
			}
		}
		return false
	}

	for _, m := range linkMarkdownPattern.FindAllStringSubmatchIndex(s, -1) {
		claim(m[0], m[1])
		out = append(out, Link{
			Label: s[m[2]:m[3]], Target: s[m[4]:m[5]],
			Line: line, Start: m[0], End: m[1], Kind: LinkURL,
		})
	}
	for _, m := range linkURLPattern.FindAllStringIndex(s, -1) {
		if overlaps(m[0], m[1]) {
			continue
		}
		claim(m[0], m[1])
		out = append(out, Link{
			Label: s[m[0]:m[1]], Target: s[m[0]:m[1]],
			Line: line, Start: m[0], End: m[1], Kind: LinkURL,
		})
	}
	for _, m := range linkFilePattern.FindAllStringIndex(s, -1) {
		if overlaps(m[0], m[1]) {
			continue
		}
		claim(m[0], m[1])
		out = append(out, Link{
			Label: s[m[0]:m[1]], Target: s[m[0]:m[1]],
			Line: line, Start: m[0], End: m[1], Kind: LinkFile,
		})
	}
	sortLinksByStart(out)
	return out
}

// sortLinksByStart puts a line's links back into reading order after the three
// passes found them out of it. Insertion sort: a line has a handful of links.
func sortLinksByStart(ls []Link) {
	for i := 1; i < len(ls); i++ {
		for j := i; j > 0 && ls[j].Start < ls[j-1].Start; j-- {
			ls[j], ls[j-1] = ls[j-1], ls[j]
		}
	}
}

// The selection mark, written as raw SGR: reverse video is the one highlight
// every terminal has had since before it had colour.
const (
	ansiReverse    = "\x1b[7m"
	ansiReverseOff = "\x1b[27m"
)

// renderLink draws one link: a real OSC 8 hyperlink where the terminal has them,
// styled text where it does not. The plain rendering is not a failure — it stays
// selectable and copyable, which is what the operator had before and more than
// an escape sequence printed as litter would leave them.
func renderLink(l Link, caps Capabilities, selected bool) string {
	label := linkStyle.Render(l.Label)
	if selected {
		// Reverse video rather than another colour: this has to be visible on a
		// 16-colour terminal and through whatever the theme is doing underneath.
		label = ansiReverse + label + ansiReverseOff
	}
	if !caps.Hyperlinks {
		return label
	}
	return "\x1b]8;;" + l.Target + "\x1b\\" + label + "\x1b]8;;\x1b\\"
}

// applyLinks rewrites lines so every link is rendered as one, with sel (an index
// into links, or -1) highlighted. It walks each line's spans from the right so
// an earlier replacement cannot shift a later one's offsets.
func applyLinks(lines []string, links []Link, caps Capabilities, sel int) []string {
	for i := len(links) - 1; i >= 0; i-- {
		l := links[i]
		if l.Line < 0 || l.Line >= len(lines) || l.End > len(lines[l.Line]) {
			continue
		}
		ln := lines[l.Line]
		lines[l.Line] = ln[:l.Start] + renderLink(l, caps, i == sel) + ln[l.End:]
	}
	return lines
}
