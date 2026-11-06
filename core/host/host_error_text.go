package host

import (
	"context"
	"errors"
	"strings"

	contracts "github.com/Herrscherd/herrscher-contracts"
	"github.com/Herrscherd/herrscher-contracts/i18n"
	"github.com/Herrscherd/herrscher/core/cli"
	"github.com/Herrscherd/herrscher/core/clitext"
)

type localizedHostError struct {
	text  string
	cause error
}

func (e localizedHostError) Error() string { return e.text }

func (e localizedHostError) Unwrap() error { return e.cause }

type hostErrorKey struct {
	sentinel error
	key      string
}

func hostErrorKeys() []hostErrorKey {
	return []hostErrorKey{
		{errConversationAccount, "cli.host.fault.conversation_account"},
		{errConversationFork, "cli.host.fault.conversation_fork"},
		{errConversationRestore, "cli.host.fault.conversation_restore"},
		{errScopeConflict, "cli.host.fault.scope_conflict"},
		{errNoRiskModel, "cli.host.fault.no_risk_model"},
		{errRiskUnreadable, "cli.host.fault.risk_unreadable"},
		{errWhatInvalid, "cli.host.fault.what_invalid"},
		{errRewindNeedsDaemon, "cli.host.fault.rewind_needs_daemon"},
		{errForkNeedsDaemon, "cli.host.fault.fork_needs_daemon"},
		{errTurnInFlight, "cli.host.fault.turn_in_flight"},
		{errCheckpointNumber, "cli.host.fault.checkpoint_number"},
		{errNotCheckpointable, "cli.host.fault.not_checkpointable"},
		{errForkNoWorktree, "cli.host.fault.fork_no_worktree"},
		{errCompactUnsupported, "cli.host.fault.compact_unsupported"},
		{errBridgeGone, "cli.host.fault.bridge_gone"},
		{errNoBackendFactory, "cli.host.fault.no_backend_factory"},
		{errNotCompactorPort, "cli.host.fault.not_compactor_port"},
		{errNoCompactorPlugin, "cli.host.fault.no_compactor_plugin"},
		{errCompactQueueFull, "cli.host.fault.compact_queue_full"},
		{errRequestNameMissing, "cli.host.fault.request_name_missing"},
		{errApproveModeUsage, "cli.host.fault.approve_mode_usage"},
		{errMemoryLocateUnsupported, "cli.host.fault.memory_locate_unsupported"},
		{errMemoryForgetUnsupported, "cli.host.fault.memory_forget_unsupported"},
		{errCommandSocketEmpty, "cli.host.fault.command_socket_empty"},
		{errNoGatewayPlugin, "cli.host.fault.no_gateway_plugin"},
		{errGatewayFactoryEmpty, "cli.host.fault.gateway_factory_empty"},
		{errHostNameMissing, "cli.host.fault.host_name_missing"},
		{errLocalHostReserved, "cli.host.fault.local_host_reserved"},
		{errNoMemoryPlugin, "cli.host.fault.no_memory_plugin"},
		{errPrincipalMissing, "cli.host.fault.principal_missing"},
		{errPrincipalExample, "cli.host.fault.principal_example"},
		{errLocalPrincipalRole, "cli.host.fault.local_principal_role"},
		{errSourceNotSet, "cli.host.fault.source_not_set"},
		{errWorktreeSubcommand, "cli.host.fault.worktree_subcommand"},
		{errWorktreeMaterializeRequired, "cli.host.fault.worktree_materialize_required"},
		{errSeedTimeout, "cli.host.fault.seed_timeout"},
		{errNoBackendPlugin, "cli.host.fault.no_backend_plugin"},
		{errHostNoneRegistered, "cli.host.fault.host_none_registered"},
		{errGatewayOnlyNoModel, "cli.host.fault.gateway_only_no_model"},
		{errFanoutNoTasks, "cli.host.fault.fanout_no_tasks"},
		{errRouteEmptyTask, "cli.host.fault.route_empty_task"},
		{errRouteNoAgent, "cli.host.fault.route_no_agent"},
		{errSealExpectedPositive, "cli.host.fault.seal_expected_positive"},
		{errDiscoveryNoAnswer, "cli.host.fault.discovery_no_answer"},
		{errCacheLockBusy, "cli.host.fault.cache_lock_busy"},
		{errNoPendingChoice, "cli.host.fault.no_pending_choice"},
		{errHandoffTargets, "cli.host.fault.handoff_targets"},
		{errHandoffAgentTargets, "cli.host.fault.handoff_agent_targets"},
		{errHandoffNoCreate, "cli.host.fault.handoff_no_create"},
		{errHandoffNoSource, "cli.host.fault.handoff_no_source"},
		{errHandoffBusy, "cli.host.fault.handoff_busy"},
		{errHandoffQuota, "cli.host.fault.handoff_quota"},
		{errShellRefused, "cli.host.fault.shell_refused"},
		{errEmptyShellCommand, "cli.host.fault.empty_shell_command"},
		{errUnknownShellSession, "cli.host.fault.unknown_shell_session"},
		{errRemoteShellSession, "cli.host.fault.remote_shell_session"},
	}
}

func localizeHostError(lang i18n.Language, err error) error {
	if err == nil {
		return nil
	}
	for _, known := range hostErrorKeys() {
		if !errors.Is(err, known.sentinel) {
			continue
		}
		text, textErr := clitext.Text(i18n.Resolve(lang), clitext.Key(known.key), nil)
		if textErr != nil {
			return err
		}
		return localizedHostError{text: strings.Replace(err.Error(), known.sentinel.Error(), text, 1), cause: err}
	}
	return err
}

func localizeCommandErrors(reg *cli.Registry, live *operatorLanguage) {
	reg.Intercept(nil, func(_ contracts.Cmd, next cli.Runner) cli.Runner {
		return func(ctx context.Context, in contracts.Input) (contracts.Result, error) {
			res, err := next(ctx, in)
			return res, localizeHostError(live.Get(), err)
		}
	})
}
