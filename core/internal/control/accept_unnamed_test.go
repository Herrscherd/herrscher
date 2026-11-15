package control

import (
	"errors"
	"testing"
)

func TestListenRefusesAnEmptyPath(t *testing.T) {
	listener, err := Listen(t.Context(), "")
	if !errors.Is(err, errNoSocketPath) {
		t.Fatalf("Listen(\"\") = %v, want errNoSocketPath", err)
	}
	if listener != nil {
		t.Fatal("Listen(\"\") returned a listener")
	}
}
