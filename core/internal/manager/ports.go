package manager

import (
	"context"

	contracts "github.com/Herrscherd/herrscher-contracts"
	"github.com/Herrscherd/herrscher-contracts/i18n"
	"github.com/Herrscherd/herrscher/core/internal/agent"
	"github.com/Herrscherd/herrscher/core/internal/state"
	"github.com/Herrscherd/herrscher/core/usage"
)

type agentStore interface {
	Create(spec agent.CreateSpec) (agent.Agent, error)
	Get(name string) (agent.Agent, bool)
	List() ([]agent.Agent, error)
	SetSoul(name, soul string) error
}

type channelAdmin interface {
	Kind(ctx context.Context, id string) (string, error)
	CreateUnder(ctx context.Context, parentID, name string) (channelID string, err error)
	ForumPost(ctx context.Context, forumID, name, content string) (channelID string, err error)
	Archive(ctx context.Context, id string) error
	Send(ctx context.Context, channelID, content string) error
	ChannelRef(id string) string
}

type supervisor interface {
	Start(s state.Session)
	Stop(name string) error
	Restart(s state.Session) error
}

// Worktrees owns per-session git worktree lifecycle. Create returns the worktree
// path ("" + nil error means "fall back to shared", e.g. not a git repo). The
// repo root is passed per call so one Worktreer serves every project.
type Worktrees interface {
	Create(ctx context.Context, repo, name, base string) (path string, err error)
	// PreExisting says whether Create would reuse a worktree rather than make
	// one, which is what tells a rollback whether the worktree is its to remove.
	PreExisting(ctx context.Context, repo, name string) bool
	Branch(name string) string
	Remove(ctx context.Context, repo, name string, force bool) error
	Scratch(root, name string) (path string, err error)
	ScratchPath(root, name string) string
	RemoveScratch(root, name string) error
}

// Hosts resolves where a session's process runs. The manager needs four things
// from a host: whether it can carry a new session, the worktree lifecycle over
// there, the repository root over there, and a way to put an agent's files in a
// worktree that is not here. All of them are ports, so a local session keeps
// calling the local Worktreer directly and pays no process spawn for a path it
// shares with nothing.
//
// Exported, unlike its neighbours, because the composition root implements it:
// it is the only piece that needs both the host records and a runner, and
// neither belongs in this package.
type Hosts interface {
	// Ready refuses a host that cannot carry a NEW session, version drift
	// included. It is asked at create time only: an existing session must stay
	// closable on a host this daemon would no longer start one on, and removing
	// a worktree over there needs no agreement on versions.
	Ready(name string) error
	// Worktrees returns the worktree implementation for a host name. An empty
	// name is this machine.
	Worktrees(name string) (Worktrees, error)
	// Workspace returns the workspace root on a host. An empty name gives the
	// daemon's own.
	Workspace(name string) (string, error)
	// Materialize provisions an agent into a worktree on a host. hook says
	// whether the session's settings must carry the approval hook, which each
	// side renders with its own herrscher binary.
	Materialize(ctx context.Context, name string, a agent.Agent, worktreePath string, hook bool) error
	// EnsureProject makes sure repo exists on the host, cloning it from origin
	// when it does not. An empty host name is a no-op: the daemon's own
	// workspace is already where it is.
	EnsureProject(ctx context.Context, name, project, repo string) error
}

type forges interface {
	Clone(ctx context.Context, spec, workspace string) (projectDir string, err error)
}

type updater interface {
	Build(ctx context.Context, pull bool) (version string, err error)
	Restart(ctx context.Context) error
}

type accountFallback interface {
	Balance(ctx context.Context, vendor, planned string) usage.Choice
	Notify(ctx context.Context, home state.HomeRef, channelID string, lang i18n.Language, notice contracts.Notice)
}
