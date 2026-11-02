package agent

import "errors"

var (
	ErrAgentExists             = errors.New("agent")
	ErrEmptyMCPServerName      = errors.New("empty MCP server name in mcp.json")
	ErrHooksNotObject          = errors.New("settings.json: hooks is")
	ErrInvalidAgentName        = errors.New("invalid agent name")
	ErrMissingMCPCommand       = errors.New("missing command for MCP server")
	ErrNoAgent                 = errors.New("no agent")
	ErrPreToolUseNotList       = errors.New("settings.json: hooks.PreToolUse is")
	ErrUnsupportedMCPTransport = errors.New("unsupported MCP transport")
)
