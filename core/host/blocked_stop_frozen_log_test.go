package host

import (
	"log/slog"
	"testing"
	"time"
)

func TestBlockedStopWithAFrozenJournalStaysWithinItsBound(t *testing.T) {
	for _, c := range blockedWriterCases() {
		for _, path := range c.paths {
			t.Run(c.name+"/"+path.name, func(t *testing.T) {
				name := "inflight-" + newTurnID()
				frozen := freezeLogOn(t, "")
				frozenLine := runInBackground(func() { slog.Warn("frozen line") })
				awaitBackground(t, frozen.entered, "the frozen journal never received a line")
				warnings := newGatedWarnings(name)
				t.Cleanup(warnings.open)
				h := c.start(t, name, blockedGrace, warnings)
				started := time.Now()
				returned := runInBackground(func() { path.run(h, name) })
				t.Cleanup(func() {
					warnings.open()
					frozen.thaw()
					awaitBackground(t, returned, "the stop path did not return once the frozen journal was thawed")
					awaitBackground(t, frozenLine, "the frozen journal line did not return once thawed")
				})
				select {
				case <-returned:
				case <-time.After(c.bound):
					t.Fatalf("a frozen journal held the stop past its bound of %v", c.bound)
				}
				if elapsed := time.Since(started); elapsed > c.bound {
					t.Fatalf("a frozen journal held the stop for %v, past its bound of %v", elapsed.Round(time.Millisecond), c.bound)
				}
			})
		}
	}
}
