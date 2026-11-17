package state

import "fmt"

// SetResumeToken records the backend resume token for the named session,
// persisting only when it changes. Turns report the same id, so this avoids
// rewriting state.json every turn. A missing session or an unchanged token is a
// no-op.
func (s *State) SetResumeToken(name, token string) error {
	return s.setSessionField(name, func(sess *Session) *string { return &sess.ResumeToken }, token)
}

// MemoryRoots answers which project and agent this session files what it learns
// under. A memory root wins over the placement field of the same name: Project
// may be steering the session into a workspace sub-directory and Agent may be
// demanding an isolated worktree, and neither says where knowledge goes. Every
// path that scopes memory (the supervisor spawning a bridge, the daemon running
// a one-shot seed) asks here, so the two cannot drift.
func (s Session) MemoryRoots() (string, string) {
	project, agent := s.MemoryProject, s.MemoryAgent
	if project == "" {
		project = s.Project
	}
	if agent == "" {
		agent = s.Agent
	}
	return project, agent
}

// SetProjectPinned records the memory project a session settled on and marks it
// chosen, so no later turn re-opens the question. Mirrors SetResumeToken's
// locking and persistence: a missing session, or a project already pinned to the
// same value, is a no-op rather than a rewrite of state.json.
func (s *State) SetProjectPinned(name, project string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for idx := range s.Sessions {
		if s.Sessions[idx].Name != name {
			continue
		}
		if s.Sessions[idx].MemoryProject == project && s.Sessions[idx].ProjectPinned {
			return nil
		}
		s.Sessions[idx].MemoryProject = project
		s.Sessions[idx].ProjectPinned = true
		return s.saveLocked()
	}
	return nil
}

// SetPausedReason records why a session halted on a budget cap ("" clears it,
// resuming the session). Mirrors SetResumeToken's locking + persistence.
func (s *State) SetPausedReason(name, reason string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for idx := range s.Sessions {
		if s.Sessions[idx].Name == name {
			if s.Sessions[idx].PausedReason == reason {
				return nil
			}
			s.Sessions[idx].PausedReason = reason
			return s.saveLocked()
		}
	}
	return nil
}

// SetBudget persists the budget caps and paused reason for the named session
// in one write. Mirrors SetResumeToken/SetPausedReason's locking + persistence.
// Used by session set-budget so raising a cap (which clears PausedReason)
// survives restart. A missing session is a no-op.
func (s *State) SetBudget(name string, costCap float64, tokenCap uint64, cohortCostCap float64, cohortTokenCap uint64, pausedReason string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for idx := range s.Sessions {
		if s.Sessions[idx].Name != name {
			continue
		}
		s.Sessions[idx].CostCap = costCap
		s.Sessions[idx].TokenCap = tokenCap
		s.Sessions[idx].CohortCostCap = cohortCostCap
		s.Sessions[idx].CohortTokenCap = cohortTokenCap
		s.Sessions[idx].PausedReason = pausedReason
		return s.saveLocked()
	}
	return nil
}

// SetCompactPercent stores a session's automatic compaction threshold, nil dropping the override so the config default applies. It returns false when no session matches name.
func (s *State) SetCompactPercent(name string, percent *int) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for idx := range s.Sessions {
		if s.Sessions[idx].Name != name {
			continue
		}
		s.Sessions[idx].CompactPercent = percent
		return true, s.saveLocked()
	}
	return false, nil
}

// SetAccount sets a session account profile, dropping its resume token and interrupted-turn mark when fresh; false when no session matches.
func (s *State) SetAccount(name, account string, fresh bool) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for idx := range s.Sessions {
		if s.Sessions[idx].Name != name {
			continue
		}
		sess := &s.Sessions[idx]
		previous := *sess
		sess.Account = account
		if fresh {
			sess.ResumeToken = ""
			sess.InterruptedTurn = ""
		}
		if err := s.saveLocked(); err != nil {
			*sess = previous
			return true, err
		}
		return true, nil
	}
	return false, nil
}

// SetBackendTarget re-points a live session's backend: it rewrites Vendor and the
// opaque Cmd string (which carries model+effort) and clears the resume token,
// which is backend-specific and meaningless once the vendor/model changes. It also
// drops the interrupted-turn mark, which belongs to the previous backend. The
// caller restarts the supervised child so the new values take effect. A failed
// save restores the session as it was in memory. Returns false when no session
// matches name, and the persistence error when the new target could not be
// written.
func (s *State) SetBackendTarget(name, vendor, cmd, modelID string) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for idx := range s.Sessions {
		if s.Sessions[idx].Name != name {
			continue
		}
		sess := &s.Sessions[idx]
		previous := *sess
		sess.Vendor = vendor
		sess.Cmd = cmd
		sess.ModelID = modelID
		sess.ResumeToken = ""
		sess.InterruptedTurn = ""
		if err := s.saveLocked(); err != nil {
			*sess = previous
			return true, err
		}
		return true, nil
	}
	return false, nil
}

// SetArchived sets a session's archived flag and persists only on change. An
// unknown name is a no-op (best-effort, mirrors SetResumeToken).
func (s *State) SetArchived(name string, archived bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for idx := range s.Sessions {
		if s.Sessions[idx].Name == name {
			if s.Sessions[idx].Archived == archived {
				return nil
			}
			s.Sessions[idx].Archived = archived
			return s.saveLocked()
		}
	}
	return nil
}

// SetSessionApprovals changes one session's approval mode and persists.
func (s *State) SetSessionApprovals(name, mode string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for idx := range s.Sessions {
		if s.Sessions[idx].Name == name {
			s.Sessions[idx].Approvals = mode
			return s.saveLocked()
		}
	}
	return fmt.Errorf("%w: %q", errNoSession, name)
}

// SetSessionLanguage stores the language a session answers in and persists it.
func (s *State) SetSessionLanguage(name, lang string) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for idx := range s.Sessions {
		if s.Sessions[idx].Name == name {
			s.Sessions[idx].Language = lang
			return true, s.saveLocked()
		}
	}
	return false, nil
}
