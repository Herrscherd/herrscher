package host

import (
	"context"
	"strconv"

	contracts "github.com/Herrscherd/herrscher-contracts"
)

func (d *sessionDriver) eachGateway(visit func(index int, gateway contracts.GatewaySet)) {
	for index, gateway := range d.gateways {
		visit(index, gateway)
	}
}

func (d *sessionDriver) emitToGateway(ctx context.Context, index int, gateway contracts.GatewaySet, event contracts.Event) {
	if rs, ok := gateway.Gateway.(contracts.RoutedEventSink); ok {
		rs.EmitTo(contracts.Conversation{
			Gateway: gateway.Gateway.Manifest().Kind,
			ID:      d.renderChannel(gateway),
		}, event)
		return
	}
	if sink, ok := gateway.Gateway.(contracts.EventSink); ok {
		sink.Emit(event)
		return
	}
	key := strconv.Itoa(index) + ":" + gateway.Gateway.Manifest().Kind
	renderer := d.renderers[key]
	if renderer == nil {
		renderer = newGatewayRenderer(gateway.Gateway, d.renderChannel(gateway))
		d.renderers[key] = renderer
	}
	renderer.handle(ctx, event)
}

func outsideBoundGateways(origin contracts.Conversation, gws []contracts.GatewaySet) bool {
	if origin.Gateway == "" {
		return false
	}
	for _, g := range gws {
		if g.Gateway != nil && g.Gateway.Manifest().Kind == origin.Gateway {
			return false
		}
	}
	return true
}

func gatewayChannel(gateway contracts.GatewaySet) string {
	if gateway.Reader != nil {
		return gateway.Reader.DefaultChannel()
	}
	return ""
}

func (d *sessionDriver) renderChannel(gateway contracts.GatewaySet) string {
	if d.channel != "" {
		return d.channel
	}
	return gatewayChannel(gateway)
}
