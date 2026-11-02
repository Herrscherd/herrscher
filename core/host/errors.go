package host

import (
	"errors"
	"fmt"
	"time"
)

var (
	ErrRequestNameMissing          = errors.New("name a request (see `approve list`)")
	ErrApproveModeUsage            = errors.New("usage: approve mode <session> <ask|bypass|strict>")
	ErrMemoryLocateUnsupported     = errors.New("memory backend does not support locate")
	ErrMemoryForgetUnsupported     = errors.New("memory backend does not support forget")
	ErrCommandSocketEmpty          = errors.New("command socket: empty response")
	ErrNoGatewayPlugin             = errors.New("no gateway plugin registered")
	ErrGatewayFactoryEmpty         = errors.New("factory returned no gateway")
	ErrHostNameMissing             = errors.New("a host needs a name")
	ErrLocalHostReserved           = errors.New("`local` is this machine and always exists: it cannot be registered")
	ErrNoMemoryPlugin              = errors.New("memory: no memory plugin registered")
	ErrPrincipalMissing            = errors.New("name a principal (see `role list`)")
	ErrSourceNotSet                = errors.New("no source set; run /set source <path> first")
	ErrWorktreeSubcommand          = errors.New("worktree: want one of create, pre-existing, remove, materialize")
	ErrWorktreeMaterializeRequired = errors.New("worktree materialize: --worktree is required")
	ErrSeedTimeout                 = errors.New("seed timeout")
	ErrNoBackendPlugin             = errors.New("no backend plugin registered")
)

type NotFoundError struct {
	Kind string
	Name string
}

func (e *NotFoundError) Error() string {
	return fmt.Sprintf("no %s named %q", e.Kind, e.Name)
}

type RequestNotWaitingError struct {
	ID string
}

func (e *RequestNotWaitingError) Error() string {
	return fmt.Sprintf("no request %q is waiting: it may have been answered, or timed out", e.ID)
}

type NoSuchRuleError struct {
	Rule string
}

func (e *NoSuchRuleError) Error() string {
	return fmt.Sprintf("no such rule: %s", e.Rule)
}

type UnknownModeError struct {
	Mode string
}

func (e *UnknownModeError) Error() string {
	return fmt.Sprintf("unknown mode %q: ask, bypass or strict", e.Mode)
}

type ModelNotRedirectableError struct {
	Model   string
	Backend string
}

func (e *ModelNotRedirectableError) Error() string {
	return fmt.Sprintf("model %q is routed through the gateway but backend %q cannot be redirected", e.Model, e.Backend)
}

type DuplicateModelError struct {
	ID     string
	First  string
	Second string
}

func (e *DuplicateModelError) Error() string {
	return fmt.Sprintf("model ID %q is declared by both %q and %q", e.ID, e.First, e.Second)
}

type UnknownModelError struct {
	Model  string
	Policy string
}

func (e *UnknownModelError) Error() string {
	return fmt.Sprintf("unknown model %q (not offered under route policy %q)", e.Model, e.Policy)
}

type NoTurnInFlightError struct {
	Session string
}

func (e *NoTurnInFlightError) Error() string {
	return fmt.Sprintf("session %q has no turn in flight", e.Session)
}

type UnknownMemoryKindError struct {
	Kind string
}

func (e *UnknownMemoryKindError) Error() string {
	return fmt.Sprintf("memory record: unknown kind %q", e.Kind)
}

type CommandSocketError struct {
	Message string
}

func (e *CommandSocketError) Error() string {
	return e.Message
}

type ContributionClashError struct {
	Kind string
	Path string
}

func (e *ContributionClashError) Error() string {
	return fmt.Sprintf("plugin %q contributes %q twice", e.Kind, e.Path)
}

type DaemonNotRunningError struct {
	Socket string
}

func (e *DaemonNotRunningError) Error() string {
	return fmt.Sprintf("no herrscher daemon is listening on %s", e.Socket)
}

type NoGatewayError struct {
	Reasons string
}

func (e *NoGatewayError) Error() string {
	return fmt.Sprintf("no gateway available: %s", e.Reasons)
}

type HostNeedsSSHError struct {
	Name string
}

func (e *HostNeedsSSHError) Error() string {
	return fmt.Sprintf("host %q needs an ssh target, e.g. --ssh me@%s", e.Name, e.Name)
}

type HostWorkspaceError struct {
	Name      string
	Workspace string
}

func (e *HostWorkspaceError) Error() string {
	return fmt.Sprintf("host %q needs an absolute workspace path, got %q", e.Name, e.Workspace)
}

type HostExistsError struct {
	Name string
}

func (e *HostExistsError) Error() string {
	return fmt.Sprintf("a host named %q already exists (remove it first, or run `host provision %s`)", e.Name, e.Name)
}

type HostNotProvisionedError struct {
	Name string
}

func (e *HostNotProvisionedError) Error() string {
	return fmt.Sprintf("host %q is not provisioned: run `host provision %s`", e.Name, e.Name)
}

type NoRoleError struct {
	Principal string
}

func (e *NoRoleError) Error() string {
	return fmt.Sprintf("%s holds no role", e.Principal)
}

type UnknownAgentError struct {
	Agent string
}

func (e *UnknownAgentError) Error() string {
	return fmt.Sprintf("unknown agent %q", e.Agent)
}

type ScheduleExistsError struct {
	Name string
}

func (e *ScheduleExistsError) Error() string {
	return fmt.Sprintf("a schedule named %q already exists", e.Name)
}

type ScheduleBusyError struct {
	Name string
}

func (e *ScheduleBusyError) Error() string {
	return fmt.Sprintf("schedule %q already has a turn in flight", e.Name)
}

type InvalidDurationError struct {
	Source string
	Value  string
}

func (e *InvalidDurationError) Error() string {
	return fmt.Sprintf("%s %q is not a duration (try 90s or 30m)", e.Source, e.Value)
}

type NonPositiveDurationError struct {
	Source   string
	Duration time.Duration
}

func (e *NonPositiveDurationError) Error() string {
	return fmt.Sprintf("%s must be positive, got %s", e.Source, e.Duration)
}

type WorktreeFlagsError struct {
	Sub string
}

func (e *WorktreeFlagsError) Error() string {
	return fmt.Sprintf("worktree %s: --repo and --name are required", e.Sub)
}

type UnknownWorktreeSubcommandError struct {
	Sub string
}

func (e *UnknownWorktreeSubcommandError) Error() string {
	return fmt.Sprintf("worktree: unknown subcommand %q (want create, pre-existing, remove or materialize)", e.Sub)
}

type UnknownBackendError struct {
	Backend string
}

func (e *UnknownBackendError) Error() string {
	return fmt.Sprintf("unknown backend %q", e.Backend)
}
