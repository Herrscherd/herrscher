package vendortest

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strconv"
)

const (
	grokLabel          = "grok"
	grokVersion        = "grok 0.0.0 (vendortest)"
	grokModelsListing  = "You are logged in with grok.com.\n\nDefault model: grok-4.7\n\nAvailable models:\n  * grok-4.7 (default)\n"
	defaultGrokSession = "vendortest-grok-session"
	grokPrintFlag      = "-p"
	grokStreamFormat   = "streaming-json"
	grokModelsVerb     = "models"
	grokInspectVerb    = "inspect"
	grokHooksDir       = ".grok/hooks"
	grokStatusDone     = "completed"
	grokStatusFailed   = "failed"
	grokEndTurn        = "end_turn"
	grokHookDenied     = "Hook denied: "
)

var (
	errGrokArgs  = errors.New("the grok fake speaks only streaming-json, models and --version")
	errGrokPrint = errors.New("the grok fake needs -p, which the grok backend always passes")
)

// Grok runs the fake grok CLI on args and returns its exit status.
func Grok(args []string, streams Streams, files Files) int {
	switch {
	case slices.Contains(args, flagVersion):
		return exitOf(streams.Err, writeLine(streams.Out, grokVersion))
	case len(args) > 0 && args[0] == grokModelsVerb:
		_, err := io.WriteString(streams.Out, grokModelsListing)
		return exitOf(streams.Err, err)
	case len(args) > 0 && args[0] == grokInspectVerb:
		return exitOf(streams.Err, inspectHooks(streams.Out))
	case !slices.Contains(args, grokStreamFormat):
		return exitOf(streams.Err, errGrokArgs)
	case !slices.Contains(args, grokPrintFlag):
		return exitOf(streams.Err, errGrokPrint)
	}
	fake, err := openVendor(grokLabel, files)
	if err != nil {
		return exitOf(streams.Err, err)
	}
	prompt, err := io.ReadAll(streams.In)
	if err != nil {
		return exitOf(streams.Err, err)
	}
	failed, err := fake.grokTurn(streams, flagValue(args, "--resume"), string(prompt))
	if err == nil && failed {
		return 1
	}
	return exitOf(streams.Err, err)
}

type grokLine struct {
	Type       string          `json:"type"`
	Data       string          `json:"data,omitempty"`
	Message    string          `json:"message,omitempty"`
	ToolCallID string          `json:"toolCallId,omitempty"`
	ToolName   string          `json:"toolName,omitempty"`
	Status     string          `json:"status,omitempty"`
	RawInput   json.RawMessage `json:"rawInput,omitempty"`
	Content    []grokContent   `json:"content,omitempty"`
	StopReason string          `json:"stopReason,omitempty"`
	SessionID  string          `json:"sessionId,omitempty"`
}

type grokContent struct {
	Type    string `json:"type"`
	Content struct {
		Type string `json:"type"`
		Text string `json:"text"`
	} `json:"content"`
}

type grokPlay struct {
	vendor  *vendor
	streams Streams
	calls   int
	decided *verdict
}

func (v *vendor) grokTurn(streams Streams, resume, prompt string) (bool, error) {
	if err := v.recordPrompt(prompt); err != nil {
		return false, err
	}
	turn, err := v.nextTurn()
	if err != nil {
		return false, err
	}
	play := &grokPlay{vendor: v, streams: streams}
	for _, current := range turn.steps {
		failed, err := play.step(current, prompt)
		if err != nil || failed {
			return failed, err
		}
	}
	sessionID := resume
	if sessionID == "" {
		sessionID = defaultGrokSession
	}
	return false, writeJSON(streams.Out, grokLine{Type: "end", StopReason: grokEndTurn, SessionID: sessionID})
}

func (p *grokPlay) step(current step, prompt string) (bool, error) {
	switch current.kind {
	case stepText, stepEcho:
		return false, writeJSON(p.streams.Out, grokLine{Type: "text", Data: spokenText(current, prompt)})
	case stepFail:
		return true, writeJSON(p.streams.Out, grokLine{Type: "error", Message: current.text})
	case stepTool:
		return false, p.callTool(current)
	case stepExpect:
		return p.judge(current.text)
	case stepHang, stepCrash, stepAPIError, stepUsage, stepSession:
	}
	return false, errUnsupportedStep
}

func (p *grokPlay) judge(want string) (bool, error) {
	err := expectation(p.decided, want)
	if err == nil {
		return false, nil
	}
	if logErr := p.vendor.record("failed: " + err.Error()); logErr != nil {
		return true, logErr
	}
	return true, writeJSON(p.streams.Out, grokLine{Type: "error", Message: err.Error()})
}

func (p *grokPlay) callTool(tool step) error {
	p.calls++
	callID := "call-" + strconv.Itoa(p.calls)
	if err := writeJSON(p.streams.Out, grokLine{Type: "tool_call", ToolCallID: callID, ToolName: tool.tool, Status: "pending", RawInput: tool.input}); err != nil {
		return err
	}
	outcome, err := gateGrokTool(tool, p.streams.Err)
	if err != nil {
		return err
	}
	p.decided = &outcome
	if err := p.vendor.record(outcomeLine(callID, outcome)); err != nil {
		return err
	}
	update := grokLine{Type: "tool_call_update", ToolCallID: callID, Status: grokStatusDone}
	if !outcome.allowed {
		update.Status = grokStatusFailed
		update.Content = []grokContent{grokText(grokHookDenied + outcome.reason)}
	}
	return writeJSON(p.streams.Out, update)
}

func grokText(text string) grokContent {
	var content grokContent
	content.Type = "content"
	content.Content.Type = contentBlockTypeText
	content.Content.Text = text
	return content
}

type grokHookCommand struct {
	Command    string `json:"command"`
	FailClosed bool   `json:"failClosed"`
}

type inspectedHook struct {
	Event   string `json:"event"`
	Target  string `json:"target"`
	Matcher string `json:"matcher"`
	Source  struct {
		Type string `json:"type"`
	} `json:"source"`
}

func inspectHooks(out io.Writer) error {
	commands, err := grokHookCommands()
	if err != nil {
		return err
	}
	hooks := make([]inspectedHook, 0, len(commands))
	for _, command := range commands {
		hook := inspectedHook{Event: "pre_tool_use", Target: command.Command, Matcher: "*"}
		hook.Source.Type = "project"
		hooks = append(hooks, hook)
	}
	return writeJSON(out, struct {
		Hooks []inspectedHook `json:"hooks"`
	}{Hooks: hooks})
}

type grokHookFile struct {
	Hooks struct {
		PreToolUse []struct {
			Hooks []grokHookCommand `json:"hooks"`
		} `json:"PreToolUse"`
	} `json:"hooks"`
}

func gateGrokTool(tool step, errOut io.Writer) (verdict, error) {
	commands, err := grokHookCommands()
	if err != nil {
		return verdict{}, err
	}
	payload, err := json.Marshal(hookPayload{HookEventName: preToolUseEvent, ToolName: tool.tool, ToolInput: tool.input})
	if err != nil {
		return verdict{}, fmt.Errorf("cannot encode the hook payload: %w", err)
	}
	for _, command := range commands {
		outcome, err := runHook(command.Command, payload, errOut)
		switch {
		case err != nil && command.FailClosed:
			return verdict{reason: err.Error()}, nil
		case err != nil:
			continue
		case !outcome.allowed:
			return outcome, nil
		}
	}
	return verdict{allowed: true}, nil
}

func grokHookCommands() ([]grokHookCommand, error) {
	workdir, err := os.Getwd()
	if err != nil {
		return nil, fmt.Errorf("cannot find the work directory: %w", err)
	}
	paths, err := filepath.Glob(filepath.Join(workdir, grokHooksDir, "*.json"))
	if err != nil {
		return nil, fmt.Errorf("cannot list the grok hooks: %w", err)
	}
	var commands []grokHookCommand
	for _, path := range paths {
		data, err := os.ReadFile(filepath.Clean(path))
		if err != nil {
			return nil, fmt.Errorf("cannot read the grok hook: %w", err)
		}
		var file grokHookFile
		if err := json.Unmarshal(data, &file); err != nil {
			return nil, fmt.Errorf("cannot parse the grok hook: %w", err)
		}
		for _, group := range file.Hooks.PreToolUse {
			commands = append(commands, group.Hooks...)
		}
	}
	return commands, nil
}
