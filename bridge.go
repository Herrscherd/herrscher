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

func runBridge(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("bridge", flag.ContinueOnError)
	ch := channelFlag(fs)
	backendFlags := declareBackendFlags(fs)
	session := fs.String("session", "", "session name (scopes the orchestrator/attachment dir)")
	project := fs.String("project", "", "project name, the shared memory scope (P1: every agent of this game recalls it)")
	agent := fs.String("agent", "", "agent name, the private memory scope (P1: this agent's learned skills)")
	projectPinned := fs.Bool("project-pinned", false, "the memory project was chosen by a human, not guessed from a directory: never revise it")
	verbose := fs.Bool("v", false, "log activity to stderr")
	hubSocket := fs.String("hub-socket", "", "unix socket of the daemon hub: when set, run as a pure backend runner (no gateway polling)")
	agentsRoot := fs.String("agents-root", "", "directory holding agent homes for the delegation roster (empty = the daemon default beside state.json)")
	extractor := fs.String("extractor", "", "name of a registered curation extractor. It enables the P1 learning loop (empty = plain Curator, no learning)")
	journal := fs.String("journal", "", "path to the call journal Consolidate reads (worktree-relative ok); only used with --extractor")
	consolidateEvery := fs.Int("consolidate-every", 0, "run Consolidate every N turns (0 = manual only); only used with --extractor")
	envStdin := fs.Bool("env-stdin", false, "read KEY=VALUE lines from stdin until a blank line and put them in this process's environment (how a remote bridge gets its gateway credentials)")
	if err := fs.Parse(args); err != nil {
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

	br, err := newResolver(log)
	if err != nil {
		return err
	}
	newBackend := func(channelID string) (contracts.Backend, error) {
		return newBackendFor(ctx, br, backendFlags.request())
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
		Channel:       *ch,
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

func declareBackendFlags(fs *flag.FlagSet) backendFlags {
	f := backendFlags{
		cmd:     fs.String("cmd", "", "base command (default 'claude' in stream mode; the per-message program in one-shot mode)"),
		kind:    fs.String("backend", "", "responder backend: stream (default) | oneshot"),
		vendor:  fs.String("vendor", "", "backend vendor: claude | codex | cursor (empty = first registered / HERRSCHER_BACKEND)"),
		resume:  fs.String("resume", "", "backend resume token to resume the conversation on start"),
		modelID: fs.String("model", "", "catalog model id (see `herrscher models list`); empty = legacy path, the model rides in --cmd"),
	}
	fs.Bool("stream", true, "deprecated, ignored (accepted for backward compatibility)")
	return f
}

func (f backendFlags) request() host.BackendRequest {
	return host.BackendRequest{
		Vendor: *f.vendor, Cmd: *f.cmd, Kind: *f.kind, Resume: *f.resume, ModelID: *f.modelID,
	}
}

type backendResolver interface {
	Backend(context.Context, []contracts.Plugin, ...string) (contracts.Backend, error)
}

func newBackendFor(ctx context.Context, br backendResolver, req host.BackendRequest) (contracts.Backend, error) {
	be, err := br.Backend(ctx, contracts.Default.Backends())
	if err != nil {
		return nil, err
	}
	if be != nil {
		if req.ModelID != "" {
			return nil, fmt.Errorf("model %q cannot be routed through the remote backend resolver (HERRSCHER_REMOTE=backend): the proxy cannot carry the model's spawn environment", req.ModelID)
		}
		return be, nil
	}
	return host.BuildBackendFor(ctx, req)
}

func buildMemory(ctx context.Context, log *slog.Logger) contracts.Memory {
	disabled := func(kind string, err error) contracts.Memory {
		log.Debug("memory disabled", "kind", kind, "err", err)
		return nil
	}
	r, err := newResolver(log)
	if err != nil {
		return disabled("memory", err)
	}
	mem, err := r.Memory(ctx, contracts.Default.Memories(), os.Getenv)
	if err != nil {
		return disabled("memory", err)
	}
	return mem
}

func provisionScope(ctx context.Context, mem contracts.Memory, project, agent string, log *slog.Logger) {
	p, ok := mem.(contracts.Provisioner)
	if !ok {
		return
	}
	if project != "" {
		if err := p.EnsureProject(ctx, contracts.ProjectKey(project), project); err != nil {
			log.Warn("ensure project root", "project", project, "err", err)
		}
	}
	if agent != "" {
		if err := p.EnsureAgent(ctx, contracts.AgentKey(agent), agent); err != nil {
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
	r, err := newResolver(log)
	if err != nil {
		return disabled("orchestrator", err)
	}
	if orch, err := r.Orchestrator(ctx, contracts.Default.Orchestrators()); err != nil {
		return disabled("orchestrator", err)
	} else if orch != nil {
		return orch
	}
	for _, p := range contracts.Default.Orchestrators() {
		if p.Orchestrator == nil {
			continue
		}
		cfg, err := contracts.Resolve(p.Manifest.Config, os.Getenv)
		if err != nil {
			return disabled(p.Manifest.Kind, err)
		}
		host.ApplyOrchestratorScope(&cfg, session, project, agent, learn.extractor, learn.journal, learn.consolidateEvery)
		orch, err := p.Orchestrator(ctx, cfg, mem)
		if err != nil {
			return disabled(p.Manifest.Kind, err)
		}
		return orch
	}
	return nil
}

func applyEnvFromStdin(r io.Reader) error {
	br := bufio.NewReader(r)
	var block strings.Builder
	for {
		line, err := br.ReadString('\n')
		trimmed := strings.TrimRight(line, "\r\n")
		if trimmed != "" {
			block.WriteString(trimmed)
			block.WriteByte('\n')
		} else if line != "" {
			break
		}
		if err != nil {
			break
		}
	}
	for k, v := range contracts.ParseEnvSetting(block.String()) {
		if err := os.Setenv(k, v); err != nil {
			return fmt.Errorf("apply %s from stdin: %w", k, err)
		}
	}
	return nil
}

func closeLogged(log *slog.Logger, what string, c io.Closer) {
	if err := c.Close(); err != nil {
		log.Warn("close failed", "what", what, "err", err)
	}
}
