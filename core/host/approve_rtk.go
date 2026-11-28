package host

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"slices"

	"github.com/Herrscherd/herrscher/core/health"
	"github.com/Herrscherd/herrscher/core/internal/rtk"
)

const (
	hookFlagRTK  = "--rtk"
	toolBash     = "Bash"
	inputCommand = "command"
)

// HookOptions are the switches the materialized approval hook was written with.
type HookOptions struct {
	RTK bool
}

// ParseHookOptions reads the switches the hook command carries after `approve hook`.
func ParseHookOptions(args []string) HookOptions {
	return HookOptions{RTK: slices.Contains(args, hookFlagRTK)}
}

const decisionAllow = "allow"

type commandRewriter func(ctx context.Context, command string) (string, bool)

func rtkRewriter(enabled bool) commandRewriter {
	if !enabled {
		return nil
	}
	return func(ctx context.Context, command string) (string, bool) {
		if rtk.HasUserHook(claudeSettingsPaths()...) {
			return "", false
		}
		binary, err := exec.LookPath(rtk.Binary)
		if err != nil {
			return "", false
		}
		return rtk.Rewrite(ctx, binary, command)
	}
}

func claudeSettingsPaths() []string {
	var paths []string
	if cwd, err := os.Getwd(); err == nil {
		paths = append(paths, filepath.Join(cwd, ".claude", "settings.json"), filepath.Join(cwd, ".claude", "settings.local.json"))
	}
	return append(paths, homeSettingsPaths()...)
}

func homeSettingsPaths() []string {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil
	}
	return []string{filepath.Join(home, ".claude", "settings.json")}
}

func measuredRTK() health.RTK {
	status := rtk.Probe(context.Background(), exec.LookPath, claudeSettingsPaths()...)
	return health.RTK{Present: status.Present, Version: status.Version, UserHook: status.UserHook}
}

func decisionInput(ctx context.Context, rewrite commandRewriter, payload hookInput, decision string) map[string]any {
	if decision != decisionAllow {
		return nil
	}
	return updatedInput(payload, rewriteCommand(ctx, rewrite, payload))
}

func rewriteCommand(ctx context.Context, rewrite commandRewriter, payload hookInput) string {
	if rewrite == nil || payload.ToolName != toolBash {
		return ""
	}
	command, isString := payload.ToolInput[inputCommand].(string)
	if !isString {
		return ""
	}
	rewritten, ok := rewrite(ctx, command)
	if !ok {
		return ""
	}
	return rewritten
}

func updatedInput(payload hookInput, rewritten string) map[string]any {
	if rewritten == "" {
		return nil
	}
	input := make(map[string]any, len(payload.ToolInput))
	for key, value := range payload.ToolInput {
		input[key] = value
	}
	input[inputCommand] = rewritten
	return input
}
