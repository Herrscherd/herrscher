package state

import "time"

// ApprovalRules returns a copy of the daemon-wide rule set.
func (s *State) ApprovalRules() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.Approvals...)
}

// AddApprovalRule appends a rendered rule, ignoring one already present, and
// persists. The caller parses first: this stores text it was told is valid.
func (s *State) AddApprovalRule(rendered string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, r := range s.Approvals {
		if r == rendered {
			return nil
		}
	}
	s.Approvals = append(s.Approvals, rendered)
	return s.saveLocked()
}

// RemoveApprovalRule drops a rendered rule, reporting whether it was there.
func (s *State) RemoveApprovalRule(rendered string) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for idx, r := range s.Approvals {
		if r == rendered {
			s.Approvals = append(s.Approvals[:idx], s.Approvals[idx+1:]...)
			return true, s.saveLocked()
		}
	}
	return false, nil
}

// DefaultApprovalWait is how long a pending request waits for a human before it
// is refused. Long enough to walk back to a keyboard, short enough that a
// scheduled turn firing at 3am is not still holding a session at breakfast.
//
// Exported so the broker that does the waiting falls back to the same number
// this file returns, instead of keeping a second copy of it that could drift.
const DefaultApprovalWait = 5 * time.Minute

// ApprovalWait is the configured wait, or five minutes. No verb writes the
// field: it is edited by hand in state.json, so anything unparseable or
// negative is answered with the default here rather than refused at a door
// there is none of.
func (s *State) ApprovalWait() time.Duration {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.ApprovalTimeout == "" {
		return DefaultApprovalWait
	}
	timeout, err := time.ParseDuration(s.ApprovalTimeout)
	if err != nil || timeout <= 0 {
		return DefaultApprovalWait
	}
	return timeout
}
