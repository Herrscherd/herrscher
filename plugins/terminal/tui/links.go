package tui

import (
	"regexp"
	"strings"
)

// LinkKind separates what a reference points at, because the two are opened by
// different things: a URL by the system handler, a file by the editor.
type LinkKind int

// The kinds of target a link can point at.
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
	for lineIndex, line := range lines {
		if fence := strings.TrimSpace(line); strings.HasPrefix(fence, codeFence) || strings.HasPrefix(fence, "~~~") {
			inFence = !inFence
			continue
		}
		if inFence {
			continue
		}
		out = append(out, lineLinks(lineIndex, line)...)
	}
	return out
}

func lineLinks(line int, text string) []Link {
	var out []Link
	var claimed [][2]int
	claim := func(lo, hi int) { claimed = append(claimed, [2]int{lo, hi}) }
	overlaps := func(high, until int) bool {
		for _, c := range claimed {
			if high < c[1] && c[0] < until {
				return true
			}
		}
		return false
	}

	for _, match := range linkMarkdownPattern.FindAllStringSubmatchIndex(text, -1) {
		if hasControl(text[match[4]:match[5]]) {
			continue
		}
		claim(match[0], match[1])
		out = append(out, Link{
			Label: text[match[2]:match[3]], Target: text[match[4]:match[5]],
			Line: line, Start: match[0], End: match[1], Kind: LinkURL,
		})
	}
	for _, match := range linkURLPattern.FindAllStringIndex(text, -1) {
		if overlaps(match[0], match[1]) {
			continue
		}
		claim(match[0], match[1])
		out = append(out, Link{
			Label: text[match[0]:match[1]], Target: text[match[0]:match[1]],
			Line: line, Start: match[0], End: match[1], Kind: LinkURL,
		})
	}
	for _, match := range linkFilePattern.FindAllStringIndex(text, -1) {
		if overlaps(match[0], match[1]) {
			continue
		}
		claim(match[0], match[1])
		out = append(out, Link{
			Label: text[match[0]:match[1]], Target: text[match[0]:match[1]],
			Line: line, Start: match[0], End: match[1], Kind: LinkFile,
		})
	}
	sortLinksByStart(out)
	return out
}

func hasControl(text string) bool {
	for i := range len(text) {
		if text[i] < 0x20 || text[i] == 0x7f {
			return true
		}
	}
	return false
}

func sortLinksByStart(links []Link) {
	for i := 1; i < len(links); i++ {
		for j := i; j > 0 && links[j].Start < links[j-1].Start; j-- {
			links[j], links[j-1] = links[j-1], links[j]
		}
	}
}

const (
	ansiReverse    = "\x1b[7m"
	ansiReverseOff = "\x1b[27m"
)

func renderLink(link Link, caps Capabilities, selected bool, st uiStyles) string {
	label := st.link.Render(link.Label)
	if selected {
		label = ansiReverse + label + ansiReverseOff
	}
	if !caps.Hyperlinks {
		return label
	}
	return "\x1b]8;;" + link.Target + "\x1b\\" + label + "\x1b]8;;\x1b\\"
}

func applyLinks(lines []string, links []Link, caps Capabilities, sel int, styles uiStyles) []string {
	for index := len(links) - 1; index >= 0; index-- {
		link := links[index]
		if link.Line < 0 || link.Line >= len(lines) || link.End > len(lines[link.Line]) {
			continue
		}
		ln := lines[link.Line]
		lines[link.Line] = ln[:link.Start] + renderLink(link, caps, index == sel, styles) + ln[link.End:]
	}
	return lines
}
