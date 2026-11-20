package bridge

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	contracts "github.com/Herrscherd/herrscher-contracts"
	"github.com/Herrscherd/herrscher-contracts/i18n"
	"github.com/Herrscherd/herrscher/core/eventwire"
)

type askRecord struct {
	call   string
	asked  bool
	answer contracts.ApprovalAnswer
}

type askingBackend struct {
	records chan askRecord
}

func (b askingBackend) ask(ctx context.Context, call string) {
	asker, ok := contracts.ApprovalAskerFrom(ctx)
	record := askRecord{call: call, asked: ok}
	if ok {
		record.answer = asker(ctx, contracts.ApprovalRequest{ID: call, Tool: "Bash", Subject: "git push"})
	}
	b.records <- record
}

func (b askingBackend) Respond(ctx context.Context, _ contracts.Prompt, _ func(contracts.BackendEvent)) (string, error) {
	b.ask(ctx, "respond")
	return "", nil
}

func (b askingBackend) Compact(ctx context.Context, _ contracts.CompactRequest, _ func(contracts.BackendEvent)) (contracts.CompactResult, error) {
	b.ask(ctx, "compact")
	return contracts.CompactResult{}, errors.New("stop after the asker")
}

func (askingBackend) Close() error { return nil }

func operatorAllows(t *testing.T, sent <-chan contracts.Event, approvals *approvalChannel) {
	t.Helper()
	for event := range sent {
		var req contracts.ApprovalRequest
		if err := json.Unmarshal([]byte(event.Value), &req); err != nil {
			t.Errorf("request %q unreadable: %v", event.Value, err)
			return
		}
		payload, err := json.Marshal(contracts.ApprovalAnswer{ID: req.ID, Decision: contracts.DecisionAllow})
		if err != nil {
			t.Errorf("answer: %v", err)
			return
		}
		approvals.deliver(contracts.Event{T: eventwire.EventApprovalAnswer, Value: string(payload)})
	}
}

func newChannelUnderOperator(t *testing.T) (*approvalChannel, chan contracts.Event) {
	t.Helper()
	sent := make(chan contracts.Event, 8)
	approvals := newApprovalChannel(func(event contracts.Event) error {
		sent <- event
		return nil
	})
	go operatorAllows(t, sent, approvals)
	t.Cleanup(func() { close(sent) })
	return approvals, sent
}

func TestTurnCompactAndTurnEachReceiveTheBridgeAsker(t *testing.T) {
	approvals, _ := newChannelUnderOperator(t)
	records := make(chan askRecord, 3)
	backend := askingBackend{records: records}
	aff := affordances{approvals: approvals}
	sink := &recordSink{}
	input := contracts.Event{T: eventwire.EventInput, Text: "une question"}

	if err := runOneTurn(context.Background(), sink, backend, nil, input, nil, nil, aff, nil, i18n.English); err != nil {
		t.Fatalf("first turn: %v", err)
	}
	runCompact(context.Background(), sink, backend, nil, approvals, i18n.English, compactFrame(t))
	if err := runOneTurn(context.Background(), sink, backend, nil, input, nil, nil, aff, nil, i18n.English); err != nil {
		t.Fatalf("second turn: %v", err)
	}

	for _, want := range []string{"respond", "compact", "respond"} {
		record := <-records
		if record.call != want {
			t.Fatalf("call %q, want %q: the calls must reach the backend in order", record.call, want)
		}
		if !record.asked {
			t.Fatalf("%s reached the backend without an approval asker in its context", want)
		}
		if record.answer.ID != want || !record.answer.Allowed() {
			t.Fatalf("%s answer %+v, want the operator's allow for that request", want, record.answer)
		}
	}
}

func TestApprovalChannelRefusesWithALocalizedReasonWhenTheHubIsGone(t *testing.T) {
	failure := errors.New("hub socket closed")
	approvals := newApprovalChannel(func(contracts.Event) error { return failure })
	req := contracts.ApprovalRequest{ID: "req-1", Tool: "Bash", Subject: "git push"}

	english := approvals.asker(i18n.English)(context.Background(), req)
	if english.ID != "req-1" || english.Allowed() || !strings.Contains(english.Reason, "hub socket closed") || !strings.Contains(english.Reason, "approval") {
		t.Fatalf("english refusal %+v, want a denial carrying the cause in english", english)
	}
	french := approvals.asker(i18n.French)(context.Background(), req)
	if french.Allowed() || !strings.Contains(french.Reason, "hub socket closed") || !strings.Contains(french.Reason, "approbation") {
		t.Fatalf("french refusal %+v, want a denial carrying the cause in french", french)
	}
}

func TestApprovalChannelRefusesWhenTheTurnEndsBeforeTheAnswer(t *testing.T) {
	approvals := newApprovalChannel(func(contracts.Event) error { return nil })
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	answer := approvals.asker(i18n.English)(ctx, contracts.ApprovalRequest{ID: "req-2", Tool: "Bash", Subject: "git push"})
	if answer.ID != "req-2" || answer.Allowed() {
		t.Fatalf("answer %+v, want a denial once the turn is cancelled", answer)
	}
}

func TestApprovalChannelIgnoresAnswersNobodyAwaits(t *testing.T) {
	approvals := newApprovalChannel(func(contracts.Event) error { return nil })
	if !approvals.deliver(contracts.Event{T: eventwire.EventApprovalAnswer, Value: `{"ID":"nobody","Decision":"allow"}`}) {
		t.Fatal("an approval answer was not recognised as one")
	}
	if approvals.deliver(contracts.Event{T: eventwire.EventChunk, Text: "chunk"}) {
		t.Fatal("a chunk was taken for an approval answer")
	}
}
