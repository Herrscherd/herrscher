package vendortest

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
)

const (
	envScript = "VENDORTEST_SCRIPT"
	envLog    = "VENDORTEST_LOG"
	envTurns  = "VENDORTEST_TURNS"

	claudeVersion    = "2.1.0 (Claude Code)"
	defaultSessionID = "vendortest-session"
	echoPrefix       = "echo: "
	privateFile      = 0o600
	exitFailure      = 1
	exitUnreported   = 2
	maxLineBytes     = 1 << 24
)

// Files are the paths a fake reads its script from and writes its record to.
type Files struct {
	Script string
	Log    string
	Turns  string
}

// FilesFrom reads the paths from the VENDORTEST_SCRIPT, VENDORTEST_LOG and
// VENDORTEST_TURNS variables of getenv. An empty script path makes every turn an
// echo.
func FilesFrom(getenv func(string) string) Files {
	return Files{Script: getenv(envScript), Log: getenv(envLog), Turns: getenv(envTurns)}
}

// Environ is the environment block that points a fake at the files.
func (f Files) Environ() []string {
	return []string{envScript + "=" + f.Script, envLog + "=" + f.Log, envTurns + "=" + f.Turns}
}

// Streams are the standard streams of the faked process.
type Streams struct {
	In  io.Reader
	Out io.Writer
	Err io.Writer
}

// Claude runs the fake claude CLI on args and returns its exit status. It speaks
// stream-json when args ask for it, and answers one prompt in JSON otherwise.
func Claude(args []string, streams Streams, files Files) int {
	if slices.Contains(args, "--version") {
		return exitOf(streams.Err, writeLine(streams.Out, claudeVersion))
	}
	replay, err := openReplay(files, args)
	if err != nil {
		return exitOf(streams.Err, err)
	}
	if !streamJSON(args) {
		return exitOf(streams.Err, replay.oneShot(streams.Out, oneShotPrompt(args)))
	}
	return replay.stream(streams)
}

func exitOf(stderr io.Writer, err error) int {
	if err == nil {
		return 0
	}
	if _, werr := fmt.Fprintln(stderr, "vendortest:", err); werr != nil {
		return exitUnreported
	}
	return exitFailure
}

func streamJSON(args []string) bool {
	for i := range len(args) - 1 {
		if args[i] == "--input-format" && args[i+1] == "stream-json" {
			return true
		}
	}
	return false
}

func oneShotPrompt(args []string) string {
	for index := range len(args) - 1 {
		if args[index] == "-p" && !strings.HasPrefix(args[index+1], "-") {
			return args[index+1]
		}
	}
	return ""
}

func flagValue(args []string, name string) string {
	for index := range len(args) - 1 {
		if args[index] == name {
			return args[index+1]
		}
	}
	return ""
}

type replay struct {
	script    Script
	files     Files
	sessionID string
	toolCalls int
}

func openReplay(files Files, args []string) (*replay, error) {
	script, err := loadScript(files.Script)
	if err != nil {
		return nil, err
	}
	sessionID := flagValue(args, "--resume")
	if sessionID == "" {
		sessionID = defaultSessionID
	}
	player := &replay{script: script, files: files, sessionID: sessionID}
	if err := player.record("start " + strings.Join(args, " ")); err != nil {
		return nil, err
	}
	return player, nil
}

func loadScript(path string) (Script, error) {
	if path == "" {
		return Script{}, nil
	}
	data, err := os.ReadFile(filepath.Clean(path))
	if errors.Is(err, fs.ErrNotExist) {
		return Script{}, nil
	}
	if err != nil {
		return Script{}, fmt.Errorf("cannot read the script: %w", err)
	}
	return ParseScript(bytes.NewReader(data))
}

func (r *replay) record(line string) error {
	if r.files.Log == "" {
		return nil
	}
	file, err := os.OpenFile(r.files.Log, os.O_APPEND|os.O_CREATE|os.O_WRONLY, privateFile)
	if err != nil {
		return fmt.Errorf("cannot open the log: %w", err)
	}
	_, werr := fmt.Fprintln(file, line)
	return errors.Join(werr, file.Close())
}

func (r *replay) nextTurn() (Turn, error) {
	if r.files.Turns == "" {
		return r.script.Turn(0), nil
	}
	done := 0
	data, err := os.ReadFile(r.files.Turns)
	switch {
	case errors.Is(err, fs.ErrNotExist):
	case err != nil:
		return Turn{}, fmt.Errorf("cannot read the turn count: %w", err)
	default:
		if done, err = strconv.Atoi(strings.TrimSpace(string(data))); err != nil {
			return Turn{}, fmt.Errorf("cannot parse the turn count: %w", err)
		}
	}
	if err := os.WriteFile(r.files.Turns, []byte(strconv.Itoa(done+1)), privateFile); err != nil {
		return Turn{}, fmt.Errorf("cannot write the turn count: %w", err)
	}
	return r.script.Turn(done), nil
}

type turnOutcome uint8

const (
	outcomeAnswered turnOutcome = iota + 1
	outcomeHung
	outcomeCrashed
)

func (r *replay) stream(streams Streams) int {
	lines := bufio.NewScanner(streams.In)
	lines.Buffer(nil, maxLineBytes)
	hung := false
	for lines.Scan() {
		if hung {
			continue
		}
		outcome, err := r.answer(streams.Out, lines.Bytes())
		if err != nil {
			return exitOf(streams.Err, err)
		}
		switch outcome {
		case outcomeCrashed:
			return exitOf(streams.Err, errCrashed)
		case outcomeHung:
			hung = true
		case outcomeAnswered:
		}
	}
	return exitOf(streams.Err, lines.Err())
}

var errCrashed = errors.New("crashed as scripted")

type userLine struct {
	Message struct {
		Content string `json:"content"`
	} `json:"message"`
}

func (r *replay) answer(out io.Writer, line []byte) (turnOutcome, error) {
	var user userLine
	if err := json.Unmarshal(line, &user); err != nil {
		return 0, fmt.Errorf("cannot decode a user line: %w", err)
	}
	prompt := user.Message.Content
	if err := r.record("prompt " + strconv.Quote(prompt)); err != nil {
		return 0, err
	}
	turn, err := r.nextTurn()
	if err != nil {
		return 0, err
	}
	return r.play(out, turn, prompt)
}

func (r *replay) play(out io.Writer, turn Turn, prompt string) (turnOutcome, error) {
	var reply []string
	for _, current := range turn.steps {
		switch current.kind {
		case stepHang:
			return outcomeHung, nil
		case stepCrash:
			return outcomeCrashed, nil
		case stepFail:
			return outcomeAnswered, writeJSON(out, r.result(current.text, prompt, true))
		case stepEcho:
			text := echoPrefix + lastLine(prompt)
			reply = append(reply, text)
			if err := writeJSON(out, assistantText(text)); err != nil {
				return 0, err
			}
		case stepText:
			reply = append(reply, current.text)
			if err := writeJSON(out, assistantText(current.text)); err != nil {
				return 0, err
			}
		case stepTool:
			r.toolCalls++
			if err := writeJSON(out, assistantTool(r.toolCalls, current)); err != nil {
				return 0, err
			}
		}
	}
	return outcomeAnswered, writeJSON(out, r.result(strings.Join(reply, "\n\n"), prompt, false))
}

func (r *replay) oneShot(out io.Writer, prompt string) error {
	if err := r.record("prompt " + strconv.Quote(prompt)); err != nil {
		return err
	}
	return writeJSON(out, r.result(echoPrefix+lastLine(prompt), prompt, false))
}

func lastLine(text string) string {
	lines := strings.Split(strings.TrimSpace(text), "\n")
	return strings.TrimSpace(lines[len(lines)-1])
}

type contentBlock struct {
	Type  string          `json:"type"`
	Text  string          `json:"text,omitempty"`
	ID    string          `json:"id,omitempty"`
	Name  string          `json:"name,omitempty"`
	Input json.RawMessage `json:"input,omitempty"`
}

type assistantEvent struct {
	Type    string `json:"type"`
	Message struct {
		Content []contentBlock `json:"content"`
	} `json:"message"`
}

func assistantText(text string) assistantEvent {
	event := assistantEvent{Type: "assistant"}
	event.Message.Content = []contentBlock{{Type: "text", Text: text}}
	return event
}

func assistantTool(call int, tool step) assistantEvent {
	event := assistantEvent{Type: "assistant"}
	event.Message.Content = []contentBlock{{Type: "tool_use", ID: "toolu_" + strconv.Itoa(call), Name: tool.tool, Input: tool.input}}
	return event
}

type tokenUsage struct {
	InputTokens  int `json:"input_tokens"`
	OutputTokens int `json:"output_tokens"`
}

type resultEvent struct {
	Type      string     `json:"type"`
	Subtype   string     `json:"subtype"`
	IsError   bool       `json:"is_error"`
	SessionID string     `json:"session_id"`
	Result    string     `json:"result"`
	CostUSD   float64    `json:"total_cost_usd"`
	Usage     tokenUsage `json:"usage"`
}

const charsPerToken = 4

func (r *replay) result(text, prompt string, failed bool) resultEvent {
	subtype := "success"
	if failed {
		subtype = "error_during_execution"
	}
	return resultEvent{
		Type:      "result",
		Subtype:   subtype,
		IsError:   failed,
		SessionID: r.sessionID,
		Result:    text,
		Usage:     tokenUsage{InputTokens: len(prompt)/charsPerToken + 1, OutputTokens: len(text)/charsPerToken + 1},
	}
}

func writeJSON(out io.Writer, event any) error {
	encoded, err := json.Marshal(event)
	if err != nil {
		return fmt.Errorf("cannot encode an event: %w", err)
	}
	return writeLine(out, string(encoded))
}

func writeLine(out io.Writer, line string) error {
	_, err := fmt.Fprintln(out, line)
	return err
}
