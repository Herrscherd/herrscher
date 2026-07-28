package host

import (
	"context"
	"testing"
	"time"

	contracts "github.com/Herrscherd/herrscher-contracts"
	"github.com/Herrscherd/herrscher/core/cli"
)

func TestHubDispatchSeedAllowsCoordinatorReentry(t *testing.T) {
	reg := &cli.Registry{}
	h := &hub{reg: reg}
	if err := reg.Add(contracts.New("session", "seed").
		Do(func(context.Context, contracts.Input) (string, error) {
			h.dispatchMu.Lock()
			h.dispatchMu.Unlock()
			return "seeded", nil
		})); err != nil {
		t.Fatal(err)
	}

	done := make(chan struct{})
	var out string
	var err error
	go func() {
		out, err = h.Dispatch(context.Background(), []string{"session", "seed"})
		close(done)
	}()

	select {
	case <-done:
		if err != nil || out != "seeded" {
			t.Fatalf("Dispatch = (%q, %v), want seeded/nil", out, err)
		}
	case <-time.After(250 * time.Millisecond):
		t.Fatal("session seed held dispatchMu across coordinator re-entry")
	}
}
