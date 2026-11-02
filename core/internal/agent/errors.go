package agent

import (
	"errors"
	"fmt"
)

var (
	ErrEmptyMCPServerName      = errors.New("empty MCP server name in mcp.json")
	ErrMissingMCPCommand       = errors.New("missing command for MCP server")
	ErrUnsupportedMCPTransport = errors.New("unsupported MCP transport")
)

type ExistsError struct {
	Name string
}

func (e *ExistsError) Error() string {
	return fmt.Sprintf("agent %q already exists", e.Name)
}

type InvalidNameError struct {
	Name string
}

func (e *InvalidNameError) Error() string {
	return fmt.Sprintf("invalid agent name %q", e.Name)
}

type NotFoundError struct {
	Name string
}

func (e *NotFoundError) Error() string {
	return fmt.Sprintf("no agent %q", e.Name)
}

type SettingsShapeError struct {
	Path string
	Want string
	Got  any
}

func (e *SettingsShapeError) Error() string {
	return fmt.Sprintf("settings.json: %s is %T, not %s", e.Path, e.Got, e.Want)
}
