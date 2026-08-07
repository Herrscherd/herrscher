package tui

import (
	"strings"
	"testing"
)

// A transcript is already full of links; the terminal simply never noticed them.
// The table is the contract: what counts as one, and what its target resolves to.
func TestFindLinks(t *testing.T) {
	cases := []struct {
		name  string
		lines []string
		want  []Link
	}{
		{
			name:  "a bare url",
			lines: []string{"see https://example.com/a for more"},
			want:  []Link{{Label: "https://example.com/a", Target: "https://example.com/a", Line: 0, Start: 4, End: 25, Kind: LinkURL}},
		},
		{
			// The case the status line exists for: the label names one destination
			// and the target is another. Both are captured, and they differ.
			name:  "a markdown link whose label is not its target",
			lines: []string{"read [the docs](https://elsewhere.test/x)"},
			want:  []Link{{Label: "the docs", Target: "https://elsewhere.test/x", Line: 0, Start: 5, End: 41, Kind: LinkURL}},
		},
		{
			name:  "a file reference",
			lines: []string{"broke at core/host/turnloop.go:42 today"},
			want:  []Link{{Label: "core/host/turnloop.go:42", Target: "core/host/turnloop.go:42", Line: 0, Start: 9, End: 33, Kind: LinkFile}},
		},
		{
			name:  "trailing punctuation is not part of the target",
			lines: []string{"go to https://example.com/a."},
			want:  []Link{{Label: "https://example.com/a", Target: "https://example.com/a", Line: 0, Start: 6, End: 27, Kind: LinkURL}},
		},
		{
			// Inside a fence the characters are the content, not a reference to
			// somewhere else — turning them into a link would misread the block.
			name:  "a url inside a code block is not a link",
			lines: []string{"```", "curl https://example.com/a", "```", "then https://example.com/b"},
			want:  []Link{{Label: "https://example.com/b", Target: "https://example.com/b", Line: 3, Start: 5, End: 26, Kind: LinkURL}},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := findLinks(c.lines)
			if len(got) != len(c.want) {
				t.Fatalf("findLinks = %+v, want %+v", got, c.want)
			}
			for i := range c.want {
				if got[i] != c.want[i] {
					t.Errorf("link %d = %+v, want %+v", i, got[i], c.want[i])
				}
			}
		})
	}
}

// A markdown link's label and target must stay distinct all the way through:
// collapsing them is how a transcript would come to lie about where it goes.
func TestMarkdownLabelAndTargetStayDistinct(t *testing.T) {
	got := findLinks([]string{"[example.com](https://phish.test/)"})
	if len(got) != 1 {
		t.Fatalf("findLinks = %+v", got)
	}
	if got[0].Label == got[0].Target {
		t.Fatalf("label and target must not be collapsed: %+v", got[0])
	}
}

func TestRenderLinkUsesOSC8WhereAvailable(t *testing.T) {
	l := Link{Label: "the docs", Target: "https://example.com/x", Kind: LinkURL}

	with := renderLink(l, Capabilities{Hyperlinks: true}, false)
	if !strings.Contains(with, "\x1b]8;;https://example.com/x") {
		t.Errorf("an OSC 8 terminal must get the hyperlink: %q", with)
	}
	if !strings.HasSuffix(with, "\x1b]8;;\x1b\\") {
		t.Errorf("the hyperlink must be closed, or the rest of the line joins it: %q", with)
	}

	without := renderLink(l, Capabilities{}, false)
	if strings.Contains(without, "\x1b]8;;") {
		t.Errorf("a terminal without OSC 8 must not be sent the escape: %q", without)
	}
	if !strings.Contains(without, "the docs") {
		t.Errorf("styled text must still say what it said: %q", without)
	}
}

// The selected link has to be findable by eye, on a terminal with hyperlinks and
// on one without: the gesture is aimed at whichever one is highlighted.
func TestRenderLinkHighlightsTheSelection(t *testing.T) {
	l := Link{Label: "x", Target: "https://example.com/x", Kind: LinkURL}
	for _, caps := range []Capabilities{{Hyperlinks: true}, {}} {
		if renderLink(l, caps, true) == renderLink(l, caps, false) {
			t.Fatalf("the selected link must render differently (caps %+v)", caps)
		}
	}
}

// A markdown target is whatever the agent wrote between the parens, and it does
// not stay a string: it is written into the OSC 8 payload, where an ESC ends the
// sequence and hands the terminal whatever follows as a command of its own. Such
// a target is not a link, and the characters stay on screen as text.
func TestATargetCarryingAControlByteIsNotALink(t *testing.T) {
	for _, s := range []string{
		"see [docs](https://a.test/\x1b]0;pwned\x07x) here",
		"see [docs](https://a.test/\x07x) here",
	} {
		links := findLinks([]string{s})
		for _, l := range links {
			if strings.ContainsAny(l.Target, "\x1b\x07") {
				t.Fatalf("a control byte must not reach a link target: %q", l.Target)
			}
		}
	}

	// And what does survive detection is never emitted raw into an OSC 8 payload.
	out := renderLink(Link{Label: "x", Target: "https://a.test/ok"}, Capabilities{Hyperlinks: true}, false)
	if !strings.Contains(out, "\x1b]8;;https://a.test/ok\x1b\\") {
		t.Fatalf("an ordinary target must still hyperlink: %q", out)
	}
}
