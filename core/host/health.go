package host

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"time"

	contracts "github.com/Herrscherd/herrscher-contracts"

	"github.com/Herrscherd/herrscher/core/accounts"
	"github.com/Herrscherd/herrscher/core/health"
	"github.com/Herrscherd/herrscher/core/keyproxy"
)

type healthInputs struct {
	daemon     health.Daemon
	keys       *keyproxy.Proxy
	live       map[string]bool
	memory     string
	accounts   accounts.Store
	compaction compactionPolicy
	now        func() time.Time
}

type healthSource struct {
	inputs healthInputs
	ready  bool
}

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
	return health.Snapshot{Daemon: inputs.daemon, ProbedAt: probedAt, Elapsed: elapsed, Checks: checks}
}

func healthProbes(inputs healthInputs) []health.Probe {
	probes := []health.Probe{daemonProbe(inputs.daemon)}
	probes = append(probes, keyProbes(inputs)...)
	probes = append(probes, gatewayProbes(inputs)...)
	probes = append(probes, backendProbes(inputs)...)
	probes = append(probes, memoryProbes(inputs.memory)...)
	return append(probes, compactionProbes(inputs.compaction.names)...)
}

func daemonProbe(daemon health.Daemon) health.Probe {
	return health.Probe{
		Check: health.Check{Group: health.GroupCore, Name: "daemon", Facts: health.Facts{Socket: daemon.Socket}},
		Run: func(context.Context, bool) contracts.ProbeReport {
			return contracts.ProbeReport{State: contracts.HealthOK}
		},
	}
}

func keyProbes(inputs healthInputs) []health.Probe {
	if inputs.keys == nil {
		return nil
	}
	pools := inputs.keys.Pools(inputs.now())
	probes := make([]health.Probe, 0, len(pools))
	for _, pool := range pools {
		report := contracts.ProbeReport{State: poolState(pool)}
		probes = append(probes, health.Probe{
			Check: health.Check{
				Group: health.GroupKeys,
				Name:  pool.Provider,
				Facts: health.Facts{Listen: inputs.keys.Listen(), Pool: &health.Pool{
					Keys:       pool.Keys,
					Usable:     pool.Usable,
					LastStatus: pool.LastStatus,
					CoolingFor: pool.CoolingFor,
				}},
			},
			Run: func(context.Context, bool) contracts.ProbeReport { return report },
		})
	}
	return probes
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

func gatewayRun(plugin contracts.Plugin, live bool) func(context.Context, bool) contracts.ProbeReport {
	if plugin.Probe != nil {
		return func(ctx context.Context, deep bool) contracts.ProbeReport {
			return plugin.Probe(ctx, contracts.ProbeAccess{Getenv: os.Getenv, Deep: deep})
		}
	}
	state := contracts.HealthOff
	if live {
		state = contracts.HealthOK
	}
	return func(context.Context, bool) contracts.ProbeReport {
		return contracts.ProbeReport{State: state}
	}
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
			env, envErr := contracts.ProfileEnv(kind, profile.Dir)
			if envErr != nil {
				slog.Warn("health: profile env unavailable", "vendor", kind, "err", envErr)
				continue
			}
			check := health.Check{
				Group:   health.GroupBackends,
				Name:    kind,
				Account: contracts.ProfileName(profile.Name),
				Default: profile.Default,
			}
			probes = append(probes, backendProbe(plugin, check, env))
		}
	}
	return probes
}

func backendProbe(plugin contracts.Plugin, check health.Check, env map[string]string) health.Probe {
	return health.Probe{Check: check, Run: backendRun(plugin, check.Account, env)}
}

func backendRun(plugin contracts.Plugin, account contracts.ProfileName, env map[string]string) func(context.Context, bool) contracts.ProbeReport {
	if plugin.Probe != nil {
		return func(probeCtx context.Context, deep bool) contracts.ProbeReport {
			return plugin.Probe(probeCtx, contracts.ProbeAccess{Getenv: envGetter(env), Account: account, Deep: deep})
		}
	}
	return func(runCtx context.Context, _ bool) contracts.ProbeReport {
		return loginReport(runCtx, plugin.Manifest.Kind, account, env)
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

func loginReport(ctx context.Context, vendor string, account contracts.ProfileName, env map[string]string) contracts.ProbeReport {
	loggedIn, err := accounts.Status(ctx, accounts.OSRunner, vendor, env)
	switch {
	case errors.Is(err, contracts.ErrNoProfileHome):
		return contracts.ProbeReport{State: contracts.HealthOff}
	case err != nil:
		return contracts.ProbeReport{
			State:  contracts.HealthDown,
			Detail: contracts.Notice{Code: contracts.CodeHealthUnreachable, Args: []contracts.Arg{contracts.Text("Reason", err.Error())}},
		}
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

func memoryRun(plugin contracts.Plugin, live bool) func(context.Context, bool) contracts.ProbeReport {
	if plugin.Probe != nil {
		return func(ctx context.Context, deep bool) contracts.ProbeReport {
			return plugin.Probe(ctx, contracts.ProbeAccess{Getenv: os.Getenv, Deep: deep})
		}
	}
	state := contracts.HealthOff
	if live {
		state = contracts.HealthOK
	}
	return func(context.Context, bool) contracts.ProbeReport {
		return contracts.ProbeReport{State: state}
	}
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

func compactionProbes(names []string) []health.Probe {
	var probes []health.Probe
	for _, name := range names {
		if name == contracts.CompactStageBackend {
			continue
		}
		probes = append(probes, health.Probe{
			Check: health.Check{Group: health.GroupCompaction, Name: name},
			Run:   compactionRun(name),
		})
	}
	return probes
}

func compactionRun(name string) func(context.Context, bool) contracts.ProbeReport {
	plugin, found := compactorPlugin(name)
	if !found || plugin.Probe == nil {
		return func(context.Context, bool) contracts.ProbeReport {
			return contracts.ProbeReport{State: contracts.HealthOK}
		}
	}
	return func(ctx context.Context, deep bool) contracts.ProbeReport {
		return plugin.Probe(ctx, contracts.ProbeAccess{Getenv: os.Getenv, Deep: deep})
	}
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

func newHealthInputs(instanceID string, keys *keyproxy.Proxy, gws []Deps, policy compactionPolicy) healthInputs {
	store, err := accountStore(BackendRequest{})
	if err != nil {
		slog.Warn("health: account store unavailable", "err", err)
	}
	return healthInputs{
		daemon:     health.Daemon{PID: os.Getpid(), Started: time.Now(), Socket: CommandSocketPath(instanceID)},
		keys:       keys,
		live:       liveKinds(gws),
		memory:     firstMemoryKind(),
		accounts:   store,
		compaction: policy,
		now:        time.Now,
	}
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
