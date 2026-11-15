package supervisor

import (
	"context"
	"errors"
	"testing"
)

func TestDrainBridgesRefusesWhileContextIsLive(t *testing.T) {
	sup := NewSupervisor(context.Background(), "herrscher", testLayout(t))

	if err := sup.DrainBridges(); !errors.Is(err, ErrDrainBeforeCancel) {
		t.Fatalf("DrainBridges on a live context = %v, want ErrDrainBeforeCancel", err)
	}
}
