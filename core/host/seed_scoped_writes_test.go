package host

import (
	"io"
	"testing"
	"time"
)

func TestOneShotDoesNotWaitForAnUnrelatedDaemonWrite(t *testing.T) {
	shared := newPersistWrites()
	end := shared.begin()
	t.Cleanup(end)

	oneShot := shared.child()
	started := time.Now()
	awaitOneShotWrites(newHostMessages(), oneShot, io.Discard, testLanguage())
	if elapsed := time.Since(started); elapsed > time.Second {
		t.Fatalf("the one-shot waited %v for a daemon write it did not start, want none", elapsed)
	}
}

func TestOneShotWriteIsSeenByTheDaemonStop(t *testing.T) {
	shared := newPersistWrites()
	oneShot := shared.child()
	end := oneShot.begin()
	if shared.group.pending() != 1 {
		t.Fatal("a write started by the one-shot is not in the daemon's group: a stop would not wait for it")
	}
	end()
	if shared.group.pending() != 0 {
		t.Fatal("the one-shot write did not leave the daemon's group when it ended")
	}
}
