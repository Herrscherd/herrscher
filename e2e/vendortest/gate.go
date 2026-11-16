package vendortest

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
)

const (
	preToolUseEvent = "PreToolUse"
	decisionAllow   = "allow"
	decisionDeny    = "deny"
	hookCommandEnv  = "VENDORTEST_HOOK"
	hookShellScript = `eval "$VENDORTEST_HOOK"`
)

func settingsPaths() []string {
	return []string{".claude/settings.json", ".claude/settings.local.json"}
}

var (
	errHookFailed    = errors.New("the hook failed")
	errHookAnswer    = errors.New("the hook answered something that is not JSON")
	errNoDecision    = errors.New("the hook answered without a permission decision")
	errExpectedAllow = errors.New("expected the tool call to be allowed")
	errExpectedDeny  = errors.New("expected the tool call to be denied, but the hook allowed it")
	errExpectNoTool  = errors.New("expect has no tool call before it to judge")
)

type verdict struct {
	allowed bool
	reason  string
}

type hookCommand struct {
	Command string `json:"command"`
}

type hookGroup struct {
	Hooks []hookCommand `json:"hooks"`
}

type settingsDoc struct {
	Hooks struct {
		PreToolUse []hookGroup `json:"PreToolUse"`
	} `json:"hooks"`
}

type hookPayload struct {
	HookEventName string          `json:"hook_event_name"`
	ToolName      string          `json:"tool_name"`
	ToolInput     json.RawMessage `json:"tool_input"`
}

type hookReply struct {
	HookSpecificOutput struct {
		PermissionDecision       string `json:"permissionDecision"`
		PermissionDecisionReason string `json:"permissionDecisionReason"`
	} `json:"hookSpecificOutput"`
}

func gateTool(tool step, errOut io.Writer) (verdict, error) {
	commands, err := preToolUseCommands()
	if err != nil {
		return verdict{}, err
	}
	payload, err := json.Marshal(hookPayload{HookEventName: preToolUseEvent, ToolName: tool.tool, ToolInput: tool.input})
	if err != nil {
		return verdict{}, fmt.Errorf("cannot encode the hook payload: %w", err)
	}
	for _, command := range commands {
		outcome, err := runHook(command, payload, errOut)
		if err != nil || !outcome.allowed {
			return outcome, err
		}
	}
	return verdict{allowed: true}, nil
}

func preToolUseCommands() ([]string, error) {
	workdir, err := os.Getwd()
	if err != nil {
		return nil, fmt.Errorf("cannot find the work directory: %w", err)
	}
	var commands []string
	for _, name := range settingsPaths() {
		found, err := hooksIn(filepath.Clean(filepath.Join(workdir, name)))
		if err != nil {
			return nil, err
		}
		commands = append(commands, found...)
	}
	return commands, nil
}

func hooksIn(path string) ([]string, error) {
	data, err := os.ReadFile(filepath.Clean(path))
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("cannot read the settings: %w", err)
	}
	var doc settingsDoc
	if err := json.Unmarshal(data, &doc); err != nil {
		return nil, fmt.Errorf("cannot parse the settings: %w", err)
	}
	var commands []string
	for _, group := range doc.Hooks.PreToolUse {
		for _, hook := range group.Hooks {
			commands = append(commands, hook.Command)
		}
	}
	return commands, nil
}

func runHook(command string, payload []byte, errOut io.Writer) (verdict, error) {
	hook := exec.CommandContext(context.Background(), "sh", "-c", hookShellScript)
	hook.Env = append(os.Environ(), hookCommandEnv+"="+command)
	hook.Stdin = bytes.NewReader(payload)
	hook.Stderr = errOut
	answer, err := hook.Output()
	if err != nil {
		return verdict{}, fmt.Errorf("%w %q: %w", errHookFailed, command, err)
	}
	return decide(answer)
}

func decide(answer []byte) (verdict, error) {
	var reply hookReply
	if err := json.Unmarshal(answer, &reply); err != nil {
		return verdict{}, fmt.Errorf("%w %s: %w", errHookAnswer, answer, err)
	}
	specific := reply.HookSpecificOutput
	switch specific.PermissionDecision {
	case decisionAllow:
		return verdict{allowed: true}, nil
	case decisionDeny:
		return verdict{reason: specific.PermissionDecisionReason}, nil
	}
	return verdict{}, fmt.Errorf("%w: %s", errNoDecision, answer)
}

func expectation(decided *verdict, want string) error {
	switch {
	case decided == nil:
		return errExpectNoTool
	case want == wordAllowed && !decided.allowed:
		return fmt.Errorf("%w, the hook denied it: %s", errExpectedAllow, decided.reason)
	case want == wordDenied && decided.allowed:
		return errExpectedDeny
	}
	return nil
}
