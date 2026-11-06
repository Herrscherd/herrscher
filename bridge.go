package main

import (
	"bufio"
	"context"
	"errors"
	"flag"
	"io"
	"log/slog"
	"os"
	"strconv"
	"strings"

	contracts "github.com/Herrscherd/herrscher-contracts"
	"github.com/Herrscherd/herrscher/core/bridge"
	"github.com/Herrscherd/herrscher/core/clitext"
	"github.com/Herrscherd/herrscher/core/config"
	"github.com/Herrscherd/herrscher/core/host"
	"github.com/Herrscherd/herrscher/core/keyproxy"
	"github.com/Herrscherd/herrscher/core/scope"
)

var errModelNotRoutable = errors.New("cannot be routed through the remote backend resolver")

func runBridge(ctx context.Context, args []string) error {
	translator := clitext.For(clitext.ClientLanguage())
	flags := flag.NewFlagSet("bridge", flag.ContinueOnError)
	channel := channelFlag(flags)
	backendFlags := declareBackendFlags(flags, translator)
	session := flags.String("session", "", translator.Text(clitext.BridgeFlagSession, nil))
	project := flags.String("project", "", translator.Text(clitext.BridgeFlagProject, nil))
	agent := flags.String("agent", "", translator.Text(clitext.BridgeFlagAgent, nil))
	projectPinned := flags.Bool("project-pinned", false, translator.Text(clitext.BridgeFlagProjectPinned, nil))
	verbose := flags.Bool("v", false, translator.Text(clitext.BridgeFlagVerbose, nil))
	hubSocket := flags.String("hub-socket", "", translator.Text(clitext.BridgeFlagHubSocket, nil))
	agentsRoot := flags.String("agents-root", "", translator.Text(clitext.BridgeFlagAgentsRoot, nil))
	extractor := flags.String("extractor", "", translator.Text(clitext.BridgeFlagExtractor, nil))
	journal := flags.String("journal", "", translator.Text(clitext.BridgeFlagJournal, nil))
	consolidateEvery := flags.Int("consolidate-every", 0, translator.Text(clitext.BridgeFlagConsolidateEvery, nil))
	statePath := flags.String("state", "", translator.Text(clitext.BridgeFlagState, nil))
	envStdin := flags.Bool("env-stdin", false, translator.Text(clitext.BridgeFlagEnvStdin, nil))
	if err := translator.Err(); err != nil {
		return err
	}
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
	host.CaptureAnnounceKey()

	log := host.Logger(*verbose).With("component", "bridge", "session", *session)

	resolver, err := newResolver(log)
	if err != nil {
		return err
	}
	cfg, err := config.Load(config.DefaultPath())
	if err != nil {
		return err
	}
	keys, err := keyproxy.Open(ctx, cfg.Keys, keyproxy.OSLoader(), log)
	if err != nil && !errors.Is(err, keyproxy.ErrDisabled) {
		return err
	}
	defer host.CloseKeyProxy(ctx, log, keys)
	requestFor := bridgeRequests(backendFlags, *statePath)
	newBackend := func(_ string) (contracts.Backend, error) {
		req := requestFor()
		req.Keys = keys
		return newBackendFor(ctx, resolver, req)
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
	account *string
}

func declareBackendFlags(flags *flag.FlagSet, translator *clitext.Translator) backendFlags {
	parsed := backendFlags{
		cmd:     flags.String("cmd", "", translator.Text(clitext.BridgeFlagCmd, nil)),
		kind:    flags.String("backend", "", translator.Text(clitext.BridgeFlagBackend, nil)),
		vendor:  flags.String("vendor", "", translator.Text(clitext.BridgeFlagVendor, nil)),
		resume:  flags.String("resume", "", translator.Text(clitext.BridgeFlagResume, nil)),
		modelID: flags.String("model", "", translator.Text(clitext.BridgeFlagModel, nil)),
		account: flags.String("account", "", translator.Text(clitext.BridgeFlagAccount, nil)),
	}
	flags.Bool("stream", true, translator.Text(clitext.BridgeFlagStream, nil))
	return parsed
}

func (f backendFlags) request(models *host.ModelDiscovery) host.BackendRequest {
	return host.BackendRequest{
		Vendor: *f.vendor, Cmd: *f.cmd, Kind: *f.kind, Resume: *f.resume, ModelID: *f.modelID, Account: *f.account,
		Models: models,
	}
}

func bridgeRequests(flags backendFlags, statePath string) func() host.BackendRequest {
	models := host.BridgeModelDiscovery(statePath)
	return func() host.BackendRequest {
		return flags.request(models)
	}
}

type backendResolver interface {
	Backend(context.Context, []contracts.Plugin, ...string) (contracts.Backend, bool, error)
}

func newBackendFor(ctx context.Context, br backendResolver, req host.BackendRequest) (contracts.Backend, error) {
	backend, err := resolveBackendFor(ctx, br, req)
	return backend, host.LocalizeError(clitext.ClientLanguage(), err)
}

func resolveBackendFor(ctx context.Context, br backendResolver, req host.BackendRequest) (contracts.Backend, error) {
	backend, fromRemote, err := br.Backend(ctx, contracts.Default.Backends())
	if err != nil {
		return nil, err
	}
	if fromRemote {
		if err := req.RemoteAccountRefusal(); err != nil {
			return nil, err
		}
		if req.ModelID != "" {
			return nil, renderError(clitext.ClientLanguage(), errModelNotRoutable, clitext.BridgeModelNotRoutable, clitext.Params{clitext.ParamModel: strconv.Quote(req.ModelID)})
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
			return renderError(clitext.ClientLanguage(), err, clitext.BridgeEnvStdinFailed, clitext.Params{clitext.ParamKey: key, clitext.ParamErr: err.Error()})
		}
	}
	return nil
}

func closeLogged(log *slog.Logger, what string, c io.Closer) {
	if err := c.Close(); err != nil {
		log.Warn("close failed", "what", what, "err", err)
	}
}
