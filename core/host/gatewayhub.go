package host

import (
	"context"
	"fmt"
	"strings"
	"time"

	contracts "github.com/Herrscherd/herrscher-contracts"
)

// GatewayHub holds every registered gateway plugin instantiated into a
// GatewaySet, keyed by Manifest.Kind. It is the core's multi-gateway port: the
// daemon resolves a session's bound gateways through it instead of hand-wiring a
// single gateway. Kinds() preserves registration order.
type GatewayHub struct {
	sets     map[string]contracts.GatewaySet
	order    []string
	failures []string
	pending  []contracts.Plugin
}

// BuildHub instantiates each gateway plugin in plugins. A plugin whose config
// can't resolve, or whose factory errors, is skipped (its required vars are
// absent — e.g. a missing gateway token — which must not stop other gateways from
// running). If NO gateway builds, the aggregated per-gateway reasons are
// returned so a single-gateway stack still fails fast with a clear message.
// Every skip is also recorded on the hub (Failures) so the daemon can report a
// gateway that dropped out — a stale token silently costing you a whole edge is
// worse than a noisy line at boot.
func BuildHub(ctx context.Context, plugins []contracts.Plugin, getenv func(string) string) (*GatewayHub, error) {
	h := &GatewayHub{sets: map[string]contracts.GatewaySet{}}
	var candidates []contracts.Plugin
	for _, p := range plugins {
		if p.Gateway != nil {
			candidates = append(candidates, p)
		}
	}
	h.attempt(ctx, candidates, getenv)
	if len(h.sets) == 0 {
		if len(h.failures) == 0 {
			return nil, fmt.Errorf("no gateway plugin registered")
		}
		return nil, fmt.Errorf("no gateway available: %s", strings.Join(h.failures, "; "))
	}
	return h, nil
}

// attempt builds each candidate, recording the ones that came up and keeping the
// ones that did not, with the reason, for Retry to try again.
func (h *GatewayHub) attempt(ctx context.Context, candidates []contracts.Plugin, getenv func(string) string) {
	h.failures = nil
	h.pending = nil
	for _, p := range candidates {
		set, err := buildOne(ctx, p, getenv)
		if err != nil {
			h.failures = append(h.failures, fmt.Sprintf("%s: %v", p.Manifest.Kind, err))
			h.pending = append(h.pending, p)
			continue
		}
		// Duplicate Manifest.Kind: keep first-seen order, last factory wins the set.
		if _, dup := h.sets[p.Manifest.Kind]; !dup {
			h.order = append(h.order, p.Manifest.Kind)
		}
		h.sets[p.Manifest.Kind] = set
	}
}

func buildOne(ctx context.Context, p contracts.Plugin, getenv func(string) string) (contracts.GatewaySet, error) {
	cfg, err := contracts.Resolve(p.Manifest.Config, getenv)
	if err != nil {
		return contracts.GatewaySet{}, err
	}
	set, err := p.Gateway(ctx, cfg)
	if err != nil {
		return contracts.GatewaySet{}, err
	}
	if set.Gateway == nil {
		return contracts.GatewaySet{}, fmt.Errorf("factory returned no gateway")
	}
	return set, nil
}

// Retry rebuilds the gateways that did not come up and reports how many joined.
// A factory can fail for a reason that fixes itself: at boot the daemon races
// the network, and a name that does not resolve yet is a few seconds of waiting
// rather than a broken install — while the process stayed up, so nothing
// restarted it and the edge it serves was simply gone. A genuinely bad
// credential keeps failing, which is why the waiting is bounded by the caller
// rather than endless.
//
// It is a no-op once everything is up, and safe to call again: whatever is still
// pending stays pending, with its latest reason.
func (h *GatewayHub) Retry(ctx context.Context, getenv func(string) string) int {
	if len(h.pending) == 0 {
		return 0
	}
	before := len(h.sets)
	h.attempt(ctx, h.pending, getenv)
	return len(h.sets) - before
}

// Pending reports whether any registered gateway is still missing.
func (h *GatewayHub) Pending() bool { return len(h.pending) > 0 }

// GatewayRetryWindow bounds how long the daemon waits at startup for a gateway
// that did not build. Long enough for a machine that has just booted to finish
// bringing its network up, short enough that a stack whose credential is simply
// wrong still gets on with serving the gateways that do work.
const GatewayRetryWindow = 2 * time.Minute

// The wait between attempts doubles from base to max. Both are variables so a
// test can run the loop without spending the wall-clock time it describes.
var (
	gatewayRetryBase = 2 * time.Second
	gatewayRetryMax  = 20 * time.Second
)

// AwaitPending retries the gateways that did not build, backing off between
// attempts, until every one is up or the window closes, and reports how many
// joined. It belongs to the daemon at startup, where nothing is supervising
// sessions yet and waiting costs nothing — never to a one-shot operator command,
// which must not hang because a name will not resolve.
//
// note, if given, is called before each wait with the reasons still standing, so
// the caller decides how a retry is reported.
func (h *GatewayHub) AwaitPending(ctx context.Context, getenv func(string) string, window time.Duration, note func(failures []string, in time.Duration)) int {
	joined := 0
	deadline := time.Now().Add(window)
	for wait := gatewayRetryBase; h.Pending() && time.Now().Before(deadline); wait *= 2 {
		if wait > gatewayRetryMax {
			wait = gatewayRetryMax
		}
		if note != nil {
			note(h.Failures(), wait)
		}
		select {
		case <-ctx.Done():
			return joined
		case <-time.After(wait):
		}
		joined += h.Retry(ctx, getenv)
	}
	return joined
}

// Kinds returns the built gateway kinds in registration order.
func (h *GatewayHub) Kinds() []string { return append([]string(nil), h.order...) }

// Failures returns one "kind: reason" line per gateway that did not build, for
// the caller to log. It is empty when every registered gateway came up.
func (h *GatewayHub) Failures() []string { return append([]string(nil), h.failures...) }

// Get returns the GatewaySet for a kind and whether it was built.
func (h *GatewayHub) Get(kind string) (contracts.GatewaySet, bool) {
	s, ok := h.sets[kind]
	return s, ok
}

// First returns the first built gateway set (registration order) and whether
// the hub has one. It preserves the pre-hub "first registered gateway" behavior
// for callers not yet gateway-aware.
func (h *GatewayHub) First() (contracts.GatewaySet, bool) {
	if len(h.order) == 0 {
		return contracts.GatewaySet{}, false
	}
	return h.sets[h.order[0]], true
}
