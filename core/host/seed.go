package host

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
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
	"github.com/Herrscherd/herrscher/core/clitext"
	"github.com/Herrscherd/herrscher/core/eventwire"
	"github.com/Herrscherd/herrscher/core/internal/state"
	"github.com/Herrscherd/herrscher/core/usage"
)

const seedConsolidateTimeout = 60 * time.Second

const seedTurnTimeout = 120 * time.Second

const seedJoinGrace = 2 * time.Second

type oneShotSeedRuntime struct {
	messages        *hostMessages
	cliMessages     *clitext.Messages
	warnings        io.Writer
	sessionLanguage func(name string) languageSource
	coordinator     contracts.Coordinator
	publish         func(session string, event contracts.Event)
	record          func(session string, entry state.TranscriptEntry)
	recordUsage     func(sess state.Session, records []usage.Record)
	resume          func(session, token string)
	timeout         time.Duration
	backend         func(context.Context, state.Session) (contracts.Backend, error)
	models          *ModelDiscovery
	sessions        *sessionTable
	persists        *persistWrites
}

func (r oneShotSeedRuntime) backendFactory() func(context.Context, state.Session) (contracts.Backend, error) {
	if r.backend != nil {
		return r.backend
	}
	return func(ctx context.Context, sess state.Session) (contracts.Backend, error) {
		return newSeedBackend(ctx, r.messages, sess, nil, r.models)
	}
}

type oneShotSeedRuntimeKey struct{}

func withOneShotSeedRuntime(ctx context.Context, runtime oneShotSeedRuntime) context.Context {
	return context.WithValue(ctx, oneShotSeedRuntimeKey{}, runtime)
}

func oneShotSeedRuntimeFrom(ctx context.Context, messages *hostMessages, cliMessages *clitext.Messages, newOneShot func() *persistWrites) oneShotSeedRuntime {
	runtime, ok := ctx.Value(oneShotSeedRuntimeKey{}).(oneShotSeedRuntime)
	if !ok {
		return oneShotSeedRuntime{messages: messages, cliMessages: cliMessages, persists: newOneShot()}
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
	if sess.Archived {
		return "", errSeedArchivedSession
	}
	orch, mem, err := seedOrchestrator(ctx, runtime.messages, sess)
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
	driver := newSessionDriver(runtime.messages, runtime.cliMessages, sess.Name, nil, toBridge, fromBridge, runtime.warnings)
	oneShot := runtime.persists
	driver.persists = oneShot
	defer awaitOneShotWrites(runtime.messages, oneShot, runtime.warnings, runtime.languageOf(sess))
	driver.incarnation = sess.Incarnation
	driver.agent = sess.Agent
	driver.language = func() i18n.Language { return knownSessionLanguage(sess) }
	gate := attachOneShotTaps(driver, sess, runtime)
	defer gate.shut()
	runtime.sessions.attachCompaction(driver, sess.Name)
	terms := modelTermsFor(runtime.models)
	driver.terms = func() modelTerms { return terms(sess) }
	pumpCtx, pumpCancel := context.WithCancel(seedCtx)
	defer pumpCancel()
	pumpDone := make(chan struct{})
	go func() { defer close(pumpDone); driver.pump(pumpCtx) }()

	bridgeCtx, stopBridge := context.WithCancel(seedCtx)
	defer stopBridge()
	bridgeErr := make(chan error, 1)
	go runOneShotBridge(bridgeCtx, cancel, sess, orch, runtime, toBridge, fromBridge, bridgeErr)

	stopSeedWork := func() {
		pumpCancel()
		if !joinWithin(pumpDone, seedJoinGrace) {
			slog.Warn("seed pump outlived its grace", "session", sess.Name)
		}
		stopBridge()
	}
	reply, ok := driver.SeedAndWaitWithTurnID(seedCtx, task, turnID)
	if !ok {
		timeoutErr := oneShotTimeoutError(bridgeErr)
		stopSeedWork()
		return "", timeoutErr
	}
	driver.awaitTurnRelease(seedCtx)
	stopSeedWork()
	ended, bridgeRunErr := bridgeEndWithin(bridgeErr, seedJoinGrace)
	if !ended {
		slog.Warn("seed bridge outlived its grace", "session", sess.Name)
	}
	if reportsFailedTurn(bridgeRunErr) {
		return "", bridgeRunErr
	}
	if runtime.coordinator != nil {
		driver.coordinator = runtime.coordinator
		driver.activeTurnID = turnID
		if coordination := driver.maybeCoordinate(seedCtx, reply); coordination != nil {
			driver.fanOut(seedCtx, contracts.Event{T: eventwire.EventReply, Done: true, Text: reply, Coordination: coordination})
		}
		driver.activeTurnID = ""
	}
	consolidateOneShot(ctx, orch, gate, sess, runtime.messages, runtime.warnings, runtime.languageOf(sess))
	return reply, nil
}

func awaitOneShotWrites(messages *hostMessages, persists *persistWrites, warnings io.Writer, lang languageSource) {
	select {
	case <-persists.group.settled():
	case <-time.After(persists.grace):
		warnLive(messages, warnings, lang, "warn.persist_overrun", map[string]any{graceParam: persists.grace.String()})
	}
}

func oneShotTurnTimeout(runtime oneShotSeedRuntime) time.Duration {
	if runtime.timeout <= 0 {
		return seedTurnTimeout
	}
	return runtime.timeout
}

func attachOneShotTaps(driver *sessionDriver, sess state.Session, runtime oneShotSeedRuntime) *seedGate {
	gate := newSeedGate(runtime.messages, sess.Name, runtime.warnings, runtime.languageOf(sess))
	if runtime.record != nil {
		driver.sink.Transcript = func(entry state.TranscriptEntry) {
			gate.admit(lateTranscriptKey, func() { runtime.record(sess.Name, entry) })
		}
	}
	if runtime.publish != nil {
		driver.emitTap = func(e contracts.Event) {
			gate.admit(lateEventKey, func() { runtime.publish(sess.Name, e) })
		}
	}
	if runtime.recordUsage != nil {
		driver.sink.Usage = func(records []usage.Record) {
			gate.admit(lateUsageKey, func() { runtime.recordUsage(sess, records) })
		}
	}
	if runtime.resume != nil {
		driver.sink.Resume = func(token string) {
			gate.admit(lateResumeKey, func() { runtime.resume(sess.Name, token) })
		}
	}
	return gate
}

func runOneShotBridge(ctx context.Context, cancel context.CancelFunc, sess state.Session, orch contracts.Orchestrator, runtime oneShotSeedRuntime, toBridge, fromBridge chan contracts.Event, bridgeErr chan<- error) {
	err := bridge.RunUntilClosed(ctx, func(string) (contracts.Backend, error) {
		return runtime.backendFactory()(ctx, sess)
	}, orch, sess.ChannelID, toBridge, fromBridge)
	bridgeErr <- err
	if cancelsSeed(err) && ctx.Err() == nil {
		cancel()
	}
}

func cancelsSeed(err error) bool {
	return err != nil && !reportsFailedTurn(err)
}

func reportsFailedTurn(err error) bool {
	_, failedTurn := errors.AsType[*bridge.TurnFailedError](err)
	return failedTurn
}

func joinWithin(done <-chan struct{}, grace time.Duration) bool {
	select {
	case <-done:
		return true
	case <-time.After(grace):
		return false
	}
}

func bridgeEndWithin(bridgeErr <-chan error, grace time.Duration) (bool, error) {
	select {
	case err := <-bridgeErr:
		return true, err
	case <-time.After(grace):
		return false, nil
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

func consolidateOneShot(ctx context.Context, orch contracts.Orchestrator, gate *seedGate, sess state.Session, messages *hostMessages, warn io.Writer, lang languageSource) {
	if orch == nil || sess.ConsolidateEvery > 0 {
		return
	}
	gate.shut()
	consCtx, consCancel := context.WithTimeout(ctx, seedConsolidateTimeout)
	started := time.Now()
	err := orch.Consolidate(consCtx)
	consCancel()
	if err != nil {
		warnTo(messages, warn, lang(), "seed.consolidate", map[string]any{nameParam: sess.Name, "Elapsed": time.Since(started).Round(time.Millisecond).String(), detailParam: fmt.Sprint(err)})
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

func seedOrchestrator(ctx context.Context, messages *hostMessages, sess state.Session) (contracts.Orchestrator, contracts.Memory, error) {
	memProject, memAgent := sess.MemoryRoots()
	resolver := newResolver(messages, nil, os.Getenv("HERRSCHER_NATS"))
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
