package state

import "fmt"

// CINudge is what the host last told the agent of a session about the checks of its pull
// request: the number of that request, how many fix attempts were spent on it, and the head
// commit the last message named.
type CINudge struct {
	Number   int
	Attempts int
	Head     string
}

// CINudge returns the CI nudge recorded on the session.
func (s Session) CINudge() CINudge {
	return CINudge{Number: s.CINudgePullRequest, Attempts: s.CINudgeAttempts, Head: s.CINudgeHead}
}

// SetCINudge records nudge on the named session, and restores the previous record when the
// state cannot be saved.
func (s *State) SetCINudge(name string, nudge CINudge) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	idx, ok := s.sessionIndexLocked(name)
	if !ok {
		return fmt.Errorf("%w: %q", errNoSession, name)
	}
	previous := s.Sessions[idx].CINudge()
	if previous == nudge {
		return nil
	}
	setSessionCINudge(&s.Sessions[idx], nudge)
	if err := s.saveLocked(); err != nil {
		setSessionCINudge(&s.Sessions[idx], previous)
		return err
	}
	return nil
}

func setSessionCINudge(session *Session, nudge CINudge) {
	session.CINudgePullRequest = nudge.Number
	session.CINudgeAttempts = nudge.Attempts
	session.CINudgeHead = nudge.Head
}
