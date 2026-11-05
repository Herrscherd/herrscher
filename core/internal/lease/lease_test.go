package lease

import (
	"context"
	"errors"
	"testing"

	contracts "github.com/Herrscherd/herrscher-contracts"
)

type closing struct{ closed int }

func (c *closing) Respond(context.Context, contracts.Prompt, func(contracts.BackendEvent)) (string, error) {
	return "", nil
}

func (c *closing) Close() error {
	c.closed++
	return errors.New("close failed")
}

func TestCloseReleasesOnceAndKeepsTheInnerError(t *testing.T) {
	inner := &closing{}
	releases := 0
	backend := Wrap(inner, func() { releases++ })
	if err := backend.Close(); err == nil || err.Error() != "close failed" {
		t.Fatalf("Close error = %v, want the inner error", err)
	}
	if err := backend.Close(); err == nil {
		t.Fatal("second Close error = nil, want the inner error again")
	}
	if releases != 1 {
		t.Fatalf("release ran %d times, want 1", releases)
	}
}

func TestUnwrapReturnsTheInnerBackend(t *testing.T) {
	inner := &closing{}
	if got := Unwrap(Wrap(inner, func() {})); got != contracts.Backend(inner) {
		t.Fatalf("Unwrap = %v, want the inner backend", got)
	}
	if got := Unwrap(inner); got != contracts.Backend(inner) {
		t.Fatalf("Unwrap of a bare backend = %v, want it unchanged", got)
	}
}
