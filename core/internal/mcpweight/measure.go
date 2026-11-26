package mcpweight

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"maps"
	"os"
	"slices"
	"strings"
	"time"

	"github.com/Herrscherd/herrscher/core/contextwindow"
)

// DefaultTimeout bounds the whole exchange with one server, from its start to its last answer.
const DefaultTimeout = 15 * time.Second

// MaxPages caps how many tools/list pages one server may return before the measure gives up.
const MaxPages = 100

const (
	maxMessageBytes     = 4 << 20
	initialBuffer       = 64 << 10
	stopWaitDelay       = 2 * time.Second
	rpcVersion          = "2.0"
	protocolVersion     = "2025-06-18"
	clientName          = "herrscher-mcp-weight"
	clientVersion       = "1"
	scratchPattern      = "herrscher-mcp-weight-"
	worktreePlaceholder = "{{WORKTREE}}"

	methodInitialize  = "initialize"
	methodInitialized = "notifications/initialized"
	methodToolsList   = "tools/list"
)

// Cause names why a server was not measured, or where its exchange failed.
type Cause string

// The causes a Result carries when it is not measured.
const (
	CauseNotStdio     Cause = "not_stdio"
	CauseNotStarted   Cause = "not_started"
	CauseStart        Cause = "start"
	CauseExited       Cause = "exited"
	CauseTimeout      Cause = "timeout"
	CauseProtocol     Cause = "protocol"
	CauseRejected     Cause = "rejected"
	CauseTooLarge     Cause = "too_large"
	CauseTooManyPages Cause = "too_many_pages"
	CauseStop         Cause = "stop"
)

var (
	errNoTools   = errors.New("tools/list answered without a tools array")
	errPageLimit = errors.New("tools/list kept returning a next cursor past the page limit")
)

// Result is the weight of one server's tool definitions, or the reason it has none.
type Result struct {
	Name      string `json:"name"`
	Transport string `json:"transport"`
	Measured  bool   `json:"measured"`
	Tools     int    `json:"tools"`
	Bytes     int    `json:"bytes"`
	Tokens    int    `json:"tokens"`
	Cause     Cause  `json:"cause,omitempty"`
	Detail    string `json:"detail,omitempty"`
}

type failure struct {
	cause Cause
	err   error
}

func (f *failure) Error() string { return f.err.Error() }

func (f *failure) Unwrap() error { return f.err }

func fail(cause Cause, err error) error {
	return &failure{cause: cause, err: err}
}

// Describe reports a server without starting it: a stdio server is listed as not started, any other transport as not stdio.
func Describe(server Server) Result {
	result := Result{Name: server.Name, Transport: server.Transport(), Cause: CauseNotStdio}
	if server.Stdio() {
		result.Cause = CauseNotStarted
	}
	return result
}

// Measure starts a stdio server in a scratch directory, lists its tools across every page and reports their count and size. Any other transport is reported as not measured, and a failure is reported with its cause, never dropped.
func Measure(ctx context.Context, server Server, timeout time.Duration) Result {
	if !server.Stdio() {
		return Describe(server)
	}
	result := Result{Name: server.Name, Transport: server.Transport()}
	tools, err := serve(ctx, server, timeout)
	if err != nil {
		result.Cause = causeOf(err)
		result.Detail = err.Error()
		return result
	}
	var definitions strings.Builder
	for _, tool := range tools {
		definitions.Write(tool)
	}
	result.Measured = true
	result.Tools = len(tools)
	result.Bytes = definitions.Len()
	result.Tokens = contextwindow.EstimateTokens(definitions.String())
	return result
}

func causeOf(err error) Cause {
	if broken, ok := errors.AsType[*failure](err); ok {
		return broken.cause
	}
	return CauseProtocol
}

func serve(ctx context.Context, server Server, timeout time.Duration) ([]json.RawMessage, error) {
	dir, err := os.MkdirTemp("", scratchPattern)
	if err != nil {
		return nil, fail(CauseStart, err)
	}
	defer removeScratch(dir)
	runCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	session, err := start(runCtx, server, dir)
	if err != nil {
		return nil, err
	}
	tools, listErr := listTools(runCtx, session)
	cancel()
	stopErr := session.stop()
	if listErr != nil {
		return nil, listErr
	}
	if stopErr != nil {
		return nil, fail(CauseStop, stopErr)
	}
	return tools, nil
}

func removeScratch(dir string) {
	if err := os.RemoveAll(dir); err != nil {
		slog.Warn("mcp weight: scratch directory not removed", "path", dir, "err", err)
	}
}

func substitute(value, dir string) string {
	return strings.ReplaceAll(value, worktreePlaceholder, dir)
}

func environment(extra map[string]string, dir string) []string {
	env := os.Environ()
	for _, name := range slices.Sorted(maps.Keys(extra)) {
		env = append(env, name+"="+substitute(extra[name], dir))
	}
	return env
}
