package main

import (
	"testing"
	"time"

	contracts "github.com/Herrscherd/herrscher-contracts"
	"github.com/Herrscherd/herrscher-contracts/i18n"
	"github.com/Herrscherd/herrscher/core/eventwire"
	"github.com/Herrscherd/herrscher/core/host"
	"github.com/Herrscherd/herrscher/plugins/terminal/tui"
)

func TestAttachedPumpForwardsAResetIntact(t *testing.T) {
	done := make(chan struct{})
	defer close(done)
	events := make(chan tui.RoutedEvent, 1)
	var lang i18n.Language
	state := newAttachedDaemonState(nil, nil, lang, done, events)
	state.channelOf["sess"] = "chan-1"

	stream := make(chan host.DaemonEvent, 1)
	stream <- host.DaemonEvent{Session: "sess", Event: contracts.Event{T: eventwire.EventReset, Tokens: 7}}
	close(stream)
	go state.pump(stream)

	select {
	case routed := <-events:
		if routed.Event.T != eventwire.EventReset || routed.Event.Tokens != 7 {
			t.Fatalf("the reset must reach the frontend intact: %+v", routed.Event)
		}
		if routed.Conv.Gateway != kindTerminal || routed.Conv.ID != "chan-1" {
			t.Fatalf("the reset must be routed to its channel: %+v", routed.Conv)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the reset never reached the frontend")
	}
}
