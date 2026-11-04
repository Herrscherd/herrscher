package tui

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

const (
	barSourceBuiltin          = "builtin"
	barSourceScript           = "script"
	statuslineSection         = "statusline"
	statuslineTimeout         = time.Second
	statuslineWaitDelay       = 100 * time.Millisecond
	statuslineDefaultInterval = time.Second
	statuslineMinInterval     = 100 * time.Millisecond
	statuslineGateway         = "terminal"
)

var (
	errStatuslineNoCommand = errors.New("no statusline command set")
	errStatuslineEmpty     = errors.New("statusline script printed no line")
)

type statuslineConfig struct {
	Source   string
	Command  string
	Interval time.Duration
	Fields   []string
}

func defaultStatuslineConfig() statuslineConfig {
	return statuslineConfig{Source: barSourceBuiltin, Interval: statuslineDefaultInterval}
}

func statuslineConfigFile() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".config", "herrscher", "terminal.toml")
}

func loadStatuslineConfig(path string) statuslineConfig {
	cfg := defaultStatuslineConfig()
	if path == "" {
		return cfg
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return cfg
	}
	return parseStatuslineSection(string(data), cfg)
}

func parseStatuslineSection(text string, cfg statuslineConfig) statuslineConfig {
	for _, entry := range scanTomlEntries(strings.Split(text, "\n")) {
		if entry.section == statuslineSection {
			cfg = applyStatuslineKey(cfg, entry.key, entry.value)
		}
	}
	return cfg
}

func tomlSectionName(line string) string {
	end := strings.Index(line, "]")
	if end < 0 {
		return ""
	}
	return strings.TrimSpace(strings.Trim(line[:end], "[ "))
}

func tomlValue(raw string) (string, bool) {
	if raw == "" {
		return "", false
	}
	switch raw[0] {
	case '"':
		return tomlBasicString(raw)
	case '\'':
		end := strings.IndexByte(raw[1:], '\'')
		if end < 0 {
			return "", false
		}
		return raw[1 : end+1], true
	}
	bare, _, _ := strings.Cut(raw, "#")
	return strings.TrimSpace(bare), true
}

func tomlBasicString(raw string) (string, bool) {
	end := basicStringEnd(raw)
	if end < 0 {
		return "", false
	}
	value, err := strconv.Unquote(raw[:end+1])
	return value, err == nil
}

func basicStringEnd(raw string) int {
	for i := 1; i < len(raw); i++ {
		switch raw[i] {
		case '\\':
			i++
		case '"':
			return i
		}
	}
	return -1
}

func applyStatuslineKey(cfg statuslineConfig, key, raw string) statuslineConfig {
	if key == statuslineFieldsKey {
		if fields, ok := tomlStringArray(raw); ok {
			cfg.Fields = fields
		}
		return cfg
	}
	value, ok := tomlValue(raw)
	if !ok {
		return cfg
	}
	switch key {
	case "source":
		if value == barSourceBuiltin || value == barSourceScript {
			cfg.Source = value
		}
	case "command":
		cfg.Command = value
	case "interval":
		if d, err := time.ParseDuration(value); err == nil && d > 0 {
			cfg.Interval = max(d, statuslineMinInterval)
		}
	}
	return cfg
}

type scriptTickMsg struct{}

type scriptDoneMsg struct {
	line string
	err  error
}

type statuslinePayload struct {
	Session    string         `json:"session"`
	Vendor     string         `json:"vendor"`
	Model      string         `json:"model"`
	Mode       string         `json:"mode"`
	Gateway    string         `json:"gateway"`
	Git        statuslineGit  `json:"git"`
	Context    statuslineCtx  `json:"context"`
	Cost       statuslineCost `json:"cost"`
	Queue      int            `json:"queue"`
	Background int            `json:"background"`
	Turn       statuslineTurn `json:"turn"`
}

type statuslineGit struct {
	Repo     string `json:"repo"`
	Branch   string `json:"branch"`
	Worktree string `json:"worktree"`
}

type statuslineCtx struct {
	Used    int `json:"used"`
	Percent int `json:"percent"`
}

type statuslineCost struct {
	USD float64 `json:"usd"`
}

type statuslineTurn struct {
	Busy bool `json:"busy"`
	Secs int  `json:"secs"`
}

func (m *model) scriptInput(tb *tab) []byte {
	if tb == nil {
		tb = &tab{}
	}
	info, _ := m.activeInfo()
	used := m.contextTokens(tb)
	branch, linked := m.gitHeadOf(info.Dir)
	payload := statuslinePayload{
		Session:    tb.label,
		Vendor:     info.Vendor,
		Model:      modelLabel(info),
		Mode:       modeSpec(tb.modeID()).label,
		Gateway:    statuslineGateway,
		Git:        statuslineGit{Repo: info.Project, Branch: branch},
		Context:    statuslineCtx{Used: used, Percent: int(contextRatio(used, contextLimit(info.Vendor)) * percentScale)},
		Cost:       statuslineCost{USD: tb.costTotal},
		Queue:      m.bar.queued,
		Background: m.bar.background,
		Turn:       statuslineTurn{Busy: tb.busy},
	}
	if linked {
		payload.Git.Worktree = info.Dir
	}
	if tb.busy && !tb.startedAt.IsZero() {
		payload.Turn.Secs = int(time.Since(tb.startedAt).Seconds())
	}
	data, err := json.Marshal(payload)
	if err != nil {
		return nil
	}
	return data
}

func scriptTick(interval time.Duration) tea.Cmd {
	return tea.Tick(interval, func(time.Time) tea.Msg { return scriptTickMsg{} })
}

func runStatuslineScript(command string, payload []byte) tea.Cmd {
	return func() tea.Msg {
		line, err := execStatusline(command, payload)
		return scriptDoneMsg{line: line, err: err}
	}
}

func execStatusline(command string, payload []byte) (string, error) {
	if command == "" {
		return "", errStatuslineNoCommand
	}
	ctx, cancel := context.WithTimeout(context.Background(), statuslineTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "sh", "-c", command)
	cmd.Stdin = bytes.NewReader(payload)
	cmd.WaitDelay = statuslineWaitDelay
	var out bytes.Buffer
	cmd.Stdout = &out
	if err := cmd.Run(); err != nil {
		return "", err
	}
	first, _, _ := strings.Cut(out.String(), "\n")
	first = strings.TrimSpace(first)
	if first == "" {
		return "", errStatuslineEmpty
	}
	return first, nil
}

func (m *model) armStatusline() tea.Cmd {
	if !m.bar.scripted() || m.bar.script.armed {
		return nil
	}
	m.bar.script.armed = true
	return m.handleScriptTick()
}

func (m *model) handleScriptTick() tea.Cmd {
	if !m.bar.scripted() {
		m.bar.script.armed = false
		return nil
	}
	next := scriptTick(m.bar.config.Interval)
	if m.bar.script.busy {
		return next
	}
	if m.bar.config.Command == "" {
		m.receiveScript(scriptDoneMsg{err: errStatuslineNoCommand})
		return next
	}
	m.bar.script.busy = true
	return tea.Batch(next, runStatuslineScript(m.bar.config.Command, m.scriptInput(m.activeTab())))
}

func (m *model) receiveScript(msg scriptDoneMsg) {
	m.bar.script.busy = false
	if msg.err != nil {
		m.bar.script.failed = true
		m.bar.script.line = ""
		return
	}
	m.bar.script.failed = false
	m.bar.script.line = msg.line
}
