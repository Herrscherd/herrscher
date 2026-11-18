package host

import (
	"context"
	"strings"
	"unicode/utf8"

	contracts "github.com/Herrscherd/herrscher-contracts"
	"github.com/Herrscherd/herrscher/core/eventwire"
)

func (d *sessionDriver) maybeCoordinate(ctx context.Context, reply string) *contracts.CoordinationEvent {
	if d.coordinator == nil {
		return nil
	}
	if summary, ok := parseDone(reply); ok {
		return d.reportDone(ctx, summary)
	}
	if toAgent, task, ok := parseDelegate(reply); ok {
		return d.delegateTask(ctx, toAgent, task)
	}
	if toAgent, tasks, ok := parseFanOut(reply); ok {
		d.launchCohort(ctx, toAgent, tasks)
		return nil
	}
	if task, ok := parseRoute(reply); ok {
		d.routeTask(ctx, task)
		return nil
	}
	if n, ok := parseSeal(reply); ok {
		d.sealCohort(ctx, n)
		return nil
	}
	if worker, ok := parseMerge(reply); ok {
		d.mergeWorker(ctx, worker)
		return nil
	}
	if toAgent, task, ok := parseHandoff(reply); ok {
		d.handoffTask(ctx, toAgent, task)
	}
	return nil
}

func (d *sessionDriver) reportDone(ctx context.Context, summary string) *contracts.CoordinationEvent {
	parent, err := d.coordinator.Report(ctx, contracts.ReportRequest{
		FromSession: d.name, Summary: summary,
	})
	if err != nil {
		d.fanOut(ctx, contracts.Event{T: eventwire.EventStatus, Text: d.hostText("coordination.report_refused", nil)})
		return &contracts.CoordinationEvent{
			Kind: "report_failed", SourceSession: d.name,
			Summary: sanitizeCoordinationError(localizeHostError(d.messages, d.cliMessages, d.textLanguage()(), err)),
		}
	}
	d.fanOut(ctx, contracts.Event{T: eventwire.EventStatus, Text: d.hostText("coordination.report_delivered", map[string]any{"Parent": parent})})
	return &contracts.CoordinationEvent{
		Kind: "reported", SourceSession: d.name,
		ParentSession: parent, Summary: summary,
	}
}

func (d *sessionDriver) delegateTask(ctx context.Context, toAgent, task string) *contracts.CoordinationEvent {
	target, err := d.coordinator.Delegate(ctx, contracts.DelegateRequest{
		FromSession: d.name, ToAgent: toAgent, Task: task,
	})
	if err != nil {
		d.fanOut(ctx, contracts.Event{T: eventwire.EventStatus, Text: d.hostText("coordination.delegate_refused", nil)})
		return &contracts.CoordinationEvent{
			Kind: "delegate_failed", SourceSession: d.name,
			Agent: toAgent, Summary: sanitizeCoordinationError(localizeHostError(d.messages, d.cliMessages, d.textLanguage()(), err)),
		}
	}
	return &contracts.CoordinationEvent{
		Kind: "delegated", SourceSession: d.name,
		TargetSession: target, Agent: toAgent,
	}
}

func (d *sessionDriver) launchCohort(ctx context.Context, toAgent string, tasks []string) {
	spawned, err := d.coordinator.FanOut(ctx, contracts.FanOutRequest{
		FromSession: d.name, ToAgent: toAgent, Tasks: tasks,
	})
	if err != nil {
		d.fanOut(ctx, contracts.Event{
			T:    eventwire.EventStatus,
			Text: d.hostText("coordination.fanout_partial", map[string]any{"Launched": len(spawned), detailParam: localizeHostError(d.messages, d.cliMessages, d.textLanguage()(), err).Error()}),
		})
		return
	}
	d.fanOut(ctx, contracts.Event{
		T:    eventwire.EventStatus,
		Text: d.hostText("coordination.launched", map[string]any{"Count": len(spawned), "Workers": strings.Join(spawned, ", ")}),
	})
}

func (d *sessionDriver) routeTask(ctx context.Context, task string) {
	toAgent, session, err := d.coordinator.Route(ctx, contracts.RouteRequest{
		FromSession: d.name, Task: task,
	})
	if err != nil {
		d.fanOut(ctx, contracts.Event{T: eventwire.EventStatus, Text: d.hostText("coordination.route_refused", map[string]any{detailParam: localizeHostError(d.messages, d.cliMessages, d.textLanguage()(), err).Error()})})
		return
	}
	d.fanOut(ctx, contracts.Event{T: eventwire.EventStatus, Text: d.hostText("coordination.routed", map[string]any{"Agent": toAgent, "Session": session})})
}

func (d *sessionDriver) sealCohort(ctx context.Context, node int) {
	if _, err := d.coordinator.Seal(ctx, contracts.SealRequest{
		FromSession: d.name, Expected: node,
	}); err != nil {
		d.fanOut(ctx, contracts.Event{T: eventwire.EventStatus, Text: d.hostText("coordination.seal_refused", map[string]any{detailParam: localizeHostError(d.messages, d.cliMessages, d.textLanguage()(), err).Error()})})
		return
	}
	d.fanOut(ctx, contracts.Event{T: eventwire.EventStatus, Text: d.hostText("coordination.sealed", map[string]any{"Node": node})})
}

func (d *sessionDriver) mergeWorker(ctx context.Context, worker string) {
	lead, err := d.coordinator.Merge(ctx, contracts.MergeRequest{
		FromSession: d.name, Worker: worker,
	})
	if err != nil {
		d.fanOut(ctx, contracts.Event{T: eventwire.EventStatus, Text: d.hostText("coordination.merge_refused", map[string]any{detailParam: localizeHostError(d.messages, d.cliMessages, d.textLanguage()(), err).Error()})})
		return
	}
	d.fanOut(ctx, contracts.Event{T: eventwire.EventStatus, Text: d.hostText("coordination.merged", map[string]any{"Lead": lead})})
}

func (d *sessionDriver) handoffTask(ctx context.Context, toAgent, task string) {
	if _, err := d.coordinator.Handoff(ctx, contracts.HandoffRequest{
		FromSession: d.name, ToAgent: toAgent, Task: task,
	}); err != nil {
		d.fanOut(ctx, contracts.Event{T: eventwire.EventStatus, Text: d.hostText("coordination.handoff_refused", map[string]any{detailParam: localizeHostError(d.messages, d.cliMessages, d.textLanguage()(), err).Error()})})
	}
}

func sanitizeCoordinationError(err error) string {
	const maxLen = 160
	message := strings.Join(strings.Fields(err.Error()), " ")
	if len(message) <= maxLen {
		return message
	}
	cut := maxLen
	for cut > 0 && !utf8.RuneStart(message[cut]) {
		cut--
	}
	return message[:cut]
}
