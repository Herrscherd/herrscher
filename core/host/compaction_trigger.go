package host

import (
	"context"
	"errors"
	"slices"
	"time"

	contracts "github.com/Herrscherd/herrscher-contracts"
	"github.com/Herrscherd/herrscher/core/eventwire"
)

func (d *sessionDriver) compactPercent() int {
	if d.percentOf != nil {
		return d.percentOf()
	}
	return d.compaction.percent
}

func (d *sessionDriver) modelNow() modelTerms {
	if d.terms == nil {
		return modelTerms{}
	}
	return d.terms()
}

func (d *sessionDriver) contextLimit() int {
	now := d.modelNow()
	return d.compaction.limits.LimitFor(now.vendor, now.window)
}

func (d *sessionDriver) compactSupported() bool {
	return d.compaction.supports(d.modelNow().vendor)
}

func (d *sessionDriver) compactStages(names []string, notes stageNotes) ([]contracts.CompactStage, error) {
	if !d.compactSupported() {
		return nil, unsupportedCompaction(d.name, d.modelNow().vendor)
	}
	stages := make([]contracts.CompactStage, 0, len(names))
	for _, name := range names {
		var compactor contracts.Compactor = bridgeCompactor{driver: d}
		if name != contracts.CompactStageBackend {
			compactor = historyStage{compactor: d.compaction.plugins[name], history: d.sessionHistory()}
		}
		stages = append(stages, contracts.CompactStage{Name: name, Compactor: recordedStage{name: name, compactor: compactor, notes: notes}})
	}
	return stages, nil
}

func (d *sessionDriver) sessionHistory() contracts.HistoryEditor {
	if !d.compaction.editsHistory(d.modelNow().vendor) {
		return nil
	}
	return bridgeHistory{driver: d}
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

func crossed(used, limit, percent int) bool {
	return used*percentScale >= percent*limit
}

type compactionPass struct {
	names []string
	full  bool
}

func (p compactionPass) due() bool {
	return p.full || len(p.names) > 0
}

func (p *compactionPolicy) stageThreshold(name string, host int) int {
	own := p.triggers[name]
	if host == 0 || own == 0 || own >= host {
		return host
	}
	return own
}

func (d *sessionDriver) automaticRun(used, limit int) compactionPass {
	host := d.compactPercent()
	if host == 0 {
		return compactionPass{}
	}
	if !crossed(used, limit, host) {
		d.hostSpent = false
	} else if !d.hostSpent {
		return compactionPass{names: d.compaction.names, full: true}
	}
	var due []string
	for _, name := range d.compaction.names {
		threshold := d.compaction.stageThreshold(name, host)
		if threshold == host {
			continue
		}
		if !crossed(used, limit, threshold) {
			d.stageSpent[name] = false
			continue
		}
		if !d.stageSpent[name] {
			due = append(due, name)
		}
	}
	return compactionPass{names: due}
}

func (d *sessionDriver) markSpent(run compactionPass) {
	if run.full {
		d.hostSpent = true
	}
	for _, name := range run.names {
		d.stageSpent[name] = true
	}
}

func (d *sessionDriver) maybeAutoCompact(ctx context.Context) {
	if d.turnReading == 0 {
		return
	}
	limit := d.contextLimit()
	run := d.automaticRun(d.turnReading, limit)
	if !run.due() {
		return
	}
	settled := d.runAutomatic(ctx, run)
	if !settled || run.full || d.hostSpent || !crossed(d.turnReading, limit, d.compactPercent()) {
		return
	}
	rest := stagesAfter(d.compaction.names, run.names)
	if len(rest) == 0 {
		return
	}
	d.runAutomatic(ctx, compactionPass{names: rest, full: true})
}

func stagesAfter(stack, ran []string) []string {
	rest := make([]string, 0, len(stack))
	for _, name := range stack {
		if !slices.Contains(ran, name) {
			rest = append(rest, name)
		}
	}
	return rest
}

func (d *sessionDriver) runAutomatic(ctx context.Context, run compactionPass) bool {
	d.markSpent(run)
	result, err := d.runCompaction(ctx, contracts.TriggerAutomatic, "", run)
	if err != nil || !result.TokensMeasured {
		return false
	}
	if percent := d.compactPercent(); run.full && crossed(result.TokensAfter, d.contextLimit(), percent) {
		d.fanOut(ctx, contracts.Event{T: eventwire.EventStatus, Text: d.hostText("compaction.no_effect", map[string]any{"Percent": percent})})
	}
	return true
}
