package state

// SetInterruptedTurn marks the turn sent to the backend and not yet settled, or clears the mark when turnID is empty.
func (s *State) SetInterruptedTurn(name, turnID string) error {
	return s.setSessionField(name, func(sess *Session) *string { return &sess.InterruptedTurn }, turnID)
}
