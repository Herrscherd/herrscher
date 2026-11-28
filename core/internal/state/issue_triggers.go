package state

import (
	"maps"
	"slices"
	"time"

	contracts "github.com/Herrscherd/herrscher-contracts"
)

const maxTakenIssues = 200

// IssueWatch is how far one issue filter of a repository has been read. Since is the
// instant the next read starts from, inclusive; Revision is the forge's version of the
// listing that read produced, valid only while Since is unchanged.
type IssueWatch struct {
	Since    time.Time          `json:"since,omitzero"`
	Revision contracts.Revision `json:"revision,omitempty"`
}

// TakenIssue is an issue that already opened the session Session.
type TakenIssue struct {
	Number  int    `json:"number"`
	Session string `json:"session"`
}

// IssueTriggerState is the issue trigger record of one repository: a watch per filter
// and the bounded list of issues already taken, newest last.
type IssueTriggerState struct {
	Repo    string                `json:"repo"`
	Watches map[string]IssueWatch `json:"watches,omitempty"`
	Taken   []TakenIssue          `json:"taken,omitempty"`
}

// IssueTriggerRepo returns a copy of the trigger record of repo, zero when it has none.
func (s *State) IssueTriggerRepo(repo string) IssueTriggerState {
	s.mu.Lock()
	defer s.mu.Unlock()
	idx, ok := s.issueTriggerIndexLocked(repo)
	if !ok {
		return IssueTriggerState{}
	}
	record := s.IssueTriggers[idx]
	record.Watches = maps.Clone(record.Watches)
	record.Taken = slices.Clone(record.Taken)
	return record
}

// AdvanceIssueWatch replaces the watch of filter on repo with next only when it still
// holds expected, and reports whether it did. A tick that read the repository while
// another one advanced it gets false and writes nothing.
func (s *State) AdvanceIssueWatch(repo, filter string, expected, next IssueWatch) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	idx, ok := s.issueTriggerIndexLocked(repo)
	if !ok {
		if !sameWatch(IssueWatch{}, expected) {
			return false, nil
		}
		s.IssueTriggers = append(s.IssueTriggers, IssueTriggerState{Repo: repo})
		idx = len(s.IssueTriggers) - 1
	}
	record := &s.IssueTriggers[idx]
	if !sameWatch(record.Watches[filter], expected) {
		return false, nil
	}
	if record.Watches == nil {
		record.Watches = make(map[string]IssueWatch)
	}
	record.Watches[filter] = next
	return true, s.saveLocked()
}

// ClaimIssue records that issue number of repo opens session, and reports false when
// the issue was already taken. The oldest taken issues drop out past the bound.
func (s *State) ClaimIssue(repo string, number int, session string) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	idx, ok := s.issueTriggerIndexLocked(repo)
	if !ok {
		s.IssueTriggers = append(s.IssueTriggers, IssueTriggerState{Repo: repo})
		idx = len(s.IssueTriggers) - 1
	}
	record := &s.IssueTriggers[idx]
	if slices.ContainsFunc(record.Taken, func(taken TakenIssue) bool { return taken.Number == number }) {
		return false, nil
	}
	record.Taken = append(record.Taken, TakenIssue{Number: number, Session: session})
	if overflow := len(record.Taken) - maxTakenIssues; overflow > 0 {
		record.Taken = slices.Delete(record.Taken, 0, overflow)
	}
	return true, s.saveLocked()
}

// ReleaseIssue forgets that issue number of repo was taken, after its session failed
// to open, so the next read can take it again.
func (s *State) ReleaseIssue(repo string, number int) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	idx, ok := s.issueTriggerIndexLocked(repo)
	if !ok {
		return nil
	}
	record := &s.IssueTriggers[idx]
	before := len(record.Taken)
	record.Taken = slices.DeleteFunc(record.Taken, func(taken TakenIssue) bool { return taken.Number == number })
	if len(record.Taken) == before {
		return nil
	}
	return s.saveLocked()
}

func (s *State) issueTriggerIndexLocked(repo string) (int, bool) {
	for idx := range s.IssueTriggers {
		if s.IssueTriggers[idx].Repo == repo {
			return idx, true
		}
	}
	return 0, false
}

func sameWatch(a, b IssueWatch) bool {
	return a.Since.Equal(b.Since) && a.Revision == b.Revision
}

// SessionNamed reports whether a session with this name is recorded, which marks an issue already linked to one.
func (s *State) SessionNamed(name string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	for idx := range s.Sessions {
		if s.Sessions[idx].Name == name {
			return true
		}
	}
	return false
}
