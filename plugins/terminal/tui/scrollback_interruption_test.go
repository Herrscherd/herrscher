package tui

import (
	"strings"
	"testing"

	contracts "github.com/Herrscherd/herrscher-contracts"
)

func TestLegacyInterruptionTextIsRenderedFromItsCode(t *testing.T) {
	m, tb := compactionTurnTab(t)
	notice := contracts.Notice{Code: contracts.CodeTurnInterrupted}
	want, err := contracts.NewRenderer().Render(m.tr.lang, notice)
	if err != nil || want == "" {
		t.Fatalf("interruption notice does not render: %q, %v", want, err)
	}
	m.seedScrollback(tb, []contracts.ScrollbackLine{{Role: "notice", Notice: &notice, Text: "ancien texte", TurnID: "turn-1"}})
	notices := 0
	for _, e := range tb.entries {
		if strings.Contains(e.text, "ancien texte") {
			t.Fatalf("entries = %+v, the legacy stored text must not be shown", tb.entries)
		}
		if e.role == roleNotice {
			notices++
			if e.text != want {
				t.Fatalf("notice line = %q, want %q", e.text, want)
			}
		}
	}
	if notices != 1 {
		t.Fatalf("entries = %+v, want exactly one interruption line", tb.entries)
	}
}

func TestResumePreviewShowsTheInterruptionInTheTabLanguage(t *testing.T) {
	notice := contracts.Notice{Code: contracts.CodeTurnInterrupted}
	f := &fakeBackend{
		sessions: []contracts.SessionInfo{{Name: "api", ChannelID: "c", Vendor: "claude"}},
		scrollback: map[string][]contracts.ScrollbackLine{"api": {
			{Role: "user", Text: "question"},
			{Role: "notice", Notice: &notice, TurnID: "turn-1"},
		}},
	}
	m := startedModel(f)
	m.resumeRows = []contracts.SessionInfo{{Name: "api"}}
	m.resumeIdx = 0
	m.loadResumePreview()
	want, err := contracts.NewRenderer().Render(m.tr.lang, notice)
	if err != nil || want == "" {
		t.Fatalf("interruption notice does not render: %q, %v", want, err)
	}
	if len(m.resumePreviewLines) != 2 || !strings.Contains(m.resumePreviewLines[1], want) {
		t.Fatalf("resume preview = %q, want the question then the interruption rendered as %q", m.resumePreviewLines, want)
	}
}
