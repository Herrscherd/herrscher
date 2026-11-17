package host

import (
	"io"
	"testing"

	contracts "github.com/Herrscherd/herrscher-contracts"
	"github.com/Herrscherd/herrscher/core/internal/state"
)

func TestATapIsRefusedOnceTheSeedShutsItsGate(t *testing.T) {
	var published []string
	runtime := oneShotSeedRuntime{publish: func(_ string, event contracts.Event) {
		published = append(published, event.T)
	}}
	sess := state.Session{Name: "window"}
	driver := newSessionDriver(sess.Name, nil, make(chan contracts.Event, 1), make(chan contracts.Event, 1), io.Discard)
	gate := attachOneShotTaps(driver, sess, runtime)

	driver.tapEvent(contracts.Event{T: "before"})
	gate.shut()
	driver.tapEvent(contracts.Event{T: "during consolidation"})

	if len(published) != 1 || published[0] != "before" {
		t.Fatalf("published = %v, want only the tap admitted before the gate shut", published)
	}
}
