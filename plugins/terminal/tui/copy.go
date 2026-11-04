package tui

import (
	"fmt"
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
		m.flash = "copier quoi ? " + strings.Join(copyTargets(), ", ")
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
	text := copyText(tab.entries, target)
	if strings.TrimSpace(text) == "" {
		m.flash = "rien à copier ici"
		return
	}
	if err := m.clip.WriteText(m.runContext(), text); err != nil {
		m.flash = "copie impossible : " + err.Error()
		return
	}
	n := len(strings.Split(text, "\n"))
	m.flash = fmt.Sprintf("%s copié, %d ligne%s", target, n, plural(n))
}

func copyText(entries []entry, target string) string {
	switch target {
	case copyReply:
		for index := len(entries) - 1; index >= 0; index-- {
			if entries[index].role == roleAgent {
				return strings.TrimSpace(entries[index].text)
			}
		}
		return ""
	case copyTurn:
		return renderCopy(entries[lastTurnStart(entries):])
	default:
		return renderCopy(entries)
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

func renderCopy(entries []entry) string {
	var blocks []string
	for index := range entries {
		item := &entries[index]
		text := strings.TrimSpace(item.text)
		if text == "" {
			continue
		}
		switch item.role {
		case roleYou:
			blocks = append(blocks, "you: "+text)
		case roleAgent:
			blocks = append(blocks, "agent: "+text)
		}
	}
	return strings.Join(blocks, "\n\n")
}
