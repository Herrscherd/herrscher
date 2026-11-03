package host

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	contracts "github.com/Herrscherd/herrscher-contracts"
	"github.com/Herrscherd/herrscher/core/bridge"
	"github.com/Herrscherd/herrscher/core/cli"
	"github.com/Herrscherd/herrscher/core/internal/state"
)

const seedConsolidateTimeout = 60 * time.Second

const seedTurnTimeout = 120 * time.Second

type oneShotSeedRuntime struct {
	coordinator contracts.Coordinator
	publish     func(session string, event contracts.Event)
	record      func(session string, entry state.TranscriptEntry)
	timeout     time.Duration
	backend     func(context.Context, state.Session) (contracts.Backend, error)
}

func (r oneShotSeedRuntime) backendFactory() func(context.Context, state.Session) (contracts.Backend, error) {
	if r.backend != nil {
		return r.backend
	}
	return newSeedBackend
}

type oneShotSeedRuntimeKey struct{}

func withOneShotSeedRuntime(ctx context.Context, runtime oneShotSeedRuntime) context.Context {
	return context.WithValue(ctx, oneShotSeedRuntimeKey{}, runtime)
}

func oneShotSeedRuntimeFrom(ctx context.Context) oneShotSeedRuntime {
	runtime, ok := ctx.Value(oneShotSeedRuntimeKey{}).(oneShotSeedRuntime)
	if !ok {
		return oneShotSeedRuntime{}
	}
	return runtime
}

type seedCommandForwarder func(context.Context, string, []string) (string, bool, error)

func runOneShotSeedCommand(ctx context.Context, st *state.State, name, task, turnID string, runtime oneShotSeedRuntime, coord contracts.Coordinator, instID string, forward seedCommandForwarder) (string, error) {
	if runtime.coordinator == nil {
		runtime.coordinator = coord
	}
	if runtime.coordinator == nil && forward != nil {
		argv := append([]string{nsSession, verbSeed, "--name", name}, cli.FlagArg("task", task)...)
		argv = append(argv, "--turn_id", turnID)
		if runtime.timeout > 0 {
			argv = append(argv, "--timeout", runtime.timeout.String())
		}
		if reply, handled, err := forward(ctx, commandSocketTarget(instID), argv); handled {
			return reply, err
		}
	}
	return runOneShotSeedIDWithRuntime(ctx, st, name, task, turnID, runtime)
}

func runOneShotSeedIDWithRuntime(ctx context.Context, st *state.State, name, task, turnID string, runtime oneShotSeedRuntime) (string, error) {
	sess, ok := st.FindSession(name)
	if !ok {
		return "", &NotFoundError{Kind: kindSession, Name: name}
	}
	orch, mem, err := seedOrchestrator(ctx, sess)
	if err != nil {
		return "", err
	}
	if mem != nil {
		defer closeRead(mem)
	}
	return runOneShotSeedWithIDRuntime(ctx, sess, task, turnID, orch, runtime)
}

func runOneShotSeedWithIDRuntime(ctx context.Context, sess state.Session, task, turnID string, orch contracts.Orchestrator, runtime oneShotSeedRuntime) (string, error) {
	if orch != nil {
		defer closeRead(orch)
	}
	seedCtx, cancel := context.WithTimeout(ctx, oneShotTurnTimeout(runtime))
	defer cancel()
	toBridge := make(chan contracts.Event, 1)
	fromBridge := make(chan contracts.Event, bridgeEventBuffer)
	d := newSessionDriver(sess.Name, nil, toBridge, fromBridge)
	d.incarnation = sess.Incarnation
	d.agent = sess.Agent
	attachOneShotTaps(d, sess.Name, runtime)
	pumpCtx, pumpCancel := context.WithCancel(seedCtx)
	defer pumpCancel()
	pumpDone := make(chan struct{})
	go func() { defer close(pumpDone); d.pump(pumpCtx) }()

	bridgeErr := make(chan error, 1)
	go runOneShotBridge(seedCtx, cancel, sess, orch, runtime, toBridge, fromBridge, bridgeErr)

	reply, ok := d.SeedAndWaitWithTurnID(seedCtx, task, turnID)
	if !ok {
		return "", oneShotTimeoutError(bridgeErr)
	}
	if err := <-bridgeErr; err != nil {
		return "", err
	}
	if runtime.coordinator != nil {
		pumpCancel()
		<-pumpDone
		d.coordinator = runtime.coordinator
		d.activeTurnID = turnID
		if coordination := d.maybeCoordinate(seedCtx, reply); coordination != nil {
			d.fanOut(seedCtx, contracts.Event{T: eventReply, Done: true, Text: reply, Coordination: coordination})
		}
		d.activeTurnID = ""
	}
	consolidateOneShot(ctx, orch, sess)
	return reply, nil
}

func oneShotTurnTimeout(runtime oneShotSeedRuntime) time.Duration {
	if runtime.timeout <= 0 {
		return seedTurnTimeout
	}
	return runtime.timeout
}

func attachOneShotTaps(driver *sessionDriver, name string, runtime oneShotSeedRuntime) {
	if runtime.record != nil {
		driver.sink.Transcript = func(entry state.TranscriptEntry) { runtime.record(name, entry) }
	}
	if runtime.publish != nil {
		driver.emitTap = func(e contracts.Event) { runtime.publish(name, e) }
	}
}

func runOneShotBridge(ctx context.Context, cancel context.CancelFunc, sess state.Session, orch contracts.Orchestrator, runtime oneShotSeedRuntime, toBridge, fromBridge chan contracts.Event, bridgeErr chan<- error) {
	err := bridge.RunOneShot(ctx, func(string) (contracts.Backend, error) {
		return runtime.backendFactory()(ctx, sess)
	}, orch, sess.ChannelID, toBridge, fromBridge)
	bridgeErr <- err
	if err != nil {
		cancel()
	}
}

func oneShotTimeoutError(bridgeErr <-chan error) error {
	select {
	case err := <-bridgeErr:
		if err != nil {
			return err
		}
	default:
	}
	return errSeedTimeout
}

func consolidateOneShot(ctx context.Context, orch contracts.Orchestrator, sess state.Session) {
	if orch == nil || sess.ConsolidateEvery > 0 {
		return
	}
	consCtx, consCancel := context.WithTimeout(ctx, seedConsolidateTimeout)
	started := time.Now()
	err := orch.Consolidate(consCtx)
	consCancel()
	if err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "herrscher: session %q: consolidate after %s: %v\n", sess.Name, time.Since(started).Round(time.Millisecond), err)
	}
}

// ApplyOrchestratorScope threads a session's runtime scope into an orchestrator
// plugin's config bag. It is the single source of truth for these Settings keys,
// shared by the live bridge (bridge.go) and the one-shot seed so the two paths
// cannot drift when a scope key is added or renamed. Empty optional values are
// omitted so a plain/unconfigured run's config stays byte-for-byte unchanged.
func ApplyOrchestratorScope(cfg *contracts.PluginConfig, session, project, agent, extractor, journal string, consolidateEvery int) {
	if cfg.Settings == nil {
		cfg.Settings = map[string]string{}
	}
	cfg.Settings[keySession] = session
	if project != "" {
		cfg.Settings["memory.project"] = project
	}
	if agent != "" {
		cfg.Settings["memory.agent"] = agent
	}
	if extractor != "" {
		cfg.Settings["memory.extractor"] = extractor
	}
	if journal != "" {
		cfg.Settings["memory.journal"] = journal
	}
	if consolidateEvery > 0 {
		cfg.Settings["memory.consolidate-every"] = strconv.Itoa(consolidateEvery)
	}
}

// BackendRequest is everything building a backend needs. It used to be six
// positional parameters; routing would have added a seventh, which made call
// sites unreadable.
type BackendRequest struct {
	Vendor  string
	Cmd     string
	Kind    string
	Dir     string
	Resume  string
	ModelID string

	resolver remoteBackendResolver
}

func (req BackendRequest) remoteResolver() remoteBackendResolver {
	if req.resolver != nil {
		return req.resolver
	}
	return NewResolver(nil, os.Getenv("HERRSCHER_NATS"))
}

type remoteBackendResolver interface {
	Backend(context.Context, []contracts.Plugin, ...string) (contracts.Backend, bool, error)
}

// BuildBackendFor selects and constructs a backend. A remote resolver backend
// wins when configured; otherwise the matching registered plugin is built
// with the invocation, kind, working directory, and, if a ModelID is
// supplied, the environment variables its route requires.
func BuildBackendFor(ctx context.Context, req BackendRequest) (contracts.Backend, error) {
	desired := req.Vendor
	if desired == "" {
		desired = os.Getenv("HERRSCHER_BACKEND")
	}
	plugins := contracts.Default.Backends()
	if req.ModelID == "" && ResolvePolicy(os.Getenv) == contracts.PolicyGatewayOnly {
		return nil, errGatewayOnlyNoModel
	}
	var route modelRoute
	if req.ModelID != "" {
		var err error
		if desired, route, err = resolveModelRoute(req.ModelID, plugins, desired); err != nil {
			return nil, err
		}
	}
	if backend, fromRemote, err := req.remoteResolver().Backend(ctx, plugins, desired); err != nil {
		return nil, err
	} else if fromRemote {
		if len(route.env) > 0 {
			return nil, &gatewayEnvUnsupportedError{Model: req.ModelID}
		}
		return backend, nil
	}
	plugin, err := selectBackend(desired, plugins)
	if err != nil {
		return nil, err
	}
	cfg, err := contracts.Resolve(plugin.Manifest.Config, os.Getenv)
	if err != nil {
		return nil, err
	}
	if cfg.Settings == nil {
		cfg.Settings = map[string]string{}
	}
	applyBackendRequest(cfg.Settings, req, route)
	return plugin.Backend(ctx, cfg)
}

type modelRoute struct {
	env map[string]string
	arg string
}

func resolveModelRoute(modelID string, plugins []contracts.Plugin, desired string) (string, modelRoute, error) {
	entry, err := LookupModel(plugins, ResolvePolicy(os.Getenv), modelID)
	if err != nil {
		return "", modelRoute{}, err
	}
	if desired == "" {
		desired = entry.Vendor
	} else if desired != entry.Vendor {
		return "", modelRoute{}, &modelBackendMismatchError{Model: modelID, Owner: entry.Vendor, Requested: desired}
	}
	env, err := spawnEnvFor(entry, os.Getenv)
	if err != nil {
		return "", modelRoute{}, err
	}
	return desired, modelRoute{env: env, arg: entry.Arg}, nil
}

func applyBackendRequest(settings map[string]string, req BackendRequest, route modelRoute) {
	setIfPresent(settings, "cmd", req.Cmd)
	setIfPresent(settings, "kind", req.Kind)
	setIfPresent(settings, "dir", req.Dir)
	setIfPresent(settings, "resume", req.Resume)
	setIfPresent(settings, "model", route.arg)
	if len(route.env) > 0 {
		settings["env"] = contracts.EncodeEnvSetting(route.env)
	}
}

func setIfPresent(settings map[string]string, key, value string) {
	if value != "" {
		settings[key] = value
	}
}

func newSeedBackend(ctx context.Context, sess state.Session) (contracts.Backend, error) {
	dir := sess.Dir
	if dir == "" {
		dir = sess.Worktree
	}
	return BuildBackendFor(ctx, BackendRequest{
		Vendor:  sess.Vendor,
		Cmd:     sess.Cmd,
		Kind:    sess.Backend,
		Dir:     resolveBackendDir(dir),
		Resume:  sess.ResumeToken,
		ModelID: sess.ModelID,
	})
}

func resolveBackendDir(dir string) string {
	if dir == "" {
		return ""
	}
	dir = filepath.Clean(dir)
	if filepath.IsAbs(dir) {
		return dir
	}
	cwd, err := os.Getwd()
	if err != nil {
		return dir
	}
	cwd = filepath.Clean(cwd)
	suffix := string(os.PathSeparator) + dir
	if (os.PathSeparator == '\\' && strings.HasSuffix(strings.ToLower(cwd), strings.ToLower(suffix))) ||
		(os.PathSeparator != '\\' && strings.HasSuffix(cwd, suffix)) {
		return cwd
	}
	return filepath.Join(cwd, dir)
}

func selectBackend(desired string, plugins []contracts.Plugin) (contracts.Plugin, error) {
	for _, plugin := range plugins {
		if plugin.Backend == nil {
			continue
		}
		if desired == "" || plugin.Manifest.Kind == desired {
			return plugin, nil
		}
	}
	if desired != "" {
		return contracts.Plugin{}, &UnknownError{Kind: "backend", Name: desired}
	}
	return contracts.Plugin{}, errNoBackendPlugin
}

func provisionSeedScope(ctx context.Context, mem contracts.Memory, project, agent string) {
	p, ok := mem.(contracts.Provisioner)
	if !ok {
		return
	}
	if project != "" {
		logCleanup(p.EnsureProject(ctx, contracts.ProjectKey(project), project))
	}
	if agent != "" {
		logCleanup(p.EnsureAgent(ctx, contracts.AgentKey(agent), agent))
	}
}

func seedOrchestrator(ctx context.Context, sess state.Session) (contracts.Orchestrator, contracts.Memory, error) {
	memProject, memAgent := sess.MemoryRoots()
	resolver := NewResolver(nil, os.Getenv("HERRSCHER_NATS"))
	mem, _, err := resolver.Memory(ctx, contracts.Default.Memories(), os.Getenv)
	if err != nil {
		return nil, nil, err
	}
	provisionSeedScope(ctx, mem, memProject, memAgent)
	orch, orchResolved, err := resolver.Orchestrator(ctx, contracts.Default.Orchestrators())
	if err != nil {
		closeMemory(mem)
		return nil, nil, err
	}
	if orchResolved {
		return orch, mem, nil
	}
	orch, found, err := buildLocalOrchestrator(ctx, sess, mem, memProject, memAgent)
	if err != nil {
		closeMemory(mem)
		return nil, nil, err
	}
	if !found {
		return nil, mem, nil
	}
	return orch, mem, nil
}

func buildLocalOrchestrator(ctx context.Context, sess state.Session, mem contracts.Memory, memProject, memAgent string) (contracts.Orchestrator, bool, error) {
	for _, plugin := range contracts.Default.Orchestrators() {
		if plugin.Orchestrator == nil {
			continue
		}
		cfg, err := contracts.Resolve(plugin.Manifest.Config, os.Getenv)
		if err != nil {
			return nil, false, err
		}
		ApplyOrchestratorScope(&cfg, sess.Name, memProject, memAgent, sess.Extractor, sess.Journal, sess.ConsolidateEvery)
		orch, err := plugin.Orchestrator(ctx, cfg, mem)
		if err != nil {
			return nil, false, err
		}
		return orch, true, nil
	}
	return nil, false, nil
}

func closeMemory(mem contracts.Memory) {
	if mem != nil {
		logCleanup(mem.Close())
	}
}
