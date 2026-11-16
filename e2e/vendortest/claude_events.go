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
		Usage   *messageUsage  `json:"usage,omitempty"`
	} `json:"message"`
}

func assistantText(text string) assistantEvent {
	event := assistantEvent{Type: assistantType}
	event.Message.Content = []contentBlock{{Type: "text", Text: text}}
	return event
}

func apiErrorText(text string) assistantEvent {
	event := assistantText(text)
	event.IsAPIError = true
	event.Message.Model = "<synthetic>"
	return event
}

func assistantUsage(tokens int) assistantEvent {
	event := assistantEvent{Type: assistantType}
	event.Message.Content = []contentBlock{}
	event.Message.Usage = &messageUsage{InputTokens: tokens}
	return event
}

func assistantTool(call int, tool step) assistantEvent {
	event := assistantEvent{Type: assistantType}
	event.Message.Content = []contentBlock{{Type: "tool_use", ID: "toolu_" + strconv.Itoa(call), Name: tool.tool, Input: tool.input}}
	return event
}

type messageUsage struct {
	InputTokens int `json:"input_tokens"`
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
