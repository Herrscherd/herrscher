package tui

import (
	"regexp"
	"strings"
)

// LinkKind separates what a reference points at, because the two are opened by
// different things: a URL by the system handler, a file by the editor.
type LinkKind int

const (
	LinkURL LinkKind = iota
	LinkFile
)

// Link is one reference found in the transcript. Label is what the reader sees
// and Target is where it actually goes. A markdown label can name one
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

var linkURLPattern = regexp.MustCompile(`https?://[^ <>"'\x00-\x1f\x7f]*[^ <>"'.,;:!?)\]\x00-\x1f\x7f]`)

var linkMarkdownPattern = regexp.MustCompile(`\[([^\]\n]+)\]\(([^)\s]+)\)`)

var linkFilePattern = regexp.MustCompile(`[\w.~/-]*[\w~-]\.[A-Za-z]\w*:\d+`)

func findLinks(lines []string) []Link {
	var out []Link
	inFence := false
	for i, ln := range lines {
		if fence := strings.TrimSpace(ln); strings.HasPrefix(fence, codeFence) || strings.HasPrefix(fence, "~~~") {
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
		if hasControl(s[m[4]:m[5]]) {
			continue
		}
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

func hasControl(s string) bool {
	for i := range len(s) {
		if s[i] < 0x20 || s[i] == 0x7f {
			return true
		}
	}
	return false
}

func sortLinksByStart(ls []Link) {
	for i := 1; i < len(ls); i++ {
		for j := i; j > 0 && ls[j].Start < ls[j-1].Start; j-- {
			ls[j], ls[j-1] = ls[j-1], ls[j]
		}
	}
}

const (
	ansiReverse    = "\x1b[7m"
	ansiReverseOff = "\x1b[27m"
)

func renderLink(l Link, caps Capabilities, selected bool, st uiStyles) string {
	label := st.link.Render(l.Label)
	if selected {
		label = ansiReverse + label + ansiReverseOff
	}
	if !caps.Hyperlinks {
		return label
	}
	return "\x1b]8;;" + l.Target + "\x1b\\" + label + "\x1b]8;;\x1b\\"
}

func applyLinks(lines []string, links []Link, caps Capabilities, sel int, st uiStyles) []string {
	for i := len(links) - 1; i >= 0; i-- {
		l := links[i]
		if l.Line < 0 || l.Line >= len(lines) || l.End > len(lines[l.Line]) {
			continue
		}
		ln := lines[l.Line]
		lines[l.Line] = ln[:l.Start] + renderLink(l, caps, i == sel, st) + ln[l.End:]
	}
	return lines
}
