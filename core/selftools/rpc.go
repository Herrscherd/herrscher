// Package selftools is the MCP server a session runs to reach its own daemon:
// every verb of the command registry becomes a tool, and every call goes to the
// session's command socket, where the daemon decides it as it decides any other.
package selftools

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"slices"
	"sync"
)

const (
	jsonRPCVersion        = "2.0"
	codeParseError        = -32700
	codeInvalidRequest    = -32600
	codeMethodNotFound    = -32601
	codeInvalidParams     = -32602
	maxRequestBytes       = 4 << 20
	maxConcurrentCalls    = 8
	latestProtocolVersion = "2025-06-18"
)

func supportedProtocolVersions() []string {
	return []string{"2024-11-05", "2025-03-26", latestProtocolVersion}
}

var errRequestTooLarge = errors.New("selftools: request exceeds the size limit")

type request struct {
	Version string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

func (r request) isNotification() bool {
	return len(r.ID) == 0
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type response struct {
	Version string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Result  any             `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

type initializeParams struct {
	ProtocolVersion string `json:"protocolVersion"`
}

type serverInfo struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

type toolsCapability struct {
	ListChanged bool `json:"listChanged"`
}

type serverCapabilities struct {
	Tools toolsCapability `json:"tools"`
}

type initializeResult struct {
	ProtocolVersion string             `json:"protocolVersion"`
	Capabilities    serverCapabilities `json:"capabilities"`
	ServerInfo      serverInfo         `json:"serverInfo"`
	Instructions    string             `json:"instructions"`
}

type toolsListResult struct {
	Tools []Tool `json:"tools"`
}

type callParams struct {
	Name      string          `json:"name"`
	Arguments json.RawMessage `json:"arguments,omitempty"`
}

type textContent struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

type callResult struct {
	Content []textContent `json:"content"`
	IsError bool          `json:"isError"`
}

// Server answers MCP requests with the tools of one daemon.
type Server struct {
	daemon  Daemon
	version string
	catalog *catalog
}

// NewServer returns a Server that reaches the daemon through daemon, names itself with version,
// and offers the local tools beside the daemon's verbs.
func NewServer(daemon Daemon, version string, exposure Exposure, locals ...LocalTool) *Server {
	return &Server{daemon: daemon, version: version, catalog: newCatalog(daemon, exposure, locals)}
}

// Serve reads one JSON-RPC message per line from in and writes each answer to out, until in ends or ctx is done.
// Requests run concurrently, so a call waiting on the operator does not hold the others back.
func (s *Server) Serve(ctx context.Context, in io.Reader, out io.Writer) error {
	scanner := bufio.NewScanner(in)
	scanner.Buffer(make([]byte, 0, bufio.MaxScanTokenSize), maxRequestBytes)
	answers := &answerWriter{encoder: json.NewEncoder(out)}
	slots := make(chan struct{}, maxConcurrentCalls)
	var running sync.WaitGroup
	defer running.Wait()
	for scanner.Scan() {
		if err := ctx.Err(); err != nil {
			return err
		}
		line := slices.Clone(scanner.Bytes())
		if len(line) == 0 {
			continue
		}
		select {
		case slots <- struct{}{}:
		case <-ctx.Done():
			return ctx.Err()
		}
		running.Go(func() {
			defer func() { <-slots }()
			if reply, answer := s.handle(ctx, line); answer {
				answers.write(reply)
			}
		})
	}
	if err := scanner.Err(); err != nil {
		if errors.Is(err, bufio.ErrTooLong) {
			return errRequestTooLarge
		}
		return fmt.Errorf("selftools: read request: %w", err)
	}
	running.Wait()
	return answers.failure()
}

type answerWriter struct {
	mu      sync.Mutex
	encoder *json.Encoder
	err     error
}

func (w *answerWriter) write(reply response) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.err != nil {
		return
	}
	if err := w.encoder.Encode(reply); err != nil {
		w.err = fmt.Errorf("selftools: write answer: %w", err)
	}
}

func (w *answerWriter) failure() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.err
}

func (s *Server) handle(ctx context.Context, line []byte) (response, bool) {
	var req request
	if err := json.Unmarshal(line, &req); err != nil {
		return failure(json.RawMessage("null"), codeParseError, err.Error()), true
	}
	if req.Version != jsonRPCVersion || req.Method == "" {
		if req.isNotification() {
			return response{}, false
		}
		return failure(req.ID, codeInvalidRequest, "not a JSON-RPC 2.0 request"), true
	}
	result, rpcErr := s.dispatch(ctx, req)
	if req.isNotification() {
		return response{}, false
	}
	if rpcErr != nil {
		return response{Version: jsonRPCVersion, ID: req.ID, Error: rpcErr}, true
	}
	return response{Version: jsonRPCVersion, ID: req.ID, Result: result}, true
}

func (s *Server) dispatch(ctx context.Context, req request) (any, *rpcError) {
	switch req.Method {
	case "initialize":
		return s.initialize(req.Params), nil
	case "ping":
		return struct{}{}, nil
	case "tools/list":
		return toolsListResult{Tools: s.catalog.tools(ctx)}, nil
	case "tools/call":
		return s.call(ctx, req.Params)
	}
	if req.isNotification() {
		return nil, nil
	}
	return nil, &rpcError{Code: codeMethodNotFound, Message: "method not found: " + req.Method}
}

func (s *Server) initialize(raw json.RawMessage) initializeResult {
	var params initializeParams
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &params); err != nil {
			params = initializeParams{}
		}
	}
	version := latestProtocolVersion
	if slices.Contains(supportedProtocolVersions(), params.ProtocolVersion) {
		version = params.ProtocolVersion
	}
	return initializeResult{
		ProtocolVersion: version,
		Capabilities:    serverCapabilities{Tools: toolsCapability{ListChanged: false}},
		ServerInfo:      serverInfo{Name: ServerName, Version: s.version},
		Instructions:    serverInstructions,
	}
}

func (s *Server) call(ctx context.Context, raw json.RawMessage) (any, *rpcError) {
	var params callParams
	if err := json.Unmarshal(raw, &params); err != nil {
		return nil, &rpcError{Code: codeInvalidParams, Message: err.Error()}
	}
	text, err := s.catalog.call(ctx, params.Name, params.Arguments)
	if errors.Is(err, errUnknownTool) {
		return nil, &rpcError{Code: codeInvalidParams, Message: err.Error()}
	}
	if err != nil {
		return callResult{Content: []textContent{{Type: "text", Text: err.Error()}}, IsError: true}, nil
	}
	return callResult{Content: []textContent{{Type: "text", Text: text}}, IsError: false}, nil
}

func failure(id json.RawMessage, code int, message string) response {
	return response{Version: jsonRPCVersion, ID: id, Error: &rpcError{Code: code, Message: message}}
}
