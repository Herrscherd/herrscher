package supervisor

import (
	"errors"
	"time"
)

// BridgeTermGrace is how long a local bridge may take to exit after SIGTERM before the supervisor kills it.
const BridgeTermGrace = 3 * time.Second

// ErrDrainBeforeCancel means DrainBridges was called while the supervisor's context was still live, so no loop was asked to stop and waiting would only run out the clock.
var ErrDrainBeforeCancel = errors.New("bridges drained before the supervisor context was cancelled")

const bridgeDrainSlack = time.Second

// DrainBound is the longest DrainBridges waits for the bridge loops once the supervisor context is cancelled.
const DrainBound = BridgeTermGrace + bridgeDrainSlack

// DrainBridges waits for every bridge loop to return, for at most DrainBound.
// Call it after the supervisor's context is cancelled, so that no local bridge outlives the daemon.
func (s *Supervisor) DrainBridges() error {
	if s.ctx == nil || s.ctx.Err() == nil {
		return ErrDrainBeforeCancel
	}

	s.mu.Lock()
	dones := make([]chan struct{}, 0, len(s.runs))
	for _, run := range s.runs {
		dones = append(dones, run.done)
	}
	s.mu.Unlock()

	expired := time.After(DrainBound)
	for _, done := range dones {
		select {
		case <-done:
		case <-expired:
			return ErrStillStopping
		}
	}
	return nil
}
