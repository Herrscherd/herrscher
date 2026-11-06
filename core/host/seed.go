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
	"github.com/Herrscherd/herrscher-contracts/i18n"
	"github.com/Herrscherd/herrscher/core/bridge"
	"github.com/Herrscherd/herrscher/core/cli"
	"github.com/Herrscherd/herrscher/core/envx"
	"github.com/Herrscherd/herrscher/core/internal/lease"
	"github.com/Herrscherd/herrscher/core/internal/state"
	"github.com/Herrscherd/herrscher/core/keyproxy"
)

const seedConsolidateTimeout = 60 * time.Second

const seedTurnTimeout = 120 * time.Second

type oneShotSeedRuntime struct {
	coordinator contracts.Coordinator
	publish     func(session string, event contracts.Event)
	record      func(session string, entry state.TranscriptEntry)
	timeout     time.Duration
	backend     func(context.Context, state.Session) (contracts.Backend, error)
	models      *ModelDiscovery
}

func (r oneShotSeedRuntime) backendFactory() func(context.Context, state.Session) (contracts.Backend, error) {
	if r.backend != nil {
		return r.backend
	}
	return func(ctx context.Context, sess state.Session) (contracts.Backend, error) {
		return newSeedBackend(ctx, sess, nil, r.models)
	}
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

func runOneShotSeedCommand(ctx context.Context, hostState *state.State, name, task, turnID string, runtime oneShotSeedRuntime, coord contracts.Coordinator, instID string, forward seedCommandForwarder) (string, error) {
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
	return runOneShotSeedIDWithRuntime(ctx, hostState, name, task, turnID, runtime)
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
	driver := newSessionDriver(sess.Name, nil, toBridge, fromBridge)
	driver.incarnation = sess.Incarnation
	driver.agent = sess.Agent
	driver.language = func() i18n.Language { return knownSessionLanguage(sess) }
	attachOneShotTaps(driver, sess.Name, runtime)
	pumpCtx, pumpCancel := context.WithCancel(seedCtx)
	defer pumpCancel()
	pumpDone := make(chan struct{})
	go func() { defer close(pumpDone); driver.pump(pumpCtx) }()

	bridgeErr := make(chan error, 1)
	go runOneShotBridge(seedCtx, cancel, sess, orch, runtime, toBridge, fromBridge, bridgeErr)

	reply, ok := driver.SeedAndWaitWithTurnID(seedCtx, task, turnID)
	if !ok {
		return "", oneShotTimeoutError(bridgeErr)
	}
	if err := <-bridgeErr; err != nil {
		return "", err
	}
	if runtime.coordinator != nil {
		pumpCancel()
		<-pumpDone
		driver.coordinator = runtime.coordinator
		driver.activeTurnID = turnID
		if coordination := driver.maybeCoordinate(seedCtx, reply); coordination != nil {
			driver.fanOut(seedCtx, contracts.Event{T: eventReply, Done: true, Text: reply, Coordination: coordination})
		}
		driver.activeTurnID = ""
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
		_, _ = fmt.Fprintln(os.Stderr, newHostMessages().text(i18n.English, "seed.consolidate", map[string]any{nameParam: sess.Name, "Elapsed": time.Since(started).Round(time.Millisecond).String(), detailParam: fmt.Sprint(err)}))
	}
}

// ApplyOrchestratorScope threads a session's runtime scope into an orchestrator
// plugin's config bag. It is the single source of truth for these Settings keys,
// shared by the live bridge (bridge.go) and the one-shot seed so the two paths
// cannot drift when a scope key is added or renamed. Empty optional values are
// omitted so a plain/unconfigured run's config stays byte-for-byte unchanged.
func ApplyOrchestratorScope(cfg *contracts.PluginConfig, session, project, agent, extractor, journal string, consolidateEvery int) {
	if cfg.Settings == nil {
		cfg.Settings = make(map[string]string)
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
	Vendor      string
	Cmd         string
	Kind        string
	Dir         string
	Resume      string
	ModelID     string
	Account     string
	AccountRoot string
	Keys        *keyproxy.Proxy
	Models      *ModelDiscovery

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
	backend, release, err := buildBackend(ctx, req)
	if err != nil {
		return nil, err
	}
	return lease.Wrap(backend, release), nil
}

func buildBackend(ctx context.Context, req BackendRequest) (contracts.Backend, func(), error) {
	plugins := contracts.Default.Backends()
	if req.ModelID == "" && ResolvePolicy(envx.Getenv) == contracts.PolicyGatewayOnly {
		return nil, noRelease, errGatewayOnlyNoModel
	}
	desired, entry, err := chooseVendor(req.Vendor, req.ModelID, plugins, req.Models)
	if err != nil {
		return nil, noRelease, err
	}
	route, err := routeFor(req.ModelID, entry, req.Keys)
	if err != nil {
		return nil, noRelease, err
	}
	failWith := func(err error) (contracts.Backend, func(), error) {
		route.lease()()
		return nil, noRelease, err
	}
	backend, fromRemote, err := req.remoteResolver().Backend(ctx, plugins, desired)
	if err != nil {
		return failWith(err)
	}
	if fromRemote {
		if refusal := req.remoteRefusal(route); refusal != nil {
			return failWith(refusal)
		}
		return backend, noRelease, nil
	}
	plugin, err := selectBackend(desired, plugins)
	if err != nil {
		return failWith(err)
	}
	cfg, err := contracts.Resolve(plugin.Manifest.Config, os.Getenv)
	if err != nil {
		return failWith(err)
	}
	if cfg.Settings == nil {
		cfg.Settings = make(map[string]string)
	}
	env, release, err := sessionEnv(req, desired, route)
	if err != nil {
		return failWith(err)
	}
	route.env = env
	applyBackendRequest(cfg.Settings, req, route)
	backend, err = plugin.Backend(ctx, cfg)
	if err != nil {
		release()
		return nil, noRelease, err
	}
	return backend, release, nil
}

type modelRoute struct {
	env   map[string]string
	arg   string
	grant func()
}

func (r modelRoute) lease() func() {
	if r.grant == nil {
		return noRelease
	}
	return r.grant
}

func chooseVendor(requested, modelID string, plugins []contracts.Plugin, models *ModelDiscovery) (string, CatalogEntry, error) {
	vendor := requested
	if vendor == "" {
		vendor = os.Getenv("HERRSCHER_BACKEND")
	}
	if modelID == "" {
		return vendor, CatalogEntry{}, nil
	}
	entry, err := LookupModelWith(plugins, ResolvePolicy(envx.Getenv), models.Sources(), modelID)
	if err != nil {
		return "", CatalogEntry{}, err
	}
	if vendor == "" {
		return entry.Vendor, entry, nil
	}
	if vendor != entry.Vendor {
		return "", CatalogEntry{}, &modelBackendMismatchError{Model: modelID, Owner: entry.Vendor, Requested: vendor}
	}
	return vendor, entry, nil
}

func routeFor(modelID string, entry CatalogEntry, keys *keyproxy.Proxy) (modelRoute, error) {
	if modelID == "" {
		return modelRoute{}, nil
	}
	env, release, err := spawnEnvVia(entry, os.Getenv, keys)
	if err != nil {
		return modelRoute{}, err
	}
	return modelRoute{env: env, arg: entry.Arg, grant: release}, nil
}

func (req BackendRequest) remoteRefusal(route modelRoute) error {
	if len(route.env) > 0 {
		return &gatewayEnvUnsupportedError{Model: req.ModelID}
	}
	return req.RemoteAccountRefusal()
}

// RemoteAccountRefusal refuses an account on a remote backend, which resolves accounts in its own store.
func (req BackendRequest) RemoteAccountRefusal() error {
	if req.Account != "" {
		return &remoteAccountError{Account: req.Account}
	}
	return nil
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

func newSeedBackend(ctx context.Context, sess state.Session, keys *keyproxy.Proxy, models *ModelDiscovery) (contracts.Backend, error) {
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
		Account: sess.Account,
		Keys:    keys,
		Models:  models,
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
	for idx := range plugins {
		plugin := plugins[idx]
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
	provisioner, ok := mem.(contracts.Provisioner)
	if !ok {
		return
	}
	if project != "" {
		logCleanup(provisioner.EnsureProject(ctx, contracts.ProjectKey(project), project))
	}
	if agent != "" {
		logCleanup(provisioner.EnsureAgent(ctx, contracts.AgentKey(agent), agent))
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
	orchestrators := contracts.Default.Orchestrators()
	for idx := range orchestrators {
		plugin := orchestrators[idx]
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
