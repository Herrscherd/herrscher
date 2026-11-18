package host

import (
	contracts "github.com/Herrscherd/herrscher-contracts"
)

func (d *sessionDriver) currentTurnID() string {
	if d.activeTurnID != "" {
		return d.activeTurnID
	}
	return d.pickTurnID
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
