package main

import (
	"context"
	"flag"
	"log/slog"
	"os"

	"github.com/Herrscherd/herrscher-contracts"
	"github.com/Herrscherd/herrscher/core/bridge"
	"github.com/Herrscherd/herrscher/core/host"
)

// runBridge runs a pure backend runner against the daemon hub: it dials the hub
// control socket (--hub-socket), takes one input frame per turn, and streams the
// backend's events back over the same connection — it does no gateway I/O itself.
// The backend is selected by --vendor from the plugin registry (Claude when
// unset); --backend picks the kind (stream|oneshot) and --cmd the invocation.
func runBridge(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("bridge", flag.ExitOnError)
	ch := channelFlag(fs)
	cmdStr := fs.String("cmd", "", "base command (default 'claude' in stream mode; the per-message program in one-shot mode)")
	// Accepted but ignored; kept so existing --stream/--model callers don't error.
	// Kind is selected by --backend; the model now rides in --cmd (or CLAUDE_MODEL).
	fs.Bool("stream", true, "deprecated, ignored (accepted for backward compatibility)")
	fs.String("model", "", "deprecated, ignored — pass the model via --cmd (e.g. 'claude --model …')")
	session := fs.String("session", "", "session name (scopes the orchestrator/attachment dir)")
	project := fs.String("project", "", "project name — the shared memory scope (P1: every agent of this game recalls it)")
	agent := fs.String("agent", "", "agent name — the private memory scope (P1: this agent's learned skills)")
	verbose := fs.Bool("v", false, "log activity to stderr")
	backend := fs.String("backend", "", "responder backend: stream (default) | oneshot")
	vendor := fs.String("vendor", "", "backend vendor: claude | codex | cursor (empty = first registered / HERRSCHER_BACKEND)")
	hubSocket := fs.String("hub-socket", "", "unix socket of the daemon hub: when set, run as a pure backend runner (no gateway polling)")
	agentsRoot := fs.String("agents-root", "", "directory holding agent homes for the delegation roster (empty = the daemon default beside state.json)")
	extractor := fs.String("extractor", "", "name of a registered curation extractor — enables the P1 learning loop (empty = plain Curator, no learning)")
	journal := fs.String("journal", "", "path to the call journal Consolidate reads (worktree-relative ok); only used with --extractor")
	consolidateEvery := fs.Int("consolidate-every", 0, "run Consolidate every N turns (0 = manual only); only used with --extractor")
	resume := fs.String("resume", "", "backend resume token to resume the conversation on start")
	fs.Parse(args)

	log := host.Logger(*verbose).With("component", "bridge", "session", *session)

	// The backend is the model edge: core never knows which model answers. When
	// HERRSCHER_REMOTE names "backend" the resolver returns an out-of-process
	// proxy; otherwise the shared host factory selects the requested vendor.
	br, err := newResolver(log)
	if err != nil {
		return err
	}
	newBackend := func(channelID string) (contracts.Backend, error) {
		if be, err := br.Backend(ctx, contracts.Default.Backends()); err != nil {
			return nil, err
		} else if be != nil {
			return be, nil
		}
		return host.BuildBackend(ctx, *vendor, *cmdStr, *backend, "", *resume)
	}

	mem := buildMemory(ctx, log)
	if mem != nil {
		defer mem.Close()
		provisionScope(ctx, mem, *project, *agent, log)
	}
	orch := buildOrchestrator(ctx, mem, *session, *project, *agent,
		learnConfig{extractor: *extractor, journal: *journal, consolidateEvery: *consolidateEvery}, log)
	if orch != nil {
		defer orch.Close()
	}
	rosterRoot := *agentsRoot
	if rosterRoot == "" {
		rosterRoot = host.DefaultAgentsRoot()
	}
	return bridge.Run(ctx, newBackend, orch, bridge.Options{
		Channel:   *ch,
		HubSocket: *hubSocket,
		Roster:    host.NewRoster(rosterRoot),
	})
}

// buildMemory instantiates the first registered memory plugin from the registry,
// or returns nil when none is compiled in or its config is unset. Memory is
// optional: a config/instantiation failure disables it (logged) rather than
// blocking the bridge. The vault self-provisions (its plugin defaults the path
// and creates the folder), so no env is required; OBSIDIAN_VAULT only overrides
// where it lives.
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

// provisionScope ensures this session's memory scope roots exist before the first
// turn, so B can record and A can recall from turn one. It is plugin-agnostic and
// best-effort: memory implementations that can create nodes satisfy
// contracts.Provisioner (the local obsidian vault does; a remote proxy may not
// and is skipped). It keys the roots with the same contracts helpers the
// orchestrator derives its scope from, so the keys cannot drift. Errors are
// logged, never fatal — memory stays optional, matching buildMemory.
func provisionScope(ctx context.Context, mem contracts.Memory, project, agent string, log *slog.Logger) {
	p, ok := mem.(contracts.Provisioner)
	if !ok {
		return
	}
	// Warn, not Debug: reaching here means a memory that can create nodes is
	// configured and a scope name was given, so a failure is unexpected and leaves
	// the root missing — every later RecordShared/RecallScoped then fails on the
	// absent node. Still non-fatal (memory is optional), but it must be visible.
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

// learnConfig is the opt-in P1 write side: when extractor names a registered
// curation extractor, the orchestrator builds a Learner that runs Consolidate
// over journal every `every` turns. A zero value keeps the plain Curator.
type learnConfig struct {
	extractor        string
	journal          string
	consolidateEvery int
}

// buildOrchestrator instantiates the first registered orchestrator plugin over
// mem (the conversation-policy edge), or returns nil when none is compiled in.
// The session name is threaded through the config bag (key "session") since it is
// runtime state, not env config. A config/instantiation failure disables it
// (logged) rather than blocking the bridge.
func buildOrchestrator(ctx context.Context, mem contracts.Memory, session, project, agent string, learn learnConfig, log *slog.Logger) contracts.Orchestrator {
	disabled := func(kind string, err error) contracts.Orchestrator {
		log.Debug("orchestrator disabled", "kind", kind, "err", err)
		return nil
	}
	// Remote (opt-in via HERRSCHER_REMOTE=orchestrator): resolve a proxy to an
	// out-of-process orchestrator, reusing the Stage A retry/timeout/metrics on
	// the resolver. The remote orchestrator owns its config in its plugin-host, so
	// the session/scope/learn bag below applies to the local path only. When
	// orchestrator is not remote the resolver returns (nil, nil) and we build
	// local as today.
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
		// Runtime state threaded through the config bag: the session scopes the
		// rolling transcript; project/agent scope the shared/private memory (P1);
		// a set extractor/journal/cadence flips the orchestrator into a learning
		// Learner. Shared with the one-shot seed path via ApplyOrchestratorScope.
		host.ApplyOrchestratorScope(&cfg, session, project, agent, learn.extractor, learn.journal, learn.consolidateEvery)
		orch, err := p.Orchestrator(ctx, cfg, mem)
		if err != nil {
			return disabled(p.Manifest.Kind, err)
		}
		return orch
	}
	return nil
}
