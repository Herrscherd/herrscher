package clitext

// Keys of the MCP tool weight command.
const (
	CmdHelpToolWeight         = "cli.help.cmd.mcp.weight"
	CmdParamToolWeightAgent   = "cli.help.param.mcp.weight.agent"
	CmdParamToolWeightDeep    = "cli.help.param.mcp.weight.deep"
	ToolWeightHeader          = "cli.mcp.weight.header"
	ToolWeightNone            = "cli.mcp.weight.none"
	ToolWeightMeasured        = "cli.mcp.weight.measured"
	ToolWeightFailed          = "cli.mcp.weight.failed"
	ToolWeightFailedDetail    = "cli.mcp.weight.failed_detail"
	ToolWeightStopped         = "cli.mcp.weight.stopped"
	ToolWeightEstimate        = "cli.mcp.weight.estimate"
	ToolWeightCauseNotStdio   = "cli.mcp.weight.cause.not_stdio"
	ToolWeightCauseNotStarted = "cli.mcp.weight.cause.not_started"
	ToolWeightCauseStart      = "cli.mcp.weight.cause.start"
	ToolWeightCauseExited     = "cli.mcp.weight.cause.exited"
	ToolWeightCauseTimeout    = "cli.mcp.weight.cause.timeout"
	ToolWeightCauseCanceled   = "cli.mcp.weight.cause.canceled"
	ToolWeightCauseProtocol   = "cli.mcp.weight.cause.protocol"
	ToolWeightCauseRejected   = "cli.mcp.weight.cause.rejected"
	ToolWeightCauseTooLarge   = "cli.mcp.weight.cause.too_large"
	ToolWeightCauseTooMany    = "cli.mcp.weight.cause.too_many_pages"
	ToolWeightCauseStop       = "cli.mcp.weight.cause.stop"
)

func mcpKeys() map[Key][]string {
	return map[Key][]string{
		CmdHelpToolWeight:         nil,
		CmdParamToolWeightAgent:   nil,
		CmdParamToolWeightDeep:    nil,
		ToolWeightHeader:          {ParamAgent},
		ToolWeightNone:            nil,
		ToolWeightMeasured:        {ParamName, ParamTransport, ParamTools, ParamBytes, ParamTokens},
		ToolWeightFailed:          {ParamName, ParamTransport, ParamReason},
		ToolWeightFailedDetail:    {ParamName, ParamTransport, ParamReason, ParamDetail},
		ToolWeightStopped:         {ParamName, ParamReason, ParamDetail},
		ToolWeightEstimate:        nil,
		ToolWeightCauseNotStdio:   nil,
		ToolWeightCauseNotStarted: nil,
		ToolWeightCauseStart:      nil,
		ToolWeightCauseExited:     nil,
		ToolWeightCauseTimeout:    nil,
		ToolWeightCauseCanceled:   nil,
		ToolWeightCauseProtocol:   nil,
		ToolWeightCauseRejected:   nil,
		ToolWeightCauseTooLarge:   nil,
		ToolWeightCauseTooMany:    nil,
		ToolWeightCauseStop:       nil,
	}
}
