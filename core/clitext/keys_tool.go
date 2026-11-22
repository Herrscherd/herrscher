package clitext

import (
	"time"

	contracts "github.com/Herrscherd/herrscher-contracts"
	"github.com/Herrscherd/herrscher-contracts/i18n"
	"github.com/Herrscherd/herrscher/core/timeline"
)

// Keys of the lines that show one tool call of a turn: its outcome, duration and tokens.
const (
	ToolSucceeded  = "cli.tool.succeeded"
	ToolFailed     = "cli.tool.failed"
	ToolUnfinished = "cli.tool.unfinished"
	ToolBound      = "cli.tool.bound"
)

func toolKeys() map[Key][]string {
	return map[Key][]string{
		ToolSucceeded:  {ParamCall, ParamDuration, ParamTokens},
		ToolFailed:     {ParamCall, ParamDuration, ParamError},
		ToolUnfinished: {ParamCall},
		ToolBound:      {ParamLimit},
	}
}

// ToolCallLine picks the key and the params of the line that shows call in lang.
// A call still open has no duration and no outcome, so it gets the unfinished line.
func ToolCallLine(lang i18n.Language, call timeline.Call) (Key, Params) {
	if !call.Finished() {
		return ToolUnfinished, Params{ParamCall: call.Label()}
	}
	params := Params{
		ParamCall:     call.Label(),
		ParamDuration: FormatDuration(lang, time.Duration(*call.DurationMs)*time.Millisecond),
	}
	if call.State == contracts.ToolCallFailed {
		params[ParamError] = call.Error
		return ToolFailed, params
	}
	params[ParamTokens] = FormatNumber(lang, float64(call.Tokens), 0)
	return ToolSucceeded, params
}
