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

// seedConsolidateTimeout caps the consolidation that closes a seed turn. It is
// deliberately separate from the turn's own cap: the two are different pieces of
// work, and sharing one deadline makes the memory write hostage to how long the
// model took to answer.
const seedConsolidateTimeout = 60 * time.Second

// seedTurnTimeout is the cap a seed turn runs under when nothing names another.
// A coordination seed is a short question; a turn started by hand from a terminal
// is not, which is what --timeout / HERRSCHER_SEED_TIMEOUT exist for.
const seedTurnTimeout = 120 * time.Second

// oneShotBackendFactory is a test seam for the one-shot bridge. The production
// factory resolves a remote backend when configured and otherwise builds the
// registered local backend with the session's backend settings.
var oneShotBackendFactory = newSeedBackend

// oneShotSeedRuntime carries the live daemon-only dependencies of a session
// seed. The operator CLI dispatches the same registry command without this
// request-scoped value, preserving its short-lived, coordination-free path.
// Keeping these dependencies on the command context avoids process-global
// mutable state when multiple seed requests overlap.
type oneShotSeedRuntime struct {
	coordinator contracts.Coordinator
	publish     func(session string, event contracts.Event)
	record      func(session string, entry state.TranscriptEntry)
	// timeout caps this turn; zero keeps seedTurnTimeout. It rides the runtime
	// because it is per-request like the rest of this struct, and the seed path
	// already threads exactly one such value through its three call layers.
	timeout time.Duration
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

// runOneShotSeedCommand sends an operator-process seed to the running daemon
// when one is available, because that process owns the live Coordinator. A
// daemon-side invocation already has coord and runs locally. With neither a
// daemon nor a coordinator it falls back to the historical uncoordinated
// one-shot behavior.
func runOneShotSeedCommand(ctx context.Context, st *state.State, name, task, turnID string, runtime oneShotSeedRuntime, coord contracts.Coordinator, instID string, forward seedCommandForwarder) (string, error) {
	// The coordinator can reach a seed two ways: the daemon path delivers it on
	// the request-scoped runtime (hub.Dispatch injects it via context), while a
	// boot-time slot (seedCoord) is the local fallback. Fold the slot into the
	// runtime so the seed runner always finds it in one place.
	if runtime.coordinator == nil {
		runtime.coordinator = coord
	}
	// No coordinator in this process (the operator CLI) → forward to the running
	// daemon, which owns the live Coordinator. The resolved turn identity travels
	// with the forward so both processes stamp the same turn.
	if runtime.coordinator == nil && forward != nil {
		argv := append([]string{"session", "seed", "--name", name}, cli.FlagArg("task", task)...)
		argv = append(argv, "--turn_id", turnID)
		// Only a settled timeout travels. The daemon runs this turn, so a silent
		// caller must leave the daemon's own default in force — sending a resolved
		// value unconditionally would make this process's environment decide a cap
		// for a turn it does not run.
		if runtime.timeout > 0 {
			argv = append(argv, "--timeout", runtime.timeout.String())
		}
		if reply, handled, err := forward(ctx, commandSocketTarget(instID), argv); handled {
			return reply, err
		}
	}
	return runOneShotSeedIDWithRuntime(ctx, st, name, task, turnID, runtime)
}

// runOneShotSeed builds the session-scoped orchestrator and delegates to the
// testable one-shot runner. Resolver.Orchestrator supplies a remote proxy when
// requested; otherwise the local plugin receives the session name and the
// persisted extractor/journal/cadence config in its PluginConfig.
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

// runOneShotSeedWith mounts the same in-process bridge turn used by the daemon:
// newSessionDriver owns the FIFO and SeedAndWait awaits reply{done}; bridge.RunOneShot
// supplies the registered backend over channels. Unlike RunSession/goLive this
// deliberately has no control socket, supervisor, or gateway binding.
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
	// The seed turn deliberately runs with a NIL coordinator so the pump
	// goroutine never reads d.coordinator while this turn writes it. Coordination
	// is applied after the pump is stopped and joined (see below).
	if runtime.record != nil {
		name := sess.Name
		d.sink.Transcript = func(entry state.TranscriptEntry) { runtime.record(name, entry) }
	}
	// Tap the seed turn onto the daemon's events socket (when one is serving) so
	// the app sees live thinking/status/chunk/reply. The seed path binds no
	// gateways, so this tap is the only way its events escape the process.
	if runtime.publish != nil {
		name := sess.Name
		d.emitTap = func(e contracts.Event) { runtime.publish(name, e) }
	}
	pumpCtx, pumpCancel := context.WithCancel(seedCtx)
	defer pumpCancel()
	pumpDone := make(chan struct{})
	go func() { defer close(pumpDone); d.pump(pumpCtx) }()

	var bridgeErr = make(chan error, 1)
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
		// The seed turn ran with a nil coordinator so the pump never re-enters
		// the dispatch mutex mid-turn; coordination happens here instead. Stop
		// the pump and join it before writing d.coordinator, so the field is
		// never read (turnloop) and written (here) from two goroutines at once.
		pumpCancel()
		<-pumpDone
		d.coordinator = runtime.coordinator
		// activeTurnID is stamped onto anything fanned out below so status/reply
		// events carry this turn's identity, matching the live path.
		d.activeTurnID = turnID
		// The reply already fanned out during the turn carried no Coordination
		// (the turn ran coordinator-free). Re-emit the terminal reply with the
		// coordination outcome so live subscribers still see it, mirroring the
		// live path where awaitTurn stamps Coordination onto the reply.
		if coordination := d.maybeCoordinate(seedCtx, reply); coordination != nil {
			d.fanOut(seedCtx, contracts.Event{T: "reply", Done: true, Text: reply, Coordination: coordination})
		}
		d.activeTurnID = ""
	}
	// A cadence (--consolidate-every N) means the orchestrator already
	// consolidated inside the turn. Doing it again here repeats the extraction
	// for nothing; the closing call exists for the manual case, where nothing
	// else ever writes what the turn learned.
	if orch != nil && sess.ConsolidateEvery <= 0 {
		// Consolidation gets its own budget, taken from the caller's context
		// rather than the turn's: seedCtx is what the turn was allowed to spend,
		// and a turn that spent it leaves nothing here. With --consolidate-every 1
		// the orchestrator already consolidates inside the turn, so this one
		// arrives on an exhausted deadline and fails every time.
		consCtx, consCancel := context.WithTimeout(ctx, seedConsolidateTimeout)
		started := time.Now()
		err := orch.Consolidate(consCtx)
		consCancel()
		if err != nil {
			// The elapsed time is what separates the two ways this fails: a budget
			// genuinely spent on a slow extractor, or a parent context that was
			// already dead when we got here — the latter returns in microseconds.
			// The turn answered; that is what the caller asked for. A memory
			// write that failed on the way out is worth saying and no more —
			// returning the error here would drop a finished reply, and under
			// --print would print nothing at all.
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
	ModelID string // empty = session predates the catalog, legacy path
}

// remoteBackendResolver is the remote-resolution seam BuildBackendFor consults.
type remoteBackendResolver interface {
	Backend(context.Context, []contracts.Plugin, ...string) (contracts.Backend, error)
}

// newBackendResolver builds that resolver. A package variable so tests can
// drive the remote branch without a live announcement bus.
var newBackendResolver = func() remoteBackendResolver {
	return NewResolver(nil, os.Getenv("HERRSCHER_NATS"))
}

// BuildBackendFor selects and constructs a backend. A remote resolver backend
// wins when configured; otherwise the matching registered plugin is built
// with the invocation, kind, working directory — and, if a ModelID is
// supplied, the environment variables its route requires.
func BuildBackendFor(ctx context.Context, req BackendRequest) (contracts.Backend, error) {
	desired := req.Vendor
	if desired == "" {
		desired = os.Getenv("HERRSCHER_BACKEND")
	}
	plugins := contracts.Default.Backends()

	// Resolve the model BEFORE touching the remote resolver: an unknown or
	// policy-excluded model must fail early, with a message that names it,
	// rather than on the first turn.
	var spawnEnv map[string]string
	var modelArg string
	// Defence in depth: the manager already refuses a modelless session under
	// gateway-only, but it is not the only caller here — a legacy state.json, a
	// seed, or a future verb can reach this with no model. A modelless spawn gets
	// no gateway environment, i.e. it runs on the machine's own vendor login,
	// which is precisely what gateway-only exists to forbid.
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
			// The requested vendor decides which plugin is built, but the spawn
			// environment is keyed off the model's OWNING vendor. Letting them
			// disagree spawns e.g. codex with ANTHROPIC_* it ignores: no gateway
			// redirection at all, the turn silently running on the machine's own
			// vendor login while the session still reads gateway-routed.
			return nil, fmt.Errorf("model %q belongs to backend %q, but backend %q was requested — pick a model offered by %q or switch the vendor", req.ModelID, entry.Vendor, desired, desired)
		}
		if spawnEnv, err = spawnEnvFor(entry, os.Getenv); err != nil {
			return nil, err
		}
		modelArg = entry.Arg
	}

	if backend, err := newBackendResolver().Backend(ctx, plugins, desired); err != nil {
		return nil, err
	} else if backend != nil {
		// A remote proxy is built from its announcement: there is no seam to hand
		// it the spawn environment resolved just above. Returning it would drop
		// the gateway credentials and run the turn on the machine's own vendor
		// login. Refuse explicitly instead of degrading silently.
		if len(spawnEnv) > 0 {
			return nil, fmt.Errorf("model %q needs a gateway spawn environment, which the remote backend resolver cannot carry — unset HERRSCHER_REMOTE=backend or pick a native model", req.ModelID)
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
	// The catalog's Arg is what actually selects the model: every backend
	// declares a "model" setting and passes cfg.Get("model") to its CLI as an
	// explicit flag. Without this the native route selects no model at all and
	// the codex gateway route runs codex's default model — billed to us.
	// Only set when a model was resolved, so a legacy session keeps whatever
	// the backend's own env binding (CLAUDE_MODEL, …) resolved.
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

// resolveBackendDir upgrades persisted relative session directories at the
// backend boundary. A supervised bridge may already be running inside that
// relative directory, so detect that suffix before joining it to cwd; otherwise
// the path would be applied twice (…/worker/…/worker).
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

// provisionSeedScope ensures the memory scope roots exist before a one-shot seed
// turn, keyed with the same contracts helpers the orchestrator derives its scope
// from so the keys cannot drift. It is the seed-path counterpart of the live
// bridge's provisionScope: best-effort (memory stays optional) and skipped for
// memories that cannot create nodes.
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
	// The seed turn scopes memory by the memory roots, not by the placement
	// fields: a TUI session sets MemoryProject/MemoryAgent and leaves
	// Project/Agent empty, and reading the latter here would file what the turn
	// learns under no root at all — written to the vault but linked to nothing,
	// so the next turn's recall never finds it.
	memProject, memAgent := sess.MemoryRoots()
	resolver := NewResolver(nil, os.Getenv("HERRSCHER_NATS"))
	mem, err := resolver.Memory(ctx, contracts.Default.Memories(), os.Getenv)
	if err != nil {
		return nil, nil, err
	}
	// Ensure the scope roots exist before the turn, mirroring the live bridge's
	// provisionScope. Without this a one-shot seed against a fresh vault fails at
	// the first Consolidate: RecordShared/RecordPrivate link candidates under the
	// project/agent roots, and the obsidian vault errors when those parent notes
	// are absent. Best-effort and plugin-agnostic — a memory that cannot create
	// nodes simply does not satisfy Provisioner and is skipped.
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
