package host

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"os"
	"slices"
	"strings"
	"sync"

	contracts "github.com/Herrscherd/herrscher-contracts"
	"github.com/Herrscherd/herrscher/core/compactwire"
	"github.com/Herrscherd/herrscher/core/config"
	"github.com/Herrscherd/herrscher/core/contextwindow"
	"github.com/Herrscherd/herrscher/core/internal/state"
)

const (
	defaultCompactPercent = 92
	percentScale          = 100
	eventCompactNow       = "compact_now"
	eventCompactStarted   = "compaction_started"
	eventCompactDone      = "compaction_done"
	eventCompactFailed    = "compaction_failed"
)

var (
	errCompactUnsupported = errors.New("its backend does not implement Compactor")
	errBridgeGone         = errors.New("the bridge closed during a compaction")
	errNoBackendFactory   = errors.New("declares Capabilities.Compactor but provides no backend factory")
	errNotCompactorPort   = errors.New("declares Capabilities.Compactor but does not implement the port")
	errNoCompactorPlugin  = errors.New("no plugin declares Capabilities.Compactor under this name")
	errCompactQueueFull   = errors.New("the turn queue is full, the compaction was not queued")
)

type compactionPolicy struct {
	percent  int
	limits   contextwindow.Limits
	names    []string
	plugins  map[string]contracts.Compactor
	backends map[string]bool
	warnOnce *sync.Once
	warn     func(format string, args ...any)
}

func newCompactionPolicy(ctx context.Context, percent *int, names []string, perVendor map[string]int) (compactionPolicy, error) {
	policy := compactionPolicy{
		percent:  defaultCompactPercent,
		names:    []string{contracts.CompactStageBackend},
		plugins:  make(map[string]contracts.Compactor),
		backends: make(map[string]bool),
		warnOnce: &sync.Once{},
		warn:     warnStderr,
	}
	if percent != nil {
		if err := contracts.ValidateAutoCompactPercent(*percent); err != nil {
			return compactionPolicy{}, fmt.Errorf("compactAt: %w", err)
		}
		policy.percent = *percent
	}
	if err := config.ValidateCompactStack(names); err != nil {
		return compactionPolicy{}, fmt.Errorf("compactStack: %w", err)
	}
	if len(names) > 0 {
		policy.names = names
	}
	warnInvalidContextLimit(policy.warn, os.Getenv(contextwindow.EnvVar()))
	policy.limits = contextwindow.NewLimits(os.Getenv(contextwindow.EnvVar()), perVendor)
	backends := contracts.Default.Backends()
	kinds := make([]string, 0, len(backends))
	for index := range backends {
		kind := backends[index].Manifest.Kind
		kinds = append(kinds, kind)
		if backends[index].Manifest.Capabilities.Compactor {
			policy.backends[kind] = true
		}
	}
	warnUnknownContextLimits(policy.warn, perVendor, kinds)
	for _, name := range policy.names {
		if name == contracts.CompactStageBackend {
			continue
		}
		compactor, err := customStage(ctx, name)
		if err != nil {
			return compactionPolicy{}, fmt.Errorf("compactStack %q: %w", name, err)
		}
		policy.plugins[name] = compactor
	}
	return policy, nil
}

func warnInvalidContextLimit(warn func(format string, args ...any), raw string) {
	if err := contextwindow.CheckOverride(raw); err != nil {
		warn("herrscher serve: %s ignored, %v: the vendor default window applies\n", contextwindow.EnvVar(), err)
	}
}

func warnUnknownContextLimits(warn func(format string, args ...any), perVendor map[string]int, kinds []string) {
	for _, vendor := range slices.Sorted(maps.Keys(perVendor)) {
		if !slices.ContainsFunc(kinds, func(kind string) bool { return strings.EqualFold(kind, vendor) }) {
			warn("herrscher serve: contextLimits: no backend %q in this daemon, its window size is unused\n", vendor)
		}
	}
}

func customStage(ctx context.Context, name string) (contracts.Compactor, error) {
	plugins := contracts.Default.Plugins()
	for index := range plugins {
		plugin := &plugins[index]
		if plugin.Manifest.Kind != name || !plugin.Manifest.Capabilities.Compactor {
			continue
		}
		if plugin.Backend == nil {
			return nil, errNoBackendFactory
		}
		backend, err := plugin.Backend(ctx, contracts.PluginConfig{})
		if err != nil {
			return nil, err
		}
		compactor, ok := backend.(contracts.Compactor)
		if !ok {
			return nil, errNotCompactorPort
		}
		return compactor, nil
	}
	return nil, errNoCompactorPlugin
}

func (p compactionPolicy) percentFor(sess state.Session) int {
	if sess.CompactPercent != nil {
		return *sess.CompactPercent
	}
	return p.percent
}

func (p compactionPolicy) supports(vendor string) bool {
	return p.backends[vendor]
}

func (p compactionPolicy) warnNoPort(vendor string) {
	p.warnOnce.Do(func() {
		p.warn("herrscher serve: backend %q declares Capabilities.Compactor but its instance does not implement the port, its compaction is refused\n", vendor)
	})
}

func (d *sessionDriver) compactPercent() int {
	if d.percentOf != nil {
		return d.percentOf()
	}
	return d.compaction.percent
}

func (d *sessionDriver) contextLimit() int {
	return d.compaction.limits.Limit(d.vendor)
}

func (d *sessionDriver) compactSupported() bool {
	return d.compaction.supports(d.vendor)
}

func (d *sessionDriver) compactStages() ([]contracts.CompactStage, error) {
	if !d.compactSupported() {
		return nil, fmt.Errorf("session %q runs %q: %w", d.name, d.vendor, errCompactUnsupported)
	}
	stages := make([]contracts.CompactStage, 0, len(d.compaction.names))
	for _, name := range d.compaction.names {
		if name == contracts.CompactStageBackend {
			stages = append(stages, contracts.CompactStage{Name: name, Compactor: bridgeCompactor{driver: d}})
			continue
		}
		stages = append(stages, contracts.CompactStage{Name: name, Compactor: d.compaction.plugins[name]})
	}
	return stages, nil
}

func crossed(used, limit, percent int) bool {
	return used*percentScale >= percent*limit
}

func (d *sessionDriver) maybeAutoCompact(ctx context.Context) {
	percent := d.compactPercent()
	limit := d.contextLimit()
	if percent == 0 || d.turnReading == 0 {
		return
	}
	if !crossed(d.turnReading, limit, percent) {
		d.compactSpent = false
		return
	}
	if d.compactSpent {
		return
	}
	d.compactSpent = true
	result, err := d.runCompaction(ctx, contracts.TriggerAutomatic, "")
	if err != nil || !result.TokensMeasured {
		return
	}
	if crossed(result.TokensAfter, limit, percent) {
		d.fanOut(ctx, contracts.Event{T: eventStatus, Text: fmt.Sprintf(
			"compaction automatique sans effet : le contexte reste au-dessus de %d %%, pas de nouvelle tentative avant qu'il repasse sous le seuil", percent)})
	}
}

func (d *sessionDriver) runCompaction(ctx context.Context, trigger contracts.CompactTrigger, instruction string) (contracts.CompactResult, error) {
	stages, err := d.compactStages()
	if err != nil {
		d.fanOut(ctx, contracts.Event{T: eventCompactFailed, Value: trigger.String(), Text: err.Error()})
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
	d.fanOut(ctx, contracts.Event{T: eventCompactStarted, Value: trigger.String(), Tokens: d.turnReading})
	result, err := contracts.RunCompactStack(ctx, stages, req, d.compactionProgress(ctx))
	if err != nil {
		d.fanOut(ctx, contracts.Event{T: eventCompactFailed, Value: trigger.String(), Text: err.Error()})
		return contracts.CompactResult{}, err
	}
	d.settleCompaction(result, limit)
	d.fanOut(ctx, contracts.Event{T: eventCompactDone, Value: trigger.String(), Text: result.Note, Tokens: measuredTokens(result)})
	return result, nil
}

func (d *sessionDriver) settleCompaction(result contracts.CompactResult, limit int) {
	if !result.TokensMeasured {
		return
	}
	d.turnReading = result.TokensAfter
	d.compactSpent = crossed(result.TokensAfter, limit, d.compactPercent())
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
			d.fanOut(ctx, contracts.Event{T: eventStatus, Text: event.Detail})
		}
	}
}

func (d *sessionDriver) sessionInfo() contracts.SessionInfo {
	return contracts.SessionInfo{Name: d.name, Incarnation: d.incarnation, Vendor: d.vendor, AutoCompactPercent: d.compactPercent()}
}

func (d *sessionDriver) runCompactNow(ctx context.Context, instruction string) {
	if _, err := d.runCompaction(ctx, contracts.TriggerManual, instruction); err != nil {
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
	request.queued = d.enqueue(queued{ev: contracts.Event{T: eventCompactNow}, origin: origin})
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

type bridgeCompactor struct {
	driver *sessionDriver
}

func (c bridgeCompactor) Compact(ctx context.Context, req contracts.CompactRequest, onEvent func(contracts.BackendEvent)) (contracts.CompactResult, error) {
	return c.driver.compactThroughBridge(ctx, req, onEvent)
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
				return compactwire.DecodeResult(frame), nil
			case compactwire.EventError:
				if frame.Value == compactwire.NoCompactor {
					d.compaction.warnNoPort(d.vendor)
				}
				return contracts.CompactResult{}, compactwire.DecodeFailure(frame)
			case compactwire.EventProgress:
				onEvent(contracts.BackendEvent{Kind: "progress", Detail: frame.Text})
			}
		}
	}
}
