package host

import (
	"context"
	"path/filepath"
	"sync"
	"testing"
	"time"

	contracts "github.com/Herrscherd/herrscher-contracts"
	"github.com/Herrscherd/herrscher/core/internal/state"
	"github.com/Herrscherd/herrscher/core/internal/supervisor"
)

func tapping(t *testing.T, d *sessionDriver) func() []contracts.Event {
	t.Helper()
	var mu sync.Mutex
	var seen []contracts.Event
	d.emitTap = func(e contracts.Event) {
		mu.Lock()
		seen = append(seen, e)
		mu.Unlock()
	}
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	go d.pump(ctx)
	return func() []contracts.Event {
		mu.Lock()
		defer mu.Unlock()
		return append([]contracts.Event(nil), seen...)
	}
}

func sawReply(events []contracts.Event, text string) bool {
	for _, e := range events {
		if e.T == "reply" && e.Done && e.Text == text {
			return true
		}
	}
	return false
}

func TestTurnSaidOutsideTheBoundChannelsIsNotPostedThere(t *testing.T) {
	a := &fanRecorder{}
	toBridge := make(chan contracts.Event, 4)
	fromBridge := make(chan contracts.Event, 4)
	d := newSessionDriver("s1", []contracts.GatewaySet{{Gateway: a, Reader: a}}, toBridge, fromBridge)
	d.channel = "chan-1"
	tapped := tapping(t, d)

	if !d.submit(context.Background(), contracts.Inbound{
		Author:       "you",
		Text:         "reprends ici",
		Conversation: contracts.Conversation{Gateway: "terminal", ID: "chan-1"},
	}) {
		t.Fatal("submit reported a cancelled context")
	}
	select {
	case in := <-toBridge:
		if in.T != "input" || in.Text != "reprends ici" {
			t.Fatalf("driver wrote %+v, want the pushed input", in)
		}
	case <-time.After(time.Second):
		t.Fatal("driver did not pump the pushed input to the bridge")
	}
	fromBridge <- contracts.Event{T: "reply", Text: "done", Done: true}

	waitFor(t, func() bool { return sawReply(tapped(), "done") }, "the pushing client sees the reply on the event stream")
	a.mu.Lock()
	defer a.mu.Unlock()
	if len(a.posted) != 0 || a.upserts != 0 {
		t.Fatalf("the session's channel must stay silent, got posted=%v statuses=%v", a.posted, a.statuses)
	}
}

func TestTurnSaidInABoundChannelIsPostedThere(t *testing.T) {
	a := &fanRecorder{}
	toBridge := make(chan contracts.Event, 4)
	fromBridge := make(chan contracts.Event, 4)
	d := newSessionDriver("s1", []contracts.GatewaySet{{Gateway: a, Reader: a}}, toBridge, fromBridge)
	tapping(t, d)

	d.submit(context.Background(), contracts.Inbound{
		Author:       "you",
		Text:         "salut",
		Conversation: contracts.Conversation{Gateway: "rec", ID: "c"},
	})
	<-toBridge
	fromBridge <- contracts.Event{T: "reply", Text: "done", Done: true}

	waitFor(t, func() bool {
		a.mu.Lock()
		defer a.mu.Unlock()
		return len(a.posted) == 1 && a.posted[0] == "done"
	}, "a turn said in the bound channel is answered there")
}

func TestSessionSendCarriesTheOriginItWasTypedIn(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	dir := t.TempDir()
	st := state.NewState(filepath.Join(dir, "s.json"))
	sup := supervisor.NewSupervisor(ctx, "/nonexistent/herrscher")
	reg, _, err := buildRegistry(ctx, Deps{}, Options{StatePath: filepath.Join(dir, "s.json"), DefaultCmd: "claude"}, st, sup, "", nil)
	if err != nil {
		t.Fatal(err)
	}
	d := newSessionDriver("s1", nil, nil, nil)
	registerDriver("s1", d)
	defer unregisterDriver("s1", d)

	if _, err := reg.Dispatch(ctx, []string{"session", "send", "--name", "s1", "--text", "reprends ici", "--origin", "terminal"}); err != nil {
		t.Fatalf("session send: %v", err)
	}
	select {
	case q := <-d.queue:
		if q.origin.Gateway != "terminal" {
			t.Fatalf("queued origin = %+v, want the terminal it was typed in", q.origin)
		}
	default:
		t.Fatal("session send queued nothing")
	}

	if _, err := reg.Dispatch(ctx, []string{"session", "send", "--name", "s1", "--text", "vas-y"}); err != nil {
		t.Fatalf("session send: %v", err)
	}
	select {
	case q := <-d.queue:
		if q.origin.Gateway != "" {
			t.Fatalf("queued origin = %+v, want none claimed", q.origin)
		}
	default:
		t.Fatal("session send queued nothing")
	}
}

func TestTurnWithNoOriginIsPostedToTheBoundChannels(t *testing.T) {
	a := &fanRecorder{}
	toBridge := make(chan contracts.Event, 4)
	fromBridge := make(chan contracts.Event, 4)
	d := newSessionDriver("s1", []contracts.GatewaySet{{Gateway: a, Reader: a}}, toBridge, fromBridge)
	tapping(t, d)

	d.submit(context.Background(), contracts.Inbound{Author: "script", Text: "vas-y"})
	<-toBridge
	fromBridge <- contracts.Event{T: "reply", Text: "done", Done: true}

	waitFor(t, func() bool {
		a.mu.Lock()
		defer a.mu.Unlock()
		return len(a.posted) == 1 && a.posted[0] == "done"
	}, "a turn with no origin is answered in the session's channel")
}
