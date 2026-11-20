package host

import (
	"context"
	"time"

	contracts "github.com/Herrscherd/herrscher-contracts"
	"github.com/Herrscherd/herrscher/core/eventwire"
	"github.com/Herrscherd/herrscher/core/internal/approval"
)

func (d *sessionDriver) remember(req approval.Request) {
	d.grantMu.Lock()
	defer d.grantMu.Unlock()
	if d.grants == nil {
		d.grants = make(map[approval.Request]bool)
	}
	d.grants[req] = true
}

func (d *sessionDriver) remembered(req approval.Request) bool {
	d.grantMu.Lock()
	defer d.grantMu.Unlock()
	return d.grants[req]
}

func (d *sessionDriver) verdict(req approval.Request) (approval.Risk, bool) {
	d.grantMu.Lock()
	defer d.grantMu.Unlock()
	risk, ok := d.verdicts[req]
	return risk, ok
}

func (d *sessionDriver) setVerdict(req approval.Request, risk approval.Risk) {
	d.grantMu.Lock()
	defer d.grantMu.Unlock()
	if d.verdicts == nil {
		d.verdicts = make(map[approval.Request]approval.Risk)
	}
	d.verdicts[req] = risk
}

func (d *sessionDriver) askApproval(ctx context.Context, approvalID string, req approval.Request, deadline time.Time) {
	opts := approvalChoices(approvalID, req)
	prompt := d.hostText("approval.prompt", requestParams(req))
	hint := d.hostText("approval.hint", map[string]any{"Command": "approve allow|deny " + approvalID, "Help": "approve allow --help"})
	for _, gateway := range d.gateways {
		channel := d.renderChannel(gateway)
		if gateway.Gateway != nil {
			conv := contracts.Conversation{Gateway: gateway.Gateway.Manifest().Kind, ID: channel}
			announceApprovalDeadline(gateway.Gateway, conv, approvalID, deadline)
			if risk, ok := d.verdict(req); ok {
				announceApprovalRisk(gateway.Gateway, conv, approvalID, risk)
			}
			announceApprovalSubject(gateway.Gateway, conv, approvalID, describeRequest(req))
		}
		if mr, ok := gateway.Reader.(contracts.MenuRouter); ok && channel != "" {
			if _, err := mr.RouteMenu(ctx, channel, "", prompt, d.name, opts); err == nil {
				continue
			}
		}
		if gateway.Gateway == nil {
			continue
		}
		conv := contracts.Conversation{Gateway: gateway.Gateway.Manifest().Kind, ID: channel}
		logCleanup(contracts.Degrade(gateway.Gateway).Menu(ctx, conv, "", prompt+hint, opts))
	}
}

func announceApprovalDeadline(gateway contracts.Gateway, conv contracts.Conversation, approvalID string, deadline time.Time) {
	emitApprovalEvent(gateway, conv, contracts.Event{T: eventwire.EventApproval, Value: approvalID, Text: deadline.UTC().Format(time.RFC3339)})
}

func announceApprovalRisk(gateway contracts.Gateway, conv contracts.Conversation, approvalID string, risk approval.Risk) {
	emitApprovalEvent(gateway, conv, contracts.Event{T: eventwire.EventApprovalRisk, Value: approvalID, Text: risk.String()})
}

func announceApprovalSubject(gateway contracts.Gateway, conv contracts.Conversation, approvalID, subject string) {
	emitApprovalEvent(gateway, conv, contracts.Event{T: eventwire.EventApprovalSubject, Value: approvalID, Text: subject})
}

func emitApprovalEvent(gateway contracts.Gateway, conv contracts.Conversation, event contracts.Event) {
	switch sink := gateway.(type) {
	case contracts.RoutedEventSink:
		sink.EmitTo(conv, event)
	case contracts.EventSink:
		sink.Emit(event)
	}
}

func askApprovalOn(ctx context.Context, sessions *sessionTable, session, approvalID string, req approval.Request, deadline time.Time) {
	driver := sessions.lookup(session)
	if driver == nil {
		return
	}
	driver.askApproval(ctx, approvalID, req, deadline)
}
