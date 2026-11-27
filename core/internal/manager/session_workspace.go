package manager

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"time"

	contracts "github.com/Herrscherd/herrscher-contracts"
	"github.com/Herrscherd/herrscher/core/clitext"
	"github.com/Herrscherd/herrscher/core/internal/approval"
	"github.com/Herrscherd/herrscher/core/internal/forge"
	"github.com/Herrscherd/herrscher/core/internal/state"
	"github.com/Herrscherd/herrscher/core/internal/worktree"
)

const cloneTimeout = 10 * time.Minute

type workspace struct {
	path    string
	reused  bool
	scratch bool
}

type placement struct {
	wt      Worktrees
	repo    string
	project string
	space   workspace
}

func (h *Handler) placeSession(ctx context.Context, input contracts.Input, req *createRequest) (placement, error) {
	worktrees, wtRoot, err := h.worktreesOn(req.hostName)
	if err != nil {
		return placement{}, err
	}
	project, err := h.resolveProject(ctx, input, req.hostName, wtRoot)
	if err != nil {
		return placement{}, err
	}
	repo := repoFor(wtRoot, project)
	if h.hs != nil {
		if projectErr := h.hs.EnsureProject(ctx, req.hostName, project, repo); projectErr != nil {
			return placement{}, projectErr
		}
	}
	space, err := h.ensureWorkspace(ctx, worktrees, repo, *req)
	if err != nil {
		return placement{}, err
	}
	if req.runtime.agent != "" {
		if provisionErr := h.provisionAgent(ctx, req, worktrees, repo, &space); provisionErr != nil {
			return placement{}, provisionErr
		}
	}
	return placement{wt: worktrees, repo: repo, project: project, space: space}, nil
}

func (h *Handler) rollbackPlacement(ctx context.Context, place placement, name string) {
	h.rollbackWorkspace(ctx, place.wt, place.repo, name, place.space)
}

func (h *Handler) resolveProject(ctx context.Context, input contracts.Input, hostName, wtRoot string) (string, error) {
	project, _ := input.Lookup("project")
	if wtRoot == "" {
		return project, nil
	}
	if spec, ok := input.Lookup("clone"); ok && spec != "" {
		if hostName != "" {
			return "", localize(h.messages, h.currentLanguage(), fmt.Sprintf("%v: %q", ErrCloneUnsupported, hostName), clitext.HostErrCloneUnsupported, clitext.Params{clitext.ParamHost: hostName}, ErrCloneUnsupported)
		}
		cctx, cancel := context.WithTimeout(ctx, cloneTimeout)
		dir, err := h.fg.Clone(cctx, spec, wtRoot)
		cancel()
		if err != nil {
			return "", h.cloneError(err)
		}
		project = filepath.Base(dir)
	}
	if project != "" && !projectRe.MatchString(project) {
		return "", &InvalidValueError{Field: "project", Value: project, Hint: "use a single name (no /, spaces, or ..)"}
	}
	return project, nil
}

func (h *Handler) cloneError(err error) error {
	switch {
	case errors.Is(err, forge.ErrRepoSpec):
		return h.localized(forge.ErrRepoSpec, clitext.HostErrRepoSpec)
	case errors.Is(err, forge.ErrConfigCount):
		return h.localized(forge.ErrConfigCount, clitext.HostErrConfigCount)
	case errors.Is(err, contracts.ErrForgeNotConnected):
		return localize(h.messages, h.currentLanguage(), "clone: "+err.Error(), clitext.HostErrForgeNotConnected, clitext.Params{}, err)
	}
	return h.labelled("clone", clitext.HostErrCloneFailed, err)
}

func (h *Handler) ensureWorkspace(ctx context.Context, worktrees Worktrees, repo string, req createRequest) (workspace, error) {
	if req.shared {
		return workspace{}, nil
	}
	if err := h.claimWorkspace(req, worktrees.Path(repo, req.name)); err != nil {
		return workspace{}, err
	}
	reused := worktrees.PreExisting(ctx, repo, req.name)
	path, err := worktrees.Create(ctx, repo, req.name, req.base)
	if err != nil {
		return workspace{}, h.labelled("worktree", clitext.HostErrWorktree, err)
	}
	return workspace{path: path, reused: reused}, nil
}

func (h *Handler) claimWorkspace(req createRequest, path string) error {
	sess, taken := h.folderHolder(req.name, worktree.Placement{Host: req.hostName, Path: path})
	if taken {
		english := fmt.Sprintf("session %q: %v: %s belongs to session %q", req.name, ErrWorkspaceTaken, path, sess.Name)
		params := clitext.Params{clitext.ParamName: strconv.Quote(req.name), clitext.ParamOwner: strconv.Quote(sess.Name), clitext.ParamPath: strconv.Quote(path)}
		return localize(h.messages, h.currentLanguage(), english, clitext.HostErrWorkspaceTaken, params, ErrWorkspaceTaken)
	}
	return nil
}

func (h *Handler) folderHolder(name string, place worktree.Placement) (state.Session, bool) {
	sessions := h.st.SnapshotSessions()
	for idx := range sessions {
		if sessions[idx].Name != name && place.Same(worktree.Placement{Host: sessions[idx].Host, Path: sessions[idx].Worktree}) {
			return sessions[idx], true
		}
	}
	return state.Session{}, false
}

func (h *Handler) provisionAgent(ctx context.Context, req *createRequest, worktrees Worktrees, repo string, space *workspace) error {
	agentName := req.runtime.agent
	if req.shared {
		return &SharedAgentError{Agent: agentName}
	}
	if space.path == "" {
		if err := h.claimWorkspace(*req, worktrees.ScratchPath(repo, req.name)); err != nil {
			return err
		}
		if _, statErr := os.Stat(worktrees.ScratchPath(repo, req.name)); statErr == nil {
			space.reused = true
		}
		path, err := worktrees.Scratch(repo, req.name)
		if err != nil {
			return localize(h.messages, h.currentLanguage(), fmt.Sprintf("provision agent %q: %v", agentName, err), clitext.HostErrProvisionAgent, clitext.Params{clitext.ParamAgent: agentName, clitext.ParamErr: err.Error()}, err)
		}
		space.path, space.scratch = path, true
	}
	agentSpec, found := h.agents.Get(agentName)
	if !found {
		h.rollbackWorkspace(ctx, worktrees, repo, req.name, *space)
		return &UnknownAgentError{Name: agentName}
	}
	if req.runtime.vendor == "" {
		req.runtime.vendor = agentSpec.Backend
	}
	if !req.runtime.explicit && agentSpec.Cmd != "" {
		req.runtime.cmd = agentSpec.Cmd
	}
	if err := h.materializeOn(ctx, req.hostName, agentSpec, space.path, req.mode != approval.ModeBypass.String()); err != nil {
		h.rollbackWorkspace(ctx, worktrees, repo, req.name, *space)
		return localize(h.messages, h.currentLanguage(), fmt.Sprintf("provision agent %q: %v", agentName, err), clitext.HostErrProvisionAgent, clitext.Params{clitext.ParamAgent: agentName, clitext.ParamErr: err.Error()}, err)
	}
	return nil
}

func (h *Handler) rollbackWorkspace(ctx context.Context, worktrees Worktrees, repo, name string, space workspace) {
	if space.reused {
		return
	}
	if space.scratch {
		if rmErr := worktrees.RemoveScratchAt(repo, space.path); rmErr != nil {
			h.log.Warn("scratch rollback failed", "session", name, "err", rmErr)
		}
		return
	}
	if rmErr := worktrees.RemoveAt(context.WithoutCancel(ctx), repo, space.path, true); rmErr != nil {
		h.log.Warn("worktree rollback failed", "session", name, "err", rmErr)
	}
}
