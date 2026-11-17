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

func TestProductionOneShotRouteDoesNotWaitForAnUnrelatedDaemonWrite(t *testing.T) {
	sessions := newSessionTable(newHostMessages())
	end := sessions.persists.begin()
	t.Cleanup(end)

	oneShot := sessions.oneShotPersists()
	started := time.Now()
	awaitOneShotWrites(sessions.messages, oneShot, io.Discard, testLanguage())
	if elapsed := time.Since(started); elapsed > time.Second {
		t.Fatalf("the handoff brief waited %v for a daemon write it did not start, want none", elapsed)
	}
}

func TestOneShotPersistsIsAChildOfTheDaemonTracker(t *testing.T) {
	sessions := newSessionTable(newHostMessages())
	oneShot := sessions.oneShotPersists()
	if oneShot == sessions.persists || oneShot.parent != sessions.persists {
		t.Fatal("the one-shot of the handoff brief must run on a child of the daemon tracker, not on the tracker itself")
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
