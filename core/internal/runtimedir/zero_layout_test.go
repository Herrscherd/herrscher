package runtimedir

import (
	"errors"
	"testing"
)

func TestZeroLayoutNamesNoPath(t *testing.T) {
	var zero Layout
	if got := zero.Dir(); got != "" {
		t.Fatalf("zero Layout Dir() = %q, want empty", got)
	}
	if got := zero.Socket("herrscher-command"); got != "" {
		t.Fatalf("zero Layout Socket() = %q, want empty", got)
	}
	if got := zero.Lock("instance-1"); got != "" {
		t.Fatalf("zero Layout Lock() = %q, want empty", got)
	}
}

func TestZeroLayoutChecksReturnATypedError(t *testing.T) {
	var zero Layout
	var unusable *UnusableLayoutError
	if err := zero.Check("/run/user/1000/herrscher/herrscher-command.sock"); !errors.As(err, &unusable) {
		t.Fatalf("zero Layout Check() = %v, want an UnusableLayoutError", err)
	}
}
