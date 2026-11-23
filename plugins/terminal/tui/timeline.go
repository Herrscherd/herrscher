package tui

import (
	"errors"
	"log/slog"

	contracts "github.com/Herrscherd/herrscher-contracts"
	"github.com/Herrscherd/herrscher/core/clitext"
	"github.com/Herrscherd/herrscher/core/eventwire"
	"github.com/Herrscherd/herrscher/core/timeline"
)

func (m *model) observeToolCalls(target *tab, event contracts.Event) {
	if _, _, err := target.calls.Apply(event); errors.Is(err, timeline.ErrFull) {
		target.appendEntry(entry{role: roleNotice, text: m.cliText(clitext.ToolBound, clitext.Params{clitext.ParamLimit: timeline.MaxCalls})})
	}
}

func (m *model) appendTimeline(target *tab) {
	for _, call := range target.calls.Drain() {
		key, params := clitext.ToolCallLine(m.tr.lang, call)
		target.appendEntry(entry{role: roleNotice, text: m.cliText(key, params)})
	}
}

func (m *model) closeTurnTimeline(target *tab) {
	m.appendTimeline(target)
	target.calls = timeline.Recorder{}
}

func replayedToolStart(target *tab, event contracts.Event) bool {
	return event.T == eventwire.EventStatus && event.ToolCall != nil && target.calls.Knows(event.ToolCall.ID)
}

func (m *model) cliText(key clitext.Key, params clitext.Params) string {
	text, err := m.tr.messages.Text(m.tr.lang, key, params)
	if err != nil {
		slog.Warn("tool timeline line not rendered", "key", key, "err", err)
		return string(key)
	}
	return text
}
