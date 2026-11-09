package host

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"runtime/debug"
	"slices"
	"strings"
	"time"

	contracts "github.com/Herrscherd/herrscher-contracts"

	"github.com/Herrscherd/herrscher/core/accounts"
	"github.com/Herrscherd/herrscher/core/envx"
	"github.com/Herrscherd/herrscher/core/health"
	"github.com/Herrscherd/herrscher/core/keyproxy"
	"github.com/Herrscherd/herrscher/core/usage"
)

type healthInputs struct {
	daemon     health.Daemon
	keys       *keyproxy.Proxy
	live       map[string]bool
	memory     string
	accounts   accounts.Store
	compaction compactionPolicy
	ledger     usage.Ledger
	now        func() time.Time
}

type healthSource struct {
	inputs healthInputs
	ready  bool
}

type runFunc = func(ctx context.Context, deep bool) contracts.ProbeReport

var errHealthNotReady = errors.New("health probes not ready")

func (s *healthSource) run(ctx context.Context, deep bool) (health.Snapshot, error) {
	if s == nil || !s.ready {
		return health.Snapshot{}, errHealthNotReady
	}
	return probeHealth(ctx, s.inputs, deep), nil
}

func probeHealth(ctx context.Context, inputs healthInputs, deep bool) health.Snapshot {
	probedAt := inputs.now()
	checks, elapsed := health.Runner{Now: inputs.now}.Run(ctx, healthProbes(inputs), deep)
	return health.Snapshot{Daemon: inputs.daemon, ProbedAt: probedAt, Elapsed: elapsed, Checks: checks, CompactStack: slices.Clone(inputs.compaction.names)}
}

func healthProbes(inputs healthInputs) []health.Probe {
	probes := []health.Probe{daemonProbe(inputs.daemon)}
	probes = append(probes, keyProbes(inputs)...)
	probes = append(probes, gatewayProbes(inputs)...)
	probes = append(probes, backendProbes(inputs)...)
	probes = append(probes, memoryProbes(inputs.memory)...)
	return append(probes, compactionProbes(inputs.compaction.names, lastCompactions(inputs.ledger))...)
}

func daemonProbe(daemon health.Daemon) health.Probe {
	return health.Probe{
		Check: health.Check{Group: health.GroupCore, Name: health.ServiceDaemon, Facts: health.Facts{Socket: daemon.Socket}},
		Run:   reportRun(contracts.ProbeReport{State: contracts.HealthOK}),
	}
}

func keyProbes(inputs healthInputs) []health.Probe {
	if inputs.keys == nil {
		return nil
	}
	pools := inputs.keys.Pools(inputs.now())
	probes := make([]health.Probe, 0, len(pools)+1)
	probes = append(probes, proxyProbe(inputs.keys.Listen(), len(pools)))
	for _, pool := range pools {
		check := health.Check{
			Group: health.GroupKeys,
			Name:  pool.Provider,
			Facts: health.Facts{Listen: inputs.keys.Listen(), Pool: &health.Pool{
				Keys:       pool.Keys,
				Usable:     pool.Usable,
				LastStatus: pool.LastStatus,
				CoolingFor: pool.CoolingFor,
			}},
		}
		probes = append(probes, health.Probe{Check: check, Run: reportRun(contracts.ProbeReport{State: poolState(pool)})})
	}
	return probes
}

func proxyProbe(listen string, providers int) health.Probe {
	state := contracts.HealthOK
	if providers == 0 {
		state = contracts.HealthOff
	}
	return health.Probe{
		Check: health.Check{Group: health.GroupCore, Name: health.ServiceProxy, Facts: health.Facts{Listen: listen, Providers: providers}},
		Run:   reportRun(contracts.ProbeReport{State: state}),
	}
}

func poolState(pool keyproxy.PoolState) contracts.HealthState {
	switch {
	case pool.Keys == 0:
		return contracts.HealthOff
	case pool.Usable == 0:
		return contracts.HealthDown
	case pool.Usable < pool.Keys:
		return contracts.HealthDegraded
	}
	return contracts.HealthOK
}

func gatewayProbes(inputs healthInputs) []health.Probe {
	plugins := contracts.Default.Gateways()
	probes := make([]health.Probe, 0, len(plugins))
	for index := range plugins {
		plugin := plugins[index]
		kind := plugin.Manifest.Kind
		probes = append(probes, health.Probe{
			Check: health.Check{Group: health.GroupGateways, Name: kind},
			Run:   gatewayRun(plugin, inputs.live[kind]),
		})
	}
	return probes
}

func gatewayRun(plugin contracts.Plugin, live bool) runFunc {
	if !live {
		return reportRun(contracts.ProbeReport{State: contracts.HealthOff})
	}
	if plugin.Probe != nil {
		return pluginProbeRun(plugin.Manifest.Config, plugin.Probe, "", envx.Getenv)
	}
	return reportRun(contracts.ProbeReport{State: contracts.HealthOK})
}

func backendProbes(inputs healthInputs) []health.Probe {
	profiles, err := inputs.accounts.List()
	if err != nil {
		slog.Warn("health: account profiles unreadable", "err", err)
	}
	var probes []health.Probe
	backends := contracts.Default.Backends()
	for index := range backends {
		plugin := backends[index]
		kind := plugin.Manifest.Kind
		probes = append(probes, backendProbe(plugin, health.Check{Group: health.GroupBackends, Name: kind}, nil))
		for _, profile := range profiles {
			if profile.Vendor != kind {
				continue
			}
			check := health.Check{
				Group:   health.GroupBackends,
				Name:    kind,
				Account: contracts.ProfileName(profile.Name),
				Default: profile.Default,
			}
			env, envErr := contracts.ProfileEnv(kind, profile.Dir)
			if envErr != nil {
				slog.Warn("health: profile env unavailable", "vendor", kind, "err", envErr)
				probes = append(probes, health.Probe{Check: check, Run: reportRun(profileEnvReport(envErr))})
				continue
			}
			probes = append(probes, backendProbe(plugin, check, env))
		}
	}
	return probes
}

func profileEnvReport(err error) contracts.ProbeReport {
	if errors.Is(err, contracts.ErrNoProfileHome) {
		return contracts.ProbeReport{State: contracts.HealthOff}
	}
	return unreachableReport(err)
}

func backendProbe(plugin contracts.Plugin, check health.Check, env map[string]string) health.Probe {
	return health.Probe{Check: check, Run: backendRun(plugin, check.Account, env)}
}

func backendRun(plugin contracts.Plugin, account contracts.ProfileName, env map[string]string) runFunc {
	if plugin.Probe != nil {
		return pluginProbeRun(plugin.Manifest.Config, plugin.Probe, account, envGetter(env))
	}
	return func(runCtx context.Context, _ bool) contracts.ProbeReport {
		return loginReport(runCtx, accounts.OSRunner, plugin.Manifest.Kind, account, env)
	}
}

func envGetter(env map[string]string) func(string) string {
	return func(name string) string {
		if value, ok := env[name]; ok {
			return value
		}
		return os.Getenv(name)
	}
}

func loginReport(ctx context.Context, run accounts.Runner, vendor string, account contracts.ProfileName, env map[string]string) contracts.ProbeReport {
	loggedIn, err := accounts.Status(ctx, run, vendor, env)
	switch {
	case errors.Is(err, contracts.ErrNoProfileHome):
		return contracts.ProbeReport{State: contracts.HealthOff}
	case err != nil:
		return unreachableReport(err)
	case !loggedIn:
		return contracts.ProbeReport{
			State:  contracts.HealthOff,
			Detail: contracts.Notice{Code: contracts.CodeHealthNotLoggedIn},
			Remedy: loginRemedy(ctx, vendor, account),
		}
	}
	return contracts.ProbeReport{State: contracts.HealthOK, Detail: contracts.Notice{Code: contracts.CodeHealthConnected}}
}

func loginRemedy(ctx context.Context, vendor string, account contracts.ProfileName) contracts.Notice {
	command := accounts.AddCommand(vendor, account)
	if account == "" {
		cmd, err := accounts.LoginCommand(ctx, vendor)
		if err != nil {
			return contracts.Notice{}
		}
		command = cmd.String()
	}
	return contracts.Notice{Code: contracts.CodeHealthRemedyRun, Args: []contracts.Arg{contracts.Text("Command", command)}}
}

func unreachableReport(err error) contracts.ProbeReport {
	return contracts.ProbeReport{
		State:  contracts.HealthDown,
		Detail: contracts.Notice{Code: contracts.CodeHealthUnreachable, Args: []contracts.Arg{contracts.Text("Reason", err.Error())}},
	}
}

func pluginProbeRun(settings []contracts.Setting, probe contracts.HealthProber, account contracts.ProfileName, getenv func(string) string) runFunc {
	return func(ctx context.Context, deep bool) contracts.ProbeReport {
		cfg, err := contracts.Resolve(settings, getenv)
		if err != nil {
			return settingsReport(err)
		}
		return probe(ctx, contracts.ProbeAccess{Getenv: getenv, Account: account, Config: cfg, Deep: deep})
	}
}

func settingsReport(err error) contracts.ProbeReport {
	missing, ok := errors.AsType[*contracts.MissingSettingsError](err)
	if !ok || len(missing.Missing) == 0 {
		return unreachableReport(err)
	}
	name, fromEnv := missingEnvName(missing.Missing[0])
	if !fromEnv {
		return unreachableReport(err)
	}
	return contracts.ProbeReport{
		State:  contracts.HealthDown,
		Detail: contracts.Notice{Code: contracts.CodeHealthEnvMissing, Args: []contracts.Arg{contracts.Text("Name", name)}},
		Remedy: contracts.Notice{Code: contracts.CodeHealthRemedyEnvRestart, Args: []contracts.Arg{contracts.Text("Name", name)}},
	}
}

func missingEnvName(entry string) (string, bool) {
	_, name, found := strings.Cut(entry, " (set ")
	return strings.TrimSuffix(name, ")"), found
}

func reportRun(report contracts.ProbeReport) runFunc {
	return func(context.Context, bool) contracts.ProbeReport {
		return report
	}
}

func memoryProbes(live string) []health.Probe {
	memories := contracts.Default.Memories()
	probes := make([]health.Probe, 0, len(memories))
	for index := range memories {
		plugin := memories[index]
		kind := plugin.Manifest.Kind
		probes = append(probes, health.Probe{
			Check: health.Check{Group: health.GroupMemory, Name: kind},
			Run:   memoryRun(plugin, kind == live),
		})
	}
	return probes
}

func memoryRun(plugin contracts.Plugin, live bool) runFunc {
	if plugin.Probe != nil {
		return pluginProbeRun(plugin.Manifest.Config, plugin.Probe, "", os.Getenv)
	}
	state := contracts.HealthOff
	if live {
		state = contracts.HealthOK
	}
	return reportRun(contracts.ProbeReport{State: state})
}

func firstMemoryKind() string {
	memories := contracts.Default.Memories()
	for index := range memories {
		if memories[index].Memory != nil {
			return memories[index].Manifest.Kind
		}
	}
	return ""
}

func compactionProbes(names []string, last map[string]usage.LastCompaction) []health.Probe {
	var probes []health.Probe
	for _, name := range names {
		check := health.Check{Group: health.GroupCompaction, Name: name}
		if run, ok := last[name]; ok {
			check.Facts.Compaction = &health.Compaction{At: run.At, Outcome: run.Outcome}
		}
		probes = append(probes, health.Probe{Check: check, Run: compactionRun(name)})
	}
	return probes
}

const compactionProbeRange = usage.Range7d

func lastCompactions(ledger usage.Ledger) map[string]usage.LastCompaction {
	records, err := ledger.Records(compactionProbeRange)
	if err != nil && !errors.Is(err, usage.ErrLedgerTruncated) {
		slog.Warn("health: compaction ledger unreadable", "err", err)
	}
	return usage.LastCompactions(records)
}

func compactionRun(name string) runFunc {
	plugin, found := compactorPlugin(name)
	if !found || plugin.Probe == nil {
		return reportRun(contracts.ProbeReport{State: contracts.HealthOK})
	}
	return pluginProbeRun(plugin.Manifest.Config, plugin.Probe, "", os.Getenv)
}

func compactorPlugin(name string) (contracts.Plugin, bool) {
	compactors := contracts.Default.Compactors()
	for index := range compactors {
		if compactors[index].Manifest.Kind == name {
			return compactors[index], true
		}
	}
	return contracts.Plugin{}, false
}

func newHealthInputs(instanceID string, keys *keyproxy.Proxy, gws []Deps, policy compactionPolicy, partDir string) healthInputs {
	store, err := accountStore(BackendRequest{})
	if err != nil {
		slog.Warn("health: account store unavailable", "err", err)
	}
	return healthInputs{
		daemon:     health.Daemon{PID: os.Getpid(), Started: time.Now(), Socket: CommandSocketPath(instanceID), Version: daemonVersion()},
		keys:       keys,
		live:       liveKinds(gws),
		memory:     firstMemoryKind(),
		accounts:   store,
		compaction: policy,
		ledger:     usage.Ledger{Dir: usageDir(partDir), Now: time.Now},
		now:        time.Now,
	}
}

func daemonVersion() string {
	info, ok := debug.ReadBuildInfo()
	if !ok || info.Main.Version == "" {
		return "(devel)"
	}
	return info.Main.Version
}

func liveKinds(gws []Deps) map[string]bool {
	live := make(map[string]bool, len(gws))
	for _, kind := range kindsOf(gws) {
		live[kind] = true
	}
	return live
}

func (s *healthSource) bind(inputs healthInputs) {
	s.inputs = inputs
	s.ready = true
}
