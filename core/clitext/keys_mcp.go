package clitext

// Keys of the MCP tool weight command.
const (
	CmdHelpMCPWeight         = "cli.help.cmd.mcp.weight"
	CmdParamMCPWeightAgent   = "cli.help.param.mcp.weight.agent"
	CmdParamMCPWeightDeep    = "cli.help.param.mcp.weight.deep"
	MCPWeightHeader          = "cli.mcp.weight.header"
	MCPWeightNone            = "cli.mcp.weight.none"
	MCPWeightMeasured        = "cli.mcp.weight.measured"
	MCPWeightFailed          = "cli.mcp.weight.failed"
	MCPWeightFailedDetail    = "cli.mcp.weight.failed_detail"
	MCPWeightEstimate        = "cli.mcp.weight.estimate"
	MCPWeightUnknownAgent    = "cli.mcp.weight.unknown_agent"
	MCPWeightCauseNotStdio   = "cli.mcp.weight.cause.not_stdio"
	MCPWeightCauseNotStarted = "cli.mcp.weight.cause.not_started"
	MCPWeightCauseStart      = "cli.mcp.weight.cause.start"
	MCPWeightCauseExited     = "cli.mcp.weight.cause.exited"
	MCPWeightCauseTimeout    = "cli.mcp.weight.cause.timeout"
	MCPWeightCauseProtocol   = "cli.mcp.weight.cause.protocol"
	MCPWeightCauseRejected   = "cli.mcp.weight.cause.rejected"
	MCPWeightCauseTooLarge   = "cli.mcp.weight.cause.too_large"
	MCPWeightCauseTooMany    = "cli.mcp.weight.cause.too_many_pages"
	MCPWeightCauseStop       = "cli.mcp.weight.cause.stop"
)

func mcpKeys() map[Key][]string {
	return map[Key][]string{
		CmdHelpMCPWeight:         nil,
		CmdParamMCPWeightAgent:   nil,
		CmdParamMCPWeightDeep:    nil,
		MCPWeightHeader:          {ParamAgent},
		MCPWeightNone:            nil,
		MCPWeightMeasured:        {ParamName, ParamTransport, ParamTools, ParamBytes, ParamTokens},
		MCPWeightFailed:          {ParamName, ParamTransport, ParamReason},
		MCPWeightFailedDetail:    {ParamName, ParamTransport, ParamReason, ParamDetail},
		MCPWeightEstimate:        nil,
		MCPWeightUnknownAgent:    {ParamAgent},
		MCPWeightCauseNotStdio:   nil,
		MCPWeightCauseNotStarted: nil,
		MCPWeightCauseStart:      nil,
		MCPWeightCauseExited:     nil,
		MCPWeightCauseTimeout:    nil,
		MCPWeightCauseProtocol:   nil,
		MCPWeightCauseRejected:   nil,
		MCPWeightCauseTooLarge:   nil,
		MCPWeightCauseTooMany:    nil,
		MCPWeightCauseStop:       nil,
	}
}
