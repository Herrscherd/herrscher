package host

import (
	"context"
	"log/slog"
	"os"
	"slices"
	"strings"
	"time"

	contracts "github.com/Herrscherd/herrscher-contracts"
	"github.com/Herrscherd/herrscher/core/eventwire"
	"github.com/Herrscherd/herrscher/core/outbox"
)

func (d *sessionDriver) decodeTurnFiles(event contracts.Event) outbox.Frame {
	frame, err := outbox.Decode(event.Value)
	if err != nil {
		slog.Warn("outbound files dropped", "err", err)
		return outbox.Frame{}
	}
	if frame.Dir != "" && !outbox.Owns(d.outboxRoot, frame.Dir) {
		slog.Warn("outbound files dropped, turn directory outside the outbox", "dir", frame.Dir)
		return outbox.Frame{}
	}
	return frame
}

func (d *sessionDriver) replaceTurnFiles(previous outbox.Frame, event contracts.Event) outbox.Frame {
	next := d.decodeTurnFiles(event)
	switch {
	case next.Dir == "":
		return previous
	case previous.Dir == "":
		return next
	case previous.Dir == next.Dir:
		next.Files = slices.Concat(previous.Files, next.Files)
		next.Refused = slices.Concat(previous.Refused, next.Refused)
		return next
	default:
		slog.Warn("earlier outbound files of the turn dropped", "dir", previous.Dir)
		d.removeTurnFiles(previous)
		return next
	}
}

func frameNames(files outbox.Frame) []string {
	return append(fileNames(files.Files), files.Refused...)
}

func fileSendersOf(gateways []contracts.Gateway) map[string]contracts.FileSender {
	senders := make(map[string]contracts.FileSender, len(gateways))
	for _, gateway := range gateways {
		if sender, ok := gateway.(contracts.FileSender); ok {
			senders[gateway.Manifest().Kind] = sender
		}
	}
	return senders
}

func (d *sessionDriver) noticeNames(names []string) string {
	labelled := make([]string, 0, len(names))
	for _, name := range names {
		if name == "" {
			name = d.hostText("outbound.unnamed", nil)
		}
		labelled = append(labelled, name)
	}
	return strings.Join(labelled, ", ")
}

func fileNames(files []contracts.OutboundFile) []string {
	names := make([]string, 0, len(files))
	for _, file := range files {
		names = append(names, file.Name)
	}
	return names
}

const (
	fileSendTimeout   = 2 * time.Minute
	namesParam        = "Names"
	filesPerSendBatch = 10
	bytesPerSendBatch = 9 << 20
	unmeasuredBytes   = bytesPerSendBatch + 1
)

func (d *sessionDriver) sendReplyFiles(ctx context.Context, index int, gateway contracts.GatewaySet, sender contracts.FileSender, files []contracts.OutboundFile) {
	batches := sendBatches(files, outboundSize)
	for position, batch := range batches {
		if ctx.Err() != nil {
			d.noticeUnsentFiles(ctx, index, gateway, slices.Concat(batches[position:]...))
			return
		}
		d.sendFileBatch(ctx, index, gateway, sender, batch)
	}
}

func (d *sessionDriver) noticeUnsentFiles(ctx context.Context, index int, gateway contracts.GatewaySet, files []contracts.OutboundFile) {
	notice := d.hostText("outbound.failed", map[string]any{namesParam: d.noticeNames(fileNames(files))})
	d.emitToGateway(ctx, index, gateway, d.stamp(contracts.Event{T: eventwire.EventStatus, Text: notice}))
}

func (d *sessionDriver) sendFileBatch(ctx context.Context, index int, gateway contracts.GatewaySet, sender contracts.FileSender, batch []contracts.OutboundFile) {
	conv := contracts.Conversation{Gateway: gateway.Gateway.Manifest().Kind, ID: d.renderChannel(gateway)}
	sendCtx, cancel := context.WithTimeout(ctx, fileSendTimeout)
	defer cancel()
	if _, err := sender.SendFiles(sendCtx, conv, "", "", batch); err != nil {
		slog.Warn("reply files not sent", "gateway", conv.Gateway, "err", err)
		d.noticeUnsentFiles(ctx, index, gateway, batch)
	}
}

func sendBatches(files []contracts.OutboundFile, sizeOf func(contracts.OutboundFile) int64) [][]contracts.OutboundFile {
	var batches [][]contracts.OutboundFile
	var batch []contracts.OutboundFile
	var batchBytes int64
	for _, file := range files {
		size := sizeOf(file)
		if len(batch) > 0 && (len(batch) == filesPerSendBatch || batchBytes+size > bytesPerSendBatch) {
			batches = append(batches, batch)
			batch, batchBytes = nil, 0
		}
		batch = append(batch, file)
		batchBytes += size
	}
	if len(batch) > 0 {
		batches = append(batches, batch)
	}
	return batches
}

func outboundSize(file contracts.OutboundFile) int64 {
	info, err := os.Stat(file.Path)
	if err != nil {
		slog.Warn("outbound file not measured", "file", file.Name, "err", err)
		return unmeasuredBytes
	}
	return info.Size()
}

func (d *sessionDriver) removeTurnFiles(files outbox.Frame) {
	if files.Dir == "" {
		return
	}
	if err := outbox.Remove(d.outboxRoot, files.Dir); err != nil {
		slog.Warn("turn outbox not removed", "err", err)
	}
}
