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
	layout := isolatedRuntimeDir(t)
	sessionTab := newSessionTable()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	const name = "driventap-test"
	sock := control.SocketPath(layout, "", name)
	acc, err := control.Accept(ctx, sock)
	if err != nil {
		t.Fatalf("accept: %v", err)
	}
	gw := &fanRecorder{}
	gw.feed("ping")
	got := make(chan contracts.Event, 8)
	go runSessionIdentified(ctx, sessionTab, name, "", []contracts.GatewaySet{{Gateway: gw, Reader: gw}}, acc, "", nil, nil, sessionSink{},
		eventTap(func(_ string, e contracts.Event) { got <- e }, name),
		sessionIdentity{}, nil)

	c := dialCtl(t, sock)
	defer closeRead(c)
	gotInput(t, c, "ping")
	if err := c.Write(contracts.Event{T: "reply", Text: "pong", Done: true}); err != nil {
		t.Fatalf("write reply: %v", err)
	}
	for {
		select {
		case e := <-got:
			if e.T != "reply" {
				continue
			}
			if e.Text != "pong" {
				t.Fatalf("socket saw %+v", e)
			}
			return
		case <-time.After(2 * time.Second):
			t.Fatal("a driven session published nothing on the events socket")
		}
	}
}
