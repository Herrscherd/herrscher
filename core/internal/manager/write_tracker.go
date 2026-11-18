package manager

// WriteTracker is told when a state write starts and when it ends, so that a daemon stop can wait for the writes in flight.
type WriteTracker interface {
	Begin() func()
}

// SetWriteTracker wires the tracker that follows every state write the handler makes.
func (h *Handler) SetWriteTracker(tracker WriteTracker) { h.writes = tracker }

func (h *Handler) persisted(write func() error) error {
	if h.writes == nil {
		return write()
	}
	end := h.writes.Begin()
	defer end()
	return write()
}
