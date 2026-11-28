// Package rtk condenses the output of agent Bash calls through the rtk CLI: it
// probes whether rtk is installed and whether the operator's own Claude hook
// already rewrites commands through it, and it rewrites one read-only command to
// its rtk form only when that form is the command with a single prefix added.
package rtk

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
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
	exitDenied        = 2
	exitRewritten     = 3
	versionWait       = 2 * time.Second
	rewriteWait       = 2 * time.Second
	rewriteGrace      = time.Second
	maxRewriteOutput  = 4096
	userHookMarker    = "rtk hook"
	userRewriteMarker = "rtk-rewrite"
	preToolUse        = "PreToolUse"
	prefix            = Binary + " "
)

var errOutputTooLarge = errors.New("rtk rewrite output over the cap")

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

// Rewrite returns the rtk form of a read-only command, which is the command with the rtk prefix added, and false whenever the command is not read-only, rtk would change it any other way, rtk does not answer within its bounds, or it declines.
func Rewrite(ctx context.Context, binary, command string) (string, bool) {
	if !readOnly(command) {
		return "", false
	}
	bounded, cancel := context.WithTimeout(ctx, rewriteWait)
	defer cancel()
	stdout := &cappedOutput{limit: maxRewriteOutput, stop: cancel}
	cmd := exec.CommandContext(bounded, binary, rewriteSubcommand, command)
	cmd.Stdout = stdout
	cmd.WaitDelay = rewriteGrace
	if !rewriteUsable(cmd.Run()) || stdout.overflowed {
		return "", false
	}
	rewritten := strings.TrimSuffix(stdout.buf.String(), "\n")
	if rewritten != prefix+command {
		return "", false
	}
	return rewritten, true
}

type cappedOutput struct {
	buf        bytes.Buffer
	limit      int
	stop       context.CancelFunc
	overflowed bool
}

func (c *cappedOutput) Write(chunk []byte) (int, error) {
	if c.buf.Len()+len(chunk) > c.limit {
		c.overflowed = true
		c.stop()
		return 0, errOutputTooLarge
	}
	return c.buf.Write(chunk)
}

func rewriteUsable(err error) bool {
	if err == nil {
		return true
	}
	var exit *exec.ExitError
	if !errors.As(err, &exit) {
		slog.Debug("rtk rewrite did not run, the command runs as written", "err", err)
		return false
	}
	switch code := exit.ExitCode(); code {
	case exitRewritten:
		return true
	case exitDenied:
		slog.Debug("rtk rewrite declined by a Claude deny rule, the command runs as written", "exit", code)
	default:
		slog.Debug("rtk rewrite declined, the command runs as written", "exit", code)
	}
	return false
}
