package host

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"time"

	"github.com/nats-io/nats.go"
	"google.golang.org/grpc/credentials"

	contracts "github.com/Herrscherd/herrscher-contracts"
	transport "github.com/Herrscherd/herrscher-transport"
	"github.com/Herrscherd/herrscher/core/internal/metrics"
	"github.com/Herrscherd/herrscher/core/internal/obs"
)

const (
	defaultRetryAttempts  = 5
	defaultRetryBudget    = 20 * time.Second
	defaultAttemptTimeout = 5 * time.Second
	retryBackoffBase      = 200 * time.Millisecond
	retryBackoffMax       = 2 * time.Second
)

// Resolver turns registered plugins into live port objects, choosing local
// (in-proc factory) or remote (gRPC proxy) per category. With a nil/empty
// remote set every category resolves local; today's behaviour.
type Resolver struct {
	remote  map[contracts.Category]bool
	NatsURL string

	creds credentials.TransportCredentials

	log     *slog.Logger
	metrics *metrics.Registry

	retryAttempts    int
	retryBudget      time.Duration
	attemptTimeout   time.Duration
	dialMemory       func(context.Context, contracts.Plugin) (contracts.Memory, error)
	dialOrchestrator func(context.Context, contracts.Plugin) (contracts.Orchestrator, error)
	dialBackend      func(context.Context, contracts.Plugin) (contracts.Backend, error)
	now              func() time.Time
	sleep            func(time.Duration) <-chan time.Time
}

// NewResolver builds a Resolver for the categories in remote, reaching them over NATS at natsURL.
func NewResolver(remote map[contracts.Category]bool, natsURL string) *Resolver {
	resolver := &Resolver{
		remote:         remote,
		NatsURL:        natsURL,
		log:            obs.NewLogger(os.Stderr, slog.LevelInfo),
		retryAttempts:  defaultRetryAttempts,
		retryBudget:    defaultRetryBudget,
		attemptTimeout: defaultAttemptTimeout,
		now:            time.Now,
		sleep:          time.After,
	}
	resolver.dialMemory = resolver.dialRemoteMemoryOnce
	resolver.dialOrchestrator = resolver.dialRemoteOrchestratorOnce
	resolver.dialBackend = resolver.dialRemoteBackendOnce
	return resolver
}

// SetLogger installs the operator logger remote-resolve diagnostics flow through
// (component=resolver is attached for filtering).
func (r *Resolver) SetLogger(l *slog.Logger) {
	r.log = l.With("component", "resolver")
}

func (r *Resolver) warnUndecodableAnnouncement(err error) {
	r.log.Warn("ignoring an announcement that does not decode", "err", err)
}

// SetMetrics installs the registry remote-resolve attempts/failures/latency are
// recorded into.
func (r *Resolver) SetMetrics(m *metrics.Registry) {
	r.metrics = m
}

// SetCredentials installs the transport credentials the remote dial authenticates
// with. nil keeps plaintext loopback (the default); an mTLS credential is required
// to dial a category running on another host.
func (r *Resolver) SetCredentials(c credentials.TransportCredentials) {
	r.creds = c
}

func (r *Resolver) isRemote(c contracts.Category) bool {
	return r.remote != nil && r.remote[c]
}

// Memory resolves the first registered memory plugin. Local: call the factory.
// Remote: dial a gRPC proxy via NATS announcements, retried within a deadline.
// Returns (nil, false, nil) when none is registered, memory stays optional,
// matching buildMemory's contract.
func (r *Resolver) Memory(ctx context.Context, plugins []contracts.Plugin, getenv func(string) string) (contracts.Memory, bool, error) {
	for idx := range plugins {
		plugin := plugins[idx]
		if plugin.Memory == nil {
			continue
		}
		if r.isRemote(contracts.CategoryMemory) {
			selected := plugin
			return resolved(resolveRemoteWithRetry(ctx, r, contracts.CategoryMemory,
				func(c context.Context) (contracts.Memory, error) { return r.dialMemory(c, selected) }))
		}
		cfg, err := contracts.Resolve(plugin.Manifest.Config, getenv)
		if err != nil {
			return nil, false, err
		}
		return resolved(plugin.Memory(ctx, cfg))
	}
	return nil, false, nil
}

func resolved[T any](v T, err error) (T, bool, error) {
	return v, err == nil, err
}

// Orchestrator resolves a REMOTE orchestrator proxy when HERRSCHER_REMOTE names
// "orchestrator", reusing the same retry/timeout/metrics harness as Memory. It
// returns (nil, false, nil) when the category is not remote: the local orchestrator
// needs runtime state the resolver does not hold (the in-process Memory plus the
// session/scope/learn config bag), so the caller builds the local one itself.
func (r *Resolver) Orchestrator(ctx context.Context, plugins []contracts.Plugin) (contracts.Orchestrator, bool, error) {
	if !r.isRemote(contracts.CategoryOrchestrator) {
		return nil, false, nil
	}
	for idx := range plugins {
		plugin := plugins[idx]
		if plugin.Orchestrator == nil {
			continue
		}
		return resolved(resolveRemoteWithRetry(ctx, r, contracts.CategoryOrchestrator,
			func(c context.Context) (contracts.Orchestrator, error) { return r.dialOrchestrator(c, plugin) }))
	}
	return nil, false, nil
}

// Backend resolves a REMOTE backend proxy when HERRSCHER_REMOTE names "backend",
// reusing the same retry/timeout/metrics harness. Like Orchestrator it returns
// (nil, false, nil) when the category is not remote, leaving the caller to build the
// local backend (which closes over model config the resolver does not hold).
// The remote proxy streams turn events over gRPC and surfaces a stream loss as a
// Respond error so the turn loop abandons the in-flight turn.
func (r *Resolver) Backend(ctx context.Context, plugins []contracts.Plugin, desired ...string) (contracts.Backend, bool, error) {
	if !r.isRemote(contracts.CategoryBackend) {
		return nil, false, nil
	}
	kind := ""
	if len(desired) > 0 {
		kind = desired[0]
	}
	plugin, err := selectBackend(kind, plugins)
	if err != nil {
		return nil, false, err
	}
	return resolved(resolveRemoteWithRetry(ctx, r, contracts.CategoryBackend,
		func(c context.Context) (contracts.Backend, error) { return r.dialBackend(c, plugin) }))
}

// RemoteResolveError reports that a remote category could not be resolved within
// the retry budget. It carries the category, attempts made, and elapsed time so
// the caller can degrade cleanly; Unwrap exposes the last underlying error.
type RemoteResolveError struct {
	Category contracts.Category
	Attempts int
	Elapsed  time.Duration
	Err      error
}

func (e *RemoteResolveError) Error() string {
	return fmt.Sprintf("resolver: remote %s unavailable after %d attempt(s) in %s: %v",
		e.Category, e.Attempts, e.Elapsed, e.Err)
}

func (e *RemoteResolveError) Unwrap() error { return e.Err }

func resolveRemoteWithRetry[T any](ctx context.Context, resolver *Resolver, cat contracts.Category, dial func(context.Context) (T, error)) (T, error) {
	var zero T
	start := resolver.now()
	backoff := &obs.Backoff{Base: retryBackoffBase, Factor: backoffFactor, Max: retryBackoffMax, Jitter: retryJitter}
	var lastErr error
	attempt := 0
	for {
		attempt++
		resolver.metrics.RemoteAttempt()
		attemptStart := resolver.now()
		actx, cancel := context.WithTimeout(ctx, resolver.attemptTimeout)
		value, err := dial(actx)
		cancel()
		resolver.metrics.RemoteLatency(resolver.now().Sub(attemptStart))
		if err == nil {
			if attempt > 1 {
				resolver.log.Debug("remote resolve recovered", "category", cat, "attempt", attempt)
			}
			return value, nil
		}
		lastErr = err
		if attempt >= resolver.retryAttempts || resolver.now().Sub(start) >= resolver.retryBudget || ctx.Err() != nil {
			break
		}
		resolver.log.Debug("remote resolve attempt failed", "category", cat, "attempt", attempt, "err", err)
		select {
		case <-ctx.Done():
		case <-resolver.sleep(backoff.Next(0)):
		}
	}
	resolver.metrics.RemoteFailure()
	elapsed := resolver.now().Sub(start)
	resolver.log.Warn("remote resolve gave up", "category", cat, "attempts", attempt, "elapsed", elapsed, "err", lastErr)
	return zero, &RemoteResolveError{Category: cat, Attempts: attempt, Elapsed: elapsed, Err: lastErr}
}

func (r *Resolver) natsURL() string {
	if r.NatsURL != "" {
		return r.NatsURL
	}
	return nats.DefaultURL
}

func (r *Resolver) dialRemoteMemoryOnce(ctx context.Context, _ contracts.Plugin) (contracts.Memory, error) {
	conn, err := nats.Connect(r.natsURL())
	if err != nil {
		return nil, err
	}
	defer conn.Close()
	reg := transport.NewRemoteRegistry()
	seen := make(chan struct{}, 1)
	if err := transport.WatchAnnouncements(conn, func(a transport.Announcement) {
		reg.Observe(a)
		select {
		case seen <- struct{}{}:
		default:
		}
	}, r.warnUndecodableAnnouncement); err != nil {
		return nil, err
	}
	for {
		if mems := reg.Memories(); len(mems) > 0 {
			return transport.DialMemory(ctx, mems[0], r.creds)
		}
		select {
		case <-seen:
		case <-ctx.Done():
			return nil, fmt.Errorf("resolver: no remote memory announced: %w", ctx.Err())
		}
	}
}

func (r *Resolver) dialRemoteOrchestratorOnce(ctx context.Context, _ contracts.Plugin) (contracts.Orchestrator, error) {
	conn, err := nats.Connect(r.natsURL())
	if err != nil {
		return nil, err
	}
	defer conn.Close()
	reg := transport.NewRemoteRegistry()
	seen := make(chan struct{}, 1)
	if err := transport.WatchAnnouncements(conn, func(a transport.Announcement) {
		reg.Observe(a)
		select {
		case seen <- struct{}{}:
		default:
		}
	}, r.warnUndecodableAnnouncement); err != nil {
		return nil, err
	}
	for {
		if orchs := reg.Orchestrators(); len(orchs) > 0 {
			return transport.DialOrchestrator(ctx, orchs[0], r.creds)
		}
		select {
		case <-seen:
		case <-ctx.Done():
			return nil, fmt.Errorf("resolver: no remote orchestrator announced: %w", ctx.Err())
		}
	}
}

func (r *Resolver) dialRemoteBackendOnce(ctx context.Context, plugin contracts.Plugin) (contracts.Backend, error) {
	conn, err := nats.Connect(r.natsURL())
	if err != nil {
		return nil, err
	}
	defer conn.Close()
	reg := transport.NewRemoteRegistry()
	seen, err := r.watchBackendAnnouncements(conn, reg)
	if err != nil {
		return nil, err
	}
	for {
		if bes := reg.Backends(); len(bes) > 0 {
			return r.dialAnnouncedBackend(ctx, bes, plugin.Manifest.Kind)
		}
		select {
		case <-seen:
		case <-ctx.Done():
			return nil, fmt.Errorf("resolver: no remote backend announced: %w", ctx.Err())
		}
	}
}

func (r *Resolver) watchBackendAnnouncements(conn *nats.Conn, reg *transport.RemoteRegistry) (<-chan struct{}, error) {
	seen := make(chan struct{}, 1)
	err := transport.WatchAnnouncements(conn, func(a transport.Announcement) {
		reg.Observe(a)
		select {
		case seen <- struct{}{}:
		default:
		}
	}, func(err error) {
		r.log.Debug("remote announcement undecodable", "err", err)
	})
	return seen, err
}

func (r *Resolver) dialAnnouncedBackend(ctx context.Context, bes []transport.RemoteEntry, kind string) (contracts.Backend, error) {
	if kind == "" {
		return transport.DialBackend(ctx, bes[0], r.creds)
	}
	for index := range bes {
		if bes[index].Manifest.Kind == kind {
			return transport.DialBackend(ctx, bes[index], r.creds)
		}
	}
	return nil, &noRemoteBackendError{Kind: kind}
}
