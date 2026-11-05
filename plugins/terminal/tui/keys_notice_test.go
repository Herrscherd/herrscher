package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func TestUnavailableChatKeyFlashesAndKeepsTheDraft(t *testing.T) {
	cases := map[string]struct {
		key    tea.KeyMsg
		action keyAction
		moved  keyAction
	}{
		"ctrl+b sidebar":   {tea.KeyMsg{Type: tea.KeyCtrlB}, actTaskBackground, actToggleSidebar},
		"ctrl+t sessions":  {tea.KeyMsg{Type: tea.KeyCtrlT}, actToggleTodos, actOpenSessions},
		"ctrl+y copy code": {tea.KeyMsg{Type: tea.KeyCtrlY}, actChatNewerModel, actCopyCode},
		"alt+t fold turns": {tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("t"), Alt: true}, actChatThinking, actToggleTurnFold},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			m := modeSession(&fakeBackend{sessions: namedSession()})
			m.input.SetValue("brouillon")
			newKey := m.hintOf(tc.moved)
			if newKey == "" {
				t.Fatalf("%s must have a key in herrscher to point to", tc.moved)
			}
			m.handleKey(tc.key)
			if !strings.Contains(m.flash, string(tc.action)) || !strings.Contains(m.flash, "pas encore disponible dans herrscher") {
				t.Fatalf("flash must name the action and say it is not available yet, got %q", m.flash)
			}
			if !strings.Contains(m.flash, newKey) {
				t.Fatalf("flash must name the new key %q, got %q", newKey, m.flash)
			}
			if got := m.input.Value(); got != "brouillon" {
				t.Fatalf("the draft must stay intact, got %q", got)
			}
		})
	}
}

func TestAltEnterAtRestIsAdvertisedAsANewLine(t *testing.T) {
	m := modeSession(&fakeBackend{sessions: namedSession()})
	altEnter := m.hintOf(actQueueRedirect)
	if altEnter == "" {
		t.Fatal("alt+enter must have a hint")
	}
	if !strings.Contains(m.keyHelpLine(), altEnter+" nouvelle ligne au repos") {
		t.Fatalf("the help line must show alt+enter as a new line at rest, got %q", m.keyHelpLine())
	}
}
