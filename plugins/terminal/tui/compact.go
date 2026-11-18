package tui

import (
	"log/slog"

	tea "github.com/charmbracelet/bubbletea"

	contracts "github.com/Herrscherd/herrscher-contracts"
	"github.com/Herrscherd/herrscher/core/eventwire"
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
	target := m.tabs[m.active]
	if target == nil {
		return nil
	}
	if target.busy || target.compaction.active {
		m.flash = m.tr.t("tui.status.compact_waits", nil)
		return nil
	}
	name := m.sessionName(target.channel)
	if name == "" {
		m.flash = m.tr.t("tui.status.compact_no_session", nil)
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

func (target *tab) finishCompaction(trans translator) {
	if !target.compaction.active {
		return
	}
	after := unavailable
	if target.compaction.measured {
		after = formatTokens(target.compaction.to)
	}
	target.appendEntry(entry{role: roleNotice, text: trans.t("tui.render.compacted", map[string]any{rangeParam: formatTokens(target.compaction.from) + arrow + after})})
	target.lastCompact = target.compaction
	target.compaction = compaction{}
}

func isCompactionEvent(kind string) bool {
	return kind == eventwire.EventCompactionStarted || kind == eventwire.EventCompactionDone || kind == eventwire.EventCompactionFailed
}

func (target *tab) startCompaction(event contracts.Event) {
	target.busy = true
	target.compaction = compaction{active: true, from: event.Tokens, measured: event.Tokens > 0}
}

func (target *tab) completeCompaction(trans translator, event contracts.Event) {
	if event.Tokens > 0 {
		target.compaction.to = event.Tokens
		target.compaction.measured = true
		target.ctxTokens = event.Tokens
		target.ctxMeasured = true
	} else {
		target.ctxTokens = 0
		target.ctxMeasured = false
	}
	target.finishCompaction(trans)
	if event.Text != "" {
		target.appendEntry(entry{role: roleNotice, text: event.Text})
	}
	target.busy = false
}

func (target *tab) failCompaction(trans translator, event contracts.Event) {
	target.compaction = compaction{}
	target.busy = false
	if text := compactFailureText(trans, event); text != "" {
		target.appendEntry(entry{role: roleError, text: text})
	}
}

func compactFailureText(trans translator, event contracts.Event) string {
	failure, err := eventwire.DecodeCompactionFailure(event.Value)
	if err != nil {
		slog.Warn("compaction failure not shown, its payload is not readable", "value", event.Value, "err", err)
		return ""
	}
	if failure.Code != 0 {
		if text, renderErr := contracts.NewRenderer().Render(trans.lang, contracts.Notice{Code: contracts.Code(failure.Code)}); renderErr == nil {
			return text
		}
	}
	if failure.Code == 0 && failure.Reason == "" {
		return ""
	}
	return trans.t(compactFailureKey(failure.Reason), nil)
}

func compactFailureKey(reason string) string {
	switch reason {
	case eventwire.CompactReasonBridgeGone:
		return "tui.compaction.bridge_gone"
	case eventwire.CompactReasonNoCompactorFactory:
		return "tui.compaction.no_compactor_factory"
	case eventwire.CompactReasonNilCompactor:
		return "tui.compaction.nil_compactor"
	case eventwire.CompactReasonNoCompactorPlugin:
		return "tui.compaction.no_compactor_plugin"
	case eventwire.CompactReasonNoHistoryPort:
		return "tui.compaction.no_history_port"
	case eventwire.CompactReasonHistoryTimeout:
		return "tui.compaction.history_timeout"
	case eventwire.CompactReasonHistoryTooLarge:
		return "tui.compaction.history_too_large"
	case eventwire.CompactReasonTooManyEdits:
		return "tui.compaction.too_many_edits"
	}
	return "tui.compaction.failed"
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
	return m.tr.t("tui.render.last_compact", map[string]any{rangeParam: formatTokens(target.lastCompact.from) + arrow + after})
}
