package host

import (
	"bytes"
	"context"
	"io"
	"os/exec"
	"sync"
	"sync/atomic"
	"time"

	"github.com/Herrscherd/herrscher/core/internal/runner"
)

const (
	proxyCloseGrace = 200 * time.Millisecond
	proxyWaitDelay  = time.Second
	proxyLoginBin   = "herrscher"
)

// ProxyConn is a daemon socket reached through `ssh <target> <bin> proxy <channel>`:
// Write reaches the daemon through the remote proxy and Read returns what it answers.
type ProxyConn struct {
	cmd    *exec.Cmd
	stdin  io.WriteCloser
	stdout io.ReadCloser
	stderr *syncBuffer
	cancel context.CancelFunc
	killed *atomic.Bool
}

// DialProxy starts the ssh process that carries channel from the daemon of target.
// bin is the herrscher binary on target, the one host add recorded; an empty bin
// falls back to herrscher on the login PATH. The caller closes the returned connection
// with Close, which is what ends the ssh process when the daemon keeps the channel open.
func DialProxy(ctx context.Context, target, bin string, channel ProxyChannel) (*ProxyConn, error) {
	if bin == "" {
		bin = proxyLoginBin
	}
	procCtx, cancel := context.WithCancel(ctx)
	cmd := runner.SSH{Target: target}.Command(procCtx, "", bin, proxyVerb, channel.String())
	killed := &atomic.Bool{}
	cmd.Cancel = func() error {
		killed.Store(true)
		return cmd.Process.Kill()
	}
	cmd.WaitDelay = proxyWaitDelay
	stdin, err := cmd.StdinPipe()
	if err != nil {
		cancel()
		return nil, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		cancel()
		return nil, err
	}
	stderr := &syncBuffer{}
	cmd.Stderr = stderr
	if err := cmd.Start(); err != nil {
		cancel()
		return nil, err
	}
	return &ProxyConn{cmd: cmd, stdin: stdin, stdout: stdout, stderr: stderr, cancel: cancel, killed: killed}, nil
}

// Read returns what the daemon answered through the proxy.
func (c *ProxyConn) Read(p []byte) (int, error) {
	return c.stdout.Read(p)
}

// Write sends bytes to the daemon through the proxy.
func (c *ProxyConn) Write(p []byte) (int, error) {
	return c.stdin.Write(p)
}

// CloseWrite ends the request side, which the remote proxy relays as end of input.
func (c *ProxyConn) CloseWrite() error {
	return c.stdin.Close()
}

// Stderr is what the remote proxy wrote to its standard error, such as the reason it could not reach the daemon.
// It may be read while the ssh process still runs.
func (c *ProxyConn) Stderr() string {
	return c.stderr.String()
}

// Close waits briefly for the ssh process to exit on its own, reporting its status,
// then stops it. A stopped process is how a channel ends when the daemon keeps it open,
// so that kill is not reported as an error.
func (c *ProxyConn) Close() error {
	logCleanup(c.stdin.Close())
	exited := make(chan error, 1)
	go func() {
		exited <- c.cmd.Wait()
	}()
	grace := time.NewTimer(proxyCloseGrace)
	defer grace.Stop()
	var err error
	select {
	case err = <-exited:
	case <-grace.C:
		c.cancel()
		err = <-exited
	}
	c.cancel()
	if err != nil && !c.killed.Load() {
		return err
	}
	return nil
}

type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(data []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(data)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}
