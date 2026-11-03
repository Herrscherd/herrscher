package host

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"os"
	"sync"
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
	rejectionLogInterval  = time.Minute
)

// Resolver turns registered plugins into live port objects, choosing local
// (in-proc factory) or remote (gRPC proxy) per category. With a nil/empty
// remote set every category resolves local; today's behaviour.
type Resolver struct {
	remote  map[contracts.Category]bool
	NatsURL string

	creds       credentials.TransportCredentials
	announceKey transport.AnnounceKey

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

	rejectMu       sync.Mutex
	rejectionsSeen map[string]time.Time
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

func rejectionClass(err error) string {
	switch {
	case errors.Is(err, transport.ErrUnauthenticatedAnnouncement):
		return "unauthenticated"
	case errors.Is(err, transport.ErrStaleAnnouncement):
		return "stale"
	case errors.Is(err, transport.ErrOversizedAnnouncement):
		return "oversized"
	case errors.Is(err, transport.ErrMalformedAnnouncement):
		return "malformed"
	case errors.Is(err, transport.ErrUnusableAnnouncement):
		return "unusable"
	case errors.Is(err, transport.ErrAddrRefused):
		return "addr-refused"
	default:
		return "other"
	}
}

func (r *Resolver) warnRejectedAnnouncement(err error) {
	reason := rejectionClass(err)
	r.rejectMu.Lock()
	defer r.rejectMu.Unlock()
	if r.rejectionsSeen == nil {
		r.rejectionsSeen = make(map[string]time.Time)
	}
	now := r.now()
	if last, seen := r.rejectionsSeen[reason]; seen && now.Sub(last) < rejectionLogInterval {
		return
	}
	r.rejectionsSeen[reason] = now
	r.log.Warn("ignoring a rejected announcement", "err", err, "repeats_within", rejectionLogInterval)
}

// SetAnnounceKey installs the key that authenticates the announcements the remote
// dial accepts. Without it every remote resolve fails: the zero key is unusable.
func (r *Resolver) SetAnnounceKey(key transport.AnnounceKey) {
	r.announceKey = key
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

func (r *Resolver) addrAllowed(addr string) bool {
	if r.creds != nil {
		return true
	}
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return false
	}
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func (r *Resolver) dialRemoteMemoryOnce(ctx context.Context, _ contracts.Plugin) (contracts.Memory, error) {
	watch, closeConn, err := r.watchAnnouncements()
	if err != nil {
		return nil, err
	}
	defer closeConn()
	mems, err := awaitEntries(ctx, contracts.CategoryMemory, watch)
	if err != nil {
		return nil, err
	}
	return transport.DialMemory(ctx, mems[0], r.creds)
}

func (r *Resolver) dialRemoteOrchestratorOnce(ctx context.Context, _ contracts.Plugin) (contracts.Orchestrator, error) {
	watch, closeConn, err := r.watchAnnouncements()
	if err != nil {
		return nil, err
	}
	defer closeConn()
	orchs, err := awaitEntries(ctx, contracts.CategoryOrchestrator, watch)
	if err != nil {
		return nil, err
	}
	return transport.DialOrchestrator(ctx, orchs[0], r.creds)
}

func (r *Resolver) dialRemoteBackendOnce(ctx context.Context, plugin contracts.Plugin) (contracts.Backend, error) {
	watch, closeConn, err := r.watchAnnouncements()
	if err != nil {
		return nil, err
	}
	defer closeConn()
	bes, err := awaitEntries(ctx, contracts.CategoryBackend, watch)
	if err != nil {
		return nil, err
	}
	return r.dialAnnouncedBackend(ctx, bes, plugin.Manifest.Kind)
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
