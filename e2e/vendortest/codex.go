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
	codexLabel          = "codex"
	codexVersion        = "codex-cli 0.0.0 (vendortest)"
	codexLoginLine      = "Logged in using vendortest"
	codexThreadID       = "vendortest-codex-thread"
	codexTurnID         = "vendortest-codex-turn"
	codexItemID         = "vendortest-codex-item"
	appServerVerb       = "app-server"
	methodTurnStart     = "turn/start"
	methodItemCompleted = "item/completed"
)

var errCodexArgs = errors.New("the codex fake speaks only app-server, login status and --version")

// Codex runs the fake codex CLI on args. It answers the login probe and serves the
// JSON-RPC app-server that the codex backend drives in stream mode, replaying one
// turn per turn/start request.
func Codex(args []string, streams Streams, files Files) int {
	if slices.Contains(args, flagVersion) {
		return exitOf(streams.Err, writeLine(streams.Out, codexVersion))
	}
	if slices.Equal(args, []string{wordLogin, wordStatus}) {
		return exitOf(streams.Err, writeLine(streams.Out, codexLoginLine))
	}
	if !slices.Contains(args, appServerVerb) {
		return exitOf(streams.Err, errCodexArgs)
	}
	fake, err := openVendor(codexLabel, files)
	if err != nil {
		return exitOf(streams.Err, err)
	}
	return exitOf(streams.Err, fake.serveCodex(streams))
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
	return writeJSON(out, rpcResponse{ID: message.ID, Result: make(map[string]any)})
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
	started := map[string]any{wordTurn: map[string]any{"id": codexTurnID}}
	if err := writeJSON(out, rpcResponse{ID: message.ID, Result: started}); err != nil {
		return err
	}
	return writeTurnEvents(out, params.ThreadID, reply)
}

func writeTurnEvents(out io.Writer, threadID string, reply vendorReply) error {
	events := []rpcNotification{{Method: "turn/started", Params: turnPayload(threadID, map[string]any{"id": codexTurnID})}}
	if reply.failed {
		failed := map[string]any{"id": codexTurnID, wordStatus: "failed", wordError: map[string]any{"message": reply.failure}}
		events = append(events, rpcNotification{Method: "turn/completed", Params: turnPayload(threadID, failed)})
	} else {
		delta := map[string]any{keyThreadID: threadID, keyTurnID: codexTurnID, "delta": reply.text}
		item := map[string]any{"id": codexItemID, keyType: wordAgent, wordText: reply.text}
		itemDone := map[string]any{keyThreadID: threadID, keyTurnID: codexTurnID, keyItem: item}
		completed := map[string]any{"id": codexTurnID, wordStatus: "completed"}
		events = append(events,
			rpcNotification{Method: "item/agentMessage/delta", Params: delta},
			rpcNotification{Method: methodItemCompleted, Params: itemDone},
			rpcNotification{Method: "turn/completed", Params: turnPayload(threadID, completed)},
		)
	}
	for _, event := range events {
		if err := writeJSON(out, event); err != nil {
			return err
		}
	}
	return nil
}

func turnPayload(threadID string, turn map[string]any) map[string]any {
	return map[string]any{keyThreadID: threadID, wordTurn: turn}
}
