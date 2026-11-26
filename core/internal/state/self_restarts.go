package state

import (
	"maps"
	"slices"
	"time"
)

// LastSelfRestart is when session last restarted the daemon, and whether it ever did.
func (s *State) LastSelfRestart(session string) (time.Time, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	at, ok := s.SelfRestarts[session]
	return at, ok
}

// RecordSelfRestart notes that session restarts the daemon at when, that its turn resumes once the daemon is back, and forgets the restarts older than forgetBefore.
func (s *State) RecordSelfRestart(session string, when, forgetBefore time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.SelfRestarts == nil {
		s.SelfRestarts = make(map[string]time.Time)
	}
	maps.DeleteFunc(s.SelfRestarts, func(_ string, at time.Time) bool { return at.Before(forgetBefore) })
	s.SelfRestarts[session] = when
	if !slices.Contains(s.RestartResumes, session) {
		s.RestartResumes = append(s.RestartResumes, session)
	}
	return s.saveLocked()
}

// ForgetRestartResume drops session from the sessions to resume, when the restart it asked for did not happen.
func (s *State) ForgetRestartResume(session string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	kept := slices.DeleteFunc(slices.Clone(s.RestartResumes), func(name string) bool { return name == session })
	if len(kept) == len(s.RestartResumes) {
		return nil
	}
	s.RestartResumes = kept
	return s.saveLocked()
}

// TakeRestartResumes returns the sessions waiting to be told the daemon is back, and forgets them.
func (s *State) TakeRestartResumes() ([]string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	taken := s.RestartResumes
	if len(taken) == 0 {
		return nil, nil
	}
	s.RestartResumes = nil
	return taken, s.saveLocked()
}
