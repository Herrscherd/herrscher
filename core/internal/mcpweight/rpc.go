package mcpweight

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"os/exec"
)

type session struct {
	cmd     *exec.Cmd
	stdin   io.WriteCloser
	inbound chan inboundLine
	nextID  int
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
	ID     *int            `json:"id"`
	Result json.RawMessage `json:"result"`
	Error  *remoteError    `json:"error"`
}

type remoteError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func (e *remoteError) Error() string { return e.Message }

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

func start(ctx context.Context, server Server, dir string) (*session, error) {
	cmd := exec.CommandContext(ctx, substitute(server.Command, dir), substituteAll(server.Args, dir)...)
	cmd.Dir = dir
	cmd.Env = environment(server.Env, dir)
	cmd.WaitDelay = stopWaitDelay
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, fail(CauseStart, err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fail(CauseStart, err)
	}
	if err := cmd.Start(); err != nil {
		return nil, fail(CauseStart, err)
	}
	inbound := make(chan inboundLine)
	go readLines(ctx, stdout, inbound)
	return &session{cmd: cmd, stdin: stdin, inbound: inbound, nextID: 1}, nil
}

func substituteAll(values []string, dir string) []string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		out = append(out, substitute(value, dir))
	}
	return out
}

func readLines(ctx context.Context, output io.Reader, inbound chan<- inboundLine) {
	scanner := bufio.NewScanner(output)
	scanner.Buffer(make([]byte, 0, initialBuffer), maxMessageBytes)
	for scanner.Scan() {
		select {
		case inbound <- inboundLine{line: bytes.Clone(scanner.Bytes())}:
		case <-ctx.Done():
			return
		}
	}
	end := endOfOutput(scanner.Err())
	select {
	case inbound <- inboundLine{err: end}:
	case <-ctx.Done():
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
		tools = append(tools, listed.Tools...)
		if listed.NextCursor == "" {
			return tools, nil
		}
		cursor = listed.NextCursor
	}
	return nil, fail(CauseTooManyPages, errPageLimit)
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
		if message.ID == nil || *message.ID != requestID {
			continue
		}
		if message.Error != nil {
			return nil, fail(CauseRejected, message.Error)
		}
		return message.Result, nil
	}
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
		return inboundLine{}, fail(CauseTimeout, ctx.Err())
	}
}

func (s *session) stop() error {
	killErr := s.cmd.Process.Kill()
	if errors.Is(killErr, os.ErrProcessDone) {
		killErr = nil
	}
	waitErr := s.cmd.Wait()
	if _, exited := errors.AsType[*exec.ExitError](waitErr); exited {
		waitErr = nil
	}
	return errors.Join(killErr, waitErr)
}
