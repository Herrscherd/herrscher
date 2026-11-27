package host

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	contracts "github.com/Herrscherd/herrscher-contracts"
	"github.com/Herrscherd/herrscher-contracts/i18n"
	"github.com/Herrscherd/herrscher/core/cli"
	"github.com/Herrscherd/herrscher/core/clitext"
	"github.com/Herrscherd/herrscher/core/config"
	"github.com/Herrscherd/herrscher/core/envx"
	"github.com/Herrscherd/herrscher/core/internal/health"
	"github.com/Herrscherd/herrscher/core/internal/obs"
	"github.com/Herrscherd/herrscher/core/internal/state"
	"github.com/Herrscherd/herrscher/core/keyproxy"
	"github.com/Herrscherd/herrscher/core/otelexport"
)

func startTelemetry(ctx context.Context, messages *hostMessages, lang i18n.Language, log *slog.Logger, off bool) (otelexport.Exporter, func()) {
	if off {
		log.Info("turn telemetry disabled", "reason", messages.text(lang, "telemetry.disabled_by_config", nil))
		return otelexport.Exporter{}, func() {}
	}
	telemetry, err := otelexport.Setup(ctx, os.LookupEnv)
	if err != nil {
		log.Warn("turn telemetry disabled", "reason", telemetryDisabledText(messages, lang, err), "err", err)
	}
	return telemetry, func() {
		if shutdownErr := telemetry.Shutdown(ctx); shutdownErr != nil {
			log.Warn("turn telemetry did not flush before exit", "err", shutdownErr)
		}
	}
}

func startTelemetryForConfig(ctx context.Context, messages *hostMessages, lang i18n.Language, log *slog.Logger, cfg config.Config) (otelexport.Exporter, func()) {
	return startTelemetry(ctx, messages, lang, log, cfg.TelemetryOff())
}

func telemetryDisabledText(messages *hostMessages, lang i18n.Language, err error) string {
	switch {
	case errors.Is(err, otelexport.ErrInvalidEndpoint):
		return messages.text(lang, "telemetry.endpoint_invalid", nil)
	case errors.Is(err, otelexport.ErrUnsupportedProtocol):
		return messages.text(lang, "telemetry.protocol_unsupported", nil)
	}
	return messages.text(lang, "telemetry.exporter_failed", nil)
}

// RunHub is the always-on multi-gateway daemon: it supervises one pure-runner
// bridge per persisted session, drives each session's turns over a control
// Acceptor (fanning events out to every bound gateway), and serves
// health/liveness. gws are the gateway sets the daemon owns (built from the
// registry by the caller). Command dispatch no longer runs here.
// Session/service commands run through the operator CLI (see NewRegistry).
func RunHub(ctx context.Context, cliMessages *clitext.Messages, hostMessages *Messages, gws []Deps, closers []contracts.Gateway, options Options) error {
	messages := hostMessages.owner
	clock, tick, settingsErr := schedulerSettings(messages)
	if settingsErr != nil {
		closeGateways(ctx, closers, operatorLogger(options.Warnings))
		return localizeHostError(messages, cliMessages, options.operatorLanguage(), settingsErr)
	}
	serveErr := serveHub(ctx, messages, cliMessages, gws, closers, options, clock, tick)
	if settings, ok := errors.AsType[compactorSettingsError](serveErr); ok {
		return localizeCompactorSettings(messages, cliMessages, options.operatorLanguage(), settings)
	}
	return localizeHostError(messages, cliMessages, options.operatorLanguage(), serveErr)
}

func localizeCompactorSettings(messages *hostMessages, cliMessages *clitext.Messages, lang i18n.Language, settings compactorSettingsError) error {
	notice := cli.Localize(cliMessages, settings, lang).Error()
	text := messages.text(lang, "compaction.stack_named", map[string]any{nameParam: settings.stage, detailParam: notice})
	return localizedHostError{text: text, cause: settings}
}

func (options Options) withDefaultGateways(gws []Deps) Options {
	if options.DefaultGateways == nil {
		options.DefaultGateways = nonTerminalKinds(gws)
	}
	return options
}

func operatorLogger(warnings io.Writer) *slog.Logger {
	return obs.NewLogger(warningWriter(warnings), obs.ParseLevel(false, os.Getenv(obs.EnvLevel)))
}

func serveHub(ctx context.Context, messages *hostMessages, cliMessages *clitext.Messages, gws []Deps, closers []contracts.Gateway, options Options, clock func() time.Time, tick time.Duration) error {
	base := operatorLogger(options.Warnings)
	defer closeGateways(ctx, closers, base)
	if options.Runtime.Dir() == "" {
		return errNoRuntimeLayout
	}
	log := base.With("component", "serve")
	monitor := health.NewHealth(time.Now())

	store, err := loadHubState(options)
	if err != nil {
		return wrapHost(messages, err, "serve.load_state", nil)
	}

	self, execErr := os.Executable()
	logCleanup(execErr)
	startRemotePluginHosts(ctx, self, options.RemoteCategories, base)
	partDir := filepath.Dir(options.StatePath)
	sup := newHubSupervisor(ctx, self, options.Runtime, base, monitor, partDir, options.StatePath)

	instID, err := resolveInstanceID(messages, store, options.InstanceID, options.Owner, log)
	if err != nil {
		return wrapHost(messages, err, "serve.resolve_instance", nil)
	}
	releaseInstance, err := claimInstance(ctx, messages, options.Runtime, instID)
	if err != nil {
		return err
	}
	defer releaseInstance()

	options = options.withDefaultGateways(gws)
	approvals := newApprovalTable(options.Warnings).withRules(store).withMessages(messages, options.languageSource())
	sessions := newSessionTable(messages, cliMessages)
	options.health = &healthSource{}
	options.usage = newUsageSource(messages, partDir)
	stopUsageWatch := options.usage.startWatch(ctx)
	defer stopUsageWatch()
	policy, err := newCompactionPolicy(ctx, messages, options.languageSource(), options.Warnings, options.CompactPercent, options.CompactStack, options.ContextLimits)
	if err != nil {
		return wrapHost(messages, err, "serve.compaction", nil)
	}
	sessions.compaction = policy
	cfg, err := config.Load(config.DefaultPath())
	if err != nil {
		return wrapHost(messages, err, "serve.config", nil)
	}
	telemetry, stopTelemetry := startTelemetryForConfig(ctx, messages, options.operatorLanguage(), log, cfg)
	defer stopTelemetry()
	sessions.telemetry = telemetry
	sessions.store = store
	if outboxErr := prepareTurnOutboxes(log, sessions, options.StatePath); outboxErr != nil {
		return outboxErr
	}
	reg, deps, err := buildRegistry(ctx, Deps{Admin: adminForHome(gws, store.Home)}, options, store, sup, instID, options.ContributedCommands, approvals, sessions)
	if err != nil {
		return wrapHost(messages, err, "serve.registry", nil)
	}
	kinds := contributedKinds(options.ContributedCommands)
	specs, specsErr := reg.Specs(options.Language)
	if specsErr != nil {
		log.Warn("command help texts not fully rendered", "err", specsErr)
	}
	selfTools := measuredSelfTools(reg, loadedConfig(options.ConfigPath), envx.Getenv)
	measured := selfTools()
	sup.SetBridgeEnv(hubBridgeEnv(specs, kinds, measured.TurnBytes(), measured.Exposure))
	sup.SetLocalPrepare(selfToolsPreparer(selfToolsProvisioner(envx.Getenv, loadedConfig(options.ConfigPath)), log))
	ensureCodexAgent(deps.agents)
	if ta := terminalAdmin(gws); ta != nil {
		deps.handler.SetTerminalAdmin(ta)
	}
	deps.handler.SetGatewayAdmins(gatewayAdmins(gws))
	sessions.outboundMaxBytes = cfg.EffectiveOutboundMaxBytes()
	sessions.fileSenders = fileSendersOf(closers)
	if fallback := accountFallbackFor(cfg, options.usage, gws); fallback != nil {
		deps.handler.SetAccountFallback(fallback)
	}
	keys, err := keyproxy.Open(ctx, cfg.Keys, keyproxy.OSLoader(), log)
	if err != nil && !errors.Is(err, keyproxy.ErrDisabled) {
		return wrapHost(messages, err, "serve.key_proxy", nil)
	}
	defer CloseKeyProxy(ctx, log, keys)
	inputs := newHealthInputs(messages, options.Runtime, instID, keys, gws, policy, partDir, forgeCredentials(options.Forge))
	inputs.selfTools = selfTools
	inputs.selfToolsAnswer = askSelfToolsServer
	options.health.bind(inputs)
	runningHub := newHub(store, sup, gws, partDir, reg, monitor.Metrics(), approvals, sessions, withWarnings(options.Warnings), withForge(options.Forge))
	runningHub.lang = options.Language
	runningHub.language = options.languageSource()
	runningHub.models = options.modelDiscovery(messages)
	runningHub.seedBackend = func(seedCtx context.Context, sess state.Session) (contracts.Backend, error) {
		return newSeedBackendWithToken(seedCtx, messages, sess, keys, runningHub.models, sessions.seedToken(seedCtx, sess.Name, liveSessionAllowed(store)))
	}
	runningHub.desk = newApprovalDesk(messages, options.languageSource(), options.Warnings, store, deps.agents, approvals, sessions)
	runningHub.selfRestarts = newSelfRestarts(store)
	deps.notices.bind(func(session, key string, params map[string]any) {
		runningHub.announce(session, key, params)
	})
	runningHub.contributedKinds = kinds
	runningHub.instanceID = instID
	runningHub.runtime = options.Runtime
	runningHub.gate = deps.handler.BudgetGate()
	coord := wireCoordination(runningHub, deps, store, sessions)

	deps.handler.SetCoordinationReader(coordViewAdapter{c: coord})

	sockets, err := serveHubSockets(ctx, options.Runtime, instID, runningHub)
	if err != nil {
		return err
	}
	liveStopped := make(chan struct{})
	go func() {
		defer close(liveStopped)
		<-ctx.Done()
		runningHub.stopLive()
	}()
	sched := newScheduler(store, store, runningHub, sessions.Seed, clock, tick)
	deps.schedSlot.sched = sched
	go sched.Run(ctx)
	prWatcher := newSessionPRWatcher(sessions.cliMessages, options.languageSource(), store, GitHubOpener(options.Forge), forgeCredentials(options.Forge), sessions.Seed)
	prWatchStopped := make(chan struct{})
	go func() {
		defer close(prWatchStopped)
		prWatcher.Run(ctx)
	}()
	runningHub.startReconcile(ctx)
	resumesStopped := make(chan struct{})
	go func() {
		defer close(resumesStopped)
		resumeAfterSelfRestart(ctx, store, sessions.Seed, func(session string) string {
			return messages.text(runningHub.sessionLanguageSource(session)(), "selftools.restart_resumed", nil)
		}, log)
	}()
	monitor.SetSessions(len(store.SnapshotSessions()))
	bindSessionControl(gws, runningHub)

	if instID != "" {
		log.Info("instance resolved", "instance", instID)
	}
	logCompiledPlugins(log)
	startHubProbes(ctx, messages, gws, options, store, monitor, instID)

	log.Info("hub up; supervising sessions", "gateways", strings.Join(kindsOf(gws), ","))
	stopErr := awaitHubStop(ctx, runningHub, sup, liveStopped, sockets, log)
	<-prWatchStopped
	<-resumesStopped
	return stopErr
}
