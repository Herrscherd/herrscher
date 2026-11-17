package host

import (
	"bytes"
	"context"
	"fmt"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	contracts "github.com/Herrscherd/herrscher-contracts"
	"github.com/Herrscherd/herrscher/core/eventwire"
	"github.com/Herrscherd/herrscher/core/historywire"
	"github.com/Herrscherd/herrscher/core/internal/control"
)

type frameCommitBackend struct {
	mu        sync.Mutex
	frames    int
	committed int
}

func (b *frameCommitBackend) Respond(_ context.Context, _ contracts.Prompt, onEvent func(contracts.BackendEvent)) (string, error) {
	onEvent(contracts.BackendEvent{Kind: "usage", InTokens: 950})
	return "reply", nil
}

func (b *frameCommitBackend) Close() error { return nil }

func (b *frameCommitBackend) ReadHistory(context.Context) (contracts.History, error) {
	return contracts.History{}, nil
}

func (b *frameCommitBackend) PruneHistory(_ context.Context, edits []contracts.HistoryEdit) (contracts.PruneOutcome, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.frames++
	b.committed += len(edits)
	return contracts.PruneOutcome{Edited: len(edits)}, nil
}

func (b *frameCommitBackend) applied() (int, int) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.frames, b.committed
}

type floodStage struct {
	count int
}

func (s floodStage) Compact(ctx context.Context, req contracts.CompactRequest, _ func(contracts.BackendEvent)) (contracts.CompactResult, error) {
	_, err := req.History.PruneHistory(ctx, numberedEdits(s.count))
	return contracts.CompactResult{}, err
}

func TestSeedRefusesMoreEditsThanOneFrameAndSaysSo(t *testing.T) {
	backend := &frameCommitBackend{}
	var emissions seedEmissions
	reply, err := seedWithStage(floodStage{count: historywire.MaxPruneEdits + 1}, backend, time.Minute, &emissions)
	if err != nil || reply != "reply" {
		t.Fatalf("seed = (%q, %v), want the reply the turn produced", reply, err)
	}
	if frames, committed := backend.applied(); frames != 0 || committed != 0 {
		t.Fatalf("backend got %d frames committing %d edits, want none: a compaction that cannot fit one frame must edit nothing", frames, committed)
	}
	kinds := emissions.kinds()
	if !slices.Contains(kinds, eventwire.EventCompactionFailed) {
		t.Fatalf("events = %v, want the refused compaction announced as failed", kinds)
	}
	if slices.Contains(kinds, eventwire.EventCompactionDone) {
		t.Fatalf("events = %v, a refused compaction must not report itself done", kinds)
	}
}

func TestSeedPrunesTheMaximumOfEditsInOneFrame(t *testing.T) {
	backend := &frameCommitBackend{}
	var emissions seedEmissions
	reply, err := seedWithStage(floodStage{count: historywire.MaxPruneEdits}, backend, time.Minute, &emissions)
	if err != nil || reply != "reply" {
		t.Fatalf("seed = (%q, %v), want the reply the turn produced", reply, err)
	}
	if frames, committed := backend.applied(); frames != 1 || committed != historywire.MaxPruneEdits {
		t.Fatalf("backend got %d frames committing %d edits, want one frame with all %d", frames, committed, historywire.MaxPruneEdits)
	}
	if !slices.Contains(emissions.kinds(), eventwire.EventCompactionDone) {
		t.Fatalf("events = %v, want the compaction reported as done", emissions.kinds())
	}
}

func TestMaximumPruneFrameFitsTheControlWire(t *testing.T) {
	const callIDWidth = 64
	edits := make([]contracts.HistoryEdit, historywire.MaxPruneEdits)
	for i := range edits {
		edits[i] = contracts.HistoryEdit{CallID: fmt.Sprintf("%0*d", callIDWidth, i), Action: contracts.PruneResult, KeepChars: 9}
	}
	frame, err := historywire.Prune("p1", edits)
	if err != nil {
		t.Fatalf("Prune of %d edits with %d-byte call ids: %v", len(edits), callIDWidth, err)
	}
	var wire bytes.Buffer
	if err := control.WriteEvent(&wire, frame); err != nil {
		t.Fatalf("WriteEvent: %v", err)
	}
	t.Logf("%d edits, %d-byte call ids: frame text %d bytes, control line %d bytes", len(edits), callIDWidth, len(frame.Text), wire.Len())
	if headroom := control.MaxEventLine / 2; wire.Len() > headroom {
		t.Fatalf("the largest prune frame takes %d bytes of a %d byte control line, more than the %d the escaping headroom allows", wire.Len(), control.MaxEventLine, headroom)
	}
	var read []contracts.Event
	if err := control.ScanEvents(strings.NewReader(wire.String()), func(event contracts.Event) error {
		read = append(read, event)
		return nil
	}); err != nil {
		t.Fatalf("ScanEvents: %v, the frame does not fit the control line", err)
	}
	if len(read) != 1 || read[0].Text != frame.Text {
		t.Fatalf("read %d events, want the prune frame back intact", len(read))
	}
}
