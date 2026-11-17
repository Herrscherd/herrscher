package vendortest

import (
	"encoding/json"
	"fmt"
	"io"
	"strconv"
)

type contentBlock struct {
	Type  string          `json:"type"`
	Text  string          `json:"text,omitempty"`
	ID    string          `json:"id,omitempty"`
	Name  string          `json:"name,omitempty"`
	Input json.RawMessage `json:"input,omitempty"`
}

type assistantEvent struct {
	Type       string `json:"type"`
	IsAPIError bool   `json:"isApiErrorMessage,omitempty"`
	Message    struct {
		Model   string         `json:"model,omitempty"`
		Content []contentBlock `json:"content"`
	} `json:"message"`
}

func assistantText(text string) assistantEvent {
	event := assistantEvent{Type: "assistant"}
	event.Message.Content = []contentBlock{{Type: "text", Text: text}}
	return event
}

func apiErrorText(text string) assistantEvent {
	event := assistantText(text)
	event.IsAPIError = true
	event.Message.Model = "<synthetic>"
	return event
}

func toolUseID(call int) string {
	return "toolu_" + strconv.Itoa(call)
}

func assistantTool(call int, tool step) assistantEvent {
	event := assistantEvent{Type: "assistant"}
	event.Message.Content = []contentBlock{{Type: "tool_use", ID: toolUseID(call), Name: tool.tool, Input: tool.input}}
	return event
}

type toolResultBlock struct {
	Type      string `json:"type"`
	ToolUseID string `json:"tool_use_id"`
	Content   string `json:"content"`
	IsError   bool   `json:"is_error"`
}

type toolResultEvent struct {
	Type    string `json:"type"`
	Message struct {
		Role    string            `json:"role"`
		Content []toolResultBlock `json:"content"`
	} `json:"message"`
}

func toolResultOf(id string, outcome verdict) toolResultEvent {
	block := toolResultBlock{Type: "tool_result", ToolUseID: id, Content: toolDoneText, IsError: !outcome.allowed}
	if !outcome.allowed {
		block.Content = outcome.reason
	}
	event := toolResultEvent{Type: "user"}
	event.Message.Role = "user"
	event.Message.Content = []toolResultBlock{block}
	return event
}

type tokenUsage struct {
	InputTokens  int `json:"input_tokens"`
	OutputTokens int `json:"output_tokens"`
}

type resultFields struct {
	Type      string     `json:"type"`
	Subtype   string     `json:"subtype"`
	IsError   bool       `json:"is_error"`
	SessionID string     `json:"session_id"`
	CostUSD   float64    `json:"total_cost_usd"`
	Usage     tokenUsage `json:"usage"`
}

type resultEvent struct {
	resultFields

	Result string `json:"result"`
}

type failedResultEvent struct {
	resultFields

	Errors []string `json:"errors"`
}

const charsPerToken = 4

func (r *replay) fields(subtype string, isError bool, prompt, text string) resultFields {
	return resultFields{
		Type:      "result",
		Subtype:   subtype,
		IsError:   isError,
		SessionID: r.sessionID,
		Usage:     tokenUsage{InputTokens: len(prompt)/charsPerToken + 1, OutputTokens: len(text)/charsPerToken + 1},
	}
}

func (r *replay) result(text, prompt string) resultEvent {
	return resultEvent{resultFields: r.fields("success", false, prompt, text), Result: text}
}

func (r *replay) failure(text, prompt string) failedResultEvent {
	return failedResultEvent{resultFields: r.fields("error_during_execution", true, prompt, text), Errors: []string{text}}
}

func writeJSON(out io.Writer, event any) error {
	encoded, err := json.Marshal(event)
	if err != nil {
		return fmt.Errorf("cannot encode an event: %w", err)
	}
	return writeLine(out, string(encoded))
}

func writeLine(out io.Writer, line string) error {
	_, err := fmt.Fprintln(out, line)
	return err
}
