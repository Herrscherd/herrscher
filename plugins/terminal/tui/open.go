package tui

import (
	"fmt"
	"os/exec"
	"runtime"
	"strings"
)

// Following a link means this process launches a program on the operator's own
// machine, at a destination the agent chose. So the gesture is the whole feature:
// nothing here is ever reached by a message arriving or a turn completing, and the
// only caller is a keypress. Everything else in this file merely decides which
// program gets the target once that keypress has happened.

// launcher hands a target to whatever opens it. It is a function rather than an
// exec.Cmd so the decision can be tested without launching anything — a test that
// could open a browser is a test nobody runs twice.
type launcher func(target string) error

// systemLauncher returns the desktop's own handler for a URL. Delegating is the
// point: the operator's default browser is their choice, not this program's.
func systemLauncher() launcher {
	var name string
	var args []string
	switch runtime.GOOS {
	case "darwin":
		name = "open"
	case "windows":
		name, args = "rundll32", []string{"url.dll,FileProtocolHandler"}
	default:
		name = "xdg-open"
	}
	return func(target string) error {
		if err := exec.Command(name, append(args, target)...).Start(); err != nil {
			return fmt.Errorf("%s: %w", name, err)
		}
		return nil
	}
}

// editorLauncher returns the launcher for a file reference: $EDITOR at the line,
// in the `+42 file` form every editor since vi has accepted. A file handed to the
// desktop handler would open in whatever it thinks a .go file is, which is not
// where the operator was looking.
func editorLauncher(env func(string) string) launcher {
	return func(target string) error {
		editor := strings.TrimSpace(env("EDITOR"))
		if editor == "" {
			return fmt.Errorf("no $EDITOR set to open %s", target)
		}
		file, line := splitFileRef(target)
		fields := strings.Fields(editor) // $EDITOR may carry flags of its own
		args := fields[1:]
		if line != "" {
			args = append(args, "+"+line)
		}
		args = append(args, file)
		if err := exec.Command(fields[0], args...).Start(); err != nil {
			return fmt.Errorf("%s: %w", fields[0], err)
		}
		return nil
	}
}

// splitFileRef separates a path from its trailing :line. A path with no line
// number is returned whole: it is still a file, just without a destination row.
func splitFileRef(target string) (file, line string) {
	i := strings.LastIndex(target, ":")
	if i < 0 {
		return target, ""
	}
	rest := target[i+1:]
	if rest == "" || strings.TrimLeft(rest, "0123456789") != "" {
		return target, ""
	}
	return target[:i], rest
}

// openLink routes one link to the launcher that suits it. The resolved Target is
// what is launched — never the Label, which a markdown link is free to make say
// anything at all.
func openLink(l Link, sys, edit launcher) error {
	if l.Kind == LinkFile {
		if edit == nil {
			return fmt.Errorf("no editor launcher configured")
		}
		return edit(l.Target)
	}
	if sys == nil {
		return fmt.Errorf("no system launcher configured")
	}
	return sys(l.Target)
}

// selectNextLink advances the selection through the transcript's links and then
// past the last one, back to nothing. Releasing the selection matters as much as
// making one: an operator must be able to put the gesture down.
func (m *model) selectNextLink() {
	if len(m.links) == 0 {
		m.linkIdx = -1
		return
	}
	m.linkIdx++
	if m.linkIdx >= len(m.links) {
		m.linkIdx = -1
	}
	// The highlight is applied inside the cached transcript render, and the cache
	// key cannot see a selection change.
	m.tsCache.valid = false
	m.syncViewport()
}

// selectedLink is the link an open gesture would act on, if any.
func (m *model) selectedLink() (Link, bool) {
	if m.linkIdx < 0 || m.linkIdx >= len(m.links) {
		return Link{}, false
	}
	return m.links[m.linkIdx], true
}

// openSelectedLink is the only path from a keypress to a launcher. A failure is
// reported in the status line and nothing else changes: the turn is worth more
// than the link, and a missing desktop helper is not a reason to lose it.
func (m *model) openSelectedLink() {
	l, ok := m.selectedLink()
	if !ok {
		m.flash = "no link selected — ctrl+l picks one"
		return
	}
	if err := openLink(l, m.sys, m.edit); err != nil {
		m.flash = "open failed: " + err.Error()
		return
	}
	m.flash = "opening " + l.Target
}

// linkStatus is the status line while a link is selected: the resolved target,
// never the label. A markdown label can name one destination and point at
// another, and an operator about to make a gesture is owed the real one. This is
// display and not a confirmation — the keypress remains the only consent.
func (m *model) linkStatus() string {
	l, ok := m.selectedLink()
	if !ok {
		return ""
	}
	verb := "↵ open"
	if l.Kind == LinkFile {
		verb = "↵ edit"
	}
	// Not clipped here: statusRow already holds the row to one line, and a target
	// cut short by this function would be the one thing the operator misreads.
	return dimStyle.Render(verb + " " + l.Target)
}
