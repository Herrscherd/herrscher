package host

import (
	"context"
	"errors"
	"strings"

	contracts "github.com/Herrscherd/herrscher-contracts"
	"github.com/Herrscherd/herrscher-contracts/i18n"
	"github.com/Herrscherd/herrscher/core/cli"
	"github.com/Herrscherd/herrscher/core/clitext"
	"github.com/Herrscherd/herrscher/core/discovery"
	"github.com/Herrscherd/herrscher/core/internal/filelock"
	"github.com/Herrscherd/herrscher/core/internal/state"
	"github.com/Herrscherd/herrscher/core/internal/supervisor"
	"github.com/Herrscherd/herrscher/core/usage"
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
		{errHistoryTimeout, "cli.host.fault.history_timeout"},
		{errTooManyEdits, "cli.host.fault.too_many_edits"},
		{errHealthNotReady, "cli.host.fault.health_not_ready"},
		{errNoCompactorFactory, "cli.host.fault.no_compactor_factory"},
		{errNilCompactor, "cli.host.fault.nil_compactor"},
		{errNoCompactorPlugin, "cli.host.fault.no_compactor_plugin"},
		{errCompactQueueFull, "cli.host.fault.compact_queue_full"},
		{errWarningAbandoned, "cli.host.fault.warning_abandoned"},
		{errWarningLate, "cli.host.fault.warning_late"},
		{errRequestNameMissing, "cli.host.fault.request_name_missing"},
		{errApproveModeUsage, "cli.host.fault.approve_mode_usage"},
		{errApproveAskUnauthenticated, "cli.host.fault.approve_ask_unauthenticated"},
		{errSeedWithoutApprovalDesk, "cli.host.fault.seed_without_approval_desk"},
		{errMemoryLocateUnsupported, "cli.host.fault.memory_locate_unsupported"},
		{errMemoryForgetUnsupported, "cli.host.fault.memory_forget_unsupported"},
		{errCommandSocketEmpty, "cli.host.fault.command_socket_empty"},
		{errDaemonRequired, "cli.host.fault.daemon_required"},
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
		{errSeedArchivedSession, "cli.host.fault.seed_archived_session"},
		{errNoBackendPlugin, "cli.host.fault.no_backend_plugin"},
		{errHostNoneRegistered, "cli.host.fault.host_none_registered"},
		{errGatewayOnlyNoModel, "cli.host.fault.gateway_only_no_model"},
		{errFanoutNoTasks, "cli.host.fault.fanout_no_tasks"},
		{errRouteEmptyTask, "cli.host.fault.route_empty_task"},
		{errRouteNoAgent, "cli.host.fault.route_no_agent"},
		{errSealExpectedPositive, "cli.host.fault.seal_expected_positive"},
		{errDiscoveryNoAnswer, "cli.host.fault.discovery_no_answer"},
		{errCacheLockBusy, "cli.host.fault.cache_lock_busy"},
		{filelock.ErrNotOwned, "cli.host.fault.lock_not_owned"},
		{filelock.ErrSymlink, "cli.host.fault.lock_symlink"},
		{errNoPendingChoice, "cli.host.fault.no_pending_choice"},
		{errHandoffTargets, "cli.host.fault.handoff_targets"},
		{errHandoffAgentTargets, "cli.host.fault.handoff_agent_targets"},
		{errHandoffNoCreate, "cli.host.fault.handoff_no_create"},
		{errHandoffNoSource, "cli.host.fault.handoff_no_source"},
		{errHandoffBusy, "cli.host.fault.handoff_busy"},
		{errHandoffQuota, "cli.host.fault.handoff_quota"},
		{errShellRefused, "cli.host.fault.shell_refused"},
		{errDaemonEventsUnreachable, "cli.host.fault.daemon_events_unreachable"},
		{errSocketTaken, "cli.host.fault.socket_taken"},
		{errInstanceLocked, "cli.host.fault.instance_locked"},
		{errNoRuntimeLayout, "cli.host.fault.no_runtime_dir"},
		{errHubClosed, "cli.host.fault.hub_closed"},
		{supervisor.ErrStillStopping, "cli.host.fault.still_stopping"},
		{errEmptyShellCommand, "cli.host.fault.empty_shell_command"},
		{errUnknownShellSession, "cli.host.fault.unknown_shell_session"},
		{errRemoteShellSession, "cli.host.fault.remote_shell_session"},
		{errUnsupportedLang, "cli.host.fault.unsupported_lang"},
		{usage.ErrEmptyValue, "cli.host.fault.usage_empty_value"},
		{errEnvKey, "cli.host.fault.env_key"},
		{errEnvValue, "cli.host.fault.env_value"},
		{errEnvNoFile, "cli.host.fault.env_no_file"},
		{errEnvNoSuchKey, "cli.host.fault.env_no_such_key"},
		{errSecretSession, "cli.host.fault.secret_from_session"},
		{errEnvRequestOutside, "cli.host.fault.env_request_outside"},
		{errEnvRequestUnheard, "cli.host.fault.env_request_unheard"},
		{errEnvQuote, "cli.host.fault.env_quote"},
		{errSelfToolsSilent, "cli.host.fault.self_tools_silent"},
		{discovery.ErrWindowOutOfRange, "cli.host.fault.announce_window"},
		{discovery.ErrFingerprintInvalid, "cli.host.fault.fingerprint_invalid"},
		{discovery.ErrNoAddress, "cli.host.fault.announce_no_address"},
		{discovery.ErrTailscaleStatus, "cli.host.fault.tailscale_status"},
	}
}

func localizeHostError(messages *hostMessages, cliMessages *clitext.Messages, lang i18n.Language, err error) error {
	if err == nil {
		return nil
	}
	resolved := i18n.Resolve(lang)
	text := errorTextIn(messages, resolved, err)
	isLocal := isLocalized(err)
	translated := false
	for _, known := range hostErrorKeys() {
		if !errors.Is(err, known.sentinel) {
			continue
		}
		phrase, textErr := cliMessages.Text(resolved, clitext.Key(known.key), nil)
		if textErr != nil {
			continue
		}
		text = strings.Replace(text, known.sentinel.Error(), phrase, 1)
		translated = true
	}
	if !translated && !isLocal {
		return err
	}
	return localizedHostError{text: text, cause: err}
}

func localizeCommandErrors(messages *hostMessages, cliMessages *clitext.Messages, reg *cli.Registry, store *state.State, live *operatorLanguage) {
	reg.Intercept(nil, func(_ contracts.Cmd, next cli.Runner) cli.Runner {
		return func(ctx context.Context, input contracts.Input) (contracts.Result, error) {
			res, err := next(ctx, input)
			return res, localizeHostError(messages, cliMessages, commandErrorLanguage(store, live.Get(), input), err)
		}
	})
}

func commandErrorLanguage(store *state.State, operator i18n.Language, input contracts.Input) i18n.Language {
	name := input.Get(paramName)
	if name == "" {
		return operator
	}
	return namedSessionLanguage(store, name, operator)
}
