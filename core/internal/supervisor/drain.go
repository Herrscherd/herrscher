package supervisor

import "time"

// BridgeTermGrace is how long a bridge may take to exit after SIGTERM before the supervisor kills it.
const BridgeTermGrace = 3 * time.Second

const bridgeDrainSlack = time.Second

// DrainBridges waits for every bridge loop to return, for at most BridgeTermGrace plus a second.
// Call it after the supervisor's context is cancelled, so that no bridge outlives the daemon.
func (s *Supervisor) DrainBridges() error {
	s.mu.Lock()
	dones := make([]chan struct{}, 0, len(s.runs))
	for _, run := range s.runs {
		dones = append(dones, run.done)
	}
	s.mu.Unlock()

	expired := time.After(BridgeTermGrace + bridgeDrainSlack)
	for _, done := range dones {
		select {
		case <-done:
		case <-expired:
			return ErrStillStopping
		}
	}
	return nil
}
