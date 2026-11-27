package host

import (
	"bytes"
	"context"
	"io"
	"os/exec"
	"sync/atomic"
	"time"

	"github.com/Herrscherd/herrscher/core/internal/runner"
)

const (
	proxyCloseGrace = 200 * time.Millisecond
	proxyWaitDelay  = time.Second
)

// ProxyConn is a daemon socket reached through `ssh <target> herrscher proxy <channel>`:
// Write reaches the daemon through the remote proxy and Read returns what it answers.
type ProxyConn struct {
	cmd    *exec.Cmd
	stdin  io.WriteCloser
	stdout io.ReadCloser
	stderr *bytes.Buffer
	cancel context.CancelFunc
	killed *atomic.Bool
}

// DialProxy starts the ssh process that carries channel from the daemon of target.
func DialProxy(ctx context.Context, target string, channel ProxyChannel) (*ProxyConn, error) {
	procCtx, cancel := context.WithCancel(ctx)
	cmd := runner.SSH{Target: target}.Command(procCtx, "", "herrscher", proxyVerb, channel.String())
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
	stderr := &bytes.Buffer{}
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
