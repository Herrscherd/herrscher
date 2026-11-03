package main

import (
	"bufio"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"
	"strings"

	contracts "github.com/Herrscherd/herrscher-contracts"
	"github.com/Herrscherd/herrscher/core/bridge"
	"github.com/Herrscherd/herrscher/core/host"
	"github.com/Herrscherd/herrscher/core/scope"
)

var errModelNotRoutable = errors.New("cannot be routed through the remote backend resolver")

func runBridge(ctx context.Context, args []string) error {
	flags := flag.NewFlagSet("bridge", flag.ContinueOnError)
	channel := channelFlag(flags)
	backendFlags := declareBackendFlags(flags)
	session := flags.String("session", "", "session name (scopes the orchestrator/attachment dir)")
	project := flags.String("project", "", "project name, the shared memory scope (P1: every agent of this game recalls it)")
	agent := flags.String("agent", "", "agent name, the private memory scope (P1: this agent's learned skills)")
	projectPinned := flags.Bool("project-pinned", false, "the memory project was chosen by a human, not guessed from a directory: never revise it")
	verbose := flags.Bool("v", false, "log activity to stderr")
	hubSocket := flags.String("hub-socket", "", "unix socket of the daemon hub: when set, run as a pure backend runner (no gateway polling)")
	agentsRoot := flags.String("agents-root", "", "directory holding agent homes for the delegation roster (empty = the daemon default beside state.json)")
	extractor := flags.String("extractor", "", "name of a registered curation extractor. It enables the P1 learning loop (empty = plain Curator, no learning)")
	journal := flags.String("journal", "", "path to the call journal Consolidate reads (worktree-relative ok); only used with --extractor")
	consolidateEvery := flags.Int("consolidate-every", 0, "run Consolidate every N turns (0 = manual only); only used with --extractor")
	envStdin := flags.Bool("env-stdin", false, "read KEY=VALUE lines from stdin until a blank line and put them in this process's environment (how a remote bridge gets its gateway credentials)")
	if err := flags.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}

	if *envStdin {
		if err := applyEnvFromStdin(os.Stdin); err != nil {
			return err
		}
	}

	host.CaptureGatewayCreds()

	log := host.Logger(*verbose).With("component", "bridge", "session", *session)

	resolver, err := newResolver(log)
	if err != nil {
		return err
	}
	newBackend := func(_ string) (contracts.Backend, error) {
		return newBackendFor(ctx, resolver, backendFlags.request())
	}

	mem := buildMemory(ctx, log)
	if mem != nil {
		defer closeLogged(log, "memory", mem)
		provisionScope(ctx, mem, *project, *agent, log)
	}
	orch := buildOrchestrator(ctx, mem, *session, *project, *agent,
		learnConfig{extractor: *extractor, journal: *journal, consolidateEvery: *consolidateEvery}, log)
	if orch != nil {
		defer closeLogged(log, "orchestrator", orch)
	}
	rosterRoot := *agentsRoot
	if rosterRoot == "" {
		rosterRoot = host.DefaultAgentsRoot()
	}
	opts := bridge.Options{
		Channel:       *channel,
		HubSocket:     *hubSocket,
		Roster:        host.NewRoster(rosterRoot),
		Capabilities:  os.Getenv(host.EnvCapabilities),
		LaunchProject: *project,
		ProjectPinned: *projectPinned,
		MemoryAgent:   *agent,
	}
	if mem != nil {
		opts.Scope = vaultScopeResolver{mem: mem, log: log}
	}
	return bridge.Run(ctx, newBackend, orch, opts)
}

type backendFlags struct {
	cmd     *string
	kind    *string
	vendor  *string
	resume  *string
	modelID *string
}

func declareBackendFlags(flags *flag.FlagSet) backendFlags {
	parsed := backendFlags{
		cmd:     flags.String("cmd", "", "base command (default 'claude' in stream mode; the per-message program in one-shot mode)"),
		kind:    flags.String("backend", "", "responder backend: stream (default) | oneshot"),
		vendor:  flags.String("vendor", "", "backend vendor: claude | codex | cursor (empty = first registered / HERRSCHER_BACKEND)"),
		resume:  flags.String("resume", "", "backend resume token to resume the conversation on start"),
		modelID: flags.String("model", "", "catalog model id (see `herrscher models list`); empty = legacy path, the model rides in --cmd"),
	}
	flags.Bool("stream", true, "deprecated, ignored (accepted for backward compatibility)")
	return parsed
}

func (f backendFlags) request() host.BackendRequest {
	return host.BackendRequest{
		Vendor: *f.vendor, Cmd: *f.cmd, Kind: *f.kind, Resume: *f.resume, ModelID: *f.modelID,
	}
}

type backendResolver interface {
	Backend(context.Context, []contracts.Plugin, ...string) (contracts.Backend, bool, error)
}

func newBackendFor(ctx context.Context, br backendResolver, req host.BackendRequest) (contracts.Backend, error) {
	backend, fromRemote, err := br.Backend(ctx, contracts.Default.Backends())
	if err != nil {
		return nil, err
	}
	if fromRemote {
		if req.ModelID != "" {
			return nil, fmt.Errorf("model %q %w (HERRSCHER_REMOTE=backend): the proxy cannot carry the model's spawn environment", req.ModelID, errModelNotRoutable)
		}
		return backend, nil
	}
	return host.BuildBackendFor(ctx, req)
}

func buildMemory(ctx context.Context, log *slog.Logger) contracts.Memory {
	disabled := func(kind string, err error) contracts.Memory {
		log.Debug("memory disabled", "kind", kind, "err", err)
		return nil
	}
	resolver, err := newResolver(log)
	if err != nil {
		return disabled("memory", err)
	}
	mem, _, err := resolver.Memory(ctx, contracts.Default.Memories(), os.Getenv)
	if err != nil {
		return disabled("memory", err)
	}
	return mem
}

func provisionScope(ctx context.Context, mem contracts.Memory, project, agent string, log *slog.Logger) {
	provisioner, ok := mem.(contracts.Provisioner)
	if !ok {
		return
	}
	if project != "" {
		if err := provisioner.EnsureProject(ctx, contracts.ProjectKey(project), project); err != nil {
			log.Warn("ensure project root", "project", project, "err", err)
		}
	}
	if agent != "" {
		if err := provisioner.EnsureAgent(ctx, contracts.AgentKey(agent), agent); err != nil {
			log.Warn("ensure agent root", "agent", agent, "err", err)
		}
	}
}

type vaultScopeResolver struct {
	mem contracts.Memory
	log *slog.Logger
}

func (r vaultScopeResolver) Resolve(ctx context.Context, prompt string) string {
	nodes, err := r.mem.Search(ctx, contracts.Query{Kinds: []contracts.NodeKind{contracts.KindProject}})
	if err != nil {
		r.log.Warn("list known projects", "err", err)
		return ""
	}
	known := make([]string, 0, len(nodes))
	for _, n := range nodes {
		known = append(known, strings.TrimPrefix(n.Key, "projects/"))
	}
	chosen := scope.MatchProject(prompt, known)
	if chosen == "" {
		return ""
	}
	if p, ok := r.mem.(contracts.Provisioner); ok {
		if err := p.EnsureProject(ctx, contracts.ProjectKey(chosen), chosen); err != nil {
			r.log.Warn("ensure project root", "project", chosen, "err", err)
		}
	}
	return chosen
}

type learnConfig struct {
	extractor        string
	journal          string
	consolidateEvery int
}

func buildOrchestrator(ctx context.Context, mem contracts.Memory, session, project, agent string, learn learnConfig, log *slog.Logger) contracts.Orchestrator {
	disabled := func(kind string, err error) contracts.Orchestrator {
		log.Debug("orchestrator disabled", "kind", kind, "err", err)
		return nil
	}
	resolver, err := newResolver(log)
	if err != nil {
		return disabled("orchestrator", err)
	}
	if orch, resolved, err := resolver.Orchestrator(ctx, contracts.Default.Orchestrators()); err != nil {
		return disabled("orchestrator", err)
	} else if resolved {
		return orch
	}
	orchestrators := contracts.Default.Orchestrators()
	for i := range orchestrators {
		plugin := &orchestrators[i]
		if plugin.Orchestrator == nil {
			continue
		}
		cfg, err := contracts.Resolve(plugin.Manifest.Config, os.Getenv)
		if err != nil {
			return disabled(plugin.Manifest.Kind, err)
		}
		host.ApplyOrchestratorScope(&cfg, session, project, agent, learn.extractor, learn.journal, learn.consolidateEvery)
		orch, err := plugin.Orchestrator(ctx, cfg, mem)
		if err != nil {
			return disabled(plugin.Manifest.Kind, err)
		}
		return orch
	}
	return nil
}

func applyEnvFromStdin(r io.Reader) error {
	reader := bufio.NewReader(r)
	var block strings.Builder
	for {
		line, err := reader.ReadString('\n')
		trimmed := strings.TrimRight(line, "\r\n")
		if trimmed != "" {
			block.WriteString(trimmed)
			_ = block.WriteByte('\n')
		} else if line != "" {
			break
		}
		if err != nil {
			break
		}
	}
	for key, v := range contracts.ParseEnvSetting(block.String()) {
		if err := os.Setenv(key, v); err != nil {
			return fmt.Errorf("apply %s from stdin: %w", key, err)
		}
	}
	return nil
}

func closeLogged(log *slog.Logger, what string, c io.Closer) {
	if err := c.Close(); err != nil {
		log.Warn("close failed", "what", what, "err", err)
	}
}
