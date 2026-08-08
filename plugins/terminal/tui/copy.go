package tui

import (
	"fmt"
	"strings"
)

// Copying out of this window is not the same problem as copying out of a normal
// terminal. The transcript lives on the alternate screen, which has no
// scrollback of its own, and the mouse is captured so the wheel can scroll it —
// between them, the terminal's own click-drag selection is unavailable exactly
// where the interesting text is. Two answers, because they fail in different
// places: copyTarget puts a whole message on the clipboard without any
// selection at all, and freeMouse hands the mouse back so the terminal can
// select the way it always could.

// copyTarget is what /copy operates on.
const (
	copyReply = "reply" // the last thing the agent said
	copyTurn  = "turn"  // the last exchange: your message and everything since
	copyAll   = "all"   // the whole transcript of this tab
)

// copyTargets is the accepted vocabulary, in the order the palette shows it.
var copyTargets = []string{copyReply, copyTurn, copyAll}

// copyCmd answers `/copy [reply|turn|all]`. An unknown word is refused by name
// rather than silently treated as the default: copying the wrong thing is
// discovered at the paste, somewhere else, too late to notice the typo.
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
		m.flash = "copy what? " + strings.Join(copyTargets, ", ")
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

// copyTarget puts the requested part of the active tab on the clipboard.
func (m *model) copyTarget(target string) {
	tb := m.tabs[m.active]
	if tb == nil || m.clip == nil {
		return
	}
	text := copyText(tb.entries, target)
	if strings.TrimSpace(text) == "" {
		m.flash = "nothing to copy here"
		return
	}
	if err := m.clip.WriteText(text); err != nil {
		m.flash = "copy failed: " + err.Error()
		return
	}
	m.flash = fmt.Sprintf("copied the %s — %d lines", target, len(strings.Split(text, "\n")))
}

// copyText renders the requested slice of a transcript as plain text.
//
// Only what was said is copied — your messages and the agent's answers. Tool
// lines, reasoning, notices and cost lines are this window talking about the
// conversation, and a paste of them into an issue or a chat is noise the reader
// has to step over.
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

// lastTurnStart is the index of the message that opened the last exchange —
// what you sent — so a copied turn carries the question with the answer. With
// nothing of yours in the transcript (a session replayed from an agent-only
// scrollback), the whole thing is the turn.
func lastTurnStart(entries []entry) int {
	for i := len(entries) - 1; i >= 0; i-- {
		if entries[i].role == roleYou {
			return i
		}
	}
	return 0
}

// renderCopy labels each block with who said it. The labels are the plainest
// thing that survives a paste anywhere: no glyphs, no rules, no colour — those
// are for the screen, and the clipboard is going somewhere else.
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
