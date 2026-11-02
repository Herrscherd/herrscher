package host

import (
	"bytes"
	"context"
	"fmt"
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
	ctx           context.Context
	st            *state.State
	instanceID    string
	local         *worktree.Worktreer
	sourceVersion func() string
}

func (p hostPlacer) resolve(name string) (state.Host, *worktree.Remote, error) {
	h, ok := p.st.FindHost(name)
	if !ok {
		return state.Host{}, nil, unknownHost(p.st, name)
	}
	if h.Bin == "" {
		return state.Host{}, nil, &hostNotProvisionedError{Name: name}
	}
	r := runner.SSH{Target: h.SSH, ControlPath: runner.ControlPathFor(h.SSH)}
	return h, worktree.NewRemote(p.ctx, r, h.Bin, p.instanceID), nil
}

func (p hostPlacer) Ready(name string) error {
	if name == "" {
		return nil
	}
	h, _, err := p.resolve(name)
	if err != nil {
		return err
	}
	if p.sourceVersion == nil {
		return nil
	}
	if want := p.sourceVersion(); want != "" && h.Version != want {
		return &hostVersionMismatchError{Name: name, Have: h.Version, Want: want}
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
	h, _, err := p.resolve(name)
	if err != nil {
		return "", err
	}
	return h.Workspace, nil
}

func (p hostPlacer) Materialize(ctx context.Context, name string, a agent.Agent, worktreePath string, hook bool) error {
	if name == "" {
		return a.MaterializeAs(worktreePath, binIf(hook, agent.SelfBin()))
	}
	h, rem, err := p.resolve(name)
	if err != nil {
		return err
	}
	payload, err := stageAgentTar(a, worktreePath, binIf(hook, h.Bin))
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
	h, _, err := p.resolve(name)
	if err != nil {
		return err
	}
	run := runnerFor(h)
	if _, captureErr := capture(ctx, run, "test", "-d", repo+"/.git"); captureErr == nil {
		return nil
	}
	origin, err := originOf(ctx, filepath.Join(p.st.WorkspaceRoot(), project))
	if err != nil {
		return fmt.Errorf("cannot clone %s onto %s: %w", project, name, err)
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
