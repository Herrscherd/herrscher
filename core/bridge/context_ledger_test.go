package bridge

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"

	contracts "github.com/Herrscherd/herrscher-contracts"
	"github.com/Herrscherd/herrscher-contracts/i18n"
	"github.com/Herrscherd/herrscher/core/compactwire"
	"github.com/Herrscherd/herrscher/core/eventwire"
	"github.com/Herrscherd/herrscher/core/historywire"
	"github.com/Herrscherd/herrscher/core/identity"
)

type ledgerBackend struct {
	mu      sync.Mutex
	token   string
	script  func(turn int, onEvent func(contracts.BackendEvent)) error
	prompts []contracts.Prompt
}

func (b *ledgerBackend) Respond(_ context.Context, prompt contracts.Prompt, onEvent func(contracts.BackendEvent)) (string, error) {
	b.mu.Lock()
	b.prompts = append(b.prompts, prompt)
	turn := len(b.prompts)
	b.mu.Unlock()
	if b.script != nil {
		if err := b.script(turn, onEvent); err != nil {
			return "", err
		}
	}
	return "ok", nil
}

func (b *ledgerBackend) Close() error { return nil }

func (b *ledgerBackend) ResumeToken() string { return b.token }

func (b *ledgerBackend) contexts() []string {
	b.mu.Lock()
	defer b.mu.Unlock()
	out := make([]string, 0, len(b.prompts))
	for i := range b.prompts {
		out = append(out, b.prompts[i].Context)
	}
	return out
}

type noHistoryBackend struct{ prompts []contracts.Prompt }

func (b *noHistoryBackend) Respond(_ context.Context, prompt contracts.Prompt, _ func(contracts.BackendEvent)) (string, error) {
	b.prompts = append(b.prompts, prompt)
	return "ok", nil
}

func (b *noHistoryBackend) Close() error { return nil }

func deltaAffordances(ledger *contextLedger) affordances {
	return affordances{caps: "  session: list", user: identity.Identity{Name: "Jane Doe"}, ledger: ledger}
}

func runTurnWith(t *testing.T, backend contracts.Backend, aff affordances, incarnation, memory string) {
	t.Helper()
	sink := &recordSink{}
	event := contracts.Event{T: eventwire.EventInput, Text: "hello", SessionIncarnation: incarnation}
	ctrl := &turnController{}
	if err := runOneTurn(context.Background(), sink, backend, stubMemory{recall: memory}, event, ctrl, nil, aff, nil, i18n.Language(0)); err != nil {
		t.Logf("turn ended with %v", err)
	}
}

func TestContextDeltaOmitsAnUnchangedBlockAndResendsAChangedOne(t *testing.T) {
	backend := &ledgerBackend{token: "vendor-1"}
	aff := deltaAffordances(newContextLedger(true))
	runTurnWith(t, backend, aff, "inc-1", "MEM ONE")
	runTurnWith(t, backend, aff, "inc-1", "MEM ONE")
	runTurnWith(t, backend, aff, "inc-1", "MEM TWO")

	contexts := backend.contexts()
	full := composeTurnContext("MEM ONE", nil, deltaAffordances(nil)).String()
	if contexts[0] != full {
		t.Fatalf("first turn context = %q, want the full context %q", contexts[0], full)
	}
	if contexts[1] != "" {
		t.Fatalf("second turn context = %q, want nothing resent when every block is unchanged", contexts[1])
	}
	if !strings.Contains(contexts[2], "MEM TWO") || strings.Contains(contexts[2], "MEM ONE") {
		t.Fatalf("third turn context = %q, want the changed memory alone", contexts[2])
	}
	if strings.Contains(contexts[2], "session: list") {
		t.Fatalf("third turn context = %q, want the unchanged capabilities block kept out", contexts[2])
	}
}

func TestContextDeltaSizesOnlyWhatWasSentThisTurn(t *testing.T) {
	turn := composeTurnContext("MEM", nil, affordances{caps: "  session: list"})
	turn.blocks[1].omitted = true
	turn.blocks[3].omitted = true
	sizes := turn.sizes()
	if len(sizes) != len(turn.blocks) {
		t.Fatalf("sizes = %+v, want every block reported so the parts stay measured", sizes)
	}
	if sizes[3].Tokens != 0 || sizes[0].Tokens == 0 {
		t.Fatalf("sizes = %+v, want the omitted capabilities at 0 and the memory still sized", sizes)
	}
	if strings.Contains(turn.String(), "session: list") {
		t.Fatalf("turn context = %q, want the omitted block left out of the prompt", turn.String())
	}
}

type resetTrigger func(t *testing.T, backend *ledgerBackend, aff affordances)

func newIncarnationTrigger(t *testing.T, backend *ledgerBackend, aff affordances) {
	t.Helper()
	runTurnWith(t, backend, aff, "inc-2", "MEM ONE")
}

func vendorChangedTrigger(t *testing.T, backend *ledgerBackend, _ affordances) {
	t.Helper()
	backend.token = "vendor-2"
}

func vendorLostTrigger(t *testing.T, backend *ledgerBackend, aff affordances) {
	t.Helper()
	backend.token = ""
	runTurnWith(t, backend, aff, "inc-1", "MEM ONE")
}

func noticeTrigger(t *testing.T, backend *ledgerBackend, aff affordances) {
	t.Helper()
	backend.script = func(turn int, onEvent func(contracts.BackendEvent)) error {
		if turn == 2 {
			onEvent(contracts.BackendEvent{Kind: contracts.BackendKindNotice})
		}
		return nil
	}
	runTurnWith(t, backend, aff, "inc-1", "MEM ONE")
}

func attemptResetTrigger(t *testing.T, backend *ledgerBackend, aff affordances) {
	t.Helper()
	backend.script = func(turn int, onEvent func(contracts.BackendEvent)) error {
		if turn == 2 {
			onEvent(contracts.BackendEvent{Kind: backendKindReset})
		}
		return nil
	}
	runTurnWith(t, backend, aff, "inc-1", "MEM ONE")
}

func failedTurnTrigger(t *testing.T, backend *ledgerBackend, aff affordances) {
	t.Helper()
	backend.script = func(turn int, _ func(contracts.BackendEvent)) error {
		if turn == 2 {
			return errors.New("backend down")
		}
		return nil
	}
	runTurnWith(t, backend, aff, "inc-1", "MEM ONE")
}

func compactionTrigger(t *testing.T, backend *ledgerBackend, aff affordances) {
	t.Helper()
	router := turnRouter{sink: &recordSink{}, backend: backend, aff: aff}
	if err := router.route(context.Background(), contracts.Event{T: compactwire.EventRequest}, i18n.Language(0)); err != nil {
		t.Fatalf("compact route: %v", err)
	}
}

func pruneTrigger(t *testing.T, backend *ledgerBackend, aff affordances) {
	t.Helper()
	router := turnRouter{sink: &recordSink{}, backend: backend, aff: aff}
	if err := router.route(context.Background(), contracts.Event{T: historywire.EventPrune}, i18n.Language(0)); err != nil {
		t.Fatalf("prune route: %v", err)
	}
}

func TestContextDeltaResetTriggersResendEverything(t *testing.T) {
	triggers := map[string]resetTrigger{
		"new session incarnation":            newIncarnationTrigger,
		"vendor session changed":             vendorChangedTrigger,
		"vendor session lost after the turn": vendorLostTrigger,
		"resume notice during the turn":      noticeTrigger,
		"attempt reset during the turn":      attemptResetTrigger,
		"failed turn":                        failedTurnTrigger,
		"compaction":                         compactionTrigger,
		"history prune":                      pruneTrigger,
	}
	for name, trigger := range triggers {
		t.Run(name, func(t *testing.T) {
			backend := &ledgerBackend{token: "vendor-1"}
			aff := deltaAffordances(newContextLedger(true))
			runTurnWith(t, backend, aff, "inc-1", "MEM ONE")
			trigger(t, backend, aff)
			runTurnWith(t, backend, aff, "inc-1", "MEM ONE")
			contexts := backend.contexts()
			last := contexts[len(contexts)-1]
			if !strings.Contains(last, "session: list") || !strings.Contains(last, "MEM ONE") {
				t.Fatalf("turn after %s = %q, want the full context resent", name, last)
			}
		})
	}
}

func TestContextDeltaStaysOffWhenTheSwitchIsOff(t *testing.T) {
	backend := &ledgerBackend{token: "vendor-1"}
	aff := deltaAffordances(newContextLedger(false))
	runTurnWith(t, backend, aff, "inc-1", "MEM ONE")
	runTurnWith(t, backend, aff, "inc-1", "MEM ONE")
	contexts := backend.contexts()
	if contexts[0] != contexts[1] || !strings.Contains(contexts[1], "session: list") {
		t.Fatalf("contexts = %q, want every block sent on every turn when the switch is off", contexts)
	}
}

func TestContextDeltaSendsEverythingToBackendsWithoutHistory(t *testing.T) {
	backend := &noHistoryBackend{}
	aff := deltaAffordances(newContextLedger(true))
	runTurnWith(t, backend, aff, "inc-1", "MEM ONE")
	runTurnWith(t, backend, aff, "inc-1", "MEM ONE")
	if backend.prompts[0].Context != backend.prompts[1].Context || !strings.Contains(backend.prompts[1].Context, "session: list") {
		t.Fatalf("contexts = %q and %q, want a backend that keeps no history to get everything every turn", backend.prompts[0].Context, backend.prompts[1].Context)
	}
}

func TestContextDeltaSendsEverythingWithoutASessionIncarnation(t *testing.T) {
	backend := &ledgerBackend{token: "vendor-1"}
	aff := deltaAffordances(newContextLedger(true))
	runTurnWith(t, backend, aff, "", "MEM ONE")
	runTurnWith(t, backend, aff, "", "MEM ONE")
	contexts := backend.contexts()
	if contexts[0] != contexts[1] || !strings.Contains(contexts[1], "session: list") {
		t.Fatalf("contexts = %q, want full context when the session cannot be identified", contexts)
	}
}

func TestContextDeltaOneShotRouterWithoutLedgerSendsEverything(t *testing.T) {
	backend := &ledgerBackend{token: "vendor-1"}
	aff := deltaAffordances(nil)
	runTurnWith(t, backend, aff, "inc-1", "MEM ONE")
	runTurnWith(t, backend, aff, "inc-1", "MEM ONE")
	contexts := backend.contexts()
	if contexts[0] != contexts[1] || !strings.Contains(contexts[1], "session: list") {
		t.Fatalf("contexts = %q, want full context for a path with no ledger", contexts)
	}
}

func TestContextDeltaSessionsKeepSeparateDigests(t *testing.T) {
	var wg sync.WaitGroup
	results := make([][]string, 2)
	for i := range results {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			backend := &ledgerBackend{token: "vendor-" + string(rune('a'+i))}
			aff := deltaAffordances(newContextLedger(true))
			runTurnWith(t, backend, aff, "inc-"+string(rune('a'+i)), "MEM ONE")
			runTurnWith(t, backend, aff, "inc-"+string(rune('a'+i)), "MEM ONE")
			results[i] = backend.contexts()
		}(i)
	}
	wg.Wait()
	for i, contexts := range results {
		if !strings.Contains(contexts[0], "session: list") || contexts[1] != "" {
			t.Fatalf("session %d contexts = %q, want its own first turn full and its second turn delta", i, contexts)
		}
	}
}

func TestKeepsHistoryReadsOnlyTheResumeSignal(t *testing.T) {
	if keepsHistory(&noHistoryBackend{}) {
		t.Fatal("a backend without a resume token must not be treated as keeping history")
	}
	if !keepsHistory(&ledgerBackend{}) {
		t.Fatal("a backend exposing its resume token must be treated as keeping history")
	}
}
