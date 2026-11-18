package host

import (
	"context"
	"log/slog"
	"time"

	contracts "github.com/Herrscherd/herrscher-contracts"
	"github.com/Herrscherd/herrscher/core/eventwire"
	"github.com/Herrscherd/herrscher/core/internal/obs"
)

func (d *sessionDriver) Pick(value string) bool {
	return d.enqueue(queued{ev: contracts.Event{T: eventwire.EventPick, Value: value}})
}

func (d *sessionDriver) enqueue(item queued) bool {
	select {
	case d.queue <- item:
		return true
	case <-time.After(d.enqueueTimeout):
		return false
	}
}

const interruptSendTimeout = 2 * time.Second

func (d *sessionDriver) Interrupt() {
	go func() {
		select {
		case d.toBridge <- contracts.Event{T: eventwire.EventInterrupt}:
		case <-time.After(interruptSendTimeout):
		}
	}()
}

func (d *sessionDriver) emitPaused(ctx context.Context, reason string) {
	d.fanOut(ctx, contracts.Event{T: eventwire.EventPaused, Text: reason})
}

func (d *sessionDriver) Seed(task string) bool {
	return d.enqueue(queued{ev: contracts.Event{T: eventwire.EventInput, Who: "handoff", Text: task, TurnID: newTurnID()}})
}

func (d *sessionDriver) SeedAndWait(ctx context.Context, task string) (string, bool) {
	return d.SeedAndWaitWithTurnID(ctx, task, newTurnID())
}

func (d *sessionDriver) SeedAndWaitWithTurnID(ctx context.Context, task, turnID string) (string, bool) {
	reply := make(chan string, 1)
	d.seenMu.Lock()
	d.pendingReply = reply
	d.seenMu.Unlock()
	select {
	case d.queue <- queued{ev: contracts.Event{T: eventwire.EventInput, Who: authorSeed, Text: task, TurnID: turnID}}:
	case <-ctx.Done():
		d.seenMu.Lock()
		d.pendingReply = nil
		d.seenMu.Unlock()
		return "", false
	}
	select {
	case r := <-reply:
		return r, true
	case <-ctx.Done():
		d.seenMu.Lock()
		d.pendingReply = nil
		d.seenMu.Unlock()
		select {
		case r := <-reply:
			return r, true
		default:
			return "", false
		}
	}
}

func (d *sessionDriver) run(ctx context.Context) {
	for _, gateway := range d.gateways {
		if gateway.Reader != nil {
			go d.poll(ctx, gateway.Reader)
		}
	}
	go d.deliverNotices(ctx)
	d.pump(ctx)
}

func (d *sessionDriver) poll(ctx context.Context, reader contracts.ChannelReader) {
	channel := d.channel
	if channel == "" {
		channel = reader.DefaultChannel()
	}
	log := obs.Stderr(false).With("component", "poll", keySession, d.name, "channel", channel)
	var last string
	if d.channel != "" {
		cursor, ok := initPollCursor(ctx, reader, channel, log)
		if !ok {
			return
		}
		last = cursor
	}
	d.pollLoop(ctx, reader, channel, last, log)
}

func initPollCursor(ctx context.Context, reader contracts.ChannelReader, channel string, log *slog.Logger) (string, bool) {
	for delay := time.Second; ; delay = min(backoffFactor*delay, maxPollBackoff) {
		msgs, err := reader.Read(ctx, channel, 1, "")
		if err == nil {
			return lastMessageID(msgs), true
		}
		log.Warn("cursor init failed; retrying rather than replaying history", "err", err, "retry_in", delay)
		if !sleepOrDone(ctx, delay) {
			return "", false
		}
	}
}

func lastMessageID(msgs []contracts.Message) string {
	if len(msgs) == 0 {
		return ""
	}
	return msgs[len(msgs)-1].ID
}

func sleepOrDone(ctx context.Context, delay time.Duration) bool {
	select {
	case <-ctx.Done():
		return false
	case <-time.After(delay):
		return true
	}
}

func (d *sessionDriver) pollLoop(ctx context.Context, reader contracts.ChannelReader, channel, last string, log *slog.Logger) {
	delay := pollInterval
	for ctx.Err() == nil {
		msgs, err := reader.Read(ctx, channel, pollBatchSize, last)
		if err != nil {
			delay = min(backoffFactor*delay, maxPollBackoff)
			log.Warn("poll read failed", "err", err, "retry_in", delay)
		} else {
			delay = pollInterval
			var ok bool
			if last, ok = d.forwardPolled(ctx, msgs, last); !ok {
				return
			}
		}
		if !sleepOrDone(ctx, delay) {
			return
		}
	}
}

func (d *sessionDriver) forwardPolled(ctx context.Context, msgs []contracts.Message, last string) (string, bool) {
	for _, msg := range msgs {
		if msg.AuthorBot {
			continue
		}
		last = msg.ID
		if !d.submit(ctx, contracts.Inbound{
			Author:      msg.AuthorName,
			AuthorID:    msg.AuthorID,
			Text:        msg.Content,
			Attachments: msg.Attachments,
			MessageID:   contracts.MessageID(msg.ID),
		}) {
			return last, false
		}
	}
	return last, true
}

func (d *sessionDriver) submit(ctx context.Context, inbound contracts.Inbound) bool {
	d.journal(inbound.AuthorID)
	atts := d.resolveAttachments(ctx, contracts.Message{
		ID:          string(inbound.MessageID),
		Content:     inbound.Text,
		AuthorID:    inbound.AuthorID,
		AuthorName:  inbound.Author,
		Attachments: inbound.Attachments,
	})
	select {
	case d.queue <- queued{
		ev:     contracts.Event{T: eventwire.EventInput, Who: inbound.Author, Text: inbound.Text, Attachments: atts},
		origin: inbound.Conversation,
	}:
		return true
	case <-ctx.Done():
		return false
	}
}
