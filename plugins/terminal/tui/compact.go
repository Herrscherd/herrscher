package tui

import (
	tea "github.com/charmbracelet/bubbletea"

	contracts "github.com/Herrscherd/herrscher-contracts"
)

const (
	verbCompact            = "compact"
	eventCompactionStarted = "compaction_started"
	eventCompactionDone    = "compaction_done"
	eventCompactionFailed  = "compaction_failed"
	originTerminal         = "terminal"
	argName                = "--name"
	argOrigin              = "--origin"
	arrow                  = " → "
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
	target := m.tabs[m.active]
	if target == nil {
		return nil
	}
	if target.busy || target.compaction.active {
		m.flash = "le compactage attend la fin du tour en cours"
		return nil
	}
	name := m.sessionName(target.channel)
	if name == "" {
		m.flash = "aucune session vivante à compacter"
		return nil
	}
	target.busy = true
	target.compaction = compaction{active: true, from: m.contextTokens(target)}
	backend := m.tm
	channel := target.channel
	args := []string{sessionVerb, verbCompact, argName, name, argOrigin, originTerminal}
	return func() tea.Msg {
		out, err := backend.Dispatch(args)
		return compactResultMsg{channel: channel, out: out, err: err}
	}
}

func (m *model) handleCompactResult(msg compactResultMsg) {
	target := m.tabs[msg.channel]
	if target == nil || msg.err == nil {
		return
	}
	target.compaction = compaction{}
	target.busy = false
	target.appendEntry(entry{role: roleError, text: msg.err.Error()})
	m.syncViewport()
}

func (target *tab) measureCompaction(used int) {
	if !target.compaction.active || used <= 0 {
		return
	}
	target.compaction.to = used
	target.compaction.measured = true
	target.ctxTokens = used
}

func (target *tab) finishCompaction() {
	if !target.compaction.active {
		return
	}
	after := unavailable
	if target.compaction.measured {
		after = formatTokens(target.compaction.to)
	}
	target.appendEntry(entry{role: roleNotice, text: "compacté : " + formatTokens(target.compaction.from) + arrow + after})
	target.lastCompact = target.compaction
	target.compaction = compaction{}
}

func (target *tab) startCompaction(event contracts.Event) {
	target.busy = true
	target.compaction = compaction{active: true, from: event.Tokens, measured: event.Done}
}

func (target *tab) completeCompaction(event contracts.Event) {
	if event.Done && event.Tokens > 0 {
		target.compaction.to = event.Tokens
		target.compaction.measured = true
		target.ctxTokens = event.Tokens
		target.ctxMeasured = true
	}
	target.finishCompaction()
	if event.Text != "" {
		target.appendEntry(entry{role: roleNotice, text: event.Text})
	}
	target.busy = false
}

func (target *tab) failCompaction(event contracts.Event) {
	target.compaction = compaction{}
	target.busy = false
	target.appendEntry(entry{role: roleError, text: event.Text})
}

func (m *model) lastCompactNote() string {
	target := m.tabs[m.active]
	if target == nil || target.lastCompact.from == 0 {
		return ""
	}
	after := unavailable
	if target.lastCompact.measured {
		after = formatTokens(target.lastCompact.to)
	}
	return "dernier compactage : " + formatTokens(target.lastCompact.from) + arrow + after
}
