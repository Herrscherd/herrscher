package tui

import (
	"fmt"
	"net/url"
	"os/exec"
	"runtime"
	"strings"
)

type launcher func(target string) error

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
		if err := checkTarget(target); err != nil {
			return err
		}
		if err := checkURLTarget(target); err != nil {
			return err
		}
		return start(exec.Command(name, append(args, target)...), name)
	}
}

func checkURLTarget(target string) error {
	u, err := url.Parse(target)
	if err != nil {
		return fmt.Errorf("refusing an unparsable target: %w", err)
	}
	switch strings.ToLower(u.Scheme) {
	case "http", "https", "mailto":
		return nil
	default:
		return fmt.Errorf("refusing a %q target: %s", u.Scheme, target)
	}
}

func checkTarget(target string) error {
	if strings.HasPrefix(target, "-") {
		return fmt.Errorf("refusing a target that reads as an option: %s", target)
	}
	if hasControl(target) {
		return fmt.Errorf("refusing a target carrying a control byte")
	}
	return nil
}

func start(cmd *exec.Cmd, name string) error {
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("%s: %w", name, err)
	}
	go cmd.Wait()
	return nil
}

func editorLauncher(env func(string) string) launcher {
	return func(target string) error {
		if err := checkTarget(target); err != nil {
			return err
		}
		editor := strings.TrimSpace(env("EDITOR"))
		if editor == "" {
			return fmt.Errorf("no $EDITOR set to open %s", target)
		}
		file, line := splitFileRef(target)
		fields := strings.Fields(editor)
		args := fields[1:]
		if line != "" {
			args = append(args, "+"+line)
		}
		args = append(args, file)
		return start(exec.Command(fields[0], args...), fields[0])
	}
}

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

func (m *model) selectNextLink() {
	if len(m.links) == 0 {
		m.linkIdx = -1
		return
	}
	m.linkIdx++
	if m.linkIdx >= len(m.links) {
		m.linkIdx = -1
	}
	m.tsCache.valid = false
	m.syncViewport()
}

func (m *model) selectedLink() (Link, bool) {
	if m.linkIdx < 0 || m.linkIdx >= len(m.links) {
		return Link{}, false
	}
	return m.links[m.linkIdx], true
}

func (m *model) openSelectedLink() {
	l, ok := m.selectedLink()
	if !ok {
		m.flash = "aucun lien sélectionné · ctrl+l en choisit un"
		return
	}
	if err := openLink(l, m.sys, m.edit); err != nil {
		m.flash = "ouverture impossible : " + err.Error()
		return
	}
	m.flash = "ouverture de " + l.Target
}

func (m *model) linkStatus() string {
	l, ok := m.selectedLink()
	if !ok {
		return ""
	}
	verb := "↵ open"
	if l.Kind == LinkFile {
		verb = "↵ edit"
	}
	return dimStyle.Render(verb + " " + l.Target)
}
