package host

import (
	"context"
	"time"

	contracts "github.com/Herrscherd/herrscher-contracts"
)

// pollInterval is how often the driver polls each bound gateway's Read for new
// inbound lines.
var pollInterval = 50 * time.Millisecond

// sessionDriver owns one session's turn lifecycle: it polls every bound
// gateway's Reader for inbound messages, serializes them through a FIFO, writes
// one input frame to the bridge per turn, and fans the bridge's reply events out
// to all bound gateways. toBridge/fromBridge are the two directions of the
// session's control connection (a *control.Conn in production; channels in
// tests).
type sessionDriver struct {
	name     string
	gateways []contracts.GatewaySet
	toBridge chan<- contracts.Event
	from     <-chan contracts.Event
	queue    chan contracts.Event
}

func newSessionDriver(name string, gws []contracts.GatewaySet, toBridge chan<- contracts.Event, fromBridge <-chan contracts.Event) *sessionDriver {
	return &sessionDriver{
		name:     name,
		gateways: gws,
		toBridge: toBridge,
		from:     fromBridge,
		queue:    make(chan contracts.Event, 64),
	}
}

// run starts the pollers and the turn pump; it blocks until ctx is cancelled.
func (d *sessionDriver) run(ctx context.Context) {
	for _, g := range d.gateways {
		if g.Reader != nil {
			go d.poll(ctx, g.Reader)
		}
	}
	d.pump(ctx)
}

// poll reads one gateway's inbound messages and enqueues them as input frames.
func (d *sessionDriver) poll(ctx context.Context, r contracts.ChannelReader) {
	ch := r.DefaultChannel()
	var last string
	for {
		if ctx.Err() != nil {
			return
		}
		msgs, err := r.Read(ctx, ch, 100, last)
		if err == nil {
			for _, m := range msgs {
				if m.AuthorBot {
					continue
				}
				last = m.ID
				select {
				case d.queue <- contracts.Event{T: "input", Who: m.AuthorName, Text: m.Content}:
				case <-ctx.Done():
					return
				}
			}
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(pollInterval):
		}
	}
}

// pump dequeues one input at a time, writes it to the bridge, then blocks
// fanning the bridge's events out until that turn's reply{done} — this is the
// FIFO serialization.
func (d *sessionDriver) pump(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case ev := <-d.queue:
			select {
			case d.toBridge <- ev:
			case <-ctx.Done():
				return
			}
			d.awaitTurn(ctx)
		}
	}
}

// awaitTurn fans every event for the current turn to all bound gateways and
// returns when it sees reply{done} (or ctx is cancelled, the bridge closed, or a
// reset signals the in-flight turn was abandoned on a bridge disconnect).
func (d *sessionDriver) awaitTurn(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case e, ok := <-d.from:
			if !ok {
				return // bridge connection lost; abandon this turn
			}
			if e.T == "reset" {
				return // bridge reset/disconnected mid-turn; abandon it
			}
			d.fanOut(ctx, e)
			if e.T == "reply" && e.Done {
				return
			}
		}
	}
}

// fanOut delivers one turn event to every bound gateway: a gateway implementing
// EventSink renders the full stream itself; otherwise only the final reply is
// posted through the Gateway port (the host renderer added in M5 enriches the
// non-EventSink path).
func (d *sessionDriver) fanOut(ctx context.Context, e contracts.Event) {
	for _, g := range d.gateways {
		if sink, ok := g.Gateway.(contracts.EventSink); ok {
			sink.Emit(e)
			continue
		}
		if e.T == "reply" && e.Done && e.Text != "" {
			_, _ = g.Gateway.Post(ctx, contracts.Conversation{Gateway: g.Gateway.Manifest().Kind, ID: gatewayChannel(g)}, e.Text)
		}
	}
}

// gatewayChannel returns the default channel for a gateway set, or "" when it
// has no reader.
func gatewayChannel(g contracts.GatewaySet) string {
	if g.Reader != nil {
		return g.Reader.DefaultChannel()
	}
	return ""
}
