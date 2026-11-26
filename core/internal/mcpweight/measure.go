package mcpweight

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"maps"
	"os"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Herrscherd/herrscher/core/contextwindow"
	"github.com/Herrscherd/herrscher/core/internal/agent"
)

// DefaultTimeout bounds the whole exchange with one server, from its start to its last answer.
const DefaultTimeout = 15 * time.Second

// DefaultBudget bounds the measure of all the servers of one agent together.
const DefaultBudget = 40 * time.Second

// ReplyMargin is the room kept between the budget and the caller's deadline, for the servers still stopping and for the report to be written.
const ReplyMargin = 10 * time.Second

// Concurrency caps how many servers are measured at the same time.
const Concurrency = 4

// MaxPages caps how many tools/list pages one server may return before the measure gives up.
const MaxPages = 100

const (
	maxMessageBytes     = 4 << 20
	maxDefinitionBytes  = 16 << 20
	initialBuffer       = 64 << 10
	stderrTailBytes     = 1 << 10
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

// The causes a Result carries when the server was not measured, or was measured but did not stop cleanly.
const (
	CauseNotStdio     Cause = "not_stdio"
	CauseNotStarted   Cause = "not_started"
	CauseStart        Cause = "start"
	CauseExited       Cause = "exited"
	CauseTimeout      Cause = "timeout"
	CauseBudget       Cause = "budget"
	CauseCanceled     Cause = "canceled"
	CauseProtocol     Cause = "protocol"
	CauseRejected     Cause = "rejected"
	CauseTooLarge     Cause = "too_large"
	CauseTooManyPages Cause = "too_many_pages"
	CauseStop         Cause = "stop"
)

var (
	errNoTools             = errors.New("tools/list answered without a tools array")
	errPageLimit           = errors.New("tools/list kept returning a next cursor past the page limit")
	errDefinitionsTooLarge = errors.New("tools/list definitions exceed the total size limit")
	errOutputOpen          = errors.New("the server output is still open after the server was killed")
	errBudgetExceeded      = errors.New("the measure budget of all the servers is spent")
)

// Result is the weight of one server's tool definitions, or the reason it has none. Cause is set when the server was not measured, and also when it was measured but did not stop cleanly, in which case Measured stays true.
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
func Describe(server agent.MCPServer) Result {
	result := Result{Name: server.Name, Transport: server.Transport(), Cause: CauseNotStdio}
	if server.Stdio() {
		result.Cause = CauseNotStarted
	}
	return result
}

// Measure starts a stdio server in a scratch directory, lists its tools across every page and reports their count and size. Any other transport is reported as not measured, and a failure is reported with its cause, never dropped.
func Measure(ctx context.Context, server agent.MCPServer, timeout time.Duration) Result {
	if !server.Stdio() {
		return Describe(server)
	}
	result := Result{Name: server.Name, Transport: server.Transport()}
	outcome := serve(ctx, server, timeout)
	if outcome.err != nil {
		result.Cause = causeOf(outcome.err)
		result.Detail = detail(outcome.err, outcome.stderr)
		return result
	}
	fillWeight(&result, outcome.tools)
	if outcome.stop != nil {
		result.Cause = CauseStop
		result.Detail = detail(outcome.stop, outcome.stderr)
	}
	return result
}

// Measurer measures servers under one slot pool shared by all its calls, so that reports asked at the same time never run more than Concurrency servers together.
type Measurer struct {
	slots chan struct{}
}

// NewMeasurer returns a Measurer that keeps at most Concurrency stdio servers running at a time.
func NewMeasurer() *Measurer {
	return &Measurer{slots: make(chan struct{}, Concurrency)}
}

// MeasureAll measures the servers within one budget, each stdio server only once it holds one of the measurer's slots, and the results keep the order of the servers given. A server the budget or the caller cuts short while it waits for a slot is reported with its cause, and every server gets a Result.
func (m *Measurer) MeasureAll(ctx context.Context, servers []agent.MCPServer, budget time.Duration) []Result {
	budgetCtx, cancel := context.WithTimeoutCause(ctx, budget, errBudgetExceeded)
	defer cancel()
	results := make([]Result, len(servers))
	var measures sync.WaitGroup
	for index, server := range servers {
		measures.Go(func() {
			results[index] = m.measureInTurn(budgetCtx, server)
		})
	}
	measures.Wait()
	return results
}

func (m *Measurer) measureInTurn(ctx context.Context, server agent.MCPServer) Result {
	if !server.Stdio() {
		return Describe(server)
	}
	if err := m.acquire(ctx); err != nil {
		return Result{Name: server.Name, Transport: server.Transport(), Cause: causeOf(err), Detail: err.Error()}
	}
	defer m.release()
	return Measure(ctx, server, DefaultTimeout)
}

func (m *Measurer) acquire(ctx context.Context) error {
	select {
	case m.slots <- struct{}{}:
		return nil
	case <-ctx.Done():
		return contextFailure(ctx)
	}
}

func (m *Measurer) release() {
	<-m.slots
}

func fillWeight(result *Result, tools []json.RawMessage) {
	var definitions strings.Builder
	definitions.Grow(totalBytes(tools))
	for _, tool := range tools {
		definitions.Write(tool)
	}
	result.Measured = true
	result.Tools = len(tools)
	result.Bytes = definitions.Len()
	result.Tokens = contextwindow.EstimateTokens(definitions.String())
}

func detail(err error, stderr string) string {
	if stderr == "" {
		return err.Error()
	}
	return err.Error() + "; stderr: " + strconv.Quote(stderr)
}

func causeOf(err error) Cause {
	if broken, ok := errors.AsType[*failure](err); ok {
		return broken.cause
	}
	return CauseProtocol
}

type outcome struct {
	tools  []json.RawMessage
	stderr string
	err    error
	stop   error
}

func serve(ctx context.Context, server agent.MCPServer, timeout time.Duration) outcome {
	dir, err := os.MkdirTemp("", scratchPattern)
	if err != nil {
		return outcome{err: fail(CauseStart, err)}
	}
	defer removeScratch(dir)
	runCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	session, err := start(runCtx, server, dir)
	if err != nil {
		return outcome{err: startFailure(runCtx, err)}
	}
	tools, listErr := listTools(runCtx, session)
	cancel()
	stopErr := session.stop()
	return outcome{tools: tools, stderr: session.stderrTail(), err: listErr, stop: stopErr}
}

func startFailure(ctx context.Context, err error) error {
	if ctx.Err() != nil {
		return contextFailure(ctx)
	}
	return err
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
