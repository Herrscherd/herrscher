package vendortest

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"slices"
	"strings"
)

const (
	codexLabel      = "codex"
	codexVersion    = "codex-cli 0.0.0 (vendortest)"
	codexLoginLine  = "Logged in using vendortest"
	codexThreadID   = "vendortest-codex-thread"
	codexTurnID     = "vendortest-codex-turn"
	appServerVerb   = "app-server"
	methodTurnStart = "turn/start"
)

var errCodexArgs = errors.New("the codex fake speaks only app-server, login status and --version")

// Codex runs the fake codex CLI on args. It answers the login probe and serves the
// JSON-RPC app-server that the codex backend drives in stream mode, replaying one
// turn per turn/start request.
func Codex(args []string, streams Streams, files Files) int {
	if slices.Contains(args, "--version") {
		return exitOf(streams.Err, writeLine(streams.Out, codexVersion))
	}
	if slices.Equal(args, []string{"login", "status"}) {
		return exitOf(streams.Err, writeLine(streams.Out, codexLoginLine))
	}
	if !slices.Contains(args, appServerVerb) {
		return exitOf(streams.Err, errCodexArgs)
	}
	v, err := openVendor(codexLabel, files)
	if err != nil {
		return exitOf(streams.Err, err)
	}
	return exitOf(streams.Err, v.serveCodex(streams))
}

type rpcMessage struct {
	ID     json.RawMessage `json:"id,omitempty"`
	Method string          `json:"method"`
	Params json.RawMessage `json:"params,omitempty"`
}

type rpcResponse struct {
	ID     json.RawMessage `json:"id"`
	Result any             `json:"result"`
}

type rpcNotification struct {
	Method string `json:"method"`
	Params any    `json:"params"`
}

type threadParams struct {
	ThreadID string `json:"threadId"`
}

type turnStartParams struct {
	ThreadID string `json:"threadId"`
	Input    []struct {
		Text string `json:"text"`
	} `json:"input"`
}

func (p turnStartParams) prompt() string {
	texts := make([]string, 0, len(p.Input))
	for _, item := range p.Input {
		texts = append(texts, item.Text)
	}
	return strings.Join(texts, "\n")
}

func (v *vendor) serveCodex(streams Streams) error {
	lines := bufio.NewScanner(streams.In)
	lines.Buffer(nil, maxLineBytes)
	for lines.Scan() {
		if err := v.handleRPC(streams.Out, lines.Bytes()); err != nil {
			return err
		}
	}
	return lines.Err()
}

func (v *vendor) handleRPC(out io.Writer, line []byte) error {
	var message rpcMessage
	if err := json.Unmarshal(line, &message); err != nil {
		return fmt.Errorf("cannot decode a request: %w", err)
	}
	switch message.Method {
	case "thread/start", "thread/resume":
		return respondThread(out, message)
	case methodTurnStart:
		return v.startTurn(out, message)
	}
	if len(message.ID) == 0 {
		return nil
	}
	return writeJSON(out, rpcResponse{ID: message.ID, Result: map[string]any{}})
}

func respondThread(out io.Writer, message rpcMessage) error {
	var params threadParams
	if err := decodeParams(message.Params, &params); err != nil {
		return err
	}
	threadID := params.ThreadID
	if threadID == "" {
		threadID = codexThreadID
	}
	return writeJSON(out, rpcResponse{ID: message.ID, Result: map[string]any{"thread": map[string]any{"id": threadID}}})
}

func decodeParams(raw json.RawMessage, into any) error {
	if len(raw) == 0 {
		return nil
	}
	if err := json.Unmarshal(raw, into); err != nil {
		return fmt.Errorf("cannot decode the params: %w", err)
	}
	return nil
}

func (v *vendor) startTurn(out io.Writer, message rpcMessage) error {
	var params turnStartParams
	if err := decodeParams(message.Params, &params); err != nil {
		return err
	}
	prompt := params.prompt()
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
	started := map[string]any{"turn": map[string]any{"id": codexTurnID}}
	if err := writeJSON(out, rpcResponse{ID: message.ID, Result: started}); err != nil {
		return err
	}
	return writeTurnEvents(out, params.ThreadID, reply)
}

func writeTurnEvents(out io.Writer, threadID string, reply vendorReply) error {
	notes := []rpcNotification{{Method: "turn/started", Params: map[string]any{"threadId": threadID, "turn": map[string]any{"id": codexTurnID}}}}
	if reply.failed {
		notes = append(notes, rpcNotification{Method: "turn/completed", Params: map[string]any{
			"threadId": threadID,
			"turn":     map[string]any{"id": codexTurnID, "status": "failed", "error": map[string]any{"message": reply.failure}},
		}})
	} else {
		notes = append(notes,
			rpcNotification{Method: "item/agentMessage/delta", Params: map[string]any{"threadId": threadID, "turnId": codexTurnID, "delta": reply.text}},
			rpcNotification{Method: "turn/completed", Params: map[string]any{"threadId": threadID, "turn": map[string]any{"id": codexTurnID, "status": "completed"}}},
		)
	}
	for _, note := range notes {
		if err := writeJSON(out, note); err != nil {
			return err
		}
	}
	return nil
}
