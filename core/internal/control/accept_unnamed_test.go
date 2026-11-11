package control

import (
	"errors"
	"testing"
)

func TestAcceptRefusesAnEmptyPath(t *testing.T) {
	acceptor, err := Accept(t.Context(), "")
	if !errors.Is(err, errNoSocketPath) {
		t.Fatalf("Accept(\"\") = %v, want errNoSocketPath", err)
	}
	if acceptor != nil {
		t.Fatal("Accept(\"\") returned a listener")
	}
}
