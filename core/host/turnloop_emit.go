package host

import (
	"context"
	"strconv"

	contracts "github.com/Herrscherd/herrscher-contracts"
	"github.com/Herrscherd/herrscher-contracts/i18n"
	"github.com/Herrscherd/herrscher/core/eventwire"
)

func (d *sessionDriver) currentLanguage() i18n.Language {
	if d.language == nil {
		return 0
	}
	return d.language()
}

func (d *sessionDriver) textLanguage() languageSource {
	return func() i18n.Language {
		if lang := d.currentLanguage(); lang != 0 {
			return lang
		}
		if d.operator != nil {
			return d.operator()
		}
		return 0
	}
}

func (d *sessionDriver) announceLanguage(ctx context.Context) bool {
	if d.language == nil {
		return true
	}
	return d.sendToBridge(ctx, contracts.Event{T: eventwire.EventLanguage, Value: languageTag(d.language())})
}

func languageTag(lang i18n.Language) string {
	if lang == 0 {
		return ""
	}
	return lang.String()
}

func (d *sessionDriver) stamp(event contracts.Event) contracts.Event {
	if d.activeTurnID != "" {
		event.SessionIncarnation = d.incarnation
		event.TurnID = d.activeTurnID
		event.Agent = d.agent
		return event
	}
	event.SessionIncarnation = ""
	event.TurnID = d.pickTurnID
	event.Agent = ""
	return event
}

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
