package host

import (
	"context"
	"testing"
	"time"

	contracts "github.com/Herrscherd/herrscher-contracts"
	"github.com/Herrscherd/herrscher/core/internal/control"
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

	acc, err := control.Accept(t.Context(), control.SocketPath("driventap-test"))
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
	var d *sessionDriver
	waitFor(t, &sessions.changed, func() bool {
		d = sessions.lookup(name)
		return d != nil
	}, "session "+name+" to register a driver")
	return d
}
