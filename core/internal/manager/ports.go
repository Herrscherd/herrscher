package manager

import (
	"context"

	"github.com/Herrscherd/herrscher/core/internal/agent"
	"github.com/Herrscherd/herrscher/core/internal/state"
)

// agentStore owns durable companion agent homes. The Handler depends on this
// port (not the concrete *agent.Store) so routing stays testable, like every
// other dependency.
type agentStore interface {
	Create(spec agent.CreateSpec) (agent.Agent, error)
	Get(name string) (agent.Agent, bool)
	List() ([]agent.Agent, error)
	SetSoul(name, soul string) error
}

// channelAdmin is the subset of a channel-admin client the Handler needs (injected so routing is testable).
type channelAdmin interface {
	Kind(ctx context.Context, id string) (string, error)
	CreateUnder(ctx context.Context, parentID, name string) (channelID string, err error)
	ForumPost(ctx context.Context, forumID, name, content string) (channelID string, err error)
	Archive(ctx context.Context, id string) error
	Send(ctx context.Context, channelID, content string) error
	// ChannelRef renders a channel id as this platform's operator-facing
	// reference, so the manager never switches on home type to format it.
	ChannelRef(id string) string
}

// supervisor starts/stops the bridge process backing a session.
type supervisor interface {
	Start(s state.Session)
	Stop(name string) error
	Restart(s state.Session) error
}

// Worktrees owns per-session git worktree lifecycle. Create returns the worktree
// path ("" + nil error means "fall back to shared", e.g. not a git repo). The
// repo root is passed per call so one Worktreer serves every project.
type Worktrees interface {
	Create(repo, name, base string) (path string, err error)
	// PreExisting says whether Create would reuse a worktree rather than make
	// one, which is what tells a rollback whether the worktree is its to remove.
	PreExisting(repo, name string) bool
	Branch(name string) string
	Remove(repo, name string, force bool) error
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

// forges clones a remote repo into the workspace via gh/glab (see internal/forge),
// so a session can pick the repo its backend works on.
type forges interface {
	Clone(ctx context.Context, spec, workspace string) (projectDir string, err error)
}

// updater rebuilds the daemon from source and restarts its service. Build pulls
// (when pull is true) and recompiles, returning the new short version; Restart
// restarts the running service out-of-band so it survives the daemon being
// killed mid-restart. Both are injected so routing stays testable.
type updater interface {
	Build(ctx context.Context, pull bool) (version string, err error)
	Restart(ctx context.Context) error
}
