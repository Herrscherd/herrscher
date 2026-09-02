package host

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	contracts "github.com/Herrscherd/herrscher-contracts"
)

// flaky is a gateway plugin whose factory fails the first n times, the way one
// does when the daemon starts before the machine can resolve a name.
func flaky(kind string, n int, calls *int) contracts.Plugin {
	return contracts.Plugin{
		Manifest: contracts.Manifest{Kind: kind, Category: contracts.CategoryGateway},
		Gateway: func(context.Context, contracts.PluginConfig) (contracts.GatewaySet, error) {
			*calls++
			if *calls <= n {
				return contracts.GatewaySet{}, errors.New("temporary failure in name resolution")
			}
			return contracts.GatewaySet{Gateway: fakeGateway{}}, nil
		},
	}
}

// fastRetries shrinks the backoff so a test spends milliseconds where the daemon
// spends seconds.
func fastRetries(t *testing.T) {
	t.Helper()
	base, max := gatewayRetryBase, gatewayRetryMax
	gatewayRetryBase, gatewayRetryMax = time.Millisecond, 2*time.Millisecond
	t.Cleanup(func() { gatewayRetryBase, gatewayRetryMax = base, max })
}

// The daemon raced the network at boot: the gateway's factory could not resolve
// a name, the gateway was dropped, and the process stayed up — so nothing
// restarted it and the whole edge was gone until somebody noticed hours later.
func TestAGatewayThatLosesTheRaceWithTheNetworkStillJoins(t *testing.T) {
	fastRetries(t)
	calls := 0
	hub, err := BuildHub(context.Background(), []contracts.Plugin{
		gw("terminal", false), flaky("remote", 2, &calls),
	}, func(string) string { return "" })
	if err != nil {
		t.Fatalf("BuildHub: %v", err)
	}
	if _, ok := hub.Get("remote"); ok {
		t.Fatal("the flaky gateway must not be up before any retry")
	}

	if joined := hub.AwaitPending(context.Background(), time.Second, nil); joined != 1 {
		t.Fatalf("joined = %d, want the gateway to come up", joined)
	}
	if _, ok := hub.Get("remote"); !ok {
		t.Fatal("the gateway came up but is not in the hub")
	}
	if hub.Pending() || len(hub.Failures()) != 0 {
		t.Fatalf("pending=%v failures=%v, want a clean hub", hub.Pending(), hub.Failures())
	}
	// The kind is registered once, however many attempts it took.
	if got := hub.Kinds(); len(got) != 2 {
		t.Fatalf("kinds = %v, want terminal and remote once each", got)
	}
}

// A credential that is simply wrong never fixes itself. The window has to close
// so the gateways that do work get on with serving.
func TestAGatewayThatKeepsFailingStopsBeingWaitedFor(t *testing.T) {
	fastRetries(t)
	hub, err := BuildHub(context.Background(), []contracts.Plugin{
		gw("terminal", false), gw("remote", true),
	}, func(string) string { return "" })
	if err != nil {
		t.Fatalf("BuildHub: %v", err)
	}
	start := time.Now()
	if joined := hub.AwaitPending(context.Background(), 20*time.Millisecond, nil); joined != 0 {
		t.Fatalf("joined = %d, want none", joined)
	}
	if time.Since(start) > time.Second {
		t.Fatal("the window did not close")
	}
	if !hub.Pending() {
		t.Fatal("the broken gateway must still be reported as missing")
	}
	if f := strings.Join(hub.Failures(), ";"); !strings.Contains(f, "remote") {
		t.Fatalf("failures = %q, want the kind named", f)
	}
	// The working gateway is untouched by its neighbour's failure.
	if _, ok := hub.Get("terminal"); !ok {
		t.Fatal("terminal must still be up")
	}
}

// Waiting must not outlive a shutdown: ctrl-C during a boot retry has to stop
// the daemon, not hold it for the rest of the window.
func TestWaitingStopsWhenTheDaemonIsCancelled(t *testing.T) {
	fastRetries(t)
	// An hour between attempts: only the cancellation can end this wait.
	gatewayRetryBase, gatewayRetryMax = time.Hour, time.Hour
	hub, err := BuildHub(context.Background(), []contracts.Plugin{
		gw("terminal", false), gw("remote", true),
	}, func(string) string { return "" })
	if err != nil {
		t.Fatalf("BuildHub: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	done := make(chan int, 1)
	go func() {
		done <- hub.AwaitPending(ctx, time.Hour, nil)
	}()
	select {
	case joined := <-done:
		if joined != 0 {
			t.Fatalf("joined = %d, want none", joined)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("AwaitPending ignored a cancelled context")
	}
}

// Nothing to retry is not a reason to sleep: a stack that came up whole must
// start now.
func TestAWholeHubIsNotWaitedFor(t *testing.T) {
	fastRetries(t)
	// An hour between attempts: reaching the first wait at all would hang.
	gatewayRetryBase, gatewayRetryMax = time.Hour, time.Hour
	hub, err := BuildHub(context.Background(), []contracts.Plugin{gw("terminal", false)},
		func(string) string { return "" })
	if err != nil {
		t.Fatalf("BuildHub: %v", err)
	}
	start := time.Now()
	if joined := hub.AwaitPending(context.Background(), time.Hour, nil); joined != 0 {
		t.Fatalf("joined = %d, want none", joined)
	}
	if time.Since(start) > time.Second {
		t.Fatal("a hub with nothing pending must not wait")
	}
}

// The reasons are what an operator reads while the daemon is still starting, so
// each wait announces what it is waiting on and for how long.
func TestEachWaitReportsWhatItIsWaitingOn(t *testing.T) {
	fastRetries(t)
	hub, err := BuildHub(context.Background(), []contracts.Plugin{
		gw("terminal", false), gw("remote", true),
	}, func(string) string { return "" })
	if err != nil {
		t.Fatalf("BuildHub: %v", err)
	}
	var seen []string
	var waits []time.Duration
	hub.AwaitPending(context.Background(), 10*time.Millisecond,
		func(failures []string, in time.Duration) {
			seen = append(seen, failures...)
			waits = append(waits, in)
		})
	if len(seen) == 0 {
		t.Fatal("a retry must say what it is waiting on")
	}
	if !strings.Contains(seen[0], "remote") {
		t.Fatalf("note = %q, want the failing kind named", seen[0])
	}
	for _, w := range waits {
		if w > gatewayRetryMax {
			t.Fatalf("wait = %s, want it capped at %s", w, gatewayRetryMax)
		}
	}
}

// unconfigured is a gateway plugin that declares a required env-bound setting,
// the way a chat gateway declares its bot token. With the var unset the host
// cannot even reach the factory.
func unconfigured(kind, env string) contracts.Plugin {
	return contracts.Plugin{
		Manifest: contracts.Manifest{
			Kind: kind, Category: contracts.CategoryGateway,
			Config: []contracts.Setting{{Key: "token", Env: env, Required: true}},
		},
		Gateway: func(context.Context, contracts.PluginConfig) (contracts.GatewaySet, error) {
			return contracts.GatewaySet{Gateway: fakeGateway{}}, nil
		},
	}
}

// A gateway nobody configured is not a gateway that is slow to come up: the
// environment is fixed for the life of the process, so retrying a missing token
// can only ever fail again. Waiting on one held the whole binary: an operator
// who never set a chat token watched the retry notice for the length of the
// window before the frontend they asked for opened.
func TestAnUnconfiguredGatewayIsNotWaitedFor(t *testing.T) {
	fastRetries(t)
	// An hour between attempts: reaching the first wait at all would hang.
	gatewayRetryBase, gatewayRetryMax = time.Hour, time.Hour
	hub, err := BuildHub(context.Background(), []contracts.Plugin{
		gw("terminal", false), unconfigured("chat", "CHAT_BOT_TOKEN"),
	}, func(string) string { return "" })
	if err != nil {
		t.Fatalf("BuildHub: %v", err)
	}
	if hub.Pending() {
		t.Fatal("a gateway whose config is absent must not be pending")
	}
	start := time.Now()
	if joined := hub.AwaitPending(context.Background(), time.Hour,
		func([]string, time.Duration) { t.Error("an unconfigured gateway must not be announced as a retry") }); joined != 0 {
		t.Fatalf("joined = %d, want none", joined)
	}
	if time.Since(start) > time.Second {
		t.Fatal("the daemon waited on a gateway that can never come up")
	}
	// It is still reported — silently dropping an edge is how a revoked token
	// goes unnoticed — but as something to configure, not something to wait for.
	if f := strings.Join(hub.Unconfigured(), ";"); !strings.Contains(f, "CHAT_BOT_TOKEN") {
		t.Fatalf("unconfigured = %q, want the missing var named", f)
	}
	if f := strings.Join(hub.Failures(), ";"); f != "" {
		t.Fatalf("failures = %q, want an unconfigured gateway kept out of the retry reasons", f)
	}
	if _, ok := hub.Get("terminal"); !ok {
		t.Fatal("terminal must still be up")
	}
}

// The one gateway in the stack being unconfigured is still a hard failure, and
// the message has to name what to set.
func TestAStackWithNothingConfiguredSaysWhatToSet(t *testing.T) {
	_, err := BuildHub(context.Background(), []contracts.Plugin{
		unconfigured("chat", "CHAT_BOT_TOKEN"),
	}, func(string) string { return "" })
	if err == nil {
		t.Fatal("a hub with no gateway at all must fail")
	}
	if !strings.Contains(err.Error(), "CHAT_BOT_TOKEN") {
		t.Fatalf("err = %v, want the missing var named", err)
	}
}
