package host

import (
	"bytes"
	"context"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/Herrscherd/herrscher/core/internal/agent"
	"github.com/Herrscherd/herrscher/core/internal/manager"
	"github.com/Herrscherd/herrscher/core/internal/runner"
	"github.com/Herrscherd/herrscher/core/internal/state"
	"github.com/Herrscherd/herrscher/core/internal/worktree"
)

type hostPlacer struct {
	st            *state.State
	layout        RuntimeLayout
	local         *worktree.Worktreer
	sourceVersion func() string
	newRemote     func(runner.SSH, string) *worktree.Remote
}

func newHostPlacer(hostState *state.State, layout RuntimeLayout, instanceID string, local *worktree.Worktreer, sourceVersion func() string) hostPlacer {
	return hostPlacer{
		st:            hostState,
		layout:        layout,
		local:         local,
		sourceVersion: sourceVersion,
		newRemote: func(r runner.SSH, bin string) *worktree.Remote {
			return worktree.NewRemote(r, bin, instanceID)
		},
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
	transport, err := sshRunner(p.layout, host.SSH)
	if err != nil {
		return state.Host{}, nil, err
	}
	return host, p.newRemote(transport, host.Bin), nil
}

func (p hostPlacer) Ready(name string) error {
	if name == "" {
		return nil
	}
	host, _, err := p.resolve(name)
	if err != nil {
		return err
	}
	if p.sourceVersion == nil {
		return nil
	}
	if want := p.sourceVersion(); want != "" && host.Version != want {
		return &hostVersionMismatchError{Name: name, Have: host.Version, Want: want}
	}
	return nil
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
	payload, err := stageAgentTar(assigned, worktreePath, binIf(hook, host.Bin))
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
	run, err := runnerFor(p.layout, host)
	if err != nil {
		return err
	}
	if _, captureErr := capture(ctx, run, "test", "-d", repo+"/.git"); captureErr == nil {
		return nil
	}
	origin, err := originOf(ctx, filepath.Join(p.st.WorkspaceRoot(), project))
	if err != nil {
		return wrapHost(err, "hosts.clone", map[string]any{"Project": project, "Host": name})
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
