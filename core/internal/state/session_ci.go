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

// CompareAndSetCINudge replaces the CI nudge of the named session by next only when it still
// holds prev, in one step under the state lock. It restores prev and returns the error when the
// state cannot be saved, and reports false when the record no longer holds prev.
func (s *State) CompareAndSetCINudge(name string, prev, next CINudge) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	idx, ok := s.sessionIndexLocked(name)
	if !ok {
		return false, fmt.Errorf("%w: %q", errNoSession, name)
	}
	if s.Sessions[idx].CINudge() != prev {
		return false, nil
	}
	setSessionCINudge(&s.Sessions[idx], next)
	if err := s.saveLocked(); err != nil {
		setSessionCINudge(&s.Sessions[idx], prev)
		return false, err
	}
	return true, nil
}

func setSessionCINudge(session *Session, nudge CINudge) {
	session.CINudgePullRequest = nudge.Number
	session.CINudgeAttempts = nudge.Attempts
	session.CINudgeHead = nudge.Head
}
