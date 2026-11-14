package host

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
	"unicode"

	contracts "github.com/Herrscherd/herrscher-contracts"
	"github.com/Herrscherd/herrscher-contracts/i18n"
	"github.com/Herrscherd/herrscher/core/bridge"
	"github.com/Herrscherd/herrscher/core/cli"
	"github.com/Herrscherd/herrscher/core/envx"
	"github.com/Herrscherd/herrscher/core/internal/lease"
	"github.com/Herrscherd/herrscher/core/internal/state"
	"github.com/Herrscherd/herrscher/core/keyproxy"
	"github.com/Herrscherd/herrscher/core/usage"
)

const seedConsolidateTimeout = 60 * time.Second

const seedTurnTimeout = 120 * time.Second

type oneShotSeedRuntime struct {
	warnings        io.Writer
	sessionLanguage func(name string) languageSource
	coordinator     contracts.Coordinator
	publish         func(session string, event contracts.Event)
	record          func(session string, entry state.TranscriptEntry)
	recordUsage     func(sess state.Session, records []usage.Record)
	timeout         time.Duration
	backend         func(context.Context, state.Session) (contracts.Backend, error)
	models          *ModelDiscovery
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

func runOneShotSeedCommand(ctx context.Context, hostState *state.State, name, task, turnID string, runtime oneShotSeedRuntime, coord contracts.Coordinator, layout RuntimeLayout, instID string, forward seedCommandForwarder) (string, error) {
	if runtime.coordinator == nil {
		runtime.coordinator = coord
	}
	if runtime.coordinator == nil && forward != nil {
		argv := append([]string{nsSession, verbSeed, "--name", name}, cli.FlagArg("task", task)...)
		argv = append(argv, "--turn_id", turnID)
		if runtime.timeout > 0 {
			argv = append(argv, "--timeout", runtime.timeout.String())
		}
		if reply, handled, err := forward(ctx, commandSocketTarget(layout, instID), argv); handled {
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
	driver := newSessionDriver(sess.Name, nil, toBridge, fromBridge, runtime.warnings)
	driver.incarnation = sess.Incarnation
	driver.agent = sess.Agent
	driver.language = func() i18n.Language { return knownSessionLanguage(sess) }
	attachOneShotTaps(driver, sess, runtime)
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
	consolidateOneShot(ctx, orch, sess, runtime.warnings, runtime.languageOf(sess))
	return reply, nil
}

func oneShotTurnTimeout(runtime oneShotSeedRuntime) time.Duration {
	if runtime.timeout <= 0 {
		return seedTurnTimeout
	}
	return runtime.timeout
}

func attachOneShotTaps(driver *sessionDriver, sess state.Session, runtime oneShotSeedRuntime) {
	if runtime.record != nil {
		driver.sink.Transcript = func(entry state.TranscriptEntry) { runtime.record(sess.Name, entry) }
	}
	if runtime.publish != nil {
		driver.emitTap = func(e contracts.Event) { runtime.publish(sess.Name, e) }
	}
	if runtime.recordUsage != nil {
		driver.sink.Usage = func(records []usage.Record) { runtime.recordUsage(sess, records) }
	}
}

func runOneShotBridge(ctx context.Context, cancel context.CancelFunc, sess state.Session, orch contracts.Orchestrator, runtime oneShotSeedRuntime, toBridge, fromBridge chan contracts.Event, bridgeErr chan<- error) {
	err := bridge.RunOneShot(ctx, func(string) (contracts.Backend, error) {
		return runtime.backendFactory()(ctx, sess)
	}, orch, sess.ChannelID, toBridge, fromBridge)
	bridgeErr <- err
	if _, failedTurn := errors.AsType[*bridge.TurnFailedError](err); err != nil && !failedTurn {
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

func (r oneShotSeedRuntime) languageOf(sess state.Session) languageSource {
	if r.sessionLanguage != nil {
		return r.sessionLanguage(sess.Name)
	}
	return func() i18n.Language { return knownSessionLanguage(sess) }
}

func consolidateOneShot(ctx context.Context, orch contracts.Orchestrator, sess state.Session, warn io.Writer, lang languageSource) {
	if orch == nil || sess.ConsolidateEvery > 0 {
		return
	}
	consCtx, consCancel := context.WithTimeout(ctx, seedConsolidateTimeout)
	started := time.Now()
	err := orch.Consolidate(consCtx)
	consCancel()
	if err != nil {
		warnTo(warn, lang(), "seed.consolidate", map[string]any{nameParam: sess.Name, "Elapsed": time.Since(started).Round(time.Millisecond).String(), detailParam: fmt.Sprint(err)})
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
	desired, entry, err := chooseVendor(req.Vendor, req.Cmd, req.ModelID, plugins, req.Models)
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

func chooseVendor(requested, cmd, modelID string, plugins []contracts.Plugin, models *ModelDiscovery) (string, CatalogEntry, error) {
	if modelID == "" {
		return firstNonEmpty(requested, vendorOfCmd(cmd, plugins), os.Getenv("HERRSCHER_BACKEND")), CatalogEntry{}, nil
	}
	vendor := requested
	if vendor == "" {
		vendor = os.Getenv("HERRSCHER_BACKEND")
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

func vendorOfCmd(cmd string, plugins []contracts.Plugin) string {
	first := commandBinary(commandWords(cmd))
	if first == "" {
		return ""
	}
	binary := filepath.Base(first)
	for idx := range plugins {
		plugin := &plugins[idx]
		if plugin.Backend == nil {
			continue
		}
		for _, setting := range plugin.Manifest.Config {
			if setting.Key == "cmd" && setting.Default == binary {
				return plugin.Manifest.Kind
			}
		}
	}
	return ""
}

func commandWords(cmd string) []string {
	var words []string
	var word strings.Builder
	var quote rune
	inWord := false
	for _, char := range cmd {
		switch {
		case quote != 0:
			if char == quote {
				quote = 0
			} else {
				word.WriteRune(char)
			}
		case char == '\'' || char == '"':
			quote = char
			inWord = true
		case unicode.IsSpace(char):
			if inWord {
				words = append(words, word.String())
				word.Reset()
				inWord = false
			}
		default:
			word.WriteRune(char)
			inWord = true
		}
	}
	if inWord {
		words = append(words, word.String())
	}
	return words
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

func commandBinary(words []string) string {
	return envNamedCommand(words, 0)
}

func skipEnvAssignments(words []string) int {
	idx := 0
	for idx < len(words) && isEnvAssignment(words[idx]) {
		idx++
	}
	return idx
}

const envNestingLimit = 8

func envCommandAt(args []string, depth int) string {
	if depth > envNestingLimit {
		return ""
	}
	for idx := 0; idx < len(args); idx++ {
		word := args[idx]
		switch {
		case word == "--":
			return envNamedCommand(args[idx+1:], depth)
		case word == "--unset" || word == "--chdir":
			idx++
		case word == "--split-string" || strings.HasPrefix(word, "--split-string="):
			return envSplitLong(word, args[idx+1:], depth+1)
		case strings.HasPrefix(word, "--"):
		case word == "-":
		case strings.HasPrefix(word, "-") && len(word) > 1:
			if at := shortEnvSplitAt(word); at > 0 {
				return envSplitCommand(word[at+1:], args[idx+1:], depth+1)
			}
			if shortEnvOptionTakesNext(word) {
				idx++
			}
		case isEnvAssignment(word):
		default:
			return envNamedCommand(args[idx:], depth)
		}
	}
	return ""
}

func envSplitLong(word string, rest []string, depth int) string {
	if value, inline := strings.CutPrefix(word, "--split-string="); inline {
		return envCommandAt(append(commandWords(value), rest...), depth)
	}
	if len(rest) == 0 {
		return ""
	}
	return envCommandAt(append(commandWords(rest[0]), rest[1:]...), depth)
}

func envNamedCommand(words []string, depth int) string {
	idx := skipEnvAssignments(words)
	if idx >= len(words) {
		return ""
	}
	if filepath.Base(words[idx]) == "env" {
		return envCommandAt(words[idx+1:], depth+1)
	}
	return words[idx]
}

func envSplitCommand(value string, rest []string, depth int) string {
	if value == "" {
		if len(rest) == 0 {
			return ""
		}
		value, rest = rest[0], rest[1:]
	}
	return envCommandAt(append(commandWords(value), rest...), depth)
}

func shortEnvSplitAt(word string) int {
	for idx := 1; idx < len(word); idx++ {
		switch word[idx] {
		case 'S':
			return idx
		case 'u', 'C':
			return -1
		}
	}
	return -1
}

func shortEnvOptionTakesNext(word string) bool {
	for idx := 1; idx < len(word); idx++ {
		if word[idx] == 'u' || word[idx] == 'C' {
			return idx == len(word)-1
		}
	}
	return false
}

func isEnvAssignment(word string) bool {
	name, _, found := strings.Cut(word, "=")
	return found && name != "" && !strings.HasPrefix(word, "-")
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
