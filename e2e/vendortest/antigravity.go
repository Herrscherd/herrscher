package vendortest

import (
	"encoding/json"
	"errors"
	"io"
	"slices"
	"strings"
)

const (
	antigravityLabel           = "antigravity"
	antigravityVersion         = "1.5.18 (vendortest)"
	antigravityFetching        = "Fetching available models..."
	antigravityModels          = "vendortest-model\tVendortest Model (High)"
	defaultAntigravityConv     = "vendortest-antigravity-conversation"
	antigravityInputFormat     = "--input-format"
	antigravityOutputFormat    = "--output-format"
	antigravityStreamFormat    = "stream-json"
	antigravityModelsCommand   = "models"
	antigravityConversation    = "--conversation"
	antigravityEventUser       = "user"
	antigravityStatusSucceeded = "SUCCESS"
	antigravityStatusFailed    = "ERROR"
)

var (
	errAntigravityArgs  = errors.New("the antigravity fake speaks only models, --version and stream-json turns on stdin")
	errAntigravityInput = errors.New("the antigravity fake reads one user event on stdin")
)

// Antigravity runs the fake agy CLI on args and returns its exit status.
func Antigravity(args []string, streams Streams, files Files) int {
	if slices.Contains(args, flagVersion) {
		return exitOf(streams.Err, writeLine(streams.Out, antigravityVersion))
	}
	if len(args) > 0 && args[0] == antigravityModelsCommand {
		return exitOf(streams.Err, errors.Join(writeLine(streams.Err, antigravityFetching), writeLine(streams.Out, antigravityModels)))
	}
	if flagValue(args, antigravityInputFormat) != antigravityStreamFormat || flagValue(args, antigravityOutputFormat) != antigravityStreamFormat {
		return exitOf(streams.Err, errAntigravityArgs)
	}
	prompt, err := readAntigravityPrompt(streams.In)
	if err != nil {
		return exitOf(streams.Err, err)
	}
	fake, err := openVendor(antigravityLabel, files)
	if err != nil {
		return exitOf(streams.Err, err)
	}
	return exitOf(streams.Err, fake.antigravityTurn(streams.Out, flagValue(args, antigravityConversation), prompt))
}

type antigravityInput struct {
	Event   string `json:"event"`
	Message struct {
		Content string `json:"content"`
	} `json:"message"`
}

func readAntigravityPrompt(in io.Reader) (string, error) {
	data, err := io.ReadAll(in)
	if err != nil {
		return "", err
	}
	var input antigravityInput
	if json.Unmarshal([]byte(strings.TrimSpace(string(data))), &input) != nil || input.Event != antigravityEventUser {
		return "", errAntigravityInput
	}
	return input.Message.Content, nil
}

type antigravityInit struct {
	Event          string `json:"event"`
	ConversationID string `json:"conversation_id"`
}

type antigravityStep struct {
	Event      string `json:"event"`
	StepUpdate struct {
		StepIndex int    `json:"step_index"`
		State     string `json:"state"`
		StepType  string `json:"step_type"`
		TextDelta string `json:"text_delta"`
	} `json:"step_update"`
}

type antigravityResult struct {
	Event  string `json:"event"`
	Result struct {
		ConversationID string `json:"conversation_id"`
		Status         string `json:"status"`
		Response       string `json:"response"`
		Error          string `json:"error,omitempty"`
	} `json:"result"`
}

func (v *vendor) antigravityTurn(out io.Writer, resume, prompt string) error {
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
	conversation := resume
	if conversation == "" {
		conversation = defaultAntigravityConv
	}
	if err := writeJSON(out, antigravityInit{Event: "init", ConversationID: conversation}); err != nil {
		return err
	}
	var step antigravityStep
	step.Event = "step_update"
	step.StepUpdate.StepIndex = 1
	step.StepUpdate.State = "DONE"
	step.StepUpdate.StepType = "agent_response"
	step.StepUpdate.TextDelta = reply.text
	if err := writeJSON(out, step); err != nil {
		return err
	}
	var result antigravityResult
	result.Event = wordResult
	result.Result.ConversationID = conversation
	result.Result.Status = antigravityStatusSucceeded
	result.Result.Response = reply.text
	if reply.failed {
		result.Result.Status = antigravityStatusFailed
		result.Result.Error = reply.failure
	}
	return writeJSON(out, result)
}
