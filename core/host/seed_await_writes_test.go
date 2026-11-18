package host

import (
	"io"
	"strings"
	"testing"
	"time"
)

func TestOneShotSeedAwaitsItsWritesBeforeItReturns(t *testing.T) {
	persists := newPersistWrites()
	end := persists.begin()
	release := time.AfterFunc(200*time.Millisecond, end)
	t.Cleanup(func() { release.Stop() })

	started := time.Now()
	awaitOneShotWrites(newHostMessages(), persists, io.Discard, testLanguage())
	if elapsed := time.Since(started); elapsed < 150*time.Millisecond {
		t.Fatalf("the one-shot seed returned after %v, before its write ended", elapsed)
	}
}

func TestOneShotSeedGivesUpOnAStuckWriteAfterItsGrace(t *testing.T) {
	persists := newPersistWrites()
	end := persists.begin()
	t.Cleanup(end)
	var warnings strings.Builder

	started := time.Now()
	awaitOneShotWrites(newHostMessages(), persists, &warnings, testLanguage())
	elapsed := time.Since(started)
	if elapsed < persistWriteGrace-100*time.Millisecond || elapsed > persistWriteGrace+5*time.Second {
		t.Fatalf("the one-shot seed waited %v for a stuck write, want about %v", elapsed, persistWriteGrace)
	}
	if warnings.Len() == 0 {
		t.Fatal("the one-shot seed gave up on a stuck write without a warning")
	}
}
