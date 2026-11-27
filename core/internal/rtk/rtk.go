// Package rtk condenses the output of agent Bash calls through the rtk CLI: it
// probes whether rtk is installed and whether the operator's own Claude hook
// already rewrites commands through it, and it rewrites one command to its rtk
// form only when that form is the command with a single prefix added.
package rtk

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"strings"
	"time"
)

// Binary is the name rtk is installed under.
const Binary = "rtk"

const (
	rewriteSubcommand = "rewrite"
	versionFlag       = "--version"
	exitAskDeferred   = 3
	versionWait       = 2 * time.Second
	userHookMarker    = "rtk hook"
	userRewriteMarker = "rtk-rewrite"
	preToolUse        = "PreToolUse"
	prefix            = Binary + " "
)

// LookPath resolves a binary name to a path, as exec.LookPath does.
type LookPath func(file string) (string, error)

// Status is what the probe finds: whether rtk is on the PATH, its version, and whether the operator's own hook already rewrites commands through it.
type Status struct {
	Present  bool
	Version  string
	UserHook bool
}

// Probe reports rtk's state, reading the settings files for an rtk hook that already rewrites commands.
func Probe(ctx context.Context, look LookPath, settings ...string) Status {
	status := Status{UserHook: HasUserHook(settings...)}
	path, err := look(Binary)
	if err != nil {
		return status
	}
	status.Present = true
	status.Version = version(ctx, path)
	return status
}

func version(ctx context.Context, path string) string {
	bounded, cancel := context.WithTimeout(ctx, versionWait)
	defer cancel()
	out, err := exec.CommandContext(bounded, path, versionFlag).Output()
	if err != nil {
		return ""
	}
	return ParseVersion(string(out))
}

// ParseVersion reads the version out of the line `rtk --version` prints, empty when it names none.
func ParseVersion(out string) string {
	fields := strings.Fields(out)
	if len(fields) == 2 && fields[0] == Binary {
		return fields[1]
	}
	return ""
}

type settingsFile struct {
	Hooks map[string][]struct {
		Hooks []struct {
			Command string `json:"command"`
		} `json:"hooks"`
	} `json:"hooks"`
}

// HasUserHook reports whether one of the settings files declares a PreToolUse hook that runs rtk.
func HasUserHook(paths ...string) bool {
	for _, path := range paths {
		if fileHasUserHook(path) {
			return true
		}
	}
	return false
}

func fileHasUserHook(path string) bool {
	raw, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	var doc settingsFile
	if err := json.Unmarshal(raw, &doc); err != nil {
		return false
	}
	for _, group := range doc.Hooks[preToolUse] {
		for _, hook := range group.Hooks {
			if strings.Contains(hook.Command, userHookMarker) || strings.Contains(hook.Command, userRewriteMarker) {
				return true
			}
		}
	}
	return false
}

// Rewrite returns the rtk form of command, which is the command with the rtk prefix added, and false whenever rtk would change the command any other way or cannot answer.
func Rewrite(ctx context.Context, binary, command string) (string, bool) {
	if command == "" {
		return "", false
	}
	out, err := exec.CommandContext(ctx, binary, rewriteSubcommand, command).Output()
	if !rewriteUsable(err) {
		return "", false
	}
	rewritten := strings.TrimSuffix(string(out), "\n")
	if rewritten != prefix+command {
		return "", false
	}
	return rewritten, true
}

func rewriteUsable(err error) bool {
	if err == nil {
		return true
	}
	var exit *exec.ExitError
	return errors.As(err, &exit) && exit.ExitCode() == exitAskDeferred
}
