package tui

import (
	"errors"
	"log/slog"

	contracts "github.com/Herrscherd/herrscher-contracts"
	"github.com/Herrscherd/herrscher/core/clitext"
	"github.com/Herrscherd/herrscher/core/timeline"
)

func (m *model) settleCalls(target *tab, event contracts.Event) {
	calls, err := target.calls.Settle(event, timeline.AtGap)
	if errors.Is(err, timeline.ErrFull) {
		target.appendEntry(entry{role: roleNotice, text: m.cliText(clitext.ToolBound, clitext.Params{clitext.ParamLimit: timeline.MaxCalls})})
	}
	m.appendCalls(target, calls)
}

func (m *model) appendCalls(target *tab, calls []timeline.Call) {
	for _, call := range calls {
		key, params := clitext.ToolCallLine(m.tr.lang, call)
		target.appendEntry(entry{role: roleNotice, text: m.cliText(key, params)})
	}
}

func (m *model) cliText(key clitext.Key, params clitext.Params) string {
	text, err := m.tr.messages.Text(m.tr.lang, key, params)
	if err != nil {
		slog.Warn("tool timeline line not rendered", "key", key, "err", err)
		return string(key)
	}
	return text
}
