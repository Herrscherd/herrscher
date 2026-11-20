package bridge

import (
	"context"
	"testing"

	contracts "github.com/Herrscherd/herrscher-contracts"
	"github.com/Herrscherd/herrscher/core/internal/lease"
)

type everyPort struct{ contracts.Backend }

func (everyPort) Compact(context.Context, contracts.CompactRequest, func(contracts.BackendEvent)) (contracts.CompactResult, error) {
	return contracts.CompactResult{}, nil
}

func (everyPort) ResumeToken() string { return "tok" }

func (everyPort) InjectChoice(_ context.Context, value string) (string, error) { return value, nil }

func (everyPort) NativeSkills() bool { return true }

func (everyPort) TakeFiles() []contracts.OutboundFile { return nil }

func (everyPort) ReadHistory(context.Context) (contracts.History, error) {
	return contracts.History{}, nil
}

func (everyPort) PruneHistory(context.Context, []contracts.HistoryEdit) (contracts.PruneOutcome, error) {
	return contracts.PruneOutcome{}, nil
}

func TestOptionalPortsAreFoundThroughTheLeaseWrap(t *testing.T) {
	ports := []struct {
		name string
		held func(contracts.Backend) bool
	}{
		{"Compactor", func(b contracts.Backend) bool { _, ok := compactorOf(b); return ok }},
		{"ResumeAware", func(b contracts.Backend) bool { _, ok := resumeAwareOf(b); return ok }},
		{"ChoiceInjector", func(b contracts.Backend) bool { _, ok := choiceInjectorOf(b); return ok }},
		{"SkillNative", func(b contracts.Backend) bool {
			native, ok := skillNativeOf(b)
			return ok && native.NativeSkills()
		}},
		{"FileProducer", func(b contracts.Backend) bool { _, ok := fileProducerOf(b); return ok }},
		{"HistoryEditor", func(b contracts.Backend) bool { _, ok := historyEditorOf(b); return ok }},
	}
	wraps := []struct {
		name  string
		build func() contracts.Backend
	}{
		{"bare", func() contracts.Backend { return everyPort{} }},
		{"leased", func() contracts.Backend { return lease.Wrap(everyPort{}, func() {}) }},
	}
	for _, port := range ports {
		for _, wrap := range wraps {
			t.Run(port.name+"/"+wrap.name, func(t *testing.T) {
				if !port.held(wrap.build()) {
					t.Fatalf("%s not found on a %s backend", port.name, wrap.name)
				}
			})
		}
	}
}
