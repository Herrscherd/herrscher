package host

import (
	"context"
	"testing"
	"time"

	contracts "github.com/Herrscherd/herrscher-contracts"
	control "github.com/Herrscherd/herrscher/core/internal/control"
)

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

func TestDrivenSessionTapsTheEventSocket(t *testing.T) {
	sessionTab := newSessionTable()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	acc, err := control.Accept(control.SocketPath("driventap-test"))
	if err != nil {
		t.Fatalf("accept: %v", err)
	}
	got := make(chan contracts.Event, 4)
	go runSessionIdentified(ctx, sessionTab, "driventap-test", "", nil, acc, "", nil, nil, sessionSink{},
		eventTap(func(_ string, e contracts.Event) { got <- e }, "driventap-test"),
		sessionIdentity{}, nil)

	d := waitForDriver(t, sessionTab, "driventap-test")
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

func waitForDriver(t *testing.T, sessions *sessionTable, name string) *sessionDriver {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for {
		d := sessions.lookup(name)
		if d != nil {
			return d
		}
		if time.Now().After(deadline) {
			t.Fatalf("session %q never registered a driver", name)
		}
		time.Sleep(10 * time.Millisecond)
	}
}
