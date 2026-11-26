package state

import "slices"

// SessionApprovalRules returns the rules that bind session alone, and whether the session exists.
func (s *State) SessionApprovalRules(session string) ([]string, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for idx := range s.Sessions {
		if s.Sessions[idx].Name == session {
			return slices.Clone(s.Sessions[idx].ApprovalRules), true
		}
	}
	return nil, false
}

// AddSessionApprovalRule appends a rendered rule to session, ignoring one already there, and persists. It returns false when no session matches.
func (s *State) AddSessionApprovalRule(session, rendered string) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for idx := range s.Sessions {
		if s.Sessions[idx].Name != session {
			continue
		}
		if !slices.Contains(s.Sessions[idx].ApprovalRules, rendered) {
			s.Sessions[idx].ApprovalRules = append(s.Sessions[idx].ApprovalRules, rendered)
		}
		return true, s.saveLocked()
	}
	return false, nil
}

// RemoveSessionApprovalRule drops a rendered rule from session, reporting whether it was there.
func (s *State) RemoveSessionApprovalRule(session, rendered string) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for idx := range s.Sessions {
		if s.Sessions[idx].Name != session {
			continue
		}
		found := slices.Index(s.Sessions[idx].ApprovalRules, rendered)
		if found < 0 {
			return false, nil
		}
		s.Sessions[idx].ApprovalRules = slices.Delete(s.Sessions[idx].ApprovalRules, found, found+1)
		return true, s.saveLocked()
	}
	return false, nil
}
