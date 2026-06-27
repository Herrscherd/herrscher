// Package tui renders a gateway's live event stream and captures the operator's
// input, driving it through the narrow Backend interface. It is the terminal
// gateway's frontend: the daemon hub treats that gateway like any other; the TUI
// is what makes it a human-usable pane. Depending on Backend (not the concrete
// gateway) keeps this package importable by the gateway without a cycle.
package tui

import (
	"context"
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	contracts "github.com/Herrscherd/herrscher-contracts"
)

// RoutedEvent is a turn event tagged with the conversation (session channel) it
// belongs to, so the TUI can route it to the right tab.
type RoutedEvent struct {
	Conv  contracts.Conversation
	Event contracts.Event
}

// Backend is the narrow view of the terminal gateway the TUI drives: it reads
// routed outbound events to render, submits the lines the operator types into a
// specific channel, and enumerates the hub's sessions for tab labels. Taking an
// interface keeps this package free of any dependency on the terminal plugin.
type Backend interface {
	Frontend() <-chan RoutedEvent
	Submit(channel, text string)
	Sessions() []contracts.SessionInfo
}

var (
	humanStyle  = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("12"))
	statusStyle = lipgloss.NewStyle().Faint(true)
	replyStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("10"))
	costStyle   = lipgloss.NewStyle().Faint(true)
)

// tab is one session's pane: its transcript and unread flag.
type tab struct {
	channel string
	label   string
	lines   []string
	unread  bool
}

type eventMsg RoutedEvent

type model struct {
	tm     Backend
	vp     viewport.Model
	input  textinput.Model
	tabs   map[string]*tab
	order  []string
	active string
	ready  bool
}

func newModel(tm Backend) *model {
	in := textinput.New()
	in.Placeholder = "type a message…"
	in.Focus()
	return &model{tm: tm, input: in, tabs: map[string]*tab{}}
}

// ensureTab creates a tab for channel if missing, making the first tab active.
func (m *model) ensureTab(channel string) *tab {
	if tb, ok := m.tabs[channel]; ok {
		return tb
	}
	tb := &tab{channel: channel, label: channel}
	m.tabs[channel] = tb
	m.order = append(m.order, channel)
	if m.active == "" {
		m.active = channel
	}
	return tb
}

// route delivers a routed event to its tab, marking inactive tabs unread.
func (m *model) route(re RoutedEvent) {
	tb := m.ensureTab(re.Conv.ID)
	before := len(tb.lines)
	m.renderInto(tb, re.Event)
	if len(tb.lines) != before && tb.channel != m.active {
		tb.unread = true
	}
	if tb.channel == m.active {
		m.syncViewport()
	}
}

func (m *model) switchTab(delta int) {
	if len(m.order) == 0 {
		return
	}
	idx := 0
	for i, ch := range m.order {
		if ch == m.active {
			idx = i
			break
		}
	}
	idx = (idx + delta + len(m.order)) % len(m.order)
	m.active = m.order[idx]
	m.tabs[m.active].unread = false
	m.syncViewport()
}

func (m *model) renderInto(tb *tab, e contracts.Event) {
	switch e.T {
	case "chunk":
		tb.lines = append(tb.lines, e.Text)
	case "status":
		tb.lines = append(tb.lines, statusStyle.Render("· "+e.Text))
	case "reply":
		if e.Text != "" {
			tb.lines = append(tb.lines, replyStyle.Render(e.Text))
		}
		if e.Cost > 0 {
			tb.lines = append(tb.lines, costStyle.Render(formatCost(e.Cost)))
		}
	case "reset":
		tb.lines = append(tb.lines, statusStyle.Render("· (turn reset)"))
	case "abandoned":
		tb.lines = append(tb.lines, statusStyle.Render("· (turn abandoned)"))
	}
}

func (m *model) syncViewport() {
	if !m.ready {
		return
	}
	tb := m.tabs[m.active]
	if tb == nil {
		m.vp.SetContent("")
		return
	}
	m.vp.SetContent(strings.Join(tb.lines, "\n"))
	m.vp.GotoBottom()
}

// tabBar renders the tab strip: active tab highlighted, unread marked with •.
func (m *model) tabBar() string {
	var b strings.Builder
	for _, ch := range m.order {
		tb := m.tabs[ch]
		name := tb.label
		if tb.unread {
			name = "•" + name
		}
		if ch == m.active {
			b.WriteString(humanStyle.Render("[" + name + "] "))
		} else {
			b.WriteString(statusStyle.Render(" " + name + " "))
		}
	}
	return b.String()
}

// formatCost renders a turn's USD cost, matching the host progress summary:
// sub-cent costs get four decimals, larger ones two.
func formatCost(c float64) string {
	if c < 0.01 {
		return fmt.Sprintf("$%.4f", c)
	}
	return fmt.Sprintf("$%.2f", c)
}

// Run starts the TUI bound to the given gateway backend, blocking until the user
// quits; quitting cancels ctx (wired by the caller) so the daemon shuts down
// cleanly.
func Run(ctx context.Context, cancel context.CancelFunc, tm Backend) error {
	m := newModel(tm)
	p := tea.NewProgram(m, tea.WithAltScreen())
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case e, ok := <-tm.Frontend():
				if !ok {
					return
				}
				p.Send(eventMsg(e))
			}
		}
	}()
	_, err := p.Run()
	cancel()
	return err
}

func (m *model) Init() tea.Cmd { return textinput.Blink }

func (m *model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		if !m.ready {
			m.vp = viewport.New(msg.Width, msg.Height-4)
			m.ready = true
			m.syncViewport()
		} else {
			m.vp.Width = msg.Width
			m.vp.Height = msg.Height - 4
		}
		m.input.Width = msg.Width - 2
	case tea.KeyMsg:
		switch msg.Type {
		case tea.KeyCtrlC, tea.KeyEsc:
			return m, tea.Quit
		case tea.KeyTab:
			m.switchTab(1)
			return m, nil
		case tea.KeyShiftTab:
			m.switchTab(-1)
			return m, nil
		case tea.KeyEnter:
			text := strings.TrimSpace(m.input.Value())
			if text != "" && m.active != "" {
				m.tm.Submit(m.active, text)
				tb := m.tabs[m.active]
				tb.lines = append(tb.lines, humanStyle.Render("you ")+text)
				m.syncViewport()
				m.input.Reset()
			}
		}
	case eventMsg:
		m.route(RoutedEvent(msg))
	}
	var cmds []tea.Cmd
	var c tea.Cmd
	m.input, c = m.input.Update(msg)
	cmds = append(cmds, c)
	m.vp, c = m.vp.Update(msg)
	cmds = append(cmds, c)
	return m, tea.Batch(cmds...)
}

func (m *model) View() string {
	if !m.ready {
		return "starting…"
	}
	return fmt.Sprintf("%s\n%s\n%s", m.tabBar(), m.vp.View(), m.input.View())
}
