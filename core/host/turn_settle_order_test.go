package host

import (
	"context"
	"errors"
	"io"
	"slices"
	"testing"
	"time"

	contracts "github.com/Herrscherd/herrscher-contracts"
	"github.com/Herrscherd/herrscher/core/clitext"
	"github.com/Herrscherd/herrscher/core/eventwire"
	"github.com/Herrscherd/herrscher/core/internal/state"
	"github.com/Herrscherd/herrscher/core/outbox"
)

var errTraceRefused = errors.New("transcript write refused")

const (
	stepAssistant   = "assistant"
	stepInterrupted = "interrupted"
	stepSettled     = "settled"
)

type settleOrder struct {
	driver *sessionDriver
	steps  []string
}

func newSettleOrder(refused string) *settleOrder {
	order := &settleOrder{}
	d := newSessionDriver(newHostMessages(), clitext.NewMessages(), "main", nil, make(chan contracts.Event, 8), make(chan contracts.Event, 8), io.Discard)
	d.gate = noBudgetGate{}
	d.activeTurnID = "turn-1"
	d.sink.Transcript = func(e state.TranscriptEntry) error {
		step := e.Role
		if e.Kind == turnInterruptedKind {
			step = stepInterrupted
		}
		if step == refused {
			return errTraceRefused
		}
		order.steps = append(order.steps, step)
		return nil
	}
	d.sink.TurnSettled = func(string) { order.steps = append(order.steps, stepSettled) }
	order.driver = d
	return order
}

func (o *settleOrder) finish(text string, outcome contracts.TurnOutcome) {
	o.driver.completeTurn(context.Background(), contracts.Event{T: eventwire.EventReply, Text: text, Done: true, Outcome: outcome, TurnID: "turn-1"}, time.Now(), 0, outbox.Frame{})
}

func TestCompletedTurnSettlesOnlyAfterItsReplyIsWritten(t *testing.T) {
	order := newSettleOrder("")
	order.finish("fini", "")

	if want := []string{stepAssistant, stepSettled}; !slices.Equal(order.steps, want) {
		t.Fatalf("steps = %v, want %v", order.steps, want)
	}
}

func TestCompletedTurnKeepsTheMarkWhenItsReplyCannotBeWritten(t *testing.T) {
	order := newSettleOrder(stepAssistant)
	order.finish("fini", "")

	if want := []string{}; !slices.Equal(order.steps, want) {
		t.Fatalf("steps = %v, want no write and no settle", order.steps)
	}
}

func TestInterruptedTurnSettlesOnlyAfterPartialReplyAndNotice(t *testing.T) {
	order := newSettleOrder("")
	order.finish("demi-pensée", contracts.TurnInterrupted)

	if want := []string{stepAssistant, stepInterrupted, stepSettled}; !slices.Equal(order.steps, want) {
		t.Fatalf("steps = %v, want %v", order.steps, want)
	}
}

func TestInterruptedTurnWithoutOutputSettlesOnlyAfterItsNotice(t *testing.T) {
	order := newSettleOrder("")
	order.finish("", contracts.TurnInterrupted)

	if want := []string{stepInterrupted, stepSettled}; !slices.Equal(order.steps, want) {
		t.Fatalf("steps = %v, want %v", order.steps, want)
	}
}

func TestInterruptedTurnKeepsTheMarkWhenItsPartialReplyCannotBeWritten(t *testing.T) {
	order := newSettleOrder(stepAssistant)
	order.finish("demi-pensée", contracts.TurnInterrupted)

	if slices.Contains(order.steps, stepSettled) {
		t.Fatalf("steps = %v, the mark must stay open when the partial reply is not written", order.steps)
	}
}

func TestInterruptedTurnKeepsTheMarkWhenItsNoticeCannotBeWritten(t *testing.T) {
	order := newSettleOrder(stepInterrupted)
	order.finish("demi-pensée", contracts.TurnInterrupted)

	if slices.Contains(order.steps, stepSettled) {
		t.Fatalf("steps = %v, the mark must stay open when the interruption notice is not written", order.steps)
	}
}

func TestInterruptedTurnWithoutOutputKeepsTheMarkWhenItsNoticeCannotBeWritten(t *testing.T) {
	order := newSettleOrder(stepInterrupted)
	order.finish("", contracts.TurnInterrupted)

	if slices.Contains(order.steps, stepSettled) {
		t.Fatalf("steps = %v, the mark must stay open when the interruption notice is not written", order.steps)
	}
}
