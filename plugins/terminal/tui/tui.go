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

// Backend is the narrow view of the terminal gateway the TUI drives: it reads
// outbound events to render and submits the lines the operator types. Taking an
// interface (rather than the concrete gateway) keeps this package free of any
// dependency on the terminal plugin, so the gateway can own its frontend without
// an import cycle.
type Backend interface {
	Frontend() <-chan contracts.Event
	Submit(text string)
}

var (
	humanStyle  = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("12"))
	statusStyle = lipgloss.NewStyle().Faint(true)
	replyStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("10"))
	costStyle   = lipgloss.NewStyle().Faint(true)
)

// eventMsg wraps a gateway event for the Bubbletea update loop.
type eventMsg contracts.Event

type model struct {
	tm    Backend
	vp    viewport.Model
	input textinput.Model
	lines []string
	ready bool
}

// Run starts the TUI bound to the given gateway backend, blocking until the user
// quits; quitting cancels ctx (wired by the caller) so the daemon shuts down
// cleanly.
func Run(ctx context.Context, cancel context.CancelFunc, tm Backend) error {
	in := textinput.New()
	in.Placeholder = "type a message…"
	in.Focus()
	m := model{tm: tm, input: in}
	p := tea.NewProgram(m, tea.WithAltScreen())

	// Forward gateway events into the program.
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
	cancel() // quitting the TUI tears the daemon down
	return err
}

func (m model) Init() tea.Cmd { return textinput.Blink }

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		if !m.ready {
			m.vp = viewport.New(msg.Width, msg.Height-3)
			m.ready = true
		} else {
			m.vp.Width = msg.Width
			m.vp.Height = msg.Height - 3
		}
		m.input.Width = msg.Width - 2
	case tea.KeyMsg:
		switch msg.Type {
		case tea.KeyCtrlC, tea.KeyEsc:
			return m, tea.Quit
		case tea.KeyEnter:
			text := strings.TrimSpace(m.input.Value())
			if text != "" {
				m.tm.Submit(text)
				m.append(humanStyle.Render("you ") + text)
				m.input.Reset()
			}
		}
	case eventMsg:
		m.renderEvent(contracts.Event(msg))
	}
	var cmds []tea.Cmd
	var c tea.Cmd
	m.input, c = m.input.Update(msg)
	cmds = append(cmds, c)
	m.vp, c = m.vp.Update(msg)
	cmds = append(cmds, c)
	return m, tea.Batch(cmds...)
}

func (m *model) renderEvent(e contracts.Event) {
	switch e.T {
	case "chunk":
		m.append(e.Text)
	case "status":
		m.append(statusStyle.Render("· " + e.Text))
	case "reply":
		if e.Text != "" {
			m.append(replyStyle.Render(e.Text))
		}
		if e.Cost > 0 {
			m.append(costStyle.Render(formatCost(e.Cost)))
		}
	case "reset":
		m.append(statusStyle.Render("· (turn reset)"))
	case "abandoned":
		// The turn ended without a reply (bridge disconnect or shutdown). Mark it
		// so the transcript doesn't read as still pending; the host left how to
		// present it to the gateway.
		m.append(statusStyle.Render("· (turn abandoned)"))
	}
}

// formatCost renders a turn's USD cost, matching the host progress summary:
// sub-cent costs get four decimals, larger ones two.
func formatCost(c float64) string {
	if c < 0.01 {
		return fmt.Sprintf("$%.4f", c)
	}
	return fmt.Sprintf("$%.2f", c)
}

func (m *model) append(line string) {
	m.lines = append(m.lines, line)
	if m.ready {
		m.vp.SetContent(strings.Join(m.lines, "\n"))
		m.vp.GotoBottom()
	}
}

func (m model) View() string {
	if !m.ready {
		return "starting…"
	}
	return fmt.Sprintf("%s\n%s", m.vp.View(), m.input.View())
}
