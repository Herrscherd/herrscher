package state

func (s *State) setSessionField(name string, field func(*Session) *string, value string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for idx := range s.Sessions {
		if s.Sessions[idx].Name != name {
			continue
		}
		current := field(&s.Sessions[idx])
		if *current == value {
			return nil
		}
		previous := *current
		*current = value
		if err := s.saveLocked(); err != nil {
			*current = previous
			return err
		}
		return nil
	}
	return nil
}
