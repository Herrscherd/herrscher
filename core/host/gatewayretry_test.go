package host

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	contracts "github.com/Herrscherd/herrscher-contracts"
)

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

func fastRetries(hub *GatewayHub) {
	hub.retryBase, hub.retryMax = time.Millisecond, 2*time.Millisecond
}

func parkRetries(hub *GatewayHub) {
	hub.retryBase, hub.retryMax = time.Hour, time.Hour
}

func TestAGatewayThatLosesTheRaceWithTheNetworkStillJoins(t *testing.T) {
	calls := 0
	hub, err := BuildHub(context.Background(), []contracts.Plugin{
		gw("terminal", false), flaky("remote", 2, &calls),
	}, func(string) string { return "" })
	if err != nil {
		t.Fatalf("BuildHub: %v", err)
	}
	fastRetries(hub)
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
	if got := hub.Kinds(); len(got) != 2 {
		t.Fatalf("kinds = %v, want terminal and remote once each", got)
	}
}

func TestAGatewayThatKeepsFailingStopsBeingWaitedFor(t *testing.T) {
	hub, err := BuildHub(context.Background(), []contracts.Plugin{
		gw("terminal", false), gw("remote", true),
	}, func(string) string { return "" })
	if err != nil {
		t.Fatalf("BuildHub: %v", err)
	}
	fastRetries(hub)
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
	if _, ok := hub.Get("terminal"); !ok {
		t.Fatal("terminal must still be up")
	}
}

func TestWaitingStopsWhenTheDaemonIsCancelled(t *testing.T) {
	hub, err := BuildHub(context.Background(), []contracts.Plugin{
		gw("terminal", false), gw("remote", true),
	}, func(string) string { return "" })
	if err != nil {
		t.Fatalf("BuildHub: %v", err)
	}
	parkRetries(hub)
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

func TestAWholeHubIsNotWaitedFor(t *testing.T) {
	hub, err := BuildHub(context.Background(), []contracts.Plugin{gw("terminal", false)},
		func(string) string { return "" })
	if err != nil {
		t.Fatalf("BuildHub: %v", err)
	}
	parkRetries(hub)
	start := time.Now()
	if joined := hub.AwaitPending(context.Background(), time.Hour, nil); joined != 0 {
		t.Fatalf("joined = %d, want none", joined)
	}
	if time.Since(start) > time.Second {
		t.Fatal("a hub with nothing pending must not wait")
	}
}

func TestEachWaitReportsWhatItIsWaitingOn(t *testing.T) {
	hub, err := BuildHub(context.Background(), []contracts.Plugin{
		gw("terminal", false), gw("remote", true),
	}, func(string) string { return "" })
	if err != nil {
		t.Fatalf("BuildHub: %v", err)
	}
	fastRetries(hub)
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
		if w > hub.retryMax {
			t.Fatalf("wait = %s, want it capped at %s", w, hub.retryMax)
		}
	}
}

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

func TestAnUnconfiguredGatewayIsNotWaitedFor(t *testing.T) {
	hub, err := BuildHub(context.Background(), []contracts.Plugin{
		gw("terminal", false), unconfigured("chat", "CHAT_BOT_TOKEN"),
	}, func(string) string { return "" })
	if err != nil {
		t.Fatalf("BuildHub: %v", err)
	}
	parkRetries(hub)
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
