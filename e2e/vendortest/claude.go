package vendortest

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"time"
)

// DescendantSleep is how long a descendant started by spawn lives at most.
const DescendantSleep = 60 * time.Second

const descendantSleepArg = "60"

const (
	envScript = "VENDORTEST_SCRIPT"
	envLog    = "VENDORTEST_LOG"
	envTurns  = "VENDORTEST_TURNS"

	envConfigDir = "CLAUDE_CONFIG_DIR"

	claudeVersion    = "2.1.0 (Claude Code)"
	defaultSessionID = "vendortest-session"
	echoPrefix       = "echo: "
	toolDoneText     = "done"
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

// FilesFrom reads the fake CLI file paths from the VENDORTEST variables of getenv.
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

// Claude runs the fake claude CLI on args and returns its exit status.
func Claude(args []string, streams Streams, files Files) int {
	if slices.Contains(args, flagVersion) {
		return exitOf(streams.Err, writeLine(streams.Out, claudeVersion))
	}
	replay, err := openReplay(files, args)
	if err != nil {
		return exitOf(streams.Err, err)
	}
	if !streamJSON(args) {
		return exitOf(streams.Err, replay.oneShot(streams.Out, args))
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
	script      Script
	files       Files
	sessionID   string
	toolCalls   int
	descendants []*exec.Cmd
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
	if dir := os.Getenv(envConfigDir); dir != "" {
		if err := player.record("account " + dir); err != nil {
			return nil, err
		}
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
	return appendLine(r.files.Log, line)
}

func (r *replay) nextTurn() (Turn, error) {
	return nextTurnOf(r.script, r.files)
}
