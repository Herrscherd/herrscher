package host

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"net"
	"os"
	"sync"
	"time"

	contracts "github.com/Herrscherd/herrscher-contracts"

	"github.com/Herrscherd/herrscher/core/compactwire"
	"github.com/Herrscherd/herrscher/core/internal/runtimedir"
)

type resultDispatcher interface {
	Dispatch(context.Context, []string) (contracts.Result, error)
}

type dispatcher interface {
	Dispatch(context.Context, []string) (string, error)
}

const defaultCommandReadTimeout = 10 * time.Second

type cmdRequest struct {
	Argv []string `json:"argv"`
}

type cmdResponse struct {
	Ok     *string `json:"ok,omitempty"`
	Err    *string `json:"err,omitempty"`
	Notice string  `json:"notice,omitempty"`
}

func dispatchLiveCommand(ctx context.Context, layout runtimedir.Layout, path string, argv []string) (string, bool, error) {
	dialCtx, cancel := context.WithTimeout(ctx, commandDialTimeout)
	defer cancel()
	conn, err := dialCommandSocket(dialCtx, layout, path)
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return "", true, ctxErr
		}
		return "", false, nil
	}
	defer closeRead(conn)
	stopClose := closeOnDone(ctx, conn)
	defer stopClose()
	if deadline, ok := ctx.Deadline(); ok {
		logCleanup(conn.SetDeadline(deadline))
	}
	payload, err := json.Marshal(cmdRequest{Argv: argv})
	if err != nil {
		return "", true, err
	}
	if _, writeErr := conn.Write(append(payload, '\n')); writeErr != nil {
		return "", true, cancellationOr(ctx, writeErr)
	}
	line, err := bufio.NewReader(conn).ReadBytes('\n')
	if err != nil && len(line) == 0 {
		return "", true, cancellationOr(ctx, err)
	}
	var resp cmdResponse
	if err := json.Unmarshal(line, &resp); err != nil {
		return "", true, wrapHost(err, "command.socket_decode", nil)
	}
	if resp.Err != nil {
		return "", true, commandFailure(resp)
	}
	if resp.Ok == nil {
		return "", true, errCommandSocketEmpty
	}
	return *resp.Ok, true, nil
}

func commandFailure(resp cmdResponse) error {
	plain := commandSocketError{Message: *resp.Err}
	if resp.Notice == "" {
		return &plain
	}
	notice, err := compactwire.DecodeNotice(resp.Notice)
	if err != nil {
		return &plain
	}
	return &noticedSocketError{commandSocketError: plain, notice: notice}
}

func encodedNotice(err error) string {
	noticed, ok := errors.AsType[contracts.NoticeError](err)
	if !ok {
		return ""
	}
	encoded, encodeErr := compactwire.EncodeNotice(noticed.Notice())
	logCleanup(encodeErr)
	return encoded
}

func cancellationOr(ctx context.Context, err error) error {
	if ctxErr := ctx.Err(); ctxErr != nil {
		return ctxErr
	}
	return err
}

func serveCommandSocket(ctx context.Context, layout runtimedir.Layout, path string, disp dispatcher, lang languageSource) {
	if err := serveCommandSocketWithTimeout(ctx, layout, path, disp, defaultCommandReadTimeout); err != nil {
		warnLive(lang, "warn.command_socket_listen", map[string]any{pathParam: path, detailParam: err})
	}
}

func serveCommandSocketWithTimeout(ctx context.Context, layout runtimedir.Layout, path string, disp dispatcher, readTimeout time.Duration) error {
	listener, err := openCommandSocket(ctx, layout, path)
	if err != nil {
		return err
	}
	acceptCommands(ctx, listener, disp, readTimeout)
	return nil
}

func openCommandSocket(ctx context.Context, layout runtimedir.Layout, path string) (ownedListener, error) {
	if err := claimSocketPath(ctx, layout, path); err != nil {
		return ownedListener{}, err
	}
	return listenOwned(ctx, layout, path)
}

type ownedListener struct {
	net.Listener

	path     string
	own      os.FileInfo
	released *sync.Once
}

func listenOwned(ctx context.Context, layout runtimedir.Layout, path string) (ownedListener, error) {
	listener, err := listenCommandSocket(ctx, layout, path)
	if err != nil {
		return ownedListener{}, err
	}
	return ownedListener{Listener: listener, path: path, own: socketIdentity(path), released: new(sync.Once)}, nil
}

func (l ownedListener) release() {
	l.released.Do(func() {
		closeRead(l.Listener)
		removeOwnSocket(l.path, l.own)
	})
}

var errSocketTaken = errors.New("another daemon already answers on this socket (same instance, another state file): stop it or pass --instance")

var errInstanceLocked = errors.New("another daemon already holds the lock of this instance: stop it or pass --instance")

func refuseTakenInstance(ctx context.Context, layout runtimedir.Layout, instID string) error {
	for _, path := range []string{CommandSocketPathIn(layout, instID), EventsSocketPathIn(layout, instID)} {
		if socketAnswers(ctx, layout, path) {
			return wrapHost(errSocketTaken, "daemon.socket_taken", map[string]any{pathParam: path})
		}
	}
	return nil
}

func claimSocketPath(ctx context.Context, layout runtimedir.Layout, path string) error {
	if socketAnswers(ctx, layout, path) {
		return wrapHost(errSocketTaken, "daemon.socket_taken", map[string]any{pathParam: path})
	}
	logCleanup(os.Remove(path))
	return nil
}

func socketIdentity(path string) os.FileInfo {
	info, err := os.Lstat(path)
	logCleanup(err)
	return info
}

func removeOwnSocket(path string, own os.FileInfo) {
	if own == nil {
		return
	}
	current, err := os.Lstat(path)
	if err != nil || !os.SameFile(current, own) {
		return
	}
	logCleanup(os.Remove(path))
}

func socketAnswers(ctx context.Context, layout runtimedir.Layout, path string) bool {
	conn, err := dialCommandSocket(ctx, layout, path)
	if err != nil {
		return false
	}
	logCleanup(conn.Close())
	return true
}

func acceptCommands(ctx context.Context, listener ownedListener, disp dispatcher, readTimeout time.Duration) <-chan struct{} {
	released := make(chan struct{})
	go func() {
		<-ctx.Done()
		listener.release()
		close(released)
	}()
	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			go handleCommandConn(ctx, conn, disp, readTimeout)
		}
	}()
	return released
}

func handleCommandConn(ctx context.Context, conn net.Conn, disp dispatcher, readTimeout time.Duration) {
	defer closeRead(conn)
	logCleanup(conn.SetReadDeadline(time.Now().Add(readTimeout)))
	line, err := bufio.NewReader(conn).ReadBytes('\n')
	var resp cmdResponse
	var req cmdRequest
	switch {
	case len(line) == 0 && err != nil:
		return
	case json.Unmarshal(line, &req) != nil || len(req.Argv) == 0:
		msg := "command socket: malformed request"
		resp.Err = &msg
	default:
		out, derr := disp.Dispatch(ctx, req.Argv)
		if derr != nil {
			msg := derr.Error()
			resp.Err = &msg
			resp.Notice = encodedNotice(derr)
		} else {
			resp.Ok = &out
		}
	}
	payload, marshalErr := json.Marshal(resp)
	logCleanup(marshalErr)
	logWriteFailure(conn.Write(append(payload, '\n')))
}

func closeOnDone(ctx context.Context, conn net.Conn) func() {
	closed := make(chan struct{})
	stopAfter := context.AfterFunc(ctx, func() {
		defer close(closed)
		logCleanup(conn.Close())
	})
	return func() {
		if !stopAfter() {
			<-closed
		}
	}
}
