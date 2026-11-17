// Package vendortest stands in for the vendor CLIs a backend runs, so a scenario
// drives the real herrscher binary with no network and no vendor account. Each
// fake replays a script: one block of steps per turn, the blocks separated by a
// line that holds only "---".
package vendortest

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
	"unicode"
)

const turnSeparator = "---"

type stepKind uint8

const (
	stepText stepKind = iota + 1
	stepTool
	stepFail
	stepHang
	stepCrash
	stepEcho
	stepAPIError
	stepUsage
	stepExpect
)

const (
	wordAllowed = "allowed"
	wordDenied  = "denied"
)

type step struct {
	kind   stepKind
	text   string
	tool   string
	input  json.RawMessage
	tokens int
}

// Turn is the scripted reply to one prompt.
type Turn struct {
	steps  []step
	spawns []string
}

// Script is the parsed list of turns, replayed in order.
type Script struct {
	turns []Turn
}

// LineError reports a script line that names no step or carries a bad argument.
type LineError struct {
	Line int
	Text string
	Err  error
}

// Error renders the line number, its text and why it was refused.
func (e *LineError) Error() string {
	return fmt.Sprintf("script line %d %q: %v", e.Line, e.Text, e.Err)
}

// Unwrap returns why the line was refused.
func (e *LineError) Unwrap() error {
	return e.Err
}

// ParseScript reads a script of steps, one per line, and refuses an empty turn.
func ParseScript(source io.Reader) (Script, error) {
	var script Script
	current := Turn{}
	scanner := bufio.NewScanner(source)
	lineNo := 0
	for scanner.Scan() {
		lineNo++
		line := strings.TrimSpace(scanner.Text())
		switch line {
		case "":
			continue
		case turnSeparator:
			if current.empty() {
				return Script{}, &LineError{Line: lineNo, Text: line, Err: errEmptyTurn}
			}
			script.turns = append(script.turns, current)
			current = Turn{}
			continue
		}
		if err := current.add(line); err != nil {
			return Script{}, &LineError{Line: lineNo, Text: line, Err: err}
		}
	}
	if err := scanner.Err(); err != nil {
		return Script{}, fmt.Errorf("cannot read the script: %w", err)
	}
	if !current.empty() {
		script.turns = append(script.turns, current)
	}
	return script, nil
}

var (
	errUnknownStep  = errors.New("unknown step, want text, tool, expect, error, apierror, usage, spawn, echo, hang or crash")
	errUsageCount   = errors.New("usage wants a whole number of input tokens, as in: usage 950")
	errExpectWord   = errors.New("expect wants allowed or denied after its name")
	errToolNeedsArg = errors.New("tool wants a name and a JSON input")
	errToolInput    = errors.New("tool input is not valid JSON")
	errNeedsText    = errors.New("step wants text after its name")
	errSpawnNoFile  = errors.New("spawn is missing the pid file to write, as in: spawn descendant.pid")
	errTakesNoText  = errors.New("echo, hang and crash take nothing after their name")
	errEmptyTurn    = errors.New("a turn needs at least one step or spawn before its separator")
)

func (t *Turn) add(line string) error {
	name, rest := cutWord(line)
	if name == "spawn" {
		return t.addSpawn(rest)
	}
	parsed, err := parseStep(line)
	if err != nil {
		return err
	}
	t.steps = append(t.steps, parsed)
	return nil
}

func (t *Turn) addSpawn(pidFile string) error {
	if pidFile == "" {
		return errSpawnNoFile
	}
	t.spawns = append(t.spawns, pidFile)
	return nil
}

func (t *Turn) empty() bool {
	return len(t.steps) == 0 && len(t.spawns) == 0
}

func parseStep(line string) (step, error) {
	name, rest := cutWord(line)
	switch name {
	case scriptKeywordText:
		return textStep(stepText, rest)
	case scriptKeywordError:
		return textStep(stepFail, rest)
	case "apierror":
		return textStep(stepAPIError, rest)
	case "tool":
		return toolStep(rest)
	case "usage":
		return usageStep(rest)
	case "expect":
		return expectStep(rest)
	case "echo":
		return bareStep(stepEcho, rest)
	case "hang":
		return bareStep(stepHang, rest)
	case "crash":
		return bareStep(stepCrash, rest)
	}
	return step{}, errUnknownStep
}

func bareStep(kind stepKind, rest string) (step, error) {
	if rest != "" {
		return step{}, errTakesNoText
	}
	return step{kind: kind}, nil
}

func cutWord(line string) (string, string) {
	end := strings.IndexFunc(line, unicode.IsSpace)
	if end < 0 {
		return line, ""
	}
	return line[:end], strings.TrimSpace(line[end:])
}

func textStep(kind stepKind, text string) (step, error) {
	if text == "" {
		return step{}, errNeedsText
	}
	return step{kind: kind, text: text}, nil
}

func usageStep(rest string) (step, error) {
	tokens, err := strconv.Atoi(rest)
	if err != nil || tokens <= 0 {
		return step{}, errUsageCount
	}
	return step{kind: stepUsage, tokens: tokens}, nil
}

func expectStep(word string) (step, error) {
	if word != wordAllowed && word != wordDenied {
		return step{}, errExpectWord
	}
	return step{kind: stepExpect, text: word}, nil
}

func toolStep(rest string) (step, error) {
	tool, input := cutWord(rest)
	if input == "" || tool == "" {
		return step{}, errToolNeedsArg
	}
	if !json.Valid([]byte(input)) {
		return step{}, errToolInput
	}
	return step{kind: stepTool, tool: tool, input: json.RawMessage(input)}, nil
}

// Turn returns the reply scripted for the turn at index, counted across processes.
func (s Script) Turn(index int) Turn {
	if index < len(s.turns) {
		return s.turns[index]
	}
	return Turn{steps: []step{{kind: stepEcho}}}
}
