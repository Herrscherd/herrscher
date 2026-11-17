package vendortest

import (
	"errors"
	"io"
	"slices"
)

const (
	cursorLabel          = "cursor"
	cursorVersion        = "cursor-agent 0.0.0 (vendortest)"
	cursorStatusLine     = `{"status":"authenticated","userInfo":{"email":"vendortest@example.invalid"}}`
	defaultCursorSession = "vendortest-cursor-session"
	cursorPrintFlag      = "-p"
	cursorStreamFormat   = "stream-json"
)

var (
	errCursorArgs  = errors.New("the cursor fake speaks only stream-json, status and --version")
	errCursorPrint = errors.New("the cursor fake needs -p, which the cursor backend always passes")
)

// Cursor runs the fake cursor-agent CLI on args and returns its exit status.
func Cursor(args []string, streams Streams, files Files) int {
	if slices.Contains(args, flagVersion) {
		return exitOf(streams.Err, writeLine(streams.Out, cursorVersion))
	}
	if isStatusProbe(args) {
		return exitOf(streams.Err, writeLine(streams.Out, cursorStatusLine))
	}
	if !slices.Contains(args, cursorStreamFormat) {
		return exitOf(streams.Err, errCursorArgs)
	}
	if !slices.Contains(args, cursorPrintFlag) {
		return exitOf(streams.Err, errCursorPrint)
	}
	fake, err := openVendor(cursorLabel, files)
	if err != nil {
		return exitOf(streams.Err, err)
	}
	prompt, err := io.ReadAll(streams.In)
	if err != nil {
		return exitOf(streams.Err, err)
	}
	return exitOf(streams.Err, fake.cursorTurn(streams.Out, flagValue(args, "--resume"), string(prompt)))
}

type cursorBlock struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

type cursorAssistant struct {
	Type    string `json:"type"`
	Message struct {
		Content []cursorBlock `json:"content"`
	} `json:"message"`
}

type cursorResult struct {
	Type      string `json:"type"`
	Subtype   string `json:"subtype"`
	SessionID string `json:"session_id"`
	IsError   bool   `json:"is_error"`
	Result    string `json:"result"`
}

func (v *vendor) cursorTurn(out io.Writer, resume, prompt string) error {
	if err := v.recordPrompt(prompt); err != nil {
		return err
	}
	turn, err := v.nextTurn()
	if err != nil {
		return err
	}
	reply, err := v.scripted(turn, prompt)
	if err != nil {
		return err
	}
	sessionID := resume
	if sessionID == "" {
		sessionID = defaultCursorSession
	}
	var message cursorAssistant
	message.Type = wordAssistant
	message.Message.Content = []cursorBlock{{Type: contentBlockTypeText, Text: reply.text}}
	if err := writeJSON(out, message); err != nil {
		return err
	}
	if reply.failed {
		return writeJSON(out, cursorResult{Type: wordResult, Subtype: resultSubtypeError, SessionID: sessionID, IsError: true, Result: reply.failure})
	}
	return writeJSON(out, cursorResult{Type: wordResult, Subtype: valueSuccess, SessionID: sessionID, Result: reply.text})
}

func isStatusProbe(args []string) bool {
	return len(args) > 0 && args[0] == wordStatus
}
