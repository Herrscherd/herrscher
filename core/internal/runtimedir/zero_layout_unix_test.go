//go:build unix

package runtimedir

import (
	"errors"
	"testing"
)

func TestZeroLayoutPrivateReturnsATypedError(t *testing.T) {
	var zero Layout
	var unusable *UnusableLayoutError
	if _, err := zero.Private(); !errors.As(err, &unusable) {
		t.Fatalf("zero Layout Private() = %v, want an UnusableLayoutError", err)
	}
}
