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
)

type step struct {
	kind  stepKind
	text  string
	tool  string
	input json.RawMessage
}

// Turn is the scripted reply to one prompt.
type Turn struct {
	steps []step
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

// ParseScript reads a script. Each line is one step: "text <words>", "tool
// <Name> <json input>", "error <message>", "echo", "hang" or "crash". Blank
// lines are skipped.
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
			script.turns = append(script.turns, current)
			current = Turn{}
			continue
		}
		parsed, err := parseStep(line)
		if err != nil {
			return Script{}, &LineError{Line: lineNo, Text: line, Err: err}
		}
		current.steps = append(current.steps, parsed)
	}
	if err := scanner.Err(); err != nil {
		return Script{}, fmt.Errorf("cannot read the script: %w", err)
	}
	if len(current.steps) > 0 {
		script.turns = append(script.turns, current)
	}
	return script, nil
}

var (
	errUnknownStep  = errors.New("unknown step, want text, tool, error, echo, hang or crash")
	errToolNeedsArg = errors.New("tool wants a name and a JSON input")
	errToolInput    = errors.New("tool input is not valid JSON")
	errNeedsText    = errors.New("step wants text after its name")
)

func parseStep(line string) (step, error) {
	name, rest := cutWord(line)
	switch name {
	case "text":
		return textStep(stepText, rest)
	case "error":
		return textStep(stepFail, rest)
	case "tool":
		return toolStep(rest)
	case "echo":
		return step{kind: stepEcho}, nil
	case "hang":
		return step{kind: stepHang}, nil
	case "crash":
		return step{kind: stepCrash}, nil
	}
	return step{}, errUnknownStep
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

// Turn returns the reply scripted for the turn at index, counted from zero
// across every process that replayed this script. A turn past the end echoes.
func (s Script) Turn(index int) Turn {
	if index < len(s.turns) {
		return s.turns[index]
	}
	return Turn{steps: []step{{kind: stepEcho}}}
}
