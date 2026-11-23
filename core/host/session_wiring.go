package host

import "context"

func (t *sessionTable) attachTelemetry(driver *sessionDriver) {
	if t == nil {
		return
	}
	driver.telemetry = t.telemetry
}

func (t *sessionTable) attachCompaction(driver *sessionDriver, name string) {
	if t == nil {
		return
	}
	driver.compaction = t.compaction
	driver.percentOf = func() int { return t.percentOfSession(name) }
}

func (d *sessionDriver) awaitTurnRelease(ctx context.Context) {
	select {
	case d.turn <- struct{}{}:
		<-d.turn
	case <-ctx.Done():
	}
}
