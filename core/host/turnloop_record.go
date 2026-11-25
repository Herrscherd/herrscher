package host

import (
	"context"
	"strconv"
	"strings"
	"time"

	contracts "github.com/Herrscherd/herrscher-contracts"
	"github.com/Herrscherd/herrscher/core/bridge"
	"github.com/Herrscherd/herrscher/core/eventwire"
	"github.com/Herrscherd/herrscher/core/internal/state"
	"github.com/Herrscherd/herrscher/core/timeline"
	"github.com/Herrscherd/herrscher/core/usage"
)

func attachmentHosts(gws []contracts.GatewaySet) map[string]bool {
	var hosts map[string]bool
	for _, gateway := range gws {
		if gateway.Gateway == nil {
			continue
		}
		for _, attachmentHost := range gateway.Gateway.Manifest().AttachmentHosts {
			attachmentHost = strings.ToLower(strings.TrimSpace(attachmentHost))
			if attachmentHost == "" {
				continue
			}
			if hosts == nil {
				hosts = make(map[string]bool)
			}
			hosts[attachmentHost] = true
		}
	}
	return hosts
}

func (d *sessionDriver) journal(authorID string) {
	if d.participants == "" || authorID == "" {
		return
	}
	d.seenMu.Lock()
	first := !d.seen[authorID]
	d.seen[authorID] = true
	d.seenMu.Unlock()
	if first {
		end := d.persists.begin()
		defer end()
		_, appendErr := state.AppendParticipant(d.participants, authorID)
		logCleanup(appendErr)
	}
}

func (d *sessionDriver) resolveAttachments(ctx context.Context, message contracts.Message) []string {
	if len(message.Attachments) == 0 {
		return nil
	}
	return bridge.ResolveAttachments(ctx, nil, message, d.name, d.attachHosts)
}

type turnUsage struct {
	InTokens, OutTokens, CacheRead, CacheCreate, DurMs int
	CtxTokens                                          int
	Tools                                              []timeline.Call
	Injected                                           []contracts.InjectedPart
}

func (d *sessionDriver) recordUsage(event contracts.Event) {
	if d.sink.Usage == nil {
		return
	}
	records := usageRecords(d.name, time.Now().UTC(), d.modelNow().model, event)
	if len(records) == 0 {
		return
	}
	d.sink.Usage(records)
}

func usageRecords(session string, when time.Time, model string, event contracts.Event) []usage.Record {
	if len(event.Models) == 0 {
		if !hasTurnUsage(event) {
			return nil
		}
		return []usage.Record{{Time: when, Session: session, Model: model, In: int64(event.TokensIn), Out: int64(event.Tokens), CacheRead: int64(event.CacheRead), CacheCreate: int64(event.CacheCreate), Cost: event.Cost}}
	}
	records := make([]usage.Record, 0, len(event.Models))
	for _, spent := range event.Models {
		records = append(records, usage.Record{Time: when, Session: session, Model: spent.Model, In: int64(spent.In), Out: int64(spent.Out), CacheRead: int64(spent.CacheRead), CacheCreate: int64(spent.CacheCreate), Cost: spent.Cost})
	}
	return records
}

func hasTurnUsage(event contracts.Event) bool {
	return event.TokensIn != 0 || event.Tokens != 0 || event.CacheRead != 0 || event.CacheCreate != 0 || event.Cost != 0
}

func (d *sessionDriver) recordEntry(role, text, shown string, cost float64, turn turnUsage) {
	if d.sink.Transcript == nil || (text == "" && shown == "" && len(turn.Tools) == 0) {
		return
	}
	entry := state.TranscriptEntry{
		Ts:          time.Now().UTC().Format(time.RFC3339),
		Role:        role,
		Text:        text,
		TurnID:      d.currentTurnID(),
		Cost:        cost,
		TokensIn:    turn.InTokens,
		TokensOut:   turn.OutTokens,
		CacheRead:   turn.CacheRead,
		CacheCreate: turn.CacheCreate,
		DurMs:       turn.DurMs,
		CtxTokens:   turn.CtxTokens,
		Tools:       turn.Tools,
		Injected:    turn.Injected,
	}
	if shown != text {
		entry.Displayed = shown
	}
	d.sink.Transcript(entry)
}

func (d *sessionDriver) currentTurnID() string {
	if d.activeTurnID != "" {
		return d.activeTurnID
	}
	return d.pickTurnID
}

func (d *sessionDriver) enforceTokenGuard(ctx context.Context, guard *tokenGuard, e contracts.Event) {
	spent, over := guard.trips(e)
	if !over {
		return
	}
	d.Interrupt()
	d.fanOut(ctx, contracts.Event{T: eventwire.EventStatus, Text: d.hostText("turn.token_ceiling", map[string]any{"Spent": strconv.FormatUint(spent, 10), "Margin": strconv.FormatUint(guard.headroom, 10)})})
}

func promptSize(e contracts.Event) int {
	return e.TokensIn + e.CacheRead + e.CacheCreate
}

func contextReading(live int, done contracts.Event) int {
	if live > 0 {
		return live
	}
	return promptSize(done)
}

func turnTokens(event contracts.Event) uint64 {
	total := 0
	if event.Tokens > 0 {
		total += event.Tokens
	}
	if event.TokensIn > 0 {
		total += event.TokensIn
	}
	return uint64(total)
}
