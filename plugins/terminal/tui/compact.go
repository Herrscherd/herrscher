package tui

import (
	tea "github.com/charmbracelet/bubbletea"
)

const (
	verbCompact    = "compact"
	originTerminal = "terminal"
	argName        = "--name"
	argOrigin      = "--origin"
	arrow          = " → "
)

type compaction struct {
	active   bool
	from     int
	to       int
	measured bool
}

type compactResultMsg struct {
	channel string
	out     string
	err     error
}

func (m *model) compactCmd() tea.Cmd {
	tb := m.tabs[m.active]
	if tb == nil {
		return nil
	}
	if tb.busy || tb.compaction.active {
		m.flash = "le compactage attend la fin du tour en cours"
		return nil
	}
	name := m.sessionName(tb.channel)
	if name == "" {
		m.flash = "aucune session vivante à compacter"
		return nil
	}
	tb.busy = true
	tb.compaction = compaction{active: true, from: m.contextTokens(tb)}
	backend := m.tm
	channel := tb.channel
	args := []string{sessionVerb, verbCompact, argName, name, argOrigin, originTerminal}
	return func() tea.Msg {
		out, err := backend.Dispatch(args)
		return compactResultMsg{channel: channel, out: out, err: err}
	}
}

func (m *model) handleCompactResult(msg compactResultMsg) {
	tb := m.tabs[msg.channel]
	if tb == nil || msg.err == nil {
		return
	}
	tb.compaction = compaction{}
	tb.busy = false
	tb.appendEntry(entry{role: roleError, text: msg.err.Error()})
	m.syncViewport()
}

func (tb *tab) measureCompaction(used int) {
	if !tb.compaction.active || used <= 0 {
		return
	}
	tb.compaction.to = used
	tb.compaction.measured = true
	tb.ctxTokens = used
}

func (tb *tab) finishCompaction() {
	if !tb.compaction.active {
		return
	}
	after := unavailable
	if tb.compaction.measured {
		after = formatTokens(tb.compaction.to)
	}
	tb.appendEntry(entry{role: roleNotice, text: "compacté : " + formatTokens(tb.compaction.from) + arrow + after})
	tb.lastCompact = tb.compaction
	tb.compaction = compaction{}
}

func (m *model) lastCompactNote() string {
	tb := m.tabs[m.active]
	if tb == nil || tb.lastCompact.from == 0 {
		return ""
	}
	after := unavailable
	if tb.lastCompact.measured {
		after = formatTokens(tb.lastCompact.to)
	}
	return "dernier compactage : " + formatTokens(tb.lastCompact.from) + arrow + after
}
