package mcpweight

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Herrscherd/herrscher/core/internal/agent"
)

type session struct {
	cmd            *exec.Cmd
	stdin          io.WriteCloser
	inbound        chan inboundLine
	stopping       chan struct{}
	readerDone     chan struct{}
	stderr         *tailBuffer
	nextID         int
	outputCloseErr error
}

type inboundLine struct {
	line []byte
	err  error
}

type request struct {
	JSONRPC string `json:"jsonrpc"`
	ID      *int   `json:"id,omitempty"`
	Method  string `json:"method"`
	Params  any    `json:"params,omitempty"`
}

type response struct {
	ID     json.RawMessage `json:"id"`
	Method string          `json:"method"`
	Result json.RawMessage `json:"result"`
	Error  *remoteError    `json:"error"`
}

type remoteError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func (e *remoteError) Error() string {
	return fmt.Sprintf("code %d: %s", e.Code, strconv.Quote(e.Message))
}

type clientInfo struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

type initializeParams struct {
	ProtocolVersion string     `json:"protocolVersion"`
	Capabilities    struct{}   `json:"capabilities"`
	ClientInfo      clientInfo `json:"clientInfo"`
}

type listParams struct {
	Cursor string `json:"cursor,omitempty"`
}

type toolsPage struct {
	Tools      []json.RawMessage `json:"tools"`
	NextCursor string            `json:"nextCursor"`
}

type tailBuffer struct {
	mu    sync.Mutex
	limit int
	data  []byte
}

func (b *tailBuffer) Write(chunk []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	written := len(chunk)
	if len(chunk) > b.limit {
		chunk = chunk[len(chunk)-b.limit:]
	}
	b.data = append(b.data, chunk...)
	if excess := len(b.data) - b.limit; excess > 0 {
		b.data = b.data[excess:]
	}
	return written, nil
}

func (b *tailBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return strings.TrimSpace(string(b.data))
}

func start(ctx context.Context, server agent.MCPServer, dir string) (*session, error) {
	cmd := exec.CommandContext(ctx, substitute(server.Command, dir), substituteAll(server.Args, dir)...)
	cmd.Dir = dir
	cmd.Env = environment(server.Env, dir)
	cmd.WaitDelay = stopWaitDelay
	cmd.Cancel = func() error { return killProcessTree(cmd) }
	isolateProcessGroup(cmd)
	stderr := &tailBuffer{limit: stderrTailBytes}
	cmd.Stderr = stderr
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, fail(CauseStart, err)
	}
	stdout, outputWriter, err := os.Pipe()
	if err != nil {
		return nil, fail(CauseStart, errors.Join(err, closeAll(stdin)))
	}
	cmd.Stdout = outputWriter
	if err := cmd.Start(); err != nil {
		return nil, fail(CauseStart, errors.Join(err, closeAll(stdin, stdout, outputWriter)))
	}
	if err := outputWriter.Close(); err != nil {
		return nil, fail(CauseStart, errors.Join(err, discardStarted(cmd, stdin, stdout)))
	}
	session := &session{
		cmd:        cmd,
		stdin:      stdin,
		inbound:    make(chan inboundLine),
		stopping:   make(chan struct{}),
		readerDone: make(chan struct{}),
		stderr:     stderr,
		nextID:     1,
	}
	go session.read(stdout)
	return session, nil
}

func closeAll(closers ...io.Closer) error {
	var errs []error
	for _, closer := range closers {
		errs = append(errs, closer.Close())
	}
	return errors.Join(errs...)
}

func discardStarted(cmd *exec.Cmd, closers ...io.Closer) error {
	killErr := killProcessTree(cmd)
	return errors.Join(killErr, closeAll(closers...), cmd.Wait())
}

func substituteAll(values []string, dir string) []string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		out = append(out, substitute(value, dir))
	}
	return out
}

func (s *session) read(output *os.File) {
	defer close(s.readerDone)
	defer s.closeOutput(output)
	scanner := bufio.NewScanner(output)
	scanner.Buffer(make([]byte, 0, initialBuffer), maxMessageBytes)
	for scanner.Scan() {
		select {
		case s.inbound <- inboundLine{line: bytes.Clone(scanner.Bytes())}:
		case <-s.stopping:
			return
		}
	}
	select {
	case s.inbound <- inboundLine{err: endOfOutput(scanner.Err())}:
	case <-s.stopping:
	}
}

func endOfOutput(err error) error {
	switch {
	case err == nil:
		return fail(CauseExited, io.EOF)
	case errors.Is(err, bufio.ErrTooLong):
		return fail(CauseTooLarge, err)
	}
	return fail(CauseExited, err)
}

func listTools(ctx context.Context, session *session) ([]json.RawMessage, error) {
	if _, err := session.call(ctx, methodInitialize, initializeParams{
		ProtocolVersion: protocolVersion,
		ClientInfo:      clientInfo{Name: clientName, Version: clientVersion},
	}); err != nil {
		return nil, err
	}
	if err := session.send(request{JSONRPC: rpcVersion, Method: methodInitialized}); err != nil {
		return nil, err
	}
	var tools []json.RawMessage
	definitionBytes := 0
	cursor := ""
	for page := 1; page <= MaxPages; page++ {
		raw, err := session.call(ctx, methodToolsList, listParams{Cursor: cursor})
		if err != nil {
			return nil, err
		}
		var listed toolsPage
		if err := json.Unmarshal(raw, &listed); err != nil {
			return nil, fail(CauseProtocol, err)
		}
		if listed.Tools == nil {
			return nil, fail(CauseProtocol, errNoTools)
		}
		definitionBytes += totalBytes(listed.Tools)
		if definitionBytes > maxDefinitionBytes {
			return nil, fail(CauseTooLarge, errDefinitionsTooLarge)
		}
		tools = append(tools, listed.Tools...)
		if listed.NextCursor == "" {
			return tools, nil
		}
		cursor = listed.NextCursor
	}
	return nil, fail(CauseTooManyPages, errPageLimit)
}

func totalBytes(tools []json.RawMessage) int {
	total := 0
	for _, tool := range tools {
		total += len(tool)
	}
	return total
}

func (s *session) call(ctx context.Context, method string, params any) (json.RawMessage, error) {
	requestID := s.nextID
	s.nextID++
	if err := s.send(request{JSONRPC: rpcVersion, ID: &requestID, Method: method, Params: params}); err != nil {
		return nil, err
	}
	for {
		received, err := s.receive(ctx)
		if err != nil {
			return nil, err
		}
		var message response
		if err := json.Unmarshal(received.line, &message); err != nil {
			return nil, fail(CauseProtocol, err)
		}
		if message.Method != "" || !answers(message.ID, requestID) {
			continue
		}
		if message.Error != nil {
			return nil, fail(CauseRejected, message.Error)
		}
		return message.Result, nil
	}
}

func answers(rawID json.RawMessage, requestID int) bool {
	var id *int
	return json.Unmarshal(rawID, &id) == nil && id != nil && *id == requestID
}

func (s *session) send(message request) error {
	payload, err := json.Marshal(message)
	if err != nil {
		return fail(CauseProtocol, err)
	}
	if _, err := s.stdin.Write(append(payload, '\n')); err != nil {
		return fail(CauseExited, err)
	}
	return nil
}

func (s *session) receive(ctx context.Context) (inboundLine, error) {
	select {
	case received := <-s.inbound:
		if received.err != nil {
			return inboundLine{}, received.err
		}
		return received, nil
	case <-ctx.Done():
		return inboundLine{}, contextFailure(ctx.Err())
	}
}

func contextFailure(err error) error {
	if errors.Is(err, context.DeadlineExceeded) {
		return fail(CauseTimeout, err)
	}
	return fail(CauseCanceled, err)
}

func (s *session) stop() error {
	close(s.stopping)
	killErr := killProcessTree(s.cmd)
	outputErr := s.awaitOutput()
	waitErr := s.cmd.Wait()
	if _, exited := errors.AsType[*exec.ExitError](waitErr); exited {
		waitErr = nil
	}
	return errors.Join(killErr, outputErr, waitErr)
}

func (s *session) awaitOutput() error {
	select {
	case <-s.readerDone:
		return s.outputCloseErr
	case <-time.After(stopWaitDelay):
		return errOutputOpen
	}
}

func (s *session) closeOutput(output *os.File) {
	s.outputCloseErr = output.Close()
}

func (s *session) stderrTail() string {
	return s.stderr.String()
}
