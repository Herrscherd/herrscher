package host

import (
	"context"
	"testing"
	"time"

	contracts "github.com/Herrscherd/herrscher-contracts"
	control "github.com/Herrscherd/herrscher/core/internal/control"
)

// TestEventTapAddressesTheSession: the driver knows only its own events, so the
// tap is what puts a session's name on them. A nil publisher must yield a nil
// tap, not a closure calling nothing — the driver's own nil check is the single
// place the absence is decided.
func TestEventTapAddressesTheSession(t *testing.T) {
	if eventTap(nil, "s") != nil {
		t.Fatal("a nil publisher must give a nil tap")
	}
	var gotSess string
	var gotEv contracts.Event
	tap := eventTap(func(s string, e contracts.Event) { gotSess, gotEv = s, e }, "main")
	tap(contracts.Event{T: "reply", Text: "pong"})
	if gotSess != "main" || gotEv.Text != "pong" {
		t.Fatalf("tap delivered %q / %+v", gotSess, gotEv)
	}
}

// TestDrivenSessionTapsTheEventSocket is the regression for an attached frontend
// that sat on `thinking…` forever: the events socket was created, served and
// bound, but only the one-shot seed path ever fed it, so an ordinary session's
// turn published nothing and every attached reader went blind.
func TestDrivenSessionTapsTheEventSocket(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	acc, err := control.Accept(control.SocketPath("driventap-test"))
	if err != nil {
		t.Fatalf("accept: %v", err)
	}
	got := make(chan contracts.Event, 4)
	go runSessionIdentified(ctx, "driventap-test", "", nil, acc, "", nil, nil, sessionSink{},
		eventTap(func(_ string, e contracts.Event) { got <- e }, "driventap-test"),
		sessionIdentity{}, nil)

	d := waitForDriver(t, "driventap-test")
	d.fanOut(ctx, contracts.Event{T: "reply", Text: "pong", Done: true})

	select {
	case e := <-got:
		if e.Text != "pong" {
			t.Fatalf("socket saw %+v", e)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("a driven session published nothing on the events socket")
	}
}

func waitForDriver(t *testing.T, name string) *sessionDriver {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for {
		sessionRegistry.mu.Lock()
		d := sessionRegistry.m[name]
		sessionRegistry.mu.Unlock()
		if d != nil {
			return d
		}
		if time.Now().After(deadline) {
			t.Fatalf("session %q never registered a driver", name)
		}
		time.Sleep(10 * time.Millisecond)
	}
}
