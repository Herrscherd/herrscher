package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
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

func TestInlineMarkdownLeavesUnderscoreIdentifiersAlone(t *testing.T) {
	plain, strong, code := lipgloss.NewStyle(), lipgloss.NewStyle().Bold(true), lipgloss.NewStyle().Italic(true)
	for _, text := range []string{"run __init__.py now", "the __pycache__ dir", "my__var__name", "__init__/app.py"} {
		if got, want := styleInlineLine(text, plain, strong, code), plain.Render(text); got != want {
			t.Errorf("styleInlineLine(%q) = %q, want the text unstyled %q", text, got, want)
		}
	}
}

func TestInlineMarkdownBoldsADoubleUnderscorePhrase(t *testing.T) {
	plain, strong, code := lipgloss.NewStyle(), lipgloss.NewStyle().Bold(true), lipgloss.NewStyle().Italic(true)
	got := styleInlineLine("this is __very important__ now", plain, strong, code)
	want := plain.Render("this is ") + strong.Render("very important") + plain.Render(" now")
	if got != want {
		t.Fatalf("styleInlineLine = %q, want %q", got, want)
	}
}

func TestInlineMarkdownKeepsBalancedParenthesesInLinks(t *testing.T) {
	plain, strong, code := lipgloss.NewStyle(), lipgloss.NewStyle().Bold(true), lipgloss.NewStyle().Italic(true)
	got := styleInlineLine("see [wiki](https://en.wikipedia.org/wiki/Foo_(bar)) now", plain, strong, code)
	want := plain.Render("see ") + strong.Render("wiki") + plain.Render(" (https://en.wikipedia.org/wiki/Foo_(bar))") + plain.Render(" now")
	if got != want {
		t.Fatalf("styleInlineLine = %q, want %q", got, want)
	}
}

func TestInlineMarkdownAppliesTheStylesToEachSpan(t *testing.T) {
	plain, strong, code := lipgloss.NewStyle(), lipgloss.NewStyle().Bold(true), lipgloss.NewStyle().Italic(true)
	got := styleInlineLine("a **b** `c` [d](https://e.dev)", plain, strong, code)
	want := plain.Render("a ") + strong.Render("b") + plain.Render(" ") + code.Render("c") + plain.Render(" ") + strong.Render("d") + plain.Render(" (https://e.dev)")
	if got != want {
		t.Fatalf("styleInlineLine = %q, want %q", got, want)
	}
}

func TestNoticeKeepsIdentifierUnderscoresOnScreen(t *testing.T) {
	out := ansi.Strip(renderEntry(entry{role: roleNotice, text: "run __init__.py now", markdown: true}, 120, view{tr: newTranslator(), st: testStyles()}))
	if !strings.Contains(out, "run __init__.py now") {
		t.Fatalf("identifier underscores were eaten: %q", out)
	}
}
