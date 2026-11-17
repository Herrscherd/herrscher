package state

// RoleAssignments returns a copy of the role table.
func (s *State) RoleAssignments() map[string]RoleAssignment {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make(map[string]RoleAssignment, len(s.Roles))
	for k, v := range s.Roles {
		out[k] = v
	}
	return out
}

// RoleNames is the table as the pure authorization package reads it: principal
// to role name, with the display label dropped. It decides nothing, so it does
// not travel to the code that decides.
func (s *State) RoleNames() map[string]string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.Roles) == 0 {
		return nil
	}
	out := make(map[string]string, len(s.Roles))
	for k, v := range s.Roles {
		out[k] = v.Role
	}
	return out
}

// SetRole assigns a role to a principal and persists. It reports whether the
// table was empty before, because that first assignment is a cliff: from then
// on every gateway account without a role is an observer. The caller validates
// the role name first; this stores text it was told is valid, like
// AddApprovalRule.
func (s *State) SetRole(principal, role, label string) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	first := len(s.Roles) == 0
	if s.Roles == nil {
		s.Roles = make(map[string]RoleAssignment)
	}
	s.Roles[principal] = RoleAssignment{Role: role, Label: label}
	return first, s.saveLocked()
}

// RemoveRole drops an assignment, reporting whether it was there.
func (s *State) RemoveRole(principal string) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.Roles[principal]; !ok {
		return false, nil
	}
	delete(s.Roles, principal)
	if len(s.Roles) == 0 {
		s.Roles = nil
	}
	return true, s.saveLocked()
}
