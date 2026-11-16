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
)

var errCursorArgs = errors.New("the cursor fake speaks only stream-json, status and --version")

// Cursor runs the fake cursor-agent CLI on args. It answers the status probe and
// replays one turn per invocation in the stream-json format the cursor backend
// reads: the prompt arrives raw on stdin and the reply leaves as JSON lines.
func Cursor(args []string, streams Streams, files Files) int {
	if slices.Contains(args, flagVersion) {
		return exitOf(streams.Err, writeLine(streams.Out, cursorVersion))
	}
	if len(args) > 0 && args[0] == wordStatus {
		return exitOf(streams.Err, writeLine(streams.Out, cursorStatusLine))
	}
	if !slices.Contains(args, "stream-json") {
		return exitOf(streams.Err, errCursorArgs)
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
	if reply.failed {
		return writeJSON(out, cursorResult{Type: wordResult, Subtype: wordError, SessionID: sessionID, IsError: true, Result: reply.failure})
	}
	var message cursorAssistant
	message.Type = wordAssistant
	message.Message.Content = []cursorBlock{{Type: wordText, Text: reply.text}}
	if err := writeJSON(out, message); err != nil {
		return err
	}
	return writeJSON(out, cursorResult{Type: wordResult, Subtype: "success", SessionID: sessionID, Result: reply.text})
}
