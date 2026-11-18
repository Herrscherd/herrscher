package host

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"time"

	contracts "github.com/Herrscherd/herrscher-contracts"
	"github.com/Herrscherd/herrscher/core/historywire"
)

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
