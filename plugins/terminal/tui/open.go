package tui

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"os/exec"
	"runtime"
	"strings"
)

var (
	errRefusedTarget    = errors.New("refused target scheme")
	errOptionTarget     = errors.New("target reads as an option")
	errControlByte      = errors.New("target contains a control byte")
	errNoEditor         = errors.New("no $EDITOR or $VISUAL to open with")
	errNoEditorLauncher = errors.New("no editor launcher configured")
	errNoSystemLauncher = errors.New("no system launcher configured")
)

type launcher func(ctx context.Context, target string) error

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
	return func(ctx context.Context, target string) error {
		if err := checkTarget(target); err != nil {
			return err
		}
		if err := checkURLTarget(target); err != nil {
			return err
		}
		return start(exec.CommandContext(ctx, name, append(args, target)...), name)
	}
}

func checkURLTarget(target string) error {
	parsed, err := url.Parse(target)
	if err != nil {
		return fmt.Errorf("refusing an unparsable target: %w", err)
	}
	switch strings.ToLower(parsed.Scheme) {
	case "http", "https", "mailto":
		return nil
	default:
		return fmt.Errorf("%w : %q (%s)", errRefusedTarget, parsed.Scheme, target)
	}
}

func checkTarget(target string) error {
	if strings.HasPrefix(target, "-") {
		return fmt.Errorf("%w : %s", errOptionTarget, target)
	}
	if hasControl(target) {
		return errControlByte
	}
	return nil
}

func start(cmd *exec.Cmd, name string) error {
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("%s: %w", name, err)
	}
	go reap(cmd)
	return nil
}

func editorLauncher(env func(string) string) launcher {
	return func(ctx context.Context, target string) error {
		if err := checkTarget(target); err != nil {
			return err
		}
		fields := configuredEditor(env)
		if len(fields) == 0 {
			return fmt.Errorf("%w %s", errNoEditor, target)
		}
		file, line := splitFileRef(target)
		args := fields[1:]
		if line != "" {
			args = append(args, "+"+line)
		}
		args = append(args, file)
		return start(exec.CommandContext(ctx, fields[0], args...), fields[0])
	}
}

func splitFileRef(target string) (file, line string) {
	index := strings.LastIndex(target, ":")
	if index < 0 {
		return target, ""
	}
	rest := target[index+1:]
	if rest == "" || strings.TrimLeft(rest, "0123456789") != "" {
		return target, ""
	}
	return target[:index], rest
}

func openLink(ctx context.Context, link Link, sys, edit launcher) error {
	if link.Kind == LinkFile {
		if edit == nil {
			return errNoEditorLauncher
		}
		return edit(ctx, link.Target)
	}
	if sys == nil {
		return errNoSystemLauncher
	}
	return sys(ctx, link.Target)
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
	launch, ok := m.selectedLink()
	if !ok {
		m.flash = joinNonEmpty(m.tr.t("tui.open.none", nil), m.labelledHint(actNextLink, m.tr.t("tui.open.pick_one", nil), chatKeyStack()...))
		return
	}
	if err := openLink(m.runContext(), launch, m.sys, m.edit); err != nil {
		m.flash = m.tr.t("tui.open.failed", map[string]any{tplArgReason: localizedReason(m.tr, err)})
		return
	}
	m.flash = m.tr.t("tui.open.opening", map[string]any{"Target": launch.Target})
}

func (m *model) linkStatus() string {
	launch, ok := m.selectedLink()
	if !ok {
		return ""
	}
	verb := m.tr.t("tui.open.verb_open", nil)
	if launch.Kind == LinkFile {
		verb = m.tr.t("tui.open.verb_edit", nil)
	}
	return m.st.dim.Render(verb + " " + launch.Target)
}
