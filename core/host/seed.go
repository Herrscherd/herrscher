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

var oneShotBackendFactory = newSeedBackend

type oneShotSeedRuntime struct {
	coordinator contracts.Coordinator
	publish     func(session string, event contracts.Event)
	record      func(session string, entry state.TranscriptEntry)
	timeout     time.Duration
}

type oneShotSeedRuntimeKey struct{}

func withOneShotSeedRuntime(ctx context.Context, runtime oneShotSeedRuntime) context.Context {
	return context.WithValue(ctx, oneShotSeedRuntimeKey{}, runtime)
}

func oneShotSeedRuntimeFrom(ctx context.Context) oneShotSeedRuntime {
	runtime, _ := ctx.Value(oneShotSeedRuntimeKey{}).(oneShotSeedRuntime)
	return runtime
}

type seedCommandForwarder func(context.Context, string, []string) (string, bool, error)

func runOneShotSeedCommand(ctx context.Context, st *state.State, name, task, turnID string, runtime oneShotSeedRuntime, coord contracts.Coordinator, instID string, forward seedCommandForwarder) (string, error) {
	if runtime.coordinator == nil {
		runtime.coordinator = coord
	}
	if runtime.coordinator == nil && forward != nil {
		argv := append([]string{"session", "seed", "--name", name}, cli.FlagArg("task", task)...)
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

func runOneShotSeed(ctx context.Context, st *state.State, name, task string) (string, error) {
	return runOneShotSeedID(ctx, st, name, task, newTurnID())
}

func runOneShotSeedID(ctx context.Context, st *state.State, name, task, turnID string) (string, error) {
	return runOneShotSeedIDWithRuntime(ctx, st, name, task, turnID, oneShotSeedRuntime{})
}

func runOneShotSeedIDWithRuntime(ctx context.Context, st *state.State, name, task, turnID string, runtime oneShotSeedRuntime) (string, error) {
	sess, ok := st.FindSession(name)
	if !ok {
		return "", fmt.Errorf("no session %q", name)
	}
	orch, mem, err := seedOrchestrator(ctx, sess)
	if err != nil {
		return "", err
	}
	if mem != nil {
		defer mem.Close()
	}
	return runOneShotSeedWithIDRuntime(ctx, sess, task, turnID, orch, runtime)
}

func runOneShotSeedWith(ctx context.Context, sess state.Session, task string, orch contracts.Orchestrator) (string, error) {
	return runOneShotSeedWithID(ctx, sess, task, newTurnID(), orch)
}

func runOneShotSeedWithID(ctx context.Context, sess state.Session, task, turnID string, orch contracts.Orchestrator) (string, error) {
	return runOneShotSeedWithIDRuntime(ctx, sess, task, turnID, orch, oneShotSeedRuntime{})
}

func runOneShotSeedWithIDRuntime(ctx context.Context, sess state.Session, task, turnID string, orch contracts.Orchestrator, runtime oneShotSeedRuntime) (string, error) {
	if orch != nil {
		defer orch.Close()
	}

	turnTimeout := runtime.timeout
	if turnTimeout <= 0 {
		turnTimeout = seedTurnTimeout
	}
	seedCtx, cancel := context.WithTimeout(ctx, turnTimeout)
	defer cancel()
	toBridge := make(chan contracts.Event, 1)
	fromBridge := make(chan contracts.Event, 8)
	d := newSessionDriver(sess.Name, nil, toBridge, fromBridge)
	d.incarnation = sess.Incarnation
	d.agent = sess.Agent
	if runtime.record != nil {
		name := sess.Name
		d.sink.Transcript = func(entry state.TranscriptEntry) { runtime.record(name, entry) }
	}
	if runtime.publish != nil {
		name := sess.Name
		d.emitTap = func(e contracts.Event) { runtime.publish(name, e) }
	}
	pumpCtx, pumpCancel := context.WithCancel(seedCtx)
	defer pumpCancel()
	pumpDone := make(chan struct{})
	go func() { defer close(pumpDone); d.pump(pumpCtx) }()

	bridgeErr := make(chan error, 1)
	go func() {
		err := bridge.RunOneShot(seedCtx, func(channel string) (contracts.Backend, error) {
			return oneShotBackendFactory(seedCtx, sess)
		}, orch, sess.ChannelID, toBridge, fromBridge)
		bridgeErr <- err
		if err != nil {
			cancel()
		}
	}()

	reply, ok := d.SeedAndWaitWithTurnID(seedCtx, task, turnID)
	if !ok {
		select {
		case err := <-bridgeErr:
			if err != nil {
				return "", err
			}
		default:
		}
		return "", fmt.Errorf("seed timeout")
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
			d.fanOut(seedCtx, contracts.Event{T: "reply", Done: true, Text: reply, Coordination: coordination})
		}
		d.activeTurnID = ""
	}
	if orch != nil && sess.ConsolidateEvery <= 0 {
		consCtx, consCancel := context.WithTimeout(ctx, seedConsolidateTimeout)
		started := time.Now()
		err := orch.Consolidate(consCtx)
		consCancel()
		if err != nil {
			fmt.Fprintf(os.Stderr, "herrscher: session %q: consolidate after %s: %v\n", sess.Name, time.Since(started).Round(time.Millisecond), err)
		}
	}
	return reply, nil
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
	cfg.Settings["session"] = session
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
}

type remoteBackendResolver interface {
	Backend(context.Context, []contracts.Plugin, ...string) (contracts.Backend, error)
}

var newBackendResolver = func() remoteBackendResolver {
	return NewResolver(nil, os.Getenv("HERRSCHER_NATS"))
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

	var spawnEnv map[string]string
	var modelArg string
	if req.ModelID == "" && ResolvePolicy(os.Getenv) == contracts.PolicyGatewayOnly {
		return nil, fmt.Errorf("the gateway-only route policy forbids a spawn with no model: it would run on this machine's own vendor login")
	}
	if req.ModelID != "" {
		entry, err := LookupModel(plugins, ResolvePolicy(os.Getenv), req.ModelID)
		if err != nil {
			return nil, err
		}
		if desired == "" {
			desired = entry.Vendor
		} else if desired != entry.Vendor {
			return nil, fmt.Errorf("model %q belongs to backend %q, but backend %q was requested; pick a model offered by %q or switch the vendor", req.ModelID, entry.Vendor, desired, desired)
		}
		if spawnEnv, err = spawnEnvFor(entry, os.Getenv); err != nil {
			return nil, err
		}
		modelArg = entry.Arg
	}

	if backend, err := newBackendResolver().Backend(ctx, plugins, desired); err != nil {
		return nil, err
	} else if backend != nil {
		if len(spawnEnv) > 0 {
			return nil, fmt.Errorf("model %q needs a gateway spawn environment, which the remote backend resolver cannot carry; unset HERRSCHER_REMOTE=backend or pick a native model", req.ModelID)
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
	if req.Cmd != "" {
		cfg.Settings["cmd"] = req.Cmd
	}
	if req.Kind != "" {
		cfg.Settings["kind"] = req.Kind
	}
	if req.Dir != "" {
		cfg.Settings["dir"] = req.Dir
	}
	if req.Resume != "" {
		cfg.Settings["resume"] = req.Resume
	}
	if modelArg != "" {
		cfg.Settings["model"] = modelArg
	}
	if len(spawnEnv) > 0 {
		cfg.Settings["env"] = contracts.EncodeEnvSetting(spawnEnv)
	}
	return plugin.Backend(ctx, cfg)
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
		return contracts.Plugin{}, fmt.Errorf("unknown backend %q", desired)
	}
	return contracts.Plugin{}, fmt.Errorf("no backend plugin registered")
}

func provisionSeedScope(ctx context.Context, mem contracts.Memory, project, agent string) {
	p, ok := mem.(contracts.Provisioner)
	if !ok {
		return
	}
	if project != "" {
		_ = p.EnsureProject(ctx, contracts.ProjectKey(project), project)
	}
	if agent != "" {
		_ = p.EnsureAgent(ctx, contracts.AgentKey(agent), agent)
	}
}

func seedOrchestrator(ctx context.Context, sess state.Session) (contracts.Orchestrator, contracts.Memory, error) {
	memProject, memAgent := sess.MemoryRoots()
	resolver := NewResolver(nil, os.Getenv("HERRSCHER_NATS"))
	mem, err := resolver.Memory(ctx, contracts.Default.Memories(), os.Getenv)
	if err != nil {
		return nil, nil, err
	}
	provisionSeedScope(ctx, mem, memProject, memAgent)
	orch, err := resolver.Orchestrator(ctx, contracts.Default.Orchestrators())
	if err != nil {
		if mem != nil {
			_ = mem.Close()
		}
		return nil, nil, err
	}
	if orch != nil {
		return orch, mem, nil
	}
	for _, plugin := range contracts.Default.Orchestrators() {
		if plugin.Orchestrator == nil {
			continue
		}
		cfg, err := contracts.Resolve(plugin.Manifest.Config, os.Getenv)
		if err != nil {
			if mem != nil {
				_ = mem.Close()
			}
			return nil, nil, err
		}
		ApplyOrchestratorScope(&cfg, sess.Name, memProject, memAgent, sess.Extractor, sess.Journal, sess.ConsolidateEvery)
		orch, err := plugin.Orchestrator(ctx, cfg, mem)
		if err != nil {
			if mem != nil {
				_ = mem.Close()
			}
			return nil, nil, err
		}
		return orch, mem, nil
	}
	return nil, mem, nil
}
