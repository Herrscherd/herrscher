package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

func TestHostNoticeRendersItsInlineMarkdown(t *testing.T) {
	item := entry{role: roleNotice, text: "session **herrscher** in `~/dev` see [docs](https://x.dev)", markdown: true}
	got := ansi.Strip(renderEntry(item, 120, view{tr: newTranslator(), st: testStyles()}))
	for _, raw := range []string{"**", "`", "]("} {
		if strings.Contains(got, raw) {
			t.Fatalf("notice kept the markup %q: %q", raw, got)
		}
	}
	if !strings.Contains(got, "session herrscher in ~/dev see docs (https://x.dev)") {
		t.Fatalf("notice text lost: %q", got)
	}
}

func TestShellNoticeKeepsItsOutputVerbatim(t *testing.T) {
	item := entry{role: roleNotice, text: "$ cat README.md\n**bold** stays"}
	got := ansi.Strip(renderEntry(item, 120, view{tr: newTranslator(), st: testStyles()}))
	if !strings.Contains(got, "**bold** stays") {
		t.Fatalf("shell output was rewritten: %q", got)
	}
}

func TestHostNoticeKeepsEachLineTight(t *testing.T) {
	item := entry{role: roleNotice, text: "a much longer first line of the banner\n• Command: `claude`", markdown: true}
	got := ansi.Strip(renderEntry(item, 120, view{tr: newTranslator(), st: testStyles()}))
	if !strings.Contains(got, "• Command: claude") {
		t.Fatalf("a short line was padded before its code span: %q", got)
	}
}
