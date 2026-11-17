package state

// SetInterruptedTurn marks the turn sent to the backend and not yet settled, or clears the mark when turnID is empty.
func (s *State) SetInterruptedTurn(name, turnID string) error {
	return s.setSessionField(name, func(sess *Session) *string { return &sess.InterruptedTurn }, turnID)
}

// SetResumeTokenAndClearInterruptedTurn stores a restored resume token and drops the mark of the previous conversation in one save; on a failed save both fields stay as they were.
func (s *State) SetResumeTokenAndClearInterruptedTurn(name, token string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for idx := range s.Sessions {
		if s.Sessions[idx].Name != name {
			continue
		}
		sess := &s.Sessions[idx]
		previousToken, previousTurn := sess.ResumeToken, sess.InterruptedTurn
		sess.ResumeToken = token
		sess.InterruptedTurn = ""
		if err := s.saveLocked(); err != nil {
			sess.ResumeToken, sess.InterruptedTurn = previousToken, previousTurn
			return err
		}
		return nil
	}
	return nil
}
