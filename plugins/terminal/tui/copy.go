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

var copyTargets = []string{copyReply, copyTurn, copyAll}

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
		m.flash = "copier quoi ? " + strings.Join(copyTargets, ", ")
		return
	}
	m.copyTarget(target)
}

func validCopyTarget(s string) bool {
	for _, t := range copyTargets {
		if t == s {
			return true
		}
	}
	return false
}

func (m *model) copyTarget(target string) {
	tb := m.tabs[m.active]
	if tb == nil || m.clip == nil {
		return
	}
	text := copyText(tb.entries, target)
	if strings.TrimSpace(text) == "" {
		m.flash = "rien à copier ici"
		return
	}
	if err := m.clip.WriteText(text); err != nil {
		m.flash = "copie impossible : " + err.Error()
		return
	}
	n := len(strings.Split(text, "\n"))
	m.flash = fmt.Sprintf("%s copié, %d ligne%s", target, n, plural(n))
}

func copyText(entries []entry, target string) string {
	switch target {
	case copyReply:
		for i := len(entries) - 1; i >= 0; i-- {
			if entries[i].role == roleAgent {
				return strings.TrimSpace(entries[i].text)
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
	for i := len(entries) - 1; i >= 0; i-- {
		if entries[i].role == roleYou {
			return i
		}
	}
	return 0
}

func renderCopy(entries []entry) string {
	var blocks []string
	for _, e := range entries {
		text := strings.TrimSpace(e.text)
		if text == "" {
			continue
		}
		switch e.role {
		case roleYou:
			blocks = append(blocks, "you: "+text)
		case roleAgent:
			blocks = append(blocks, "agent: "+text)
		}
	}
	return strings.Join(blocks, "\n\n")
}
