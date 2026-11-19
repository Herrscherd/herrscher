package host

import (
	"context"
	"errors"
	"time"

	contracts "github.com/Herrscherd/herrscher-contracts"
	"github.com/Herrscherd/herrscher/core/compactwire"
	"github.com/Herrscherd/herrscher/core/eventwire"
)

func (d *sessionDriver) runCompaction(ctx context.Context, trigger contracts.CompactTrigger, instruction string, run compactionPass) (contracts.CompactResult, error) {
	d.compacting.Store(true)
	defer d.compacting.Store(false)
	notes := make(stageNotes)
	stages, err := d.compactStages(run.names, notes)
	if err != nil {
		d.failCompaction(ctx, trigger, err)
		return contracts.CompactResult{}, err
	}
	limit := d.contextLimit()
	measured := d.turnReading > 0
	req := contracts.CompactRequest{
		Session:       d.sessionInfo(),
		Trigger:       trigger,
		UsageMeasured: measured,
		TokensUsed:    d.turnReading,
		TokenLimit:    limit,
		Instruction:   instruction,
	}
	d.fanOut(ctx, contracts.Event{T: eventwire.EventCompactionStarted, Value: trigger.String(), Tokens: d.turnReading})
	result, err := contracts.RunCompactStack(ctx, stages, req, d.compactionProgress(ctx))
	d.recordCompaction(result.StageStats, notes)
	if err != nil {
		d.failCompaction(ctx, trigger, err)
		return contracts.CompactResult{}, err
	}
	d.settleCompaction(result, limit, run)
	d.announceCompaction(ctx, trigger, result)
	return result, nil
}

func (d *sessionDriver) settleCompaction(result contracts.CompactResult, limit int, run compactionPass) {
	if !result.TokensMeasured {
		return
	}
	d.turnReading = result.TokensAfter
	host := d.compactPercent()
	if run.full {
		d.hostSpent = crossed(result.TokensAfter, limit, host)
	}
	for _, name := range run.names {
		d.stageSpent[name] = crossed(result.TokensAfter, limit, d.compaction.stageThreshold(name, host))
	}
}

func measuredTokens(result contracts.CompactResult) int {
	if !result.TokensMeasured {
		return 0
	}
	return result.TokensAfter
}

func (d *sessionDriver) compactionProgress(ctx context.Context) func(contracts.BackendEvent) {
	return func(event contracts.BackendEvent) {
		if event.Detail != "" {
			d.fanOut(ctx, contracts.Event{T: eventwire.EventStatus, Text: event.Detail})
		}
	}
}

func (d *sessionDriver) sessionInfo() contracts.SessionInfo {
	return contracts.SessionInfo{Name: d.name, Incarnation: d.incarnation, Vendor: d.modelNow().vendor, AutoCompactPercent: d.compactPercent(), ContextWindow: d.contextLimit(), Language: d.currentLanguage()}
}

func (d *sessionDriver) runCompactNow(ctx context.Context, instruction string) {
	if _, err := d.runCompaction(ctx, contracts.TriggerManual, instruction, compactionPass{names: d.compaction.names, full: true}); err != nil {
		return
	}
}

type compactRequest struct {
	instruction string
	done        chan struct{}
	queued      bool
}

func (d *sessionDriver) requestCompaction(instruction string, origin contracts.Conversation) (bool, bool) {
	d.compactMu.Lock()
	if waiting := d.compactWaiting; waiting != nil {
		if instruction != "" {
			waiting.instruction = instruction
		}
		d.compactMu.Unlock()
		<-waiting.done
		return true, waiting.queued
	}
	request := &compactRequest{instruction: instruction, done: make(chan struct{})}
	d.compactWaiting = request
	d.compactMu.Unlock()
	if d.compactRegistered != nil {
		d.compactRegistered()
	}
	request.queued = d.enqueue(queued{ev: contracts.Event{T: eventwire.EventCompactNow}, origin: origin})
	if !request.queued {
		d.clearCompactWaiting(request)
	}
	close(request.done)
	return false, request.queued
}

func (d *sessionDriver) clearCompactWaiting(request *compactRequest) {
	d.compactMu.Lock()
	defer d.compactMu.Unlock()
	if d.compactWaiting == request {
		d.compactWaiting = nil
	}
}

func (d *sessionDriver) takeCompactInstruction() string {
	d.compactMu.Lock()
	defer d.compactMu.Unlock()
	waiting := d.compactWaiting
	if waiting == nil {
		return ""
	}
	d.compactWaiting = nil
	return waiting.instruction
}

type historyStage struct {
	compactor contracts.Compactor
	history   contracts.HistoryEditor
}

func (s historyStage) Compact(ctx context.Context, req contracts.CompactRequest, onEvent func(contracts.BackendEvent)) (contracts.CompactResult, error) {
	if s.compactor == nil {
		return contracts.CompactResult{}, contracts.ErrNilCompactor
	}
	req.History = s.history
	return s.compactor.Compact(ctx, req, onEvent)
}

type recordedStage struct {
	name      string
	compactor contracts.Compactor
	notes     stageNotes
}

func (s recordedStage) Compact(ctx context.Context, req contracts.CompactRequest, onEvent func(contracts.BackendEvent)) (contracts.CompactResult, error) {
	started := time.Now()
	result, err := s.compactor.Compact(ctx, req, onEvent)
	if errors.Is(err, contracts.ErrNilCompactor) {
		return result, err
	}
	s.notes.fail(s.name, err)
	if result.Stats != nil {
		stats := *result.Stats
		stats.Stage = s.name
		result.Stats = &stats
		return result, err
	}
	if err == nil {
		return result, nil
	}
	result.Stats = &contracts.CompactStats{Stage: s.name, Outcome: contracts.CompactFailed, Duration: time.Since(started)}
	return result, err
}

type bridgeCompactor struct {
	driver *sessionDriver
}

func (c bridgeCompactor) Compact(ctx context.Context, req contracts.CompactRequest, onEvent func(contracts.BackendEvent)) (contracts.CompactResult, error) {
	return c.driver.compactThroughBridge(ctx, req, onEvent)
}

func (d *sessionDriver) decodeCompactResult(frame contracts.Event) (contracts.CompactResult, error) {
	res, err := compactwire.DecodeResult(frame)
	if errors.Is(err, compactwire.ErrNoticeMalformed) {
		warnTo(d.messages, d.warnings, d.textLanguage()(), "warn.session_compaction_notice", map[string]any{nameParam: d.name, detailParam: err})
		return res, nil
	}
	return res, err
}

func (d *sessionDriver) compactThroughBridge(ctx context.Context, req contracts.CompactRequest, onEvent func(contracts.BackendEvent)) (contracts.CompactResult, error) {
	frame, err := compactwire.Request(req)
	if err != nil {
		return contracts.CompactResult{}, err
	}
	select {
	case d.toBridge <- frame:
	case <-d.hangup:
		return contracts.CompactResult{}, errBridgeGone
	case <-ctx.Done():
		return contracts.CompactResult{}, ctx.Err()
	}
	for {
		select {
		case <-ctx.Done():
			d.Interrupt()
			return contracts.CompactResult{}, ctx.Err()
		case <-d.hangup:
			return contracts.CompactResult{}, errBridgeGone
		case frame, ok := <-d.from:
			if !ok {
				return contracts.CompactResult{}, errBridgeGone
			}
			switch frame.T {
			case compactwire.EventResult:
				return d.decodeCompactResult(frame)
			case compactwire.EventError:
				if frame.Value == compactwire.NoCompactor {
					d.compaction.warnNoPort(d.modelNow().vendor)
				}
				return contracts.CompactResult{}, compactwire.DecodeFailure(frame)
			case compactwire.EventProgress:
				onEvent(contracts.BackendEvent{Kind: "progress", Detail: frame.Text})
			}
		}
	}
}
