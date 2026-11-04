package host

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/Herrscherd/herrscher/core/internal/authz"
)

var (
	errRequestNameMissing          = errors.New("name a request (see `approve list`)")
	errApproveModeUsage            = errors.New("usage: approve mode <session> <ask|bypass|strict|smart>")
	errMemoryLocateUnsupported     = errors.New("memory backend does not support locate")
	errMemoryForgetUnsupported     = errors.New("memory backend does not support forget")
	errCommandSocketEmpty          = errors.New("command socket: empty response")
	errNoGatewayPlugin             = errors.New("no gateway plugin registered")
	errGatewayFactoryEmpty         = errors.New("factory returned no gateway")
	errHostNameMissing             = errors.New("a host needs a name")
	errLocalHostReserved           = errors.New("`local` is this machine and always exists: it cannot be registered")
	errNoMemoryPlugin              = errors.New("memory: no memory plugin registered")
	errPrincipalMissing            = errors.New("name a principal (see `role list`)")
	errPrincipalExample            = errors.New("name a principal: a refusal spells it, e.g. chat:1234")
	errLocalPrincipalRole          = errors.New("`" + authz.LocalPrincipal + "` is the daemon's own socket, and opening it already needs the daemon's uid: it is always `owner`")
	errSourceNotSet                = errors.New("no source set; run /set source <path> first")
	errWorktreeSubcommand          = errors.New("worktree: want one of create, pre-existing, remove, materialize")
	errWorktreeMaterializeRequired = errors.New("worktree materialize: --worktree is required")
	errSeedTimeout                 = errors.New("seed timeout")
	errNoBackendPlugin             = errors.New("no backend plugin registered")
	errHostNoneRegistered          = errors.New("name a host: none is registered yet, add one with `host add <name> --ssh <target> --workspace <path>`")
	errGatewayOnlyNoModel          = errors.New("the gateway-only route policy forbids a spawn with no model: it would run on this machine's own vendor login")
	errFanoutNoTasks               = errors.New("fanout: no tasks")
	errRouteEmptyTask              = errors.New("route: empty task")
	errRouteNoAgent                = errors.New("route: no agent matches task")
	errSealExpectedPositive        = errors.New("seal refused: expected must be > 0")
)

// NotFoundError reports that no entity of Kind is registered under Name.
type NotFoundError struct {
	Kind string
	Name string
}

func (e *NotFoundError) Error() string {
	return fmt.Sprintf("no %s named %q", e.Kind, e.Name)
}

// UnknownError reports that Name is not a known entity of Kind.
type UnknownError struct {
	Kind string
	Name string
}

func (e *UnknownError) Error() string {
	return fmt.Sprintf("unknown %s %q", e.Kind, e.Name)
}

type requestNotWaitingError struct {
	ID string
}

func (e *requestNotWaitingError) Error() string {
	return fmt.Sprintf("no request %q is waiting: it may have been answered, or timed out", e.ID)
}

type noSuchRuleError struct {
	Rule string
}

func (e *noSuchRuleError) Error() string {
	return "no such rule: " + e.Rule
}

type unknownModeError struct {
	Mode string
}

func (e *unknownModeError) Error() string {
	return fmt.Sprintf("unknown mode %q: ask, accept-edits, plan, auto, bypass or strict", e.Mode)
}

type accessDeniedError struct {
	Reason string
}

func (e *accessDeniedError) Error() string {
	return e.Reason
}

type modelNotRedirectableError struct {
	Model   string
	Backend string
}

func (e *modelNotRedirectableError) Error() string {
	return fmt.Sprintf("model %q is routed through the gateway but backend %q cannot be redirected", e.Model, e.Backend)
}

type duplicateModelError struct {
	ID     string
	First  string
	Second string
}

func (e *duplicateModelError) Error() string {
	return fmt.Sprintf("model ID %q is declared by both %q and %q", e.ID, e.First, e.Second)
}

type unknownModelError struct {
	Model  string
	Policy string
}

func (e *unknownModelError) Error() string {
	return fmt.Sprintf("unknown model %q (not offered under route policy %q)", e.Model, e.Policy)
}

type noTurnInFlightError struct {
	Session string
}

func (e *noTurnInFlightError) Error() string {
	return fmt.Sprintf("session %q has no turn in flight", e.Session)
}

type commandSocketError struct {
	Message string
}

func (e *commandSocketError) Error() string {
	return e.Message
}

type contributionClashError struct {
	Kind string
	Path string
}

func (e *contributionClashError) Error() string {
	return fmt.Sprintf("plugin %q contributes %q twice", e.Kind, e.Path)
}

type daemonNotRunningError struct {
	Socket string
}

func (e *daemonNotRunningError) Error() string {
	return "no herrscher daemon is listening on " + e.Socket
}

type daemonRequiredError struct {
	Command string
	Target  string
}

func (e *daemonRequiredError) Error() string {
	return fmt.Sprintf("%#q needs a running herrscher daemon, and none answered on %s", e.Command, e.Target)
}

type noGatewayError struct {
	Reasons string
}

func (e *noGatewayError) Error() string {
	return "no gateway available: " + e.Reasons
}

type modelBackendMismatchError struct {
	Model     string
	Owner     string
	Requested string
}

func (e *modelBackendMismatchError) Error() string {
	return fmt.Sprintf("model %q belongs to backend %q, but backend %q was requested; pick a model offered by %q or switch the vendor", e.Model, e.Owner, e.Requested, e.Requested)
}

type noRoleError struct {
	Principal string
}

func (e *noRoleError) Error() string {
	return e.Principal + " holds no role"
}

type sessionRoleError struct {
	Principal string
}

func (e *sessionRoleError) Error() string {
	return fmt.Sprintf("%#q is a session, and a session is always `agent`: the socket it arrives on decides that, not this table", e.Principal)
}

type roleRequiredError struct {
	Grantable []string
}

func (e *roleRequiredError) Error() string {
	return "name a role: one of " + strings.Join(e.Grantable, ", ")
}

type unknownRoleError struct {
	Role      string
	Grantable []string
}

func (e *unknownRoleError) Error() string {
	return fmt.Sprintf("unknown role %q: grant one of %s", e.Role, strings.Join(e.Grantable, ", "))
}

type scheduleExistsError struct {
	Name string
}

func (e *scheduleExistsError) Error() string {
	return fmt.Sprintf("a schedule named %q already exists", e.Name)
}

type scheduleBusyError struct {
	Name string
}

func (e *scheduleBusyError) Error() string {
	return fmt.Sprintf("schedule %q already has a turn in flight", e.Name)
}

type noDaemonToFireError struct {
	Name string
}

func (e *noDaemonToFireError) Error() string {
	return fmt.Sprintf("no running daemon to fire %q: `schedule run` needs `herrscher serve`", e.Name)
}

type taskNotTakenError struct {
	Schedule string
}

func (e *taskNotTakenError) Error() string {
	return fmt.Sprintf("schedule %q: no session took the task, see the daemon log", e.Schedule)
}

type invalidDurationError struct {
	Source string
	Value  string
}

func (e *invalidDurationError) Error() string {
	return fmt.Sprintf("%s %q is not a duration (try 90s or 30m)", e.Source, e.Value)
}

type nonPositiveDurationError struct {
	Source   string
	Duration time.Duration
}

func (e *nonPositiveDurationError) Error() string {
	return fmt.Sprintf("%s must be positive, got %s", e.Source, e.Duration)
}

type worktreeFlagsError struct {
	Sub string
}

func (e *worktreeFlagsError) Error() string {
	return fmt.Sprintf("worktree %s: --repo and --name are required", e.Sub)
}

type unknownWorktreeSubcommandError struct {
	Sub string
}

func (e *unknownWorktreeSubcommandError) Error() string {
	return fmt.Sprintf("worktree: unknown subcommand %q (want create, pre-existing, remove or materialize)", e.Sub)
}

type uncommittedError struct {
	Role string
	Name string
}

func (e *uncommittedError) Error() string {
	return fmt.Sprintf("%s %q has uncommitted changes; commit first", e.Role, e.Name)
}

type noIsolatedWorktreeError struct {
	Session string
}

func (e *noIsolatedWorktreeError) Error() string {
	return fmt.Sprintf("session %q has no isolated worktree to continue", e.Session)
}

type freeNameExhaustedError struct {
	Base  string
	Tries int
}

func (e *freeNameExhaustedError) Error() string {
	return fmt.Sprintf("no free session name for %q after %d tries", e.Base, e.Tries)
}

type seedTimedOutError struct {
	Session string
}

func (e *seedTimedOutError) Error() string {
	return fmt.Sprintf("session %q created but seeding timed out; rolled back", e.Session)
}

type noParentError struct {
	Session string
}

func (e *noParentError) Error() string {
	return fmt.Sprintf("session %q has no parent; nothing to deliver", e.Session)
}

type deliveryTimeoutError struct {
	Role string
	Name string
}

func (e *deliveryTimeoutError) Error() string {
	return fmt.Sprintf("delivery to %s %q timed out", e.Role, e.Name)
}

type notAWorkerError struct {
	Worker string
	Lead   string
}

func (e *notAWorkerError) Error() string {
	return fmt.Sprintf("%q is not a worker of %q", e.Worker, e.Lead)
}

type sealBelowCohortError struct {
	Expected int
	Cohort   int
}

func (e *sealBelowCohortError) Error() string {
	return fmt.Sprintf("expected %d below current cohort size %d", e.Expected, e.Cohort)
}
