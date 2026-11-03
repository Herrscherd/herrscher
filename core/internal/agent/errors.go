package agent

import (
	"errors"
	"fmt"
)

var (
	// ErrEmptyMCPServerName is returned when an mcp.json entry has an empty server name.
	ErrEmptyMCPServerName = errors.New("empty MCP server name in mcp.json")
	// ErrMissingMCPCommand is returned when an mcp.json entry has no command to launch.
	ErrMissingMCPCommand = errors.New("missing command for MCP server")
	// ErrUnsupportedMCPTransport is returned when an mcp.json entry names a transport the agent cannot use.
	ErrUnsupportedMCPTransport = errors.New("unsupported MCP transport")
)

// ExistsError reports an attempt to create an agent whose name is already taken.
type ExistsError struct {
	Name string
}

func (e *ExistsError) Error() string {
	return fmt.Sprintf("agent %q already exists", e.Name)
}

// InvalidNameError reports an agent name that is not a usable slug.
type InvalidNameError struct {
	Name string
}

func (e *InvalidNameError) Error() string {
	return fmt.Sprintf("invalid agent name %q", e.Name)
}

// NotFoundError reports a reference to an agent that does not exist.
type NotFoundError struct {
	Name string
}

func (e *NotFoundError) Error() string {
	return fmt.Sprintf("no agent %q", e.Name)
}

// SettingsShapeError reports a settings document whose shape is not the expected one.
type SettingsShapeError struct {
	Path string
	Want string
	Got  any
}

func (e *SettingsShapeError) Error() string {
	return fmt.Sprintf("settings.json: %s is %T, not %s", e.Path, e.Got, e.Want)
}
