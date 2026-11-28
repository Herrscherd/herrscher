package host

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/Herrscherd/herrscher/core/clitext"
	"github.com/Herrscherd/herrscher/core/internal/agent"
	"github.com/Herrscherd/herrscher/core/internal/manager"
	"github.com/Herrscherd/herrscher/core/internal/runner"
	"github.com/Herrscherd/herrscher/core/internal/state"
	"github.com/Herrscherd/herrscher/core/internal/worktree"
)

const (
	defaultHostWaitLimit    = 10 * time.Minute
	defaultHostPollInterval = 5 * time.Second
)

type hostPlacer struct {
	messages      *hostMessages
	lang          languageSource
	warn          io.Writer
	st            *state.State
	layout        RuntimeLayout
	local         *worktree.Worktreer
	sourceVersion func() string
	newRemote     func(runner.SSH, string) *worktree.Remote
	cli           *clitext.Messages
	waitLimit     time.Duration
	pollInterval  time.Duration
}

func newHostPlacer(messages *hostMessages, cliMessages *clitext.Messages, lang languageSource, warn io.Writer, hostState *state.State, layout RuntimeLayout, instanceID string, local *worktree.Worktreer, sourceVersion func() string) hostPlacer {
	return hostPlacer{
		cli:           cliMessages,
		messages:      messages,
		lang:          lang,
		warn:          warn,
		st:            hostState,
		layout:        layout,
		local:         local,
		sourceVersion: sourceVersion,
		newRemote: func(r runner.SSH, bin string) *worktree.Remote {
			return worktree.NewRemote(cliMessages, r, bin, instanceID)
		},
		waitLimit:    defaultHostWaitLimit,
		pollInterval: defaultHostPollInterval,
	}
}

func (p hostPlacer) resolve(name string) (state.Host, *worktree.Remote, error) {
	host, ok := p.st.FindHost(name)
	if !ok {
		return state.Host{}, nil, unknownHost(p.st, name)
	}
	if host.Bin == "" {
		return state.Host{}, nil, &hostNotProvisionedError{Name: name}
	}
	transport, err := sshRunner(p.messages, p.lang, p.warn, p.layout, host)
	if err != nil {
		return state.Host{}, nil, err
	}
	return host, p.newRemote(transport, host.Bin), nil
}

func (p hostPlacer) Ready(ctx context.Context, name string) error {
	if name == "" {
		return nil
	}
	return p.awaitRoom(ctx, name)
}

func (p hostPlacer) awaitRoom(ctx context.Context, name string) error {
	limit := time.NewTimer(p.waitLimit)
	defer limit.Stop()
	poll := time.NewTicker(p.pollInterval)
	defer poll.Stop()
	for {
		err := p.admit(ctx, name)
		if err == nil || !retryableAdmission(err) {
			return err
		}
		if host, _ := p.st.FindHost(name); host.WhenFull != state.FullWait {
			return err
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("%w: %w", ctx.Err(), err)
		case <-limit.C:
			return fmt.Errorf("%w: %w", errHostWaitLimit, err)
		case <-poll.C:
		}
	}
}

func retryableAdmission(err error) bool {
	return errors.Is(err, errHostFull) || errors.Is(err, errHostUnreachable)
}

func (p hostPlacer) admit(ctx context.Context, name string) error {
	host, _, err := p.resolve(name)
	if err != nil {
		return err
	}
	transport, err := sshRunner(p.messages, p.lang, p.warn, p.layout, host)
	if err != nil {
		return err
	}
	if host.KeyChanged != "" {
		return withDetail(errHostKeyChanged, fmt.Sprintf("host %q, pinned %s, now %s", name, host.HostKey, host.KeyChanged))
	}
	if host.HostKey == "" {
		warnCatalog(p.warn, p.cli, p.lang, clitext.HostWarnUnpinned, clitext.Params{clitext.ParamHost: name})
	}
	if err := p.verifyBinary(ctx, host, transport); err != nil {
		return err
	}
	if host.MaxSessions > 0 {
		if running := p.st.SessionsOn(name); running >= host.MaxSessions {
			return withDetail(errHostFull, fmt.Sprintf("host %q runs %d of %d sessions", name, running, host.MaxSessions))
		}
	}
	if p.sourceVersion == nil {
		return nil
	}
	if want := p.sourceVersion(); want != "" && host.Version != want {
		return &hostVersionMismatchError{Name: name, Have: host.Version, Want: want}
	}
	return nil
}

func (p hostPlacer) verifyBinary(ctx context.Context, host state.Host, transport runner.SSH) error {
	if host.BinSHA256 == "" {
		warnCatalog(p.warn, p.cli, p.lang, clitext.HostBinaryUnrecorded, clitext.Params{clitext.ParamHost: host.Name})
		return nil
	}
	err := transport.VerifyBinary(ctx, host.Bin, host.BinSHA256)
	if err == nil || errors.Is(err, runner.ErrBinaryChanged) {
		return err
	}
	return fmt.Errorf("%w: %w", errHostUnreachable, err)
}

func (p hostPlacer) Worktrees(name string) (manager.Worktrees, error) {
	if name == "" {
		return p.local, nil
	}
	_, rem, err := p.resolve(name)
	if err != nil {
		return nil, err
	}
	return rem, nil
}

func (p hostPlacer) Workspace(name string) (string, error) {
	if name == "" {
		return p.st.WorkspaceRoot(), nil
	}
	host, _, err := p.resolve(name)
	if err != nil {
		return "", err
	}
	return host.Workspace, nil
}

func (p hostPlacer) Materialize(ctx context.Context, name string, assigned agent.Agent, worktreePath string, hook bool) error {
	if name == "" {
		return assigned.MaterializeAs(ctx, worktreePath, binIf(hook, agent.SelfBin()))
	}
	host, rem, err := p.resolve(name)
	if err != nil {
		return err
	}
	payload, err := stageAgentTar(p.messages, assigned, worktreePath, binIf(hook, host.Bin))
	if err != nil {
		return err
	}
	return rem.Materialize(ctx, worktreePath, payload)
}

func binIf(hook bool, bin string) string {
	if !hook {
		return ""
	}
	return bin
}

func (p hostPlacer) EnsureProject(ctx context.Context, name, project, repo string) error {
	if name == "" || project == "" {
		return nil
	}
	host, _, err := p.resolve(name)
	if err != nil {
		return err
	}
	if !projectAllowed(host, project) {
		return withDetail(errProjectNotAllowed, fmt.Sprintf("%q on host %q", project, name))
	}
	run, err := runnerFor(p.messages, p.lang, p.warn, p.layout, host)
	if err != nil {
		return err
	}
	if _, captureErr := capture(ctx, run, "test", "-d", repo+"/.git"); captureErr == nil {
		return nil
	}
	origin, err := originOf(ctx, filepath.Join(p.st.WorkspaceRoot(), project))
	if err != nil {
		return wrapHost(p.messages, err, "hosts.clone", map[string]any{"Project": project, "Host": name})
	}
	if _, err := capture(ctx, run, "git", "clone", origin, repo); err != nil {
		return err
	}
	return nil
}

func originOf(ctx context.Context, dir string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", "-C", dir, "remote", "get-url", "origin")
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = err.Error()
		}
		return "", &originReadError{Dir: dir, Msg: msg}
	}
	url := strings.TrimSpace(stdout.String())
	if url == "" {
		return "", &noOriginError{Dir: dir}
	}
	return url, nil
}

func projectAllowed(host state.Host, project string) bool {
	return len(host.Projects) == 0 || slices.Contains(host.Projects, project)
}
