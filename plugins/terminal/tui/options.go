package tui

import (
	"log/slog"
)

// Option adjusts a window before it is drawn. Options exist so a caller can say
// what the window opens on without the TUI growing a constructor per launch path.
type Option func(*model)

// Logger sends the warnings of the window to log instead of the process default logger.
func Logger(log *slog.Logger) Option {
	return func(m *model) { m.log = log }
}

func (m *model) logger() *slog.Logger {
	if m.log == nil {
		return slog.Default()
	}
	return m.log
}

// OpenOn opens the window on a named session and sends text there as its first
// message. An empty text opens the session and sends nothing, which is how a
// caller says "show me this one" without starting a turn.
func OpenOn(session, text string) Option {
	return func(m *model) { m.beginOpen(session, text) }
}
