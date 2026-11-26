package host

import (
	"context"
	"maps"
	"os"
	"path/filepath"
	"strings"

	contracts "github.com/Herrscherd/herrscher-contracts"
	"github.com/Herrscherd/herrscher/core/envx"
	"github.com/Herrscherd/herrscher/core/internal/control"
	"github.com/Herrscherd/herrscher/core/internal/lease"
	"github.com/Herrscherd/herrscher/core/internal/state"
	"github.com/Herrscherd/herrscher/core/keyproxy"
	"github.com/Herrscherd/herrscher/core/skills"
)

// BackendRequest is everything building a backend needs.
type BackendRequest struct {
	Vendor       string
	Cmd          string
	Kind         string
	Dir          string
	Resume       string
	ModelID      string
	Account      string
	AccountRoot  string
	Keys         *keyproxy.Proxy
	Models       *ModelDiscovery
	Session      string
	SessionToken string
	SkillRoot    string

	resolver remoteBackendResolver
}

func (req BackendRequest) remoteResolver(messages *hostMessages) remoteBackendResolver {
	if req.resolver != nil {
		return req.resolver
	}
	return newResolver(messages, nil, os.Getenv("HERRSCHER_NATS"))
}

type remoteBackendResolver interface {
	Backend(context.Context, []contracts.Plugin, ...string) (contracts.Backend, bool, error)
}

// BuildBackendFor selects and constructs a backend. A remote resolver backend
// wins when configured; otherwise the matching registered plugin is built
// with the invocation, kind, working directory, and, if a ModelID is
// supplied, the environment variables its route requires.
func BuildBackendFor(ctx context.Context, owner *Messages, req BackendRequest) (contracts.Backend, error) {
	return buildBackendFor(ctx, owner.owner, req)
}

func buildBackendFor(ctx context.Context, messages *hostMessages, req BackendRequest) (contracts.Backend, error) {
	backend, release, err := buildBackend(ctx, messages, req)
	if err != nil {
		return nil, err
	}
	return lease.Wrap(backend, release), nil
}

func buildBackend(ctx context.Context, messages *hostMessages, req BackendRequest) (contracts.Backend, func(), error) {
	plugins := contracts.Default.Backends()
	if req.ModelID == "" && ResolvePolicy(envx.Getenv) == contracts.PolicyGatewayOnly {
		return nil, noRelease, errGatewayOnlyNoModel
	}
	desired, entry, err := chooseVendor(req.Vendor, req.Cmd, req.ModelID, plugins, req.Models)
	if err != nil {
		return nil, noRelease, err
	}
	route, err := routeFor(messages, req.ModelID, entry, req.Keys)
	if err != nil {
		return nil, noRelease, err
	}
	failWith := func(err error) (contracts.Backend, func(), error) {
		route.lease()()
		return nil, noRelease, err
	}
	backend, fromRemote, err := req.remoteResolver(messages).Backend(ctx, plugins, desired)
	if err != nil {
		return failWith(err)
	}
	if fromRemote {
		if refusal := req.remoteRefusal(messages, route); refusal != nil {
			return failWith(refusal)
		}
		return remoteBackend{Backend: backend}, noRelease, nil
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
	env, release, err := sessionEnv(messages, req, desired, route)
	if err != nil {
		return failWith(err)
	}
	route.env = env
	req.Cmd = commandFor(plugin.Manifest.Kind, req.Cmd, plugins)
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

func routeFor(messages *hostMessages, modelID string, entry CatalogEntry, keys *keyproxy.Proxy) (modelRoute, error) {
	if modelID == "" {
		return modelRoute{}, nil
	}
	env, release, err := spawnEnvVia(messages, entry, os.Getenv, keys)
	if err != nil {
		return modelRoute{}, err
	}
	return modelRoute{env: env, arg: entry.Arg, grant: release}, nil
}

func (req BackendRequest) remoteRefusal(messages *hostMessages, route modelRoute) error {
	if len(route.env) > 0 {
		return &gatewayEnvUnsupportedError{messages: messages, Model: req.ModelID}
	}
	return req.remoteAccountRefusal(messages)
}

// RemoteAccountRefusal refuses an account on a remote backend, which resolves accounts in its own store.
func (req BackendRequest) RemoteAccountRefusal(owner *Messages) error {
	return req.remoteAccountRefusal(owner.owner)
}

func (req BackendRequest) remoteAccountRefusal(messages *hostMessages) error {
	if req.Account != "" {
		return &remoteAccountError{messages: messages, Account: req.Account}
	}
	return nil
}

func commandFor(vendor, cmd string, plugins []contracts.Plugin) string {
	if owner := vendorOfCmd(cmd, plugins); owner != "" && owner != vendor {
		return ""
	}
	return cmd
}

func applyBackendRequest(settings map[string]string, req BackendRequest, route modelRoute) {
	setIfPresent(settings, "cmd", req.Cmd)
	setIfPresent(settings, "kind", req.Kind)
	setIfPresent(settings, "dir", req.Dir)
	setIfPresent(settings, "resume", req.Resume)
	setIfPresent(settings, "skill-root", req.SkillRoot)
	setIfPresent(settings, "model", route.arg)
	if env := withSessionIdentity(route.env, req.Session, req.SessionToken); len(env) > 0 {
		settings["env"] = contracts.EncodeEnvSetting(env)
	}
}

func withSessionIdentity(env map[string]string, session, token string) map[string]string {
	if session == "" {
		return env
	}
	identity := make(map[string]string, len(env)+1)
	maps.Copy(identity, env)
	identity[control.SessionVar] = session
	if token != "" {
		identity[control.ApprovalKeyVar] = token
	}
	return identity
}

func setIfPresent(settings map[string]string, key, value string) {
	if value != "" {
		settings[key] = value
	}
}

func newSeedBackend(ctx context.Context, messages *hostMessages, sess state.Session, keys *keyproxy.Proxy, models *ModelDiscovery) (contracts.Backend, error) {
	return newSeedBackendWithToken(ctx, messages, sess, keys, models, "")
}

func newSeedBackendWithToken(ctx context.Context, messages *hostMessages, sess state.Session, keys *keyproxy.Proxy, models *ModelDiscovery, token string) (contracts.Backend, error) {
	dir := seedBackendDir(sess)
	return buildBackendFor(ctx, messages, BackendRequest{
		Vendor:       sess.Vendor,
		Cmd:          sess.Cmd,
		Kind:         sess.Backend,
		Dir:          dir,
		SkillRoot:    learnedHomeOf(seedRunDir(sess)),
		Resume:       sess.ResumeToken,
		ModelID:      sess.ModelID,
		Account:      sess.Account,
		Keys:         keys,
		Models:       models,
		Session:      sess.Name,
		SessionToken: token,
	})
}

func seedBackendDir(sess state.Session) string {
	dir := sess.Dir
	if dir == "" {
		dir = sess.Worktree
	}
	return resolveBackendDir(dir)
}

func seedRunDir(sess state.Session) string {
	if dir := seedBackendDir(sess); dir != "" {
		return dir
	}
	cwd, err := os.Getwd()
	if err != nil {
		return "."
	}
	return cwd
}

func learnedHomeOf(dir string) string {
	if dir == "" || skills.LinkedLearnedDir(dir) != "" {
		return ""
	}
	return skills.LearnedHome(dir)
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

// PreferredBackend names the backend kind the host puts first in the registry
// at startup: HERRSCHER_BACKEND when it is set, claude otherwise. Go
// initialises the compiled plugins in the order of their import paths, so
// without it the default backend would change whenever a backend whose path
// sorts first is compiled in.
func PreferredBackend(getenv func(string) string) string {
	if kind := strings.TrimSpace(getenv("HERRSCHER_BACKEND")); kind != "" {
		return kind
	}
	return vendorClaude
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
