package host

import (
	"context"
	"errors"
	"fmt"
	"io"
	"maps"
	"os"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	contracts "github.com/Herrscherd/herrscher-contracts"
	"github.com/Herrscherd/herrscher-contracts/i18n"
	"github.com/Herrscherd/herrscher/core/config"
	"github.com/Herrscherd/herrscher/core/contextwindow"
	"github.com/Herrscherd/herrscher/core/historywire"
	"github.com/Herrscherd/herrscher/core/internal/state"
)

const (
	defaultCompactPercent = 92
	percentScale          = 100
	noticeTimeout         = 10 * time.Second
	noticeQueueCapacity   = 32
)

type compactUnavailableError struct {
	cause  error
	reason string
}

func (e compactUnavailableError) Error() string {
	return e.reason
}

func (e compactUnavailableError) Unwrap() error {
	return e.cause
}

func (e compactUnavailableError) Notice() contracts.Notice {
	return contracts.Notice{Code: contracts.CodeCompactUnavailable, Args: []contracts.Arg{contracts.Text("Reason", e.reason)}}
}

func unsupportedCompaction(session, vendor string) error {
	return compactUnavailableError{
		cause:  errCompactUnsupported,
		reason: fmt.Sprintf("session %q runs %q: its backend does not implement Compactor", session, vendor),
	}
}

func queueFullCompaction(session string) error {
	return compactUnavailableError{
		cause:  errCompactQueueFull,
		reason: fmt.Sprintf("session %q: the turn queue is full, the compaction was not queued", session),
	}
}

var (
	errCompactUnsupported = errors.New("its backend does not implement Compactor")
	errBridgeGone         = errors.New("the bridge closed during a compaction")
	errHistoryTimeout     = errors.New("the bridge did not answer a history call in time")
	errTooManyEdits       = errors.New("the compaction would edit too many tool calls at once")
	errNoCompactorFactory = errors.New("is a compactor plugin but provides no Compactor factory")
	errNilCompactor       = errors.New("its Compactor factory returned no stage")
	errNoCompactorPlugin  = errors.New("no compactor plugin is registered under this name")
	errCompactorSettings  = errors.New("the compaction stage is missing required settings")
	errCompactQueueFull   = errors.New("the turn queue is full, the compaction was not queued")
)

type compactorSettingsError struct {
	messages *hostMessages
	missing  *contracts.MissingSettingsError
}

func (e compactorSettingsError) Error() string {
	return e.render(e.messages, i18n.English)
}

func (e compactorSettingsError) Unwrap() []error {
	return []error{errCompactorSettings, e.missing}
}

func (e compactorSettingsError) Notice() contracts.Notice {
	return contracts.Notice{Code: contracts.CodeSettingMissing, Args: []contracts.Arg{contracts.Text("Settings", strings.Join(e.missing.Missing, ", "))}}
}

func (e compactorSettingsError) render(_ *hostMessages, lang i18n.Language) string {
	text, err := contracts.NewRenderer().Render(lang, e.Notice())
	if err != nil {
		return e.missing.Error()
	}
	return text
}

type compactionPolicy struct {
	messages       *hostMessages
	percent        int
	limits         contextwindow.Limits
	names          []string
	plugins        map[string]contracts.Compactor
	triggers       map[string]int
	backends       map[string]bool
	historyEditors map[string]bool
	warnOnce       *sync.Once
	historyOnce    *sync.Once
	warn           func(key string, params map[string]any)
}

func newCompactionPolicy(ctx context.Context, messages *hostMessages, lang languageSource, warn io.Writer, percent *int, names []string, perVendor map[string]int) (compactionPolicy, error) {
	policy := compactionPolicy{
		messages:       messages,
		percent:        defaultCompactPercent,
		names:          []string{contracts.CompactStageBackend},
		plugins:        make(map[string]contracts.Compactor),
		triggers:       make(map[string]int),
		backends:       make(map[string]bool),
		historyEditors: make(map[string]bool),
		warnOnce:       &sync.Once{},
		historyOnce:    &sync.Once{},
		warn: func(key string, params map[string]any) {
			warnLive(messages, warn, lang, key, params)
		},
	}
	if percent != nil {
		if err := contracts.ValidateAutoCompactPercent(*percent); err != nil {
			return compactionPolicy{}, wrapHost(messages, err, "compaction.at", nil)
		}
		policy.percent = *percent
	}
	if err := config.ValidateCompactStack(names); err != nil {
		return compactionPolicy{}, wrapHost(messages, err, "compaction.stack", nil)
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
		capabilities := backends[index].Manifest.Capabilities
		if capabilities.Compactor {
			policy.backends[kind] = true
		}
		if capabilities.HistoryEditor {
			policy.historyEditors[kind] = true
		}
	}
	warnUnknownContextLimits(policy.warn, perVendor, kinds)
	for _, name := range policy.names {
		if name == contracts.CompactStageBackend {
			continue
		}
		if err := policy.addStage(ctx, name); err != nil {
			return compactionPolicy{}, err
		}
	}
	return policy, nil
}

func (p *compactionPolicy) addStage(ctx context.Context, name string) error {
	compactor, err := customStage(ctx, p.messages, name, os.Getenv)
	if err != nil {
		return wrapHost(p.messages, err, "compaction.stack_named", map[string]any{nameParam: name})
	}
	if trigger, ok := compactor.(contracts.StageTrigger); ok {
		percent := trigger.TriggerPercent()
		if err := contracts.ValidateStageTriggerPercent(percent); err != nil {
			return wrapHost(p.messages, err, "compaction.stage_trigger", map[string]any{nameParam: name, "Percent": percent})
		}
		if percent != 0 {
			p.triggers[name] = percent
		}
	}
	p.plugins[name] = compactor
	return nil
}

func warnInvalidContextLimit(warn func(key string, params map[string]any), raw string) {
	if err := contextwindow.CheckOverride(raw); err != nil {
		warn("warn.context_limit_env", map[string]any{nameParam: contextwindow.EnvVar(), detailParam: err})
	}
}

func warnUnknownContextLimits(warn func(key string, params map[string]any), perVendor map[string]int, kinds []string) {
	for _, vendor := range slices.Sorted(maps.Keys(perVendor)) {
		if !slices.ContainsFunc(kinds, func(kind string) bool { return strings.EqualFold(kind, vendor) }) {
			warn("warn.context_limit_unknown_vendor", map[string]any{nameParam: vendor})
		}
	}
}

func customStage(ctx context.Context, messages *hostMessages, name string, getenv func(string) string) (contracts.Compactor, error) {
	plugins := contracts.Default.Compactors()
	for index := range plugins {
		plugin := &plugins[index]
		if plugin.Manifest.Kind != name {
			continue
		}
		if plugin.Compactor == nil {
			return nil, errNoCompactorFactory
		}
		cfg, err := contracts.Resolve(plugin.Manifest.Config, getenv)
		if missing, ok := errors.AsType[*contracts.MissingSettingsError](err); ok {
			return nil, compactorSettingsError{messages: messages, missing: missing}
		}
		if err != nil {
			return nil, err
		}
		compactor, err := plugin.Compactor(ctx, cfg)
		if err != nil {
			return nil, err
		}
		if compactor == nil {
			return nil, errNilCompactor
		}
		return compactor, nil
	}
	return nil, errNoCompactorPlugin
}

func (p *compactionPolicy) percentFor(sess state.Session) int {
	if sess.CompactPercent != nil {
		return *sess.CompactPercent
	}
	return p.percent
}

func (p *compactionPolicy) supports(vendor string) bool {
	return p.backends[vendor]
}

func (p *compactionPolicy) editsHistory(vendor string) bool {
	return p.historyEditors[vendor]
}

func (p *compactionPolicy) warnNoHistoryPort(vendor string) {
	p.historyOnce.Do(func() {
		p.warn("warn.history_port_missing", map[string]any{nameParam: vendor})
	})
}

func (p *compactionPolicy) warnNoPort(vendor string) {
	p.warnOnce.Do(func() {
		p.warn("warn.compactor_port_missing", map[string]any{nameParam: vendor})
	})
}

const historyCallTimeout = 2 * time.Minute

type bridgeHistory struct {
	driver *sessionDriver
}

func (h bridgeHistory) ReadHistory(ctx context.Context) (contracts.History, error) {
	var collector historywire.Collector
	err := h.driver.historyThroughBridge(ctx, historywire.Read(h.driver.nextHistoryCall()), func(frame contracts.Event) (bool, error) {
		if frame.T != historywire.EventHistory {
			return false, nil
		}
		return collector.Add(frame)
	})
	if err != nil {
		return contracts.History{}, err
	}
	return collector.History()
}

func (h bridgeHistory) PruneHistory(ctx context.Context, edits []contracts.HistoryEdit) (contracts.PruneOutcome, error) {
	if err := contracts.ValidateHistoryEdits(edits); err != nil {
		return contracts.PruneOutcome{}, err
	}
	request, err := historywire.Prune(h.driver.nextHistoryCall(), edits)
	if errors.Is(err, historywire.ErrTooManyEdits) {
		return contracts.PruneOutcome{}, errTooManyEdits
	}
	if err != nil {
		return contracts.PruneOutcome{}, err
	}
	var outcome contracts.PruneOutcome
	err = h.driver.historyThroughBridge(ctx, request, func(frame contracts.Event) (bool, error) {
		if frame.T != historywire.EventPruned {
			return false, nil
		}
		var decodeErr error
		outcome, decodeErr = historywire.DecodePruned(frame)
		return true, decodeErr
	})
	return outcome, err
}

func (d *sessionDriver) nextHistoryCall() string {
	return strconv.FormatUint(d.historyCalls.Add(1), 36)
}

func (d *sessionDriver) historyThroughBridge(ctx context.Context, request contracts.Event, accept func(contracts.Event) (bool, error)) error {
	ctx, cancel := context.WithTimeoutCause(ctx, historyCallTimeout, errHistoryTimeout)
	defer cancel()
	select {
	case d.toBridge <- request:
	case <-d.hangup:
		return errBridgeGone
	case <-ctx.Done():
		return historyCallEnded(ctx)
	}
	for {
		select {
		case <-ctx.Done():
			d.Interrupt()
			return historyCallEnded(ctx)
		case <-d.hangup:
			return errBridgeGone
		case frame, ok := <-d.from:
			if !ok {
				return errBridgeGone
			}
			if done, err := d.historyAnswer(request, frame, accept); err != nil || done {
				return err
			}
		}
	}
}

func historyCallEnded(ctx context.Context) error {
	if errors.Is(context.Cause(ctx), errHistoryTimeout) {
		return fmt.Errorf("%w: %w", errHistoryTimeout, ctx.Err())
	}
	return ctx.Err()
}

func (d *sessionDriver) historyAnswer(request, frame contracts.Event, accept func(contracts.Event) (bool, error)) (bool, error) {
	if !historywire.IsFrame(frame.T) || frame.Who != request.Who {
		return false, nil
	}
	if frame.T != historywire.EventError {
		return accept(frame)
	}
	if historywire.CodeOf(frame) == historywire.FailureNoEditor {
		d.compaction.warnNoHistoryPort(d.modelNow().vendor)
	}
	return true, historywire.DecodeFailure(frame)
}
