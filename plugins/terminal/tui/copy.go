package tui

import (
	"strings"
)

const (
	copyReply = "reply"
	copyTurn  = "turn"
	copyAll   = "all"
)

func copyTargets() []string {
	return []string{copyReply, copyTurn, copyAll}
}

func (m *model) copyCmd(rest []string) {
	target := copyReply
	for _, tok := range rest {
		if tok == "" || strings.HasPrefix(tok, "--") {
			continue
		}
		target = strings.ToLower(tok)
		break
	}
	if !validCopyTarget(target) {
		m.flash = m.tr.t("tui.copy.what", map[string]any{"Targets": strings.Join(copyTargets(), ", ")})
		return
	}
	m.copyTarget(target)
}

func validCopyTarget(text string) bool {
	for _, t := range copyTargets() {
		if t == text {
			return true
		}
	}
	return false
}

func (m *model) copyTarget(target string) {
	tab := m.tabs[m.active]
	if tab == nil || m.clip == nil {
		return
	}
	text := copyText(m.tr, tab.entries, target)
	if strings.TrimSpace(text) == "" {
		m.flash = m.tr.t("tui.copy.nothing", nil)
		return
	}
	if err := m.clip.WriteText(m.runContext(), text); err != nil {
		m.flash = m.tr.t("tui.copy.failed", map[string]any{tplArgReason: localizedReason(m.tr, err)})
		return
	}
	n := len(strings.Split(text, "\n"))
	m.flash = m.tr.t("tui.copy.done", map[string]any{"Target": copyTargetLabel(m.tr, target), tplArgCount: n})
}

func copyText(trans translator, entries []entry, target string) string {
	switch target {
	case copyReply:
		for index := len(entries) - 1; index >= 0; index-- {
			if entries[index].role == roleAgent {
				return strings.TrimSpace(entries[index].text)
			}
		}
		return ""
	case copyTurn:
		return renderCopy(trans, entries[lastTurnStart(entries):])
	default:
		return renderCopy(trans, entries)
	}
}

func lastTurnStart(entries []entry) int {
	for index := len(entries) - 1; index >= 0; index-- {
		if entries[index].role == roleYou {
			return index
		}
	}
	return 0
}

func renderCopy(trans translator, entries []entry) string {
	var blocks []string
	for index := range entries {
		item := &entries[index]
		text := strings.TrimSpace(item.text)
		if text == "" {
			continue
		}
		switch item.role {
		case roleYou:
			blocks = append(blocks, trans.t("tui.copy.entry", map[string]any{tplArgLabel: trans.t("tui.copy.you", nil), tplArgText: text}))
		case roleAgent:
			blocks = append(blocks, trans.t("tui.copy.entry", map[string]any{tplArgLabel: trans.t("tui.copy.agent", nil), tplArgText: text}))
		}
	}
	return strings.Join(blocks, "\n\n")
}

func copyTargetLabel(trans translator, target string) string {
	switch target {
	case copyReply:
		return trans.t("tui.copy.target.reply", nil)
	case copyTurn:
		return trans.t("tui.copy.target.turn", nil)
	case copyAll:
		return trans.t("tui.copy.target.all", nil)
	}
	return target
}
